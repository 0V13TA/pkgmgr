package main

import (
	"fmt"
	"os"

	"github.com/0V13TA/pkgmgr/internal/dotfiles"
	"github.com/0V13TA/pkgmgr/internal/syscmd"
)

func printHelp() {
	helpText := `pkgmgr - Declarative Package and Dotfiles Manager

Usage:
  pkgmgr <command> [arguments...]

Dotfiles Management:
  watch <path>           Convert an existing config to a Stow package, track it, and commit.
  unwatch <package>      Unstow a package, restore files to $HOME, and remove from Git.
  save "<message>"       Commit changes for the dotfile package in the current directory.
  update config          Pull the latest dotfiles from git and restow all registered configs.
  bootstrap <ini-url>    Setup a fresh machine by cloning dotfiles and installing everything.

Package Management (Pacman/AUR):
  sync                   Install all packages currently defined in packages.ini.
  -S <packages...>       Install packages via yay, add them to packages.ini, and commit.
  -R <packages...>       Remove packages via yay, remove them from packages.ini, and commit.

Other commands:
  help, -h, --help       Show this help message.

Any unrecognized flags (e.g., -Syu, -Q) will be passed directly to yay without modifying your dotfiles.
`
	fmt.Print(helpText)
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	runner := syscmd.OSExec{}
	mgr := dotfiles.NewManager(runner)
	action := os.Args[1]

	switch action {
	case "help", "-h", "--help":
		printHelp()
		return
	case "watch":
		if len(os.Args) > 2 {
			mgr.WatchPath(os.Args[2])
		} else {
			fmt.Println("Usage: pkgmgr watch <path>")
		}
	case "unwatch":
		if len(os.Args) > 2 {
			mgr.UnwatchPackage(os.Args[2])
		} else {
			fmt.Println("Usage: pkgmgr unwatch <package-name> (e.g., pkgmgr unwatch nvim)")
		}
	case "save":
		if len(os.Args) > 2 {
			mgr.SaveCurrentDir(os.Args[2])
		} else {
			fmt.Println("Usage: pkgmgr save \"commit message\"")
		}
	case "update":
		if len(os.Args) > 2 && os.Args[2] == "config" {
			mgr.UpdateConfigs()
		} else {
			fmt.Println("Usage: pkgmgr update config")
		}
	case "bootstrap":
		if len(os.Args) > 2 {
			mgr.BootstrapFromIni(os.Args[2])
		} else {
			fmt.Println("Usage: pkgmgr bootstrap <ini-path-or-url>")
		}
	case "sync":
		mgr.SyncAll()
	case "-S":
		mgr.InstallPackages(os.Args[1:])
	case "-R":
		mgr.RemovePackages(os.Args[1:])
	default:
		runner.Run("yay", os.Args[1:]...)
	}
}
