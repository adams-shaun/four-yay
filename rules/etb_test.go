package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// etbConfig builds a 2-seat fixture with arbitrarily many named fixture cards
// in each seat's deck (each followed by mountains to 40) and returns the
// engine, config and a find(name, seat) closure over the live ids. Every
// fixture card is seeded as a REAL deck card, not a token: that is what lets
// addToHand/putCreature move it into hand/battlefield AND what lets
// replayFromLog reconstruct the same object from cfg.Decks alone, so
// replayCheck stays exact (these are inline fixtures, never corpus .txt).
func etbConfig(t *testing.T, seed uint64, s0, s1 []string) (
	*Engine, Config, func(string, state.PlayerID) state.ObjID) {
	t.Helper()
	names := func(srcs []string) []string {
		var out []string
		for _, s := range srcs {
			out = append(out, card(t, s).Faces[0].Name)
		}
		return out
	}
	build := func(srcs []string) []*cards.Card {
		out := make([]*cards.Card, 0, 40)
		for _, s := range srcs {
			out = append(out, card(t, s))
		}
		return append(out, mountainDeck(t, 40-len(out))...)
	}
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{build(s0), build(s1)},
		Tokens: map[string]*cards.Card{}}
	e := New(cfg)
	e.Advance()
	// Pass the start-of-game upkeep priority and drive to Main 1 of the first
	// turn so a cast/play_land option is available; then move every fixture
	// card into its owner's hand (so it is castable, and so putCreature /
	// addToHand can relocate it); finally re-ask Main-1 priority so the pending
	// decision reflects the fixtures now in hand (the cavern test's
	// castFirst("play_land") reads it directly). Mountains stay in the library.
	driveToStep(t, e, 1, 0, state.StepMain1)
	allNames := [][]string{names(s0), names(s1)}
	for seat, nms := range allNames {
		for _, nm := range nms {
			if id := findByName(e, nm, state.PlayerID(seat)); id != 0 {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id,
					From: e.G.Obj(id).Zone, To: state.ZHand})
			}
		}
	}
	// Re-ask a fresh priority decision so the options reflect the fixtures now
	// in hand (a test that reads Pending() directly -- the cavern test's
	// castFirst("play_land") -- must see them).
	e.pending = nil
	e.Advance()
	find := func(name string, p state.PlayerID) state.ObjID {
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(z, p) {
				if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
					return id
				}
			}
		}
		t.Fatalf("fixture card %q not found for seat %d", name, p)
		return 0
	}
	return e, cfg, find
}

func findByName(e *Engine, name string, p state.PlayerID) state.ObjID {
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != p {
			continue
		}
		if o.Face() != nil && o.Face().Name == name {
			return o.ID
		}
	}
	return 0
}

// findManaAbilityOption scans the pending decision (the current priority
// holder's options) for an "activate" (mana) option of obj. The third
// parameter is the seat an enriched caller might restrict to, kept for
// signature clarity; the engine only ever activates from the priority
// holder, so the pending decision is the right scope. (Renamed from
// findAbilityOption so Task 10's own findAbilityOption -- which searches
// the "ability" option kind by Ability index -- does not collide.)
func findManaAbilityOption(e *Engine, obj state.ObjID, _ state.PlayerID) (decision.Option, bool) {
	d := e.Pending()
	if d == nil {
		return decision.Option{}, false
	}
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == obj {
			return o, true
		}
	}
	return decision.Option{}, false
}

// TestEtbCounterUsesTheChosenX pins kw:etbCounter (Task 12): the replacement
// that "enters with N counters, N = {X} paid" reads the cast-time X off the
// moving object (replacement.go's Ctx.X), not zero. X=3 -> 3/3 on the
// battlefield; X=0 -> a 0/0 that dies to state-based actions.
func TestEtbCounterUsesTheChosenX(t *testing.T) {
	t.Parallel()
	src := "Name:Endless\nManaCost:X\nTypes:Creature Eldrazi\nPT:0/0\nK:etbCounter:P1P1:X\nSVar:X:Count$xPaid\nOracle:x\n"
	e, cfg, find := etbConfig(t, 41, []string{src}, nil)
	id := find("Endless", 0)
	addMana(t, e, 0, "GGG")
	castFirst(t, e, "cast")
	submitChoices(t, e, 3) // X = 3
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.Counter("P1P1") != 3 || e.Power(id) != 3 {
		t.Fatalf("%+v power %d", o, e.Power(id))
	}
	replayCheck(t, e, cfg)
	// X = 0: a 0/0 that dies to state-based actions at once.
	e2, _, find2 := etbConfig(t, 42, []string{src}, nil)
	id2 := find2("Endless", 0)
	castFirst(t, e2, "cast")
	submitChoices(t, e2, 0)
	passUntilStackEmpty(t, e2, 20)
	if e2.G.Obj(id2).Zone != state.ZGraveyard {
		t.Fatalf("a 0/0 survived: %s", e2.G.Obj(id2).Zone)
	}
}

// TestChaliceCountersSpellsOfTheChargedManaValue pins the SVar/numeric-RHS
// resolver in the trigger matchers (specCtx): Chalice enters with X charge
// counters and a SpellCast trigger on "cmcEQY" where Y is an SVar over those
// counters, so a 1-mana spell cast afterward must be countered by the
// 1-charge Chalice. Without specCtx's EvalCount/Chosen resolution the trigger
// would silently never match (cmcEQY unresolvable) and the bolt would resolve.
func TestChaliceCountersSpellsOfTheChargedManaValue(t *testing.T) {
	t.Parallel()
	chalice := "Name:Chalice\nManaCost:X X\nTypes:Artifact\nK:etbCounter:CHARGE:X\n" +
		"T:Mode$ SpellCast | ValidCard$ Card.cmcEQY | ValidActivatingPlayer$ Player | TriggerZones$ Battlefield | Execute$ TrigCounter | TriggerDescription$ x\n" +
		"SVar:TrigCounter:DB$ Counter | Defined$ TriggeredSpellAbility\nSVar:X:Count$xPaid\nSVar:Y:Count$CardCounters.CHARGE\nOracle:x\n"
	bolt := "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"
	e, cfg, find := etbConfig(t, 53, []string{chalice}, []string{bolt})
	ch := find("Chalice", 0)
	addMana(t, e, 0, "GG")
	castFirst(t, e, "cast")
	submitChoices(t, e, 1) // X = 1
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(ch).Counter("CHARGE") != 1 {
		t.Fatal("chalice has no charge counter")
	}
	b := addToHand(t, e, 1, bolt)
	passToPlayerOne(t, e) // stack_test.go helper
	addMana(t, e, 1, "R")
	life := e.G.Players[0].Life
	castObj(t, e, b)
	passUntilStackEmpty(t, e, 30)
	if e.G.Obj(b).Zone != state.ZGraveyard || e.G.Players[0].Life != life {
		t.Fatalf("bolt %s life %d (want countered, life unchanged)", e.G.Obj(b).Zone, e.G.Players[0].Life)
	}
	replayCheck(t, e, cfg)
}

// TestSanctumPrelateNumberIsChosenAtEntryAndRestrictsCasting pins the
// entry-boundary number choice: Prelate asks as it enters, and its CantBeCast
// static (cmcEQChosen, resolved through specCtx) then forbids the chosen
// conversion value afterwards.
func TestSanctumPrelateNumberIsChosenAtEntryAndRestrictsCasting(t *testing.T) {
	t.Parallel()
	prelate := "Name:Prelate\nManaCost:1 W W\nTypes:Creature Human Cleric\nPT:2/2\nK:ETBReplacement:Other:ChooseNumber\n" +
		"SVar:ChooseNumber:DB$ ChooseNumber | Defined$ You | SpellDescription$ As CARDNAME enters, choose a number.\n" +
		"S:Mode$ CantBeCast | ValidCard$ Card.nonCreature+cmcEQChosen | Description$ x\nOracle:x\n"
	bolt := "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"
	e, cfg, find := etbConfig(t, 44, []string{prelate}, []string{bolt})
	pr := find("Prelate", 0)
	addMana(t, e, 0, "WWW")
	castFirst(t, e, "cast")
	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" || d.Options[0].Kind != "number" || len(d.Options) != 13 {
		t.Fatalf("number choice %+v", d)
	}
	submitChoices(t, e, 1) // choose 1
	if e.G.Obj(pr).ChosenNumber != 1 {
		t.Fatal("choice not recorded on the card")
	}
	passUntilStackEmpty(t, e, 20)
	b := addToHand(t, e, 1, bolt)
	// Re-establish the legal active-player priority marker before setting up
	// the non-active caster handoff.
	e.pending = nil
	e.emit(events.Event{Kind: events.Priority, Player: 0})
	e.askPriority(0)
	passToPlayerOne(t, e)
	addMana(t, e, 1, "R")
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == b {
			t.Fatal("a 1-mana noncreature spell was castable under Prelate on 1")
		}
	}
	replayCheck(t, e, cfg)
}

// TestNeedleNamesACardAndCavernChoosesAType covers the name and type ETB
// choices end to end: Needle's as-enters name pick is offered at its entry
// boundary (with a battlefield candidate present), recorded as ChosenName,
// and the named card's ability is then suppressed by the CantBeActivated
// static; Cavern of Souls (a land) picks a creature type at its entry boundary
// and enters the battlefield with it recorded.
func TestNeedleNamesACardAndCavernChoosesAType(t *testing.T) {
	t.Parallel()
	needle := "Name:Needle\nManaCost:1\nTypes:Artifact\nK:ETBReplacement:Other:DBNameCard\n" +
		"SVar:DBNameCard:DB$ NameCard | Defined$ You | SpellDescription$ x\n" +
		"S:Mode$ CantBeActivated | ValidCard$ Card.NamedCard | ValidSA$ Activated.!ManaAbility | Description$ x\nOracle:x\n"
	ballista := "Name:Ballista\nManaCost:X X\nTypes:Artifact Creature Construct\nPT:0/0\nA:AB$ DealDamage | Cost$ SubCounter<1/P1P1> | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
	e, cfg, find := etbConfig(t, 54, []string{needle}, []string{ballista})
	n := find("Needle", 0)
	b := putCreature(t, e, 1, ballista)
	e.emit(events.Event{Kind: events.CounterChange, Obj: b, Counter: "P1P1", Amount: 2})
	addMana(t, e, 0, "G")
	castFirst(t, e, "cast")
	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" || d.Options[0].Kind != "name" {
		t.Fatalf("name choice %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Label == "Ballista" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Ballista (on the battlefield) not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(n).ChosenName != "Ballista" {
		t.Fatal("name not recorded")
	}
	// Re-establish the legal active-player priority marker before setting up
	// the non-active caster handoff.
	e.pending = nil
	e.emit(events.Event{Kind: events.Priority, Player: 0})
	e.askPriority(0)
	passToPlayerOne(t, e)
	if _, ok := findManaAbilityOption(e, b, 0); ok {
		t.Fatal("the named card's ability was offered")
	}
	replayCheck(t, e, cfg)

	cavern := "Name:Cavern\nManaCost:no cost\nTypes:Land\nK:ETBReplacement:Other:ChooseCT\n" +
		"SVar:ChooseCT:DB$ ChooseType | Defined$ You | Type$ Creature | SpellDescription$ x\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\nOracle:x\n"
	grunt := "Name:Grunt\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n"
	{ // a land played through its own one-stage flow, in its own scope
		e2, cfg2, find2 := etbConfig(t, 55, []string{cavern, grunt}, nil)
		cv := find2("Cavern", 0)
		idx := -1
		for _, o := range e2.Pending().Options {
			if o.Kind == "play_land" && o.Obj == cv {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("no play_land option for the cavern: %+v", e2.Pending().Options)
		}
		submitChoices(t, e2, idx)
		dd := e2.Pending()
		if dd == nil || dd.Kind != decision.KChoose || dd.Options[0].Kind != "type" || dd.Options[0].Label != "Goblin" {
			t.Fatalf("type choice %+v", dd)
		}
		submitChoices(t, e2, 0)
		if o := e2.G.Obj(cv); o.Zone != state.ZBattlefield || o.ChosenType != "Goblin" {
			t.Fatalf("cavern %+v", o)
		}
		replayCheck(t, e2, cfg2)
	}
}
