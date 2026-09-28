// morph_command_zone_test.go — the command-zone half of the Morph /
// Megamorph / Disguise face-down cast (CR 702.37a/702.168a/702.169a, CR
// 903.3d). The hand walk has always offered it (rules/morph_test.go); this
// file proves the command-zone walk offers the SAME option beside the
// printed cast, that casting it resolves face-down with the right family
// flag and spends exactly {3}, and that a second cast composes the CR 903.8
// commander tax on top of that {3} (offerCastable's tax composition must
// match beginCast's charge). No repo deck carries a morph carrier, so these
// tests do not move the golden heads or the botbench pin.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// morphCommanderEngine builds a two-seat Commander game whose seat 0
// commander is the named corpus card over Mountains, driving the CR 103.1
// toss to start seat 0 and parking at seat 0's Main1. It mirrors
// TestDashCommanderPaysTaxFromTheCommandZone's setup.
func morphCommanderEngine(t *testing.T, seed uint64, commander string) *Engine {
	t.Helper()
	reg := searchTestRegistry(t)
	c, ok := reg.Lookup(commander)
	if !ok {
		t.Fatalf("registry lacks %q", commander)
	}
	cfg := Config{Seed: seed, Names: []string{"a", "b"}, Format: FormatCommander,
		Decks: [][]*cards.Card{
			append([]*cards.Card{c}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		}, Commanders: [][]int{{0}, nil}, Tokens: reg.Tokens}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	return e
}

// assertFaceDownCommandCast is the shared assertion for a face-down
// command-zone cast that has just resolved: the object is on seat 0's
// battlefield face down with the named family flag set, and the pool holds
// exactly poolAfter mana (the {3} was spent).
func assertFaceDownCommandCast(t *testing.T, e *Engine, id state.ObjID, name string, flag uint64, poolAfter int32) {
	t.Helper()
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("%s: zone %s controller %d, want battlefield seat 0", name, o.Zone, o.Controller)
	}
	if !o.FaceDown {
		t.Fatalf("%s: FaceDown=false, want the face-down battlefield entry", name)
	}
	if o.CastFlags&flag == 0 {
		t.Fatalf("%s: CastFlags=%d lacks the family flag %d", name, o.CastFlags, flag)
	}
	if got := e.G.Players[0].Pool.Total(); got != poolAfter {
		t.Fatalf("%s: pool after face-down cast = %d, want %d (exactly the {3} spent)", name, got, poolAfter)
	}
}

// TestMorphFaceDownCastOfferedFromTheCommandZone proves Akroma, Angel of
// Fury (K:Morph) is offered its face-down cast beside its printed cast from
// the command zone, that the {3} spends and the morph family flag rides,
// and that a second command-zone cast composes the {2} commander tax on top
// of the {3}.
func TestMorphFaceDownCastOfferedFromTheCommandZone(t *testing.T) {
	t.Parallel()
	e := morphCommanderEngine(t, 941, "Akroma, Angel of Fury")
	id := e.G.Players[0].Commanders[0]
	// Precondition: the object the rule reads is in the command zone.
	if o := e.G.Obj(id); o.Zone != state.ZCommand {
		t.Fatalf("Akroma is in %s, want the command zone", o.Zone)
	}
	// The plain cast option must be present beside the face-down one: this
	// proves the face-down offer is an ADDITION, not a replacement. Fund
	// BOTH costs (Akroma's printed {5}{R}{R}{R} plus the face-down {3}).
	addMana(t, e, 0, "CCCCCCCCRRR")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	plain := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == "" {
			plain = o.Index
		}
	}
	if plain < 0 {
		t.Fatalf("the printed command-zone cast option for Akroma is missing: %+v", d.Options)
	}
	// castModeOption itself t.Fatalfs when the (morphed) option is absent —
	// this is the precondition the whole test turns on (/fails/ pre-fix).
	down := castModeOption(t, e, id, "morphed")
	submitChoices(t, e, down)
	passUntilStackEmpty(t, e, 40)
	assertFaceDownCommandCast(t, e, id, "Akroma, Angel of Fury", state.FlagMorphed, 8)

	// Return the commander to the command zone through the real accounting
	// event path. Empty the unused mana from the first cast via a logged
	// ManaClear so the second offer can be tested at exactly {3} and {5}.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZCommand})
	e.emit(events.Event{Kind: events.ManaClear, Player: 0})
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after clearing the first cast's remainder = %d, want 0", got)
	}
	if o := e.G.Obj(id); o.Zone != state.ZCommand {
		t.Fatalf("Akroma after return is in %s, want command zone", o.Zone)
	}
	if got := e.commanderTaxAmount(0, id); got != 2 {
		t.Fatalf("commander tax after one cast = %d, want 2", got)
	}
	addMana(t, e, 0, "CCC")
	for _, opt := range e.Pending().Options {
		if opt.Kind == "cast" && opt.Obj == id && opt.Mode == "morphed" {
			t.Fatal("taxed face-down offer present with only {3} in the pool")
		}
	}
	addMana(t, e, 0, "CC")
	submitChoices(t, e, castModeOption(t, e, id, "morphed"))
	passUntilStackEmpty(t, e, 40)
	if after := e.G.Players[0].Pool.Total(); after != 0 {
		t.Fatalf("second face-down command-zone cast left %d mana, want 0 ({3} + {2} tax)", after)
	}
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || !o.FaceDown {
		t.Fatalf("second cast: zone=%s faceDown=%v, want battlefield face down", o.Zone, o.FaceDown)
	}
}

// TestDisguiseFaceDownCastOfferedFromTheCommandZone proves Bayek of Siwa
// (K:Disguise) is offered its face-down cast from the command zone, that it
// resolves face-down with the disguise family flag and the cloak entry
// marker, and that {3} was spent. (No replayCheck: replay genesis drops
// commander state -- filed as .ds4/new-tickets/commander-replay-drops-commanders.md.)
func TestDisguiseFaceDownCastOfferedFromTheCommandZone(t *testing.T) {
	t.Parallel()
	e := morphCommanderEngine(t, 942, "Bayek of Siwa")
	id := e.G.Players[0].Commanders[0]
	if o := e.G.Obj(id); o.Zone != state.ZCommand {
		t.Fatalf("Bayek is in %s, want the command zone", o.Zone)
	}
	addMana(t, e, 0, "CCCCRW")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	plain := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == "" {
			plain = o.Index
		}
	}
	if plain < 0 {
		t.Fatalf("the printed command-zone cast option for Bayek is missing: %+v", d.Options)
	}
	submitChoices(t, e, castModeOption(t, e, id, "disguised"))
	passUntilStackEmpty(t, e, 40)
	assertFaceDownCommandCast(t, e, id, "Bayek of Siwa", state.FlagDisguised, 3)
	if !e.G.Obj(id).Cloaked {
		t.Fatal("disguised command-zone card Cloaked=false, want the cloak state bit")
	}
	// The cloak entry marker rode the PutOnStack and the battlefield entry.
	marked := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield {
			if ev.Counter != events.CloakEntryCounter {
				t.Fatalf("disguised battlefield entry Counter=%q, want %q", ev.Counter, events.CloakEntryCounter)
			}
			marked = true
		}
	}
	if !marked {
		t.Fatal("disguised command-zone cast recorded no battlefield entry")
	}
}
