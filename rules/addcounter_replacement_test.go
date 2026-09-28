package rules

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The counter-placement replacement class (repl:AddCounter / DB$ ReplaceCounter)
// pinned end to end on real corpus cards: Hardened Scales (that many plus one
// +1/+1 counters) and Branching Evolution (twice that many). The counter
// source is an authored fixture artifact with an AB$ PutCounter ability (never
// a corpus .txt, per the licensing rule), so every placement rides the
// engine's own effPutCounter emit path and the replacements intercept exactly
// where the live game's would.

// counterReplSource builds the authored counter-placing artifact: activate it
// and it puts n +1/+1 counters on the named target through effPutCounter's
// real CounterChange path.
func counterReplSource(t testing.TB, n int) *cards.Card {
	return card(t, "Name:Counter Source\nTypes:Artifact\n"+
		"A:AB$ PutCounter | Cost$ T | ValidTgts$ Creature | CounterType$ P1P1 | CounterNum$ "+
		strconv.Itoa(n)+" | SpellDescription$ Put counters on target creature.\n"+
		"Oracle:x\n")
}

// boardWithCounterReplacement seeds the named corpus replacement card and the
// authored counter source onto seat 0's battlefield, plus a target creature,
// and returns (engine, cfg, source, target).
func boardWithCounterReplacement(t *testing.T, seed uint64, replName string, counters int) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	repl := tokenReplCorpusCard(t, replName)
	src := counterReplSource(t, counters)
	target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, seed, repl, src, target)
	moveSeededCard(t, e, 0, repl, state.ZBattlefield)
	sourceID := moveSeededCard(t, e, 0, src, state.ZBattlefield)
	targetID := moveSeededCard(t, e, 0, target, state.ZBattlefield)
	return e, cfg, sourceID, targetID
}

// activateCounterSource drives a fresh priority round and submits the
// counter source's ability, targeting seat 0's own creature (the sole legal
// target) and draining the stack.
func activateCounterSource(t *testing.T, e *Engine, source, target state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, source, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision for the counter source: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == target {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("target %d not offerable: %+v", target, d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
}

// TestHardenedScalesAddsOneCounter is the filing card, end to end: a 1- and a
// 3-counter placement on a creature under Hardened Scales' controller becomes
// 2 and 4 (that many plus one), the real corpus R: line plus its
// ReplaceCount$CounterNum/Plus.1 body.
func TestHardenedScalesAddsOneCounter(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		placed, want int32
	}{
		{1, 2},
		{3, 4},
	} {
		e, cfg, source, target := boardWithCounterReplacement(t, 71, "Hardened Scales", int(tc.placed))
		activateCounterSource(t, e, source, target)
		if got := e.G.Obj(target).Counter("P1P1"); got != tc.want {
			t.Fatalf("Hardened Scales: %d placed -> %d counters, want %d", tc.placed, got, tc.want)
		}
		replayCheck(t, e, cfg)
	}
}

// TestBranchingEvolutionDoublesCounters pins the Twice body: a 1- and a
// 3-counter placement doubles to 2 and 6.
func TestBranchingEvolutionDoublesCounters(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		placed, want int32
	}{
		{1, 2},
		{3, 6},
	} {
		e, cfg, source, target := boardWithCounterReplacement(t, 73, "Branching Evolution", int(tc.placed))
		activateCounterSource(t, e, source, target)
		if got := e.G.Obj(target).Counter("P1P1"); got != tc.want {
			t.Fatalf("Branching Evolution: %d placed -> %d counters, want %d", tc.placed, got, tc.want)
		}
		replayCheck(t, e, cfg)
	}
}

// TestCounterReplacementDoesNotTouchRemoval is the negative direction: a
// CounterChange that REMOVES counters (a negative amount) is never an
// AddCounter event, so neither card changes it. The target's counters are
// placed BEFORE the replacement is on the battlefield, so the removal is the
// only event either card could see.
func TestCounterReplacementDoesNotTouchRemoval(t *testing.T) {
	t.Parallel()
	for _, replName := range []string{"Hardened Scales", "Branching Evolution"} {
		repl := tokenReplCorpusCard(t, replName)
		target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		e, cfg := tokenReplGame(t, 77, repl, target)
		targetID := moveSeededCard(t, e, 0, target, state.ZBattlefield)
		e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "P1P1", Amount: 5})
		moveSeededCard(t, e, 0, repl, state.ZBattlefield)
		e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "P1P1", Amount: -2})
		if got := e.G.Obj(targetID).Counter("P1P1"); got != 3 {
			t.Fatalf("%s: 2 removed from 5 = %d counters, want 3 (a removal is not an AddCounter event)", replName, got)
		}
		replayCheck(t, e, cfg)
	}
}

// TestCounterReplacementFailsClosedOnOtherCounterKinds pins the counter-kind
// gate: both cards name ValidCounterType$ P1P1, so an M1M1 placement is
// untouched (5 stays 5, never 6 or 10).
func TestCounterReplacementFailsClosedOnOtherCounterKinds(t *testing.T) {
	t.Parallel()
	for _, replName := range []string{"Hardened Scales", "Branching Evolution"} {
		e, cfg, _, target := boardWithCounterReplacement(t, 79, replName, 1)
		e.emit(events.Event{Kind: events.CounterChange, Obj: target, Counter: "M1M1", Amount: 5})
		if got := e.G.Obj(target).Counter("M1M1"); got != 5 {
			t.Fatalf("%s: 5 M1M1 counters became %d, want 5 (the replacement names P1P1)", replName, got)
		}
		replayCheck(t, e, cfg)
	}
}

// TestAddCounterReplacementsComposeScanOrder pins the CR 616.1e running
// total on a competition: Hardened Scales (+1) then Branching Evolution
// (double) turns a 1-counter placement into 4 (1 -> 2 -> 4), each modifier
// reading the amount the previous one produced.
func TestAddCounterReplacementsComposeScanOrder(t *testing.T) {
	t.Parallel()
	hs := tokenReplCorpusCard(t, "Hardened Scales")
	be := tokenReplCorpusCard(t, "Branching Evolution")
	src := counterReplSource(t, 1)
	target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, 83, hs, be, src, target)
	// Scan order is the battlefield order the MoveZone events create, so the
	// relative scan position of the two enchantments is deterministic but not
	// meaningful -- assert the COMMUTED final count is one of the two legal
	// scan orders' results (plus-then-double = 4, double-then-plus = 3).
	moveSeededCard(t, e, 0, hs, state.ZBattlefield)
	moveSeededCard(t, e, 0, be, state.ZBattlefield)
	sourceID := moveSeededCard(t, e, 0, src, state.ZBattlefield)
	targetID := moveSeededCard(t, e, 0, target, state.ZBattlefield)
	activateCounterSource(t, e, sourceID, targetID)
	got := e.G.Obj(targetID).Counter("P1P1")
	// Scan order is the battlefield order the MoveZone events create: hs was
	// moved first, so it is scanned first and the running total is
	// plus-then-double, 1 -> 2 -> 4. Pinned exactly (not a 3-or-4 union): a
	// union cannot tell plus-then-double from double-then-plus and would pass
	// for either order.
	if got != 4 {
		t.Fatalf("Hardened Scales + Branching Evolution (hs moved first) on a 1-counter placement = %d, want 4 (plus-then-double)", got)
	}
	replayCheck(t, e, cfg)
}

// TestWindingConstrictorPlayerCounters pins the PlayerCounterChange half of
// the class: Winding Constrictor's second R: line is ValidPlayer$ You with
// no ValidCard$, so a counter placed on the replacement's controller becomes
// that many plus one, while a counter on the opponent is untouched.
func TestWindingConstrictorPlayerCounters(t *testing.T) {
	t.Parallel()
	wc := tokenReplCorpusCard(t, "Winding Constrictor")
	e, cfg := tokenReplGame(t, 89, wc)
	moveSeededCard(t, e, 0, wc, state.ZBattlefield)
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: 3})
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 1, Counter: "ENERGY", Amount: 3})
	if got := e.G.Players[0].Counter("ENERGY"); got != 4 {
		t.Fatalf("Winding Constrictor: 3 ENERGY on its controller = %d, want 4", got)
	}
	if got := e.G.Players[1].Counter("ENERGY"); got != 3 {
		t.Fatalf("opponent's 3 ENERGY = %d, want 3 (ValidPlayer$ You)", got)
	}
	replayCheck(t, e, cfg)
}

// TestWindingConstrictorObjectCounters is the object-placement assertion the
// player-form gate needs: Winding Constrictor has BOTH a ValidCard$ object
// line and a ValidPlayer$ You line, and on an object CounterChange the
// ValidPlayer$ line must NOT fire (the object event's Player field is the
// zero value, so an ungated `ev.Player == you` read would collide with seat
// zero). A single +1/+1 counter on a creature it controls is that many plus
// one = 2, never the 3 two overlapping matches would give.
func TestWindingConstrictorObjectCounters(t *testing.T) {
	t.Parallel()
	wc := tokenReplCorpusCard(t, "Winding Constrictor")
	target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, 91, wc, target)
	moveSeededCard(t, e, 0, wc, state.ZBattlefield)
	targetID := moveSeededCard(t, e, 0, target, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "P1P1", Amount: 1})
	if got := e.G.Obj(targetID).Counter("P1P1"); got != 2 {
		t.Fatalf("Winding Constrictor: 1 P1P1 placed on a controlled creature = %d, want 2 (object line only; the ValidPlayer$ line must not fire on an object event)", got)
	}
	replayCheck(t, e, cfg)
}

// TestDoublingSeasonEffectOnlyIgnoresNonEffectPlacement pins the EffectOnly$
// gate the AddCounter matcher now reads. Doubling Season's oracle admits only
// placements that are the effect of a resolving spell or ability. A placement
// with NOTHING on the stack -- the turn-based-action shape (a Saga's lore
// counter, rules/saga.go advanceSagas) and the cost shape (a planeswalker's
// [+N] loyalty cost, rules/cast.go emitChoiceCosts) -- is not an effect, so
// the doubled result it must not produce is the exact regression here; the
// resolving-effect positive control follows.
func TestDoublingSeasonEffectOnlyIgnoresNonEffectPlacement(t *testing.T) {
	t.Parallel()
	ds := tokenReplCorpusCard(t, "Doubling Season")
	target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, 97, ds, target)
	moveSeededCard(t, e, 0, ds, state.ZBattlefield)
	targetID := moveSeededCard(t, e, 0, target, state.ZBattlefield)
	// Empty stack: a non-effect placement (the turn-based-action shape).
	e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "P1P1", Amount: 2})
	if got := e.G.Obj(targetID).Counter("P1P1"); got != 2 {
		t.Fatalf("Doubling Season doubled a non-effect placement: 2 -> %d, want 2 (EffectOnly$ excludes turn-based actions and costs)", got)
	}
	replayCheck(t, e, cfg)

	// Positive control: the SAME card with a REAL resolving ability (the
	// authored PutCounter source) still doubles, 1 -> 2.
	e2, cfg2, src, tgt := boardWithCounterReplacement(t, 99, "Doubling Season", 1)
	activateCounterSource(t, e2, src, tgt)
	if got := e2.G.Obj(tgt).Counter("P1P1"); got != 2 {
		t.Fatalf("Doubling Season on a resolving 1-counter effect = %d, want 2", got)
	}
	replayCheck(t, e2, cfg2)
}

// TestMeliraReplacementBodySubAbilityRunsTheLock pins that a ReplaceCounter
// body's SubAbility$ chain now RUNS: Melira, the Living Cure's body is
// `Amount$ 1 | SubAbility$ DBImmediateTrigger`, whose ImmediateTrigger
// resolves `DB$ Effect | StaticAbilities$ NoMorePoison`, a real
// `Mode$ CantPutCounter | ValidPlayer$ You | CounterType$ POISON` lock. Before
// cantputcounter1 the chain was dropped (one loud Note); now the lock is
// installed, so a SECOND poison source in the same turn places nothing. The
// amount rewrite itself still happens on the first source (3 -> 1). No loud
// rider Note may survive.
func TestMeliraReplacementBodySubAbilityRunsTheLock(t *testing.T) {
	t.Parallel()
	melira := tokenReplCorpusCard(t, "Melira, the Living Cure")
	e, cfg := tokenReplGame(t, 101, melira)
	moveSeededCard(t, e, 0, melira, state.ZBattlefield)
	before := len(e.L.Events)
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "POISON", Amount: 3})
	if got := e.G.Players[0].Counter("POISON"); got != 1 {
		t.Fatalf("Melira: 3 poison -> %d, want 1 (Amount$ 1)", got)
	}
	// The lock the rider installed must now swallow a SECOND source of MORE
	// than one counter -- a single-counter second source could pass by
	// coincidence.
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "POISON", Amount: 2})
	if got := e.G.Players[0].Counter("POISON"); got != 1 {
		t.Fatalf("Melira: a second source of 2 poison -> %d, want 1 (the lock: you can't get additional poison counters this turn)", got)
	}
	n := 0
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "replacement body SubAbility$") {
			n++
		}
	}
	if n != 0 {
		t.Fatalf("Melira: %d loud rider Notes survived, want 0 (the rider now runs)", n)
	}
	replayCheck(t, e, cfg)
}

func TestVizierOfRemediesReplacesToZero(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		placed, want int32
	}{
		{1, 0},
		{3, 2},
	} {
		vizier := tokenReplCorpusCard(t, "Vizier of Remedies")
		target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		e, cfg := tokenReplGame(t, 93, vizier, target)
		moveSeededCard(t, e, 0, vizier, state.ZBattlefield)
		targetID := moveSeededCard(t, e, 0, target, state.ZBattlefield)
		e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "M1M1", Amount: tc.placed})
		if got := e.G.Obj(targetID).Counter("M1M1"); got != tc.want {
			t.Fatalf("Vizier of Remedies: %d M1M1 placed -> %d counters, want %d (that many minus one)", tc.placed, got, tc.want)
		}
		replayCheck(t, e, cfg)
	}
}

// regenReplSource builds an authored artifact whose activated ability
// regenerates target creature through effRegenerate's real emit path, so the
// "Shield" marker is placed as the EFFECT of a resolving ability (which is
// what makes Doubling Season's EffectOnly$ gate pass, and therefore what
// makes this the load-bearing probe rather than a bare emit).
func regenReplSource(t testing.TB) *cards.Card {
	return card(t, "Name:Regen Source\nTypes:Artifact\n"+
		"A:AB$ Regenerate | Cost$ T | ValidTgts$ Creature | "+
		"SpellDescription$ Regenerate target creature.\n"+
		"Oracle:x\n")
}

// TestCounterDoublerIgnoresRegenerationShield is the marker gate: the engine
// records a regeneration shield as a positive-amount CounterChange named
// "Shield" for want of a status field, and a counter replacement whose R:
// line names no ValidCounterType$ (Doubling Season, Winding Constrictor's
// object line) matches ANY counter kind. Without state.InternalCounterMarker, one
// Regenerate would grant TWO shields -- a real wrong result, since
// rules/combat.go consumes one shield per destruction. The shield must stay
// at exactly 1 under either card.
func TestCounterDoublerIgnoresRegenerationShield(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		seed uint64
	}{
		{"Doubling Season", 103},
		{"Winding Constrictor", 105},
	} {
		repl := tokenReplCorpusCard(t, tc.name)
		src := regenReplSource(t)
		target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		e, cfg := tokenReplGame(t, tc.seed, repl, src, target)
		moveSeededCard(t, e, 0, repl, state.ZBattlefield)
		sourceID := moveSeededCard(t, e, 0, src, state.ZBattlefield)
		targetID := moveSeededCard(t, e, 0, target, state.ZBattlefield)
		activateCounterSource(t, e, sourceID, targetID)
		if got := e.G.Obj(targetID).Counter("Shield"); got != 1 {
			t.Fatalf("%s + one Regenerate = %d Shield markers, want 1 (a status marker is not a counter placement)", tc.name, got)
		}
		replayCheck(t, e, cfg)
	}
}

// TestCounterReplacementIgnoresDeathtouchedMarker is the same gate on the
// other marker: "Deathtouched" is set with Amount 1 by the combat and
// replacement damage paths and read by rules/sba.go's destruction check.
// Winding Constrictor's object line names no counter kind and carries no
// EffectOnly$, so a bare emit is the exact shape combat produces.
func TestCounterReplacementIgnoresDeathtouchedMarker(t *testing.T) {
	t.Parallel()
	wc := tokenReplCorpusCard(t, "Winding Constrictor")
	target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, 107, wc, target)
	moveSeededCard(t, e, 0, wc, state.ZBattlefield)
	targetID := moveSeededCard(t, e, 0, target, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "Deathtouched", Amount: 1})
	if got := e.G.Obj(targetID).Counter("Deathtouched"); got != 1 {
		t.Fatalf("Winding Constrictor + one Deathtouched mark = %d, want 1 (a status marker is not a counter placement)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCounterReplacementBodySubAbilityNoOpStillLocks pins the rider on a
// no-op rewrite: Melira's Amount$ 1 against a SINGLE poison counter leaves
// the count at 1, but the lock is installed just the same, so a second
// source the same turn still places nothing. No loud-rider Note may remain
// (the chain now runs).
func TestCounterReplacementBodySubAbilityNoOpStillLocks(t *testing.T) {
	t.Parallel()
	melira := tokenReplCorpusCard(t, "Melira, the Living Cure")
	e, cfg := tokenReplGame(t, 109, melira)
	moveSeededCard(t, e, 0, melira, state.ZBattlefield)
	before := len(e.L.Events)
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "POISON", Amount: 1})
	if got := e.G.Players[0].Counter("POISON"); got != 1 {
		t.Fatalf("Melira: 1 poison -> %d, want 1", got)
	}
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "POISON", Amount: 2})
	if got := e.G.Players[0].Counter("POISON"); got != 1 {
		t.Fatalf("Melira on a no-op rewrite: second source of 2 -> %d, want 1 (the lock still installs)", got)
	}
	n := 0
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "replacement body SubAbility$") {
			n++
		}
	}
	if n != 0 {
		t.Fatalf("Melira on a no-op rewrite: %d loud rider Notes, want 0", n)
	}
	replayCheck(t, e, cfg)
}
