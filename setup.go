package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)


func firstTimeSetup(configFile *os.File) {
	var username string
	var token string

	var haveToken string

	fmt.Printf("Enter your factorio username/email: ")
	_, err := fmt.Scanln(&username)
	checkErr(err, "\nPlease enter a vaild username")

	fmt.Printf("Do you have your factorio token? [Y/n] ")
	_, err = fmt.Scanln(&haveToken)
	checkErr(err, "\nPlease enter valid input")
	if haveToken == "" || (haveToken[0] != 'Y' && haveToken[0] != 'y') {
		fmt.Printf("Please enter your factorio password: ")
		var password string
		bytesPass, err := term.ReadPassword(int(os.Stdin.Fd()))
		checkErr(err, "\nPlease enter valid input")
		password = string(bytesPass)

		token, err = getAuthToken(username, password)
		checkErr(err, "Couldn't get auth token, please try again.")
		println("\nGot factorio token")
	} else {
		fmt.Printf("Please enter your factorio token: ")
		_, err = fmt.Scanln(&token)
		checkErr(err, "\nPlease enter valid input")
	}
	fileConfig["username"] = username
	fileConfig["token"] = token

	fmt.Printf("Please enter the directory you have factorio installed (or want it installed in): ")
	var installDirPath string
	_, err = fmt.Scanln(&installDirPath)
	checkErr(err, "\nPlease enter valid input")
	if strings.HasPrefix(installDirPath, "~") {
		installDirPath = strings.Replace(installDirPath, "~", os.Getenv("HOME"), 1)
	}
	installDirStat, err := os.Stat(installDirPath)
	if err != nil {
		fmt.Println("Attmepting to make new directory at:", installDirPath)
		err = os.MkdirAll(installDirPath, os.ModePerm)
		checkErr(err, "Couldn't make new directory")
	} else if !installDirStat.IsDir() {
		fmt.Println("A file already exists at ", installDirPath, ", please remove it")
		os.Exit(0)
	}

	fileConfig["installDir"] = installDirPath

	// Determine if a factorio installation exists, and version
	changelogPath := installDirPath + "/data/changelog.txt"
	_, err = os.Stat(changelogPath)
	if err != nil {
		fmt.Println("Can't find current factorio installation, setting current version to 0.0.0")
		fileConfig["currentVersion"] = "0.0.0"
	} else {
		// Fairly awful hack based on version numbers in changelog
		changelogFile, err := os.Open(changelogPath)
		if err != nil {
			fmt.Println("Can't find current factorio installation, setting current version to 0.0.0")
			fileConfig["currentVersion"] = "0.0.0"
			os.Exit(0)
		}
		version := make([]byte, 16)
		_, err = changelogFile.ReadAt(version, 100)
		checkErr(err, "Couldn't read changelog file")
		version = bytes.Trim(version, "Version: \n")
		fileConfig["currentVersion"] = string(version)
		fmt.Println("Setting factorio version as: ", string(version))
	}
	fileConfig["wantedVersion"] = "stable"

	configData, err := json.MarshalIndent(fileConfig, "", "\t")
	checkErr(err, "Couldn't convert config to json")

	// Actually make new user config
	bytesWritten, err := configFile.Write(configData)
	if err != nil || bytesWritten < len(configData) {
		fmt.Println("Error, config file may be malformed!")
		os.Exit(1)
	}
	fmt.Println("Setup complete! Further options can be configured using the cli")
	return
}

type configStruct struct {
	CurrentVersion string `json:"currentVersion"`
	InstallDir     string `json:"installDir"`
	Token          string `json:"token"`
	Username       string `json:"username"`
	WantedVersion  string `json:"wantedVersion"`
}

func editFileConfig(changeKey string, newValue string) {
	configFilepath := runtimeConfig["configFile"].(string)

	if _, ok := fileConfig[changeKey]; !ok {
		fmt.Printf("%s is not a valid config setting!\n", changeKey)
		return
	}

	fileConfig[changeKey] = newValue
	configData, err := json.MarshalIndent(fileConfig, "", "\t")
	checkErr(err, "Couldn't convert key to json")
	replaceExistingFile(configData, configFilepath)
}

func init() {
	// Parse command line args
	for _, arg := range os.Args {
		if arg == "-v" {
			runtimeConfig["verbose"] = 4
		} else if strings.HasPrefix(arg, "--verbose=") {
			runtimeConfig["verbose"], _ = strconv.Atoi(arg[10:])
		}
	}
	if _, ok := runtimeConfig["verbose"]; !ok {
		runtimeConfig["verbose"] = 0
	}

	Client := http.Client{
		Timeout: 30 * time.Second,
	}
	runtimeConfig["Client"] = Client

	// Allow users to set custom config dir by setting enviroment
	// variable 'FACCONFIGDIR' (needs a better name)
	res, set := os.LookupEnv("FACCONFIGDIR")
	var configDir string
	if !set {
		homeDir := os.Getenv("HOME")
		configDir = homeDir + "/.config/fac-update"
	} else {
		configDir = res
	}
	configDirPresent, err := os.Stat(configDir)
	if err != nil || !configDirPresent.IsDir() {
		err = os.Mkdir(configDir, os.ModePerm)
		if err != nil {
			println("Could find config dir, please check permissions at " + configDir)
			os.Exit(1)
		}
	}
	configFilePath := configDir + "/config.json"
	configFile, err := os.OpenFile(configFilePath, os.O_RDWR|os.O_CREATE, os.ModePerm)
	defer configFile.Close()
	checkErr(err, "Could open or create new config file, please check permissions at "+configDir)

	configFileData := make([]byte, 512)
	bytesRead, err := configFile.Read(configFileData)
	if err != nil && err != io.EOF {
		println("Couldn't read from config file, please check permissions at " + configFilePath)
		verbosePrint(2, "Got error: %s", err)
		os.Exit(1)
	}
	// Assume first time setup if no data was read, populate config map then write to config file
	if bytesRead == 0 {
		println("Running first time setup: ")
		firstTimeSetup(configFile)
		os.Exit(0)
	} else {
		// Trim unused buffer
		configFileData = bytes.TrimRight(configFileData, "\x00")
		setupConfigFromFile(configFileData)
	}

	runtimeConfig["configFile"] = configFilePath
	verbosePrint(6, "Got config as: %v\n", fileConfig)
}
