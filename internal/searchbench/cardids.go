package searchbench

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// LoadCardNames reads 17lands' public cards.csv into the minimal Arena-id to
// printed-name table required by replay staging. The raw card file remains an
// external CC-BY artifact.
func LoadCardNames(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	head, err := r.Read()
	if err != nil {
		return nil, err
	}
	ix := headerIndex(head)
	for _, name := range []string{"id", "name"} {
		if _, ok := ix[name]; !ok {
			return nil, fmt.Errorf("searchbench: card source has no %q column", name)
		}
	}
	out := make(map[string]string)
	for rowNo := 2; ; rowNo++ {
		row, err := r.Read()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("searchbench: reading card row %d: %w", rowNo, err)
		}
		if len(row) != len(head) {
			return nil, fmt.Errorf("searchbench: card row %d has %d fields, want %d", rowNo, len(row), len(head))
		}
		if _, err := strconv.Atoi(row[ix["id"]]); err != nil || row[ix["name"]] == "" {
			return nil, fmt.Errorf("searchbench: invalid card row %d", rowNo)
		}
		if old, ok := out[row[ix["id"]]]; ok && old != row[ix["name"]] {
			return nil, fmt.Errorf("searchbench: card id %q has conflicting names", row[ix["id"]])
		}
		out[row[ix["id"]]] = row[ix["name"]]
	}
}

// ResolveEvidence translates a pipe-separated Arena-id action field to card
// names. Ability identifiers intentionally remain explicit until the separate
// abilities.csv resolver lands; silently treating them as cards would corrupt
// human labels.
func ResolveEvidence(ids []string, cards map[string]string) ([]string, error) {
	var out []string
	for _, field := range ids {
		for _, id := range strings.Split(field, "|") {
			if id == "" {
				continue
			}
			name, ok := cards[id]
			if !ok {
				return nil, fmt.Errorf("searchbench: unknown Arena card id %q", id)
			}
			out = append(out, name)
		}
	}
	return out, nil
}
