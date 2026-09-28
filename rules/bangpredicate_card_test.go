package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestBangPredicateUnlocksTargeting is the gameplay-visible leaf for the
// leading-'!' negation grammar: a real corpus card whose printed behaviour
// was broken when `Creature.!attacking+!blocking` matched NOTHING.
//
// Unlikely Alliance reads:
//
//	{1}{W}: Target nonattacking, nonblocking creature gets +0/+2 until end of
//	turn.
//
// Its compiled ability is `A:AB$ Pump | Cost$ 1 W | ValidTgts$
// Creature.!attacking+!blocking`. With '!' unrecognised (it was only a
// literal character inside the one hand-written "!token" map entry), both
// predicates matched NOTHING, so the ability had zero legal targets and was
// withheld entirely -- "{1}{W}: target a nonattacking, nonblocking creature"
// could never be activated. With the negation implemented, the ability is
// offered and offers exactly the creature that is neither attacking nor
// blocking; the attacking Bear and the source enchantment must never be
// offered.
func TestBangPredicateUnlocksTargeting(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	alliance := mustCorpusCard(t, reg, "Unlikely Alliance") // {1}{W}: target nonattacking, nonblocking creature
	bearRest := mustCorpusCard(t, reg, "Grizzly Bears")
	bearAttack := mustCorpusCard(t, reg, "Grizzly Bears")

	cfg := Config{Seed: 3, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{alliance, bearRest, bearAttack}, mountainDeck(t, 37)...),
			mountainDeck(t, 40),
		},
	}
	e := New(cfg)
	e.Advance()

	var srcID, restID, attackID state.ObjID
	for _, c := range []*cards.Card{alliance, bearRest, bearAttack} {
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			// bearRest and bearAttack are one *cards.Card pointer (the registry
			// dedups by name), so guard on the zone to place two DISTINCT
			// objects rather than re-matching the first one already moved.
			if o.Owner == 0 && o.Card == c && o.Zone != state.ZBattlefield {
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
				if c == alliance {
					srcID = o.ID
				} else if restID == 0 {
					restID = o.ID
				} else {
					attackID = o.ID
				}
				break
			}
		}
	}
	if srcID == 0 || restID == 0 || attackID == 0 {
		t.Fatalf("setup failed to place three distinct objects: src=%d rest=%d attack=%d", srcID, restID, attackID)
	}

	// An enchantment is unaffected by summoning sickness, so no SummonSick
	// clearing is needed; the active ability only needs its {1}{W} cost. Mark
	// one Bear as attacking directly -- a fixture assignment, exactly the way
	// combat_test drives combat state -- so it must be excluded by !attacking.
	e.G.Obj(attackID).IsAttacking = true

	// Fund {1}{W} and drive to the priority decision.
	addMana(t, e, 0, "CW")
	opt := abilityOption(t, e, srcID, 0)
	submitChoices(t, e, opt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a KTarget decision after activating, got %+v", d)
	}
	restIdx, offeredAttack, offeredSrc := -1, false, false
	for _, o := range d.Options {
		switch o.Obj {
		case restID:
			restIdx = o.Index
		case attackID:
			offeredAttack = true
		case srcID:
			offeredSrc = true
		}
	}
	if restIdx < 0 {
		t.Fatalf("the nonattacking, nonblocking Grizzly Bears must be offered as a target: %+v", d.Options)
	}
	if offeredAttack {
		t.Fatalf("the attacking Grizzly Bears must not be offered (it is attacking): %+v", d.Options)
	}
	if offeredSrc {
		t.Fatalf("Unlikely Alliance (an enchantment) must not be offered as its own target: %+v", d.Options)
	}

	// Complete the activation and let the pump resolve; the target stays on
	// the battlefield (nothing destroyed it) and the ability did not wedge.
	submitChoices(t, e, restIdx)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(restID).Zone; z != state.ZBattlefield {
		t.Fatalf("pumped Grizzly Bears zone = %v, want battlefield (the +0/+2 pump resolved)", z)
	}
}
