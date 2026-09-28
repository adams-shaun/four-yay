package rules

// spcz1: a targeted root's SubAbility$ ChangeZone must ask ITS OWN
// ValidTgts$ targeting at resolution instead of silently inheriting the
// parent's targets. Before the fix changeZoneChosenTargets' inherit guard
// returned every non-TargetUnique$ sub to Defined's ValidTgts$ fallthrough,
// which read Ctx.Targets -- the parent's artifact/spell -- so the graveyard
// "may shuffle" clauses of Cathartic Parting, Put Away and Devious Cover-Up
// never asked and the option was silently swallowed.
//
// Both pins run REAL corpus cards. The Counter-parent carrier goes through
// the gitignored registry (no Forge script text is committed); the
// ChangeZone-parent carrier reads its script from the corpus file at test
// time (the alltargeted1 pattern) because its root needs an opponent-
// controlled artifact the registry engine's Mountains-only opponent deck
// cannot supply.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// drainAfterAnswer passes every priority decision until the stack is empty,
// failing if the sub's ask is RE-POSED after its answer was consumed (the
// elected-zero non-re-ask contract) or any other unexpected ask appears.
func drainAfterAnswer(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 30 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the stack (depth %d)", len(e.G.Stack))
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("re-posed or unexpected mid-resolution ask after the answered one: kind=%v resume=%q prompt=%q options=%+v",
				d.Kind, d.ResumeKind, d.Prompt, d.Options)
		}
		for _, o := range d.Options {
			if o.Kind == "pass" {
				submitChoices(t, e, o.Index)
				break
			}
		}
	}
	if len(e.G.Stack) > 0 {
		t.Fatal("the stack never drained")
	}
}

// castNamedInHand submits the cast option for the named card in seat 0's
// hand and returns the decision that follows it.
func castNamedInHand(t *testing.T, e *Engine, name string) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision to cast from")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" {
			if so := e.G.Obj(o.Obj); so != nil && so.Face() != nil && so.Face().Name == name {
				idx = o.Index
			}
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %q: %+v", name, d.Options)
	}
	submitChoices(t, e, idx)
	return e.Pending()
}

// chooseObj submits the option of kind kind offering obj.
func chooseObj(t *testing.T, e *Engine, kind string, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision to answer")
	}
	for _, o := range d.Options {
		if o.Kind == kind && o.Obj == obj {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no %s option for %d in %+v", kind, obj, d.Options)
}

// TestPutAwaySubAsksGraveyardTarget pins the Counter-parent shape end to end
// on the real corpus card: seat 0 casts Grizzly Bears, then Put Away
// countering it. The sub (`DB$ ChangeZone | Origin$ Graveyard | ... |
// Shuffle$ True | ShuffleNonMandatory$ True`) must pose its own graveyard
// ask (KChoose, resume "choice"), the answered card -- deliberately a
// DIFFERENT card than the countered spell, proving the answer decoupled from
// Ctx.Targets -- moves to the library, and the may-shuffle confirm appears
// and is honoured on both branches.
func TestPutAwaySubAsksGraveyardTarget(t *testing.T) {
	t.Parallel()
	for _, accept := range []bool{true, false} {
		name := "decline keeps order, no shuffle"
		if accept {
			name = "accept shuffles once"
		}
		t.Run(name, func(t *testing.T) {
			reg := searchTestRegistry(t)
			e, cfg := searchEngine(t, reg, "Put Away")
			paSpell := searchMoveByName(t, e, "Put Away", state.ZHand)
			bearSpell := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
			forest := searchMoveByName(t, e, "Forest", state.ZGraveyard)
			// Preconditions the assertions below depend on: the countered
			// spell is castable, the graveyard answer is in the zone the
			// sub's Origin$ reads, and the two cards differ.
			if o := e.G.Obj(paSpell); o == nil || o.Zone != state.ZHand {
				t.Fatalf("precondition: Put Away is %+v, want it in hand", o)
			}
			if o := e.G.Obj(bearSpell); o == nil || o.Zone != state.ZHand {
				t.Fatalf("precondition: the bear to cast is %+v, want it in hand", o)
			}
			if o := e.G.Obj(forest); o == nil || o.Zone != state.ZGraveyard {
				t.Fatalf("precondition: the graveyard answer is %+v, want it in the graveyard", o)
			}
			if bearSpell == forest {
				t.Fatal("precondition: the graveyard answer must differ from the countered spell")
			}
			addMana(t, e, 0, "CG")
			castNamedInHand(t, e, "Grizzly Bears")
			// The cast put the bear SPELL on the stack (a new object id may
			// wrap the card); it is the counter's target.
			var bearStack state.ObjID
			for _, id := range e.G.Zone(state.ZStack, 0) {
				bearStack = id
			}
			if bearStack == 0 {
				t.Fatal("precondition: no spell on the stack after casting the bear")
			}
			addMana(t, e, 0, "CCUU")
			d := castNamedInHand(t, e, "Put Away")
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("after casting Put Away: pending = %+v, want the counter target ask", d)
			}
			// The counter's target ask must offer the bear spell on the stack.
			inStack := false
			for _, o := range d.Options {
				if o.Kind == "spell" && o.Obj == bearStack {
					inStack = true
				}
			}
			if !inStack {
				t.Fatalf("counter ask does not offer the bear spell %d: %+v", bearStack, d.Options)
			}
			chooseObj(t, e, "spell", bearStack)

			// The sub's own graveyard ask: pre-fix this never fired (the sub
			// inherited the countered spell and its Origin$ Graveyard filter
			// silently no-oped).
			start := len(e.L.Events)
			d = passUntilAskKind(t, e, decision.KChoose, 30)
			if d.ResumeKind != "choice" || d.Prompt != "Select target card from your graveyard" {
				t.Fatalf("graveyard ask = %+v, want the sub's KChoose \"choice\" graveyard ask", d)
			}
			graveIdx := -1
			for _, o := range d.Options {
				if o.Kind == "card" && o.Obj == forest {
					graveIdx = o.Index
				}
			}
			if graveIdx < 0 {
				t.Fatalf("the graveyard ask does not offer the graveyard card %d the sub's own filter admits: %+v", forest, d.Options)
			}
			submitChoices(t, e, graveIdx)

			// The live may-shuffle confirm (ShuffleNonMandatory$ True).
			yes, no := mayShuffleConfirm(t, e, 0)
			if accept {
				submitChoices(t, e, yes)
			} else {
				submitChoices(t, e, no)
			}
			drainAfterAnswer(t, e)

			// The ANSWERED card is in the library; the countered spell's card
			// (Put Away sends it to the graveyard) stayed exactly where the
			// counter put it -- the answer was the ask's, not Ctx.Targets'.
			if o := e.G.Obj(forest); o == nil || o.Zone != state.ZLibrary {
				t.Fatalf("after resolution: the answered graveyard card is %+v, want it in seat 0's library", o)
			}
			if o := e.G.Obj(bearStack); o == nil || o.Zone != state.ZGraveyard {
				t.Fatalf("after resolution: the countered spell is %+v, want it in the graveyard untouched by the sub", o)
			}
			want := 0
			if accept {
				want = 1
			}
			if got := shuffleCountFrom(e, start, 0); got != want {
				t.Fatalf("seat-0 shuffles after a %s answer = %d, want %d", map[bool]string{true: "yes", false: "no"}[accept], got, want)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestCatharticPartingSubAsksGraveyardTargetElectedZero pins the
// ChangeZone-parent shape on the real corpus card: the root SP targets an
// opponent-controlled artifact (a battlefield permanent -- nothing like the
// sub's graveyard candidates), and the sub's "up to four target cards from
// your graveyard" ask still fires. Electing ZERO moves nothing and is
// consumed once: no re-posed ask anywhere before the stack drains.
func TestCatharticPartingSubAsksGraveyardTargetElectedZero(t *testing.T) {
	t.Parallel()
	catharticSrc := alltargetedCorpusText(t, "c/cathartic_parting.txt")
	orbSrc := "Name:Orb\nManaCost:1\nTypes:Artifact\nOracle:x\n"
	graveSrc := "Name:Grave Card\nManaCost:1\nTypes:Artifact\nOracle:x\n"
	e, cfg, _ := newFixtureDeckWithOpponentCard(t, 7011, catharticSrc, graveSrc, orbSrc)
	orb := moveSeeded(t, e, 1, orbSrc, state.ZBattlefield)
	graveCard := moveSeeded(t, e, 0, graveSrc, state.ZGraveyard)
	// Preconditions the assertions depend on: the root's target is on the
	// battlefield (the root moves it -- so the resolution demonstrably
	// reached the sub), the graveyard answer is in the graveyard and differs
	// from the root's target.
	if o := e.G.Obj(orb); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: the opponent artifact is %+v, want it on seat 1's battlefield", o)
	}
	if o := e.G.Obj(graveCard); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: the graveyard card is %+v, want it in seat 0's graveyard", o)
	}
	if orb == graveCard {
		t.Fatal("precondition: the sub's candidate must differ from the root's target")
	}
	addMana(t, e, 0, "CG")
	castFirst(t, e, "cast")
	chooseObj(t, e, "permanent", orb)

	// The sub's own graveyard ask: pre-fix it never fired.
	d := passUntilAskKind(t, e, decision.KChoose, 30)
	if d.ResumeKind != "choice" || d.Prompt != "Select target card from your graveyard" {
		t.Fatalf("graveyard ask = %+v, want the sub's KChoose \"choice\" graveyard ask", d)
	}
	graveIdx := -1
	for _, o := range d.Options {
		if o.Kind == "card" && o.Obj == graveCard {
			graveIdx = o.Index
		}
	}
	if graveIdx < 0 {
		t.Fatalf("the graveyard ask does not offer the card the sub's own filter admits (%d): %+v", graveCard, d.Options)
	}
	// Elect ZERO of the up-to-four targets.
	submitChoices(t, e)
	drainAfterAnswer(t, e)

	// The root ran (its target moved to seat 1's library); the elected-zero
	// answer moved nothing out of the graveyard.
	if o := e.G.Obj(orb); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("after resolution: the root's artifact is %+v, want it shuffled into seat 1's library", o)
	}
	if o := e.G.Obj(graveCard); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("after resolution: the graveyard card is %+v, want it untouched by the elected-zero answer", o)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == graveCard && ev.To == state.ZLibrary {
			t.Fatalf("the elected-zero answer still moved the graveyard card to a library: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}
