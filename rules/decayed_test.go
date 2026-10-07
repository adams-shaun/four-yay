package rules

// CR 702.147 Decayed: "This creature can't block" plus "When this creature
// attacks, sacrifice it at end of combat." The behaviour is implemented
// rules-side (rules/decayed.go and the Decayed arm in
// combat.ParseHiddenKeyword), reading the DERIVED keyword list so a printed
// K:Decayed, a decayed counter (CR 122.1b, cards.CounterKeyword), a
// KW$ Decayed Pump grant and a layer-6 AddKeyword$ Decayed all behave alike.
// These are the real-corpus proofs named by
// rules/keyword_registration_test.go.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// corpusCard is defined in rules/life_draw_trigger_test.go; these proofs
// reuse it so they ride the live corpus registry.

// TestDecayedCannotBlock pins CR 702.147a's can't-block half for all three
// runtime carriers: a printed K:Decayed face, a decayed counter and a layer-6
// AddKeyword$ Decayed grant. The precondition is the same board shape WITHOUT
// Decayed, which must be able to block -- so a build that restricted every
// block would fail here rather than pass vacuously.
func TestDecayedCannotBlock(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	attacker := onBoard(t, e, 1, "Name:Bear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	plain := onBoard(t, e, 0, "Name:Plain\nManaCost:W\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.G.Obj(attacker).IsAttacking, e.G.Obj(attacker).Attacking = true, 0

	// Precondition: the same board shape with no Decayed really can block, so
	// the can't-block assertions below are not trivially true.
	if !e.canBlock(plain, attacker) {
		t.Fatal("precondition: a plain creature could not block; the restriction assertion is vacuous")
	}

	// (1) Printed K:Decayed, via the real corpus card Rot-Curse Rakshasa.
	printed := onBoardCard(t, e, 0, corpusCard(t, "Rot-Curse Rakshasa"))
	if !e.HasKeyword(printed, "Decayed") {
		t.Fatal("Rot-Curse Rakshasa does not carry its printed Decayed keyword")
	}
	if e.canBlock(printed, attacker) {
		t.Fatal("a printed K:Decayed creature was allowed to block (CR 702.147a)")
	}

	// (2) A decayed counter, the runtime path that never passes through a
	// printed K: line. Start from a plain face so only the counter supplies
	// the keyword.
	countered := onBoard(t, e, 0, "Name:Countered\nManaCost:U\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.CounterChange, Obj: countered, Counter: "Decayed", Amount: 1})
	if got := e.G.Obj(countered).Counter("Decayed"); got != 1 {
		t.Fatalf("precondition: decayed counter on the creature = %d, want 1", got)
	}
	if !e.HasKeyword(countered, "Decayed") {
		t.Fatal("precondition: the decayed counter did not confer the keyword (CR 122.1b)")
	}
	if e.canBlock(countered, attacker) {
		t.Fatal("a creature with a decayed counter was allowed to block (CR 702.147a)")
	}

	// (4) The with/without predicate family Wilhelt and Jadar filter on: a
	// decayed-countered creature matches withDecayed and not withoutDecayed,
	// and a plain creature the reverse. Without the effects filter
	// registration both would match nothing (the report's fail-closed bug).
	sc := effects.SpecContext{You: 0}
	if !effects.MatchesSpecCtx(e.G, "Creature.withDecayed", countered, sc) {
		t.Fatal("withDecayed did not match a creature with a decayed counter")
	}
	if effects.MatchesSpecCtx(e.G, "Creature.withoutDecayed", countered, sc) {
		t.Fatal("withoutDecayed matched a creature with a decayed counter")
	}
	if effects.MatchesSpecCtx(e.G, "Creature.withDecayed", plain, sc) {
		t.Fatal("withDecayed matched a plain creature with no decayed counter")
	}
	if !effects.MatchesSpecCtx(e.G, "Creature.withoutDecayed", plain, sc) {
		t.Fatal("withoutDecayed did not match a plain creature with no decayed counter")
	}

	// (3) A layer-6 AddKeyword$ Decayed grant, the continuous-effect path. A
	// `KW$ Decayed` Pump/PumpAll grant (Gisa, Glorious Resurrector and Ghouls'
	// Night Out) resolves to exactly this continuous effect (rules/layers.go's
	// statKeywords), so this arm proves the KW$ grant forbids blocking too.
	granted := onBoard(t, e, 0, "Name:Granted\nManaCost:B\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.AddContinuous(state.ContinuousEffect{
		Source: granted, Affects: "Card.Self", Controller: 0,
		Layer: state.LAbilities, AddKeywords: []string{"Decayed"},
	})
	if !e.HasKeyword(granted, "Decayed") {
		t.Fatal("precondition: the layer-6 AddKeyword$ Decayed grant is not live")
	}
	if e.canBlock(granted, attacker) {
		t.Fatal("a creature granted Decayed in layer 6 was allowed to block (CR 702.147a)")
	}
}

// TestDecayedCounterGrantsBothRules pins the report's exact symptom on the
// counter path: a creature with a decayed counter can't block AND attacks
// without being sacrificed immediately, then is sacrificed at the end of
// combat. It drives the engine's REAL combat flow so the promise is armed by
// the production trigger walk, not a direct emit, and asserts at both the
// declare-attackers instant (still alive) and end of combat (gone).
func TestDecayedCounterGrantsBothRules(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 974, "Name:Placeholder\nManaCost:0\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	attacker := onBoard(t, e, 1, "Name:Defender Bear\nManaCost:G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	decayed := onBoardReady(t, e, 0, "Name:Decay Target\nManaCost:B\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n")
	e.G.Obj(attacker).IsAttacking, e.G.Obj(attacker).Attacking = true, 0

	// The counter is the runtime path, never a printed K:Decayed line on this
	// face.
	e.emit(events.Event{Kind: events.CounterChange, Obj: decayed, Counter: "Decayed", Amount: 1})
	if got := e.G.Obj(decayed).Counter("Decayed"); got != 1 {
		t.Fatalf("precondition: decayed counter = %d, want 1", got)
	}
	if !e.HasKeyword(decayed, "Decayed") {
		t.Fatal("precondition: the decayed counter did not confer the keyword")
	}

	// Declare it attacking through the REAL attack ask.
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, decayed)
	// Immediately after the declaration the creature must still be on the
	// battlefield -- this is the report's "no immediate sacrifice" failure
	// mode made into an assertion, checked before the delayed trigger could
	// possibly resolve.
	if got := e.G.Obj(decayed).Zone; got != state.ZBattlefield {
		t.Fatalf("decayed attacker left the battlefield at declaration: zone %s, want battlefield (CR 702.147a sacrifices at END of combat)", got)
	}
	// Precondition: the end-of-combat promise was really armed, so the
	// sacrifice assertion below is not vacuous.
	if !hasDecayedPromise(e, decayed) {
		t.Fatalf("no live __kwDecayedSacrifice registration after attacking: %+v", e.G.Delayed)
	}

	// Cross combat damage and reach the end-of-combat step.
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEndCombat)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(decayed).Zone; got != state.ZGraveyard {
		t.Fatalf("decayed attacker at end of combat = %s, want graveyard (not sacrificed)", got)
	}
	if hasDecayedPromise(e, decayed) {
		t.Fatalf("decayed sacrifice registration not consumed: %+v", e.G.Delayed)
	}
}

// TestDecayedDoesNotSacrificeANonDecayedAttacker is the negative control: an
// ordinary attacker must NOT be sacrificed at end of combat. Without it a
// build that sacrificed every attacker at EndCombat would still pass
// TestDecayedCounterGrantsBothRules.
func TestDecayedDoesNotSacrificeANonDecayedAttacker(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 975, "Name:Placeholder\nManaCost:0\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	defender := onBoard(t, e, 1, "Name:Defender Bear\nManaCost:G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	plain := onBoardReady(t, e, 0, "Name:Plain Attacker\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Obj(defender).IsAttacking, e.G.Obj(defender).Attacking = true, 0

	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, plain)
	// Precondition: no Decayed promise should exist for a plain attacker.
	if hasDecayedPromise(e, plain) {
		t.Fatal("a plain attacker armed a Decayed sacrifice promise")
	}
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEndCombat)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(plain).Zone; got != state.ZBattlefield {
		t.Fatalf("plain attacker at end of combat = %s, want battlefield (the sacrifice must be Decayed-only)", got)
	}
}

// hasDecayedPromise reports whether a live __kwDecayedSacrifice registration
// names id (the incarnation-tracked promise rules/decayed.go arms).
func hasDecayedPromise(e *Engine, id state.ObjID) bool {
	for i := range e.G.Delayed {
		dt := &e.G.Delayed[i]
		if dt.Source == id && dt.Execute == "__kwDecayedSacrifice" {
			return true
		}
	}
	return false
}

// TestDecayedCorpusCensus names the mechanism class corpus-wide. Only
// Rot-Curse Rakshasa and the decayed token script print K:Decayed; the other
// five carriers reach the keyword at runtime (Gisa's and Ghouls' Night Out's
// KW$ Decayed grants) or filter on it (Wilhelt's withoutDecayed, Jadar's
// withDecayed). All three shapes must be present and wired.
func TestDecayedCorpusCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// The printed carriers: the card face and the token script.
	printed := map[string]bool{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f.HasKeyword("Decayed") {
				printed[f.Name] = true
			}
		}
	}
	if !printed["Rot-Curse Rakshasa"] {
		t.Error("corpus face Rot-Curse Rakshasa does not carry the printed Decayed keyword")
	}
	token, ok := reg.Tokens["b_2_2_zombie_decayed"]
	if !ok {
		t.Fatal("corpus has no b_2_2_zombie_decayed token script")
	}
	tokenHas := false
	for _, f := range token.Faces {
		if f.HasKeyword("Decayed") {
			tokenHas = true
		}
	}
	if !tokenHas {
		t.Error("the b_2_2_zombie_decayed token script does not carry Decayed")
	}

	// The runtime-grant carriers: a KW$ Decayed on an ability's body is the
	// "also affected" class a printed expansion would never cover (Gisa,
	// Ghouls' Night Out).
	grants := map[string]bool{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			for _, sa := range f.Abilities {
				for _, v := range sa.Params {
					if v == "Decayed" {
						grants[f.Name] = true
					}
				}
			}
			for _, body := range f.SVars {
				if strings.Contains(body, "KW$ Decayed") {
					grants[f.Name] = true
				}
			}
		}
	}
	for _, name := range []string{"Gisa, Glorious Resurrector", "Ghouls' Night Out"} {
		if !grants[name] {
			t.Errorf("corpus carrier %q no longer carries a KW$ Decayed grant", name)
		}
	}

	// The filter-predicate carriers: the with/without family the effects
	// filter list must register, or Wilhelt and Jadar match nothing.
	filters := map[string]bool{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			for _, t := range f.Triggers {
				for _, v := range t.Params {
					if strings.Contains(v, "withDecayed") || strings.Contains(v, "withoutDecayed") {
						filters[f.Name] = true
					}
				}
			}
		}
	}
	for _, name := range []string{"Wilhelt, the Rotcleaver", "Jadar, Ghoulcaller of Nephalia"} {
		if !filters[name] {
			t.Errorf("corpus carrier %q no longer carries a with/withoutDecayed filter", name)
		}
	}

	// The counter path's classifier (CR 122.1b).
	if _, ok := cards.CounterKeyword("Decayed"); !ok {
		t.Fatal("cards.CounterKeyword does not classify Decayed (CR 122.1b)")
	}

	// The ticket's stated goal: kw:Decayed was Rot-Curse Rakshasa's ONLY
	// unsupported primitive, so registering it must leave the card fully
	// supported. This is the assertion that retires the coverage gap.
	rot, ok := reg.Lookup("Rot-Curse Rakshasa")
	if !ok {
		t.Fatal("corpus has no Rot-Curse Rakshasa")
	}
	if miss := reg.Unsupported(rot, effects.Supported()); len(miss) > 0 {
		t.Errorf("Rot-Curse Rakshasa still has unsupported primitives: %v", miss)
	}
}
