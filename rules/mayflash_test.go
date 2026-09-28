package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// MayFlashCost (Forge's K:MayFlashCost, the "as though it had flash" casting
// option behind CR 702.8) is pinned end to end on the real filing card,
// Tegwyll's Scouring, whose keyword parameter is the tap cost
// `tapXType<3/Creature.withFlying/creatures with flying>`. The brief's premise
// is measured below: the keyword appears in exactly 11 corpus files (9 plain
// `K:MayFlashCost:2`, 1 Behold, and this one tap shape).

// mayflashFlyerSrc is a vanilla flier, the tap-cost candidate the filing
// card's filter admits.
const mayflashFlyerSrc = "Name:Flash Flyer\nTypes:Creature Faerie\nPT:1/1\nK:Flying\nOracle:x\n"

// mayflashNonFlyerSrc is a vanilla ground creature -- never a legal tap-cost
// candidate for Tegwyll's Scouring, so it must not be offered.
const mayflashNonFlyerSrc = "Name:Ground Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// mayflashCastOption returns the mayflash cast option for id, or (-1, false).
func mayflashCastOption(opts []decision.Option, id state.ObjID) (decision.Option, bool) {
	for _, o := range opts {
		if o.Kind == "cast" && o.Obj == id && o.Mode == "mayflash" {
			return o, true
		}
	}
	return decision.Option{}, false
}

// plainCastOptionExists reports whether the ordinary (Mode "") cast option
// for id is present. hasCastOption matches EVERY "cast" option, including
// the mayflash one, so the no-plain-cast assertions need this narrower test.
func plainCastOptionExists(opts []decision.Option, id state.ObjID) bool {
	for _, o := range opts {
		if o.Kind == "cast" && o.Obj == id && o.Mode == "" {
			return true
		}
	}
	return false
}

// TestTegwyllsScouringMayflashCastTapsThreeFlyers is the filing card's fix
// leaf: in a priority window that is NOT seat 0's main phase (so the
// sorcery's ordinary sorcery-speed timing fails), Tegwyll's Scouring is
// offered through its MayFlashCost permission, labelled "(may-flash)",
// casting it asks the tap-three-fliers cost, charges the printed {4}{B}{B},
// and resolves the DestroyAll. Before the fix the option did not exist at all
// (the sorcery was only castable at sorcery speed at full cost).
func TestTegwyllsScouringMayflashCastTapsThreeFlyers(t *testing.T) {
	t.Parallel()
	teg := corpusCardText(t, "t/tegwylls_scouring.txt")
	e, cfg, id := newFixtureDeck(t, 401, teg,
		mayflashFlyerSrc, mayflashFlyerSrc, mayflashFlyerSrc, mayflashFlyerSrc, mayflashNonFlyerSrc)
	if e.G.Active != 0 {
		t.Fatalf("fixture active seat %d, want 0", e.G.Active)
	}
	// Build the board through logged MoveZone events (moveSeeded), then stop
	// in the begin-combat step: seat 0 still has priority there, but it is
	// not a main phase, so spellTimingOK withholds the sorcery and only the
	// MayFlashCost permission can offer it -- the brief's "cast it any time
	// you could cast an instant".
	flyers := []state.ObjID{
		moveSeeded(t, e, 0, mayflashFlyerSrc, state.ZBattlefield),
		moveSeeded(t, e, 0, mayflashFlyerSrc, state.ZBattlefield),
		moveSeeded(t, e, 0, mayflashFlyerSrc, state.ZBattlefield),
		moveSeeded(t, e, 0, mayflashFlyerSrc, state.ZBattlefield),
	}
	bear := moveSeeded(t, e, 0, mayflashNonFlyerSrc, state.ZBattlefield)
	// moveSeeded clears the pending decision; driveToStep needs one to pass.
	e.askPriority(0)
	driveToStep(t, e, e.G.Turn, 0, state.StepBeginCombat)

	// The corpus keyword's parameter must parse to the tap cost (not withhold).
	extra, ok := mayflashExtraCost(e.G.Obj(id).Face())
	if !ok || len(extra.TapPermanent) != 1 {
		t.Fatalf("Tegwyll's MayFlashCost parsed to (%+v, %v), want one TapPermanent part", extra, ok)
	}

	// Fund the printed {4}{B}{B} with logged ManaAdd events (floating mana
	// empties across the steps above, so it must be added here).
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 2})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 4})
	e.pending = nil
	e.askPriority(0)
	opt, ok := mayflashCastOption(e.Pending().Options, id)
	if !ok {
		t.Fatalf("MayFlashCost did not offer a mayflash cast at instant timing: %+v", e.Pending().Options)
	}
	if plainCastOptionExists(e.Pending().Options, id) {
		t.Fatal("plain sorcery-speed cast offered off-main -- only the mayflash permission should exist")
	}
	submitChoices(t, e, opt.Index)

	// CR 601.2h: the tapXType<3/...> additional cost asks a KChoose over
	// exactly the flying creatures; the bear is ineligible. Four fliers make
	// the 3-of-4 a real choice (with exactly three eligible the engine taps
	// them silently, which the corpus's own shape would also do).
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 3 || d.Max != 3 {
		t.Fatalf("tap-cost decision = %+v, want KChoose Min=Max=3", d)
	}
	for _, o := range d.Options {
		if o.Obj == bear {
			t.Fatalf("non-flying bear offered to the flyers-only tap cost: %+v", d.Options)
		}
	}
	if len(d.Options) != 4 {
		t.Fatalf("tap-cost options = %d, want exactly the 4 flyers: %+v", len(d.Options), d.Options)
	}
	submitChoices(t, e, 0, 1, 2)

	// CR 601.2h: the additional cost taps the THREE CHOSEN flyers (options
	// 0,1,2 = flyers 0,1,2) and leaves the unchosen fourth untapped -- proof
	// the cost charged the choice. Read here, while the spell is still on the
	// stack: the DestroyAll's own resolution is what follows, and a permanent
	// leaving the battlefield is a new object whose Tapped is cleared.
	for i, f := range flyers {
		want := i < 3
		if got := e.G.Obj(f).Tapped; got != want {
			t.Fatalf("flyer %d tapped = %v, want %v (before the DestroyAll resolves)", i, got, want)
		}
	}

	// The cast resolves and the DestroyAll has swept every creature.
	passUntilStackEmpty(t, e, 30)
	for i, f := range flyers {
		if got := e.G.Obj(f).Zone; got != state.ZGraveyard {
			t.Fatalf("flyer %d zone = %s, want graveyard (Destroy all creatures resolved)", i, got)
		}
	}
	if got := e.G.Obj(bear); got.Zone != state.ZGraveyard {
		t.Fatalf("bear zone = %s, want graveyard (Destroy all creatures resolved)", got.Zone)
	}
	replayCheck(t, e, cfg)
}

// TestMayflashPlainCastUnaffectedAtSorceryTiming pins the no-regression half
// of the offer split: at NORMAL sorcery timing the mayflash branch is not
// entered (the ordinary cast is strictly cheaper), so the plain cast is
// offered exactly as before and no mayflash duplicate appears. The {2} shape
// (9 of the 11 corpus carriers) is used so the test does not depend on a tap
// cost.
func TestMayflashPlainCastUnaffectedAtSorceryTiming(t *testing.T) {
	t.Parallel()
	spell := card(t, "Name:Rout\nManaCost:2 R\nTypes:Sorcery\nK:MayFlashCost:2\nA:SP$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n")
	e := handEngine(t, spell)
	e.G.Players[0].Pool[state.MR] = 1
	e.G.Players[0].Pool[state.MC] = 2
	id := e.G.Zone(state.ZHand, 0)[0]
	if !plainCastOptionExists(e.legalActions(0), id) {
		t.Fatal("plain cast missing at sorcery timing")
	}
	if _, ok := mayflashCastOption(e.legalActions(0), id); ok {
		t.Fatal("mayflash offer duplicated the plain cast at sorcery timing")
	}
}

// TestMayflashWithheldWithoutKeyword is the fail-closed negative: a sorcery
// with no K:MayFlashCost line gets no mayflash option off-main.
func TestMayflashWithheldWithoutKeyword(t *testing.T) {
	t.Parallel()
	spell := card(t, "Name:Plain Sorcery\nManaCost:1 R\nTypes:Sorcery\nA:SP$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n")
	e := handEngine(t, spell)
	e.G.Active = 1
	e.G.Players[0].Pool[state.MR] = 1
	e.G.Players[0].Pool[state.MC] = 1
	id := e.G.Zone(state.ZHand, 0)[0]
	if _, ok := mayflashCastOption(e.legalActions(0), id); ok {
		t.Fatal("mayflash offered on a card with no MayFlashCost keyword")
	}
	if plainCastOptionExists(e.legalActions(0), id) {
		t.Fatal("plain sorcery cast offered off-turn")
	}
}

// TestMayflashExtraCostGrammar pins mayflashExtraCost directly: the printed
// {2} parameter parses, an empty parameter and a token the cost grammar
// reports Unknown both withhold (the replicate/bestow fail-closed
// convention), and the real Behold carrier parses to a Behold part.
func TestMayflashExtraCostGrammar(t *testing.T) {
	t.Parallel()
	two := card(t, "Name:MF Two\nManaCost:1 R\nTypes:Sorcery\nK:MayFlashCost:2\nOracle:x\n")
	if c, ok := mayflashExtraCost(two.Faces[0]); !ok || c.Generic != 2 {
		t.Fatalf("MayFlashCost:2 -> (%+v, %v), want Generic 2", c, ok)
	}
	none := card(t, "Name:MF None\nManaCost:1 R\nTypes:Sorcery\nOracle:x\n")
	if _, ok := mayflashExtraCost(none.Faces[0]); ok {
		t.Fatal("absent MayFlashCost must withhold")
	}
	empty := card(t, "Name:MF Empty\nManaCost:1 R\nTypes:Sorcery\nK:MayFlashCost:\nOracle:x\n")
	if _, ok := mayflashExtraCost(empty.Faces[0]); ok {
		t.Fatal("empty MayFlashCost parameter must withhold")
	}
	unknown := card(t, "Name:MF Unknown\nManaCost:1 R\nTypes:Sorcery\nK:MayFlashCost:LifeTotalHalfUp\nOracle:x\n")
	if c, ok := mayflashExtraCost(unknown.Faces[0]); ok {
		t.Fatalf("unmodelled token must withhold, got %+v", c)
	}
	behold := card(t, "Name:MF Behold\nManaCost:1 R\nTypes:Sorcery\nK:MayFlashCost:Behold<1/Dragon>\nOracle:x\n")
	if c, ok := mayflashExtraCost(behold.Faces[0]); !ok || len(c.Behold) != 1 {
		t.Fatalf("Behold<1/Dragon> -> (%+v, %v), want one Behold part", c, ok)
	}
}
