package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// triggerFixtures lists the target fixtures a trigger's scenario is tried
// against. The first is always the empty one (the card and the cause alone),
// so a trigger that needs no setup keeps its scenario. When the trigger's
// effect chain has target slots, the ordinary Fixtures cross product follows
// (the same one an activated ability uses): a trigger whose mandatory target
// has no legal choice is removed from the stack (CR 603.3d), so the fire
// probe needs the objects the effect will target.
func triggerFixtures(reg *cards.Registry, f *cards.Face, t *cards.Trigger) []oraclegen.Fixture {
	out := []oraclegen.Fixture{{}}
	slots := oraclegen.AbilitySlotSpecs(f, t.Effect)
	if len(slots) == 0 {
		return out
	}
	return append(out, oraclegen.Fixtures(reg, slots)...)
}

// mergedFixtureSeats copies the fixture's two seats so a scenario can add the
// card under test and the cause's objects without touching the fixture.
func mergedFixtureSeats(fx *oraclegen.Fixture) (p0, p1 oraclegen.Seat) {
	return copySeat(*fx.P0()), copySeat(*fx.P1())
}

func copySeat(s oraclegen.Seat) oraclegen.Seat {
	dup := func(xs []string) []string { return append([]string(nil), xs...) }
	return oraclegen.Seat{
		Battlefield: dup(s.Battlefield), Tapped: dup(s.Tapped), Hand: dup(s.Hand),
		Graveyard: dup(s.Graveyard), Exile: dup(s.Exile), Library: dup(s.Library),
		LibraryTop: dup(s.LibraryTop),
	}
}

// removeString drops the first occurrence of name.
func removeString(xs []string, name string) []string {
	for i, x := range xs {
		if x == name {
			return append(xs[:i:i], xs[i+1:]...)
		}
	}
	return xs
}

// triggerSteps is the cause's steps as the scenario plays them: the card's own
// X cast when the cause asks for it, then fixtureSteps.
func triggerSteps(f *cards.Face, name string, c triggerCause, steps []oraclegen.Step, fx *oraclegen.Fixture) []oraclegen.Step {
	var pre []oraclegen.Step
	if c.castSelfX {
		if pool, why := oraclegen.PoolFor(f.ManaCost); why == "" {
			pre = []oraclegen.Step{
				{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: pool, Answers: xAnswers(f)},
				{Op: "resolve"},
			}
		}
	}
	return append(pre, fixtureSteps(fx, steps)...)
}

// castXCreature casts a creature with X in its cost (it enters with X
// counters, so setup would leave it 0/0) rather than placing it.
func castXCreature(f *cards.Face, c *triggerCause) bool {
	c.castSelfX = f.IsCreature() && strings.Contains(" "+f.ManaCost+" ", " X ")
	return c.castSelfX
}

// islandsForStarPT puts an Island beside a creature whose power and toughness
// are defined by a count (*/*): the common count is Islands, and with none the
// creature would die as it enters.
func islandsForStarPT(f *cards.Face, c *triggerCause) bool {
	if !f.IsCreature() || !strings.Contains(f.PT, "*") {
		return false
	}
	c.battlefield = append(append([]string(nil), c.battlefield...), "Island")
	return true
}

// fixtureSteps puts the fixture's prelude (an Equipment attached, an Aura
// enchanted) ahead of the cause's steps and adds the fixture's p0 attackers
// to the cause's own attack, so a trigger whose target is "another attacking
// creature" has that creature attacking beside the card. A cause with no
// attack step, and an empty fixture, return the steps unchanged.
func fixtureSteps(fx *oraclegen.Fixture, steps []oraclegen.Step) []oraclegen.Step {
	out := append(append([]oraclegen.Step(nil), fx.Prelude()...), steps...)
	combat := fx.CombatSteps()
	if len(combat) == 0 || combat[0].Seat != 0 {
		return out
	}
	for i := range out {
		if out[i].Op != "attack" || out[i].Seat != 0 {
			continue
		}
		atk := append([]string(nil), out[i].Attackers...)
		for _, a := range combat[0].Attackers {
			atk = appendFixtureUnique(atk, a)
		}
		out[i].Attackers = atk
	}
	return out
}
