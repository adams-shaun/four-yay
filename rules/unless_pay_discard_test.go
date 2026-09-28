package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// unless_pay_discard_test.go pins the ONE shared UnlessCost$ gate
// (effects.Resolve's unlessProceed dispatch) on a Discard SA — the fifth SA
// kind beside the four unless_pay_family_test.go already covers (Tap,
// DealDamage, LoseLife, ChangeZone). The premise that a Discard SA's
// UnlessCost$/UnlessPayer$ alternative was ignored was FALSE at the worktree
// base: the params never live in effDiscard (effects/cardflow.go); the shared
// gate in effects.Resolve prices and poses the ask for every API. These are
// the ENGINE tests for both branches (pay vs decline) on the two real corpus
// carriers, so the census read (param:api:Discard.UnlessCost) is backed by
// behaviour, not by a param-read shim.
//
// Tyrannize (Mode$ Hand) and Flay (a SubAbility$ Discard with its own
// unless) are the two shapes: a whole-hand discard spared by 7 life, and a
// second random discard spared by {1}. Neither test touches production code.

// seedDiscardHand puts n authored (never corpus — GPL) fixture cards in
// seat 1's hand and returns their object IDs. Each object is given its
// ZHand zone BEFORE SetZone, so the engine's zone list and the object's own
// zone agree (a stale object zone makes the discard walk read an empty
// hand and the test silently vacuous).
func seedDiscardHand(t *testing.T, e *Engine, n int) []state.ObjID {
	t.Helper()
	ids := make([]state.ObjID, 0, n)
	for i := 0; i < n; i++ {
		o := e.G.AddObject(card(t, "Name:Unless Filler\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 1)
		o.Zone = state.ZHand
		ids = append(ids, o.ID)
	}
	e.G.SetZone(state.ZHand, 1, ids)
	return ids
}

// castDiscardToUnlessAsk casts the (single) card in seat 0's hand and
// answers the cast-flow asks a Discard spell poses — Tyrannize's two hybrid
// {B/R} payment KChoose asks, and the KTarget seat-1 pick — until the shared
// unless_pay ask is pending, then returns it. It asserts the cast actually
// reached the stack (a corpus/cast regression must fail loudly here).
func castDiscardToUnlessAsk(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	e.askPriority(0)
	castFirst(t, e, "cast")
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while driving the Discard cast to its unless ask")
		}
		if d.Kind == decision.KModes && d.ResumeKind == "unless_pay" {
			return d
		}
		ch := d.Options[0].Index
		if d.Kind == decision.KPriority {
			// After the target is chosen the spell sits on the stack and both
			// seats get a priority round before the unless ask is posed; pass.
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if pass < 0 {
				t.Fatalf("priority decision with no pass while seeking the unless ask: %+v", d.Options)
			}
			ch = pass
		}
		if d.Kind == decision.KTarget {
			found := false
			for _, o := range d.Options {
				if o.Player == 1 {
					ch, found = o.Index, true
				}
			}
			if !found {
				t.Fatalf("no seat-1 target option in %+v", d.Options)
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{ch}}); err != nil {
			t.Fatalf("submit cast-flow answer %d: %v", ch, err)
		}
	}
	t.Fatalf("never reached the unless ask within %d decisions", limit)
	return nil
}

// drainDiscardResolution passes priority and answers any residual mid-
// resolution ask until the stack is empty, so the hand assertions read the
// post-resolution state.
func drainDiscardResolution(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		if len(e.G.Stack) == 0 && d.Kind == decision.KPriority {
			return
		}
		if d.Kind == decision.KPriority {
			castFirst(t, e, "pass")
			continue
		}
		if len(d.Options) == 0 {
			return
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	t.Fatalf("stack did not empty within %d decisions", limit)
}

// ---------------------------------------------------------------------------
// Tyrannize: A:SP$ Discard | ValidTgts$ Player | Mode$ Hand |
// UnlessCost$ PayLife<7> | UnlessPayer$ Targeted — the whole-hand discard
// spared by 7 life (the pay option is the direct PayLife branch).
// ---------------------------------------------------------------------------

func TestUnlessPayDiscardTyrannizePaySparesHand(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Tyrannize"))
	seeded := seedDiscardHand(t, e, 3)
	if got := len(e.G.Zone(state.ZHand, 1)); got != 3 {
		t.Fatalf("precondition: seat 1 hand holds %d cards, want the 3 seeded", got)
	}
	addMana(t, e, 0, "BRBRRRR")
	ask := castDiscardToUnlessAsk(t, e, 30)
	if ask.Player != 1 {
		t.Fatalf("pay ask player = seat %d, want the targeted seat (UnlessPayer$ Targeted)", ask.Player)
	}
	if ask.ResumeSA == nil || ask.ResumeSA.API != "Discard" {
		t.Fatalf("ask SA = %+v, want the SP$ Discard body", ask.ResumeSA)
	}
	if len(ask.Options) != 2 {
		t.Fatalf("pay ask offers %d options, want 2 (Pay 7 life / Don't pay): %+v", len(ask.Options), ask.Options)
	}
	submitChoices(t, e, ask.Options[0].Index)
	drainDiscardResolution(t, e, 60)
	if life := e.G.Players[1].Life; life != 13 {
		t.Fatalf("payer life = %d, want 13 (20 - 7)", life)
	}
	if got := e.G.Zone(state.ZHand, 1); len(got) != 3 {
		t.Fatalf("paid Tyrannize hand = %d cards, want the 3 kept (%v)", len(got), got)
	}
	for _, id := range seeded {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
			t.Fatalf("paid Tyrannize moved seeded card %d: %+v", id, o)
		}
	}
}

func TestUnlessPayDiscardTyrannizeDeclineDiscardsHand(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Tyrannize"))
	seedDiscardHand(t, e, 3)
	if got := len(e.G.Zone(state.ZHand, 1)); got != 3 {
		t.Fatalf("precondition: seat 1 hand holds %d cards, want the 3 seeded", got)
	}
	addMana(t, e, 0, "BRBRRRR")
	ask := castDiscardToUnlessAsk(t, e, 30)
	if len(ask.Options) != 2 {
		t.Fatalf("pay ask offers %d options, want 2: %+v", len(ask.Options), ask.Options)
	}
	submitChoices(t, e, ask.Options[1].Index)
	drainDiscardResolution(t, e, 60)
	if life := e.G.Players[1].Life; life != 20 {
		t.Fatalf("decliner life = %d, want 20 (nothing paid)", life)
	}
	if got := e.G.Zone(state.ZHand, 1); len(got) != 0 {
		t.Fatalf("declined Tyrannize hand = %d cards, want the whole hand discarded", len(got))
	}
}

// ---------------------------------------------------------------------------
// Flay: the main SP$ Discard (Mode$ Random, one card) plus the SVar
// DBDiscard sub (Defined$ Targeted, one more card, UnlessCost$ 1,
// UnlessPayer$ Targeted) — the payment must prevent only the SECOND discard.
// Seat 1 is floated one {R} so the {1} is payable and the ask has both
// options (a single-option ask would make the pay branch unreachable and
// the test vacuous about the pay half).
// ---------------------------------------------------------------------------

func TestUnlessPayDiscardFlayPayPreventsTheSecondDiscard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Flay"))
	seedDiscardHand(t, e, 2)
	if got := len(e.G.Zone(state.ZHand, 1)); got != 2 {
		t.Fatalf("precondition: seat 1 hand holds %d cards, want the 2 seeded", got)
	}
	addMana(t, e, 0, "BBBB")
	addMana(t, e, 1, "R")
	if pool := e.G.Players[1].Pool.Total(); pool != 1 {
		t.Fatalf("precondition: payer pool = %d, want the 1 floated for the {1}", pool)
	}
	ask := castDiscardToUnlessAsk(t, e, 30)
	if ask.Player != 1 {
		t.Fatalf("pay ask player = seat %d, want the targeted seat (UnlessPayer$ Targeted)", ask.Player)
	}
	if ask.ResumeSA == nil || ask.ResumeSA.API != "Discard" {
		t.Fatalf("ask SA = %+v, want the DBDiscard Discard body", ask.ResumeSA)
	}
	if len(ask.Options) != 2 {
		t.Fatalf("Flay pay ask offers %d options, want 2 payable from the floated {R}: %+v", len(ask.Options), ask.Options)
	}
	submitChoices(t, e, ask.Options[0].Index)
	drainDiscardResolution(t, e, 60)
	if pool := e.G.Players[1].Pool.Total(); pool != 0 {
		t.Fatalf("payer pool = %d, want 0 (the floated {R} spent on the {1})", pool)
	}
	if got := e.G.Zone(state.ZHand, 1); len(got) != 1 {
		t.Fatalf("paid Flay hand = %d cards, want 1 (the main-body random discard only; the paid second discard was prevented)", len(got))
	}
}

func TestUnlessPayDiscardFlayDeclineDiscardsTwo(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Flay"))
	seedDiscardHand(t, e, 2)
	if got := len(e.G.Zone(state.ZHand, 1)); got != 2 {
		t.Fatalf("precondition: seat 1 hand holds %d cards, want the 2 seeded", got)
	}
	addMana(t, e, 0, "BBBB")
	addMana(t, e, 1, "R")
	if pool := e.G.Players[1].Pool.Total(); pool != 1 {
		t.Fatalf("precondition: payer pool = %d, want the 1 floated", pool)
	}
	ask := castDiscardToUnlessAsk(t, e, 30)
	if len(ask.Options) != 2 {
		t.Fatalf("Flay pay ask offers %d options, want 2: %+v", len(ask.Options), ask.Options)
	}
	submitChoices(t, e, ask.Options[1].Index)
	drainDiscardResolution(t, e, 60)
	if pool := e.G.Players[1].Pool.Total(); pool != 1 {
		t.Fatalf("decliner pool = %d, want 1 (nothing spent)", pool)
	}
	if got := e.G.Zone(state.ZHand, 1); len(got) != 0 {
		t.Fatalf("declined Flay hand = %d cards, want both discarded", len(got))
	}
}
