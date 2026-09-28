package rules

// TargetType$ target legality for stack objects (CR 115.5, 601.2c, 701.5a,
// 605.3a, 603.7). Before the TargetType$ work a counterspell whose
// TargetType$ names abilities (Stifle, Voidslime, ...) could never target
// the ability objects AbilityPush/TriggerPush mint, because
// legalTargetCandidates' stack branch skipped every Face-less object.
//
// These leaves pass on this build, so per the CR-lane rule (a passing leaf's
// guard comes OFF and the ordinary suite defends it) they run in the
// ordinary lane with no GORGE_CR_CONFORMANCE guard.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const stifleSrc = "Name:Stifle\nManaCost:U\nTypes:Instant\n" +
	"A:SP$ Counter | TgtPrompt$ Choose target ability | ValidTgts$ Card,Emblem | " +
	"TargetType$ Activated,Triggered | SpellDescription$ Counter target activated or triggered ability.\nOracle:x\n"

const voidslimeSrc = "Name:Voidslime\nManaCost:1 G U\nTypes:Instant\n" +
	"A:SP$ Counter | TgtPrompt$ Select target spell or ability | ValidTgts$ Card,Emblem | " +
	"TargetType$ Spell,Activated,Triggered | SpellDescription$ Counter target spell, activated ability, or triggered ability.\nOracle:x\n"

// counterspellSrc is the plain shape (Mana Leak's): TargetType$ Spell, no
// ability kind named.
const counterspellSrc = "Name:Counterspell\nManaCost:U U\nTypes:Instant\n" +
	"A:SP$ Counter | ValidTgts$ Card | TargetType$ Spell\nOracle:x\n"

// heraldSrc has an enter-the-battlefield trigger, so casting it and passing
// priority puts a real TriggerPush-minted ability object on the stack.
const heraldSrc = "Name:Herald\nManaCost:1 G\nTypes:Creature Elf Druid\nPT:2/2\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ When Herald enters the battlefield, draw a card.\n" +
	"SVar:TrigDraw:DB$ Draw | Defined$ You\nOracle:x\n"

// tapperSrc has a plain activated (non-mana) ability.
const tapperSrc = "Name:Tapper\nManaCost:1 U\nTypes:Creature Wizard\nPT:1/3\n" +
	"A:AB$ Draw | Cost$ T | Defined$ You | SpellDescription$ Tap: draw a card.\n" +
	"T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | Execute$ TrigTDraw | TriggerDescription$ At the beginning of your upkeep, draw a card.\n" +
	"SVar:TrigTDraw:DB$ Draw | Defined$ You\nOracle:x\n"

const grizzlySrc = "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// counterHands builds a two-seat engine at seat 0's main phase with the
// named cards dealt to the named seats' hands (the same direct-state licence
// handEngine takes: there is no logged path that deals an exact hand), plus
// the named permanents in play, and a pending priority decision for seat 0.
func counterHands(t *testing.T, hand0, hand1 []*cards.Card, board0, board1 []*cards.Card) *Engine {
	t.Helper()
	e := New(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	for p := state.PlayerID(0); p < 2; p++ {
		var ids []state.ObjID
		hand := hand0
		if p == 1 {
			hand = hand1
		}
		for _, c := range hand {
			o := e.G.AddObject(c, p)
			o.Zone = state.ZHand
			ids = append(ids, o.ID)
		}
		e.G.SetZone(state.ZHand, p, ids)
		var bids []state.ObjID
		board := board0
		if p == 1 {
			board = board1
		}
		for _, c := range board {
			o := e.G.AddObject(c, p)
			o.Zone = state.ZBattlefield
			bids = append(bids, o.ID)
		}
		e.G.SetZone(state.ZBattlefield, p, bids)
	}
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	return e
}

// handObj finds the id of the hand card named name on seat p.
func handObj(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZHand, p) {
		if e.G.Obj(id).Face().Name == name {
			return id
		}
	}
	t.Fatalf("seat %d's hand has no %q", p, name)
	return 0
}

// castOptionIdx returns the index of the option of kind kind for obj, or -1.
func castOptionIdx(d *decision.Decision, kind string, obj state.ObjID) int {
	for _, o := range d.Options {
		if o.Kind == kind && o.Obj == obj {
			return o.Index
		}
	}
	return -1
}

// passOnce submits the pass option for the pending decision, whatever seat
// owns it.
func passOnce(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision to pass")
	}
	idx := castOptionIdx(d, "pass", 0)
	if idx < 0 {
		t.Fatalf("pending decision %+v has no pass option", d)
	}
	submitChoices(t, e, idx)
}

// TestStifleCountersOpponentTriggeredAbility (CR 701.5a): a counterspell
// whose TargetType$ names abilities is offered when the only legal target is
// the triggered ability object a TriggerPush minted, and countering it
// removes that object from the stack WITHOUT a graveyard move -- an ability
// is not a card, so it takes the exile parking every ceased-to-exist ability
// takes (CR 608.2m), and the trigger's effect never happens.
func TestStifleCountersOpponentTriggeredAbility(t *testing.T) {
	t.Parallel()
	e := counterHands(t,
		[]*cards.Card{card(t, heraldSrc)},
		[]*cards.Card{card(t, stifleSrc)}, nil, nil)
	herald := handObj(t, e, 0, "Herald")
	stifle := handObj(t, e, 1, "Stifle")
	e.G.Players[0].Pool[state.MG] = 2
	e.G.Players[1].Pool[state.MU] = 2
	e.askPriority(0)

	// Cast Herald; it sits on the stack untargeted. The caster receives
	// priority back first (CR 117.3c's shape the engine uses).
	submitChoices(t, e, passToCast(t, e, herald))
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's follow-up priority, got %+v", d)
	}
	passOnce(t, e) // seat 0 passes -> seat 1's priority, Herald spell still on the stack

	// Seat 1's priority: the stack holds ONLY the Herald spell, and
	// Stifle's TargetType$ admits no spell, so it must not be offered
	// (CR 601.2c: a spell with no legal target may not be announced).
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 1 {
		t.Fatalf("expected seat 1's priority, got %+v", d)
	}
	if idx := castOptionIdx(d, "cast", stifle); idx >= 0 {
		t.Fatalf("Stifle offered with only a spell on the stack: %+v", d.Options)
	}
	passOnce(t, e) // seat 1 passes; Herald resolves, its ETB trigger is pushed

	// The trigger object is atop the stack; the active player (seat 0) gets
	// priority first, then seat 1 may cast Stifle at it.
	var trigID state.ObjID
	for _, id := range e.G.Zone(state.ZStack, 0) {
		if o := e.G.Obj(id); o != nil && o.Ability != nil {
			trigID = id
		}
	}
	if trigID == 0 {
		t.Fatalf("no ability object on the stack: %+v", e.G.Zone(state.ZStack, 0))
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority after the trigger pushed, got %+v", d)
	}
	passOnce(t, e)
	d = e.Pending()
	if d == nil || d.Player != 1 {
		t.Fatalf("expected seat 1's priority with the trigger on the stack, got %+v", d)
	}
	if idx := castOptionIdx(d, "cast", stifle); idx < 0 {
		t.Fatalf("Stifle not offered with a triggered ability on the stack: %+v", d.Options)
	} else {
		submitChoices(t, e, idx)
	}

	// The target ask must offer the trigger object, labelled without
	// dereferencing its nil Face.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision after casting Stifle, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != trigID {
		t.Fatalf("trigger not the (only) offered target: %+v", d.Options)
	}
	if d.Options[0].Label == "" {
		t.Fatal("trigger target option has an empty label")
	}
	submitChoices(t, e, d.Options[0].Index)

	// Resolution: seat 1 passes, seat 0 passes, Stifle resolves. Genesis's
	// opening-hand draws are already in the log, so the "the trigger never
	// drew" assertion counts from the baseline captured here.
	drawsBefore := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw {
			drawsBefore++
		}
	}
	passOnce(t, e)
	passOnce(t, e)

	if o := e.G.Obj(trigID); o.Zone != state.ZExile {
		t.Fatalf("countered trigger went to %s, want exile", o.Zone)
	}
	counter := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == trigID &&
			ev.From == state.ZStack && ev.To == state.ZExile && ev.Text == "countered" {
			counter = true
		}
	}
	if !counter {
		t.Fatal("no countered MoveZone Stack->Exile for the trigger")
	}
	drawsAfter := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw {
			drawsAfter++
		}
	}
	if drawsAfter != drawsBefore {
		t.Fatalf("the countered ETB trigger still drew: %d draws after vs %d before", drawsAfter, drawsBefore)
	}
}

// TestStifleNotCastableWithOnlyASpellOnTheStack (CR 601.2c) pins the offer
// gate in isolation: with a spell on the stack and nothing else, a
// TargetType$ Activated,Triggered counter is not offered at all.
func TestStifleNotCastableWithOnlyASpellOnTheStack(t *testing.T) {
	t.Parallel()
	e := counterHands(t,
		[]*cards.Card{card(t, grizzlySrc)},
		[]*cards.Card{card(t, stifleSrc)}, nil, nil)
	bear := handObj(t, e, 0, "Grizzly Bears")
	e.G.Players[0].Pool[state.MG] = 2
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, bear))
	passOnce(t, e) // the caster passes -> seat 1's priority, spell on the stack
	d := e.Pending()
	if d == nil || d.Player != 1 {
		t.Fatalf("expected seat 1's priority, got %+v", d)
	}
	if idx := castOptionIdx(d, "cast", handObj(t, e, 1, "Stifle")); idx >= 0 {
		t.Fatalf("Stifle offered with only a spell on the stack: %+v", d.Options)
	}
}

// stackTargetsFixture mints one object of each stack kind directly (the
// same licence handEngine takes): a spell card object, an activated ability
// object and a triggered ability object, all controller by seat 1 -- plus a
// second activated ability controlled by seat 0 for the YouCtrl test.
func stackTargetsFixture(t *testing.T) (*Engine, spellActTrig) {
	t.Helper()
	e := counterHands(t, nil, nil, nil, []*cards.Card{card(t, tapperSrc)})
	perm := e.G.Zone(state.ZBattlefield, 1)[0]
	face := e.G.Obj(perm).Face()

	spell := e.G.AddObject(card(t, grizzlySrc), 1)
	spell.Zone = state.ZStack
	activated := e.G.AddObject(nil, 1)
	activated.Ability = face.Abilities[0]
	activated.Source = perm
	activated.Zone = state.ZStack
	triggered := e.G.AddObject(nil, 1)
	triggered.Ability = face.Triggers[0].Effect
	triggered.Source = perm
	triggered.Zone = state.ZStack
	mine := e.G.AddObject(nil, 0)
	mine.Ability = face.Abilities[0]
	mine.Source = perm
	mine.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{spell.ID, activated.ID, triggered.ID, mine.ID})
	return e, spellActTrig{spell.ID, activated.ID, triggered.ID, mine.ID, perm}
}

type spellActTrig struct {
	spell, activated, triggered, mine, perm state.ObjID
}

// censusOf runs legalTargetCandidates for sa from chooser you and returns the
// offered object ids in order.
func censusOf(t *testing.T, e *Engine, you state.PlayerID, sa *cards.SA) []state.ObjID {
	t.Helper()
	var out []state.ObjID
	for _, c := range e.legalTargetCandidates(you, 0, 0, sa) {
		if c.obj != 0 {
			out = append(out, c.obj)
		}
	}
	return out
}

// TestVoidslimeTargetsSpellActivatedAndTriggered (CR 115.1/601.2c): a
// `TargetType$ Spell,Activated,Triggered` counter can target each of the
// three stack kinds, while a TargetType$-less counter (Spell default) sees
// only the spell and Stifle's Activated,Triggered sees only the abilities.
func TestVoidslimeTargetsSpellActivatedAndTriggered(t *testing.T) {
	t.Parallel()
	e, ids := stackTargetsFixture(t)

	stifle := card(t, stifleSrc).Faces[0].SpellAbility()
	got := censusOf(t, e, 0, stifle)
	if containsID(got, ids.spell) || !containsID(got, ids.activated) || !containsID(got, ids.triggered) {
		t.Fatalf("Stifle census %v, want the two abilities only (activated %d, triggered %d, not %d)",
			got, ids.activated, ids.triggered, ids.spell)
	}
	if !containsID(got, ids.mine) {
		// ids.mine is an activated ability too: it MUST be offered (same
		// kind, no controller qualifier) -- guarding against an accidental
		// controller filter on the unqualified token.
		t.Fatalf("Stifle census dropped an unqualified activated ability: %v", got)
	}

	voidslime := card(t, voidslimeSrc).Faces[0].SpellAbility()
	got = censusOf(t, e, 0, voidslime)
	for _, want := range []state.ObjID{ids.spell, ids.activated, ids.triggered, ids.mine} {
		if !containsID(got, want) {
			t.Fatalf("Voidslime census %v missing %d", got, want)
		}
	}

	counterspell := card(t, counterspellSrc).Faces[0].SpellAbility()
	got = censusOf(t, e, 0, counterspell)
	// TargetType$ Spell and nothing else: the stack is searched (the
	// TargetType$ names a stack kind) and only the spell object is admitted
	// from it -- no ability object appears. The battlefield tapper is NOT in
	// this census at all: TargetType$ Spell routes the search to the stack
	// alone, which is what makes a plain counterspell never offer a
	// permanent (the pre-TargetType$ bug the targetZones doc records).
	if len(got) != 1 || got[0] != ids.spell {
		t.Fatalf("plain Spell counter census %v, want exactly the spell %d", got, ids.spell)
	}
}

// TestTargetTypeYouCtrlAdmitsOnlyYourAbilities: Weaver of Harmony's
// `TargetType$ Activated.YouCtrl,Triggered.YouCtrl` reads the token's
// controller qualifier -- an opponent's ability is not offered to you, your
// own is.
func TestTargetTypeYouCtrlAdmitsOnlyYourAbilities(t *testing.T) {
	t.Parallel()
	e, ids := stackTargetsFixture(t)
	src := "Name:Weaver\nManaCost:G\nTypes:Creature\nPT:1/1\n" +
		"A:AB$ CopySpellAbility | Cost$ G T | ValidTgts$ Card,Emblem | " +
		"TargetType$ Activated.YouCtrl,Triggered.YouCtrl | MayChooseTarget$ True | SpellDescription$ copy your ability.\nOracle:x\n"
	sa := card(t, src).Faces[0].Abilities[0]

	// "You" is the CHOOSER: from seat 1, seat 1's own abilities are
	// targetable and seat 0's are not; from seat 0 the reverse.
	fromSeat1 := censusOf(t, e, 1, sa)
	if !containsID(fromSeat1, ids.activated) || !containsID(fromSeat1, ids.triggered) || containsID(fromSeat1, ids.mine) {
		t.Fatalf("YouCtrl census from seat 1 %v, want only seat 1's own abilities", fromSeat1)
	}
	fromSeat0 := censusOf(t, e, 0, sa)
	if len(fromSeat0) != 1 || fromSeat0[0] != ids.mine {
		t.Fatalf("YouCtrl census from seat 0 %v, want only seat 0's own ability %d", fromSeat0, ids.mine)
	}
}

// TestActivatedAbilityCanBeCountered (CR 701.5a, 608.2m): end to end, a
// countered ACTIVATED ability is removed from the stack to the exile
// parking, never the graveyard, and never resolves.
func TestActivatedAbilityCanBeCountered(t *testing.T) {
	t.Parallel()
	e := counterHands(t,
		[]*cards.Card{card(t, stifleSrc)}, nil,
		nil, []*cards.Card{card(t, tapperSrc)})
	stifle := handObj(t, e, 0, "Stifle")
	e.G.Players[0].Pool[state.MU] = 2
	e.askPriority(0)

	// Seat 1 activates its Tapper's draw ability.
	e.G.Priority = 1
	e.askPriority(1)
	d := e.Pending()
	if d == nil || d.Player != 1 {
		t.Fatalf("expected seat 1's priority, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("seat 1 has no ability option: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// The activator receives priority back first; pass it so seat 0 gets
	// its turn to respond with the ability object atop the stack.
	passOnce(t, e)

	// Baseline for the "never resolved" assertion: genesis's opening-hand
	// draws are already in the log, so count from here.
	drawsBefore := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw {
			drawsBefore++
		}
	}
	var actID state.ObjID
	for _, id := range e.G.Zone(state.ZStack, 0) {
		if o := e.G.Obj(id); o != nil && o.Ability != nil {
			actID = id
		}
	}
	if actID == 0 {
		t.Fatalf("activated ability not on the stack: %+v", e.G.Zone(state.ZStack, 0))
	}

	// Seat 0 (active) gets priority first: cast Stifle at the ability.
	d = e.Pending()
	if d == nil || d.Player != 0 || d.Kind != decision.KPriority {
		t.Fatalf("expected seat 0's priority, got %+v", d)
	}
	if cidx := castOptionIdx(d, "cast", stifle); cidx < 0 {
		t.Fatalf("Stifle not offered against an activated ability: %+v", d.Options)
	} else {
		submitChoices(t, e, cidx)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || len(d.Options) != 1 || d.Options[0].Obj != actID {
		t.Fatalf("activated ability not the offered counter target: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passOnce(t, e)
	passOnce(t, e)

	if o := e.G.Obj(actID); o.Zone != state.ZExile {
		t.Fatalf("countered activated ability went to %s, want exile", o.Zone)
	}
	drawsAfter := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw {
			drawsAfter++
		}
	}
	if drawsAfter != drawsBefore {
		t.Fatalf("the countered ability still resolved: %d draws after vs %d before", drawsAfter, drawsBefore)
	}
}

// TestManaAbilityNeverTargetable (CR 605.3a/b): a mana ability never uses
// the stack at all -- the offer loop skips AB$ Mana -- so there is nothing
// for an ability counter to point at. Pin the offer side.
func TestManaAbilityNeverTargetable(t *testing.T) {
	t.Parallel()
	src := "Name:Petall\nManaCost:0\nTypes:Creature Plant\nPT:0/1\n" +
		"A:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ add G\n" +
		"A:AB$ Draw | Cost$ T | Defined$ You | SpellDescription$ draw\nOracle:x\n"
	e := counterHands(t, nil, nil, []*cards.Card{card(t, src)}, nil)
	e.G.Priority = 0
	e.askPriority(0)
	d := e.Pending()
	n := 0
	for _, o := range d.Options {
		if o.Kind == "ability" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want exactly the one non-mana ability option, got %d: %+v", n, d.Options)
	}
}

// spiderSenseSrc is the bare-base shape (Spider Sense's): the Instant and
// Sorcery tokens name card objects restricted to that card type, not every
// spell.
const spiderSenseSrc = "Name:Spider-Sense\nManaCost:1 U\nTypes:Instant\n" +
	"A:SP$ Counter | TgtPrompt$ Select target spell or ability | ValidTgts$ Card,Emblem | " +
	"TargetType$ Instant,Sorcery,Triggered | SpellDescription$ Counter target instant spell, sorcery spell, or triggered ability.\nOracle:x\n"

// sisterSrc is the qualified-Spell shape (Sister of Silence's): the
// restriction rides the Spell token's qualifiers, not the base.
const sisterSrc = "Name:Sister of Silence\nManaCost:1 W\nTypes:Creature Cleric\nPT:1/2\n" +
	"A:SP$ Counter | TgtPrompt$ Select target instant spell, sorcery spell, activated ability, or triggered ability | ValidTgts$ Card,Emblem | " +
	"TargetType$ Spell.Instant,Spell.Sorcery,Activated,Triggered\nOracle:x\n"

// envelopSrc is the single-restriction base (Dispel/Envelop's): TargetType$
// Instant alone, no ability kind.
const envelopSrc = "Name:Envelop\nManaCost:2 U\nTypes:Instant\n" +
	"A:SP$ Counter | ValidTgts$ Card | TargetType$ Instant\nOracle:x\n"

const flashSrc = "Name:Flashcard\nManaCost:U\nTypes:Instant\nOracle:x\n"
const riteSrc = "Name:Ritepiece\nManaCost:B\nTypes:Sorcery\nOracle:x\n"

// TestInstantSorceryCounterRestrictsCardObjects (CR 115.1/601.2c): an
// Instant/Sorcery TargetType$ token does NOT collapse into the unrestricted
// Spell kind -- a creature spell on the stack is not a legal target for
// Spider Sense's `Instant,Sorcery,Triggered` (bare bases) or Sister of
// Silence's `Spell.Instant,Spell.Sorcery,...` (qualified bases), while a
// real instant, a real sorcery and the ability objects still are.
func TestInstantSorceryCounterRestrictsCardObjects(t *testing.T) {
	t.Parallel()
	e, ids := stackTargetsFixture(t)
	inst := e.G.AddObject(card(t, flashSrc), 1)
	inst.Zone = state.ZStack
	sorc := e.G.AddObject(card(t, riteSrc), 1)
	sorc.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, append(e.G.Zone(state.ZStack, 0), inst.ID, sorc.ID))

	spider := card(t, spiderSenseSrc).Faces[0].SpellAbility()
	got := censusOf(t, e, 0, spider)
	for _, want := range []state.ObjID{inst.ID, sorc.ID, ids.triggered} {
		if !containsID(got, want) {
			t.Fatalf("Spider-Sense census %v missing %d (instant/sorcery spell or triggered ability)", got, want)
		}
	}
	// Spider Sense's card text is "instant spell, sorcery spell, or triggered
	// ability": neither the creature spell nor the ACTIVATED ability is
	// legal (no Activated token in its TargetType$).
	if containsID(got, ids.spell) {
		t.Fatalf("Instant/Sorcery counter illegally offers creature spell %d: %v", ids.spell, got)
	}
	if containsID(got, ids.activated) {
		t.Fatalf("Spider-Sense (no Activated token) illegally offers activated ability %d: %v", ids.activated, got)
	}

	sister := card(t, sisterSrc).Faces[0].SpellAbility()
	got = censusOf(t, e, 0, sister)
	for _, want := range []state.ObjID{inst.ID, sorc.ID, ids.activated, ids.triggered} {
		if !containsID(got, want) {
			t.Fatalf("Sister-of-Silence census %v missing %d", got, want)
		}
	}
	if containsID(got, ids.spell) {
		t.Fatalf("Spell.Instant/Spell.Sorcery counter illegally offers creature spell %d: %v", ids.spell, got)
	}
}

// TestInstantCounterNotCastableAgainstCreatureSpell (CR 601.2c): with a
// creature spell the ONLY object on the stack, a `TargetType$ Instant`
// counter (Dispel/Envelop's shape) has no legal target and is not offered at
// all -- the census restriction reaches the cast-offer gate.
func TestInstantCounterNotCastableAgainstCreatureSpell(t *testing.T) {
	t.Parallel()
	e := counterHands(t,
		[]*cards.Card{card(t, grizzlySrc)},
		[]*cards.Card{card(t, envelopSrc)}, nil, nil)
	e.G.Players[0].Pool[state.MG] = 2
	e.G.Players[1].Pool[state.MG] = 2
	e.G.Players[1].Pool[state.MU] = 1
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, handObj(t, e, 0, "Grizzly Bears")))
	passOnce(t, e)
	d := e.Pending()
	if d == nil || d.Player != 1 {
		t.Fatalf("expected seat 1's priority, got %+v", d)
	}
	if idx := castOptionIdx(d, "cast", handObj(t, e, 1, "Envelop")); idx >= 0 {
		t.Fatalf("Instant-only counter offered against a creature spell: %+v", d.Options)
	}
}
