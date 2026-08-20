package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestLoadSavePackages verifies that the custom INI parser correctly serializes
// and deserializes the PackageList struct without data loss.
func TestLoadSavePackages(t *testing.T) {
	tempDir := t.TempDir() // Automatically cleaned up after the test
	iniPath := filepath.Join(tempDir, "packages.ini")

	original := &PackageList{
		Remote:  "https://github.com/0V13TA/dotfiles.git",
		Pacman:  map[string]bool{"git": true, "neovim": true, "btop": true},
		AUR:     map[string]bool{"yay": true, "bastet": true},
		Configs: map[string]bool{"nvim": true, "zsh": true, "hypr": true},
	}

	// Test Saving
	err := savePackages(iniPath, original)
	if err != nil {
		t.Fatalf("Failed to save packages: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(iniPath); os.IsNotExist(err) {
		t.Fatalf("packages.ini was not created")
	}

	// Test Loading
	loaded, err := loadPackages(iniPath)
	if err != nil {
		t.Fatalf("Failed to load packages: %v", err)
	}

	// Assertions
	if loaded.Remote != original.Remote {
		t.Errorf("Expected remote %q, got %q", original.Remote, loaded.Remote)
	}
	if !reflect.DeepEqual(loaded.Pacman, original.Pacman) {
		t.Errorf("Expected pacman %v, got %v", original.Pacman, loaded.Pacman)
	}
	if !reflect.DeepEqual(loaded.AUR, original.AUR) {
		t.Errorf("Expected aur %v, got %v", original.AUR, loaded.AUR)
	}
	if !reflect.DeepEqual(loaded.Configs, original.Configs) {
		t.Errorf("Expected configs %v, got %v", original.Configs, loaded.Configs)
	}
}

// TestUpdateMakefilePackages ensures the regex correctly identifies and appends
// a new package to the PACKAGES line without breaking the rest of the Makefile.
func TestUpdateMakefilePackages(t *testing.T) {
	tempDir := t.TempDir()
	makefilePath := filepath.Join(tempDir, "Makefile")

	initialContent := "PACKAGES = zsh nvim\nall:\n\tstow -v -R -t ~ $(PACKAGES)\n"
	err := os.WriteFile(makefilePath, []byte(initialContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write initial Makefile: %v", err)
	}

	// Append a new package
	err = updateMakefilePackages(tempDir, "hypr")
	if err != nil {
		t.Fatalf("Failed to update Makefile: %v", err)
	}

	content, err := os.ReadFile(makefilePath)
	if err != nil {
		t.Fatalf("Failed to read updated Makefile: %v", err)
	}

	expectedLine := "PACKAGES = zsh nvim hypr"
	if !strings.Contains(string(content), expectedLine) {
		t.Errorf("Expected Makefile to contain %q, but got:\n%s", expectedLine, string(content))
	}

	// Test idempotent behavior (adding a package that already exists shouldn't duplicate it)
	err = updateMakefilePackages(tempDir, "nvim")
	if err != nil {
		t.Fatalf("Failed second update: %v", err)
	}
	
	content, _ = os.ReadFile(makefilePath)
	if strings.Contains(string(content), "nvim nvim") {
		t.Errorf("Duplicate package detected. Idempotency failed:\n%s", string(content))
	}
}

// TestEmptyLoadPackages ensures the parser handles a non-existent file gracefully 
// by returning an empty, initialized struct rather than a nil pointer crash.
func TestEmptyLoadPackages(t *testing.T) {
	tempDir := t.TempDir()
	missingFile := filepath.Join(tempDir, "does_not_exist.ini")

	loaded, err := loadPackages(missingFile)
	if err != nil {
		t.Fatalf("Expected no error for missing file, got: %v", err)
	}

	if loaded.Pacman == nil || loaded.AUR == nil || loaded.Configs == nil {
		t.Errorf("Expected maps to be initialized, got nil maps")
	}
}
