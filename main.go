package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type PackageList struct {
	Pacman map[string]bool
	AUR    map[string]bool
}

func getDotfilesDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting home dir: %v\n", err)
		os.Exit(1)
	}
	return filepath.Join(home, "dotfiles")
}

func getIniPath() string {
	return filepath.Join(getDotfilesDir(), "packages.ini")
}

func loadPackages(path string) (*PackageList, error) {
	pkgList := &PackageList{
		Pacman: make(map[string]bool),
		AUR:    make(map[string]bool),
	}

	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return pkgList, nil
	} else if err != nil {
		return nil, err
	}
	defer file.Close()

	var currentSection string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.ToLower(line[1 : len(line)-1])
			continue
		}

		pkg := strings.TrimSpace(strings.Split(line, "=")[0])
		if currentSection == "pacman" {
			pkgList.Pacman[pkg] = true
		} else if currentSection == "aur" {
			pkgList.AUR[pkg] = true
		}
	}
	return pkgList, scanner.Err()
}

func savePackages(path string, pkgs *PackageList) error {
	var builder strings.Builder

	builder.WriteString("[pacman]\n")
	var pacmanSorted []string
	for p := range pkgs.Pacman {
		pacmanSorted = append(pacmanSorted, p)
	}
	sort.Strings(pacmanSorted)
	for _, p := range pacmanSorted {
		builder.WriteString(p + "\n")
	}

	builder.WriteString("\n[aur]\n")
	var aurSorted []string
	for p := range pkgs.AUR {
		aurSorted = append(aurSorted, p)
	}
	sort.Strings(aurSorted)
	for _, p := range aurSorted {
		builder.WriteString(p + "\n")
	}

	return os.WriteFile(path, []byte(builder.String()), 0644)
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func gitCommit(dotfilesDir, iniPath, message string) {
	_ = runCommand("git", "-C", dotfilesDir, "add", iniPath)
	if err := runCommand("git", "-C", dotfilesDir, "commit", "-m", message); err != nil {
		fmt.Printf("Git commit skipped or failed: %v\n", err)
	}
}

func isOfficialPackage(pkg string) bool {
	cmd := exec.Command("pacman", "-Si", pkg)
	return cmd.Run() == nil
}

func syncAll(iniPath string) {
	pkgs, err := loadPackages(iniPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse %s: %v\n", iniPath, err)
		os.Exit(1)
	}

	var pacmanList []string
	for p := range pkgs.Pacman {
		pacmanList = append(pacmanList, p)
	}
	var aurList []string
	for p := range pkgs.AUR {
		aurList = append(aurList, p)
	}

	if len(pacmanList) > 0 {
		fmt.Println("==> Syncing Pacman packages...")
		args := append([]string{"-S", "--needed"}, pacmanList...)
		_ = runCommand("sudo", append([]string{"pacman"}, args...)...)
	}

	if len(aurList) > 0 {
		fmt.Println("==> Syncing AUR packages...")
		args := append([]string{"-S", "--needed"}, aurList...)
		_ = runCommand("yay", args...)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  pkgmgr sync                  # Install all packages defined in packages.ini")
		fmt.Println("  pkgmgr -S <packages...>      # Install packages & track them")
		fmt.Println("  pkgmgr -R <packages...>      # Remove packages & untrack them")
		os.Exit(1)
	}

	dotfilesDir := getDotfilesDir()
	iniPath := getIniPath()

	action := os.Args[1]

	if action == "sync" {
		syncAll(iniPath)
		return
	}

	pkgs, err := loadPackages(iniPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading packages: %v\n", err)
		os.Exit(1)
	}

	var targetPkgs []string
	for _, arg := range os.Args[2:] {
		if !strings.HasPrefix(arg, "-") {
			targetPkgs = append(targetPkgs, arg)
		}
	}

	if strings.HasPrefix(action, "-S") {
		// Pass-through execution directly to yay
		if err := runCommand("yay", os.Args[1:]...); err != nil {
			os.Exit(1)
		}

		var pacmanAdded, aurAdded []string
		for _, p := range targetPkgs {
			if isOfficialPackage(p) {
				pkgs.Pacman[p] = true
				pacmanAdded = append(pacmanAdded, p)
			} else {
				pkgs.AUR[p] = true
				aurAdded = append(aurAdded, p)
			}
		}

		if len(pacmanAdded) > 0 || len(aurAdded) > 0 {
			_ = savePackages(iniPath, pkgs)
			allAdded := append(pacmanAdded, aurAdded...)
			commitMsg := fmt.Sprintf("Installed %s", strings.Join(allAdded, ", "))
			gitCommit(dotfilesDir, iniPath, commitMsg)
		}

	} else if strings.HasPrefix(action, "-R") {
		// Pass-through execution directly to yay
		if err := runCommand("yay", os.Args[1:]...); err != nil {
			os.Exit(1)
		}

		var removed []string
		for _, p := range targetPkgs {
			if pkgs.Pacman[p] {
				delete(pkgs.Pacman, p)
				removed = append(removed, p)
			}
			if pkgs.AUR[p] {
				delete(pkgs.AUR, p)
				removed = append(removed, p)
			}
		}

		if len(removed) > 0 {
			_ = savePackages(iniPath, pkgs)
			commitMsg := fmt.Sprintf("Uninstalled %s", strings.Join(removed, ", "))
			gitCommit(dotfilesDir, iniPath, commitMsg)
		}
	} else {
		// Fallback for standard yay/pacman queries like -Q, -Ss, etc.
		_ = runCommand("yay", os.Args[1:]...)
	}
}
