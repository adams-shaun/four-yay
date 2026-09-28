package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The ulalek-eldrazi commander deck import's engine mechanics, pinned on
// fixture cards shaped like the real corpus carriers so the tests run
// without guessing at corpus drift:
//
//   - the and/or two-part Kicker (Wastescape Battlemage's "Kicker {G} and/or
//     {1}{U}", Forge's colon-separated Kicker:<a>:<b>) offers one cast per
//     independently payable part and flags each part on the cast, which is
//     what the "Card.Self+kicked <n>" trigger specs read;
//   - a MayPlay static's MayPlayAltManaCost$ (Darksteel Monolith's "pay {0}
//     rather than the mana cost") reaches the cast walk as an alternative
//     cost through mayPlayAltCosts;
//   - a ReduceCost static's OnlyFirstSpell$ (Conduit of Ruin) discounts only
//     the first covered spell each turn;
//   - TargetValidTargeting$ (Not of This World) holds a counter to targets
//     whose own targets match the spec.

const twoPartKickerSrc = "Name:VolcanicMage\nManaCost:2\nTypes:Creature Eldrazi Wizard\nPT:2/2\n" +
	"K:Kicker:G:1 U\n" +
	"T:Mode$ SpellCast | ValidCard$ Card.Self+kicked 1 | Execute$ TrigOne | TriggerDescription$ first kicker\n" +
	"T:Mode$ SpellCast | ValidCard$ Card.Self+kicked 2 | Execute$ TrigTwo | TriggerDescription$ second kicker\n" +
	"SVar:TrigOne:DB$ DealDamage | Defined$ Opponent | NumDmg$ 1\n" +
	"SVar:TrigTwo:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

// drainStack answers priority with pass and trigger_order with the offered
// indices in ascending order (the recorded mirror of the engine-side
// first-order stand-in) until the stack is empty.
func drainKickerStack(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for n := 0; n < limit && !e.G.Over && len(e.G.Stack) > 0; n++ {
		d := e.Pending()
		switch d.Kind {
		case decision.KTriggerOrder:
			choices := make([]int, 0, d.Max)
			for i := 0; i < d.Max && i < len(d.Options); i++ {
				choices = append(choices, d.Options[i].Index)
			}
			submitChoices(t, e, choices...)
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, idx)
		default:
			t.Fatalf("unexpected decision %+v while draining the stack", d)
		}
	}
}

func kickerModes(t *testing.T, e *Engine) []string {
	t.Helper()
	var modes []string
	for _, o := range castOptions(t, e) {
		modes = append(modes, o.Mode)
	}
	return modes
}

func TestTwoPartKickerOffersEachIndependentPart(t *testing.T) {
	t.Parallel()
	// {2} plus {G}: the plain cast and the first-kicker cast are affordable,
	// the second-kicker ({1}{U}) and both-parts casts are not.
	e, _, _ := newFixtureDeck(t, 44, twoPartKickerSrc)
	addMana(t, e, 0, "GGG")
	got := kickerModes(t, e)
	if len(got) != 2 || got[0] != "" || got[1] != "kicked1" {
		t.Fatalf("with {2}{G}: modes %v, want [\"\" kicked1]", got)
	}
	// {G} and {U}: all three part-shapes, never the old whole-string
	// "kicked" mode whose parse degraded the colon into a generic pip.
	addMana(t, e, 0, "U")
	got = kickerModes(t, e)
	if len(got) != 3 || got[1] != "kicked1" || got[2] != "kicked2" {
		t.Fatalf("with {2}{G}{U}: modes %v, want no kickedboth yet", got)
	}
	addMana(t, e, 0, "GG")
	got = kickerModes(t, e)
	if len(got) != 4 || got[3] != "kickedboth" {
		t.Fatalf("funded: modes %v, want all four shapes", got)
	}
	// The first-kicker cast pays {2}+{G} exactly and rides FlagKicked1 with
	// the bare FlagKicked (every part paid IS a kicked cast).
	e2, cfg2, id2 := newFixtureDeck(t, 45, twoPartKickerSrc)
	addMana(t, e2, 0, "GGG")
	opts := castOptions(t, e2)
	submitChoices(t, e2, opts[1].Index)
	if o := e2.G.Obj(id2); o.CastFlags&(state.FlagKicked|state.FlagKicked1) != state.FlagKicked|state.FlagKicked1 ||
		o.CastFlags&state.FlagKicked2 != 0 {
		t.Fatalf("kicked1 flags: %+v", o.CastFlags)
	}
	if got := e2.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("kicked1 pool after paying {2}{G}: %d", got)
	}
	drainKickerStack(t, e2, 30)
	if !hasEvent(e2, events.TriggerPush, id2) {
		t.Fatal("the kicked-1 trigger did not fire")
	}
	// The first kicker only: the opponent took 1, seat 0 gained nothing.
	if e2.G.Players[1].Life != 19 || e2.G.Players[0].Life != 20 {
		t.Fatalf("after kicked1: lives %d/%d, want 20/19", e2.G.Players[0].Life, e2.G.Players[1].Life)
	}
	replayCheck(t, e2, cfg2)
}

func TestTwoPartKickerBothPartsPayBothTriggers(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 46, twoPartKickerSrc)
	addMana(t, e, 0, "GGUUU")
	opts := castOptions(t, e)
	var both decision.Option
	found := false
	for _, o := range opts {
		if o.Mode == "kickedboth" {
			both, found = o, true
		}
	}
	if !found {
		t.Fatalf("no kickedboth option: %+v", opts)
	}
	submitChoices(t, e, both.Index)
	if o := e.G.Obj(id); o.CastFlags&(state.FlagKicked|state.FlagKicked1|state.FlagKicked2) !=
		state.FlagKicked|state.FlagKicked1|state.FlagKicked2 {
		t.Fatalf("kickedboth flags: %+v", o.CastFlags)
	}
	drainKickerStack(t, e, 30)
	// Both kickers: the opponent took 1 and seat 0 gained 1.
	if e.G.Players[1].Life != 19 || e.G.Players[0].Life != 21 {
		t.Fatalf("after kickedboth: lives %d/%d, want 21/19", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	replayCheck(t, e, cfg)
}

const altCostGrantorSrc = "Name:NullMonolith\nManaCost:3\nTypes:Artifact\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | Amount$ 3 | SpellDescription$ Add {C}{C}{C}.\n" +
	"S:Mode$ Continuous | MayPlay$ True | MayPlayAltManaCost$ 0 | MayPlayLimit$ 1 | " +
	"MayPlayDontGrantZonePermissions$ True | Affected$ Card.nonLand+Colorless+YouOwn | AffectedZone$ Hand | " +
	"Description$ Once each turn, you may pay {0} rather than pay the mana cost for a colorless spell that you cast from your hand.\nOracle:x\n"

const colorlessSpellSrc = "Name:NullGiant\nManaCost:4\nTypes:Creature Golem\nPT:4/4\nOracle:x\n"

// altCostBoard puts the grantor on the battlefield (a logged move) with the
// spell still in hand.
// cardName reads a fixture source's Name: line.
func cardName(src string) string {
	name := src
	if i := indexByte(name, '\n'); i >= 0 {
		name = name[:i]
	}
	return strings.TrimPrefix(name, "Name:")
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func altCostBoard(t *testing.T, seed uint64, grantorSrc string, spellSrcs ...string) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, _ := newFixtureDeck(t, seed, grantorSrc, spellSrcs...)
	grantorName := cardName(grantorSrc)
	// Bridge every wanted card into seat 0's hand (newFixtureDeck only
	// bridges the fixture); each call hands out one distinct copy, so a
	// repeated name bridges its second copy on the second call.
	bridged := map[state.ObjID]bool{}
	bridge := func(name string) state.ObjID {
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Owner != 0 || bridged[o.ID] || o.Face() == nil || o.Face().Name != name {
				continue
			}
			bridged[o.ID] = true
			if o.Zone == state.ZLibrary {
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZHand})
			}
			return o.ID
		}
		t.Fatalf("card %q not found in seat 0's hand or library", name)
		return 0
	}
	for _, sp := range spellSrcs {
		bridge(cardName(sp))
	}
	grantor := bridge(grantorName)
	// The bridge ran after Advance built the upkeep priority snapshot (the
	// same net-state dance newFixtureDeck's own bridge documents): clear the
	// stale pending and re-drive so the pending decision offers the newly
	// bridged cards.
	e.pending = nil
	e.Advance()
	e.emit(events.Event{Kind: events.MoveZone, Obj: grantor, From: state.ZHand, To: state.ZBattlefield})
	toMain1(t, e)
	return e, cfg, grantor
}

func TestMayPlayAltManaCostIsACastOption(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostBoard(t, 47, altCostGrantorSrc, colorlessSpellSrc)
	addMana(t, e, 0, "G")
	opts := castOptions(t, e)
	// {0} rather than the {4}: the alternative is affordable with one mana
	// floating, the plain cast is not.
	if len(opts) != 1 || opts[0].AltCostIndex == 0 {
		t.Fatalf("cast options %+v, want exactly the {0} alternative", opts)
	}
	submitChoices(t, e, opts[0].Index)
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool after the {0} cast: %d, want the mana untouched", got)
	}
	drainKickerStack(t, e, 30)
	replayCheck(t, e, cfg)
}

func TestMayPlayAltManaCostSparesNonColorlessSpells(t *testing.T) {
	t.Parallel()
	e, _, _ := altCostBoard(t, 48, altCostGrantorSrc, greenGiantSrc)
	addMana(t, e, 0, "GGG")
	opts := castOptions(t, e)
	for _, o := range opts {
		if o.AltCostIndex != 0 {
			t.Fatalf("a coloured spell was offered the colorless-only alternative: %+v", o)
		}
	}
	if len(opts) != 1 || opts[0].AltCostIndex != 0 {
		t.Fatalf("cast options %+v, want only the plain cast", opts)
	}
}

const greenGiantSrc = "Name:GreenGiant\nManaCost:2 G\nTypes:Creature Golem\nPT:3/3\nOracle:x\n"

const onlyFirstSpellGrantorSrc = "Name:RuinHerald\nManaCost:5\nTypes:Creature Nightmare\nPT:5/5\n" +
	"S:Mode$ ReduceCost | EffectZone$ Battlefield | ValidCard$ Card.Creature | Activator$ You | " +
	"Type$ Spell | OnlyFirstSpell$ True | Amount$ 2 | Description$ The first creature spell you cast each turn costs {2} less to cast.\nOracle:x\n"

const beanSrc = "Name:Bean\nManaCost:4\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

func TestOnlyFirstSpellDiscountsOnce(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostBoard(t, 49, onlyFirstSpellGrantorSrc, beanSrc, beanSrc)
	// Exactly the discounted {2}: the first creature spell this turn.
	addMana(t, e, 0, "GG")
	// Both Bean copies are offered at the discounted {2}.
	opts := castOptions(t, e)
	if len(opts) != 2 {
		t.Fatalf("first creature cast options %+v, want both Beans at the discounted price", opts)
	}
	for _, o := range opts {
		if o.AltCostIndex != 0 {
			t.Fatalf("a discount never rides AltCostIndex: %+v", o)
		}
	}
	submitChoices(t, e, opts[0].Index)
	drainKickerStack(t, e, 30)
	// The discount was spent on the first cast (the log walk counts the
	// first covered cast), so the second Bean is the full-price {4}. Funded
	// to exactly the still-discounted {2} first: zero options is the
	// DISCRIMINATING assertion (review sol2) -- against a tracking that
	// never spends, the second cast is payable at {2} and this fails -- and
	// then to the full {4}, where exactly one plain cast is offered.
	addMana(t, e, 0, "GG")
	if opts := castOptions(t, e); len(opts) != 0 {
		t.Fatalf("second creature cast at the still-discounted {2}: %+v, want none (the discount was spent)", opts)
	}
	replayCheck(t, e, cfg)
	addMana(t, e, 0, "GG")
	opts = castOptions(t, e)
	if len(opts) != 1 || opts[0].AltCostIndex != 0 {
		t.Fatalf("second creature cast at {4}: %+v, want the full-price plain cast", opts)
	}
	replayCheck(t, e, cfg)
}

const colorlessArtifactSrc = "Name:Gravestone\nManaCost:3\nTypes:Artifact\nOracle:x\n"
const greenCreatureSrc = "Name:Mossback\nManaCost:1 G\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"

// TestRestrictedFloatRefusesAColouredCast pins the RestrictValid$ emit ->
// consume round trip the ulalek-eldrazi deck's Shrine of the Forsaken Gods
// leans on ("Add {C}{C}. Spend this mana only to cast colorless spells.",
// script line `RestrictValid$ Spell.Colorless`): restricted mana carries its
// spend restriction on the ManaAdd event itself (events.ManaRestrictionText,
// the same provenance effMana emits) and the payment path admits it only to
// a matching spell. A coloured spell is refused even for its generic pips --
// restrictValidMatches evaluates the whole payment, never one pip -- and an
// unrestricted pip keeps the ordinary path.
func TestRestrictedFloatRefusesAColouredCast(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostBoard(t, 60, beanSrc, colorlessArtifactSrc, greenCreatureSrc)
	// Three colourless, restricted to colorless spells only (the same
	// emission effMana rides on every restricted ManaAdd; src 0 is the
	// source-less batch reading).
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 3,
		Text: events.ManaRestrictionText("Spell.Colorless", 0)})
	e.priorityRound()
	opts := castOptions(t, e)
	if len(opts) != 1 || opts[0].Label != "Cast Gravestone" {
		t.Fatalf("restricted-float cast options %+v, want only the colorless Gravestone", opts)
	}
	// One unrestricted generic: the coloured spell is still refused, because
	// its {G} pip cannot be paid from the restricted batch and its {1} pip
	// may not consume restricted mana toward a coloured cast either.
	addMana(t, e, 0, "C")
	if opts := castOptions(t, e); len(opts) != 1 || opts[0].Label != "Cast Gravestone" {
		t.Fatalf("with one free generic: %+v, want the coloured Mossback still refused", opts)
	}
	// An unrestricted {G}: now the {1} comes from the free generic and the
	// pip from the free G, so the coloured spell is offered.
	addMana(t, e, 0, "G")
	opts = castOptions(t, e)
	if len(opts) != 2 {
		t.Fatalf("with a free G: %+v, want both spells offered", opts)
	}
	replayCheck(t, e, cfg)
}

func TestTargetValidTargetingHoldsTheCounterToMatchingTargets(t *testing.T) {
	t.Parallel()
	decks := [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}
	e := New(Config{Seed: 55, Names: []string{"a", "b"}, Decks: decks})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	bear := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	opp := e.G.AddObject(card(t, "Name:Ogre\nManaCost:2 R\nTypes:Creature Ogre\nPT:2/2\nOracle:x\n"), 1)
	for _, id := range []state.ObjID{bear.ID, opp.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	}
	boltAtBear := e.G.AddObject(card(t, "Name:BoltA\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"), 0)
	boltAtOgre := e.G.AddObject(card(t, "Name:BoltB\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"), 0)
	for _, id := range []state.ObjID{boltAtBear.ID, boltAtOgre.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZStack})
	}
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: boltAtBear.ID, Player: 0, IDs: []state.ObjID{bear.ID}})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: boltAtOgre.ID, Player: 0, IDs: []state.ObjID{opp.ID}})
	counter := e.G.AddObject(card(t, "Name:Hold\nManaCost:3\nTypes:Instant\n"+
		"A:SP$ Counter | TargetType$ Spell,Activated,Triggered | ValidTgts$ Card,Emblem | "+
		"TargetValidTargeting$ Permanent.YouCtrl+inRealZoneBattlefield\nOracle:x\n"), 0)
	sa := counter.Face().SpellAbility()
	if sa == nil {
		t.Fatal("no spell ability")
	}
	sc := e.targetSpecContext(counter.ID, 0, 0)
	in := []targetCandidate{
		{kind: "permanent", obj: boltAtBear.ID},
		{kind: "permanent", obj: boltAtOgre.ID},
		{kind: "player", player: 1},
	}
	out := e.filterTargetValidTargeting(in, sa, sc)
	if len(out) != 1 || out[0].obj != boltAtBear.ID {
		t.Fatalf("filter kept %v, want only the spell targeting a permanent you control", out)
	}
}
