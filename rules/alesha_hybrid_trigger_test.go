package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the CR 601.2b / 107.4e flexible-pip payment at a
// TRIGGERED-cost window, on the brief's real-corpus flagship carrier: Alesha,
// Who Smiles at Death's attack trigger `Cost$ WB WB`. Before the fix the
// window's pay/decline ask collapsed to a single `trigger_cost_decline`
// because Cost.Priceable() rejects hybrid pips and no announcement ask
// existed, so her ChangeZone body never ran. These tests drive real combat
// from the attackers declaration, elect each hybrid face through the window's
// own pip ask (the cast flow's `pay_W`/`pay_B` option kinds and Cost.announcePip
// alternatives), pay, and assert the returned creature's entry plus the exact
// mana/life spent.

// aleshaTriggerBoard builds the real Alesha attack-trigger board: seat 0 has
// Alesha and one inline power-2 graveyard candidate, seat 1 has a blocker
// body, and the clock is parked at seat 0's declare-attackers step.
func aleshaTriggerBoard(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg := combatTriggerBoard(t, reg, []string{"Alesha, Who Smiles at Death"},
		[]string{"Name:ReanimBear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"},
		nil, []string{"Name:BlockBear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	// Park the candidate in the graveyard with a logged move: Alesha's
	// ChangeZone reads Origin$ Graveyard, so the precondition is that the
	// object actually sits there.
	grav := attackingEntryMove(t, e, 0, "ReanimBear", state.ZGraveyard)
	return e, cfg, grav
}

// aleshaPipFaces returns the option Kinds of a window pip decision, or an
// error told in the fatal. It asserts the decision really is the pip ask (the
// prefixed "pay_" kinds the cast flow's manaAsk also uses) so a test that
// never reaches the election fails loudly rather than silently.
func aleshaPipFaces(t *testing.T, d *decision.Decision) []string {
	t.Helper()
	var faces []string
	for _, o := range d.Options {
		if len(o.Kind) >= 4 && o.Kind[:4] == "pay_" {
			faces = append(faces, o.Kind)
		}
	}
	if len(faces) == 0 {
		t.Fatalf("expected a hybrid pip election, got %+v", d.Options)
	}
	return faces
}

// TestAleshaHybridTriggerPaysEitherFace is the brief's real-corpus regression:
// with the required coloured mana in the pool, Alesha's trigger costs WB WB,
// the pip ask offers both faces of each pip, a legal payment is made, and the
// chosen graveyard creature leaves the graveyard and enters tapped and
// attacking. It also asserts the mana actually left the pool, so a "payment"
// that silently skipped the charge cannot pass.
func TestAleshaHybridTriggerPaysEitherFace(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, grav := aleshaTriggerBoard(t, reg)
	alesha := findBattlefield(t, e, 0, "Alesha, Who Smiles at Death", 0)

	// The precondition the payment depends on: the candidate is in the zone
	// Alesha's Origin$ Graveyard reads, and Alesha is on the battlefield.
	if o := e.G.Obj(grav); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("candidate %d is %+v, want it in the graveyard before the trigger", grav, o)
	}
	if !onBattlefield(e, alesha) {
		t.Fatal("Alesha is not on the battlefield; the attack trigger cannot fire")
	}
	// One white and one black produce the two hybrid pips' opposite faces.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "W", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 1})
	poolBefore := e.G.Players[0].Pool
	if poolBefore.Total() != 2 {
		t.Fatalf("pool before payment = %d, want 2", poolBefore.Total())
	}

	e.askAttackers()
	submitAttackers(t, e, alesha)

	facesChosen := 0
	// Face plan: the first hybrid pip paid with white, the second with black.
	// Both faces are offered on each pip (asserted below).
	facePlan := []string{"pay_W", "pay_B"}
	entryDrive(t, e, func(d *decision.Decision) {
		switch {
		case d.Kind == decision.KTarget:
			chooseObjOption(t, e, d, grav)
		case d.Kind == decision.KChoose && len(d.Options) > 0 && isPipAsk(d.Options):
			faces := aleshaPipFaces(t, d)
			// Each of Alesha's two hybrid pips offers BOTH of its colours --
			// the legal color choices the brief requires.
			hasW, hasB := false, false
			for _, f := range faces {
				if f == "pay_W" {
					hasW = true
				}
				if f == "pay_B" {
					hasB = true
				}
			}
			if !hasW || !hasB {
				t.Fatalf("pip %d faces = %v, want both pay_W and pay_B", facesChosen, faces)
			}
			if facesChosen >= len(facePlan) {
				t.Fatalf("more pip asks than the cost's 2 hybrid pips: %+v", d.Options)
			}
			chooseKindOption(t, e, d, facePlan[facesChosen])
			facesChosen++
		case d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "trigger_cost_pay":
			chooseKindOption(t, e, d, "trigger_cost_pay")
		case d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "trigger_cost_decline":
			t.Fatalf("the window offered decline-only despite WB in the pool: %+v", d.Options)
		default:
			t.Fatalf("unexpected ask during Alesha's resolution: %+v", d)
		}
	}, func() bool { return onBattlefield(e, grav) })

	if facesChosen != 2 {
		t.Fatalf("answered %d pip elections, want 2 (Alesha's two {W/B} pips)", facesChosen)
	}
	// The real assertion: the chosen card left the graveyard and entered
	// tapped and attacking, exactly one TokenAttacks event.
	assertEnteredAttacking(t, e, grav, 1)
	if o := e.G.Obj(grav); o.Zone != state.ZBattlefield {
		t.Fatalf("returned creature zone = %v, want battlefield", o.Zone)
	}
	// The mana was actually spent: both the white and the black unit are gone.
	poolAfter := e.G.Players[0].Pool
	if poolAfter.Total() != 0 {
		t.Fatalf("pool after payment = %d (W=%d B=%d), want 0 -- the hybrid charge was skipped",
			poolAfter.Total(), poolAfter[state.MW], poolAfter[state.MB])
	}
	replayCheck(t, e, cfg)
}

// TestAleshaHybridTriggerWillNotPayUnpayable is the negative half: with an
// empty pool and no untapped mana sources, the window must not falsely offer
// the payment. Since the pool-coverability gate
// (triggeredCostManaHalfPayable, ticket agent-20260922T232740Z-cf0357bb) the
// pay/decline ask is DECLINE-ONLY when neither the pool nor any activatable
// mana source can cover the announced cost -- the CR 601.2b pip announcement
// is still posed (it is a choice, not a solvency claim), but the pay option
// itself is absent, so an unpayable window can never take a payment that can
// only fail at the charge. The test asserts the option list is decline-only
// and that the card stays in the graveyard with pool and life untouched.
func TestAleshaHybridTriggerWillNotPayUnpayable(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, grav := aleshaTriggerBoard(t, reg)
	alesha := findBattlefield(t, e, 0, "Alesha, Who Smiles at Death", 0)

	if o := e.G.Obj(grav); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("candidate %d is %+v, want it in the graveyard", grav, o)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool = %d, want empty for the unpayable case", got)
	}
	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("seat 0 life = %d, want 20 (the starting total) before payment", life)
	}
	// No untapped mana sources: seat 0's only permanents that could produce
	// are Alesha (tapped by the declare) and the non-producer bodies. Assert
	// the precondition explicitly -- otherwise a board with a stray untapped
	// source would silently make this a payable run.
	if n := len(untappedManaSourceIDs(e, 0)); n != 0 {
		t.Fatalf("seat 0 has %d untapped mana sources, want 0 for the unpayable case", n)
	}

	e.askAttackers()
	submitAttackers(t, e, alesha)

	declined := false
	pipAsks := 0
	entryDrive(t, e, func(d *decision.Decision) {
		switch {
		case d.Kind == decision.KTarget:
			chooseObjOption(t, e, d, grav)
		case d.Kind == decision.KChoose && len(d.Options) > 0 && isPipAsk(d.Options):
			// The announcement is still posed (CR 601.2b); elect a legal face.
			aleshaPipFaces(t, d)
			chooseKindOption(t, e, d, "pay_W")
			pipAsks++
		case d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "trigger_cost_pay":
			// The brief's requirement: an unpayable window does not falsely
			// offer the payment. An empty pool with zero untapped mana sources
			// cannot cover the announced WB WB, so the pay option must be
			// absent -- decline-only.
			t.Fatalf("the window falsely offered trigger_cost_pay with an empty pool and no mana sources: %+v", d.Options)
		case d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "trigger_cost_decline":
			// The decline-only offer gate: exactly one option, no pay.
			if len(d.Options) != 1 {
				t.Fatalf("the unpayable window offered %d options, want exactly the single decline: %+v", len(d.Options), d.Options)
			}
			declined = true
			chooseKindOption(t, e, d, "trigger_cost_decline")
		default:
			t.Fatalf("unexpected ask during Alesha's unpaid resolution: %+v", d)
		}
	}, func() bool { return declined })

	if !declined {
		t.Fatal("the window never reached its decline-only pay/decline ask")
	}
	if pipAsks != 2 {
		t.Fatalf("saw %d pip elections, want 2 before the decline", pipAsks)
	}
	// The failed pay left the card in the graveyard, and no mana or life was
	// consumed by an unpayable attempt.
	if o := e.G.Obj(grav); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("unpayable candidate is %+v, want it still in the graveyard", o)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the failed pay = %d, want 0", got)
	}
	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("life after the failed pay = %d, want 20", life)
	}
	replayCheck(t, e, cfg)
}

// untappedManaSourceIDs returns the seat's untapped battlefield mana sources,
// the same walk paymentManaAskClass uses to populate its activate menu.
func untappedManaSourceIDs(e *Engine, p state.PlayerID) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if e.untappedManaSource(p, id) {
			out = append(out, id)
		}
	}
	return out
}

// isPipAsk reports whether a KChoose decision is one of the window's
// flexible-pip announcements (any "pay_<colour>" or pay_generic/pay_life
// option). It guards every switch arm that must recognise the ask without
// relying on option position.
func isPipAsk(opts []decision.Option) bool {
	for _, o := range opts {
		switch o.Kind {
		case "pay_W", "pay_U", "pay_B", "pay_R", "pay_G", "pay_C", "pay_generic", "pay_life":
			return true
		}
	}
	return false
}
