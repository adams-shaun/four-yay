// Count$TriggerRememberAmount — the reflexive trigger's remembered Integer
// (ticket cli-20261006T024354Z-3345d0e0, Count$ heads part 2). TDM New Way
// Forward's ImmediateTrigger rider `RememberSVarAmount$ X` evaluates X
// (`SVar:X:ReplaceCount$DamageAmount`, the prevented damage) and the
// reflexive half reads it back through `SVar:Y:Count$TriggerRememberAmount`
// for both the damage it deals and the cards it draws. Before this head and
// the rider channel existed the body was the unresolvable zero and the card
// reported as unsupported [count:TriggerRememberAmount].
//
// This pin drives the real spawning chain: it resolves New Way Forward's
// compiled DBImmediateTrigger body with the prevented damage in
// Ctx.Repl.Amount, then asserts BOTH halves -- that the minted reflexive
// trigger's context carries the remembered amount, and that the head reads
// it -- so reverting either the rider or the head arm fails the test.
package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestTriggerRememberAmountHeadOnNewWayForward pins the rider and the head
// against New Way Forward's real compiled face.
func TestTriggerRememberAmountHeadOnNewWayForward(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	nwf := onBoardCard(t, e, 0, corpusCard(t, "New Way Forward"))
	face := e.G.Obj(nwf).Face()

	body, ok := face.SVars["Y"]
	if !ok || body != "Count$TriggerRememberAmount" {
		t.Fatalf("test precondition: New Way Forward SVar Y = %q (ok %v)", body, ok)
	}
	if x := face.SVars["X"]; x != "ReplaceCount$DamageAmount" {
		t.Fatalf("test precondition: New Way Forward SVar X = %q, want ReplaceCount$DamageAmount", x)
	}
	sub := cards.ResolveSVar(face.SVars, "DBImmediateTrigger")
	if sub == nil {
		t.Fatal("test precondition: DBImmediateTrigger did not resolve")
	}

	// The spawning replacement context: the in-flight prevented damage is
	// what RememberSVarAmount$ X evaluates to.
	const prevented = 5
	c := &effects.Ctx{Controller: 0, Source: nwf, SVars: face.SVars}
	c.Repl.Amount = prevented
	c.Repl.Target = state.Target{Obj: nwf}
	effects.Resolve(e, c, sub)

	// The rider must have minted a reflexive trigger carrying the amount.
	var pt *pendingTrigger
	for i := range e.pendingTriggers {
		if e.pendingTriggers[i].Source == nwf {
			pt = &e.pendingTriggers[i]
			break
		}
	}
	if pt == nil {
		t.Fatal("precondition: DBImmediateTrigger did not queue a reflexive trigger (QueueReflexiveTrigger failed)")
	}
	if pt.Ctx.TriggerRememberedAmount != prevented {
		t.Fatalf("reflexive trigger TriggerRememberedAmount = %d, want %d (RememberSVarAmount$ rider not bound)",
			pt.Ctx.TriggerRememberedAmount, prevented)
	}
	// The head reads the bound amount off the reflexive trigger's context.
	if n, ok := effects.EvalCountOK(e, &pt.Ctx, body); !ok || n != prevented {
		t.Fatalf("Count$TriggerRememberAmount on the reflexive trigger = %d (ok %v), want %d", n, ok, prevented)
	}

	// A fresh, unbound context reads zero but stays MODELLED (ok true) --
	// Forge's default remembered amount, and the contract the evaluator's
	// bare-context probe relies on.
	if n, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 0, Source: nwf}, body); !ok || n != 0 {
		t.Fatalf("Count$TriggerRememberAmount unbound = %d (ok %v), want evaluated 0", n, ok)
	}
}

// TestTriggerRememberAmountOnManaDrainDelayedTrigger pins the second seed
// the head's carriers use: a DelayedTrigger RememberNumber$ True
// registration (Mana Drain, Plasm Capture, Scattering Stroke) carries the
// chain's RememberCounteredCMC$ binding to the fired ability, whose
// `Amount$ X` / `SVar:X:Count$TriggerRememberAmount` adds that much {C}.
// Driven end to end: Mana Drain cast from hand at a mana-value-5 spell, the
// registration's remembered amount, then the game advanced to the next main
// phase where the delayed trigger resolves.
func TestTriggerRememberAmountOnManaDrainDelayedTrigger(t *testing.T) {
	t.Parallel()
	drain := corpusCard(t, "Mana Drain")
	if x := drain.Faces[0].SVars["X"]; x != "Count$TriggerRememberAmount" {
		t.Fatalf("test precondition: Mana Drain SVar X = %q", x)
	}
	e := counterHands(t, []*cards.Card{drain}, nil, nil, nil)
	drainID := handObj(t, e, 0, "Mana Drain")
	spell := putOppSpellOnStack(t, e, card(t, beast5Src))
	if got := e.G.Obj(spell).Face().Cmc(); got != 5 {
		t.Fatalf("precondition: countered spell mana value %d, want 5", got)
	}
	e.G.Players[0].Pool[state.MU] = 2
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, drainID))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision after casting Mana Drain: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == spell {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the stack spell was not offerable: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passOnce(t, e)
	passOnce(t, e)
	if o := e.G.Obj(spell); o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: countered spell is in %s, want graveyard", o.Zone)
	}
	// The registration carries the countered spell's mana value.
	var reg *state.DelayedTrigger
	for i := range e.G.Delayed {
		if e.G.Delayed[i].Source == drainID && e.G.Delayed[i].Execute == "AddMana" {
			reg = &e.G.Delayed[i]
		}
	}
	if reg == nil {
		t.Fatal("precondition: Mana Drain registered no AddMana delayed trigger")
	}
	if reg.RememberedAmount != 5 {
		t.Fatalf("delayed registration RememberedAmount = %d, want 5 (RememberNumber$ True not carried)", reg.RememberedAmount)
	}
	if reg.Phase != state.StepMain2 {
		t.Fatalf("precondition: registration waits for %v, want Main2 (cast in Main1)", reg.Phase)
	}
	// Advance to the postcombat main phase and let the trigger resolve.
	var added int32
	for i := 0; i < 200 && e.G.Turn == 1 && added == 0; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if e.G.Step == state.StepMain2 && len(e.G.Zone(state.ZStack, 0))+len(e.G.Zone(state.ZStack, 1)) == 0 {
			added = e.G.Players[0].Pool[state.MC]
			if added != 0 {
				break
			}
		}
		if d.Kind == decision.KPriority {
			passOnce(t, e)
			continue
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	if added != 5 {
		t.Fatalf("Mana Drain's delayed trigger added %d {C} in the next main phase, want 5", added)
	}
}

// TestTriggerRememberAmountCensusEveryCarrier enumerates every corpus card
// whose SVars read Count$TriggerRememberAmount and asserts the head is
// supported and every extracted body resolves against a bare context. The
// two seeds (ImmediateTrigger RememberSVarAmount$ and DelayedTrigger
// RememberNumber$) feed 22 faces, and a body that silently stops resolving
// would leave those cards reading Forge's default zero while the coverage
// check still calls them supported. Scanning every SVar body (not just the
// ones cards.Face.ValueHeads calls referenced) catches a head read from a DB
// parameter value (NumDmg$ Count$TriggerRememberAmount), the shape a
// bare-SVar scan would miss. The pinned count (22 at FORGE_REF
// 95f04e8a04c8925fa97cb226fc3341cabcc90a53) fails both ways: a body the
// build newly cannot resolve, and a stale pin after a corpus move.
func TestTriggerRememberAmountCensusEveryCarrier(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	reg := testutil.CorpusRegistry(t)
	const head = "TriggerRememberAmount"
	const wantCarriers = 22
	if !effects.Supported()[cards.ValueHeadPrefix+head] {
		t.Fatalf("effects.Supported has no count:%s", head)
	}
	found := map[string]bool{}
	for _, c := range reg.AllCards() {
		if len(c.Faces) == 0 || c.Faces[0].Name == "" {
			continue
		}
		carrier := false
		for fi := range c.Faces {
			f := c.Faces[fi]
			for _, body := range f.SVars {
				for _, expr := range carrierBodies(body, head) {
					carrier = true
					ctx := &effects.Ctx{Controller: 0, Source: 0, SVars: f.SVars}
					if _, ok := effects.EvalCountOK(e, ctx, expr); !ok {
						t.Errorf("carrier %q body %q does not resolve (head unmodelled or malformed)", c.Faces[0].Name, expr)
					}
				}
			}
		}
		if !carrier {
			continue
		}
		found[c.Faces[0].Name] = true
		if miss := reg.Unsupported(c, effects.Supported()); slices.Contains(miss, cards.ValueHeadPrefix+head) {
			t.Errorf("carrier %q still lists count:%s unsupported", c.Faces[0].Name, head)
		}
	}
	if len(found) != wantCarriers {
		t.Fatalf("corpus references count:%s on %d cards, want %d: %v", head, len(found), wantCarriers, found)
	}
}
