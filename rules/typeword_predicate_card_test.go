package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestTypeWordPredicateUnlocksCorpusTargeting is the gameplay-visible leaf:
// a real corpus card whose printed behaviour the missing positive type-word
// predicate broke, driven through the engine.
//
// Voltaic Construct reads:
//
//	{2}: Untap target artifact creature.
//
// Its compiled ability is `A:AB$ Untap | Cost$ 2 | ValidTgts$ Creature.Artifact`.
// With the positive type-word predicate path in place, the `Artifact`
// predicate (a type word after the base `Creature`) evaluates as hasType, so
// the ability offers exactly the artifact creatures as targets and is offered
// at all. Without it the `Creature.Artifact` spec matched NOTHING, so
// abilityTargetsAvailable saw zero legal targets and withheld the ability
// entirely -- `{2}: Untap target artifact creature` could never be activated.
// The non-artifact Grizzly Bears (a creature, but not an Artifact) must never
// be offered.
func TestTypeWordPredicateUnlocksCorpusTargeting(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	vc := mustCorpusCard(t, reg, "Voltaic Construct") // {2}: Untap target artifact creature.
	bear := mustCorpusCard(t, reg, "Grizzly Bears")   // a creature, but not an artifact
	armor := card(t, "Name:Armor\nManaCost:2\nTypes:Artifact Creature Golem\nPT:1/1\nOracle:x\n")

	cfg := Config{Seed: 3, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{vc, bear, armor}, mountainDeck(t, 37)...),
			mountainDeck(t, 40),
		},
	}
	e := New(cfg)
	e.Advance()

	// Place all three on the battlefield with logged moves. The object's own
	// zone at genesis is the library; MoveZone's apply does the mutation.
	ids := map[string]state.ObjID{}
	for _, c := range []*cards.Card{vc, bear, armor} {
		name := c.Faces[0].Name
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Owner == 0 && o.Card == c {
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
				ids[name] = o.ID
				break
			}
		}
	}
	vcID, bearID, armorID := ids["Voltaic Construct"], ids["Grizzly Bears"], ids["Armor"]
	if vcID == 0 || bearID == 0 || armorID == 0 {
		t.Fatalf("setup failed to place all three: %+v", ids)
	}

	// Fund {2} and drive to a fresh main-1 priority decision. Voltaic
	// Construct's cost is {2} (no tap), so summoning sickness does not gate
	// the activation.
	addMana(t, e, 0, "CC")

	// Before the fix the ability is not offered at all (no legal target for
	// Creature.Artifact). With the fix it is.
	opt := abilityOption(t, e, vcID, 0)
	submitChoices(t, e, opt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a KTarget decision after activating, got %+v", d)
	}
	artifactIdx, offeredBear := -1, false
	for _, o := range d.Options {
		if o.Obj == armorID {
			artifactIdx = o.Index
		}
		if o.Obj == bearID {
			offeredBear = true
		}
	}
	if artifactIdx < 0 {
		t.Fatalf("the artifact creature (Armor) must be offered as a target: %+v", d.Options)
	}
	if offeredBear {
		t.Fatalf("the non-artifact Grizzly Bears must not be offered as a target: %+v", d.Options)
	}

	// Complete the activation so the engine's replay log is a full, valid game.
	submitChoices(t, e, artifactIdx)
	passUntilStackEmpty(t, e, 20)
	replayCheck(t, e, cfg)
}
