package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestGenUsesCanonicalCorpusNameForXMageAlias(t *testing.T) {
	out := filepath.Join(t.TempDir(), "spm.jsonl")
	if err := runGen(filepath.Join("..", "..", ".cards"), filepath.Join("..", "..", "compliance", "manifests", "SPM.json"), out); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	found := false
	s := bufio.NewScanner(f)
	for s.Scan() {
		var it oraclegen.Item
		if err := json.Unmarshal(s.Bytes(), &it); err != nil {
			t.Fatal(err)
		}
		if it.Card == "With Great Power . . ." {
			found = true
			if len(it.Steps) == 0 || it.Steps[0].Card != "p0:With Great Power . . ." {
				t.Errorf("scenario does not use canonical corpus spelling: %+v", it.Steps)
			}
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("With Great Power . . . did not receive a generated scenario")
	}
	skips, err := os.ReadFile(out + ".skips.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(skips), "With Great Power") {
		t.Fatalf("With Great Power remains skipped: %s", skips)
	}
}
