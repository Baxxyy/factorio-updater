package main

import (
	"fmt"
	"encoding/json"
	"os"
	"strings"
	"strconv"
)

func checkErr(err error, errMsg string) {
	if err != nil {
		verbosePrint(1, "err: %s\n", err)
		fmt.Printf("%s\nAborting\n", errMsg)
		os.Exit(1)
	}
}

func cleanup(configFile *os.File, filename string, delete bool) {
	defer configFile.Close()
	if delete {
		os.Remove(filename)
	}
}

func getMods()(mods modEntries) {
	modsFile := fileConfig["installDir"] + "/mods/mod-list.json"
	modInfo, err := os.ReadFile(modsFile)
	checkErr(err, "Couldn't read mod-list.json")
	var modData modEntries
	json.Unmarshal(modInfo, &modData)
	return modData
}

func verbosePrint(verboseLevel int, msg string, args ...any) {
	if runtimeConfig["verbose"].(int) >= verboseLevel {
		fmt.Printf(msg, args...)
	}
}

// Replace an existing file by creating a new file called <filename>.bak
// and ensuring it is fully created before overwritng the old one. hopefully
// should avoid problem of getting interupted during replacement
func replaceExistingFile(data []byte, fileName string) {
	tmpFile := fileName + ".bak"
	newFile, err := os.OpenFile(tmpFile, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if (err != nil) {
		println("Couldn't write to directory")
		cleanup(newFile, tmpFile, false)
		return
	}

	bytesWritten, err := newFile.Write(data)
	if err != nil || bytesWritten < len(data) {
		fmt.Println("Error: couldn't make new file!")
		verbosePrint(2, "Got err: %S\n", err)
		cleanup(newFile, tmpFile, true)
		return
	}

	err = os.Rename(tmpFile, fileName)
	if err != nil {
		println("Couldn't make overwrite current file")
		cleanup(newFile, tmpFile, true)
		return
	}
}

func setupConfigFromFile(configData []byte) {
	var jsonConfig configStruct
	err := json.Unmarshal(configData, &jsonConfig)
	checkErr(err, "Couldn't decode config file data, is it malformed?")

	fileConfig["token"] = jsonConfig.Token
	fileConfig["username"] = jsonConfig.Username
	fileConfig["installDir"] = jsonConfig.InstallDir
	fileConfig["wantedVersion"] = jsonConfig.WantedVersion
	fileConfig["currentVersion"] = jsonConfig.CurrentVersion
}

// Return True if newVer > curVer
func checkVersionString(curVer string, newVer string) (newer bool) {
	// Version string is of format: x.x.xx e.g. 1.1.12
	// Assume 2.4.3 > 2.2.12 > 1.9.53
	curVerStart := strings.IndexByte(curVer, '.')
	newVerStart := strings.IndexByte(newVer, '.')
	curVerBase, _ := strconv.Atoi(curVer[0:curVerStart])
	newVerBase, _ := strconv.Atoi(curVer[0:newVerStart])
	if newVerBase > curVerBase {
		return true
	}

	curVerIdx := strings.LastIndexByte(curVer, '.')
	newVerIdx := strings.LastIndexByte(newVer, '.')
	curVerMid, _ := strconv.Atoi(curVer[curVerStart+1 : curVerIdx])
	newVerMid, _ := strconv.Atoi(newVer[newVerStart+1 : newVerIdx])
	if newVerMid > curVerMid {
		return true
	}

	curVerEnd, _ := strconv.Atoi(curVer[curVerIdx+1:])
	newVerEnd, _ := strconv.Atoi(newVer[newVerIdx+1:])
	if newVerEnd > curVerEnd {
		return true
	}

	return false
}
