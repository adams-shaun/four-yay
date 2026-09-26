package rules

// Henzie "Toolbox" Torre's GRANTED blitz (ticket agent-20260926T031204Z):
// the corpus card declares a layer-6, stack-zone keyword grant
//
//	S:Mode$ Continuous | Affected$ Creature.YouCtrl+wasCast | AffectedZone$ Stack
//	  | AddKeyword$ Blitz:CardManaCost:Spell.Creature+cmcGE4
//	S:Mode$ ReduceCost | ValidCard$ Card | ValidSpell$ Spell.Blitz
//	  | Activator$ You | Amount$ CommanderCast
//	SVar:CommanderCast:Count$TotalCommanderCastFromCommandZone
//
// so a mana-value-4+ creature card gains Blitz whose cost is the card's own
// mana cost, and every blitz cost its controller pays is reduced by the
// command-zone commander-cast count. This is NOT printed K:Blitz (that is
// blitz_test.go's Sabin): the cost comes from a grant whose parameter names
// the CardManaCost placeholder, so the offer and the charge must read the
// SAME id-aware grant (rules/cast.go's blitzCost) rather than the printed
// face parameter.
//
// Fixtures are hand-authored (never corpus .txt, licensing): Blitz Four
// ({3}{G}, MV 4 -- qualifies), Blitz Three ({2}{G}, MV 3 -- withheld), and
// the opponent's Blitz Foe ({4}{G}, MV 5 -- qualifies on MV, excluded by
// controller).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	henzieFourSrc  = "Name:Blitz Four\nManaCost:3 G\nTypes:Creature\nPT:4/4\nOracle:x\n"
	henzieThreeSrc = "Name:Blitz Three\nManaCost:2 G\nTypes:Creature\nPT:3/3\nOracle:x\n"
	henzieFoeSrc   = "Name:Blitz Foe\nManaCost:4 G\nTypes:Creature\nPT:5/5\nOracle:x\n"
	henzieName     = "Henzie \"Toolbox\" Torre"
)

// henzieBlitzGame builds a two-seat game: seat 0's deck is (optionally)
// Henzie plus the two fixture creatures over Mountains, seat 1's is the
// fixture foe creature over Mountains. With commander the game is a
// commander-format game whose commander is Henzie (in the command zone at
// genesis); without, Henzie sits in seat 0's hand to be cast the ordinary
// way. Returns the engine, the replayable config and the ids of Henzie (0
// when absent), Blitz Four and Blitz Three.
func henzieBlitzGame(t *testing.T, seed uint64, commander, withHenzie bool) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	var hero []*cards.Card
	if withHenzie {
		h, ok := reg.Lookup(henzieName)
		if !ok {
			t.Fatalf("corpus lacks %q", henzieName)
		}
		hero = append(hero, h)
	}
	hero = append(hero, card(t, henzieFourSrc), card(t, henzieThreeSrc))
	hero = append(hero, mountainDeck(t, 37)...)
	foe := append([]*cards.Card{card(t, henzieFoeSrc)}, mountainDeck(t, 39)...)
	cfg := Config{Seed: seed, Names: []string{"a", "b"}, Decks: [][]*cards.Card{hero, foe},
		Tokens: reg.Tokens}
	if commander {
		cfg.Format = FormatCommander
		cfg.StartingLife = 40
		if withHenzie {
			cfg.Commanders = [][]int{{0}, nil}
		}
	}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	var henzie, four, three state.ObjID
	if withHenzie {
		if commander {
			henzie = e.G.Players[0].Commanders[0]
		} else {
			henzie = findCardObj(t, e, 0, henzieName, state.ZHand)
		}
	}
	four = findCardObj(t, e, 0, "Blitz Four", state.ZHand)
	three = findCardObj(t, e, 0, "Blitz Three", state.ZHand)
	return e, cfg, henzie, four, three
}

// assertHenzieStatics pins the parsed grant and reduction the test exercises:
// the layer-6 AddKeyword$ Blitz grant with the CardManaCost/Spell.Creature+
// cmcGE4 parameter and the ReduceCost static keyed on Spell.Blitz.
func assertHenzieStatics(t *testing.T, e *Engine, henzie state.ObjID) {
	t.Helper()
	face := e.G.Obj(henzie).Face()
	if face == nil || len(face.Statics) == 0 {
		t.Fatalf("setup: Henzie source has no parsed statics")
	}
	foundGrant, foundReduction := false, false
	for _, st := range face.Statics {
		if st.Mode == "ReduceCost" && st.Params["ValidSpell"] == "Spell.Blitz" && st.Params["Amount"] == "CommanderCast" {
			foundReduction = true
		}
		if st.Mode == "Continuous" && st.Params["AddKeyword"] == "Blitz:CardManaCost:Spell.Creature+cmcGE4" {
			foundGrant = true
		}
	}
	if !foundGrant || !foundReduction {
		t.Fatalf("setup: Henzie's parsed Blitz grant/reduction incomplete: grant=%v reduction=%v over %+v",
			foundGrant, foundReduction, face.Statics)
	}
}

// assertHenzieBoardPins pins the preconditions every subtest's assertions lean
// on: the qualifying and withheld creatures sit in hand on either side of
// mana value 4, and the grant reaches the qualifying card in the cast window.
func assertHenzieBoardPins(t *testing.T, e *Engine, henzie, four, three state.ObjID) {
	t.Helper()
	if henzie != 0 && e.G.Obj(henzie).Zone != state.ZBattlefield {
		t.Fatalf("setup: Henzie in %s, want battlefield", e.G.Obj(henzie).Zone)
	}
	if e.G.Obj(four).Zone != state.ZHand || e.G.Obj(three).Zone != state.ZHand {
		t.Fatalf("setup zones: four=%s three=%s, want both in hand", e.G.Obj(four).Zone, e.G.Obj(three).Zone)
	}
	fourMV, threeMV := e.G.Obj(four).Face().ManaValue(), e.G.Obj(three).Face().ManaValue()
	if fourMV != 4 || threeMV != 3 || fourMV == threeMV {
		t.Fatalf("setup mana values: qualifying=%d withheld=%d, want 4 and 3 (differing)", fourMV, threeMV)
	}
	if raw, ok := e.derivedKeywordParamAt(four, "Blitz", state.ZStack); !ok || raw != "CardManaCost:Spell.Creature+cmcGE4" {
		t.Fatalf("setup: layer-6 Blitz grant = %q, %v", raw, ok)
	}
	// The grant's trailing Spell.Creature+cmcGE4 filter is enforced by the
	// shared offer/charge reader (rules/cast.go's blitzCost), not by the
	// keyword-collection walk, so the MV-3 card may carry the raw entry --
	// the right-level pin is that blitzCost (and therefore the offer and
	// the charge) withholds it.
	if _, ok := e.blitzCost(0, three); ok {
		t.Fatal("setup: mana-value-3 creature prices a blitz through the grant")
	}
}

// hasCastOption reports whether the pending priority decision offers a cast
// option for id with the given mode.
func hasModeOption(e *Engine, id state.ObjID, mode string) bool {
	d := e.Pending()
	if d == nil {
		return false
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == mode {
			return true
		}
	}
	return false
}

// resolveBlitzCasted asserts the paid blitz cast resolved onto the
// battlefield with the CR 702.152c riders (FlagBlitzed, haste).
func resolveBlitzCasted(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.CastFlags&state.FlagBlitzed == 0 || !e.HasKeyword(id, "Haste") {
		t.Fatalf("granted Blitz resolution: zone=%s flags=%d haste=%v", o.Zone, o.CastFlags, e.HasKeyword(id, "Haste"))
	}
}

func TestHenzieGrantedBlitzDiscount(t *testing.T) {
	t.Run("zero commander casts charge the printed mana cost", func(t *testing.T) {
		e, cfg, henzie, four, three := henzieBlitzGame(t, 9801, false, true)
		assertHenzieStatics(t, e, henzie)
		if got := e.CommanderCastsFromCommandZone(0); got != 0 {
			t.Fatalf("setup: initial commander cast count = %d, want 0", got)
		}
		// Put Henzie on the battlefield the ordinary way: a plain hand cast
		// is NOT a command-zone cast, so the count must stay 0.
		addMana(t, e, 0, "BRG")
		submitChoices(t, e, castModeOption(t, e, henzie, ""))
		passUntilStackEmpty(t, e, 40)
		assertHenzieBoardPins(t, e, henzie, four, three)

		// Fund exactly the granted blitz cost {3}{G} (the card's own mana
		// cost, undiscounted at count 0). Both the granted blitz and the
		// ordinary printed-cost cast are offered at this pool.
		addMana(t, e, 0, "GCCC")
		if !hasModeOption(e, four, "blitzed") {
			t.Fatal("no (blitzed) cast option for the mana-value-4 creature with Henzie on the battlefield")
		}
		if !hasModeOption(e, four, "") {
			t.Fatal("ordinary printed-cost cast disappeared while the grant is active")
		}
		if hasModeOption(e, three, "blitzed") {
			t.Fatal("mana-value-3 creature received Henzie's Blitz grant")
		}
		submitChoices(t, e, castModeOption(t, e, four, "blitzed"))
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("undiscounted blitz payment left %d mana; want the full printed {3}{G} charged", got)
		}
		resolveBlitzCasted(t, e, four)
		replayCheck(t, e, cfg)

		// The ordinary cast stays a real, full-priced option: fund exactly
		// Blitz Three's printed {2}{G} and pay it -- no grant discount.
		addMana(t, e, 0, "GGC")
		if !hasModeOption(e, three, "") {
			t.Fatal("plain cast option for the mana-value-3 creature missing")
		}
		submitChoices(t, e, castModeOption(t, e, three, ""))
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("plain cast payment left %d mana; want the printed {2}{G} charged", got)
		}
		passUntilStackEmpty(t, e, 40)
		if o := e.G.Obj(three); o.Zone != state.ZBattlefield || o.CastFlags&state.FlagBlitzed != 0 {
			t.Fatalf("plain Blitz Three: zone=%s flags=%d, want battlefield with no FlagBlitzed", o.Zone, o.CastFlags)
		}
	})

	t.Run("one commander cast discounts the generic component", func(t *testing.T) {
		e, cfg, henzie, four, three := henzieBlitzGame(t, 9802, true, true)
		assertHenzieStatics(t, e, henzie)
		// Cast Henzie from the command zone so the discount count is backed
		// by a real command-zone cast, then return it to the battlefield.
		addMana(t, e, 0, "BRG")
		castCommanderAndReturn(t, e, henzie)
		e.emit(events.Event{Kind: events.MoveZone, Obj: henzie, From: state.ZCommand, To: state.ZBattlefield})
		e.pending = nil
		e.Advance()
		if got := e.CommanderCastsFromCommandZone(0); got != 1 {
			t.Fatalf("commander cast count = %d, want 1", got)
		}
		assertHenzieBoardPins(t, e, henzie, four, three)

		// The reduction is on the GENERIC component, never the colored pip:
		// with only {C}{C}{C} the pip-less shape {3} would be payable, so a
		// (blitzed) option there would prove the discount ate the {G}.
		addMana(t, e, 0, "CCC")
		if hasModeOption(e, four, "blitzed") {
			t.Fatal("blitz offered from a pool that cannot pay the {G} pip: the discount consumed a colored pip")
		}
		addMana(t, e, 0, "G")
		if !hasModeOption(e, four, "blitzed") {
			t.Fatal("no discounted (blitzed) option at {3}{G} minus one generic")
		}
		if hasModeOption(e, three, "blitzed") {
			t.Fatal("mana-value-3 creature received Henzie's Blitz grant")
		}
		poolBefore := e.G.Players[0].Pool.Total()
		submitChoices(t, e, castModeOption(t, e, four, "blitzed"))
		if got := e.G.Players[0].Pool.Total(); got != poolBefore-3 {
			t.Fatalf("discounted blitz payment charged %d of %d; want exactly {2}{G} (3)",
				poolBefore-got, poolBefore)
		}
		resolveBlitzCasted(t, e, four)
		replayCheck(t, e, cfg)
	})

	t.Run("no Henzie no grant", func(t *testing.T) {
		e, _, _, _, four := henzieBlitzGame(t, 9803, false, false)
		// Control precondition: the creature sits in hand and carries no
		// derived Blitz keyword without Henzie anywhere in play.
		if e.G.Obj(four).Zone != state.ZHand {
			t.Fatalf("setup: Blitz Four in %s, want hand", e.G.Obj(four).Zone)
		}
		if e.HasKeyword(four, "Blitz") {
			t.Fatal("setup: creature has Blitz without Henzie")
		}
		addMana(t, e, 0, "GGCC")
		if hasModeOption(e, four, "blitzed") {
			t.Fatal("Blitz offered without Henzie on the battlefield")
		}
		if !hasModeOption(e, four, "") {
			t.Fatal("control: the plain cast option itself is missing (vacuous control)")
		}
	})

	t.Run("the opponent's qualifying creature gets no grant", func(t *testing.T) {
		e, _, henzie, _, _ := henzieBlitzGame(t, 9804, false, true)
		addMana(t, e, 0, "BRG")
		submitChoices(t, e, castModeOption(t, e, henzie, ""))
		passUntilStackEmpty(t, e, 40)
		// Drive to seat 1's main phase with a mana-value-5 creature in hand
		// and more than enough mana: MV clears the cmcGE4 threshold, so only
		// the YouCtrl half of the Affected$ spec can withhold the grant.
		foe := findCardObj(t, e, 1, "Blitz Foe", state.ZHand)
		// Drive to seat 1's own main phase: MV clears the cmcGE4 threshold,
		// so only the YouCtrl half of the Affected$ spec can withhold the
		// grant. The generous pool rules out "could not afford it" as the
		// reason the blitz offer is missing.
		driveToStep(t, e, 2, 1, state.StepMain1)
		addMana(t, e, 1, "GGGGGG")
		if e.G.Active != 1 {
			t.Fatalf("setup: active seat %d, want 1", e.G.Active)
		}
		if hasModeOption(e, foe, "blitzed") {
			t.Fatal("the opponent's mana-value-5 creature received Henzie's Blitz grant")
		}
		if !hasModeOption(e, foe, "") {
			t.Fatal("setup: the opponent's plain cast option is missing (vacuous control)")
		}
	})
}
