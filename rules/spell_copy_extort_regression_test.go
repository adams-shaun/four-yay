package rules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestSpellCopyOutsideStackCeases(t *testing.T) {
	e := layerEngine(t)
	copyObj := e.G.AddObject(card(t, "Name:Copied Bolt\nTypes:Instant\nOracle:x\n"), 0)
	copyObj.IsCopy = true
	copyObj.Zone = state.ZExile
	e.G.SetZone(state.ZExile, 0, []state.ObjID{copyObj.ID})
	if !copyObj.IsCopy || copyObj.Zone != state.ZExile {
		t.Fatal("precondition: expected a spell copy outside the stack")
	}
	if !e.ceaseDeadTokens(&sbaAttempts{}) {
		t.Fatal("SBA did not process the non-token spell copy")
	}
	if got := e.G.Obj(copyObj.ID).Zone; got != state.ZCeased {
		t.Fatalf("copy zone = %v, want ceased", got)
	}
}

func TestOwnSpellCastZoneExceptionRequiresSelfCard(t *testing.T) {
	e := layerEngine(t)
	obj := e.G.AddObject(card(t, "Name:Probe\nTypes:Instant\nOracle:x\n"), 0)
	obj.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{obj.ID})
	ev := events.Event{Kind: events.PutOnStack, Obj: obj.ID, From: state.ZHand}
	bare := card(t, "Name:Bare\nTypes:Instant\nT:Mode$ SpellCast | Execute$ Trig\nSVar:Trig:DB$ GainLife | LifeAmount$ 1\nOracle:x\n").Faces[0].Triggers[0]
	if e.zoneGate(bare, obj.ID, ev) {
		t.Fatal("bare own SpellCast trigger passed the exception without ValidCard$ Card.Self")
	}
	self := card(t, "Name:Self\nTypes:Instant\nT:Mode$ SpellCast | ValidCard$ Card.Self | Execute$ Trig\nSVar:Trig:DB$ GainLife | LifeAmount$ 1\nOracle:x\n").Faces[0].Triggers[0]
	if !e.zoneGate(self, obj.ID, ev) {
		t.Fatal("self-referential own SpellCast trigger was not admitted")
	}
	cryptGhast, ok := testutil.CorpusRegistry(t).Lookup("Crypt Ghast")
	if !ok {
		t.Fatal("precondition: Crypt Ghast missing from corpus")
	}
	var extort cards.Trigger
	found := false
	for _, trig := range cryptGhast.Faces[0].Triggers {
		if trig.Mode == "SpellCast" && trig.ParamStr(cards.PKKeyword) == "Extort" {
			extort, found = trig, true
			break
		}
	}
	if !found {
		t.Fatal("precondition: Crypt Ghast has no expanded Extort SpellCast trigger")
	}
	if extort.ParamStr(cards.PKTriggerZones) != "Battlefield" {
		t.Fatalf("expanded Extort TriggerZones = %q, want Battlefield", extort.ParamStr(cards.PKTriggerZones))
	}
	if e.zoneGate(extort, obj.ID, ev) {
		t.Fatal("Extort trigger was admitted from the cast spell on the stack")
	}
}

// The corpus ratchet pins every card-file carrier, not just the cards currently
// used by behavioral tests. It is deliberately based on filenames and script
// markers, never copied Forge script text.
func TestSpellCopyExtortAndBareSpellCastCarrierCensus(t *testing.T) {
	root := filepath.Join("..", ".cards", "cardsfolder")
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(path, root+string(filepath.Separator))] = string(b)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		marker, list string
		match        func(string) bool
	}{
		{"CopySpellAbility", "testdata/spell-copy-carriers.txt", func(s string) bool { return strings.Contains(s, "CopySpellAbility") }},
		{"K:Extort", "testdata/extort-carriers.txt", func(s string) bool { return strings.Contains(s, "K:Extort") }},
		{"SpellCast trigger", "testdata/spellcast-zone-exception-carriers.txt", func(s string) bool {
			for _, line := range strings.Split(s, "\n") {
				if strings.Contains(line, "Mode$ SpellCast") && !strings.Contains(line, "TriggerZones$") && !strings.Contains(line, "ActiveZones$") && !strings.Contains(line, "Card.Self") {
					return true
				}
			}
			return false
		}},
	} {
		b, err := os.ReadFile(filepath.Join(".", tc.list))
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if line != "" {
				want = append(want, line)
			}
		}
		var got []string
		for name, content := range files {
			if tc.match(content) {
				got = append(got, name)
			}
		}
		sort.Strings(got)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s corpus carriers changed: got %d, pinned %d", tc.marker, len(got), len(want))
		}
	}
}
