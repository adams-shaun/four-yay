package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Regressions for the cardfuzz batch5 livelocks (all real corpus cards).

// b5Engine deals seat 0 the named corpus cards on top of a Plains deck and
// seat 1 all Plains, seat 0 to start, and walks to seat 0's first main phase.
func b5Engine(t *testing.T, names ...string) (*Engine, Config) {
	t.Helper()
	reg := searchTestRegistry(t)
	plains := searchCorpusCard(t, reg, "Plains")
	var deck0 []*cards.Card
	for _, n := range names {
		deck0 = append(deck0, searchCorpusCard(t, reg, n))
	}
	for len(deck0) < 40 {
		deck0 = append(deck0, plains)
	}
	deck1 := make([]*cards.Card, 40)
	for i := range deck1 {
		deck1[i] = plains
	}
	cfg := seatZeroStart(Config{Seed: 7402, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// gainedAbilityOffered reports whether a priority decision offers a GAINED
// (GainsAbilitiesOf$) activation of obj.
func gainedAbilityOffered(d *decision.Decision, obj state.ObjID) bool {
	if d == nil {
		return false
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == obj && o.GainedSource != 0 {
			return true
		}
	}
	return false
}

// TestGainedEquipNotOfferedWithoutAnotherCreature (cardfuzz batch5 line 1):
// Trazyn the Infinite has the activated abilities of the artifact cards in
// its owner's graveyard, so a Bonesplitter there gives it "Equip {1}". An
// attach ability can never target its own source (CR 701.3a; targetAsk
// excludes it), so with Trazyn the only creature the Equip has no legal
// target and must not be offered -- before the fix the offer gate let the
// source count as its own target, the ask found none and aborted, and the
// bot re-picked the same option forever ("cast aborted: no legal target").
// A second creature makes the Equip legal again, and a held-out (F05-2
// suppressed) source withholds the gained activation like a printed one.
func TestGainedEquipNotOfferedWithoutAnotherCreature(t *testing.T) {
	t.Parallel()
	e, _ := b5Engine(t, "Trazyn the Infinite", "Bonesplitter", "Grizzly Bears")
	searchMoveByName(t, e, "Bonesplitter", state.ZGraveyard)
	trazyn := searchMoveByName(t, e, "Trazyn the Infinite", state.ZBattlefield)
	addMana(t, e, 0, "WW")
	if gainedAbilityOffered(e.Pending(), trazyn) {
		t.Fatal("Trazyn's gained Equip was offered with no other creature to attach to")
	}
	searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	if !gainedAbilityOffered(e.Pending(), trazyn) {
		t.Fatalf("Trazyn's gained Equip was not offered once another creature exists: %+v", e.Pending().Options)
	}
	e.suppressedCast = map[state.ObjID]bool{trazyn: true}
	e.pending = nil
	e.priorityRound()
	if gainedAbilityOffered(e.Pending(), trazyn) {
		t.Fatal("a held-out (suppressed) source still offered its gained activation")
	}
}

// TestMavindaExilesTheRecastSpell (cardfuzz batch5 line 7): Mavinda's
// Effect lets its controller cast a graveyard instant/sorcery this turn and
// carries the ReplacementEffects$ promise "if that spell would be put into
// your graveyard, exile it instead". The promise used to be a Note, so a {0}
// Indicate returned to the graveyard while the MayPlay grant still named it
// and was recast forever inside one turn. The Effect-created Moved redirect
// is now registered: the recast spell rests in exile and is not offered
// again.
func TestMavindaExilesTheRecastSpell(t *testing.T) {
	t.Parallel()
	e, cfg := b5Engine(t, "Mavinda, Students' Advocate", "Indicate")
	ind := searchMoveByName(t, e, "Indicate", state.ZGraveyard)
	mav := searchMoveByName(t, e, "Mavinda, Students' Advocate", state.ZBattlefield)
	pick := func(kind string, obj state.ObjID) {
		t.Helper()
		d := e.Pending()
		for _, o := range d.Options {
			if o.Kind == kind && o.Obj == obj {
				submitChoices(t, e, o.Index)
				return
			}
		}
		t.Fatalf("no %s option for %d in %+v", kind, obj, d.Options)
	}
	pick("ability", mav)
	submitChoices(t, e, 0) // Mavinda's target: the graveyard Indicate
	for e.Pending().Kind == decision.KPriority && len(e.G.Stack) > 0 {
		submitPass(t, e)
	}
	pick("cast", ind)
	submitChoices(t, e, 0) // Indicate's target: Mavinda
	for e.Pending().Kind == decision.KPriority && len(e.G.Stack) > 0 {
		submitPass(t, e)
	}
	if z := e.G.Obj(ind).Zone; z != state.ZExile {
		t.Fatalf("recast Indicate rests in %v, want exile (Mavinda's replacement)", z)
	}
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == ind {
			t.Fatal("exiled Indicate is still offered for recasting")
		}
	}
	replayCheck(t, e, cfg)
}

// TestYawgmothsWillExilesCardsPutIntoTheGraveyard (cardfuzz batch5 line
// 12): Gaea's Will's and Yawgmoth's Will's (identical Effect bodies) "if a
// card would be put into your graveyard from anywhere this turn, exile it
// instead" is the same Effect-created Moved -> exile redirect (the Hidden$
// True body). Without it, Urza's Bauble was recast from the graveyard,
// sacrificed back into it, and recast until the intent cap. Yawgmoth's Will
// carries a castable mana cost where Gaea's Will is suspend-only, so it is
// the fixture.
func TestYawgmothsWillExilesCardsPutIntoTheGraveyard(t *testing.T) {
	t.Parallel()
	e, cfg := b5Engine(t, "Yawgmoth's Will", "Urza's Bauble")
	will := searchMoveByName(t, e, "Yawgmoth's Will", state.ZHand)
	bauble := searchMoveByName(t, e, "Urza's Bauble", state.ZGraveyard)
	addMana(t, e, 0, "BBB")
	castObj(t, e, will)
	for e.Pending().Kind == decision.KPriority && len(e.G.Stack) > 0 {
		submitPass(t, e)
	}
	if z := e.G.Obj(will).Zone; z != state.ZExile {
		// CR 608.2n: the spell is put into the graveyard as the FINAL step of
		// its own resolution, after its effect exists, so the Will exiles
		// itself.
		t.Fatalf("Yawgmoth's Will rests in %v, want exile", z)
	}
	castObj(t, e, bauble)
	for e.Pending().Kind == decision.KPriority && len(e.G.Stack) > 0 {
		submitPass(t, e)
	}
	if z := e.G.Obj(bauble).Zone; z != state.ZBattlefield {
		t.Fatalf("Urza's Bauble cast from the graveyard rests in %v, want battlefield", z)
	}
	// Sacrifice it through its own ability: it must be exiled, not binned.
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == bauble {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Urza's Bauble ability not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		submitChoices(t, e, 0)
	}
	if z := e.G.Obj(bauble).Zone; z != state.ZExile {
		t.Fatalf("sacrificed Urza's Bauble rests in %v, want exile (Yawgmoth's Will)", z)
	}
	replayCheck(t, e, cfg)
}

// TestCombatDamageReplacementBodyAskDoesNotRedealThePass (cardfuzz batch5
// line 10): Phyrexian Vindicator blocks and its damage replacement competes
// with Statecraft's, so its controller orders them; picking Vindicator runs
// its body, whose immediate "deals that much damage to any other target"
// asks for a target. That ask is not a replacement-order ask, so the pass
// deals its remaining assignments under it -- and, once answered, the
// Advance loop re-entered combatStep, which began the SAME damage pass again
// (re-dealing the Bears' damage, re-asking the order, forever). The pass now
// completes exactly once and combat moves on.
func TestCombatDamageReplacementBodyAskDoesNotRedealThePass(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := combatEngine(t)
	vind := onBoardCard(t, e, 1, searchCorpusCard(t, reg, "Phyrexian Vindicator"))
	onBoardCard(t, e, 1, searchCorpusCard(t, reg, "Statecraft"))
	att := onBoardReadyCard(t, e, 0, searchCorpusCard(t, reg, "Grizzly Bears"))
	e.askAttackers()
	submitAttackersOnly(t, e, att)
	drainCombatPriority(t, e)
	submitBlockersOnly(t, e, vind)
	drainCombatPriority(t, e)
	clone := e.Clone()
	for _, eng := range []*Engine{e, clone} {
		orderAsks := 0
		for i := 0; i < 12 && eng.G.Step == state.StepCombatDamage; i++ {
			d := eng.Pending()
			if d == nil {
				t.Fatal("no decision pending in the combat damage step")
			}
			if d.Kind == decision.KReplacement {
				orderAsks++
			}
			if d.Kind == decision.KPriority {
				submitPass(t, eng)
				continue
			}
			submitChoices(t, eng, 0)
		}
		if eng.G.Step == state.StepCombatDamage {
			t.Fatal("the combat damage step never finished (the pass is re-dealt)")
		}
		if orderAsks != 1 {
			t.Fatalf("replacement-order asks = %d, want exactly 1 (one damage pass)", orderAsks)
		}
		if o := eng.G.Obj(vind); o == nil || o.Zone != state.ZBattlefield || o.Damage != 0 {
			t.Fatalf("Vindicator after combat = %+v, want on the battlefield with its damage prevented", o)
		}
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the combat damage pass")
	}
}

// TestLivelockWatcherIgnoresClockTickRuns (cardfuzz batch5 line 4): a single
// resolution that registers one continuous effect per affected permanent
// (a PumpAll over hundreds of creatures -- Moogles' Valor late in a
// token-doubling game) emits one objectless ClockTick per registration. That
// run is not a loop, so the period detector must not trip on it; the runaway
// backstop still bounds a genuine effect-registration loop.
func TestLivelockWatcherIgnoresClockTickRuns(t *testing.T) {
	t.Parallel()
	e := livelockTestEngine(t, &LoopGuard{CycleEvents: 30, MaxPeriod: 8, RunawayEvents: 5000})
	if lle := drivePattern(t, e, func(int) events.Event {
		return events.Event{Kind: events.ClockTick}
	}, 1000); lle != nil {
		t.Fatalf("watcher fired on a ClockTick run: %v", lle)
	}
	// A real loop that also ticks the clock is still caught by the period
	// detector (the ticks are elided, the rest repeats).
	e = livelockTestEngine(t, &LoopGuard{CycleEvents: 30, MaxPeriod: 8, RunawayEvents: 5000})
	lle := drivePattern(t, e, func(i int) events.Event {
		if i%2 == 0 {
			return events.Event{Kind: events.ClockTick}
		}
		return events.Event{Kind: events.Note, Obj: 42, Text: "loop"}
	}, 1000)
	if lle == nil || lle.Reason != "repeating cycle" || lle.Object != 42 {
		t.Fatalf("interleaved loop diagnostic = %v, want a repeating cycle on object 42", lle)
	}
	// A loop of nothing but ClockTicks is still bounded by the backstop.
	e = livelockTestEngine(t, &LoopGuard{CycleEvents: 30, MaxPeriod: 8, RunawayEvents: 500})
	if lle := drivePattern(t, e, func(int) events.Event {
		return events.Event{Kind: events.ClockTick}
	}, 1000); lle == nil || lle.Reason != "runaway resolution" {
		t.Fatalf("pure ClockTick loop diagnostic = %v, want runaway resolution", lle)
	}
}

// TestPumpAllOverAHugeBoardIsNotALivelock is the end-to-end half of the
// ClockTick carve-out: Overrun over 450 creatures registers 900 continuous
// effects inside one resolution and must resolve.
func TestPumpAllOverAHugeBoardIsNotALivelock(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := combatEngine(t)
	var last state.ObjID
	for i := 0; i < 450; i++ {
		last = onBoard(t, e, 0, "Name:Token Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	}
	over := e.G.AddObject(searchCorpusCard(t, reg, "Overrun"), 0)
	c := &effects.Ctx{Controller: 0, Source: over.ID, SVars: over.Face().SVars}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Overrun over 450 creatures panicked: %v", r)
		}
	}()
	effects.Resolve(e, c, over.Face().SpellAbility())
	if got := e.Power(last); got != 5 {
		t.Fatalf("pumped bear power = %d, want 5", got)
	}
}
