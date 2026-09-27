package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cost_paid_exiled_revealed_test.go pins the cast-cost PAID-list references
// `Exiled$<Property>` / `Revealed$<Property>` end to end on the real compiled
// corpus carriers. A cost that exiles or reveals a card (Forge's CostExile /
// CostReveal) puts that card on Forge's SA paid list keyed "Exiled" /
// "Revealed"; AbilityUtils.getPaidCards resolves both refs from it. Before
// this work the refs fell through the count evaluator to a fail-closed zero,
// so Draconic Intervention / Corpse Explosion / Disaster Radius /
// Monstrous Emergence all resolved for zero.
//
// No Forge script text is committed here; every card comes from the compiled
// .cards corpus through searchCorpusCard. The fixtures below set the board so
// the damage the spell deals is the exiled/revealed card's characteristic and
// is DISTINCT from the source's own.

// paidCostEngine builds a two-seat game from compiled corpus cards: seat0's
// named fixtures padded with Forests, seat1's with Mountains. It advances the
// toss until seat 0 is the active player and drives to Main1.
func paidCostEngine(t *testing.T, seat0, seat1 []string) (*Engine, Config) {
	t.Helper()
	reg := searchTestRegistry(t)
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	mk := func(names []string, pad *cards.Card) []*cards.Card {
		deck := make([]*cards.Card, 0, 40)
		for _, n := range names {
			deck = append(deck, searchCorpusCard(t, reg, n))
		}
		for len(deck) < 40 {
			deck = append(deck, pad)
		}
		return deck
	}
	d0, d1 := mk(seat0, forest), mk(seat1, mountain)
	var e *Engine
	var cfg Config
	for seed := uint64(9411); ; seed++ {
		cfg = Config{Seed: seed, Names: []string{"caster", "opponent"},
			Decks: [][]*cards.Card{d0, d1}, Tokens: reg.Tokens, NameUniverse: reg.Cards}
		e = New(cfg)
		e.Advance()
		if e.G.Active == 0 {
			break
		}
	}
	toMain1(t, e)
	return e, cfg
}

// paidCostMoveTo moves the named corpus card to zone for seat p, scanning
// hand, library, battlefield and graveyard. It is conniveMoveTo's sibling with
// the graveyard added, because the exile costs pay from there.
func paidCostMoveTo(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone, avoid ...state.ObjID) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary, state.ZBattlefield, state.ZGraveyard} {
		for _, id := range e.G.Zone(z, p) {
			skip := false
			for _, a := range avoid {
				skip = skip || a == id
			}
			if skip {
				continue
			}
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
					e.pending = nil
					e.priorityRound()
				}
				return id
			}
		}
	}
	t.Fatalf("corpus card %q absent from seat %d's hand/library/battlefield/graveyard", name, p)
	return 0
}

// paidCostCast funds mana and submits the cast option for spell.
func paidCostCast(t *testing.T, e *Engine, spell state.ObjID, mana string) {
	t.Helper()
	addMana(t, e, 0, mana)
	d := e.Pending()
	castIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("no cast option for %d: %+v", spell, d.Options)
	}
	submitChoices(t, e, castIdx)
}

// answerPaidCostAsk answers any pending exile/reveal cost chooser by kind,
// restricted to picks when given (a forced selection poses no ask at all, so
// an absent ask is a normal return). It fails if the ask never clears.
func answerPaidCostAsk(t *testing.T, e *Engine, kind string, picks ...state.ObjID) {
	t.Helper()
	for deadline := 0; deadline < 8; deadline++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			return
		}
		var choices []int
		for _, o := range d.Options {
			if o.Kind != kind {
				continue
			}
			if len(picks) == 0 {
				choices = append(choices, o.Index)
				continue
			}
			for _, p := range picks {
				if o.Obj == p {
					choices = append(choices, o.Index)
				}
			}
		}
		if len(choices) == 0 {
			return
		}
		submitChoices(t, e, choices...)
	}
	t.Fatalf("%s cost ask did not clear", kind)
}

// TestDraconicInterventionPaidExileSizesDamage pins Exiled$CardManaCost on the
// real card: the spell exiles an instant/sorcery card from the graveyard as an
// additional cost and deals damage equal to ITS mana value to each non-Dragon
// creature. The fuel (Lightning Bolt, MV 1) is a different, nonzero value from
// the spell's own (MV 4), so a reading of the source or of zero is caught.
func TestDraconicInterventionPaidExileSizesDamage(t *testing.T) {
	e, cfg := paidCostEngine(t, []string{"Draconic Intervention", "Lightning Bolt"}, []string{"Ancient Brontodon"})
	fuel := paidCostMoveTo(t, e, 0, "Lightning Bolt", state.ZGraveyard)
	spell := paidCostMoveTo(t, e, 0, "Draconic Intervention", state.ZHand)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)
	// Precondition: the fuel's mana value is nonzero and DIFFERENT from the
	// source spell's, and the receiver survives the damage so it is readable.
	if got := e.G.Obj(fuel).Face().ManaValue(); got != 1 {
		t.Fatalf("precondition: Lightning Bolt MV = %d, want 1", got)
	}
	if e.G.Obj(spell).Face().ManaValue() == e.G.Obj(fuel).Face().ManaValue() {
		t.Fatal("precondition: fuel and source must have different mana values")
	}
	if int32(e.G.Obj(receiver).Face().Toughness()) <= e.G.Obj(fuel).Face().ManaValue() {
		t.Fatal("precondition: receiver must survive X damage")
	}

	paidCostCast(t, e, spell, "RRRR")
	answerPaidCostAsk(t, e, "exilecost", fuel)
	if o := e.G.Obj(fuel); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: paid exile fuel zone = %+v, want exile", o)
	}
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(receiver).Damage; got != 1 {
		t.Fatalf("Ancient Brontodon damage = %d, want 1 (the exiled card's mana value)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCorpseExplosionPaidExileSizesDamage pins Exiled$CardPower on the real
// card: exile a creature card from the graveyard, deal damage equal to ITS
// power to each creature and planeswalker. Hill Giant (power 3, MV 4) differs
// from the spell's own MV and from zero.
func TestCorpseExplosionPaidExileSizesDamage(t *testing.T) {
	e, cfg := paidCostEngine(t, []string{"Corpse Explosion", "Hill Giant"}, []string{"Ancient Brontodon"})
	fuel := paidCostMoveTo(t, e, 0, "Hill Giant", state.ZGraveyard)
	spell := paidCostMoveTo(t, e, 0, "Corpse Explosion", state.ZHand)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)
	if got := e.G.Obj(fuel).Face().Power(); got != 3 {
		t.Fatalf("precondition: Hill Giant power = %d, want 3", got)
	}
	if int32(e.G.Obj(receiver).Face().Toughness()) <= int32(e.G.Obj(fuel).Face().Power()) {
		t.Fatal("precondition: receiver must survive X damage")
	}

	paidCostCast(t, e, spell, "BR1")
	answerPaidCostAsk(t, e, "exilecost", fuel)
	if o := e.G.Obj(fuel); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: paid exile fuel zone = %+v, want exile", o)
	}
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(receiver).Damage; got != 3 {
		t.Fatalf("Ancient Brontodon damage = %d, want 3 (the exiled card's power)", got)
	}
	replayCheck(t, e, cfg)
}

// TestDisasterRadiusPaidRevealSizesDamage pins Revealed$CardManaCost on the
// real card: reveal a creature card from hand as an additional cost, deal
// damage equal to ITS mana value to each creature your opponents control.
// Grizzly Bears (MV 2) differs from the spell's own MV (7) and from zero, and
// the revealed card STAYS IN HAND (a reveal is not a move).
func TestDisasterRadiusPaidRevealSizesDamage(t *testing.T) {
	e, cfg := paidCostEngine(t, []string{"Disaster Radius", "Grizzly Bears"}, []string{"Ancient Brontodon"})
	fuel := paidCostMoveTo(t, e, 0, "Grizzly Bears", state.ZHand)
	spell := paidCostMoveTo(t, e, 0, "Disaster Radius", state.ZHand)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)
	if got := e.G.Obj(fuel).Face().ManaValue(); got != 2 {
		t.Fatalf("precondition: Grizzly Bears MV = %d, want 2", got)
	}
	if e.G.Obj(spell).Face().ManaValue() == e.G.Obj(fuel).Face().ManaValue() {
		t.Fatal("precondition: revealed card and source must have different mana values")
	}

	paidCostCast(t, e, spell, "RRRRRRR")
	answerPaidCostAsk(t, e, "revealcost", fuel)
	if o := e.G.Obj(fuel); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: revealed card zone = %+v, want hand (a reveal does not move)", o)
	}
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(receiver).Damage; got != 2 {
		t.Fatalf("Ancient Brontodon damage = %d, want 2 (the revealed card's mana value)", got)
	}
	replayCheck(t, e, cfg)
}

// TestMonstrousEmergencePaidRevealSizesDamage pins Revealed$CardPower on the
// reveal branch of RevealOrChoose<N/Spec>: Monstrous Emergence's cost reveals
// a creature card from hand (the modelled branch -- see
// rules/mana.go's choiceCostRevealOrChoose doc) and deals damage equal to ITS
// power. Grizzly Bears (power 2) differs from the spell's own power (a
// sorcery has none) and from zero.
func TestMonstrousEmergencePaidRevealSizesDamage(t *testing.T) {
	e, cfg := paidCostEngine(t, []string{"Monstrous Emergence", "Grizzly Bears"}, []string{"Ancient Brontodon"})
	fuel := paidCostMoveTo(t, e, 0, "Grizzly Bears", state.ZHand)
	spell := paidCostMoveTo(t, e, 0, "Monstrous Emergence", state.ZHand)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)
	if got := e.G.Obj(fuel).Face().Power(); got != 2 {
		t.Fatalf("precondition: Grizzly Bears power = %d, want 2", got)
	}
	if e.G.Obj(fuel).Face().Power() == 0 {
		t.Fatal("precondition: revealed card power must be nonzero")
	}

	paidCostCast(t, e, spell, "GG")
	answerPaidCostAsk(t, e, "revealcost", fuel)
	if o := e.G.Obj(fuel); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: revealed card zone = %+v, want hand", o)
	}
	// The spell targets; answer the target ask if one is still pending.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		for _, o := range d.Options {
			if o.Obj == receiver {
				submitChoices(t, e, o.Index)
			}
		}
	}
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(receiver).Damage; got != 2 {
		t.Fatalf("Ancient Brontodon damage = %d, want 2 (the revealed card's power)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCostPaidListsAreNotInheritedByAStackCopy pins the copy boundary: a
// StackCopy of a spell is a new object that paid no cost, so its
// Exiled$CardManaCost reads zero even though the original cast's paid list is
// live for the original object. The original is moved off the stack first so
// only the copy resolves; the receiver's damage then proves the copy read
// zero rather than inheriting the exiled Lightning Bolt's mana value.
func TestCostPaidListsAreNotInheritedByAStackCopy(t *testing.T) {
	e, _ := paidCostEngine(t, []string{"Draconic Intervention", "Lightning Bolt"}, []string{"Ancient Brontodon"})
	fuel := paidCostMoveTo(t, e, 0, "Lightning Bolt", state.ZGraveyard)
	spell := paidCostMoveTo(t, e, 0, "Draconic Intervention", state.ZHand)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)
	paidCostCast(t, e, spell, "RRRR")
	answerPaidCostAsk(t, e, "exilecost", fuel)
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: original must be on the stack, got %+v", o)
	}
	// Precondition: the original really carries the paid list, so a passing
	// copy assertion is about the copy, not about a broken original.
	if len(e.castExiled[spell]) != 1 || e.castExiled[spell][0] != fuel {
		t.Fatalf("precondition: original paid list = %v, want [%d]", e.castExiled[spell], fuel)
	}
	e.emit(events.Event{Kind: events.StackCopy, Obj: spell, Player: 0})
	copyID := e.G.Stack[len(e.G.Stack)-1]
	if copyID == spell {
		t.Fatal("precondition: StackCopy must mint a new object")
	}
	if len(e.castExiled[copyID]) != 0 {
		t.Fatalf("copy inherited the paid list %v", e.castExiled[copyID])
	}
	// Counter the original so only the copy's resolution is observed.
	e.emit(events.Event{Kind: events.MoveZone, Obj: spell, From: state.ZStack, To: state.ZGraveyard, Text: "countered"})
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(receiver).Damage; got != 0 {
		t.Fatalf("copy dealt %d damage, want 0 (a copy paid no exile cost)", got)
	}
}

// TestPaidCostListSurvivesASuspendedResolution pins the resume-site binding:
// effects.Ctx.Exiled is rebuilt when a resolution re-enters after a
// mid-resolution ask (rules/resolution.go resumeResolution), so a body AFTER
// the ask still reads the paid card. The synthetic probe reveals a creature as
// a cost, then poses a mid-resolution KChoose (DB$ ChooseCard) before the
// damage body that reads Revealed$CardPower.
func TestPaidCostListSurvivesASuspendedResolution(t *testing.T) {
	src := "Name:PaidRevealProbe\nManaCost:R\nTypes:Sorcery\n" +
		"A:SP$ LoseLife | Cost$ R Reveal<1/Creature> | Defined$ Opponent | LifeAmount$ 1 | SubAbility$ DBCharm\n" +
		"SVar:DBCharm:DB$ Charm | Choices$ DBDeal,DBUnused\n" +
		"SVar:DBDeal:DB$ LoseLife | Defined$ Opponent | LifeAmount$ X\n" +
		"SVar:DBUnused:DB$ Cleanup\n" +
		"SVar:X:Revealed$CardPower\nOracle:x\n"
	bear := "Name:ProbeBear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, probe := newFixtureDeck(t, 9611, src, bear)
	fuel := paidCostMoveTo(t, e, 0, "ProbeBear", state.ZHand)
	if got := e.G.Obj(fuel).Face().Power(); got != 2 {
		t.Fatalf("precondition: ProbeBear power = %d, want 2", got)
	}
	paidCostCast(t, e, probe, "R")
	if len(e.castRevealed[probe]) != 1 || e.castRevealed[probe][0] != fuel {
		t.Fatalf("precondition: paid reveal list = %v, want [%d]", e.castRevealed[probe], fuel)
	}
	// The reveal cost is forced (the fuel is the only creature in hand), so
	// no cost ask appears. Drain priority to resolve the spell; its
	// SubAbility$ Charm then poses a MID-RESOLUTION modal ask, which suspends
	// and re-enters the resolution before DBDeal reads Revealed$CardPower.
	modalAsks := 0
	var seen []string
	for deadline := 0; deadline < 30 && !e.G.Over; deadline++ {
		d := e.Pending()
		if d == nil {
			seen = append(seen, "<nil>")
			break
		}
		seen = append(seen, string(d.Kind)+"/"+d.ResumeKind)
		if d.Kind == decision.KModes {
			if len(d.Options) == 0 {
				t.Fatalf("modes ask with no options")
			}
			modalAsks++
			submitChoices(t, e, d.Options[0].Index)
			continue
		}
		if d.Kind == decision.KChoose {
			submitChoices(t, e, d.Options[0].Index)
			continue
		}
		if d.Kind == decision.KPriority {
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if pass < 0 {
				t.Fatalf("priority with no pass option: %+v", d)
			}
			submitChoices(t, e, pass)
			continue
		}
		break
	}
	if modalAsks < 1 {
		t.Fatalf("precondition: the probe's mid-resolution modal ask was never posed; decisions seen: %v", seen)
	}
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("after a mid-resolution ask, opponent life = %d, want 17 (1 + Revealed$CardPower survived the suspension)", got)
	}
	replayCheck(t, e, cfg)
}

// TestPaidCostRefFailsClosedWithoutAnExiledCost pins the absent binding: an
// ordinary cast of a card whose SVar reads Exiled$CardManaCost but whose cost
// exiled nothing reads a legitimate zero, never the source's own or the
// chosen targets. A synthetic no-cost probe exercises the empty paid list.
func TestPaidCostRefFailsClosedWithoutAnExiledCost(t *testing.T) {
	src := "Name:NoCostProbe\nManaCost:R\nTypes:Sorcery\n" +
		"A:SP$ LoseLife | Defined$ Opponent | LifeAmount$ X\n" +
		"SVar:X:Exiled$CardManaCost\nOracle:x\n"
	e, cfg, probe := newFixtureDeck(t, 9622, src)
	before := e.G.Players[1].Life
	paidCostCast(t, e, probe, "R")
	passUntilStackEmpty(t, e, 20)
	// The probe's no-cost cast exiled nothing; the ref reads zero rather than
	// the source's own mana value (1).
	if len(e.castExiled[probe]) != 0 {
		t.Fatalf("no-cost cast recorded a paid list: %v", e.castExiled[probe])
	}
	if got := e.G.Players[1].Life; got != before {
		t.Fatalf("no-cost Exiled$CardManaCost dealt %d life loss, want 0", before-got)
	}
	replayCheck(t, e, cfg)
}

// TestDreadDefilerPaidExileSizesLifeLoss pins the ACTIVATED-ability branch of
// the paid-list capture (installPaidCostLists's ability arm): Dread Defiler's
// `{3}{C}, Exile a creature card from your graveyard` activated ability reads
// the exiled card's power through `X:Exiled$CardPower`. Same binding as the
// spell branch; without it the ability loses no life.
func TestDreadDefilerPaidExileSizesLifeLoss(t *testing.T) {
	e, cfg := paidCostEngine(t, []string{"Dread Defiler", "Hill Giant"}, nil)
	defiler := paidCostMoveTo(t, e, 0, "Dread Defiler", state.ZBattlefield)
	fuel := paidCostMoveTo(t, e, 0, "Hill Giant", state.ZGraveyard)
	if got := e.G.Obj(fuel).Face().Power(); got != 3 {
		t.Fatalf("precondition: Hill Giant power = %d, want 3", got)
	}
	addMana(t, e, 0, "CCCC")
	activateRevealAbility(t, e, 0, defiler)
	answerPaidCostAsk(t, e, "exilecost", fuel)
	// The ability targets an opponent: the only offered target.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		if len(d.Options) == 0 {
			t.Fatalf("target ask with no options: %+v", d)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	if o := e.G.Obj(fuel); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: paid exile fuel zone = %+v, want exile", o)
	}
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("opponent life = %d, want 17 (lost the exiled card's power 3)", got)
	}
	replayCheck(t, e, cfg)
}

// TestSoulExchangePaidExileValidTracksThrull pins `Exiled$Valid <type>` end to
// end on a fully-supported corpus carrier: Soul Exchange's cost exiles a
// creature you control (bare `Exile<1/Creature>`, which feeds the paid list),
// and `X:Exiled$Valid Thrull` sizes the +2/+2 counter on the returned creature.
// A non-Thrull exiled creature would place zero counters, so the counter proves
// the paid card was matched, not just counted.
func TestSoulExchangePaidExileValidTracksThrull(t *testing.T) {
	e, cfg := paidCostEngine(t, []string{"Soul Exchange", "Blood Pet", "Grizzly Bears"}, nil)
	thrull := paidCostMoveTo(t, e, 0, "Blood Pet", state.ZBattlefield)
	returned := paidCostMoveTo(t, e, 0, "Grizzly Bears", state.ZGraveyard)
	spell := paidCostMoveTo(t, e, 0, "Soul Exchange", state.ZHand)
	// Precondition: the two creatures differ on the Thrull predicate, so a
	// match on the wrong one is impossible to confuse with a match on the
	// paid one.
	if !hasTypeWord(e.G.Obj(thrull).Face().Types, "Thrull") {
		t.Fatalf("precondition: Blood Pet types = %v, want a Thrull", e.G.Obj(thrull).Face().Types)
	}
	if hasTypeWord(e.G.Obj(returned).Face().Types, "Thrull") {
		t.Fatalf("precondition: returned creature must not be a Thrull: %v", e.G.Obj(returned).Face().Types)
	}
	if e.G.Obj(returned).Zone != state.ZGraveyard {
		t.Fatalf("precondition: return target zone = %s, want graveyard", e.G.Obj(returned).Zone)
	}

	paidCostCast(t, e, spell, "BB")
	answerPaidCostAsk(t, e, "exilecost", thrull)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		for _, o := range d.Options {
			if o.Obj == returned {
				submitChoices(t, e, o.Index)
			}
		}
	}
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(thrull); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: paid exile zone = %+v, want exile", o)
	}
	if o := e.G.Obj(returned); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("returned creature zone = %+v, want battlefield", o)
	} else if got := o.Counter("P2P2"); got != 1 {
		t.Fatalf("returned creature P2P2 counters = %d, want 1 (Exiled$Valid Thrull matched the exiled Blood Pet)", got)
	}
	replayCheck(t, e, cfg)
}

// TestMonstrousEmergenceRevealOrChooseParsesAsARevealCost is the grammar leaf:
// RevealOrChoose<N/Spec> prices with no generic substitution and no Unknown
// census entry, and lands in the DISTINCT Cost.RevealOrChoose slice (not the
// plain Reveal list): the token is an either-or cost whose choose arm must
// not be read as a hand reveal.
func TestMonstrousEmergenceRevealOrChooseParsesAsARevealCost(t *testing.T) {
	c := ParseCost("1 G RevealOrChoose<1/Creature>")
	if c.Generic != 1 {
		t.Fatalf("ParseCost generic = %d, want 1", c.Generic)
	}
	if len(c.Unknown) != 0 {
		t.Fatalf("ParseCost Unknown = %v, want none", c.Unknown)
	}
	if len(c.Reveal) != 0 {
		t.Fatalf("ParseCost Reveal = %+v, want none (the either-or cost is its own slice)", c.Reveal)
	}
	if len(c.RevealOrChoose) != 1 || c.RevealOrChoose[0].Spec != "Creature" || c.RevealOrChoose[0].N != 1 {
		t.Fatalf("ParseCost RevealOrChoose = %+v, want one Creature part", c.RevealOrChoose)
	}
}
