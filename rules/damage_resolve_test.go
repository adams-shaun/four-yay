package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The DamageMap$ True -> DB$ DamageResolve primitive (Forge's mark-then-resolve
// damage pattern): a marked DealDamage defers its damage, and the flush deals
// every mark inside ONE damage batch. These tests pin the whole cast end to end
// on real corpus carriers, pin that an unmarked call in the same chain keeps
// dealing immediately, and pin the motivating observable -- a DamageDealtOnce
// ("deals damage one or more times in one batch") trigger latches ONCE across a
// flush that carries more than one mark.

// damageBatchWatcher draws a card for its controller once per damage batch
// dealt by an instant or sorcery. One flush batch carrying two marks fires it
// once; per-mark batches fire it once per mark.
const damageBatchWatcher = "Name:Batch Watcher\nTypes:Creature\nPT:1/1\n" +
	"T:Mode$ DamageDealtOnce | ValidSource$ Instant,Sorcery | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ Whenever one or more permanents or players are dealt damage by an instant or sorcery in one batch, draw a card.\n" +
	"SVar:TrigDraw:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n"

// batchWatcherDealt is a freely-authored, DURABLE creature that survives the
// marked damage (so its marked-damage total is observable after the flush).
const batchWatcherDealt = "Name:Target Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/9\nOracle:x\n"

func countDrawsFor(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// hasUnimplementedResolveNote reports whether the log carries the loud
// "unimplemented API DamageResolve" Note a missing registration emits. Every
// test here asserts its absence, so a build with the registration reverted
// fails loudly instead of silently passing.
func hasUnimplementedResolveNote(e *Engine, since int) bool {
	for i := since; i < len(e.L.Events); i++ {
		ev := e.L.Events[i]
		if ev.Kind == events.Note && ev.Text == "unimplemented API DamageResolve" {
			return true
		}
	}
	return false
}

// optionForObj returns the option index naming obj, or -1.
func optionForObj(d *decision.Decision, obj state.ObjID) int {
	for _, o := range d.Options {
		if o.Obj == obj {
			return o.Index
		}
	}
	return -1
}

// optionForPlayer returns the option index naming player p, or -1.
func optionForPlayer(d *decision.Decision, p state.PlayerID) int {
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == p {
			return o.Index
		}
	}
	return -1
}

// castTwoTargetDamageSpell casts id (a spell whose root targets a creature and
// whose pre-asked sub targets a player) funding from the pool, answers BOTH
// target asks (creature = victim, then the opponent player) while passing the
// priority rounds the engine poses between them, and drains. Returns the
// pre-existing log length so callers count only what the cast did.
func castTwoTargetDamageSpell(t *testing.T, e *Engine, id, victim state.ObjID) int {
	t.Helper()
	start := len(e.L.Events)
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the fixture: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// Answer the target asks (root creature, then the player/planeswalker sub)
	// and pass the priority rounds between them, until the stack is empty.
	answeredCreature, answeredPlayer := false, false
	for i := 0; i < 40 && !e.G.Over; i++ {
		d = e.Pending()
		if d == nil || len(e.G.Stack) == 0 {
			break
		}
		switch d.Kind {
		case decision.KTarget, decision.KChoose:
			if objIdx := optionForObj(d, victim); objIdx >= 0 && !answeredCreature {
				answeredCreature = true
				submitChoices(t, e, objIdx)
				continue
			}
			if plIdx := optionForPlayer(d, 1); plIdx >= 0 {
				answeredPlayer = true
				submitChoices(t, e, plIdx)
				continue
			}
			t.Fatalf("unanswerable target ask: %+v", d.Options)
		case decision.KPriority:
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if pass < 0 {
				t.Fatalf("no pass option: %+v", d.Options)
			}
			submitChoices(t, e, pass)
		default:
			t.Fatalf("unexpected decision while casting the damage spell: %+v", d)
		}
	}
	if !answeredCreature || !answeredPlayer {
		t.Fatalf("precondition: creature ask answered=%v, player ask answered=%v, want both", answeredCreature, answeredPlayer)
	}
	passUntilStackEmpty(t, e, 40)
	drainPopPending(t, e)
	return start
}

// damageInLogSince reports the total amount of Damage events logged at or after
// start, and how many such events there were.
func damageInLogSince(e *Engine, start int) (int32, int) {
	var total int32
	n := 0
	for i := start; i < len(e.L.Events); i++ {
		if e.L.Events[i].Kind == events.Damage {
			total += e.L.Events[i].Amount
			n++
		}
	}
	return total, n
}

// TestCunningStrikeDefersItsMarkedHalf is the real-corpus end-to-end carrier:
// Cunning Strike marks 2 to a creature (the root, the only DamageMap$ True
// call) and its DB1 deals 2 to a player immediately -- an UNMARKED call, which
// keeps today's behaviour byte for byte -- then DBDamageResolve flushes the
// mark and DBDraw still runs. It also proves the loud fallback note is gone.
func TestCunningStrikeDefersItsMarkedHalf(t *testing.T) {
	src := corpusCardText(t, "c/cunning_strike.txt")
	e, cfg, id := newFixtureDeck(t, 53, src)
	bear := putToken(t, e, 1, batchWatcherDealt, state.ZBattlefield)
	addMana(t, e, 0, "UUURR")
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: victim zone = %v, want battlefield", o)
	}
	theirLife := e.G.Players[1].Life

	start := castTwoTargetDamageSpell(t, e, id, bear)

	if got := e.G.Obj(bear).Damage; got != 2 {
		t.Fatalf("victim marked damage = %d, want 2 (the root mark flushed)", got)
	}
	if got := theirLife - e.G.Players[1].Life; got != 2 {
		t.Fatalf("opponent life loss = %d, want 2 (the unmarked DB1 dealt immediately)", got)
	}
	if total, _ := damageInLogSince(e, start); total != 4 {
		t.Fatalf("total damage logged = %d, want 4 (2 marked + 2 immediate)", total)
	}
	// DBDraw after the flush: the caster's fixture resolved to the graveyard.
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("Cunning Strike zone = %s, want graveyard (it resolved)", got)
	}
	if hasUnimplementedResolveNote(e, start) {
		t.Fatal("the log carries the 'unimplemented API DamageResolve' note; the primitive is not registered")
	}
	replayCheck(t, e, cfg)
}

// TestLungeDefersItsMarkedHalf is the second real-corpus carrier: Lunge marks 2
// to a creature (root) and deals 2 to a player immediately (unmarked DB1),
// then a bare DamageResolve flushes the mark. Both halves land, no note.
func TestLungeDefersItsMarkedHalf(t *testing.T) {
	src := corpusCardText(t, "l/lunge.txt")
	e, cfg, id := newFixtureDeck(t, 54, src)
	bear := putToken(t, e, 1, batchWatcherDealt, state.ZBattlefield)
	addMana(t, e, 0, "RRR")
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: victim zone = %v, want battlefield", o)
	}
	theirLife := e.G.Players[1].Life

	start := castTwoTargetDamageSpell(t, e, id, bear)

	if got := e.G.Obj(bear).Damage; got != 2 {
		t.Fatalf("victim marked damage = %d, want 2", got)
	}
	if got := theirLife - e.G.Players[1].Life; got != 2 {
		t.Fatalf("opponent life loss = %d, want 2", got)
	}
	if total, _ := damageInLogSince(e, start); total != 4 {
		t.Fatalf("total damage logged = %d, want 4", total)
	}
	if hasUnimplementedResolveNote(e, start) {
		t.Fatal("the log carries the 'unimplemented API DamageResolve' note; the primitive is not registered")
	}
	replayCheck(t, e, cfg)
}

// TestDamageResolveFlushesAllMarksInOneBatch is the motivating observable: a
// chain whose EVERY damage call is marked (two DealDamage halves, both
// DamageMap$ True) flushes both marks inside one damage batch, so a
// DamageDealtOnce watcher latches exactly ONCE. With per-mark batches it would
// fire twice. The card text is freely authored (no corpus .txt) and mirrors the
// Lunge shape with the second half marked too.
func TestDamageResolveFlushesAllMarksInOneBatch(t *testing.T) {
	src := "Name:Doubled Lash\nManaCost:R R\nTypes:Sorcery\n" +
		"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 2 | DamageMap$ True | SubAbility$ Second\n" +
		"SVar:Second:DB$ DealDamage | Defined$ Player.Opponent | NumDmg$ 2 | DamageMap$ True | SubAbility$ Flush\n" +
		"SVar:Flush:DB$ DamageResolve\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 56, src)
	target := putToken(t, e, 0, batchWatcherDealt, state.ZBattlefield)
	watcher := putToken(t, e, 0, damageBatchWatcher, state.ZBattlefield)
	addMana(t, e, 0, "RR")
	// Precondition: the watcher is a live battlefield permanent on the caster's
	// side (so its draw lands in the caster's hand and is countable) and the
	// victim is a DIFFERENT durable creature.
	if o := e.G.Obj(watcher); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: watcher = %+v, want a battlefield permanent controlled by seat 0", o)
	}
	if target == watcher || e.G.Obj(target) == nil || e.G.Obj(target).Zone != state.ZBattlefield {
		t.Fatalf("precondition: target must be its own live battlefield creature (target=%d watcher=%d)", target, watcher)
	}
	myDraws := countDrawsFor(e, 0)

	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// Root target ask: the durable watcher.
	for i := 0; i < 40 && !e.G.Over; i++ {
		d = e.Pending()
		if d == nil || len(e.G.Stack) == 0 {
			break
		}
		switch d.Kind {
		case decision.KTarget, decision.KChoose:
			if oi := optionForObj(d, target); oi >= 0 {
				submitChoices(t, e, oi)
				continue
			}
			t.Fatalf("the durable target is not offered as the root target: %+v", d.Options)
		case decision.KPriority:
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if pass < 0 {
				t.Fatalf("no pass option: %+v", d.Options)
			}
			submitChoices(t, e, pass)
		default:
			t.Fatalf("unexpected decision: %+v", d)
		}
	}
	passUntilStackEmpty(t, e, 40)
	drainPopPending(t, e)

	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("opponent life = %d, want 18 (the marked 2 flushed)", got)
	}
	if got := countDrawsFor(e, 0) - myDraws; got != 1 {
		t.Fatalf("batch watcher drew %d cards, want 1 (both marks flushed in ONE batch)", got)
	}
	replayCheck(t, e, cfg)
}

// TestDamageMapMarksSurviveAMidChainSuspension pins the cross-suspension ride:
// a chain that MARKS damage, then suspends on a mid-resolution ask (a modal
// Charm), then continues to its DamageResolve must still flush the marks the
// rebuilt Ctx was owed. Without the ride the resumed Ctx starts with no marks
// and the flush silently deals nothing.
func TestDamageMapMarksSurviveAMidChainSuspension(t *testing.T) {
	t.Parallel()
	src := "Name:Deferred Lash\nManaCost:R\nTypes:Sorcery\n" +
		"A:SP$ DealDamage | Defined$ Player.Opponent | NumDmg$ 2 | DamageMap$ True | SubAbility$ Modes\n" +
		"SVar:Modes:DB$ Charm | Choices$ M1,M2 | SubAbility$ Flush\n" +
		"SVar:M1:DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Gain 5 life\n" +
		"SVar:M2:DB$ LoseLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Lose 5 life\n" +
		"SVar:Flush:DB$ DamageResolve\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 55, src)
	addMana(t, e, 0, "R")
	theirLife := e.G.Players[1].Life
	myLife := e.G.Players[0].Life

	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// Pass priority; the chain marks the damage and then SUSPENDS on the modal
	// ask mid-resolution.
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("mid-resolution modal ask = %+v, want KModes (the suspension)", d)
	}
	if o := e.G.Obj(id); o.Zone != state.ZStack {
		t.Fatalf("precondition: the spell must be suspended on the stack, zone %s", o.Zone)
	}
	// The marked damage must NOT have landed yet: the mark deferred it, and the
	// flush that pays it is still ahead of the suspension. This is the half a
	// lost mark (or an immediate deal) would break.
	if e.G.Players[1].Life != theirLife {
		t.Fatalf("opponent life before the suspension = %d, want %d (the mark must defer)", e.G.Players[1].Life, theirLife)
	}
	// Choose the harmless mode so the assertion on the opponent's life is
	// about the flushed damage alone.
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 40)

	if got := theirLife - e.G.Players[1].Life; got != 2 {
		t.Fatalf("opponent life loss after the resumed flush = %d, want 2 (the mark survived the suspension)", got)
	}
	if e.G.Players[0].Life != myLife+5 {
		t.Fatalf("caster life = %d, want %d (the chosen mode ran)", e.G.Players[0].Life, myLife+5)
	}
	replayCheck(t, e, cfg)
}
