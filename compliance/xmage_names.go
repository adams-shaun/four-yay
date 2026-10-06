package compliance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// LoadXMageNames returns the union of names in the committed XMage set
// manifests under dir. Split faces are retained here; oraclegen expands them
// into independently matchable face names when installing the set.
func LoadXMageNames(dir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("glob XMage manifests in %s: %w", dir, err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no XMage manifests found in %s", dir)
	}
	var names []string
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read XMage manifest %s: %w", path, err)
		}
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("parse XMage manifest %s: %w", path, err)
		}
		unfinished := make(map[string]bool, len(m.Unfinished))
		for _, name := range m.Unfinished {
			unfinished[FoldName(name)] = true
		}
		for _, card := range m.Cards {
			if card.Name != "" && !unfinished[FoldName(card.Name)] {
				names = append(names, card.Name)
			}
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("XMage manifests in %s contain no card names", dir)
	}
	return names, nil
}
