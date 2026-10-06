package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
)

func readLinesBytes(t *testing.T, path string) [][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out [][]byte
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 64<<20)
	for s.Scan() {
		if len(s.Bytes()) == 0 {
			continue
		}
		out = append(out, append([]byte(nil), s.Bytes()...))
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestGenLevelBAddsRequirementKeySkips: -level A is byte-identical to the
// generated level-A item per manifest card (today's output), and -level B
// writes those same items plus, for a requirement no template serves yet,
// one skip line naming its key.
func TestGenLevelBAddsRequirementKeySkips(t *testing.T) {
	dir := filepath.Join("..", "..", ".cards")
	m := compliance.Manifest{Code: "TINY", Cards: []compliance.ManifestCard{
		{Name: "Shock"}, {Name: "Forest"}, {Name: "Prodigal Sorcerer"},
	}}
	tmp := t.TempDir()
	mf := filepath.Join(tmp, "TINY.json")
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mf, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := loadReg(dir)
	if err != nil {
		t.Fatal(err)
	}
	var wantA [][]byte
	for _, mc := range m.Cards {
		it, skip := templates.Generate(reg, mc.Name)
		if skip != nil {
			t.Fatalf("%s: %s", mc.Name, skip.Reason)
		}
		b, _ := json.Marshal(it)
		wantA = append(wantA, b)
	}
	if len(wantA) == 0 {
		t.Fatal("the manifest generated no level-A item; the test asserts nothing")
	}

	outA := filepath.Join(tmp, "a.jsonl")
	if err := runGen(dir, mf, outA, "A"); err != nil {
		t.Fatal(err)
	}
	gotA := readLinesBytes(t, outA)
	if len(gotA) != len(wantA) {
		t.Fatalf("-level A wrote %d lines, want %d", len(gotA), len(wantA))
	}
	for i := range wantA {
		if !bytes.Equal(gotA[i], wantA[i]) {
			t.Errorf("-level A line %d changed:\n got %s\nwant %s", i, gotA[i], wantA[i])
		}
	}
	aSkips, err := os.ReadFile(outA + ".skips.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(aSkips), "activate#0.0") {
		t.Fatalf("-level A wrote a level-B requirement skip:\n%s", aSkips)
	}

	outB := filepath.Join(tmp, "b.jsonl")
	if err := runGen(dir, mf, outB, "B"); err != nil {
		t.Fatal(err)
	}
	gotB := readLinesBytes(t, outB)
	if len(gotB) != len(wantA) {
		t.Fatalf("-level B wrote %d scenarios, want the %d level-A ones (no level-B template lands yet)", len(gotB), len(wantA))
	}
	for i := range wantA {
		if !bytes.Equal(gotB[i], wantA[i]) {
			t.Errorf("-level B scenario %d is not the level-A item:\n got %s\nwant %s", i, gotB[i], wantA[i])
		}
	}
	bSkips, err := os.ReadFile(outB + ".skips.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bSkips), "activate#0.0") {
		t.Errorf("-level B skips do not carry the requirement key:\n%s", bSkips)
	}
}
