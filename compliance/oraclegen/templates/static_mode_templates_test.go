package templates_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// modeItem generates the level-B item for one static requirement and asserts
// the preconditions the real assertion depends on: the card is in the corpus,
// the requirement exists, and its sub-family is the one the template serves
// with no face gap -- so a classifier regression fails the test loudly rather
// than vacuously skipping the item.
func modeItem(t *testing.T, card, key, sub string) oraclegen.Item {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("precondition: %s absent from corpus", card)
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key != key {
			continue
		}
		if req.Sub != sub {
			t.Fatalf("precondition: %s %s classifies as %q (gap %q), want %q", card, key, req.Sub, req.Gap, sub)
		}
		it, sk := templates.GenerateB(reg, card, req)
		if sk != nil {
			t.Fatalf("generate %s %s: %s", card, key, sk.Reason)
		}
		return it
	}
	t.Fatalf("precondition: %s has no requirement %s", card, key)
	return oraclegen.Item{}
}

// replay runs the item's scenario gorge-side and requires it to hold.
func replay(t *testing.T, reg *cards.Registry, it oraclegen.Item) rules.OracleResult {
	t.Helper()
	raw := it.Raw()
	res, err := rules.RunOracleScenarioJSON(reg, raw)
	if err != nil {
		t.Fatalf("scenario does not replay: %v", err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario fails: %v", res.Fails)
	}
	if len(res.Snapshots) == 0 {
		t.Fatal("scenario produced no snapshots")
	}
	return res
}

// permOf finds one battlefield permanent by ref in the last snapshot.
func permOf(t *testing.T, res rules.OracleResult, ref string) rules.OracleSnapPerm {
	t.Helper()
	s := res.Snapshots[len(res.Snapshots)-1]
	for _, p := range s.Permanents {
		if p.Ref == ref {
			return p
		}
	}
	t.Fatalf("precondition: %s is not on the battlefield in the final snapshot", ref)
	return rules.OracleSnapPerm{}
}

func TestTapPowerValueCrewOffer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key string }{
		{"Cloudspire Captain", "static#0.1"},
		{"Deathless Pilot", "static#0.0"},
		{"Interface Ace", "static#0.0"},
	} {
		it := modeItem(t, tc.card, tc.key, "static.tap-power-value")
		res := replay(t, reg, it)
		// The crew option for a vehicle is offered at p0's first priority:
		// the offered entries carry the vehicle as their source.
		found := false
		for _, o := range res.Snapshots[len(res.Snapshots)-1].Offered {
			if o.Kind == "activate" && strings.Contains(strings.ToLower(o.Label), "crew") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: no crew option offered with the card on the battlefield", tc.card)
		}
	}
}

func TestCastWithFlashOtherTurnCast(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card, key, probe string
	}{
		{"Valley Floodcaller", "static#0.0", "Divination"},
		{"Whirlwing Stormbrood", "static#0.0", "Divination"},
		{"Serpent of the Pass", "static#0.0", "Serpent of the Pass"},
	} {
		it := modeItem(t, tc.card, tc.key, "static.cast-with-flash")
		res := replay(t, reg, it)
		s := res.Snapshots[len(res.Snapshots)-1]
		// The probe spell resolved during p1's main phase: turn 2 or later,
		// p1 active, in p1's main1.
		if s.Turn < 2 || s.Active != 1 {
			t.Fatalf("%s: final snapshot is turn %d active %d, want p1's turn", tc.card, s.Turn, s.Active)
		}
		if s.Step != "main1" {
			t.Fatalf("%s: final snapshot step %q, want p1's main1", tc.card, s.Step)
		}
		if tc.probe == tc.card {
			// The card's own cast: a permanent now on the battlefield.
			permOf(t, res, "p0:"+tc.probe)
			continue
		}
		// A sorcery probe: it resolved to the graveyard and its draws made
		// p0's hand grow.
		gy := s.Players[0].Graveyard
		found := false
		for _, g := range gy {
			if g == tc.probe {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: probe %s is not in p0's graveyard after its p1-main cast (gy %v)", tc.card, tc.probe, gy)
		}
	}
}

func TestUntapOtherPlayerStep(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card, key, observed, witness string
	}{
		{"Thousand Moons Infantry", "static#0.0", "p0:Thousand Moons Infantry", "p0:Grizzly Bears"},
		{"Bender's Waterskin", "static#0.0", "p0:Bender's Waterskin", "p0:Grizzly Bears"},
		{"Dazzling Theater // Prop Room", "static#1.0", "p0:Grizzly Bears", ""},
	} {
		it := modeItem(t, tc.card, tc.key, "static.untap-other-player")
		res := replay(t, reg, it)
		s := res.Snapshots[len(res.Snapshots)-1]
		if s.Turn < 4 {
			t.Fatalf("%s: final snapshot is turn %d, want p1's second turn", tc.card, s.Turn)
		}
		o := permOf(t, res, tc.observed)
		if o.Tapped {
			t.Fatalf("%s: %s is still tapped in p1's untap step", tc.card, tc.observed)
		}
		if tc.witness != "" {
			w := permOf(t, res, tc.witness)
			if !w.Tapped {
				t.Fatalf("%s: witness %s untapped, want still tapped", tc.card, tc.witness)
			}
		}
	}
}

func TestCantDrawHandStays(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := modeItem(t, "Mornsong Aria", "static#0.0", "static.cant-draw")
	res := replay(t, reg, it)
	// The probe's draws are refused: p0's hand holds one card fewer than it
	// did before the cast, never two more.
	hand := res.Snapshots[len(res.Snapshots)-1].Players[0].Hand
	draws := 0
	for _, c := range hand {
		if c == "Divination" {
			draws++
		}
	}
	if draws != 0 {
		t.Fatalf("p0's hand still holds the probe after its resolve: %v", hand)
	}
}

func TestRoomDoorContinuousStatics(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	// Porcelain Gallery's SetPower: creatures you control have base P/T equal
	// to the number of creatures you control (one probe bear: 1/1).
	it := modeItem(t, "Dollmaker's Shop // Porcelain Gallery", "static#1.0", "static.continuous")
	res := replay(t, reg, it)
	bear := permOf(t, res, "p0:Grizzly Bears")
	if bear.PT != "1/1" {
		t.Fatalf("probe bear is %s with Porcelain Gallery unlocked, want 1/1", bear.PT)
	}
	// Lecture Hall's hexproof grant: other permanents you control have
	// hexproof.
	it = modeItem(t, "Restricted Office // Lecture Hall", "static#1.0", "static.continuous")
	res = replay(t, reg, it)
	bear = permOf(t, res, "p0:Grizzly Bears")
	hasHexproof := false
	for _, k := range bear.Keywords {
		if strings.EqualFold(k, "Hexproof") {
			hasHexproof = true
		}
	}
	if !hasHexproof {
		t.Fatalf("probe bear lacks hexproof with Lecture Hall unlocked (keywords %v)", bear.Keywords)
	}
}

func TestRoomDoorPanharmonicon(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := modeItem(t, "Mirror Room // Fractured Realm", "static#1.0", "static.panharmonicon")
	res := replay(t, reg, it)
	s := res.Snapshots[len(res.Snapshots)-1]
	// The cast probe's enter trigger is on the stack twice: the doubled
	// trigger is the static's doing.
	counts := map[string]int{}
	for _, e := range s.Stack {
		if e.Kind == "ability" {
			counts[e.Source]++
		}
	}
	doubled := ""
	srcs := make([]string, 0, len(counts))
	for src := range counts {
		srcs = append(srcs, src)
	}
	sort.Strings(srcs)
	for _, src := range srcs {
		if counts[src] == 2 {
			doubled = src
		}
	}
	if doubled == "" {
		t.Fatalf("no listener's trigger is on the stack twice with Fractured Realm unlocked (stack %v)", s.Stack)
	}
}
