package rules

// costgate: three root-cause classes that made a corpus ReduceCost static
// silently inert (or inverted). Every carrier below is the REAL corpus card
// (testutil.CorpusRegistry by name); only neutral stand-in permanents are
// inline fixtures. Each case drives the real offer (legalActions) and, where
// the reduction must apply, the real cast/activation and payment from an
// exact pool.
//
//  1. Secondary$ True: Forge reads CardTraitBase.isSecondary only when
//     building card text, so a Secondary$ cost static applies like any other.
//  2. IsPresent$ on a cost static honours PresentCompare$ and PresentZone$
//     (the shared presentGate), not "one matching permanent exists".
//  3. ValidSpell$ Activated.<Flag> matches Forge's param-backed SA flags
//     (Exhaust$/PowerUp$/Boast$/Monstrosity$ True), not only Keyword$ tags.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const (
	costgateRelic   = "Name:Test Relic\nManaCost:1\nTypes:Artifact\nOracle:x\n"
	costgateCharm   = "Name:Test Charm\nManaCost:1\nTypes:Enchantment\nOracle:x\n"
	costgateBlade   = "Name:Test Blade\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:2\nOracle:x\n"
	costgateWaste   = "Name:Test Waste\nTypes:Land\nOracle:x\n"
	costgateForest  = "Name:Test Forest\nTypes:Basic Land Forest\nOracle:x\n"
	costgateGrizzly = "Name:Grizzly\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
)

// costgatePut moves a fresh object into (zone, owner p) through a real
// MoveZone emit, so every log-head-keyed cache sees it.
func costgatePut(t *testing.T, e *Engine, p state.PlayerID, src string, zone state.Zone) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), p)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: zone})
	return o.ID
}

// costgatePutCorpus is costgatePut for a real corpus card.
func costgatePutCorpus(t *testing.T, e *Engine, p state.PlayerID, name string, zone state.Zone) state.ObjID {
	t.Helper()
	o := e.G.AddObject(corpusAlternativeCard(t, name), p)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: zone})
	return o.ID
}

// costgateOption returns seat 0's offered option of kind for obj.
func costgateOption(e *Engine, kind string, obj state.ObjID) (decision.Option, bool) {
	for _, opt := range e.legalActions(0) {
		if opt.Kind == kind && opt.Obj == obj {
			return opt, true
		}
	}
	return decision.Option{}, false
}

// costgateSetPool empties seat 0's pool and floats exactly the given mana.
func costgateSetPool(e *Engine, mana map[int]int32) {
	e.G.Players[0].Pool = state.Mana{}
	for i, n := range mana {
		e.G.Players[0].Pool[i] = n
	}
}

// costgateTake takes an offered cast/activation, answers every interim
// decision (preferring the listed objects as targets, else option 0), and
// asserts the whole floating pool paid for it: the price was exactly the
// reduced cost the test floated.
func costgateTake(t *testing.T, e *Engine, opt decision.Option, prefer ...state.ObjID) {
	t.Helper()
	if opt.Kind == "cast" {
		e.beginCast(0, opt)
	} else {
		e.beginActivation(0, opt)
	}
	for i := 0; i < 12; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		pick := 0
	pickLoop:
		for _, want := range prefer {
			for _, o := range d.Options {
				if o.Obj == want {
					pick = o.Index
					break pickLoop
				}
			}
		}
		submitChoices(t, e, pick)
	}
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("%s of %d did not complete: pending %+v", opt.Kind, opt.Obj, d)
	}
	if len(e.G.Stack) == 0 {
		t.Fatalf("%s of %d put nothing on the stack", opt.Kind, opt.Obj)
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after %s = %d, want 0 (charged exactly the reduced cost)", opt.Kind, left)
	}
}

// --- class 1: Secondary$ True ---------------------------------------------

// Assassin's Ink ({2}{B}{B}): "{1} less if you control an artifact and {1}
// less if you control an enchantment" -- the enchantment half is the
// Secondary$ True line. Skipping it priced the both-present cast at {1}{B}{B}
// and gave the enchantment-only board no reduction at all.
func TestCostgateSecondaryAssassinsInk(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T, relic, charm bool) (*Engine, state.ObjID, state.ObjID) {
		e := handEngine(t, corpusAlternativeCard(t, "Assassin's Ink"))
		if relic {
			costgatePut(t, e, 0, costgateRelic, state.ZBattlefield)
		}
		if charm {
			costgatePut(t, e, 0, costgateCharm, state.ZBattlefield)
		}
		victim := costgatePut(t, e, 1, costgateGrizzly, state.ZBattlefield)
		return e, e.G.Zone(state.ZHand, 0)[0], victim
	}
	t.Run("both halves: {B}{B}", func(t *testing.T) {
		e, ink, victim := setup(t, true, true)
		costgateSetPool(e, map[int]int32{state.MB: 2})
		opt, ok := costgateOption(e, "cast", ink)
		if !ok {
			t.Fatalf("Assassin's Ink not offered for {B}{B} with an artifact AND an enchantment (the Secondary$ half was dropped)")
		}
		costgateTake(t, e, opt, victim)
	})
	t.Run("secondary half alone: {1}{B}{B}", func(t *testing.T) {
		e, ink, victim := setup(t, false, true)
		costgateSetPool(e, map[int]int32{state.MB: 2})
		if _, ok := costgateOption(e, "cast", ink); ok {
			t.Fatalf("Assassin's Ink offered for {B}{B} with only an enchantment: one reduction, not two")
		}
		costgateSetPool(e, map[int]int32{state.MB: 2, state.MC: 1})
		opt, ok := costgateOption(e, "cast", ink)
		if !ok {
			t.Fatalf("Assassin's Ink not offered for {1}{B}{B} with an enchantment (Secondary$ reduction skipped)")
		}
		costgateTake(t, e, opt, victim)
	})
	t.Run("neither: full price", func(t *testing.T) {
		e, ink, _ := setup(t, false, false)
		costgateSetPool(e, map[int]int32{state.MB: 2, state.MC: 1})
		if _, ok := costgateOption(e, "cast", ink); ok {
			t.Fatalf("Assassin's Ink offered for {1}{B}{B} with neither an artifact nor an enchantment")
		}
	})
}

// Nahiri, Storm of Stone: "equip abilities you activate cost {1} less" --
// the card's ONLY cost static, Secondary$ True under the Continuous primary
// that grants first strike. Equip {2} costs {1} under your own Nahiri, and
// an opponent's Nahiri (Activator$ You) discounts nothing.
func TestCostgateSecondaryNahiriEquip(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T, nahiriOwner state.PlayerID) (*Engine, state.ObjID) {
		e := handEngine(t)
		nahiri := costgatePutCorpus(t, e, nahiriOwner, "Nahiri, Storm of Stone", state.ZBattlefield)
		e.emit(events.Event{Kind: events.CounterChange, Obj: nahiri, Counter: "LOYALTY", Amount: 6})
		blade := costgatePut(t, e, 0, costgateBlade, state.ZBattlefield)
		costgatePut(t, e, 0, costgateGrizzly, state.ZBattlefield)
		return e, blade
	}
	t.Run("own Nahiri: equip for {1}", func(t *testing.T) {
		e, blade := setup(t, 0)
		costgateSetPool(e, map[int]int32{state.MC: 1})
		opt, ok := costgateOption(e, "ability", blade)
		if !ok {
			t.Fatalf("Equip {2} not offered for {1} under Nahiri, Storm of Stone (Secondary$ reduction skipped)")
		}
		costgateTake(t, e, opt)
	})
	t.Run("opponent's Nahiri: full price", func(t *testing.T) {
		e, blade := setup(t, 1)
		costgateSetPool(e, map[int]int32{state.MC: 1})
		if _, ok := costgateOption(e, "ability", blade); ok {
			t.Fatalf("Equip {2} offered for {1} under an OPPONENT's Nahiri (Activator$ You)")
		}
	})
}

// --- class 2: PresentCompare$ / PresentZone$ ------------------------------

// Forceful Cultivator ({2}{G}{G}): "{2} less if there are no land cards in
// your hand" -- IsPresent$ Land.YouOwn | PresentZone$ Hand | PresentCompare$
// EQ0. The battlefield-exists read inverted it: a land on the battlefield
// discounted, an empty board did not.
func TestCostgatePresentEQ0ForcefulCultivator(t *testing.T) {
	t.Parallel()
	t.Run("no land in hand: {G}{G}", func(t *testing.T) {
		e := handEngine(t, corpusAlternativeCard(t, "Forceful Cultivator"))
		spell := e.G.Zone(state.ZHand, 0)[0]
		costgateSetPool(e, map[int]int32{state.MG: 2})
		opt, ok := costgateOption(e, "cast", spell)
		if !ok {
			t.Fatalf("Forceful Cultivator not offered for {G}{G} with no land card in hand")
		}
		costgateTake(t, e, opt)
	})
	t.Run("land in hand: full price", func(t *testing.T) {
		e := handEngine(t, corpusAlternativeCard(t, "Forceful Cultivator"))
		spell := e.G.Zone(state.ZHand, 0)[0]
		costgatePut(t, e, 0, costgateForest, state.ZHand)
		// A land on the battlefield without a mana ability: what the
		// battlefield-exists read wrongly counted.
		costgatePut(t, e, 0, costgateWaste, state.ZBattlefield)
		costgateSetPool(e, map[int]int32{state.MG: 2})
		if _, ok := costgateOption(e, "cast", spell); ok {
			t.Fatalf("Forceful Cultivator offered for {G}{G} with a land card in hand (EQ0 gate inverted)")
		}
	})
}

// Punishing Punch ({2}{G}): "{2} less if there are two or more creature
// cards in your graveyard" -- PresentCompare$ GE2 | PresentZone$ Graveyard.
func TestCostgatePresentGraveyardGE2PunishingPunch(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T, graveyard int) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
		e := handEngine(t, corpusAlternativeCard(t, "Punishing Punch"))
		mine := costgatePut(t, e, 0, costgateGrizzly, state.ZBattlefield)
		theirs := costgatePut(t, e, 1, costgateGrizzly, state.ZBattlefield)
		for i := 0; i < graveyard; i++ {
			costgatePut(t, e, 0, costgateGrizzly, state.ZGraveyard)
		}
		return e, e.G.Zone(state.ZHand, 0)[0], mine, theirs
	}
	t.Run("two creature cards: {G}", func(t *testing.T) {
		e, spell, mine, theirs := setup(t, 2)
		costgateSetPool(e, map[int]int32{state.MG: 1})
		opt, ok := costgateOption(e, "cast", spell)
		if !ok {
			t.Fatalf("Punishing Punch not offered for {G} with two creature cards in the graveyard")
		}
		costgateTake(t, e, opt, mine, theirs)
	})
	t.Run("one creature card: full price", func(t *testing.T) {
		e, spell, _, _ := setup(t, 1)
		costgateSetPool(e, map[int]int32{state.MG: 1})
		if _, ok := costgateOption(e, "cast", spell); ok {
			t.Fatalf("Punishing Punch offered for {G} with ONE creature card in the graveyard (GE2 read as a battlefield GE1)")
		}
	})
}

// Hour of Revelation ({3}{W}{W}{W}): "{3} less if there are ten or more
// nonland permanents on the battlefield" -- PresentCompare$ GE10 on the
// default (battlefield) zone.
func TestCostgatePresentGE10HourOfRevelation(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T, perms int) (*Engine, state.ObjID) {
		e := handEngine(t, corpusAlternativeCard(t, "Hour of Revelation"))
		for i := 0; i < perms; i++ {
			costgatePut(t, e, state.PlayerID(i%2), costgateGrizzly, state.ZBattlefield)
		}
		return e, e.G.Zone(state.ZHand, 0)[0]
	}
	t.Run("ten nonland permanents: {W}{W}{W}", func(t *testing.T) {
		e, spell := setup(t, 10)
		costgateSetPool(e, map[int]int32{state.MW: 3})
		opt, ok := costgateOption(e, "cast", spell)
		if !ok {
			t.Fatalf("Hour of Revelation not offered for {W}{W}{W} with ten nonland permanents")
		}
		costgateTake(t, e, opt)
	})
	t.Run("nine nonland permanents: full price", func(t *testing.T) {
		e, spell := setup(t, 9)
		costgateSetPool(e, map[int]int32{state.MW: 3})
		if _, ok := costgateOption(e, "cast", spell); ok {
			t.Fatalf("Hour of Revelation offered for {W}{W}{W} with nine nonland permanents (GE10 read as GE1)")
		}
	})
}

// --- class 3: param-backed ValidSpell$ Activated.<Flag> -------------------

// Boom Scholar: "Exhaust abilities of other permanents you control cost {2}
// less" -- ValidSpell$ Activated.Exhaust, and an exhaust ability is marked by
// the SA's own Exhaust$ True (Forge SpellAbility.isExhaust), never a
// Keyword$ tag. Prowcatcher Specialist's Exhaust {3}{R} costs {1}{R}; Boom
// Scholar's own exhaust (ValidCard$ ...+Other) is not discounted.
func TestCostgateActivatedExhaustBoomScholar(t *testing.T) {
	t.Parallel()
	t.Run("other permanent's exhaust: {1}{R}", func(t *testing.T) {
		e := handEngine(t)
		costgatePutCorpus(t, e, 0, "Boom Scholar", state.ZBattlefield)
		prow := costgatePutCorpus(t, e, 0, "Prowcatcher Specialist", state.ZBattlefield)
		costgateSetPool(e, map[int]int32{state.MR: 1, state.MC: 1})
		opt, ok := costgateOption(e, "ability", prow)
		if !ok {
			t.Fatalf("Prowcatcher Specialist's Exhaust {3}{R} not offered for {1}{R} under Boom Scholar")
		}
		costgateTake(t, e, opt)
	})
	t.Run("own exhaust: full price", func(t *testing.T) {
		e := handEngine(t)
		scholar := costgatePutCorpus(t, e, 0, "Boom Scholar", state.ZBattlefield)
		costgateSetPool(e, map[int]int32{state.MR: 1, state.MG: 1, state.MC: 2})
		if _, ok := costgateOption(e, "ability", scholar); ok {
			t.Fatalf("Boom Scholar's own Exhaust {4}{R}{G} offered for {2}{R}{G} (ValidCard$ ...+Other)")
		}
	})
}

// Hulk, Gamma Goliath: "Power-up abilities of other creatures you control
// cost {3} less" -- ValidSpell$ Activated.PowerUp over PowerUp$ True.
// Serpent Specialist's power-up {3}{G} costs {G}; under an opponent's Hulk
// (ValidCard$ Creature.Other+YouCtrl, You = the Hulk's controller) it does
// not.
func TestCostgateActivatedPowerUpHulk(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T, hulkOwner state.PlayerID) (*Engine, state.ObjID) {
		e := handEngine(t)
		costgatePutCorpus(t, e, hulkOwner, "Hulk, Gamma Goliath", state.ZBattlefield)
		return e, costgatePutCorpus(t, e, 0, "Serpent Specialist", state.ZBattlefield)
	}
	t.Run("own Hulk: {G}", func(t *testing.T) {
		e, serpent := setup(t, 0)
		costgateSetPool(e, map[int]int32{state.MG: 1})
		opt, ok := costgateOption(e, "ability", serpent)
		if !ok {
			t.Fatalf("Serpent Specialist's power-up {3}{G} not offered for {G} under Hulk, Gamma Goliath")
		}
		costgateTake(t, e, opt)
	})
	t.Run("opponent's Hulk: full price", func(t *testing.T) {
		e, serpent := setup(t, 1)
		costgateSetPool(e, map[int]int32{state.MG: 1})
		if _, ok := costgateOption(e, "ability", serpent); ok {
			t.Fatalf("Serpent Specialist's power-up offered for {G} under an OPPONENT's Hulk")
		}
	})
}

// TestCostgateSAFlagPropertyTable keeps saFlagProperty's literal-key switch
// and the saParamFlagProperties list (the census probe's source) in step:
// every listed flag reads its own `<Name>$ True`, an explicit False reads
// absent, and a keyword-tag constraint is not a param flag.
func TestCostgateSAFlagPropertyTable(t *testing.T) {
	t.Parallel()
	for _, name := range saParamFlagProperties {
		if !saFlagProperty(&cards.SA{Params: map[string]string{name: "True"}}, name) {
			t.Errorf("saFlagProperty(%s$ True, %q) = false", name, name)
		}
		if saFlagProperty(&cards.SA{Params: map[string]string{name: "False"}}, name) {
			t.Errorf("saFlagProperty(%s$ False, %q) = true", name, name)
		}
		if saFlagProperty(&cards.SA{Params: map[string]string{}}, name) {
			t.Errorf("saFlagProperty(no %s$, %q) = true", name, name)
		}
	}
	if saFlagProperty(&cards.SA{Params: map[string]string{"Equip": "True"}}, "Equip") {
		t.Errorf("Equip is a Keyword$-tag constraint, not a param flag")
	}
}
