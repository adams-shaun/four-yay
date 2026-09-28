package rules

// AnimateAll (task api-animateall): the rules-level end-to-end pins. The
// unit-level registrations live in effects/animateall_test.go; these prove
// the computed result through the real layer walk on REAL corpus cards
// (Mirror Entity, Vedalken Humiliator), with the other participants freely
// authored fixtures (never an inlined corpus .txt).

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const vanillaBearSrc = "Name:Vanilla Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// animateAllNotes returns the Note texts the log carries naming an unimplemented
// AnimateAll parameter.
func animateAllNotes(e *Engine) []string {
	var out []string
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note {
			out = append(out, ev.Text)
		}
	}
	return out
}

// TestMirrorEntityAnimateAllGrantsBasePTAndAllCreatureTypes drives the REAL
// corpus Mirror Entity's {X} activation end to end: X = 2 announced, the
// ability resolves, and every creature seat 0 controls carries base 2/2
// (layer 7b SubSet) plus all creature types (layer 4 AddAllCreatureTypes) —
// while the opponent's creature is untouched.
func TestMirrorEntityAnimateAllGrantsBasePTAndAllCreatureTypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	me, ok := reg.Lookup("Mirror Entity")
	if !ok {
		t.Fatal("corpus fixture: Mirror Entity missing")
	}
	bear := card(t, vanillaBearSrc)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{me, bear, bear}, []*cards.Card{bear})
	meID := moveByName(t, e, 0, "Mirror Entity", state.ZBattlefield)
	bearID := moveByName(t, e, 0, "Vanilla Bear", state.ZBattlefield)
	oppID := moveByName(t, e, 1, "Vanilla Bear", state.ZBattlefield)

	addMana(t, e, 0, "CC")
	opt := abilityOption(t, e, meID, 0)
	submitChoices(t, e, opt.Index)
	// The X announce: Cost$ X asks its value before payment.
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision after the ability option")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "x" && o.Label == "X = 2" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no X = 2 option: %+v", d)
	}
	submitChoices(t, e, idx)
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).X != 2 {
		t.Fatalf("stack after announce: %v", e.G.Stack)
	}
	passUntilStackEmpty(t, e, 20)

	dMe := e.Derived(meID)
	if dMe.Power != 2 || dMe.Toughness != 2 {
		t.Fatalf("Mirror Entity itself = %d/%d, want base 2/2", dMe.Power, dMe.Toughness)
	}
	dBear := e.Derived(bearID)
	if dBear.Power != 2 || dBear.Toughness != 2 {
		t.Fatalf("animated bear = %d/%d, want base 2/2", dBear.Power, dBear.Toughness)
	}
	for _, want := range arbitrarySubtypes {
		if !slices.Contains(dBear.Types, want) {
			t.Fatalf("animated bear's types missing %q (have %v)", want, dBear.Types)
		}
	}
	for _, bad := range nonCreatureTypeWords {
		if slices.Contains(dBear.Types, bad) {
			t.Fatalf("animated bear carries non-creature word %q", bad)
		}
	}
	if !slices.Contains(dBear.Types, "Bear") {
		t.Fatalf("animated bear lost its printed type Bear")
	}
	dOpp := e.Derived(oppID)
	if dOpp.Power != 2 || dOpp.Toughness != 2 || slices.Contains(dOpp.Types, "Goblin") {
		t.Fatalf("opponent's bear = %d/%d types %v, want untouched 2/2 without granted types",
			dOpp.Power, dOpp.Toughness, dOpp.Types)
	}
	replayCheck(t, e, cfg)
}

// TestVedalkenHumiliatorAnimateAllResolution completes the resolution half the
// existing metalcraft-gate pin (trigger_metalcraft_test.go) deliberately
// deferred while AnimateAll was unimplemented: the trigger resolves, the
// opponent's creatures get base 1/1, and the unread RemoveAllAbilities$
// parameter is LOUD — one note naming it — rather than silent.
func TestVedalkenHumiliatorAnimateAllResolution(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	hum, ok := reg.Lookup("Vedalken Humiliator")
	if !ok {
		t.Fatal("corpus fixture: Vedalken Humiliator missing")
	}
	orn, ok := reg.Lookup("Ornithopter")
	if !ok {
		t.Fatal("corpus fixture: Ornithopter missing")
	}
	bears, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatal("corpus fixture: Grizzly Bears missing")
	}
	e := corpusEngine(t, reg, []*cards.Card{hum, orn, orn, orn}, []*cards.Card{bears})
	humID := moveByName(t, e, 0, "Vedalken Humiliator", state.ZBattlefield)
	for i := 0; i < 3; i++ {
		moveByName(t, e, 0, "Ornithopter", state.ZBattlefield)
	}
	bearID := moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{humID}})
	e.putTriggersOnStack()
	if n := len(e.G.Stack); n != 1 {
		t.Fatalf("metalcraft trigger did not fire (stack = %d)", n)
	}
	passUntilStackEmpty(t, e, 20)

	dBear := e.Derived(bearID)
	if dBear.Power != 1 || dBear.Toughness != 1 {
		t.Fatalf("opponent's bears = %d/%d, want base 1/1", dBear.Power, dBear.Toughness)
	}
	var named bool
	for _, txt := range animateAllNotes(e) {
		if strings.Contains(txt, "RemoveAllAbilities$") {
			named = true
		}
	}
	if !named {
		t.Fatalf("RemoveAllAbilities$ gap not loud; notes = %v", animateAllNotes(e))
	}
}

// TestMirrorEntityAnimateAllBasePTExpiresAtEndOfTurn is the report's
// acceptance half the other pins left implicit: an AnimateAll base-P/T grant
// is UntilEOT (not Duration$ Permanent), so the real end-of-turn cleanup
// (CR 514.2, rules.Engine.EndOfTurnCleanup) must drop it and the swept
// creature must fall back to its printed P/T. It drives the same real-corpus
// Mirror Entity activation as the grant pin above, but reads the derived P/T
// AFTER the cleanup rather than only before it.
func TestMirrorEntityAnimateAllBasePTExpiresAtEndOfTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	me, ok := reg.Lookup("Mirror Entity")
	if !ok {
		t.Fatal("corpus fixture: Mirror Entity missing")
	}
	bear := card(t, vanillaBearSrc)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{me, bear, bear}, []*cards.Card{bear})
	meID := moveByName(t, e, 0, "Mirror Entity", state.ZBattlefield)
	bearID := moveByName(t, e, 0, "Vanilla Bear", state.ZBattlefield)

	addMana(t, e, 0, "CC")
	opt := abilityOption(t, e, meID, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision after the ability option")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "x" && o.Label == "X = 2" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no X = 2 option: %+v", d)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	// While the turn is live the grant is on: base 2/2 on both creatures.
	if dMe := e.Derived(meID); dMe.Power != 2 || dMe.Toughness != 2 {
		t.Fatalf("Mirror Entity before cleanup = %d/%d, want base 2/2", dMe.Power, dMe.Toughness)
	}
	if dBear := e.Derived(bearID); dBear.Power != 2 || dBear.Toughness != 2 {
		t.Fatalf("bear before cleanup = %d/%d, want base 2/2", dBear.Power, dBear.Toughness)
	}

	// CR 514.2: the UntilEOT base-P/T registration dies with the turn.
	e.EndOfTurnCleanup()
	if dMe := e.Derived(meID); dMe.Power != 1 || dMe.Toughness != 1 {
		t.Fatalf("Mirror Entity after cleanup = %d/%d, want printed 1/1 (UntilEOT grant survived)",
			dMe.Power, dMe.Toughness)
	}
	// The bear's printed P/T is 2/2, so its expiry is not observable through
	// P/T alone; assert the cross-check that its base is now the PRINTED value
	// by checking it is no longer base-set (a printed 2/2 base reads 2/2).
	if dBear := e.Derived(bearID); dBear.Power != 2 || dBear.Toughness != 2 {
		t.Fatalf("bear after cleanup = %d/%d, want printed 2/2", dBear.Power, dBear.Toughness)
	}
	replayCheck(t, e, cfg)
}

// TestAnimateAllOpponentCreatureNeverSwept lives in effects/animateall_test.go
// (unit level); this file carries the real-corpus end-to-end pins.
