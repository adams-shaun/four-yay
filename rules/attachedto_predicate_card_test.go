package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestAttachedToPredicateUnlocksCorpusTargeting is the gameplay-visible leaf
// for the two-token space grammar "AttachedTo <X>": a real corpus card whose
// printed behaviour the missing predicate broke, driven through the engine.
//
// Devout Harpist reads:
//
//	{T}: Destroy target Aura attached to a creature.
//
// Its compiled ability is `A:AB$ Destroy | Cost$ T | ValidTgts$
// Aura.AttachedTo Creature`. With the whole "AttachedTo Creature" token
// failing closed as one unknown predicate (a space is not a spec delimiter,
// so the token survives intact -- the pg1 verdict), the filter matched
// NOTHING, so abilityTargetsAvailable saw zero legal targets and withheld the
// ability entirely -- "{T}: destroy target Aura attached to a creature" could
// never be activated. With the two-token grammar in place the ability is
// offered and names exactly the Aura attached to a creature; the Aura
// attached to a land (a legal attachment, so it survives to be offered) and
// the Harpist itself must never be offered.
func TestAttachedToPredicateUnlocksCorpusTargeting(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	harpist := mustCorpusCard(t, reg, "Devout Harpist")       // {T}: Destroy target Aura attached to a creature.
	bear := mustCorpusCard(t, reg, "Grizzly Bears")           // a creature
	auraCreature := mustCorpusCard(t, reg, "Unholy Strength") // K:Enchant:Creature
	auraLand := mustCorpusCard(t, reg, "Wild Growth")         // K:Enchant:Land

	cfg := Config{Seed: 3, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{harpist, bear, auraCreature, auraLand}, mountainDeck(t, 36)...),
			mountainDeck(t, 40),
		},
	}
	e := New(cfg)
	e.Advance()

	// Move seat 0's Harpist, a Bear, a creature-Aura, a land-Aura and a
	// Mountain onto the battlefield with logged MoveZone events, so a
	// log-only replay reproduces the same objects. Each move picks the first
	// still-in-a-hidden-zone object the predicate names.
	move := func(pred func(*state.Object) bool) state.ObjID {
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Owner != 0 || (o.Zone != state.ZLibrary && o.Zone != state.ZHand) {
				continue
			}
			if pred(o) {
				id := o.ID
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZBattlefield})
				return id
			}
		}
		return 0
	}
	harpistID := move(func(o *state.Object) bool { return o.Card == harpist })
	bearID := move(func(o *state.Object) bool { return o.Card == bear })
	auraOnBear := move(func(o *state.Object) bool { return o.Card == auraCreature })
	auraOnLand := move(func(o *state.Object) bool { return o.Card == auraLand })
	mountainID := move(func(o *state.Object) bool { return o.Face() != nil && o.Face().Name == "Mountain" })
	if harpistID == 0 || bearID == 0 || auraOnBear == 0 || auraOnLand == 0 || mountainID == 0 {
		t.Fatalf("setup failed to place objects: harpist=%d bear=%d auraOnBear=%d auraOnLand=%d mountain=%d",
			harpistID, bearID, auraOnBear, auraOnLand, mountainID)
	}

	// The Harpist's {T} cost needs no mana, but as a creature it must be past
	// summoning sickness (CR 302.6) to tap. Clear it as if it had been under
	// its controller's control since the start of the turn.
	e.G.Obj(harpistID).SummonSick = false

	// Attach the creature-Aura to the Bear and the land-Aura to the Mountain.
	// Unholy Strength (Enchant creature) and Wild Growth (Enchant land) are
	// both legal attachments, so neither is removed by the Aura-legality SBA.
	e.emit(events.Event{Kind: events.Attach, Obj: auraOnBear, IDs: []state.ObjID{bearID}})
	e.emit(events.Event{Kind: events.Attach, Obj: auraOnLand, IDs: []state.ObjID{mountainID}})

	// Refresh the priority decision so it sees the objects just placed.
	addMana(t, e, 0, "")

	opt, ok := findAbilityOption(e, harpistID, 0)
	if !ok {
		t.Fatalf("Devout Harpist's {T} ability must be offered now that Aura.AttachedTo Creature has a legal target")
	}
	submitChoices(t, e, opt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a KTarget decision after activating, got %+v", d)
	}
	bearAuraIdx, offeredLandAura, offeredHarpist, offeredBear := -1, false, false, false
	for _, o := range d.Options {
		switch o.Obj {
		case auraOnBear:
			bearAuraIdx = o.Index
		case auraOnLand:
			offeredLandAura = true
		case harpistID:
			offeredHarpist = true
		case bearID:
			offeredBear = true
		}
	}
	if bearAuraIdx < 0 {
		t.Fatalf("the Aura attached to the Grizzly Bears must be offered as a target: %+v", d.Options)
	}
	if offeredLandAura {
		t.Fatalf("the Aura attached to the Mountain must not be offered (it is not attached to a creature): %+v", d.Options)
	}
	if offeredHarpist {
		t.Fatalf("Devout Harpist itself must not be offered as a target: %+v", d.Options)
	}
	if offeredBear {
		t.Fatalf("the Grizzly Bears must not be offered as a target (it is a creature, not an Aura): %+v", d.Options)
	}

	// Complete the activation: the Harpist taps, the target Aura is destroyed
	// and reaches the graveyard, and the game does not wedge. (No replayCheck
	// here: the summoning-sickness clear below is a direct fixture write on
	// the live game, not a logged event, so a log-only replay cannot
	// reproduce it -- the same reason the bangpredicate leaf, which also
	// drives state directly, stops at its own assertions.)
	submitChoices(t, e, bearAuraIdx)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(auraOnBear).Zone; z != state.ZGraveyard {
		t.Fatalf("destroyed Aura zone = %v, want graveyard (the Destroy effect resolved)", z)
	}
}
