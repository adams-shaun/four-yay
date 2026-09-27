package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Fix-round regression pins for the AddsCounters$ mana-spend rider (task
// opalp), answering the two MAJORs on the previous round:
//
//  1. The captured provenance deduplicated by SOURCE, so two rider mana units
//     spent from one permanent yielded one grant. The capture is now per
//     producing ABILITY and per spent unit (state.ManaAddsCounterGrant).
//  2. The entry plan re-read the source's CURRENT face, so a different,
//     rider-bearing ability of the same permanent wrongly awarded counters
//     and a face change between payment and entry could gain or lose the
//     rider. The rider is now snapshotted at production and carried in the
//     batch provenance, so the grant is the producing ability's own.

// riderShamanSrc is a Biophagus-shaped mana creature: "{T}: Add {R}. If this
// mana is spent to cast a creature spell, that creature enters with an
// additional +1/+1 counter on it." A fixed Produced$ R keeps the activation
// decision-free (no colour ask).
const riderShamanSrc = `Name:Rider Shaman
ManaCost:1 G
Types:Creature Elf Shaman
PT:1/3
A:AB$ Mana | Cost$ T | Produced$ R | AddsCounters$ Card.Creature_P1P1_1 | SpellDescription$ Add {R}. If this mana is spent to cast a creature spell, that creature enters with an additional +1/+1 counter on it.
Oracle:x
`

// riderDoubleSrc costs {R}{R} so BOTH rider units produced by one Rider Shaman
// (untapped between activations) are spent on the one cast.
const riderDoubleSrc = `Name:Double Red
ManaCost:R R
Types:Creature Goblin
PT:1/1
Oracle:x
`

// riderTotemSrc has TWO mana abilities: the {C} one carries a RestrictValid$
// provenance but NO AddsCounters$, the {R} one carries the rider. A source-
// only capture would import the rider from the second ability when the first
// ability's mana paid; an ability-level snapshot must not.
const riderTotemSrc = `Name:Dual Totem
ManaCost:2
Types:Artifact
A:AB$ Mana | Cost$ T | Produced$ C | RestrictValid$ Spell | SpellDescription$ Add {C}. Spend this mana only to cast a spell.
A:AB$ Mana | Cost$ T | Produced$ R | AddsCounters$ Card.Creature_P1P1_1 | SpellDescription$ Add {R}. If this mana is spent to cast a creature spell, that creature enters with an additional +1/+1 counter on it.
Oracle:x
`

// riderGenericSrc costs {1}, payable by the Dual Totem's restricted {C}.
const riderGenericSrc = `Name:Generic Beast
ManaCost:1
Types:Creature Beast
PT:2/2
Oracle:x
`

// riderGame builds a two-seat constructed game whose seat 0 deck leads with
// the given fixtures and is driven to seat 0's Main1.
func riderGame(t *testing.T, seed uint64, seat0 ...*cards.Card) (*Engine, Config) {
	t.Helper()
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(seat0, mountainDeck(t, 40-len(seat0))...),
			mountainDeck(t, 40),
		},
		Tokens: testutil.CorpusRegistry(t).Tokens,
	}
	e := New(seatZeroStart(cfg))
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	e.priorityRound()
	return e, cfg
}

// riderManaAbilityIndex returns the index (in availableManaAbilities order) of
// the mana ability on obj whose AddsCounters$ presence matches want, so a
// fixture with two abilities can select the exact one.
func riderManaAbilityIndex(t *testing.T, e *Engine, obj state.ObjID, want bool) int {
	t.Helper()
	mas := e.availableManaAbilities(0, obj)
	for i, ma := range mas {
		if (strings.TrimSpace(ma.Params["AddsCounters"]) != "") == want {
			return i
		}
	}
	t.Fatalf("precondition: no mana ability on %d with AddsCounters presence = %v (%d abilities)", obj, want, len(mas))
	return -1
}

// activateRiderMana activates the mana ability at ability index ai on obj and
// answers the mana-ability wheel with that exact option. A no-choice ability
// resolves without a further decision.
func activateRiderMana(t *testing.T, e *Engine, obj state.ObjID, ai int) {
	t.Helper()
	e.priorityRound()
	activateMana(t, e, obj)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		return
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "mana" && o.Ability == ai {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no mana-ability option for ability %d on %d: %+v", ai, obj, d.Options)
	}
	submitChoices(t, e, idx)
}

// untapRider untaps an object through a logged event (a legitimate untap
// effect), so the same permanent can activate its mana ability twice before
// one payment.
func untapRider(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.Untap, Obj: obj})
	e.pending = nil
	e.Advance()
}

// TestAddsCountersRiderTwoUnitsOneSource is MAJOR 1's pin: two mana units
// produced by ONE rider ability and both spent on the same creature spell must
// place the rider twice. The old capture deduplicated by source and broke
// after one grant, so the creature entered with one counter instead of two;
// the brief's own finding spells this out for Biophagus and for Opal Palace.
func TestAddsCountersRiderTwoUnitsOneSource(t *testing.T) {
	e, cfg := riderGame(t, 501, card(t, riderShamanSrc), card(t, riderDoubleSrc))
	shaman := moveToBattlefieldByName(t, e, 0, "Rider Shaman")
	goblin := moveSeededToHand(t, e, 0, "Double Red")

	// Preconditions: the shaman is on the battlefield untapped, the cast card
	// is in hand, and its cost really needs the two units.
	so := e.G.Obj(shaman)
	if so == nil || so.Zone != state.ZBattlefield || so.Tapped {
		t.Fatalf("precondition: Rider Shaman = %+v, want untapped on the battlefield", so)
	}
	goo := e.G.Obj(goblin)
	if goo == nil || goo.Zone != state.ZHand {
		t.Fatalf("precondition: Double Red zone = %v, want the hand", goo.Zone)
	}

	// Produce two rider units from the same source: activate, untap, activate.
	ai := riderManaAbilityIndex(t, e, shaman, true)
	activateRiderMana(t, e, shaman, ai)
	untapRider(t, e, shaman)
	activateRiderMana(t, e, shaman, ai)

	batches := e.G.Players[0].RestrictedMana
	if len(batches) != 2 {
		t.Fatalf("precondition: rider batches = %d, want 2 (two activations)", len(batches))
	}
	if e.G.Players[0].Pool[state.MR] != 2 {
		t.Fatalf("precondition: pool R = %d, want 2", e.G.Players[0].Pool[state.MR])
	}
	for i, b := range batches {
		if b.Source != shaman || strings.TrimSpace(b.AddsCounters) == "" {
			t.Fatalf("precondition: batch %d = %+v, want a rider batch sourced to %d", i, b, shaman)
		}
	}

	castSeeded(t, e, goblin)
	passUntilStackEmpty(t, e, 40)
	o := e.G.Obj(goblin)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Double Red zone = %v, want the battlefield", o.Zone)
	}
	if got := o.Counter("P1P1"); got != 2 {
		t.Fatalf("Double Red counters after both rider units paid = %d, want 2 (one per spent unit)", got)
	}
	if n := len(e.G.Players[0].RestrictedMana); n != 0 {
		t.Fatalf("rider batches after payment = %d, want 0 (both consumed)", n)
	}
	replayCheck(t, e, cfg)
}

// TestAddsCountersRiderIsTheProducingAbilityNotTheFace is MAJOR 2's pin. Dual
// Totem has a restricted (provenance-bearing) {C} ability with NO rider and a
// separate {R} ability with the rider. Spending the {C} ability's mana on a
// creature must NOT award counters even though the permanent's face does
// carry an AddsCounters$ ability: the grant belongs to the ability that
// produced the mana, not to the permanent. The positive half then spends the
// {R} rider ability's mana and DOES award the counter, proving the distinction
// is ability-level rather than a blanket suppression.
func TestAddsCountersRiderIsTheProducingAbilityNotTheFace(t *testing.T) {
	e, cfg := riderGame(t, 502, card(t, riderTotemSrc), card(t, riderGenericSrc), card(t, riderGenericSrc))
	totem := moveToBattlefieldByName(t, e, 0, "Dual Totem")
	beast := moveSeededToHand(t, e, 0, "Generic Beast")

	to := e.G.Obj(totem)
	if to == nil || to.Zone != state.ZBattlefield || to.Tapped {
		t.Fatalf("precondition: Dual Totem = %+v, want untapped on the battlefield", to)
	}
	// The face really does carry a rider on one ability, so a source-only
	// capture would have imported it on the no-rider ability's mana.
	noRider := riderManaAbilityIndex(t, e, totem, false)
	rider := riderManaAbilityIndex(t, e, totem, true)

	// Negative: spend the restricted, rider-LESS {C} on the creature.
	activateRiderMana(t, e, totem, noRider)
	if n := len(e.G.Players[0].RestrictedMana); n != 1 {
		t.Fatalf("precondition: provenance batches = %d, want 1", n)
	}
	if b := e.G.Players[0].RestrictedMana[0]; b.Source != totem || strings.TrimSpace(b.AddsCounters) != "" {
		t.Fatalf("precondition: consumed batch = %+v, want a rider-less batch sourced to %d", b, totem)
	}
	castSeeded(t, e, beast)
	passUntilStackEmpty(t, e, 40)
	o := e.G.Obj(beast)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Generic Beast zone = %v, want the battlefield", o.Zone)
	}
	if got := o.Counter("P1P1"); got != 0 {
		t.Fatalf("Generic Beast counters after rider-less ability's mana paid = %d, want 0", got)
	}

	// Positive: return a second copy of the creature and spend the rider
	// ability's {R} on it. Untap first.
	e.emit(events.Event{Kind: events.Untap, Obj: totem})
	e.pending = nil
	e.Advance()
	beast2 := moveSeededToHand(t, e, 0, "Generic Beast")
	if beast2 == beast {
		t.Fatalf("precondition: expected a second Generic Beast, got the same object %d", beast)
	}
	activateRiderMana(t, e, totem, rider)
	if n := len(e.G.Players[0].RestrictedMana); n != 1 {
		t.Fatalf("precondition: rider provenance batches = %d, want 1", n)
	}
	if b := e.G.Players[0].RestrictedMana[0]; b.Source != totem || strings.TrimSpace(b.AddsCounters) == "" {
		t.Fatalf("precondition: rider batch = %+v, want a rider batch sourced to %d", b, totem)
	}
	castSeeded(t, e, beast2)
	passUntilStackEmpty(t, e, 40)
	o2 := e.G.Obj(beast2)
	if o2.Zone != state.ZBattlefield {
		t.Fatalf("precondition: second Generic Beast zone = %v, want the battlefield", o2.Zone)
	}
	if got := o2.Counter("P1P1"); got != 1 {
		t.Fatalf("second Generic Beast counters after the rider ability's mana paid = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}
