# Factorio Mod Updater

Another tool to update and install servers and mods for factorio. Doesn't manage deleting mods.

## Downloading
You can either download the latest release for a binary, or build from source. 

### Building from source
``` shell
git clone https://github.com/Baxxyy/factorio-updater.git
cd factorio-updater
go build
```

## Usage

### First time setup
The first time you run the binary it will run through setup. This involves:
- Setting user details to access the mod and download portal
- Selecting a directory where factorio is to be installed, or where it is currently installed (Needs full path!)

e.g.
``` shell
Running first time setup:
Enter your factorio username/email: baxxy
Do you have your factorio token? [Y/n] n
Please enter your factorio password:
Got factorio token
Please enter the directory you have factorio installed (or want it installed in): /home/baxxy/factorio-testing/
Setting factorio version as:  1.1.77
Setup complete! Further options can be configured using the cli
```

### General Usage

The current functionality is implememnted:

- Install and update a headless factorio server, tracking experimental or stable branch.
- Install and update mods.
- Pin a specific mod version to avoid it auto updating.

