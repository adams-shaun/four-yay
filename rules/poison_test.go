package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// api:Poison end to end, on the real corpus carriers (no Forge script text is
// committed here, per the licensing rule). Every test drives the REAL card
// through the engine's own offer/activation/cast flow, so the resolution
// reaches the effects.Resolve registry route -- a direct effPoison call would
// prove nothing about it. That is what makes these tests the registration
// revert probe: with Register("Poison", ...) removed each leaf fails on the
// folded counters AND on the generic "unimplemented API Poison" Note the
// unregistered route emits instead.

// poisonEventsSince slices the log since n0 down to the POISON
// PlayerCounterChange events (a placement and a removal both).
func poisonEventsSince(e *Engine, n0 int) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events[n0:] {
		if ev.Kind == events.PlayerCounterChange && ev.Counter == "POISON" {
			out = append(out, ev)
		}
	}
	return out
}

// noUnimplementedPoisonNote asserts the resolution produced the registered
// handler's route, not the generic unimplemented-API fallback Note.
func noUnimplementedPoisonNote(t *testing.T, e *Engine, n0 int) {
	t.Helper()
	for _, ev := range e.L.Events[n0:] {
		if ev.Kind == events.Note && ev.Text == "unimplemented API Poison" {
			t.Fatalf("generic unimplemented API Poison Note emitted: %+v", ev)
		}
	}
}

// vraskaUltGame seeds the real Vraska, Betrayal's Sting onto seat 0's
// battlefield with LOYALTY 9 (the printed 6 plus three added counters, so the
// [-9] ultimate is payable), drives to seat 0's Main 1 and returns the
// engine, config and Vraska's object id.
func vraskaUltGame(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	vraska := tokenReplCorpusCard(t, "Vraska, Betrayal's Sting")
	e, cfg := tokenReplGame(t, seed, vraska)
	id := moveSeededCard(t, e, 0, vraska, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "LOYALTY", Amount: 3})
	if got := e.G.Obj(id).Counter("LOYALTY"); got != 9 {
		t.Fatalf("precondition: Vraska loyalty = %d, want 9", got)
	}
	e.pending = nil
	e.priorityRound()
	return e, cfg, id
}

// activateVraskaUltimate finds the [-9] ability option (face ability index 2
// on a single-face card), submits it, answers its ValidTgts$ Player ask with
// `target` and drains the stack through resolution.
func activateVraskaUltimate(t *testing.T, e *Engine, id state.ObjID, target state.PlayerID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision to activate in: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id && o.Ability == 2 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Vraska [-9] not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after activation: %+v, want the target ask", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == target {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("seat %d not offered as the ultimate's target: %+v", target, d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 20)
}

// TestVraskaBetrayalsStingPoisonDifferential pins the differential end to
// end: the resolving SA reads SVar:X = TargetedPlayer$Counters.Poison for
// the printed LT9 gate, and Num$ Difference =
// Number$9/Minus.X for the amount, so a target at 3 poison gets exactly 6
// (reaching 9) and a target already at 9 gets none (the gate withholds the
// placement). The 6 (not 9) value is what proves the operand is LIVE --
// a coincidental fixed 9 or a dead zero would both fail this leaf.
func TestVraskaBetrayalsStingPoisonDifferential(t *testing.T) {
	t.Parallel()
	t.Run("targetAtThreePoisonGetsSix", func(t *testing.T) {
		e, cfg, id := vraskaUltGame(t, 9301)
		e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 1, Counter: "POISON", Amount: 3})
		if got := e.G.Players[1].Counter("POISON"); got != 3 {
			t.Fatalf("precondition: target poison = %d, want 3", got)
		}
		n0 := len(e.L.Events)
		activateVraskaUltimate(t, e, id, 1)
		noUnimplementedPoisonNote(t, e, n0)
		poison := poisonEventsSince(e, n0)
		if len(poison) != 1 || poison[0].Player != 1 || poison[0].Amount != 6 {
			t.Fatalf("poison events = %+v, want one Amount-6 placement on seat 1 (9 minus the targeted 3)", poison)
		}
		if got := e.G.Players[1].Counter("POISON"); got != 9 {
			t.Fatalf("target poison after the ultimate = %d, want 9", got)
		}
		if e.G.Players[1].Lost {
			t.Fatal("the target must survive at nine poison")
		}
		replayCheck(t, e, cfg)
	})
	t.Run("targetAtNineGetsNone", func(t *testing.T) {
		e, cfg, id := vraskaUltGame(t, 9302)
		e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 1, Counter: "POISON", Amount: 9})
		if got := e.G.Players[1].Counter("POISON"); got != 9 {
			t.Fatalf("precondition: target poison = %d, want 9", got)
		}
		n0 := len(e.L.Events)
		activateVraskaUltimate(t, e, id, 1)
		noUnimplementedPoisonNote(t, e, n0)
		if poison := poisonEventsSince(e, n0); len(poison) != 0 {
			t.Fatalf("poison events = %+v, want none (the printed LT9 gate holds)", poison)
		}
		if got := e.G.Players[1].Counter("POISON"); got != 9 {
			t.Fatalf("target poison after the ultimate = %d, want 9 (no placement)", got)
		}
		replayCheck(t, e, cfg)
	})
}

// TestLeechesPoisonRemovalMatchesPriorCount pins the inverse carrier end to
// end: Leeches' SP$ DealDamage reads NumDmg$ X =
// TargetedPlayer$Counters.Poison for the damage, and its DB$ Poison sub
// reads Num$ -X for the removal, so BOTH must see the same PRIOR count. The
// target starts at four (nonzero, non-one) and ends at zero, poisoned down
// through the signed event the fold clamps.
func TestLeechesPoisonRemovalMatchesPriorCount(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Leeches")
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 1, Counter: "POISON", Amount: 4})
	if got := e.G.Players[1].Counter("POISON"); got != 4 {
		t.Fatalf("precondition: target poison = %d, want 4 (nonzero, non-one)", got)
	}
	if e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: target life = %d, want 20", e.G.Players[1].Life)
	}
	addMana(t, e, 0, "WWW")
	id := searchMoveByName(t, e, "Leeches", state.ZHand)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Leeches zone = %v, want hand", o)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision to cast in: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Leeches not offered as a cast: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after casting: %+v, want the target ask", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("seat 1 not offered as Leeches' target: %+v", d.Options)
	}
	n0 := len(e.L.Events)
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 20)
	noUnimplementedPoisonNote(t, e, n0)
	poison := poisonEventsSince(e, n0)
	if len(poison) != 1 || poison[0].Player != 1 || poison[0].Amount != -4 {
		t.Fatalf("poison events = %+v, want one Amount -4 removal on seat 1 (the SIGNED Num$ -X)", poison)
	}
	if got := e.G.Players[1].Counter("POISON"); got != 0 {
		t.Fatalf("target poison after Leeches = %d, want 0 (the fold clamped the signed removal)", got)
	}
	if e.G.Players[1].Life != 16 {
		t.Fatalf("target life after Leeches = %d, want 16 (the damage read the same prior 4)", e.G.Players[1].Life)
	}
	replayCheck(t, e, cfg)
}

// TestPoisonSBAReachesTenLoses pins the SBA half through the resolution
// route: a resolved api:Poison placement (Prologue to Phyresis' Num$ 1) that
// takes its target from nine to ten must fire the existing poison-loss SBA.
// The placement rides the same PlayerCounterChange choke point a direct emit
// rides, so the SBA cannot be bypassed by the effect route.
func TestPoisonSBAReachesTenLoses(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Prologue to Phyresis")
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 1, Counter: "POISON", Amount: 9})
	if got := e.G.Players[1].Counter("POISON"); got != 9 {
		t.Fatalf("precondition: target poison = %d, want 9", got)
	}
	if e.G.Players[1].Lost {
		t.Fatal("precondition: nine poison must not already have lost the game")
	}
	addMana(t, e, 0, "UU")
	id := searchMoveByName(t, e, "Prologue to Phyresis", state.ZHand)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Prologue to Phyresis zone = %v, want hand", o)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision to cast in: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Prologue to Phyresis not offered as a cast: %+v", d.Options)
	}
	n0 := len(e.L.Events)
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	noUnimplementedPoisonNote(t, e, n0)
	if got := e.G.Players[1].Counter("POISON"); got != 10 {
		t.Fatalf("target poison after Prologue = %d, want 10", got)
	}
	if !e.G.Players[1].Lost {
		t.Fatal("ten poison must lose the game (the existing SBA, through the effect route)")
	}
	replayCheck(t, e, cfg)
}

// TestPoisonCounterProhibitionStillBinds casts real Prologue to Phyresis
// while its opponent controls real Phila, Unsealed. Prologue's positive
// literal Poison placement must be swallowed at the PlayerCounterChange
// choke point, proving the prohibition sees the resolving api route.
func TestPoisonCounterProhibitionStillBinds(t *testing.T) {
	t.Parallel()
	prologue := tokenReplCorpusCard(t, "Prologue to Phyresis")
	phila := tokenReplCorpusCard(t, "Phila, Unsealed")
	e, cfg := tokenReplGameSeats(t, 9305, []*cards.Card{prologue}, []*cards.Card{phila})
	pid := moveSeededCard(t, e, 1, phila, state.ZBattlefield)
	if o := e.G.Obj(pid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Phila zone = %v, want battlefield", o)
	}
	addMana(t, e, 0, "UU")
	id := searchMoveByName(t, e, "Prologue to Phyresis", state.ZHand)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Prologue zone = %v, want hand", o)
	}
	d := e.Pending()
	cast := -1
	if d != nil {
		for _, o := range d.Options {
			if o.Kind == "cast" && o.Obj == id {
				cast = o.Index
			}
		}
	}
	if cast < 0 {
		t.Fatalf("Prologue not offered as a cast: %+v", d)
	}
	n0 := len(e.L.Events)
	submitChoices(t, e, cast)
	passUntilStackEmpty(t, e, 20)
	noUnimplementedPoisonNote(t, e, n0)
	// The positive placement is swallowed at the choke point before logging,
	// leaving no POISON event and the protected opponent's count unchanged.
	if poison := poisonEventsSince(e, n0); len(poison) != 0 {
		t.Fatalf("poison events = %+v, want none (Phila's static swallowed Prologue's +1 placement)", poison)
	}
	if got := e.G.Players[1].Counter("POISON"); got != 0 {
		t.Fatalf("Phila controller poison after Prologue = %d, want 0 (Phila blocked its +1 placement)", got)
	}
	replayCheck(t, e, cfg)
}
