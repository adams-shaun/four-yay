package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/searchbench"
)

func TestManifestValidate(t *testing.T) {
	sha := strings.Repeat("a", 64)
	m := searchbench.Manifest{Kind: searchbench.ManifestKind, SchemaVersion: searchbench.ManifestSchemaVersion,
		Dataset:   searchbench.Dataset{Name: "test", License: "CC BY 4.0", URI: "https://example.invalid", SHA256: sha},
		Corpus:    searchbench.Corpus{ForgeRef: strings.Repeat("b", 40), CompilerFingerprint: "test"},
		Selection: searchbench.Selection{Dev: 0, Test: 0, MinimumGameWinRate: .6, MinimumGames: 100, MaximumItemsPerGame: 2},
	}
	if err := m.Seal(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"manifest", "validate", "-in", path}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "valid gorge-searchbench-manifest") || !strings.Contains(out.String(), m.Digest) {
		t.Fatalf("output %q", out.String())
	}
}

func TestUsageRejectsEverythingButManifestValidation(t *testing.T) {
	for _, args := range [][]string{nil, {"manifest"}, {"manifest", "validate"}, {"run", "-in", "x"}, {"manifest", "validate", "-in", "x", "extra"}} {
		if err := run(args, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("run(%q) = %v, want usage", args, err)
		}
	}
}
