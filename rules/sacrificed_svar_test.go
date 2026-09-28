package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The Task sac1 leaf tests. Each card under test is an authored fixture (the
// licensing rule forbids copying a Forge .txt into the repo); each mirrors the
// real compiled corpus card's ability shape and its SVar exactly, and the
// Sacrificed$ head being exercised is the part under test.

// sacGisa reproduces Ghoulcaller Gisa's activated ability: {B}, {T}, sacrifice
// another creature, create X 2/2 zombies where X is the sacrificed creature's
// power. The TokenScript$ stem is this test's own fixture, and K:Haste is a
// test-only accommodation so the {T} cost is legally activatable on the
// turn the creature enters (CR 302.6) without directly clearing SummonSick
// (which would break the log-only replay fidelity check).
const sacGisa = "Name:Ghoulcaller Gisa\nManaCost:3 B B\nTypes:Legendary Creature Human Wizard\nPT:3/4\nK:Haste\n" +
	"A:AB$ Token | Cost$ B T Sac<1/Creature.Other/another creature> | TokenAmount$ X | TokenScript$ sac1_zombie | TokenOwner$ You | SpellDescription$ x\n" +
	"SVar:X:Sacrificed$CardPower\n" +
	"Oracle:x\n"

const sac1Zombie = "Name:Zombie Token\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n"

func countTokensNamed(t *testing.T, e *Engine, name string) int {
	t.Helper()
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == name {
			n++
		}
	}
	return n
}

// sacrificeOption finds the "sacrifice" option for obj in the pending KChoose,
// fatal if absent.
func sacrificeOption(t *testing.T, d *decision.Decision, obj state.ObjID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "sacrifice" && o.Obj == obj {
			return o.Index
		}
	}
	t.Fatalf("no sacrifice option for obj %d: %+v", obj, d.Options)
	return -1
}

// TestGhoulcallerGisaCreatesTokensForSacrificedPower is the leaf for the
// reported bug: the sacrificed creature's power must size the token count, at
// two different powers, so the old "always zero" defect cannot pass by
// coincidence on a single value. Sacrificing a 1/1 makes one zombie;
// sacrificing a 4/4 makes four.
func TestGhoulcallerGisaCreatesTokensForSacrificedPower(t *testing.T) {
	const bear = "Name:Forest Bear\nManaCost:G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n"
	const elephant = "Name:War Elephant\nManaCost:2 G\nTypes:Creature Elephant\nPT:4/4\nOracle:x\n"
	for _, tc := range []struct {
		name       string
		sacSrc     string
		wantTokens int
	}{
		{name: "one_1_1", sacSrc: bear, wantTokens: 1},
		{name: "four_4_4", sacSrc: elephant, wantTokens: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, gisa := newFixtureDeck(t, 77, sacGisa, tc.sacSrc)
			moveSeeded(t, e, 0, sacGisa, state.ZBattlefield)
			creature := moveSeeded(t, e, 0, tc.sacSrc, state.ZBattlefield)
			e.G.Tokens["sac1_zombie"] = card(t, sac1Zombie)
			addMana(t, e, 0, "B")
			submitChoices(t, e, abilityOption(t, e, gisa, 0).Index)
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose {
				t.Fatalf("after activating Gisa: %+v, want a KChoose sacrifice decision", d)
			}
			submitChoices(t, e, sacrificeOption(t, d, creature))
			passUntilStackEmpty(t, e, 20)
			if got := countTokensNamed(t, e, "Zombie Token"); got != tc.wantTokens {
				t.Fatalf("sacrificing a %s made %d zombies, want %d", tc.name, got, tc.wantTokens)
			}
			if o := e.G.Obj(creature); o == nil || o.Zone != state.ZGraveyard {
				t.Fatalf("sacrificed creature zone = %v, want graveyard", o.Zone)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// sacMorbidCuriosity reproduces Morbid Curiosity's spell: an additional
// sacrifice of an artifact or creature, then draw cards equal to the
// sacrificed permanent's mana value (SVar:X:Sacrificed$CardManaCost).
const sacMorbidCuriosity = "Name:Morbid Curiosity\nManaCost:1 B B\nTypes:Sorcery\n" +
	"A:SP$ Draw | Cost$ 1 B B Sac<1/Artifact;Creature/artifact or creature> | NumCards$ X | SpellDescription$ x\n" +
	"SVar:X:Sacrificed$CardManaCost\n" +
	"Oracle:x\n"

// TestSacrificedCardManaCostDrawsMatchingCards pins the CardManaCost head:
// a mana-value-3 creature (1 G G) makes the spell draw exactly 3 cards.
func TestSacrificedCardManaCostDrawsMatchingCards(t *testing.T) {
	t.Parallel()
	const grizzly = "Name:Grizzly\nManaCost:1 G G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n" // mana value 3
	e, cfg, spell := newFixtureDeck(t, 971, sacMorbidCuriosity, grizzly)
	creature := moveSeeded(t, e, 0, grizzly, state.ZBattlefield)
	addMana(t, e, 0, "1BB")
	before := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("after proposing the spell: %+v, want a KChoose sacrifice decision", d)
	}
	submitChoices(t, e, sacrificeOption(t, d, creature))
	if td := e.Pending(); td != nil && td.Kind == decision.KTarget {
		submitChoices(t, e, td.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 20)
	// The spell itself was in hand at `before`; casting it leaves the hand, then
	// the draw adds back the sacrificed creature's mana value (3).
	if got := len(e.G.Zone(state.ZHand, 0)); got != before-1+3 {
		t.Fatalf("hand went from %d to %d, want %d (sacrificed a mana-value-3 creature, so draw 3)",
			before, got, before-1+3)
	}
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("sacrificed creature zone = %v, want graveyard", o.Zone)
	}
	replayCheck(t, e, cfg)
}

// sacAmountDraw reproduces a sacrifice-cost spell that counts what it
// sacrificed: sacrifice two creatures, then draw that many cards
// (SVar:Y:Sacrificed$Amount). It is the Amount leaf -- the corpus's natural
// user (Vicious Betrayal) wraps the count in an unimplemented SVar$Y/Times.2
// indirection, so this authored fixture consumes the Amount head directly.
const sacAmountDraw = "Name:Flesh Feast\nManaCost:1 B\nTypes:Sorcery\n" +
	"A:SP$ Draw | Cost$ 1 B Sac<2/Creature> | NumCards$ Y | SpellDescription$ x\n" +
	"SVar:Y:Sacrificed$Amount\n" +
	"Oracle:x\n"

// TestSacrificedAmountCountsSacrificedObjects: sacrificing two creatures
// makes Sacrificed$Amount read 2, so the spell draws exactly two cards.
func TestSacrificedAmountCountsSacrificedObjects(t *testing.T) {
	t.Parallel()
	const cub = "Name:Cub\nManaCost:G\nTypes:Creature Wolf\nPT:2/2\nOracle:x\n"
	const rat = "Name:Rat\nManaCost:B\nTypes:Creature Rat\nPT:1/1\nOracle:x\n"
	e, cfg, spell := newFixtureDeck(t, 214, sacAmountDraw, cub, rat)
	c1 := moveSeeded(t, e, 0, cub, state.ZBattlefield)
	c2 := moveSeeded(t, e, 0, rat, state.ZBattlefield)
	addMana(t, e, 0, "1B")
	before := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 2 || d.Max != 2 {
		t.Fatalf("after proposing the spell: %+v, want a 2-of KChoose sacrifice decision", d)
	}
	// Answer the two-object choice in the engine's offered order.
	for d != nil && d.Kind == decision.KChoose && d.Min == 2 && d.Max == 2 {
		submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
		d = e.Pending()
	}
	if d != nil && d.Kind == decision.KTarget {
		submitChoices(t, e, d.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 20)
	// The spell was in hand at `before`; casting it leaves the hand, then the
	// draw adds back the two sacrificed creatures.
	if got := len(e.G.Zone(state.ZHand, 0)); got != before-1+2 {
		t.Fatalf("hand went from %d to %d, want %d (sacrificed 2 creatures, so draw 2)",
			before, got, before-1+2)
	}
	for _, id := range []state.ObjID{c1, c2} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("sacrificed creature %d zone = %v, want graveyard", id, o.Zone)
		}
	}
	replayCheck(t, e, cfg)
}
