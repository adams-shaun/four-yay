// Focused tests for the attacker shapes this ticket adds: a typed attacker
// (a Wolf beside Tolsimir, Midnight's Light), a suspected attacker (Frantic
// Scapegoat cast ahead of the attack for Clandestine Meddler), and the two
// extra pass pairs a sibling cast trigger (Scolding Administrator's Repartee)
// spends before the death reaches the stack.
package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestTriggerAttackAttackerShapes(t *testing.T) {
	reg := loadGenRegistry(t)
	t.Run("Tolsimir wolf attacker", func(t *testing.T) {
		const name = "Tolsimir, Midnight's Light"
		card, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("precondition: %s not in the corpus", name)
		}
		// Precondition: the trigger really requires a Wolf to attack.
		if !strings.Contains(strings.ToLower(card.Faces[0].Triggers[1].ParamStr(cards.PKValidCard)), "wolf") {
			t.Fatalf("precondition: %s ValidCard$ = %q, want a Wolf filter", name, card.Faces[0].Triggers[1].ParamStr(cards.PKValidCard))
		}
		it := triggerItem(t, reg, name, "trigger#0.1")
		atk := attackStep(t, it.Scenario.Steps)
		if len(atk.Attackers) != 2 {
			t.Fatalf("%s: attackers %v, want the source and the Wolf", name, atk.Attackers)
		}
		wolf := false
		for _, a := range atk.Attackers {
			if c, ok := reg.Lookup(strings.TrimPrefix(a, "p0:")); ok {
				for _, ty := range c.Faces[0].Types {
					wolf = wolf || ty == "Wolf"
				}
			}
		}
		if !wolf {
			t.Fatalf("%s: attackers %v include no Wolf", name, atk.Attackers)
		}
	})
	t.Run("Clandestine Meddler suspect attacker", func(t *testing.T) {
		const name = "Clandestine Meddler"
		card, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("precondition: %s not in the corpus", name)
		}
		// Precondition: the trigger really requires a suspected attacker.
		if !strings.Contains(strings.ToLower(card.Faces[0].Triggers[1].ParamStr(cards.PKValidAttackers)), "issuspected") {
			t.Fatalf("precondition: %s ValidAttackers$ = %q, want IsSuspected", name, card.Faces[0].Triggers[1].ParamStr(cards.PKValidAttackers))
		}
		it := triggerItem(t, reg, name, "trigger#0.1")
		atk := attackStep(t, it.Scenario.Steps)
		// The suspect enters by being cast: the scapegoat is in the cause's
		// hand and its cast precedes the attack.
		suspectCast := false
		for _, st := range it.Scenario.Steps {
			if st.Op == "cast" && st.Card == "p0:"+suspectProbe {
				suspectCast = true
			}
		}
		joined := false
		for _, a := range atk.Attackers {
			joined = joined || a == "p0:"+suspectProbe
		}
		if !suspectCast || !joined {
			t.Fatalf("%s: suspect cast=%t, attackers %v joined=%t", name, suspectCast, atk.Attackers, joined)
		}
	})
	t.Run("Scolding Administrator sibling-delayed death", func(t *testing.T) {
		const name = "Scolding Administrator"
		card, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("precondition: %s not in the corpus", name)
		}
		// Precondition: the sibling Repartee trigger really fires on the
		// destroy probe's cast, which is why the plain variants miss the death.
		if len(card.Faces[0].Triggers) < 2 || card.Faces[0].Triggers[0].ModeKind().String() != "SpellCast" {
			t.Fatalf("precondition: %s sibling trigger 0 is %v, want a SpellCast", name, card.Faces[0].Triggers[0].ModeKind())
		}
		it := triggerDelayedDeathItem(t, reg, name, "trigger#0.1")
		// The cause is the destroy probe: a cast of Murder at the source with
		// its setup counter, then the resolves that empty the stack.
		kill := false
		for _, st := range it.Scenario.Steps {
			if st.Op == "cast" && strings.Contains(st.Card, "p0:Murder") && len(st.Targets) == 1 && st.Targets[0] == "p0:"+name {
				kill = true
			}
		}
		if !kill {
			t.Fatalf("%s: no Murder at the source: %v", name, it.Scenario.Steps)
		}
		if countName(it.Scenario.Setup["p0"].Battlefield, name) != 1 {
			t.Fatalf("%s: source not on its battlefield once: %v", name, it.Scenario.Setup["p0"].Battlefield)
		}
	})
}

// triggerDelayedDeathItem serves one trigger requirement whose fire is hidden
// behind a sibling trigger's resolution: the visible point needs four passes
// after the cause's cast, not the item's own resolve steps (the runner's
// resolve op empties the whole stack, the sibling included).
func triggerDelayedDeathItem(t *testing.T, reg *cards.Registry, name, key string) oraclegen.Item {
	t.Helper()
	card, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	f := card.Faces[0]
	for _, req := range levelb.Requirements(card) {
		if req.Family != "trigger" || req.Key != key {
			continue
		}
		it, skip := GenerateB(reg, name, req)
		if skip != nil {
			t.Fatalf("%s %s skipped: %s", name, key, skip.Reason)
		}
		steps := append([]oraclegen.Step(nil), it.Scenario.Steps...)
		for len(steps) > 0 && steps[len(steps)-1].Op == "resolve" {
			steps = steps[:len(steps)-1]
		}
		sc := it.Scenario
		sc.Steps = append(steps,
			oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1},
			oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1})
		_, res, ok := oraclegen.Settle(reg, sc)
		if !ok {
			t.Fatalf("%s %s: gorge cannot play the served scenario with pass pairs", name, key)
		}
		if !abilityOnStack(res.Snapshots, stackSourceWants(reg, name, f), req.Slot) {
			t.Fatalf("%s %s: the trigger ability is never on the stack", name, key)
		}
		return it
	}
	t.Fatalf("precondition: %s has no requirement %s", name, key)
	return oraclegen.Item{}
}
