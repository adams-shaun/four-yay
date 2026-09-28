package rules

// CR 702.165 Backup: "When this creature enters, put N +1/+1 counters on
// target creature. If that's another creature, it gains the following
// abilities until end of turn." cards/kw_backup.go expands the printed
// `K:Backup:<N>:<SVar>` into a ChangesZone self-entry trigger whose body is a
// TARGETED PutCounter chained to a grant built from the named SVar, streamed
// through its ordinary `DB$ Animate`/`DB$ Pump` parsing and gated on the
// target being another creature.
//
// Guardian Scalelord is the brief's pinned real-corpus carrier: its
// `SVar:BackupAbilities` prints `DB$ Animate | Keywords$ Flying | Triggers$
// AttackTrig | sVars$ AE`, so one ETB observable covers the counter, the
// keyword grant and the granted-trigger grant.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// backupConfig seeds a 2-seat game whose seat 0 carries one Guardian Scalelord
// and one Memnite plus Mountains, drives to the first Main 1 (seatZeroStart
// pins seat 0 as the turn-1 active seat) and bridges the Scalelord into hand
// while Memnite sits on the battlefield as the backup target.
func backupConfig(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, state.PlayerID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	scalelord := mustCorpusCard(t, reg, "Guardian Scalelord")
	memnite := mustCorpusCard(t, reg, "Memnite")
	for _, c := range []*cards.Card{scalelord, memnite} {
		if d := c.Link(); len(d) != 0 {
			t.Fatalf("link %s: %v", c.Faces[0].Name, d)
		}
	}
	// Preconditions the whole test rests on: the carrier really prints Backup
	// (the expansion, not a hand-built SVar) and Memnite really is a creature
	// another-permanent target.
	if !scalelord.Faces[0].HasKeyword("Backup") {
		t.Fatal("Guardian Scalelord does not print Backup in the corpus")
	}
	if scalelord.Faces[0].Power() != 3 || scalelord.Faces[0].Toughness() != 4 {
		t.Fatalf("Guardian Scalelord is %d/%d, want 3/4", scalelord.Faces[0].Power(), scalelord.Faces[0].Toughness())
	}
	seatDeck := func() []*cards.Card {
		return append([]*cards.Card{scalelord, memnite}, mountainDeck(t, 38)...)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{seatDeck(), seatDeck()}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	caster := e.G.Active
	sid := findByName(e, "Guardian Scalelord", caster)
	if sid == 0 {
		t.Fatal("Guardian Scalelord not found for the active seat -- corpus missing?")
	}
	mid := findByName(e, "Memnite", caster)
	if mid == 0 {
		t.Fatal("Memnite not found for the active seat -- corpus missing?")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: sid, From: e.G.Obj(sid).Zone, To: state.ZHand})
	e.emit(events.Event{Kind: events.MoveZone, Obj: mid, From: e.G.Obj(mid).Zone, To: state.ZBattlefield})
	e.pending = nil
	e.priorityRound()
	return e, cfg, sid, mid, caster
}

// backupAnswerTarget drains priority until the Backup trigger's target ask
// appears, asserts want is offered, submits it and reports the offer set so a
// caller can prove which creatures the keyword could choose.
func backupAnswerTarget(t *testing.T, e *Engine, want state.ObjID, limit int) map[state.ObjID]bool {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision while draining Backup (stack %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KTarget:
			offered := map[state.ObjID]bool{}
			idx := -1
			for _, o := range d.Options {
				if o.Obj != 0 {
					offered[o.Obj] = true
				}
				if o.Obj == want {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("Backup did not offer target %d; options: %+v", want, d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit Backup target: %v", err)
			}
			return offered
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		default:
			t.Fatalf("unexpected decision %+v while draining Backup", d)
		}
	}
	t.Fatalf("Backup target ask never appeared within %d steps", limit)
	return nil
}

// TestGuardianScalelordBackupCountersAndCopiesAbilities is the brief's core
// leg: a cast Guardian Scalelord's ETB puts one +1/+1 counter on another
// target creature AND grants it the card's copied rules text (Flying),
// leaving the Scalelord itself untouched.
func TestGuardianScalelordBackupCountersAndCopiesAbilities(t *testing.T) {
	e, cfg, sid, mid, caster := backupConfig(t, 501)
	addMana(t, e, caster, "WWWWW") // {4}{W}
	submitChoices(t, e, castOptionFor(t, e, sid).Index)
	offered := backupAnswerTarget(t, e, mid, 30)
	// The Scalelord is a legal target of its own trigger (CR 702.165), so both
	// creatures are offered; what matters is that the OTHER creature is.
	if !offered[mid] {
		t.Fatalf("Backup did not offer the other creature (%d)", mid)
	}
	passUntilStackEmpty(t, e, 30)

	// Precondition: the cast actually resolved the Scalelord onto the
	// battlefield, so the ETB really happened.
	if got := e.G.Obj(sid).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition failed: Guardian Scalelord zone=%s, want battlefield", got)
	}
	mem := e.G.Obj(mid)
	if mem.Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: target Memnite zone=%s, want battlefield", mem.Zone)
	}
	if got := mem.Counter("P1P1"); got != 1 {
		t.Fatalf("Backup 1 put %d +1/+1 counters on the target, want 1", got)
	}
	if e.Power(mid) != 2 || e.Toughness(mid) != 2 {
		t.Fatalf("target after Backup 1 is %d/%d, want 2/2 (printed 1/1 plus one counter)", e.Power(mid), e.Toughness(mid))
	}
	// The copied rules text: Guardian Scalelord prints Flying, so the target
	// must gain it until end of turn.
	if !e.HasKeyword(mid, "Flying") {
		t.Fatal("Backup did not copy the source's printed Flying onto the target")
	}
	// The source itself was NOT the target, so it got neither the counter nor
	// a duplicated ability grant.
	if got := e.G.Obj(sid).Counter("P1P1"); got != 0 {
		t.Fatalf("Backup wrongly countered the source: %d counters, want 0", got)
	}
	replayCheck(t, e, cfg)
}

// TestGuardianScalelordBackupCopiesThePrintedTrigger proves the other half of
// the copied rules text: the grant is not just a keyword. Memnite prints no
// attack trigger at all, so the `Triggers$ AttackTrig` the Scalelord's
// `SVar:BackupAbilities` carries can only fire from the grant. Declaring the
// backed-up Memnite as an attacker must queue exactly one `Mode$ Attacks`
// trigger.
func TestGuardianScalelordBackupCopiesThePrintedTrigger(t *testing.T) {
	e, cfg, sid, mid, caster := backupConfig(t, 503)
	if len(e.G.Obj(mid).Face().Triggers) != 0 {
		t.Fatalf("precondition failed: Memnite prints %d triggers, want 0 so the queued trigger can only be the grant", len(e.G.Obj(mid).Face().Triggers))
	}
	addMana(t, e, caster, "WWWWW")
	submitChoices(t, e, castOptionFor(t, e, sid).Index)
	backupAnswerTarget(t, e, mid, 30)
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(mid).Counter("P1P1"); got != 1 {
		t.Fatalf("precondition failed: target got %d counters, want 1", got)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("precondition failed: %d triggers pending after the entry settled", len(e.pendingTriggers))
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1 - caster, IDs: []state.ObjID{mid}})
	if got := len(e.pendingTriggers); got != 1 {
		t.Fatalf("the granted rules text queued %d attack triggers on the target, want 1 (the copied AttackTrig)", got)
	}
	replayCheck(t, e, cfg)
}

// TestGuardianScalelordBackupSelfTargetGrantsNothing pins CR 702.165's "if
// that's ANOTHER creature" restriction: targeting the entering Scalelord
// itself puts the counter but must NOT grant a second copy of its abilities.
// The observable is the number of triggers the attack declaration queues: the
// Scalelord prints one `Mode$ Attacks` trigger, so a duplicated grant would
// queue two. The count is read straight off the engine's pending queue after
// the declaration, before the (possibly targetless) triggers reach the stack,
// so it measures the grant rather than the printed trigger's own target
// legality.
func TestGuardianScalelordBackupSelfTargetGrantsNothing(t *testing.T) {
	e, cfg, sid, _, caster := backupConfig(t, 502)
	addMana(t, e, caster, "WWWWW")
	submitChoices(t, e, castOptionFor(t, e, sid).Index)
	backupAnswerTarget(t, e, sid, 30) // target the source itself
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(sid).Counter("P1P1"); got != 1 {
		t.Fatalf("Backup self-target put %d +1/+1 counters, want 1", got)
	}
	// Precondition: nothing is still queued from the entry, so the attack
	// declaration's own queue is unambiguous.
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("precondition failed: %d triggers still pending after the entry settled", len(e.pendingTriggers))
	}

	// Declare the Scalelord as an attacker directly (the mentor/training
	// fixture licence: no summoning-sickness state is consulted) and read the
	// queued trigger count before any of them resolve.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1 - caster, IDs: []state.ObjID{sid}})
	if got := len(e.pendingTriggers); got != 1 {
		t.Fatalf("Backup self-target queued %d attack triggers, want exactly 1 (the printed trigger); the grant must not duplicate the source's own abilities", got)
	}
	replayCheck(t, e, cfg)
}
