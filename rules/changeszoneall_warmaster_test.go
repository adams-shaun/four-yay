package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// Elvish Warmaster pins the report card (issue agent-20260918T211532Z-*-*
// family) end to end on the current real-corpus path. It is the sibling
// carrier rules/changeszoneall_test.go does not name: a ChangesZoneAll
// trigger whose ValidCards$ is a creature TYPE filter with the Other+YouCtrl
// predicates ("Whenever one or more OTHER Elves YOU CONTROL enter") and
// whose payoff is a TOKEN, not a draw or a counter.
//
// The card's real trigger line (verified live from the corpus, not
// committed here -- Forge scripts are GPL-3.0):
//
//	T:Mode$ ChangesZoneAll | ValidCards$ Elf.Other+YouCtrl |
//	    Destination$ Battlefield | TriggerZones$ Battlefield |
//	    ActivationLimit$ 1 | Execute$ TrigToken
//	SVar:TrigToken:DB$ Token | TokenScript$ g_1_1_elf_warrior |
//	    TokenOwner$ You
//
// What is pinned: the once-per-turn LATCH plus the card's filter/payoff. Two
// Elves entering (before/inside a zone bracket, see below) create exactly ONE
// 1/1 green Elf Warrior token, and a LATER qualifying Elf in the SAME turn
// creates no further token (ActivationLimit$ 1). The negative halves are
// pinned too: a NON-Elf entering and an OPPONENT's Elf entering each create
// nothing (the Elf type filter and YouCtrl), and the Warmaster's OWN entry is
// not "other Elves" (the Other predicate).
//
// Batch granularity is deliberately NOT claimed here. This fixture wraps the
// two Elf entries in engine.BeginZoneBatch/EndZoneBatch, but for a
// limit-carrying trigger that bracket is UNOBSERVABLE: ChangesZoneAll is in
// actionTriggerModes, so the per-turn ActivationLimit$ 1 read gate
// (triggerActivationLimitAllows, rules/trigger_match.go) runs at queue time
// and latches the second Elf BEFORE the ChangesZoneAll batch dedup/
// accumulation block is ever reached. Measured: with the bracket and with
// two plain sequential zallEnter moves the result is identical --
// pendingTriggers==1 and the queued Ctx.Remembered/Captured have length 1 in
// both (the second Elf never accumulates into a batch's moved set). So this
// test verifies the once-per-turn latch, the Elf.Other+YouCtrl filters and
// the token payoff; it does NOT exercise a simultaneous batch boundary. The
// grouped-batch semantics for an UNLIMITED ChangesZoneAll carrier are
// rules/zone_table_batch_test.go's job (RepeatEach ChangeZoneTable$ True).

// warmasterTrigger asserts the real compiled card carries the exact
// trigger line the token assertions ride on. A silent scan (or a corpus
// change) cannot make the counts below vacuous if this precondition holds.
func warmasterTrigger(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("Elvish Warmaster object %d missing a face", id)
	}
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Elvish Warmaster in %s, want Battlefield", o.Zone)
	}
	f := o.Face()
	if len(f.Triggers) == 0 {
		t.Fatalf("precondition: Elvish Warmaster has no trigger, want one ChangesZoneAll line")
	}
	tr := f.Triggers[0]
	if tr.Mode != "ChangesZoneAll" {
		t.Fatalf("precondition: Elvish Warmaster trigger mode = %q, want ChangesZoneAll", tr.Mode)
	}
	if got := tr.Params["ValidCards"]; got != "Elf.Other+YouCtrl" {
		t.Fatalf("precondition: Elvish Warmaster ValidCards = %q, want Elf.Other+YouCtrl", got)
	}
	if got := tr.Params["Destination"]; got != "Battlefield" {
		t.Fatalf("precondition: Elvish Warmaster Destination = %q, want Battlefield", got)
	}
	if got := tr.Params["ActivationLimit"]; got != "1" {
		t.Fatalf("precondition: Elvish Warmaster ActivationLimit = %q, want 1", got)
	}
	if got := tr.Params["TriggerZones"]; got != "Battlefield" {
		t.Fatalf("precondition: Elvish Warmaster TriggerZones = %q, want Battlefield", got)
	}
}

// TestElvishWarmasterTokenOncePerTurn is the reported card end to end. The
// fixture leaves Elvish Warmaster, two Llanowar Elves (Creature Elf Druid),
// an extra Elf and a Grizzly Bears in seat 0's opening hand/library, and a
// Sol Ring in seat 1's, all real corpus cards.
func TestElvishWarmasterTokenOncePerTurn(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := zallEngine(t, reg,
		[]string{"Elvish Warmaster", "Llanowar Elves", "Llanowar Elves", "Llanowar Elves", "Grizzly Bears"},
		[]string{"Llanowar Elves", "Forest"})

	// Enter the Warmaster itself first. Its OWN entry is not "other Elves",
	// so it must queue nothing; settle it before measuring.
	warmaster := zallEnter(t, e, 0, "Elvish Warmaster")
	zallDrain(t, e)
	warmasterTrigger(t, e, warmaster)
	if n := countTokensNamedOnSeat(t, e, 0, "Elf Warrior Token"); n != 0 {
		t.Fatalf("the Warmaster's own entry created %d token(s), want 0 (Other)", n)
	}

	// (1) LATCH on the first qualifying entry: two Elves entering queue
	// exactly one trigger and create exactly one token. The zone bracket here
	// is not what produces the single trigger -- ActivationLimit$ 1 does: the
	// queue-time read gate (triggerActivationLimitAllows) latches the second
	// Elf before the ChangesZoneAll batch-accumulation block runs, so the
	// bracket is unobservable for this card (see the header comment). It is
	// kept only so the fixture also runs the batch bracket harmlessly.
	e.BeginZoneBatch()
	elfA := zallEnter(t, e, 0, "Llanowar Elves")
	elfB := zallEnter(t, e, 0, "Llanowar Elves")
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("two Elves entering queued %d trigger(s), want 1 (ActivationLimit$ 1 latch)", len(e.pendingTriggers))
	}
	e.EndZoneBatch()
	if got := e.G.Obj(elfA).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition: first Llanowar Elves in %s, want Battlefield", got)
	}
	if got := e.G.Obj(elfB).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition: second Llanowar Elves in %s, want Battlefield", got)
	}
	zallDrain(t, e)
	if n := countTokensNamedOnSeat(t, e, 0, "Elf Warrior Token"); n != 1 {
		t.Fatalf("two Elves entering after the latch created %d Elf Warrior token(s), want exactly 1", n)
	}
	warmasterToken(t, e, 0)

	// (2) LATCH persists: a later qualifying Elf entering in the SAME turn
	// creates no further token (ActivationLimit$ 1).
	zallEnter(t, e, 0, "Llanowar Elves")
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("ActivationLimit$ 1 did not latch: %d trigger(s) queued on a later Elf entry", len(e.pendingTriggers))
	}
	zallDrain(t, e)
	if n := countTokensNamedOnSeat(t, e, 0, "Elf Warrior Token"); n != 1 {
		t.Fatalf("after the later Elf entry seat 0 holds %d Elf Warrior token(s), want still 1", n)
	}

	// (3) the type filter: a NON-Elf entering creates nothing. (Grizzly Bears
	// is a Bear, so it is not in ValidCards$ Elf.)
	bear := zallEnter(t, e, 0, "Grizzly Bears")
	zallDrain(t, e)
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("a non-Elf entering queued %d trigger(s), want 0", len(e.pendingTriggers))
	}
	if n := countTokensNamedOnSeat(t, e, 0, "Elf Warrior Token"); n != 1 {
		t.Fatalf("after a non-Elf entry seat 0 holds %d Elf Warrior token(s), want still 1", n)
	}
	if got := e.G.Obj(bear).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition: Grizzly Bears in %s, want Battlefield", got)
	}

	// (4) the YouCtrl predicate: a fresh engine, seat 1's Elf entering
	// creates nothing for seat 0.
	e2, _ := zallEngine(t, reg,
		[]string{"Elvish Warmaster"},
		[]string{"Llanowar Elves", "Forest"})
	warmaster2 := zallEnter(t, e2, 0, "Elvish Warmaster")
	zallDrain(t, e2)
	warmasterTrigger(t, e2, warmaster2)
	oppElf := zallEnter(t, e2, 1, "Llanowar Elves")
	zallDrain(t, e2)
	if len(e2.pendingTriggers) != 0 {
		t.Fatalf("an opponent's Elf entering queued %d trigger(s), want 0", len(e2.pendingTriggers))
	}
	if n := countTokensNamedOnSeat(t, e2, 0, "Elf Warrior Token"); n != 0 {
		t.Fatalf("an opponent's Elf entry created %d Elf Warrior token(s) for seat 0, want 0 (YouCtrl)", n)
	}
	if got := e2.G.Obj(oppElf).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition: opponent Llanowar Elves in %s, want Battlefield", got)
	}
}

// warmasterToken returns the single Elf Warrior token on seat p's
// battlefield and asserts its printed characteristics, so a wrongly shaped
// token (a different script, a non-creature) fails rather than passing a
// bare name count.
func warmasterToken(t *testing.T, e *Engine, p state.PlayerID) *state.Object {
	t.Helper()
	var tok *state.Object
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == "Elf Warrior Token" {
			tok = o
			break
		}
	}
	if tok == nil {
		t.Fatal("no Elf Warrior Token object on seat 0's battlefield")
	}
	f := tok.Face()
	if f.PT != "1/1" {
		t.Fatalf("token PT = %q, want 1/1", f.PT)
	}
	var isCreature, isElf, isWarrior bool
	for _, ty := range f.Types {
		switch ty {
		case "Creature":
			isCreature = true
		case "Elf":
			isElf = true
		case "Warrior":
			isWarrior = true
		}
	}
	if !isCreature || !isElf || !isWarrior {
		t.Fatalf("token types = %v, want Creature Elf Warrior", f.Types)
	}
	return tok
}
