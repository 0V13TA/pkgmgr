package dotfiles

import (
	"testing"
)

// MockRunner captures commands instead of running them
type MockRunner struct {
	Commands [][]string
}

func (m *MockRunner) Run(name string, args ...string) error {
	m.Commands = append(m.Commands, append([]string{name}, args...))
	return nil
}

func (m *MockRunner) Output(name string, args ...string) (string, error) {
	m.Commands = append(m.Commands, append([]string{name}, args...))
	return "", nil
}

func TestInstallPackagesGitTracking(t *testing.T) {
	mock := &MockRunner{}
	mgr := NewManager(mock)

	// Inject custom path for test safety
	mgr.IniPath = t.TempDir() + "/packages.ini"

	mgr.InstallPackages([]string{"-S", "ripgrep"})

	// Verify the mock intercepted the yay command
	if mock.Commands[0][0] != "yay" {
		t.Errorf("Expected yay, got %s", mock.Commands[0][0])
	}
	// Verify git commit was fired
	if mock.Commands[len(mock.Commands)-1][0] != "git" {
		t.Errorf("Expected git commit, got %s", mock.Commands[len(mock.Commands)-1][0])
	}
}
