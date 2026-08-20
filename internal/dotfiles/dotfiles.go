package dotfiles

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/0V13TA/pkgmgr/internal/manifest"
	"github.com/0V13TA/pkgmgr/internal/syscmd"
)

type Manager struct {
	Runner      syscmd.Runner
	DotfilesDir string
	IniPath     string
}

func NewManager(runner syscmd.Runner) *Manager {
	home, _ := os.UserHomeDir()
	dotDir := filepath.Join(home, "dotfiles")
	return &Manager{
		Runner:      runner,
		DotfilesDir: dotDir,
		IniPath:     filepath.Join(dotDir, "packages.ini"),
	}
}

func (m *Manager) gitCommit(message string, paths ...string) error {
	addArgs := append([]string{"-C", m.DotfilesDir, "add"}, paths...)
	if err := m.Runner.Run("git", addArgs...); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}
	if err := m.Runner.Run("git", "-C", m.DotfilesDir, "commit", "-m", message); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}
	return nil
}

func (m *Manager) getGitRemote() string {
	out, err := m.Runner.Output("git", "-C", m.DotfilesDir, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return out
}

func (m *Manager) isOfficialPackage(pkg string) bool {
	_, err := m.Runner.Output("pacman", "-Si", pkg)
	return err == nil
}

func (m *Manager) updateMakefile(pkgName string) error {
	makefilePath := filepath.Join(m.DotfilesDir, "Makefile")
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
			for _, p := range currentPkgs {
				if p == pkgName {
					return nil // Already exists
				}
			}
			currentPkgs = append(currentPkgs, pkgName)
			lines[i] = fmt.Sprintf("PACKAGES = %s", strings.Join(currentPkgs, " "))
			updated = true
			break
		}
	}

	if updated {
		return os.WriteFile(makefilePath, []byte(strings.Join(lines, "\n")), 0644)
	}
	return nil
}

func (m *Manager) WatchPath(targetPath string) {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(targetPath, "~/") {
		targetPath = filepath.Join(home, targetPath[2:])
	}

	absTarget, _ := filepath.Abs(targetPath)
	targetInfo, err := os.Lstat(absTarget)
	if err != nil || targetInfo.Mode()&os.ModeSymlink != 0 {
		fmt.Println("Target invalid or already a symlink.")
		return
	}

	relToHome, _ := filepath.Rel(home, absTarget)
	parts := strings.Split(filepath.Clean(relToHome), string(filepath.Separator))
	var pkgName, destInsidePkg string
	if len(parts) >= 2 && parts[0] == ".config" {
		pkgName, destInsidePkg = parts[1], relToHome
	} else {
		pkgName, destInsidePkg = strings.TrimPrefix(parts[0], "."), relToHome
	}

	stowPkgDir := filepath.Join(m.DotfilesDir, pkgName)
	stowTargetDestination := filepath.Join(stowPkgDir, destInsidePkg)

	os.MkdirAll(filepath.Dir(stowTargetDestination), 0755)
	os.Rename(absTarget, stowTargetDestination)

	if err := m.Runner.Run("stow", "-v", "-R", "-d", m.DotfilesDir, "-t", home, pkgName); err != nil {
		fmt.Fprintf(os.Stderr, "Stow failed! Rolling back %s...\n", stowTargetDestination)
		os.Rename(stowTargetDestination, absTarget)
		os.Exit(1)
	}

	pkgs, _ := manifest.Load(m.IniPath)
	pkgs.Configs[pkgName] = true
	if remote := m.getGitRemote(); remote != "" {
		pkgs.Remote = remote
	}
	manifest.Save(m.IniPath, pkgs)
	m.updateMakefile(pkgName)

	commitMsg := fmt.Sprintf("Added %s config to dotfiles", pkgName)
	if err := m.gitCommit(commitMsg, stowPkgDir, m.IniPath, filepath.Join(m.DotfilesDir, "Makefile")); err == nil {
		fmt.Printf("==> Successfully tracked %s!\n", pkgName)
	}
}

func (m *Manager) InstallPackages(args []string) {
	m.Runner.Run("yay", args...)
	pkgs, _ := manifest.Load(m.IniPath)
	
	added := false
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			if m.isOfficialPackage(arg) {
				pkgs.Pacman[arg] = true
			} else {
				pkgs.AUR[arg] = true
			}
			added = true
		}
	}

	if added {
		if remote := m.getGitRemote(); remote != "" {
			pkgs.Remote = remote
		}
		manifest.Save(m.IniPath, pkgs)
		m.gitCommit("Installed packages", m.IniPath)
	}
}

func (m *Manager) RemovePackages(args []string) {
	m.Runner.Run("yay", args...)
	pkgs, _ := manifest.Load(m.IniPath)

	removed := false
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			if pkgs.Pacman[arg] {
				delete(pkgs.Pacman, arg)
				removed = true
			}
			if pkgs.AUR[arg] {
				delete(pkgs.AUR, arg)
				removed = true
			}
		}
	}

	if removed {
		if remote := m.getGitRemote(); remote != "" {
			pkgs.Remote = remote
		}
		manifest.Save(m.IniPath, pkgs)
		m.gitCommit("Uninstalled packages", m.IniPath)
	}
}

func (m *Manager) SyncAll() {
	pkgs, err := manifest.Load(m.IniPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse %s: %v\n", m.IniPath, err)
		os.Exit(1)
	}

	var pacmanList, aurList []string
	for p := range pkgs.Pacman {
		pacmanList = append(pacmanList, p)
	}
	for p := range pkgs.AUR {
		aurList = append(aurList, p)
	}

	if len(pacmanList) > 0 {
		fmt.Println("==> Syncing Pacman packages...")
		args := append([]string{"-S", "--needed"}, pacmanList...)
		m.Runner.Run("sudo", append([]string{"pacman"}, args...)...)
	}

	if len(aurList) > 0 {
		fmt.Println("==> Syncing AUR packages...")
		args := append([]string{"-S", "--needed"}, aurList...)
		m.Runner.Run("yay", args...)
	}
}

func (m *Manager) SaveCurrentDir(message string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting current working directory: %v\n", err)
		os.Exit(1)
	}

	realPath, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		realPath = cwd
	}

	rel, err := filepath.Rel(m.DotfilesDir, realPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		fmt.Fprintf(os.Stderr, "Error: Current directory (%s) is not tracked inside %s\n", cwd, m.DotfilesDir)
		os.Exit(1)
	}

	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	pkgName := parts[0]
	targetPath := filepath.Join(m.DotfilesDir, pkgName)

	fmt.Printf("==> Staging and committing changes for package '%s'...\n", pkgName)
	if err := m.gitCommit(message, targetPath); err != nil {
		fmt.Fprintf(os.Stderr, "==> Failed to commit changes: %v\n", err)
	} else {
		fmt.Printf("==> Changes saved: %s\n", message)
	}
}

func (m *Manager) UpdateConfigs() {
	home, _ := os.UserHomeDir()
	fmt.Println("==> Pulling latest changes from git...")
	if err := m.Runner.Run("git", "-C", m.DotfilesDir, "pull"); err != nil {
		fmt.Printf("Git pull encountered a warning/error: %v\n", err)
	}

	pkgs, err := manifest.Load(m.IniPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load %s: %v\n", m.IniPath, err)
		os.Exit(1)
	}

	var configs []string
	for c := range pkgs.Configs {
		configs = append(configs, c)
	}

	if len(configs) == 0 {
		entries, _ := os.ReadDir(m.DotfilesDir)
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && e.Name() != "pkgmgr" {
				configs = append(configs, e.Name())
			}
		}
	}

	if len(configs) > 0 {
		fmt.Printf("==> Restowing %d configuration packages: %s\n", len(configs), strings.Join(configs, ", "))
		args := append([]string{"-v", "-R", "-d", m.DotfilesDir, "-t", home}, configs...)
		if err := m.Runner.Run("stow", args...); err != nil {
			fmt.Fprintf(os.Stderr, "Stow error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("==> All dotfile symlinks up to date!")
	} else {
		fmt.Println("No configuration packages found to stow.")
	}
}

func (m *Manager) BootstrapFromIni(source string) {
	tempFile := source

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		if strings.HasPrefix(source, "http://") {
			fmt.Println("Warning: Using unencrypted HTTP for bootstrap. HTTPS is recommended.")
		}
		
		fmt.Printf("==> Downloading %s...\n", source)
		resp, err := http.Get(source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to fetch URL: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "Failed to fetch URL: HTTP %d %s\n", resp.StatusCode, resp.Status)
			os.Exit(1)
		}

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

	pkgs, err := manifest.Load(tempFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load packages file: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stat(m.DotfilesDir); os.IsNotExist(err) {
		if pkgs.Remote == "" {
			fmt.Fprintf(os.Stderr, "Error: No remote repository defined in [meta] section of %s\n", source)
			os.Exit(1)
		}
		fmt.Printf("==> Cloning dotfiles repo from %s to %s...\n", pkgs.Remote, m.DotfilesDir)
		if err := m.Runner.Run("git", "clone", pkgs.Remote, m.DotfilesDir); err != nil {
			fmt.Fprintf(os.Stderr, "Git clone failed: %v\n", err)
			os.Exit(1)
		}
	}

	m.UpdateConfigs()
	m.SyncAll()
	fmt.Println("==> System bootstrap complete!")
}

func (m *Manager) removeFromMakefile(pkgName string) error {
	makefilePath := filepath.Join(m.DotfilesDir, "Makefile")
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
			var newPkgs []string
			for _, p := range currentPkgs {
				if p != pkgName {
					newPkgs = append(newPkgs, p)
				}
			}
			// If lengths differ, we successfully removed it
			if len(newPkgs) != len(currentPkgs) {
				lines[i] = fmt.Sprintf("PACKAGES = %s", strings.Join(newPkgs, " "))
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

func (m *Manager) UnwatchPackage(pkgName string) {
	home, _ := os.UserHomeDir()
	stowPkgDir := filepath.Join(m.DotfilesDir, pkgName)

	if _, err := os.Stat(stowPkgDir); os.IsNotExist(err) {
		fmt.Printf("Package '%s' does not exist in %s\n", pkgName, m.DotfilesDir)
		return
	}

	fmt.Printf("==> Unstowing package '%s'...\n", pkgName)
	if err := m.Runner.Run("stow", "-v", "-D", "-d", m.DotfilesDir, "-t", home, pkgName); err != nil {
		fmt.Fprintf(os.Stderr, "Stow delete failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("==> Restoring files to original location...")
	// cp -a src/. dest/ reliably moves contents recursively while preserving permissions
	if err := m.Runner.Run("cp", "-a", stowPkgDir+"/.", home+"/"); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to restore files: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("==> Cleaning up repository...")
	os.RemoveAll(stowPkgDir)

	pkgs, _ := manifest.Load(m.IniPath)
	if pkgs.Configs[pkgName] {
		delete(pkgs.Configs, pkgName)
		if remote := m.getGitRemote(); remote != "" {
			pkgs.Remote = remote
		}
		manifest.Save(m.IniPath, pkgs)
	}

	m.removeFromMakefile(pkgName)

	commitMsg := fmt.Sprintf("Removed %s config from dotfiles", pkgName)
	
	// Stage the specific updates and the deleted directory
	if err := m.gitCommit(commitMsg, stowPkgDir, m.IniPath, filepath.Join(m.DotfilesDir, "Makefile")); err == nil {
		fmt.Printf("==> Successfully unwatched and restored %s!\n", pkgName)
	} else {
		fmt.Fprintf(os.Stderr, "==> Failed to commit changes: %v\n", err)
	}
}
