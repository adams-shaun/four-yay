package oraclegen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// censusSets are the standard-audit sets this fixture census covers. A face
// in one of them whose target slots have no candidate list is a carrier of a
// fixture gap; the counts below pin how many, so a new carrier fails loudly.
var censusSets = []string{
	"HOB", "MSH", "SOS", "ECL", "TLA", "SPM", "FIN", "DSK", "BLB",
	"OTJ", "MKM", "LCI", "WOE", "TDM", "FDN",
}

// fixtureCensus pins, per unsatisfiable slot filter, the exact faces that
// carry it. This is the class ratchet: oraclegen.Fixtures returns nil for a
// slot whose candidateFor list is empty (a stack-only slot is coverable by a
// precast and is not a gap), so a new slot filter the builder cannot serve
// adds a key (or a face) here and fails TestFixtureCensus.
//
// Measured 2026-10-04 at the commit that fixed stack targets and additional
// costs. The list may only shrink; a fix deletes its entries, and a new
// carrier must be added deliberately (which is the point -- it fails first).
var fixtureCensus = map[string][]string{
	"Elf.YouCtrl":                       {"Trystan's Command"},
	"Equipment":                         {"Stolen Uniform"},
	"Equipment.AttachedTo ParentTarget": {"Fiery Annihilation"},
	"Goblin.YouCtrl":                    {"Grub's Command"},
	"Kithkin.YouCtrl":                   {"Brigid's Command"},
	"Merfolk.YouCtrl":                   {"Sygg's Command"},
	"Saga.YouCtrl":                      {"Clash of the Eikons"},
}

func censusRegistry(t *testing.T) *cards.Registry {
	t.Helper()
	reg, err := cards.LoadRegistry(cards.CachePath(filepath.Join("..", "..", ".cards")))
	if err != nil {
		t.Fatalf("census needs the corpus (make fetch-cards compile-cards): %v", err)
	}
	return reg
}

func censusManifestNames(t *testing.T) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	for _, set := range censusSets {
		raw, err := os.ReadFile(filepath.Join("..", "..", "compliance", "manifests", set+".json"))
		if err != nil {
			t.Fatalf("manifest %s: %v", set, err)
		}
		var m struct {
			Cards []struct {
				Name string `json:"name"`
			} `json:"cards"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("manifest %s: %v", set, err)
		}
		names := map[string]bool{}
		for _, c := range m.Cards {
			names[strings.ToLower(strings.TrimSpace(c.Name))] = true
		}
		out[set] = names
	}
	return out
}

// censusGaps scans every nonland spell face in the audit's sets and buckets
// those the static fixture builder cannot satisfy, keyed by the first
// unsatisfiable target slot.
func censusGaps(t *testing.T) map[string][]string {
	t.Helper()
	reg := censusRegistry(t)
	manifests := censusManifestNames(t)
	gaps := map[string][]string{}
	for i := range reg.Cards {
		c := reg.Cards[i]
		name := strings.ToLower(strings.TrimSpace(c.Faces[0].Name))
		inSet := false
		for _, names := range manifests {
			if names[name] {
				inSet = true
				break
			}
		}
		if !inSet {
			continue
		}
		for fi := range c.Faces {
			f := c.Faces[fi]
			if hasType(f, "Land") || strings.TrimSpace(f.ManaCost) == "" || strings.EqualFold(f.ManaCost, "no cost") {
				continue
			}
			if ok, reason := FaceHasFixture(f); !ok {
				gaps[reason] = append(gaps[reason], f.Name)
			}
		}
	}
	for k := range gaps {
		sort.Strings(gaps[k])
	}
	return gaps
}

// TestFixtureCensus is the ratchet: the set of faces with no fixture, per
// reason, is pinned. A new carrier fails and is named; a fixed carrier must
// delete its entry.
func TestFixtureCensus(t *testing.T) {
	gaps := censusGaps(t)
	for reason, want := range fixtureCensus {
		got := gaps[reason]
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("no fixture: %s\n got %v\nwant %v", reason, got, want)
		}
	}
	for reason, got := range gaps {
		if _, pinned := fixtureCensus[reason]; !pinned {
			t.Errorf("no fixture: %s %v (not pinned; add it deliberately)", reason, got)
		}
	}
}
