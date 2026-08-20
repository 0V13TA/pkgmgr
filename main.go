package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type PackageList struct {
	Remote  string
	Pacman  map[string]bool
	AUR     map[string]bool
	Configs map[string]bool
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

func getGitRemote(dotfilesDir string) string {
	cmd := exec.Command("git", "-C", dotfilesDir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func loadPackages(path string) (*PackageList, error) {
	pkgList := &PackageList{
		Pacman:  make(map[string]bool),
		AUR:     make(map[string]bool),
		Configs: make(map[string]bool),
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

		if currentSection == "meta" {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == "remote" {
				pkgList.Remote = strings.TrimSpace(parts[1])
			}
			continue
		}

		pkg := strings.TrimSpace(strings.Split(line, "=")[0])
		switch currentSection {
		case "pacman":
			pkgList.Pacman[pkg] = true
		case "aur":
			pkgList.AUR[pkg] = true
		case "configs":
			pkgList.Configs[pkg] = true
		}
	}
	return pkgList, scanner.Err()
}

func savePackages(path string, pkgs *PackageList) error {
	var builder strings.Builder

	if pkgs.Remote != "" {
		builder.WriteString("[meta]\n")
		builder.WriteString(fmt.Sprintf("remote = %s\n\n", pkgs.Remote))
	}

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

	builder.WriteString("\n[configs]\n")
	var configsSorted []string
	for c := range pkgs.Configs {
		configsSorted = append(configsSorted, c)
	}
	sort.Strings(configsSorted)
	for _, c := range configsSorted {
		builder.WriteString(c + "\n")
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

func gitCommit(dotfilesDir, message string, paths ...string) {
	addArgs := append([]string{"-C", dotfilesDir, "add"}, paths...)
	_ = runCommand("git", addArgs...)
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

func updateMakefilePackages(dotfilesDir, pkgName string) error {
	makefilePath := filepath.Join(dotfilesDir, "Makefile")
	content, err := os.ReadFile(makefilePath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	re := regexp.MustCompile(`^PACKAGES\s*=\s*(.*)$`)
	updated := false

	for i, line := range lines {
		matches := re.FindStringSubmatch(line)
		if len(matches) > 1 {
			currentPkgs := strings.Fields(matches[1])
			exists := false
			for _, p := range currentPkgs {
				if p == pkgName {
					exists = true
					break
				}
			}
			if !exists {
				currentPkgs = append(currentPkgs, pkgName)
				lines[i] = fmt.Sprintf("PACKAGES = %s", strings.Join(currentPkgs, " "))
				updated = true
			}
			break
		}
	}

	if updated {
		return os.WriteFile(makefilePath, []byte(strings.Join(lines, "\n")), 0644)
	}
	return nil
}

func watchPath(targetPath string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding home directory: %v\n", err)
		os.Exit(1)
	}

	if strings.HasPrefix(targetPath, "~/") {
		targetPath = filepath.Join(home, targetPath[2:])
	}

	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid path: %v\n", err)
		os.Exit(1)
	}

	targetInfo, err := os.Lstat(absTarget)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Target does not exist: %v\n", err)
		os.Exit(1)
	}

	if targetInfo.Mode()&os.ModeSymlink != 0 {
		fmt.Println("Target is already a symlink. Skipping move.")
		return
	}

	relToHome, err := filepath.Rel(home, absTarget)
	if err != nil || strings.HasPrefix(relToHome, "..") {
		fmt.Fprintf(os.Stderr, "Error: Path must reside inside $HOME (%s)\n", home)
		os.Exit(1)
	}

	dotfilesDir := getDotfilesDir()
	iniPath := getIniPath()

	var pkgName string
	var destInsidePkg string

	parts := strings.Split(filepath.Clean(relToHome), string(filepath.Separator))
	if len(parts) >= 2 && parts[0] == ".config" {
		pkgName = parts[1]
		destInsidePkg = relToHome
	} else {
		pkgName = strings.TrimPrefix(parts[0], ".")
		destInsidePkg = relToHome
	}

	stowPkgDir := filepath.Join(dotfilesDir, pkgName)
	stowTargetDestination := filepath.Join(stowPkgDir, destInsidePkg)

	fmt.Printf("==> Moving %s to %s\n", absTarget, stowTargetDestination)
	if err := os.MkdirAll(filepath.Dir(stowTargetDestination), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating destination dir: %v\n", err)
		os.Exit(1)
	}

	if err := os.Rename(absTarget, stowTargetDestination); err != nil {
		fmt.Fprintf(os.Stderr, "Error moving target: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("==> Running stow for package '%s'...\n", pkgName)
	if err := runCommand("stow", "-v", "-R", "-d", dotfilesDir, "-t", home, pkgName); err != nil {
		fmt.Fprintf(os.Stderr, "Stow failed: %v\n", err)
		os.Exit(1)
	}

	pkgs, _ := loadPackages(iniPath)
	pkgs.Configs[pkgName] = true
	if remote := getGitRemote(dotfilesDir); remote != "" {
		pkgs.Remote = remote
	}
	_ = savePackages(iniPath, pkgs)

	_ = updateMakefilePackages(dotfilesDir, pkgName)

	commitMsg := fmt.Sprintf("Added %s config to dotfiles", pkgName)
	gitCommit(dotfilesDir, commitMsg, stowPkgDir, iniPath, filepath.Join(dotfilesDir, "Makefile"))
	fmt.Printf("==> Successfully tracked and committed %s!\n", pkgName)
}

func saveCurrentDir(message string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting current working directory: %v\n", err)
		os.Exit(1)
	}

	realPath, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		realPath = cwd
	}

	dotfilesDir := getDotfilesDir()
	rel, err := filepath.Rel(dotfilesDir, realPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		fmt.Fprintf(os.Stderr, "Error: Current directory (%s) is not tracked inside %s\n", cwd, dotfilesDir)
		os.Exit(1)
	}

	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	pkgName := parts[0]
	targetPath := filepath.Join(dotfilesDir, pkgName)

	fmt.Printf("==> Staging and committing changes for package '%s'...\n", pkgName)
	gitCommit(dotfilesDir, message, targetPath)
	fmt.Printf("==> Changes saved: %s\n", message)
}

func updateConfigs() {
	dotfilesDir := getDotfilesDir()
	iniPath := getIniPath()
	home, _ := os.UserHomeDir()

	fmt.Println("==> Pulling latest changes from git...")
	if err := runCommand("git", "-C", dotfilesDir, "pull"); err != nil {
		fmt.Printf("Git pull encountered a warning/error: %v\n", err)
	}

	pkgs, err := loadPackages(iniPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load %s: %v\n", iniPath, err)
		os.Exit(1)
	}

	var configs []string
	for c := range pkgs.Configs {
		configs = append(configs, c)
	}

	if len(configs) == 0 {
		entries, _ := os.ReadDir(dotfilesDir)
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && e.Name() != "pkgmgr" {
				configs = append(configs, e.Name())
			}
		}
	}

	if len(configs) > 0 {
		fmt.Printf("==> Restowing %d configuration packages: %s\n", len(configs), strings.Join(configs, ", "))
		args := append([]string{"-v", "-R", "-d", dotfilesDir, "-t", home}, configs...)
		if err := runCommand("stow", args...); err != nil {
			fmt.Fprintf(os.Stderr, "Stow error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("==> All dotfile symlinks up to date!")
	} else {
		fmt.Println("No configuration packages found to stow.")
	}
}

func bootstrapFromIni(source string) {
	tempFile := source

	// If source is a URL, fetch it to a temporary file
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		fmt.Printf("==> Downloading %s...\n", source)
		resp, err := http.Get(source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to fetch URL: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		tmp, err := os.CreateTemp("", "packages-*.ini")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create temp file: %v\n", err)
			os.Exit(1)
		}
		defer os.Remove(tmp.Name())
		defer tmp.Close()

		_, _ = io.Copy(tmp, resp.Body)
		tempFile = tmp.Name()
	}

	pkgs, err := loadPackages(tempFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load packages file: %v\n", err)
		os.Exit(1)
	}

	dotfilesDir := getDotfilesDir()

	// Clone remote repo if not present
	if _, err := os.Stat(dotfilesDir); os.IsNotExist(err) {
		if pkgs.Remote == "" {
			fmt.Fprintf(os.Stderr, "Error: No remote repository defined in [meta] section of %s\n", source)
			os.Exit(1)
		}
		fmt.Printf("==> Cloning dotfiles repo from %s to %s...\n", pkgs.Remote, dotfilesDir)
		if err := runCommand("git", "clone", pkgs.Remote, dotfilesDir); err != nil {
			fmt.Fprintf(os.Stderr, "Git clone failed: %v\n", err)
			os.Exit(1)
		}
	}

	// Update configs and sync packages
	updateConfigs()
	syncAll(filepath.Join(dotfilesDir, "packages.ini"))
	fmt.Println("==> System bootstrap complete!")
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  pkgmgr bootstrap <ini-path-or-url> # Setup fresh machine from packages.ini")
		fmt.Println("  pkgmgr sync                        # Install all packages defined in packages.ini")
		fmt.Println("  pkgmgr update config               # Git pull dotfiles & restow all registered configs")
		fmt.Println("  pkgmgr watch <path>                # Move target config to dotfiles, register, stow, & commit")
		fmt.Println("  pkgmgr save <message>              # Commit changes for the active dotfile package")
		fmt.Println("  pkgmgr -S <packages...>            # Install packages & track them")
		fmt.Println("  pkgmgr -R <packages...>            # Remove packages & untrack them")
		os.Exit(1)
	}

	dotfilesDir := getDotfilesDir()
	iniPath := getIniPath()
	action := os.Args[1]

	if action == "bootstrap" {
		if len(os.Args) < 3 {
			fmt.Println("Usage: pkgmgr bootstrap <path-or-url-to-packages.ini>")
			os.Exit(1)
		}
		bootstrapFromIni(os.Args[2])
		return
	}

	if action == "sync" {
		syncAll(iniPath)
		return
	}

	if action == "update" && len(os.Args) > 2 && os.Args[2] == "config" {
		updateConfigs()
		return
	}

	if action == "watch" {
		if len(os.Args) < 3 {
			fmt.Println("Usage: pkgmgr watch <path-to-folder-or-file>")
			os.Exit(1)
		}
		watchPath(os.Args[2])
		return
	}

	if action == "save" {
		if len(os.Args) < 3 {
			fmt.Println("Usage: pkgmgr save \"commit message\"")
			os.Exit(1)
		}
		saveCurrentDir(os.Args[2])
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
			if remote := getGitRemote(dotfilesDir); remote != "" {
				pkgs.Remote = remote
			}
			_ = savePackages(iniPath, pkgs)
			allAdded := append(pacmanAdded, aurAdded...)
			commitMsg := fmt.Sprintf("Installed %s", strings.Join(allAdded, ", "))
			gitCommit(dotfilesDir, commitMsg, iniPath)
		}

	} else if strings.HasPrefix(action, "-R") {
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
			if remote := getGitRemote(dotfilesDir); remote != "" {
				pkgs.Remote = remote
			}
			_ = savePackages(iniPath, pkgs)
			commitMsg := fmt.Sprintf("Uninstalled %s", strings.Join(removed, ", "))
			gitCommit(dotfilesDir, commitMsg, iniPath)
		}
	} else {
		_ = runCommand("yay", os.Args[1:]...)
	}
}
