package rules

// CR 107.3m: the X a triggered ability reads is not its own. A cast trigger
// reads the X paid for the spell it triggered on; a ChangesZone -> Battlefield
// trigger of a permanent reads the X paid for the spell that became that
// permanent; and a permanent that entered any other way (reanimated, blinked,
// dropped straight onto the battlefield) reads 0. Before the fix every such
// trigger resolved with X = 0 -- rules/stack.go's ability/trigger branch bound
// only the trigger object's own (never-paid) X -- so Hydroid Krasis X=4 gained
// no life and drew no cards, Genesis Hydra dug 0 cards, and a Count$xPaid ETB
// trigger reanimated a permanent with no counters.
//
// Every fixture here is a real Forge script (or, for the third-party cast
// trigger, a real-script SHAPE at a ValidCard$ the corpus carriers' hasXCost
// predicate cannot express yet -- see the report's Issues section), written
// inline per the repo's licensing rule: never a .cards/ .txt.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const hydroidKrasisSrc = "Name:Hydroid Krasis\nManaCost:X G U\nTypes:Creature Jellyfish Hydra Beast\nPT:0/0\n" +
	"T:Mode$ SpellCast | ValidCard$ Card.Self | Execute$ TrigGainLife | TriggerDescription$ When you cast CARDNAME, you gain half X life and draw half X cards. Round down each time.\n" +
	"SVar:TrigGainLife:DB$ GainLife | Defined$ You | LifeAmount$ HalfXDown | SubAbility$ DBDraw\n" +
	"SVar:DBDraw:DB$ Draw | NumCards$ HalfXDown\n" +
	"K:Flying\nK:Trample\nK:etbCounter:P1P1:X\n" +
	"SVar:X:Count$xPaid\nSVar:HalfXDown:Count$xPaid/HalfDown\n" +
	"DeckHas:Ability$Counters\n" +
	"Oracle:When you cast this spell, you gain half X life and draw half X cards. Round down each time.\\nFlying, trample\\nHydroid Krasis enters with X +1/+1 counters on it.\n"

const genesisHydraSrc = "Name:Genesis Hydra\nManaCost:X G\nTypes:Creature Plant Hydra\nPT:0/0\n" +
	"K:etbCounter:P1P1:X\n" +
	"T:Mode$ SpellCast | ValidCard$ Card.Self | Execute$ TrigDig | TriggerDescription$ When you cast this spell, reveal the top X cards of your library. You may put a nonland permanent card with mana value X or less from among them onto the battlefield. Then shuffle the rest into your library.\n" +
	"SVar:TrigDig:DB$ Dig | DigNum$ X | Reveal$ True | ChangeNum$ 1 | ChangeValid$ Permanent.nonLand+cmcLEX | DestinationZone$ Battlefield | LibraryPosition2$ 0 | SubAbility$ DBShuffle | Optional$ True | RestRandomOrder$ True\n" +
	"SVar:DBShuffle:DB$ Shuffle | Defined$ You\n" +
	"SVar:X:Count$xPaid\n" +
	"Oracle:When you cast this spell, reveal the top X cards of your library. You may put a nonland permanent card with mana value X or less from among them onto the battlefield. Then shuffle the rest into your library.\\nGenesis Hydra enters the battlefield with X +1/+1 counters on it.\n"

const wanShiTongSrc = "Name:Wan Shi Tong, Librarian\nManaCost:X U U\nTypes:Legendary Creature Bird Spirit\nPT:1/1\n" +
	"K:Flash\nK:Flying\nK:Vigilance\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigPutCounter1 | TriggerDescription$ When NICKNAME enters, put X +1/+1 counters on him. Then draw half X cards, rounded down.\n" +
	"SVar:TrigPutCounter1:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ X | SubAbility$ DBDraw1\n" +
	"SVar:DBDraw1:DB$ Draw | NumCards$ Y\n" +
	"SVar:X:Count$xPaid\nSVar:Y:SVar$X/HalfDown\n" +
	"Oracle:Flash\\nFlying, vigilance\\nWhen Wan Shi Tong enters, put X +1/+1 counters on him. Then draw half X cards, rounded down.\n"

// TestHydroidKrasisSpellCastTriggerReadsThePaidX is the brief's own probe:
// cast Krasis for X=4 and its SpellCast trigger must gain half X (2 life) and
// draw half X (2 cards) -- not 0 of either -- while the ETB counter
// replacement keeps its pre-existing correct X=4.
func TestHydroidKrasisSpellCastTriggerReadsThePaidX(t *testing.T) {
	t.Parallel()
	e, cfg, find := etbConfig(t, 114, []string{hydroidKrasisSrc}, nil)
	id := find("Hydroid Krasis", 0)
	addMana(t, e, 0, "GGUUUU") // {4}{G}{U} from 2 G + 4 U
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 4)
	lifeBefore := e.G.Players[0].Life
	handBefore := len(e.G.Zone(state.ZHand, 0))
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != lifeBefore+2 {
		t.Fatalf("life %d, want %d (+X/2 for X=4)", got, lifeBefore+2)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+2 {
		t.Fatalf("hand %d, want %d (drew X/2 = 2)", got, handBefore+2)
	}
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.Counter("P1P1") != 4 || e.Power(id) != 4 {
		t.Fatalf("Krasis zone=%s counters=%d power=%d, want battlefield/4/4",
			o.Zone, o.Counter("P1P1"), e.Power(id))
	}
	replayCheck(t, e, cfg)
}

// TestGenesisHydraCastTriggerFiresAndEtbReadsThePaidX covers the brief's
// Genesis Hydra leg. Its cast trigger (Mode$ SpellCast, Card.Self) fires only
// because zoneGate now lets a spell's own cast trigger qualify while the
// source is the spell on the stack (CR 601.2i); its Dig then resolves silently
// in this build (ChangeValid$ Permanent.nonLand matches no library card -- the
// filter reads base Permanent as zone==battlefield, a pre-existing semantic
// recorded in the report's Issues), so the ask-free dig takes nothing and the
// resolution completes. The X=3 the ETB observable needs comes off the paid
// cast: Genesis Hydra enters with X +1/+1 counters (kw:etbCounter reading the
// moving spell's X), so it is a 3/3.
func TestGenesisHydraCastTriggerFiresAndEtbReadsThePaidX(t *testing.T) {
	t.Parallel()
	e, cfg, find := etbConfig(t, 115, []string{genesisHydraSrc}, nil)
	hydra := find("Genesis Hydra", 0)
	addMana(t, e, 0, "GGGG") // {3}{G}
	castFirst(t, e, "cast")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 3)
	pushes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == hydra {
			pushes++
		}
	}
	if pushes != 1 {
		t.Fatalf("Genesis Hydra cast-trigger TriggerPush count %d, want 1", pushes)
	}
	var trigger state.ObjID
	for _, stack := range e.G.Stack {
		o := e.G.Obj(stack)
		if o != nil && o.Source == hydra && o.Ability != nil {
			trigger = stack
		}
	}
	if trigger == 0 || e.triggerContexts[trigger].TriggerPaidX != 3 {
		t.Fatalf("Genesis Hydra trigger paid X = %d (stack=%d), want 3",
			e.triggerContexts[trigger].TriggerPaidX, trigger)
	}
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(hydra); o.Zone != state.ZBattlefield || o.Counter("P1P1") != 3 || e.Power(hydra) != 3 {
		t.Fatalf("Hydra zone=%s counters=%d power=%d, want battlefield/3/3",
			o.Zone, o.Counter("P1P1"), e.Power(hydra))
	}
	replayCheck(t, e, cfg)
}

// TestWanShiTongEtbTriggerReadsThePaidX pins the ETB arm (Mode$ ChangesZone ->
// Battlefield): the permanent's trigger reads the X paid for the spell that
// became it -- X=2 puts 2 +1/+1 counters on it and draws half X (1 card).
func TestWanShiTongEtbTriggerReadsThePaidX(t *testing.T) {
	t.Parallel()
	e, cfg, find := etbConfig(t, 124, []string{wanShiTongSrc}, nil)
	id := find("Wan Shi Tong, Librarian", 0)
	addMana(t, e, 0, "UUUU") // {2}{U}{U}
	castFirst(t, e, "cast")
	submitChoices(t, e, 2)
	handBefore := len(e.G.Zone(state.ZHand, 0))
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.Counter("P1P1") != 2 {
		t.Fatalf("Wan Shi Tong zone=%s counters=%d, want battlefield/2", o.Zone, o.Counter("P1P1"))
	}
	// handBefore is the POST-CAST hand (the cast's -1 already happened), so
	// the ETB draw of half X rounded down (X=2 -> 1 card) shows as exactly
	// +1. The SVar$X/HalfDown indirection resolves the paid X through the
	// card's own SVar table (effects/count.go's SVar$ branch), so the draw
	// actually happens -- an engine where that body degraded to zero would
	// leave the hand at handBefore and this assertion catches it.
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand %d, want %d (cast one, drew X/2 = 1)", got, handBefore+1)
	}
	replayCheck(t, e, cfg)
}

// TestReanimatedEtbXPaidCardResolvesWithXZero pins the CR 107.3m boundary:
// the X is the spell that became the permanent's, and nothing else's. Wan Shi
// Tong cast at X=4 enters with 4 counters; destroyed and reanimated, its ETB
// trigger fires again but reads 0 (events.Move reset X when it left the
// battlefield). The same holds for a copy that never resolved at all: cast at
// X=3, countered into the graveyard, reanimated -- the stale cast X must be
// cleared when the card left the stack for a non-battlefield zone, or the
// reanimate would resurrect the dead cast's X.
func TestReanimatedEtbXPaidCardResolvesWithXZero(t *testing.T) {
	t.Parallel()
	zombify := "Name:Rise\nManaCost:1 B\nTypes:Sorcery\n" +
		"A:SP$ ChangeZone | Origin$ Graveyard | Destination$ Battlefield | TgtZone$ Graveyard | ValidTgts$ Creature.YouCtrl | SpellDescription$ Return target creature card from your graveyard to the battlefield.\nOracle:x\n"
	bolt := "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ Destroy | ValidTgts$ Creature\nOracle:x\n"
	cancel := "Name:Cancel\nManaCost:1 U U\nTypes:Instant\nA:SP$ Counter | TargetType$ Spell | TgtPrompt$ Select target spell | ValidTgts$ Card\nOracle:x\n"
	e, cfg, find := etbConfig(t, 126, []string{wanShiTongSrc, wanShiTongSrc, zombify, zombify, bolt, bolt, cancel}, nil)
	wan1 := find("Wan Shi Tong, Librarian", 0)
	rise := find("Rise", 0)
	boltID := find("Bolt", 0)
	// The find-by-name helper returns the first match of each name, so the
	// second copy of each duplicated fixture is the OTHER object with the same
	// face. etbConfig moves one card per distinct name into the hand; the
	// second Wan Shi Tong and the second Rise stay in the library, so bridge
	// them up with logged MoveZones the way moveSeeded does.
	toHand := func(name string, skip state.ObjID) state.ObjID {
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(z, 0) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil || o.Face().Name != name || id == skip {
					continue
				}
				if o.Zone != state.ZHand {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
				}
				return id
			}
		}
		t.Fatalf("no second %q for seat 0", name)
		return 0
	}
	wan2 := toHand("Wan Shi Tong, Librarian", wan1)
	rise2 := toHand("Rise", rise)
	bolt2 := toHand("Bolt", boltID)
	cancelID := find("Cancel", 0)

	// Route 1: cast X=4 (4 counters from the ETB trigger), destroy, reanimate.
	addMana(t, e, 0, "UUUUUU") // {4}{U}{U}
	submitChoices(t, e, castOptionFor(t, e, wan1).Index)
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose {
		t.Fatalf("X decision %+v", d)
	} else {
		submitChoices(t, e, 4)
	}
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(wan1); o.Zone != state.ZBattlefield || o.Counter("P1P1") != 4 {
		t.Fatalf("pre-death Wan Shi Tong zone=%s counters=%d, want battlefield/4",
			o.Zone, o.Counter("P1P1"))
	}
	// Destroy it.
	addMana(t, e, 0, "R")
	castObjTargeting(t, e, boltID, wan1)
	if o := e.G.Obj(wan1); o.Zone != state.ZGraveyard || o.Counter("P1P1") != 0 {
		t.Fatalf("dead Wan Shi Tong zone=%s counters=%d, want graveyard/0 (X reset on leaving the battlefield)",
			o.Zone, o.Counter("P1P1"))
	}
	// Reanimate it: the ETB trigger fires again (TriggerPush after the
	// reanimate move) and must read X = 0.
	addMana(t, e, 0, "BB")
	castObjTargeting(t, e, rise, wan1)
	pushes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == wan1 {
			pushes++
		}
	}
	if pushes < 2 {
		t.Fatalf("Wan Shi Tong ETB TriggerPush count %d, want 2 (cast entry + reanimate entry)", pushes)
	}
	if o := e.G.Obj(wan1); o.Zone != state.ZBattlefield || o.Counter("P1P1") != 0 || e.Power(wan1) != 1 {
		t.Fatalf("reanimated Wan Shi Tong zone=%s counters=%d power=%d, want battlefield/0/1",
			o.Zone, o.Counter("P1P1"), e.Power(wan1))
	}

	// Route 2: cast X=3, COUNTER it while it is still on the stack (stack ->
	// graveyard must clear the paid X), reanimate: still 0.
	addMana(t, e, 0, "UUUUU") // {3}{U}{U}
	submitChoices(t, e, castOptionFor(t, e, wan2).Index)
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose {
		t.Fatalf("second X decision %+v", d)
	} else {
		submitChoices(t, e, 3)
	}
	// The Wan Shi Tong spell sits on the stack, paid for; Cancel it before
	// anyone passes priority into resolution.
	addMana(t, e, 0, "UUU") // {1}{U}{U}
	submitChoices(t, e, castOptionFor(t, e, cancelID).Index)
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision for the counter: %+v", d)
	} else {
		tgt := -1
		for _, o := range d.Options {
			if o.Obj == wan2 {
				tgt = o.Index
			}
		}
		if tgt < 0 {
			t.Fatalf("no option targeting the Wan Shi Tong spell: %+v", d.Options)
		}
		submitChoices(t, e, tgt)
	}
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(wan2); o.Zone != state.ZGraveyard {
		t.Fatalf("countered Wan Shi Tong zone = %s, want graveyard", o.Zone)
	}
	// Both copies are legendary: the first one still on the battlefield would
	// legend-rule the reanimated copy straight back to the graveyard (CR
	// 704.5j), so destroy wan1 first.
	addMana(t, e, 0, "R")
	castObjTargeting(t, e, bolt2, wan1)
	addMana(t, e, 0, "BB")
	castObjTargeting(t, e, rise2, wan2)
	if o := e.G.Obj(wan2); o.Zone != state.ZBattlefield || o.Counter("P1P1") != 0 {
		t.Fatalf("reanimated countered copy zone=%s counters=%d, want battlefield/0 (stale cast X cleared)",
			o.Zone, o.Counter("P1P1"))
	}
	replayCheck(t, e, cfg)
}

// TestGadwickEtbKeepsPaidXAfterThePermanentLeaves proves that 107.3m binds
// an ETB trigger's X when it fires. Gadwick is destroyed by a real Bolt after
// the spell resolved and its trigger was placed on the stack; Move then clears
// the card's live X, but the queued trigger must still draw X cards.
func TestGadwickEtbKeepsPaidXAfterThePermanentLeaves(t *testing.T) {
	t.Parallel()
	// Gadwick's actual compiled corpus script is this exact ChangesZone,
	// Card.Self, Count$xPaid shape, with a direct DB$ Draw observable.
	reg := testutil.CorpusRegistry(t)
	gadwick := mustCorpusCard(t, reg, "Gadwick, the Wizened")
	bolt := card(t, "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ Destroy | ValidTgts$ Creature\nOracle:x\n")
	cfg := Config{Seed: 127, Names: []string{"a", "b"}, Tokens: map[string]*cards.Card{},
		Decks: [][]*cards.Card{
			append([]*cards.Card{gadwick}, mountainDeck(t, 39)...),
			append([]*cards.Card{bolt}, mountainDeck(t, 39)...),
		}}
	e := New(cfg)
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	wan := findByName(e, "Gadwick, the Wizened", 0)
	boltID := findByName(e, "Bolt", 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: wan, From: e.G.Obj(wan).Zone, To: state.ZHand})
	e.emit(events.Event{Kind: events.MoveZone, Obj: boltID, From: e.G.Obj(boltID).Zone, To: state.ZHand})
	e.pending = nil
	e.priorityRound()
	handBefore := len(e.G.Zone(state.ZHand, 0))
	addMana(t, e, 0, "UUUUU") // {2}{U}{U}{U}
	addMana(t, e, 1, "R")
	submitChoices(t, e, castOptionFor(t, e, wan).Index)
	submitChoices(t, e, 2)

	// Resolve only the creature spell. The resulting ETB trigger remains on
	// the stack, so player 1 can answer it with Bolt.
	for steps := 0; steps < 8; steps++ {
		if len(e.G.Stack) == 1 && e.G.Obj(e.G.Stack[0]).Ability != nil {
			break
		}
		castFirst(t, e, "pass")
	}
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Ability == nil {
		t.Fatalf("stack after Gadwick resolved = %v, want its ETB trigger", e.G.Stack)
	}
	if got := e.triggerContexts[e.G.Stack[0]].TriggerPaidX; got != 2 {
		t.Fatalf("queued ETB paid X = %d, want 2", got)
	}
	// The active player gets priority first after resolution; pass to player 1,
	// who casts the real-script destroy spell before the ETB resolves.
	castFirst(t, e, "pass")
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority || d.Player != 1 {
		t.Fatalf("priority after player 0 passed = %+v, want player 1", d)
	}
	castObjTargeting(t, e, boltID, wan)
	if o := e.G.Obj(wan); o.Zone != state.ZGraveyard || o.X != 0 {
		t.Fatalf("destroyed Gadwick zone=%s X=%d, want graveyard/0", o.Zone, o.X)
	}
	// Cast -1 and the now-source-less ETB draws X=2: final hand grows by one.
	// A live-card lookup at resolution would draw zero.
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand %d, want %d (draw X after source left)", got, handBefore+1)
	}
	replayCheck(t, e, cfg)
}

// TestThirdPartyCastTriggerReadsTheTriggeringSpellsX pins the cast-trigger arm
// whose source is NOT the spell: a magecraft-style permanent reads the X of
// the spell it triggered on (Zaxara's real script shape), never its own
// cast-time X and never 0. The corpus carriers all gate on Card.hasXCost,
// which the filter does not implement yet (unknown predicate fails closed), so
// this pins the machinery behind a ValidCard$ the trigger CAN match today.
func TestThirdPartyCastTriggerReadsTheTriggeringSpellsX(t *testing.T) {
	t.Parallel()
	muse := "Name:Hydra Muse\nManaCost:1 G\nTypes:Creature Nightmare Hydra\nPT:1/1\n" +
		"T:Mode$ SpellCast | ValidCard$ Instant | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigPut | TriggerDescription$ Whenever you cast an instant spell, put X +1/+1 counters on CARDNAME.\n" +
		"SVar:TrigPut:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ X\n" +
		"SVar:X:Count$xPaid\n" +
		"Oracle:x\n"
	xBolt := "Name:X Bolt\nManaCost:X R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
	e, cfg, find := etbConfig(t, 119, []string{muse, xBolt}, nil)
	museID := putCreature(t, e, 0, muse)
	instID := find("X Bolt", 0)
	addMana(t, e, 0, "RRRR") // {3}{R}
	submitChoices(t, e, castOptionFor(t, e, instID).Index)
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose {
		t.Fatalf("X decision %+v", d)
	} else {
		submitChoices(t, e, 3)
	}
	// Target the opponent, not the muse: the 1 damage must not eat the
	// counters the trigger just put on it.
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision: %+v", d)
	} else {
		tgt := -1
		for _, o := range d.Options {
			if o.Player == 1 {
				tgt = o.Index
			}
		}
		if tgt < 0 {
			t.Fatalf("no opponent option: %+v", d.Options)
		}
		submitChoices(t, e, tgt)
	}
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(museID); o.Zone != state.ZBattlefield || o.Counter("P1P1") != 3 {
		t.Fatalf("muse zone=%s counters=%d, want battlefield/3 (X of the triggering spell)",
			o.Zone, o.Counter("P1P1"))
	}
	replayCheck(t, e, cfg)
}

// castObjTargeting casts id and answers its target decision with the option
// carrying the given object, then drains the stack.
func castObjTargeting(t *testing.T, e *Engine, id, target state.ObjID) {
	t.Helper()
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision for %d: %+v", id, d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == target {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("no option for target %d: %+v", target, d.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)
}
