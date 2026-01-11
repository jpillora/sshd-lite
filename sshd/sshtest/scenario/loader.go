package scenario

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

//go:embed fixtures/*.yml
var fixturesFS embed.FS

// LoadFixture loads a scenario from the fixtures directory by name.
// The name can be with or without the .yml/.yaml extension.
func LoadFixture(name string) (*Scenario, error) {
	// Try with .yml extension first
	content, err := loadFixtureFile(name)
	if err != nil {
		return nil, err
	}
	return Parse(string(content))
}

func loadFixtureFile(name string) ([]byte, error) {
	// Normalize name
	name = strings.TrimSuffix(name, ".yml")
	name = strings.TrimSuffix(name, ".yaml")

	// Try .yml - use forward slashes for embed.FS (not filepath.Join)
	path := "fixtures/" + name + ".yml"
	content, err := fixturesFS.ReadFile(path)
	if err == nil {
		return content, nil
	}

	// Try .yaml
	path = "fixtures/" + name + ".yaml"
	content, err = fixturesFS.ReadFile(path)
	if err == nil {
		return content, nil
	}

	return nil, fmt.Errorf("fixture %q not found", name)
}

// ListFixtures returns a list of available fixture names.
func ListFixtures() ([]string, error) {
	var names []string
	err := fs.WalkDir(fixturesFS, "fixtures", func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := path.Ext(fpath)
		if ext == ".yml" || ext == ".yaml" {
			name := strings.TrimPrefix(fpath, "fixtures/")
			name = strings.TrimSuffix(name, ext)
			names = append(names, name)
		}
		return nil
	})
	return names, err
}

// LoadAllFixtures loads all fixtures from the fixtures directory.
func LoadAllFixtures() ([]*Scenario, error) {
	names, err := ListFixtures()
	if err != nil {
		return nil, err
	}

	var scenarios []*Scenario
	for _, name := range names {
		scenario, err := LoadFixture(name)
		if err != nil {
			return nil, fmt.Errorf("failed to load fixture %q: %w", name, err)
		}
		scenarios = append(scenarios, scenario)
	}
	return scenarios, nil
}
