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
	"github.com/adams-shaun/gorge/compliance/oraclegen"
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

// TestGenLevelBAddsRequirementRows: -level A is byte-identical to the
// generated level-A item per manifest card (today's output), and -level B
// writes those same items plus a scenario for a newly served activated ability.
func TestGenLevelBAddsRequirementRows(t *testing.T) {
	dir := filepath.Join("..", "..", ".cards")
	m := compliance.Manifest{Code: "TINY", Cards: []compliance.ManifestCard{
		{Name: "Shock"}, {Name: "Forest"}, {Name: "Prodigal Sorcerer"}, {Name: "Wild Mongrel"},
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
	// Level B is a superset of A: every level-A item still appears, in its
	// original relative order, now interleaved with one scenario per served
	// level-B requirement (Prodigal Sorcerer's activate#0.0).
	if !itemsAppearInOrder(gotB, wantA) {
		t.Fatalf("-level B dropped or reordered a level-A item:\n got %s\nwant %s", gotB, wantA)
	}
	servedB := 0
	for _, line := range gotB {
		var it oraclegen.Item
		if err := json.Unmarshal(line, &it); err != nil {
			t.Fatalf("decode level-B line: %v", err)
		}
		if strings.Contains(it.Template, "#") {
			servedB++
		}
	}
	if servedB == 0 {
		t.Errorf("-level B wrote no level-B template item; the activate template did not land")
	}
	bSkips, err := os.ReadFile(outB + ".skips.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	// Wild Mongrel's Discard<1/Card> is served by the v1 activate template;
	// pin the new row rather than the obsolete cost-gap skip.
	foundWildMongrel := false
	for _, line := range gotB {
		var it oraclegen.Item
		if err := json.Unmarshal(line, &it); err != nil {
			t.Fatalf("decode level-B line: %v", err)
		}
		if it.Card == "Wild Mongrel" && it.Template == "activate#0.0" {
			foundWildMongrel = true
		}
	}
	if !foundWildMongrel {
		t.Errorf("-level B omitted Wild Mongrel/activate#0.0")
	}
	if strings.Contains(string(bSkips), `"card":"Wild Mongrel"`) {
		t.Errorf("Wild Mongrel still has a level-B skip:\n%s", bSkips)
	}
}

// itemsAppearInOrder reports whether every want line appears in got, in the
// same relative order.
func itemsAppearInOrder(got, want [][]byte) bool {
	j := 0
	for _, g := range got {
		if j < len(want) && bytes.Equal(g, want[j]) {
			j++
		}
	}
	return j == len(want)
}
