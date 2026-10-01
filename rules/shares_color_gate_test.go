package rules

// SharesColorWith <referent> -- the colour-share gate of C.A.M.P.'s
// conditional Junk-token instruction ("If that creature shares a color with
// the mana that land produced") and Jaded Response's conditional counter
// ("Counter target spell if it shares a color with a creature you control").
// CR 608.2: a resolution conditional ("if ...") is a condition on the
// instruction -- it is performed only when the condition is true. Before the
// referent was bound in the shared filter grammar the gate's
// ConditionPresent$ carried an unknown predicate, effects.conditionMet
// returned UNRESOLVED, and the registry's documented fail-open convention
// ran the sub-ability unconditionally: measured at main 8b38ed71e, C.A.M.P.
// minted a Junk token even for a Savannah Lions target that shares nothing
// with the Forest's {G} (and Jaded Response countered every spell).
//
// Both drives below run the real corpus cards end to end. The matching
// branch was already right at main and must keep holding; the non-matching
// branches are the fix. Guard Dogs' SharesColorWith ChosenCard arm is
// deliberately NOT landed -- the brief's stop rule applies: its
// ConditionDefined$ Targeted gate is evaluated before the DB sub's
// mid-resolution target ask is posed, so a recognised predicate resolves the
// gate false on the empty group and the whole sub (target ask included) is
// skipped. It stays unrecognised, and sharesColorBareReferent's comment
// records the measured reason.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// campJunkDrive builds a seat-0 board of C.A.M.P., a Forest and one target
// creature, fortifies the Forest onto it, then taps the fortified land for
// mana and resolves the queued trigger onto that creature, returning the
// engine and the target id. wantColor is the target's printed colours the
// gate reads (asserted, so a vacuous corpus lookup fails loudly).
func campJunkDrive(t *testing.T, target, wantColor string) (e *Engine, camp, forest, tgt state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	if _, ok := reg.Token("c_a_junk_sac_exileplay"); !ok {
		t.Fatal("precondition: the corpus carries no c_a_junk_sac_exileplay token script")
	}
	e, _ = linkBoard(t, reg, []string{"C.A.M.P.", "Forest", target}, nil)
	// rules.New copies cfg.Tokens, and linkBoard leaves it nil, so DB$
	// Token's TokenScript$ would report an unknown script. The established
	// test-only mirror: Config.Tokens before New, or g.Tokens after (this
	// line) -- a config mirror, not engine state.
	e.G.Tokens = reg.Tokens
	camp = findOnBoard(t, e, 0, "C.A.M.P.")
	forest = findOnBoard(t, e, 0, "Forest")
	tgt = findOnBoard(t, e, 0, target)
	if got := effects.ColorsOf(e.G.Obj(tgt)); got != wantColor {
		t.Fatalf("precondition: %s colours = %q, want %q", target, got, wantColor)
	}
	if got := e.G.Obj(tgt).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition: target starts with %d +1/+1 counters", got)
	}

	fortifyCamp(t, e, camp, forest)
	if e.G.Obj(camp).AttachedTo != forest {
		t.Fatalf("precondition failed: C.A.M.P. attached to %d, want the Forest %d", e.G.Obj(camp).AttachedTo, forest)
	}

	// Tap the fortified land for mana: C.A.M.P.'s own TapsForMana trigger
	// queues (the Forest produces {G}, the only mana the referent can
	// share).
	e.resolveManaAbility(0, forest, e.availableManaAbilities(0, forest)[0], false)
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("tapping the fortified land queued %d triggers, want C.A.M.P.'s one", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after the trigger was put on the stack: %+v, want the counter target ask", d)
	}
	targetObject(t, e, tgt)
	passUntilStackEmpty(t, e, 30)
	return e, camp, forest, tgt
}

func campJunkTokens(e *Engine) int {
	n := 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.IsToken && o.Face() != nil && o.Face().Name == "Junk Token" {
			n++
		}
	}
	return n
}

func TestCAMPJunkTokenColorGate(t *testing.T) {
	t.Parallel()

	// Matching: the green Grizzly Bears shares {G} with the mana the Forest
	// produced -- the conditional holds, so the Junk token is created.
	e, _, _, bear := campJunkDrive(t, "Grizzly Bears", "G")
	if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
		t.Fatalf("the unconditional +1/+1 counter landed %d times, want 1", got)
	}
	if n := campJunkTokens(e); n != 1 {
		t.Fatalf("Grizzly Bears shares {G} with the Forest's mana: %d Junk tokens, want 1", n)
	}

	// Non-matching: Savannah Lions is white and shares nothing with {G} --
	// CR 608.2: the token instruction is NOT performed. The unconditional
	// +1/+1 counter still lands (the trigger's own instruction is
	// unconditional).
	e2, _, _, lion := campJunkDrive(t, "Savannah Lions", "W")
	if got := e2.G.Obj(lion).Counter("P1P1"); got != 1 {
		t.Fatalf("the unconditional +1/+1 counter landed %d times, want 1", got)
	}
	if n := campJunkTokens(e2); n != 0 {
		t.Fatalf("Savannah Lions shares nothing with the Forest's {G}: %d Junk tokens, want 0", n)
	}
}

// jadedEngine seats Jaded Response and the named spell in seat 0's hand with
// a green Grizzly Bears on its battlefield and a funded pool, and asks seat 0
// priority. The bear is the gate's `Valid Creature.YouCtrl` referent; the
// spell's colours are asserted per case so a mis-seated comparison fails
// loudly.
func jadedEngine(t *testing.T, reg *cards.Registry, spell, spellColors string) (e *Engine, bear, jaded, spID state.ObjID) {
	t.Helper()
	e = handEngineTokens(t,
		mustCorpusCard(t, reg, "Grizzly Bears"),
		mustCorpusCard(t, reg, "Jaded Response"),
		mustCorpusCard(t, reg, spell))
	ids := handIDsByFace(e)
	bear, jaded, spID = ids["Grizzly Bears"], ids["Jaded Response"], ids[spell]
	if bear == 0 || jaded == 0 || spID == 0 {
		t.Fatalf("precondition: hand missing a card: %v", ids)
	}
	if got := effects.ColorsOf(e.G.Obj(bear)); got != "G" {
		t.Fatalf("precondition: referent Grizzly Bears colours = %q, want G", got)
	}
	if got := effects.ColorsOf(e.G.Obj(spID)); got != spellColors {
		t.Fatalf("precondition: %s colours = %q, want %q", spell, got, spellColors)
	}
	placeOnBattlefield(t, e, bear)
	e.G.Players[0].Pool[state.MU] = 8
	e.G.Players[0].Pool[state.MG] = 8
	e.G.Players[0].Pool[state.MW] = 8
	e.askPriority(0)
	return e, bear, jaded, spID
}

func TestJadedResponseSharesColorGate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// Sharing: Giant Growth is green and the referent bear is green -- the
	// "if it shares a color" condition is true, so the spell IS countered.
	e, _, jaded, gg := jadedEngine(t, reg, "Giant Growth", "G")
	submitChoices(t, e, passToCast(t, e, gg))
	targetObject(t, e, findOnBoard(t, e, 0, "Grizzly Bears"))
	submitChoices(t, e, passToCast(t, e, jaded))
	targetObject(t, e, gg)
	passUntilStackEmpty(t, e, 30)
	if z := e.G.Obj(gg).Zone; z != state.ZGraveyard {
		t.Fatalf("Giant Growth shares {G} with the green bear: countered spell zone = %s, want Graveyard", z)
	}
	if z := e.G.Obj(jaded).Zone; z != state.ZGraveyard {
		t.Fatalf("Jaded Response itself resolved to graveyard: zone = %s, want Graveyard", z)
	}

	// Not sharing: Savannah Lions is white and shares nothing with the green
	// bear -- the condition is false, so the counter instruction is NOT
	// performed and the spell resolves onto the battlefield.
	e2, _, jaded2, lions := jadedEngine(t, reg, "Savannah Lions", "W")
	submitChoices(t, e2, passToCast(t, e2, lions))
	submitChoices(t, e2, passToCast(t, e2, jaded2))
	targetObject(t, e2, lions)
	passUntilStackEmpty(t, e2, 30)
	if z := e2.G.Obj(lions).Zone; z != state.ZBattlefield {
		t.Fatalf("Savannah Lions shares nothing with the green bear: the gate resolved false, so the spell resolves; zone = %s, want Battlefield", z)
	}
	if z := e2.G.Obj(jaded2).Zone; z != state.ZGraveyard {
		t.Fatalf("Jaded Response resolved (gate ran, declined to counter): zone = %s, want Graveyard", z)
	}
}
