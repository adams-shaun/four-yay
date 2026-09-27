package cards

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSpecializeFacesAndRegistryNames(t *testing.T) {
	src := `Name:Front
AlternateMode:Specialize
ManaCost:2 G
Types:Creature Druid
K:Specialize:3
SPECIALIZE:WHITE
Name:White Form
ManaCost:1 W
Types:Creature
SPECIALIZE:BLUE
Name:Blue Form
ManaCost:U
Types:Creature Bird
`
	c, diags := ParseBytes("fixture.txt", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("layout diagnostics = %+v", diags)
	}
	if len(c.Faces) != 3 {
		t.Fatalf("faces = %d, want 3", len(c.Faces))
	}
	wantNames := []string{"Front", "White Form", "Blue Form"}
	wantCosts := []string{"2 G", "1 W", "U"}
	wantTypes := []string{"Druid", "Creature", "Bird"}
	r := NewRegistry()
	r.Add(c)
	for i, f := range c.Faces {
		if f.Name != wantNames[i] || f.ManaCost != wantCosts[i] || len(f.Types) == 0 || f.Types[len(f.Types)-1] != wantTypes[i] {
			t.Errorf("face %d = name %q cost %q types %v", i, f.Name, f.ManaCost, f.Types)
		}
		if i > 0 && f.SpecializeColor == "" {
			t.Errorf("face %d has no specialize color", i)
		}
		if got, ok := r.Lookup(f.Name); !ok || got != c {
			t.Errorf("Lookup(%q) = %v, %v", f.Name, got, ok)
		}
	}
}

func TestRegistrySpecializeCorpusFaces(t *testing.T) {
	root := filepath.Join("..", ".cards", "cardsfolder")
	r, diags, err := CompileDir(root)
	if err != nil {
		t.Fatal(err)
	}
	unsupported := map[string]int{}
	for _, d := range diags {
		if strings.Contains(d.Msg, "unsupported K:Specialize rider") {
			unsupported[d.Msg]++
		}
	}
	wantRiders := map[string]bool{
		"unsupported K:Specialize rider AdditionalActivationZone$": false,
		"unsupported K:Specialize rider ReduceCost$":               false,
	}
	for msg := range unsupported {
		if _, ok := wantRiders[msg]; !ok {
			t.Errorf("unexpected Specialize rider diagnostic %q", msg)
			continue
		}
		wantRiders[msg] = true
	}
	for msg, seen := range wantRiders {
		if !seen {
			t.Errorf("missing Specialize rider diagnostic %q (got %v)", msg, unsupported)
		}
	}
	if len(unsupported) != 2 {
		t.Errorf("unsupported Specialize rider diagnostics = %v, want exactly AdditionalActivationZone$ and ReduceCost$", unsupported)
	}
	count := 0
	for _, c := range r.Cards {
		if c.AlternateMode != "Specialize" {
			continue
		}
		count++
		if len(c.Faces) != 6 {
			t.Errorf("%s faces=%d, want 6", c.Path, len(c.Faces))
		}
		for _, f := range c.Faces {
			if got, ok := r.Lookup(f.Name); !ok || got != c {
				t.Errorf("%s face %q not indexed", c.Path, f.Name)
			}
		}
	}
	if count != 19 {
		t.Fatalf("Specialize cards=%d, want measured 19", count)
	}
}
