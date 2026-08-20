package manifest

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

type PackageList struct {
	Remote  string
	Pacman  map[string]bool
	AUR     map[string]bool
	Configs map[string]bool
}

func Load(path string) (*PackageList, error) {
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

func Save(path string, pkgs *PackageList) error {
	var builder strings.Builder

	if pkgs.Remote != "" {
		builder.WriteString("[meta]\n")
		builder.WriteString(fmt.Sprintf("remote = %s\n\n", pkgs.Remote))
	}

	writeSection(&builder, "pacman", pkgs.Pacman)
	writeSection(&builder, "aur", pkgs.AUR)
	writeSection(&builder, "configs", pkgs.Configs)

	return os.WriteFile(path, []byte(builder.String()), 0644)
}

func writeSection(builder *strings.Builder, name string, items map[string]bool) {
	builder.WriteString(fmt.Sprintf("[%s]\n", name))
	var sorted []string
	for p := range items {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	for _, p := range sorted {
		builder.WriteString(p + "\n")
	}
	builder.WriteString("\n")
}
