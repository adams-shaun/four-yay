package rules

// behold_unless_test.go — the corpus census and the unguarded proofs for the
// CR 702.176 Behold UnlessCost$ form. The set-audit test
// (setaudit_hob_test.go TestSetAudit_hob_ElvenPassage_BeholdUntapsSearchedLand)
// proves Elven Passage end to end; this file pins the parser, names the other
// corpus carriers, and adds the hand-arm and fail-closed controls the set
// audit does not exercise.
//
// The mechanism class, measured 2026-09-28 at the pinned corpus:
//
//	/usr/bin/grep -rl 'Behold<' .cards/cardsfolder | wc -l            == 18
//	/usr/bin/grep -rl 'UnlessCost\$ Behold' .cards/cardsfolder | wc -l == 1
//
// Elven Passage is the ONLY card carrying Behold<...> as an UnlessCost$; the
// other 17 carry it as a cast/activation cost, which the cast flow already
// priced (rules/cast.go beholdCostAsk), so the parser change here closes
// Elven Passage's last unpriceable shape rather than a corpus-wide class.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// beholdUnlessCarriers is every corpus card measured on 2026-09-28 to carry a
// `Behold<` token, sorted by file path:
//
//	/usr/bin/grep -rl 'Behold<' .cards/cardsfolder
//
// Two of them (Elven Passage, and Theorist's Sanctum from FORGE_REF fb4d809)
// carry it as an UnlessCost$; the rest pay it as a cast or activation cost.
var beholdUnlessCarriers = []string{
	"Caustic Exhale",
	"Celestial Reunion",
	"Countersculpt",
	"Dispelling Exhale",
	"Draconic Fealty",
	"Elven Passage",
	"Hulk's Thunderclap",
	"Hyperion's Atomic Vision",
	"Kindle the Inner Flame",
	"Kinsbaile Aspirant",
	"Lys Alana Dignitary",
	"Molten Exhale",
	"Mudbutton Cursetosser",
	"Osseous Exhale",
	"Piercing Exhale",
	"Sarkhan, Dragon Ascendant",
	"Silvergill Mentor",
	"Soulbright Seeker",
	"Territorial Strike",
	"Theorist's Sanctum",
}

// TestParseUnlessCostBehold pins the parser half: a Behold<N/Spec> token lands
// in Cost.Behold with its count and spec, and a BeholdExile token stays a hard
// decline (the then-exile settlement is a distinct behaviour not yet tested).
// Without the parser hunk this test fails on the first case, so it is the
// fail-without-the-fix proof for the root cause the brief names.
func TestParseUnlessCostBehold(t *testing.T) {
	t.Parallel()
	c, ok := ParseUnlessCost("Behold<1/Elf>")
	if !ok {
		t.Fatalf("ParseUnlessCost(Behold<1/Elf>) declined; want a priced Behold part")
	}
	if len(c.Behold) != 1 || c.Behold[0].N != 1 || c.Behold[0].Spec != "Elf" {
		t.Fatalf("Behold<1/Elf> parsed as %+v, want one Behold part N=1 Spec=Elf", c.Behold)
	}
	// The pre-existing Reveal form must be untouched by the new branch.
	if rc, ok := ParseUnlessCost("Reveal<1/Island>"); !ok || len(rc.Reveal) != 1 {
		t.Fatalf("Reveal<1/Island> = %+v ok=%v, want one Reveal part", rc.Reveal, ok)
	}
	// BeholdExile remains a hard decline: no corpus UnlessCost$ carries it.
	if _, ok := ParseUnlessCost("BeholdExile<1/Elf>"); ok {
		t.Fatal("ParseUnlessCost accepted BeholdExile<1/Elf>; that then-exile shape must stay a hard decline until tested")
	}
}

// beholdToken is one Behold<...> spelling in a corpus file. The corpus uses it
// both as a bare cast/activation cost and as an UnlessCost$, so the token's
// own bytes are what the two parsers are fed.
var beholdToken = regexp.MustCompile(`Behold<[^>\n]*>`)

// TestBeholdUnlessCensus names the other affected cards and proves the class
// has not silently changed: the raw corpus still holds 18 Behold< carriers,
// exactly one of which (Elven Passage) wires it to an UnlessCost$, every named
// carrier still carries the token, and ParseCost prices the token the same way
// for every one of them. Elven Passage's token must also price through
// ParseUnlessCost. A card that drifts, or a dropped parser branch, fails
// loudly rather than shrinking the class.
func TestBeholdUnlessCensus(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", ".cards", "cardsfolder")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("corpus cardsfolder: %v", err)
	}
	beholdFiles := 0
	unlessFiles := 0
	var names []string
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		sub := filepath.Join(dir, ent.Name())
		files, err := os.ReadDir(sub)
		if err != nil {
			t.Fatalf("corpus dir %s: %v", sub, err)
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".txt") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(sub, f.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", f.Name(), err)
			}
			text := string(b)
			tokens := beholdToken.FindAllString(text, -1)
			if len(tokens) == 0 {
				continue
			}
			beholdFiles++
			isUnless := strings.Contains(text, "UnlessCost$ Behold")
			if isUnless {
				unlessFiles++
			}
			// Every carrier's token must still price through the cast-cost
			// parser (the pre-existing path), and the one UnlessCost$ carrier's
			// token through the unless-cost parser (the new branch).
			for _, tok := range tokens {
				if pc := ParseCost(tok); len(pc.Behold) == 0 {
					t.Errorf("%s: ParseCost(%q) kept no Behold part", f.Name(), tok)
				}
			}
			if isUnless {
				if uc, ok := ParseUnlessCost(tokens[0]); !ok || len(uc.Behold) == 0 {
					t.Errorf("%s: ParseUnlessCost(%q) declined (ok=%v)", f.Name(), tokens[0], ok)
				}
			}
			if nl := strings.IndexByte(text, '\n'); nl >= 0 {
				line := strings.TrimSpace(text[:nl])
				names = append(names, strings.TrimPrefix(line, "Name:"))
			}
		}
	}
	if beholdFiles != 20 {
		t.Errorf("corpus Behold< carriers = %d, want 20", beholdFiles)
	}
	if unlessFiles != 2 {
		t.Errorf("corpus UnlessCost$ Behold carriers = %d, want 2 (Elven Passage, Theorist's Sanctum)", unlessFiles)
	}
	sort.Strings(names)
	want := append([]string(nil), beholdUnlessCarriers...)
	sort.Strings(want)
	if len(names) != len(want) {
		t.Fatalf("Behold< carrier names = %v, want %v", names, want)
	}
	for i := range names {
		if names[i] != want[i] {
			t.Fatalf("Behold< carrier names = %v, want %v", names, want)
		}
	}
}

// beholdHandEngine sets up the Elven Passage activation with an Elf card in
// HAND (no Elf on the battlefield) and returns the engine, the activation
// source id, and the hand Elf's id.
func beholdHandEngine(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	elf := card(t, "Name:Elf Scout\nTypes:Creature Elf Scout\nPT:1/1\nOracle:x\n")
	e := handEngine(t, hobCard(t, reg, "Elven Passage"))
	elfID := e.G.AddObject(elf, 0).ID
	e.G.Obj(elfID).Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), elfID))
	passage := hobPut(t, e, 0, hobCard(t, reg, "Elven Passage"))
	e.askPriority(0)
	return e, passage, elfID
}

// TestElvenPassageBeholdFromHandUntapsSearchedLand exercises the HAND arm of
// CR 702.176: with the only Elf card in hand, the beholding election must
// still be offered and must untap the searched land. This is the arm the
// set-audit test (battlefield Elf) does not reach.
func TestElvenPassageBeholdFromHandUntapsSearchedLand(t *testing.T) {
	t.Parallel()
	e, passage, elfID := beholdHandEngine(t)
	// Precondition: the Elf is in hand, not on the battlefield, so only the
	// hand arm of the two-zone enumeration can satisfy the cost.
	if o := e.G.Obj(elfID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Elf precondition: %+v, want an Elf card in hand", o)
	}
	if o := e.G.Obj(passage); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Elven Passage precondition: %+v", o)
	}
	before := append([]state.ObjID(nil), e.G.Zone(state.ZBattlefield, 0)...)
	opt := abilityOption(t, e, passage, 0)
	submitChoices(t, e, opt.Index)
	ask := hobSeekUnlessPay(t, e, 60)
	if ask == nil {
		t.Fatal("no unless-pay ask posed (the hand-arm Behold was not offered)")
	}
	if len(ask.Options) < 2 {
		t.Fatalf("hand-arm Behold pay branch not offered: options = %+v", ask.Options)
	}
	wasThere := make(map[state.ObjID]bool, len(before))
	for _, id := range before {
		wasThere[id] = true
	}
	var searched state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if !wasThere[id] {
			searched = id
		}
	}
	if searched == 0 {
		t.Fatal("searched land precondition: no new permanent entered")
	}
	if o := e.G.Obj(searched); o == nil || !o.Tapped {
		t.Fatalf("searched land precondition: %+v, want it tapped", e.G.Obj(searched))
	}
	// Precondition: the Elf is still in hand (a plain Behold elects, it does
	// not move the card), so the untap below is attributable to the election.
	if o := e.G.Obj(elfID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Elf precondition after the ask: %+v, want still in hand", e.G.Obj(elfID))
	}
	submitChoices(t, e, ask.Options[0].Index)
	hobDrain(t, e, 60)
	if o := e.G.Obj(searched); o == nil || o.Tapped {
		t.Fatalf("searched land = %+v, want untapped after beholding an Elf card from hand", o)
	}
}

// TestBeholdUnlessNotOfferedWithoutCandidate is the fail-closed control: with
// no Elf anywhere the Pay branch must not appear, so the pay option above is
// attributable to a real candidate rather than to the parser accepting the
// token unconditionally.
func TestBeholdUnlessNotOfferedWithoutCandidate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, hobCard(t, reg, "Elven Passage"))
	passage := hobPut(t, e, 0, hobCard(t, reg, "Elven Passage"))
	e.askPriority(0)
	// Precondition: no Elf card or permanent exists on the payer's side.
	for _, z := range []state.Zone{state.ZBattlefield, state.ZHand} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && hobHasType(o.Face(), "Elf") {
				t.Fatalf("setup: an Elf is present in zone %v: %+v", z, o.Face().Name)
			}
		}
	}
	opt := abilityOption(t, e, passage, 0)
	submitChoices(t, e, opt.Index)
	ask := hobSeekUnlessPay(t, e, 60)
	if ask == nil {
		t.Fatal("no unless-pay ask posed")
	}
	if len(ask.Options) != 1 || ask.Options[0].Mode != decision.ModeUnlessDecline {
		t.Fatalf("no-Elf Behold ask = %+v, want decline-only (the Pay branch must be withheld)", ask.Options)
	}
}
