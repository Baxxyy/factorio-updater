package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cavaliergopher/grab/v3"
)

// Global configs
var (
	fileConfig    = make(map[string]string)
	runtimeConfig = make(map[string]any)
	modVersions   sync.Map
)

const baseUrl = "https://%sfactorio.com/%s"

func buildCurrentModVersions() {
	modsFilePath := fileConfig["installDir"] + "/mods"
	filesInModDir, err := os.ReadDir(modsFilePath)
	if err != nil {
		println("Couldn't get current mod versions, continuing with empty mods list")
		verbosePrint(1, "Got err while reading mod directory: %s\n", err)
		return
	}

	for _, mod := range filesInModDir {
		modName := mod.Name()
		if !strings.HasSuffix(mod.Name(), ".zip") {
			continue
		}

		versionNumStart := strings.LastIndex(modName, "_")
		versionNumEnd := strings.LastIndex(modName, ".")
		versionNum := modName[versionNumStart+1 : versionNumEnd]
		modString := modName[:versionNumStart]
		if oldVer, present := modVersions.LoadOrStore(modString, versionNum); present {
			replace := checkVersionString(oldVer.(string), versionNum)
			if replace {
				modVersions.Store(modString, versionNum)
				os.Remove(modsFilePath + "/" + modString + "_" + oldVer.(string) + ".zip")
			}
		}
	}
}

// Get token needed for downloading mods
func getAuthToken(username string, password string) (token string, err error) {
	Client := runtimeConfig["Client"].(http.Client)
	authUrl := fmt.Sprintf(baseUrl, "auth.", "api-login?&username=%s&password=%s&api_version=4")
	authUrl = fmt.Sprintf(authUrl, username, password)
	authRequest, err := http.NewRequest(http.MethodPost, authUrl, nil)
	checkErr(err, "Got error building auth request\n")
	res, err := Client.Do(authRequest)
	// Order of these matters!!!!
	if err != nil || res.StatusCode != 200 {
		fmt.Printf("Error authenticating, please try again\n")
		verbosePrint(2, "Got error: %s\n", err)
		verbosePrint(2, "Auth request returned: %s\n", res.Body)
		return "", err
	}

	tokenResponse, err := bufio.NewReader(res.Body).ReadBytes('}')
	if err != nil {
		fmt.Printf("Failed to parse response from server.\n")
		verbosePrint(1, "Got error: %s\n", err)
		return "", err
	}

	if bytes.HasSuffix(tokenResponse, []byte(`{}`)) {
		// This checks against invalid password/username. Yes it is SHIT
		fmt.Printf("username/password were invalid, please try again\n")
		return "", errors.New("Details were invalid")
	}
	idx1 := bytes.Index(tokenResponse, []byte(`:`)) + 3
	idx2 := bytes.Index(tokenResponse, []byte(`,`)) - 1
	token = string(tokenResponse[idx1:idx2])
	return token, nil
}

// Capable of updating or installing a server at $INSTALLDIR - set in config
func getServer() (err error) {
	Client := runtimeConfig["Client"].(http.Client)
	latestUrl := fmt.Sprintf(baseUrl, "", "api/latest-releases")
	latestRes, err := Client.Get(latestUrl)
	checkErr(err, "Couldn't get latest version data")
	type Response struct {
		Experimental struct {
			Alpha    string `json:"alpha"`
			Demo     string `json:"demo"`
			Headless string `json:"headless"`
		} `json:"experimental"`
		Stable struct {
			Alpha    string `json:"alpha"`
			Demo     string `json:"demo"`
			Headless string `json:"headless"`
		} `json:"stable"`
	}

	data := Response{}
	err = json.NewDecoder(latestRes.Body).Decode(&data)
	if err != nil {
		return err
	}
	if runtimeConfig["verbose"].(int) > 1 {
		if fileConfig["wantedVersion"] == "stable" {
			fmt.Printf("Got latest stable headless version as: %s\n", data.Stable.Headless)
		} else {
			fmt.Printf("Got latest experimental headless version as: %s\n", data.Experimental.Headless)
		}
	}
	var latestVer string
	if fileConfig["wantedVersion"] == "stable" {
		latestVer = data.Stable.Headless
	} else {
		latestVer = data.Experimental.Headless
	}

	if !checkVersionString(fileConfig["currentVersion"], latestVer) {
		fmt.Println("Current version is up to date")
		return nil
	}

	fmt.Printf("New version available: (%s)\nUpdate? [Y/n] ", latestVer)
	var input string
	fmt.Scanln(&input)
	if input == "" || (input[0] != 'Y' && input[0] != 'y') {
		return nil
	}

	// Updating server flow is:
	//  - Download from https://factorio.com/get-download/{stable/latest}/headless/linux64 into base-dir
	//  - tar -xf linux64
	//  - cp -rf ./factorio/bin .
	//  - cp -rf ./factorio/data .
	//  - cp -n ./factorio/config-path.cfg .
	//  - rm -r linux64 factorio
	var downloadUrl string
	if fileConfig["wantedVersion"] == "stable" {
		downloadUrl = "https://factorio.com/get-download/stable/headless/linux64"
	} else {
		downloadUrl = "https://factorio.com/get-download/latest/headless/linux64"
	}
	c := grab.NewClient()
	req, _ := grab.NewRequest(fileConfig["installDir"], downloadUrl)
	fmt.Println("Downloading newest version to " + fileConfig["installDir"] + "...")
	got := c.Do(req)
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()

	const clearLine = "\033[2K\r"
Download:
	for {
		select {
		case <-t.C:
			fmt.Printf(clearLine+"downloading [%s%s] %.1f%%",
				strings.Repeat("=", int(got.Progress()*50)),
				strings.Repeat("-", 50-int(got.Progress()*50)),
				got.Progress()*100)

		case <-got.Done:
			fmt.Printf(clearLine)
			break Download
		}
	}
	if err := got.Err(); err != nil {
		fmt.Printf("Download Failed %v\n", err)
		return err
	}
	fmt.Println("Downloaded newest version to " + got.Filename)
	tar := exec.Command("tar", "-xf", got.Filename, "-C", fileConfig["installDir"])
	fmt.Println("Extracting new version...")
	err = tar.Run()
	checkErr(err, "Failed to extract "+got.Filename)

	fmt.Println("Copying new files into current installation...")
	copy1 := exec.Command("cp", "-rf", fileConfig["installDir"]+"/factorio/bin", fileConfig["installDir"])
	copy2 := exec.Command("cp", "-rf", fileConfig["installDir"]+"/factorio/data", fileConfig["installDir"])
	copy3 := exec.Command("cp", "-n", fileConfig["installDir"]+"/factorio/config-path.cfg", fileConfig["installDir"])
	verbosePrint(10, "Running: %s", copy1.Args)
	err = copy1.Run()
	checkErr(err, "Failed to copy ./factorio/bin")
	verbosePrint(10, "Running: %s", copy2.Args)
	err = copy2.Run()
	checkErr(err, "Failed to copy ./factorio/data")
	verbosePrint(10, "Running: %s", copy3.Args)
	err = copy3.Run()
	checkErr(err, "Failed to copy ./factorio/config-path.cfg")

	fmt.Println("Cleaning up...")
	remove := exec.Command("rm", "-r", fileConfig["installDir"]+"/factorio", got.Filename)
	remove.Run()
	checkErr(err, "Failed to cleanup after updating server version")
	fmt.Println("Done!")

	editFileConfig("currentVersion", latestVer)
	return nil
}

type modDataResponse struct {
	Pagination struct {
		Count     int `json:"count"`
		Page      int `json:"page"`
		PageCount int `json:"page_count"`
		PageSize  int `json:"page_size"`
		Links     struct {
			First any `json:"first"`
			Next  any `json:"next"`
			Prev  any `json:"prev"`
			Last  any `json:"last"`
		} `json:"links"`
	} `json:"pagination"`
	Results []struct {
		Name           string  `json:"name"`
		Title          string  `json:"title"`
		Owner          string  `json:"owner"`
		Summary        string  `json:"summary"`
		DownloadsCount int     `json:"downloads_count"`
		Category       string  `json:"category"`
		Score          float64 `json:"score"`
		Releases       []struct {
			DownloadURL string `json:"download_url"`
			FileName    string `json:"file_name"`
			InfoJSON    struct {
				FactorioVersion string `json:"factorio_version"`
			} `json:"info_json"`
			ReleasedAt time.Time `json:"released_at"`
			Version    string    `json:"version"`
			Sha1       string    `json:"sha1"`
		} `json:"releases"`
	} `json:"results"`
}

func updateGivenMods(reqUrl string, updatedMods chan int, debugUrls chan string,
					 debugErrs chan string, wg *sync.WaitGroup) {
	defer wg.Done()
	Client := runtimeConfig["Client"].(http.Client)
	debugUrls <- reqUrl
	res, err := Client.Get(reqUrl)
	if err != nil || res.StatusCode != 200 {
		debugErrs <- fmt.Sprintf("Got err %s while fetching mod data, site returned code: %d", err, res.StatusCode)
		return
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		debugErrs <- fmt.Sprintf("Failed to read server response\n Got err: %s", err)
		return
	}

	var modData modDataResponse
	err = json.Unmarshal(bodyBytes, &modData)
	if err != nil {
		debugErrs <- fmt.Sprintf("Failed to decode json response from server\n Got err: %s\n with data: %s", err, bodyBytes)
		return
	}
	verbosePrint(4, "Got mod data from server for %d mods\n", len(modData.Results))
	modsDir := fileConfig["installDir"] + "/mods"
	modApiBaseUrl := fmt.Sprintf(baseUrl, "mods.", "")
	c := grab.NewClient()

	var requests []*grab.Request
	for _, modItem := range modData.Results {
		currentVersion, _ := modVersions.Load(modItem.Name)
		newVersion := modItem.Releases[len(modItem.Releases)-1]
		if checkVersionString(currentVersion.(string), newVersion.Version) {
			authenticatedUrl := fmt.Sprintf(modApiBaseUrl+newVersion.DownloadURL+"?username=%s&token=%s",
				fileConfig["username"], fileConfig["token"])
			req, _ := grab.NewRequest(modsDir, authenticatedUrl)
			requests = append(requests, req)
			modVersions.Store(modItem.Name, newVersion.Version)
			verbosePrint(1, "Updating %s from ver %s to %s\n", modItem.Name, currentVersion.(string), newVersion.Version)
		}
	}
	responses := c.DoBatch(5, requests...)

	for err := range responses {
		if err.Err() != nil {
			fmt.Printf("Err: %s\n", err.Err())
		}
	}
	updatedMods <- len(responses)

	return
}

type modEntry struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type modEntries struct {
	Mods []modEntry `json:"mods"`
}

func checkAndUpdateMods() {
	modsFilepath := fileConfig["installDir"] + "/mods"
	modsDir, err := os.Stat(modsFilepath)
	if err != nil {
		verbosePrint(1, "%s\n", err)
		println("Can't find " + fileConfig["installDir"] + "/mods, attempting to create it and exit")
		if err = os.Mkdir(modsFilepath, os.ModePerm); err != nil {
			println("Couldn't make new mods directory, check permissions or if " + fileConfig["installDir"] + " exists")
			verbosePrint(1, "%s\n", err)
		}
		return
	}

	if !modsDir.IsDir() {
		println(modsFilepath + " is not a directory")
		return
	}

	modData := getMods()

	var enabledMods []string
	for _, modItem := range modData.Mods {
		verbosePrint(10, "Checking mod %s, enabled is %t\n", modItem.Name, modItem.Enabled)

		if !modItem.Enabled {
			continue
		}
		if modItem.Name == "base" {
			continue
		}
		verbosePrint(10, "Adding mod %s to enabled mods\n", modItem.Name)
		enabledMods = append(enabledMods, modItem.Name)
	}
	if len(enabledMods) == 0 {
		verbosePrint(1, "Couldn't find any mods\n")
		return
	}
	modApiBaseUrl := fmt.Sprintf(baseUrl, "mods.", "api/mods?namelist=")
	modApiReqUrl := modApiBaseUrl
	const maxReqInThread = 20
	// 'Funny' go math casting - why must it always be float64? are we lua?
	requests := int(math.Ceil(float64(len(enabledMods)) / maxReqInThread))
	verbosePrint(4, "Will send %d requests\n", requests)
	buildCurrentModVersions()
	urls := make(chan string, requests)
	errors := make(chan string, requests)
	updates := make(chan int, requests)
	var DoneUpdating sync.WaitGroup
	for i, modName := range enabledMods {
		modApiReqUrl += (modName + ",")
		if math.Mod(float64((i+1)), maxReqInThread) == 0 {
			DoneUpdating.Add(1)
			verbosePrint(6, "Added 1 to wait group\n")
			go updateGivenMods(modApiReqUrl, updates, urls, errors, &DoneUpdating)
			verbosePrint(6, "Started an update requester\n")
			modApiReqUrl = modApiBaseUrl
		}
	}
	// cleanup remaing mods that didn't fit nicely - probably a neater way
	if math.Mod(float64(len(enabledMods)), maxReqInThread) != 0 {
		DoneUpdating.Add(1)
		go updateGivenMods(modApiReqUrl, updates, urls, errors, &DoneUpdating)
		verbosePrint(6, "Started cleanup update requester\n")
	}

	DoneUpdating.Wait()
	close(urls)
	close(errors)
	close(updates)
	for reqUrl := range urls {
		verbosePrint(6, "Got requrl: %s\n", reqUrl)
	}

	for error := range errors {
		println("Got error: " + error + "\n")
	}

	totalUpdates := 0
	for update := range updates {
		totalUpdates += update
	}

	fmt.Printf("Updated %d mods\n", totalUpdates)
	return
}

type modDetails struct {
	Category       string `json:"category"`
	DownloadsCount int    `json:"downloads_count"`
	Name           string `json:"name"`
	Owner          string `json:"owner"`
	Releases       []struct {
		DownloadURL string `json:"download_url"`
		FileName    string `json:"file_name"`
		InfoJSON    struct {
			FactorioVersion string `json:"factorio_version"`
		} `json:"info_json"`
		ReleasedAt time.Time `json:"released_at"`
		Sha1       string    `json:"sha1"`
		Version    string    `json:"version"`
	} `json:"releases"`
	Score     float64 `json:"score"`
	Summary   string  `json:"summary"`
	Thumbnail string  `json:"thumbnail"`
	Title     string  `json:"title"`
}

func installNewMod(modName string, version string) {
	Client := runtimeConfig["Client"].(http.Client)
	modUrl := fmt.Sprintf(baseUrl, "mods.", "api/mods/" + modName)
	res, err := Client.Get(modUrl)
	if (err != nil) {
		fmt.Println("Couldn't connect to factorio servers")
		return
	} else if (res.StatusCode != 200) {
		fmt.Println("Couldn't find mod, please check the mod name")
		return
	}

	bodyBytes, err := io.ReadAll(res.Body)
	checkErr(err, "Couldn't read response from server, please retry")

	var modInfo modDetails
	json.Unmarshal(bodyBytes, &modInfo)
	checkErr(err, "Couldn't parse response from server")

	modApiBaseUrl := fmt.Sprintf(baseUrl, "mods.", "")
	modsDir := fileConfig["installDir"] + "/mods"

	if (version == "") {
		selectMod := modInfo.Releases[len(modInfo.Releases)-1]
		downloadUrl := fmt.Sprintf(modApiBaseUrl + selectMod.DownloadURL + "?username=%s&token=%s",
								   fileConfig["username"], fileConfig["token"])
		res, err := grab.Get(modsDir, downloadUrl)
		checkErr(err, "Couldn't fetch mod, please retry")
		fmt.Printf("Downloaded %s to %s\n", modName, res.Filename)
		addToModsJson(modName, true)
		return
	} else {
		for _, release := range modInfo.Releases {
			if (release.Version == version) {
				downloadUrl := fmt.Sprintf(modApiBaseUrl + release.DownloadURL + "?username=%s&token=%s",
										   fileConfig["username"], fileConfig["token"])
				res, err := grab.Get(modsDir, downloadUrl)
				checkErr(err, "Couldn't fetch mod, please retry")
				fmt.Printf("Downloaded %s to %s\n", modName, res.Filename)
				addToModsJson(modName, true)
				return
			}
		}
	}
	fmt.Println("Couldn't find version number specified, please check it is valid")
}

func addToModsJson(modName string, enabled bool) {
	modsFile := fileConfig["installDir"] + "/mods/mod-list.json"
	modData := getMods()

	for _, modItem := range modData.Mods {
		if modItem.Name == modName {
			return
		}
	}

	newEntry := new(modEntry)
	newEntry.Name = modName
	newEntry.Enabled = enabled
	newMods := append(modData.Mods, *newEntry)
	sort.Slice(newMods, func(i, j int) bool {
		return newMods[i].Name < newMods[j].Name
	})
	modData.Mods = newMods
	newModData, err := json.MarshalIndent(modData,"", "  ")
	checkErr(err, "Couldn't convert new mod data to json, mod-list.json not updated!")
	replaceExistingFile(newModData, modsFile)
}

func disableMod(modName string) {
	modsFile := fileConfig["installDir"] + "/mods/mod-list.json"
	modInfo, err := os.ReadFile(modsFile)
	checkErr(err, "Couldn't read mod-list.json")
	var modData modEntries
	json.Unmarshal(modInfo, &modData)
disableModLoop:
	for i, modItem := range modData.Mods {
		if modItem.Enabled && modItem.Name == modName  {
			modItem.Enabled = false
			modData.Mods[i] = modItem
			break disableModLoop
		}
	}
	newModData, err := json.MarshalIndent(modData,"", "  ")
	checkErr(err, "Couldn't convert new mod data to json, mod-list.json not updated!")
	replaceExistingFile(newModData, modsFile)
}

func enableMod(modName string) {
	modsFile := fileConfig["installDir"] + "/mods/mod-list.json"
	modInfo, err := os.ReadFile(modsFile)
	checkErr(err, "Couldn't read mod-list.json")
	var modData modEntries
	json.Unmarshal(modInfo, &modData)
enableModLoop:
	for i, modItem := range modData.Mods {
		if !modItem.Enabled && modItem.Name == modName {
			modItem.Enabled = true
			modData.Mods[i] = modItem
			break enableModLoop
		}
	}
	newModData, err := json.MarshalIndent(modData,"", "  ")
	checkErr(err, "Couldn't convert new mod data to json, mod-list.json not updated!")
	replaceExistingFile(newModData, modsFile)
}

func main() {
ArgLoop:
	for argNum, arg := range os.Args {
		if arg == "update" {
			if len(os.Args) < argNum + 1 {
				break ArgLoop
			}
			nextArg := os.Args[argNum + 1]
			if nextArg == "server" {
				if err := getServer(); err != nil {
					fmt.Print("Got an error with updating game version\n")
				}
				return
			} else if nextArg == "mods" {
				checkAndUpdateMods()
				return
			}
			return
		}
		if arg == "config" {
			if len(os.Args) < argNum + 1 {
				break ArgLoop
			}
			nextArg := os.Args[argNum +1 ]
			if nextArg == "list" {
				for key, value := range fileConfig {
					fmt.Printf("%s is: %s\n", key, value)
				}
				return
			} else if nextArg == "edit" {
				if len(os.Args) < argNum + 3 {
					break ArgLoop
				}
				keyChange := os.Args[argNum + 2]
				newValue := os.Args[argNum + 3]
				editFileConfig(keyChange, newValue)
				return
			}
		}
		if arg == "install" {
			if len(os.Args) < argNum + 1 {
				break ArgLoop
			}
			modName := os.Args[argNum + 1]
			if len(os.Args) > argNum + 2 {
				modVer := os.Args[argNum + 2]
				installNewMod(modName, modVer)
			} else {
				installNewMod(modName, "")
			}
			return
		}
		if arg == "-h" || arg == "--help" {
			fmt.Printf("Factorio Update Tool\n")
			fmt.Printf("Usage: %s <mode> <target>\n\n", os.Args[0])
			fmt.Printf("Options:\n")
			fmt.Printf("\t update server\t\t  - Update factorio server version\n")
			fmt.Printf("\t update mods\t\t  - Update factorio mods versions\n\n")
			fmt.Printf("\t install <name> <ver>\t  - Download a mod, takes optional version\n\n")
			fmt.Printf("\t config list\t\t  - List current config settings\n")
			fmt.Printf("\t config set <key> <value> - Set a config variable\n")
			return
		}
	}
	fmt.Println("Couldn't recognise arguments")
	fmt.Printf("\tUsage: %s <mode> <target>\n", os.Args[0])
}
