package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the two halves the shared block-legality oracle must enforce
// for a DERIVED can't-block keyword, in rules/statics.go's blockRestricted via
// rules/combat.go's canBlock:
//
//  1. both corpus spellings of Forge's textual grant reach the oracle -- the
//     HIDDEN-prefixed `KW$ HIDDEN CARDNAME can't block.` (Concussive Bolt) and
//     the bare `KW$ CARDNAME can't block.` (Unearthly Blizzard/Incite
//     Hysteria/Siegebreaker Giant); and
//  2. the restriction is a real declare-blockers outcome (the pair is not
//     offered; below the condition it IS offered and accepted), and it expires
//     with its until-end-of-turn duration.

// cantBlockKeywordLine returns the raw derived keyword that means "can't
// block", normalising the optional HIDDEN marker away, so a bare-spelling
// grant and a HIDDEN one compare equal.
func cantBlockKeywordLine(t *testing.T, e *Engine, id state.ObjID) (raw string, found bool) {
	t.Helper()
	for _, k := range e.Derived(id).Keywords {
		head := strings.TrimSpace(strings.TrimPrefix(cardsKeywordHead(k), "HIDDEN "))
		if strings.EqualFold(head, "CARDNAME can't block.") {
			return k, true
		}
	}
	return "", false
}

// boltAttacker places the authored attacker on seat 1's battlefield (the
// active player's zone askBlockers lists attackers from), makes it
// attack-ready, declares it attacking seat 0, and parks the game in the
// declare-blockers step. It returns the attacker id.
func boltAttacker(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	attacker := onBoardReady(t, e, 1, metalcraftAttacker)
	e.G.Active = 1
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{attacker}})
	if o := e.G.Obj(attacker); !o.IsAttacking || o.Attacking != 0 {
		t.Fatalf("precondition: attacker did not declare against seat 0: %+v", o)
	}
	e.G.Step = state.StepDeclareBlockers
	return attacker
}

// blockControl places an unrestricted authored blocker on seat 0's
// battlefield. A board whose ONLY blocker is restricted makes askBlockers
// pose no decision at all (it records the forced empty declaration), which
// would conflate the restriction with an empty board; the control blocker
// keeps the decision alive so the restricted pair's ABSENCE is observable.
func blockControl(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	return onBoard(t, e, 0, "Name:Bolt Control Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
}

// openBlockAsk re-opens the declare-blockers decision from scratch (the
// blockerRound is one-shot) and returns it.
func openBlockAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	e.blockerRound = blockerRound{}
	e.askBlockers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	return d
}

// castConcussiveBolt builds the real-corpus Concussive Bolt fixture (the
// chosen number of artifacts under seat 0) and resolves the Bolt at seat 0, so
// seat 0's own creatures receive the Metalcraft rider. It returns the engine,
// the victim and the graveyard-confirmed state.
func castConcussiveBolt(t *testing.T, artifacts int) (*Engine, state.ObjID) {
	t.Helper()
	e, _, spell, victim := metalcraftSpell(t, uint64(2000+artifacts), "Concussive Bolt", artifacts)
	addMana(t, e, 0, "RRRRR")
	submitChoices(t, e, castOptMode(t, castOptions(t, e), spell, "").Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the player target ask, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "player" && opt.Player == 0 {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("seat 0 not offered as the Bolt target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(spell).Zone; z != state.ZGraveyard {
		t.Fatalf("Bolt zone = %s, want graveyard", z)
	}
	if z := e.G.Obj(victim).Zone; z != state.ZBattlefield {
		t.Fatalf("victim zone = %s, want battlefield", z)
	}
	return e, victim
}

// TestConcussiveBoltCantBlockIsADeclareBlockersOutcome drives a REAL block
// decision: at the Metalcraft threshold the victim's block pair is not
// offered, and below it the pair IS offered and the declaration is accepted.
// The precondition asserts the derived keyword is the thing that differs.
func TestConcussiveBoltCantBlockIsADeclareBlockersOutcome(t *testing.T) {
	for _, tc := range []struct {
		name        string
		artifacts   int
		wantOffered bool
	}{
		{"below threshold", 2, true},
		{"at threshold", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, victim := castConcussiveBolt(t, tc.artifacts)
			// Precondition: the two branches must differ in the derived grant,
			// or the offer assertion proves nothing.
			_, granted := cantBlockKeywordLine(t, e, victim)
			if granted != !tc.wantOffered {
				t.Fatalf("precondition: derived can't-block = %v, want %v", granted, !tc.wantOffered)
			}
			attacker := boltAttacker(t, e)
			control := blockControl(t, e)
			if got := e.blockRestricted(victim, attacker); got != !tc.wantOffered {
				t.Fatalf("blockRestricted = %v, want %v", got, !tc.wantOffered)
			}
			if e.blockRestricted(control, attacker) {
				t.Fatal("precondition: the control blocker must be unrestricted")
			}
			d := openBlockAsk(t, e)
			// The control blocker keeps the ask alive in the restricted case, so
			// its presence proves the decision was really built rather than the
			// board simply being empty.
			if findBlockOption(d, control, attacker) == nil {
				t.Fatalf("control blocker not offered: %+v", d.Options)
			}
			opt := findBlockOption(d, victim, attacker)
			if (opt != nil) != tc.wantOffered {
				t.Fatalf("victim block option offered = %v, want %v (options %+v)", opt != nil, tc.wantOffered, d.Options)
			}
			if !tc.wantOffered {
				return
			}
			// Below the condition the offered pair must really be declarable.
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
				t.Fatalf("below threshold: offered block was rejected: %v", err)
			}
		})
	}
}

// TestConcussiveBoltCantBlockExpiresAtEndOfTurn proves the grant is the
// until-end-of-turn restriction and not a permanent change: after the SAME
// game's EndOfTurnCleanup the keyword is gone and the pair is offered again.
func TestConcussiveBoltCantBlockExpiresAtEndOfTurn(t *testing.T) {
	e, victim := castConcussiveBolt(t, 3)
	attacker := boltAttacker(t, e)
	// Precondition: the grant blocks the pair before cleanup, so its absence
	// after is the cleanup and not an unset fixture.
	if _, granted := cantBlockKeywordLine(t, e, victim); !granted {
		t.Fatal("precondition: no derived can't-block before cleanup")
	}
	if e.canBlock(victim, attacker) {
		t.Fatal("precondition: victim could block despite the grant")
	}
	e.EndOfTurnCleanup()
	if _, granted := cantBlockKeywordLine(t, e, victim); granted {
		t.Fatal("derived can't-block survived EndOfTurnCleanup (UntilEOT grant leaked)")
	}
	if !e.canBlock(victim, attacker) {
		t.Fatal("victim still cannot block after the grant expired")
	}
	// The same-game block decision must now offer the pair.
	d := openBlockAsk(t, e)
	if findBlockOption(d, victim, attacker) == nil {
		t.Fatalf("victim not offered as a blocker after the grant expired: %+v", d.Options)
	}
}

// TestBareCantBlockKeywordSpellingEnforced is the corpus regression for the
// UNPREFIXED spelling. Unearthly Blizzard's Pump grants `KW$ CARDNAME can't
// block.` with no HIDDEN marker; the derived grant must reach the same oracle
// the HIDDEN form does.
func TestBareCantBlockKeywordSpellingEnforced(t *testing.T) {
	e, _, spell, victim := metalcraftSpell(t, 2100, "Unearthly Blizzard", 0)
	addMana(t, e, 0, "RRR")
	submitChoices(t, e, castOptMode(t, castOptions(t, e), spell, "").Index)
	submitChoices(t, e, targetOptionFor(t, e, victim))
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(spell).Zone; z != state.ZGraveyard {
		t.Fatalf("Blizzard zone = %s, want graveyard", z)
	}
	// The attacker comes after the cast: boltAttacker parks the game in the
	// declare-blockers step, and addMana's drive-to-Main1 would stall on it.
	attacker := boltAttacker(t, e)
	if !e.blockRestricted(victim, attacker) {
		t.Fatalf("blockRestricted = false before the assertion; keywords = %v", e.Derived(victim).Keywords)
	}
	raw, granted := cantBlockKeywordLine(t, e, victim)
	if !granted {
		t.Fatalf("derived keyword missing; keywords = %v", e.Derived(victim).Keywords)
	}
	// The corpus card really does use the bare spelling -- this is the point.
	if strings.HasPrefix(strings.ToUpper(raw), "HIDDEN ") {
		t.Fatalf("fixture keyword %q carries HIDDEN; the bare spelling is untested", raw)
	}
	if !e.blockRestricted(victim, attacker) {
		t.Fatalf("blockRestricted = false despite derived %q", raw)
	}
	if e.canBlock(victim, attacker) {
		t.Fatalf("canBlock = true despite derived %q", raw)
	}
	control := blockControl(t, e)
	d := openBlockAsk(t, e)
	if findBlockOption(d, control, attacker) == nil {
		t.Fatalf("control blocker not offered: %+v", d.Options)
	}
	if findBlockOption(d, victim, attacker) != nil {
		t.Fatalf("bare-spelling grant: victim was offered as a blocker: %+v", d.Options)
	}
}
