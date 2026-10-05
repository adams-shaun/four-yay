package rules

// Round-1 regression pins for the two MSH roots:
//
//   1. A copy of a spell that leaves the stack ceases to exist (CR 707.10a,
//      704.5e). The end-to-end pin is
//      TestCopiedInstantSpellCeasesWhenItLeavesTheStack in
//      copied_permanent_token_test.go (it casts a real CopySpellAbility card,
//      drains resolution, and asserts ZCeased); TestCopiedInstantSpell* is
//      deliberately NOT a direct call to the ceaseDeadTokens private helper,
//      because that helper's branch is inert end-to-end unless the SBA quiet
//      classifier also treats a copy's off-stack move as unquiet.
//
//   2. The CR 601.2i own-cast exception in zoneGate must admit only a
//      card's own "when you cast this spell" line (ValidCard$ Card.Self),
//      not any SpellCast trigger carried by the cast object. Extort is the
//      named carrier: its keyword expansion now declares TriggerZones$
//      Battlefield, and the zone gate refuses it while the spell is on the
//      stack.
//
// The corpus census (TestSpellCopyExtortAndBareSpellCastCarrierCensus)
// ratchets every carrier filename, so a new carrier fails loudly.

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

// TestOwnSpellCastZoneExceptionRequiresSelfCard pins the gate directly: a bare
// own-cast SpellCast trigger (no ValidCard$ Card.Self) is refused the CR
// 601.2i exception, a self-referential one is admitted, and the real expanded
// Extort trigger carries TriggerZones$ Battlefield so the gate refuses it from
// the cast spell on the stack.
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

// TestRealNonSelfBattlefieldSpellCastTriggerStillFires proves the fix does not
// over-gate: a real corpus permanent whose SpellCast trigger names a
// non-Self filter with no TriggerZones$ (Tablet of the Guilds, "Whenever you
// cast a spell, if it's at least one of the chosen colors, you gain 1 life
// for each of the chosen colors it is") must still fire from the
// battlefield. The source is the permanent, not the cast object, so the
// CR 601.2i own-cast exception never applied to it; the battlefield default
// admits it, and the Card.Self tightening does not touch that path.
//
// The corpus-wide effect of the tightening (103 carrier files, see
// testdata/spellcast-zone-exception-carriers.txt) is unmeasured by a game
// here; the census below is the only corpus-wide pin. Reported as an open
// concern rather than silently claimed.
func TestRealNonSelfBattlefieldSpellCastTriggerStillFires(t *testing.T) {
	e := layerEngine(t)
	perm := e.G.AddObject(card(t, "Name:Tablet\nTypes:Artifact\nOracle:x\n"), 0)
	perm.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{perm.ID})

	reg := searchTestRegistry(t)
	tablet := mustCorpusCard(t, reg, "Tablet of the Guilds")
	var trig cards.Trigger
	found := false
	for _, tr := range tablet.Faces[0].Triggers {
		if tr.Mode == "SpellCast" {
			trig, found = tr, true
			break
		}
	}
	if !found {
		t.Fatal("precondition: Tablet of the Guilds has no SpellCast trigger in the corpus")
	}
	if trig.ParamStr(cards.PKTriggerZones) != "" {
		t.Fatalf("precondition: Tablet of the Guilds TriggerZones = %q, want empty (this is the bare shape under test)", trig.ParamStr(cards.PKTriggerZones))
	}
	if strings.Contains(trig.ParamStr(cards.PKValidCard), "Card.Self") {
		t.Fatalf("precondition: Tablet of the Guilds ValidCard = %q, want a non-Self filter", trig.ParamStr(cards.PKValidCard))
	}
	// The cast object is some other spell on the stack; the trigger's source
	// is the permanent. The own-cast exception is gated on source == ev.Obj,
	// so it cannot apply.
	cast := e.G.AddObject(card(t, "Name:Cast Spell\nTypes:Instant\nOracle:x\n"), 0)
	cast.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{cast.ID})
	ev := events.Event{Kind: events.PutOnStack, Obj: cast.ID, From: state.ZHand}
	if !e.zoneGate(trig, perm.ID, ev) {
		t.Fatal("real non-Self battlefield SpellCast trigger was gated out")
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
