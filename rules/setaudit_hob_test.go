package rules

// Set audit: The Hobbit (hob), 193 cards. Tests minted from the audit are
// named TestSetAudit_hob_<Card>_<Behaviour>. A test that asserts CORRECT
// behaviour and passes is regression coverage and stays unguarded. A test
// that asserts correct behaviour the engine does NOT have is guarded by
// GORGE_SET_AUDIT so the committed suite stays green; its guard names the
// defect and the follow-up ticket.

import (
	"os"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// hobGuard skips a finding test unless GORGE_SET_AUDIT is set.
func hobGuard(t *testing.T, defect, ticket string) {
	t.Helper()
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skip("set-audit finding (hob): " + defect + ". Follow-up: " + ticket)
	}
}

// hobCard is reg.Lookup or a fatal: a corpus card a finding test depends on
// being absent is a corpus-pin change, not something to paper over.
func hobCard(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus fixture: %q missing from the registry", name)
	}
	return c
}

// hobHasType reports whether a face's type line holds t.
func hobHasType(f *cards.Face, t string) bool {
	if f == nil {
		return false
	}
	for _, x := range f.Types {
		if x == t {
			return true
		}
	}
	return false
}

// hobPut puts a corpus card onto seat p's battlefield eventlessly (as the
// other onBoard helpers do) and returns its id.
func hobPut(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, p)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, p, append(e.G.Zone(state.ZBattlefield, p), o.ID))
	return o.ID
}

// hobSeekUnlessPay passes priority decisions and any mid-resolution search
// ask until the mid-resolution unless-pay KModes is pending, returning nil if
// none is posed within limit.
func hobSeekUnlessPay(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			return nil
		}
		if d.Kind == decision.KModes && d.ResumeKind == "unless_pay" {
			return d
		}
		switch d.Kind {
		case decision.KPriority:
			castFirst(t, e, "pass")
		case decision.KChoose:
			// A library search or similar mid-resolution pick: take the
			// first offered card so the activation can reach its unless gate.
			if len(d.Options) == 0 {
				return nil
			}
			submitChoices(t, e, d.Options[0].Index)
		default:
			t.Fatalf("unexpected decision %v (seat %d) while seeking the unless-pay ask: %+v", d.Kind, d.Player, d)
		}
	}
	return nil
}

// TestSetAudit_hob_ElvenPassage_BeholdUntapsSearchedLand: Elven Passage is
// the corpus's switched Behold carrier:
//
//	You may behold an Elf. If you do, untap that land.
//	(To behold an Elf, choose an Elf you control or reveal an Elf card from your hand.)
//
// CR 702.176 (Behold) makes beholding a choice that may name a permanent you
// control or a card in your hand; CR 608.2 applies the "if you do" body to
// the chosen branch. The Forge script carries UnlessCost$ Behold<1/Elf>,
// which ParseUnlessCost now prices (an Elf you control or an Elf card in
// hand), so the switched Untap body runs when the payer beholds.
func TestSetAudit_hob_ElvenPassage_BeholdUntapsSearchedLand(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	elf := card(t, "Name:Elf Scout\nTypes:Creature Elf Scout\nPT:1/1\nOracle:x\n")
	e := handEngine(t, hobCard(t, reg, "Elven Passage"))
	elfID := hobPut(t, e, 0, elf)
	passage := hobPut(t, e, 0, hobCard(t, reg, "Elven Passage"))
	e.askPriority(0)

	if o := e.G.Obj(passage); o == nil || o.Face() == nil || o.Face().Name != "Elven Passage" || o.Tapped {
		t.Fatalf("Elven Passage precondition: %+v, want untapped on the battlefield", o)
	}
	if o := e.G.Obj(elfID); o == nil || o.Zone != state.ZBattlefield || !hobHasType(o.Face(), "Creature") {
		t.Fatalf("Elf precondition: %+v", e.G.Obj(elfID))
	}
	before := append([]state.ObjID(nil), e.G.Zone(state.ZBattlefield, 0)...)
	opt := abilityOption(t, e, passage, 0)
	submitChoices(t, e, opt.Index)

	ask := hobSeekUnlessPay(t, e, 60)
	if ask == nil {
		t.Fatal("no unless-pay ask posed for Elven Passage's Behold branch")
	}
	if len(ask.Options) < 2 {
		t.Fatalf("Behold pay branch not offered: options = %+v", ask.Options)
	}
	// Precondition: the search put a NEW permanent on the battlefield, so
	// there is a land whose untap the beholding should cause. The activation
	// sacrificed Elven Passage itself (its Sac<1/CARDNAME> cost), so the
	// searched land is the one battlefield id that was not there before the
	// activation.
	after := e.G.Zone(state.ZBattlefield, 0)
	wasThere := make(map[state.ObjID]bool, len(before))
	for _, id := range before {
		wasThere[id] = true
	}
	var searched state.ObjID
	for _, id := range after {
		if !wasThere[id] {
			searched = id
		}
	}
	if searched == 0 {
		t.Fatalf("searched land precondition: no new permanent entered (before %v, after %v)", before, after)
	}
	if o := e.G.Obj(searched); o == nil || !o.Tapped {
		t.Fatalf("searched land precondition: %+v, want it on the battlefield tapped", e.G.Obj(searched))
	}
	submitChoices(t, e, ask.Options[0].Index) // beholding pay branch
	hobDrain(t, e, 60)
	if o := e.G.Obj(searched); o == nil || o.Tapped {
		t.Fatalf("searched land = %+v, want untapped after beholding an Elf (CR 702.176)", o)
	}
}

// TestSetAudit_hob_GreatGildedBoat_RecruitIsImplemented: Recruit is one of
// the set's named keyword actions. Its rules text --
//
//	(Draw a card, then discard a card. If you discarded a nonland card,
//	 create a 1/1 white Human Soldier creature token.)
//
// -- is a real action (CR 701.9): draw a card, discard a card, then create a
// Human Soldier if the discarded card was nonland. The ETB trigger on
// Celebrate the Mountain-king must execute the draw and reach the discard ask.
func TestSetAudit_hob_GreatGildedBoat_RecruitDrawsThenAsksDiscard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, hobCard(t, reg, "Celebrate the Mountain-king"),
		card(t, "Name:Recruit Hand Filler\nTypes:Instant\nOracle:x\n"))
	var recruit state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Celebrate the Mountain-king" {
			recruit = id
		}
	}
	if recruit == 0 {
		t.Fatal("precondition: Recruit carrier not in hand")
	}
	// Place the carrier on the battlefield and queue its compiled ETB ability
	// directly. This isolates Recruit resolution from the trigger-event setup.
	e.emit(events.Event{Kind: events.MoveZone, Obj: recruit, From: state.ZHand, To: state.ZBattlefield})
	if e.G.Obj(recruit).Zone != state.ZBattlefield {
		t.Fatal("precondition: Recruit carrier did not enter")
	}
	triggerIdx := -1
	for i, tr := range e.G.Obj(recruit).Face().Triggers {
		if tr.Params["Execute"] == "TrigRecruit" {
			triggerIdx = i
			break
		}
	}
	if triggerIdx < 0 {
		t.Fatal("precondition: compiled Recruit trigger missing")
	}
	// A draw needs a nonempty library; handEngine's minimal fixture has none.
	libCard := card(t, "Name:Recruit Library Filler\nTypes:Instant\nOracle:x\n")
	var library []state.ObjID
	for i := 0; i < 3; i++ {
		o := e.G.AddObject(libCard, 0)
		o.Zone = state.ZLibrary
		library = append(library, o.ID)
	}
	e.G.SetZone(state.ZLibrary, 0, library)
	e.pushTrigger(pendingTrigger{Source: recruit, Controller: 0, Idx: triggerIdx, SA: e.G.Obj(recruit).Face().Triggers[triggerIdx].Effect})
	if len(e.G.Stack) == 0 {
		t.Fatal("precondition: Recruit ETB trigger was not put on the stack")
	}
	before := len(e.G.Zone(state.ZHand, 0))
	e.resolveTop()
	if got := len(e.G.Zone(state.ZHand, 0)); got != before+1 {
		t.Fatalf("Recruit hand after resolving ETB = %d, want %d after its draw (CR 701.9)", got, before+1)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Recruit discard decision = %+v, want a choice after drawing", d)
	}
}

// TestSetAudit_hob_BardKingOfDale_ReplacesExtraDrawWithTwo is a guarded
// finding: Bard's CR 616.1 draw replacement should make an extra draw into two.
func TestSetAudit_hob_BardKingOfDale_ReplacesExtraDrawWithTwo(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, hobCard(t, reg, "Bard, King of Dale"))
	bard := hobPut(t, e, 0, hobCard(t, reg, "Bard, King of Dale"))
	if o := e.G.Obj(bard); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Bard is not on battlefield")
	}
	before := len(e.G.Zone(state.ZHand, 0))
	emitDraw(t, e, 0) // handEngine is outside the draw step: this is an extra draw.
	if got := len(e.G.Zone(state.ZHand, 0)) - before; got != 2 {
		t.Fatalf("Bard extra-draw hand delta = %d, want 2 (CR 616.1)", got)
	}
}

// TestSetAudit_hob_GollumRiddleMaster_ChoosesOddOrEven checks the actual
// ETB replacement choice, not merely registration. CR 614.1: the replacement
// effect modifies how Gollum enters and must ask for the chosen quality.
func TestSetAudit_hob_GollumRiddleMaster_ChoosesOddOrEven(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, hobCard(t, reg, "Gollum, Riddle Master"))
	e.askPriority(0)
	addMana(t, e, 0, "1B")
	opt := castByName(t, e, 0, "Gollum, Riddle Master")
	if opt == nil {
		t.Fatal("precondition: Gollum is not castable")
	}
	submitChoices(t, e, opt.Index)
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "etb" {
			if len(d.Options) != 2 || d.Options[0].Label != "Odd" || d.Options[1].Label != "Even" {
				t.Fatalf("Gollum entry choice = %+v, want Odd/Even", d.Options)
			}
			submitChoices(t, e, d.Options[1].Index)
			for _, id := range e.G.Zone(state.ZBattlefield, 0) {
				o := e.G.Obj(id)
				if o != nil && o.Face() != nil && o.Face().Name == "Gollum, Riddle Master" {
					if o.ChosenType != "even" {
						t.Fatalf("Gollum chosen quality = %q, want even", o.ChosenType)
					}
					return
				}
			}
			t.Fatal("precondition failed: Gollum did not enter after answering ETB choice")
		}
		if d.Kind == decision.KModes {
			t.Fatalf("unexpected modes choice before Gollum entry choice: %+v", d)
		}
		if d.Kind == decision.KPriority {
			castFirst(t, e, "pass")
			continue
		}
		t.Fatalf("unexpected decision before Gollum ETB choice: %+v", d)
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Gollum, Riddle Master" {
			t.Fatal("Gollum entered without an odd/even choice (CR 614.1)")
		}
	}
	t.Fatal("precondition failed: Gollum did not enter; no entry-choice behavior was tested")
}

// drainToEnd answers every pending decision deterministically until the stack
// empties, so a probing test can inspect the final board.
func hobDrain(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			castFirst(t, e, "pass")
		default:
			if len(d.Options) == 0 {
				return
			}
			submitChoices(t, e, d.Options[0].Index)
		}
	}
}

// TestSetAudit_hob_GoblinPlateMail_AmassesThenAttaches: the Equipment's ETB
// amasses Goblins 1 and then attaches itself to the amassed Army (the cards
// "then attach this Equipment to the amassed Army"). CR 701.3/701.34: the
// amass creates (or grows) the Army, and the attach must name THAT object.
func TestSetAudit_hob_GoblinPlateMail_AmassesThenAttaches(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngineTokens(t, hobCard(t, reg, "Goblin Plate Mail"))
	e.askPriority(0)
	mail := e.G.Zone(state.ZHand, 0)[0]
	// Play the artifact through the ordinary cast offer so its ETB trigger
	// fires through real events (eventless placement fires no trigger).
	addMana(t, e, 0, "BR")
	opt := castByName(t, e, 0, "Goblin Plate Mail")
	if opt == nil {
		t.Fatalf("Goblin Plate Mail not castable: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	hobDrain(t, e, 60)

	o := e.G.Obj(mail)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Goblin Plate Mail precondition: %+v, want on the battlefield", o)
	}
	var armies []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if id == mail {
			continue
		}
		ao := e.G.Obj(id)
		if ao != nil && ao.Face() != nil && hobHasType(ao.Face(), "Creature") && ao.Face().Name == "Goblin Army" {
			armies = append(armies, id)
		}
	}
	// Fall back to any 0/0 Army token the amass minted.
	if len(armies) == 0 {
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if id == mail {
				continue
			}
			ao := e.G.Obj(id)
			if ao != nil && ao.Face() != nil && hobHasType(ao.Face(), "Creature") && hobHasType(ao.Face(), "Army") {
				armies = append(armies, id)
			}
		}
	}
	if len(armies) == 0 {
		var names []string
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if ao := e.G.Obj(id); ao != nil && ao.Face() != nil {
				names = append(names, ao.Face().Name)
			}
		}
		t.Fatalf("amass precondition: Goblin Plate Mail's ETB minted no Army to attach to; battlefield = %v", names)
	}
	army := armies[0]
	if got := e.G.Obj(mail).AttachedTo; got != army {
		t.Fatalf("Goblin Plate Mail AttachedTo = %d, want the amassed Army %d", got, army)
	}
}

// TestSetAudit_hob_Bombur_DoesNotUntapWithoutEnduringStory pins the Storied
// replacement on Bombur, Gentle Dreamer:
//
//	Bombur doesn't untap during your untap step unless you have an enduring story.
//
// The Forge script is `R:Event$ Untap | ValidCard$ Card.Self |
// ValidStepTurnToController$ You | Layer$ CantHappen | EnduringStory$ False`.
// CR 702.175: without the enduring story the replacement must apply and the
// untap cannot happen; with it, Bombur untaps.
func TestSetAudit_hob_Bombur_DoesNotUntapWithoutEnduringStory(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, hobCard(t, reg, "Bombur, Gentle Dreamer"))
	bombur := hobPut(t, e, 0, hobCard(t, reg, "Bombur, Gentle Dreamer"))
	if o := e.G.Obj(bombur); o == nil || !o.Face().HasKeyword("Storied") {
		t.Fatalf("precondition: Bombur lacks Storied: %+v", e.G.Obj(bombur))
	}
	if got := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.Untap }); got != 0 {
		t.Fatalf("precondition: %d untap events already", got)
	}
	// Tap Bombur, then run the untap step of seat 0's next turn with no
	// enduring story: he must stay tapped.
	e.emit(events.Event{Kind: events.Tap, Obj: bombur})
	if !e.G.Obj(bombur).Tapped {
		t.Fatal("precondition: Bombur did not tap")
	}
	if e.playerHasEnduringStory(0) {
		t.Fatal("precondition: seat 0 already has an enduring story")
	}
	// The replacement scopes itself with ValidStepTurnToController$ You, so
	// the scan must run inside seat 0's own untap step -- otherwise the
	// replacement is not even consulted and the test would pass vacuously.
	e.G.Step, e.G.Active = state.StepUntap, 0
	e.finishUntapStep(0)
	if !e.G.Obj(bombur).Tapped {
		t.Fatal("Bombur untapped without an enduring story (CR 702.175)")
	}
}

// hobGraveyardFiller puts n plain cards into seat p's graveyard.
func hobGraveyardFiller(t *testing.T, e *Engine, p state.PlayerID, n int) {
	t.Helper()
	filler := card(t, "Name:Set-Audit Filler\nTypes:Instant\nOracle:x\n")
	var ids []state.ObjID
	for i := 0; i < n; i++ {
		o := e.G.AddObject(filler, p)
		o.Zone = state.ZGraveyard
		ids = append(ids, o.ID)
	}
	e.G.SetZone(state.ZGraveyard, p, ids)
}

// TestSetAudit_hob_MastersCouncillors_CountsGraveyardsWithSevenPlus pins the
// Forge static
//
//	S:Mode$ Continuous | Affected$ Card.Self | AddPower$ X
//	SVar:X:PlayerCountPlayers$HasPropertyHasCardsInGraveyard_Card_GE7/Times.2
//
// -- "This creature gets +2/+0 for each graveyard with seven or more cards in
// it." CR 604.3 makes this a static ability generating a continuous effect;
// the player-count head enumerates PLAYERS whose graveyard holds at least
// seven cards, times two. The engine never reads the
// HasPropertyHasCardsInGraveyard_<types>_GE<n> head, so X evaluates to 0 and
// the ability is a silent no-op (the creature stays 1/3 even with a full
// graveyard).
func TestSetAudit_hob_MastersCouncillors_CountsGraveyardsWithSevenPlus(t *testing.T) {
	hobGuard(t, "the PlayerCountPlayers$HasPropertyHasCardsInGraveyard_Card_GE7/Times.2 count read from a continuous AddPower$ static resolves to 0, so Master's Councillors' +2/+0 per full graveyard is a no-op", "hob-hascardsingraveyard-count")
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, hobCard(t, reg, "Master's Councillors"))
	id := hobPut(t, e, 0, hobCard(t, reg, "Master's Councillors"))
	// Precondition: the printed base is 1/3 and the graveyard starts empty.
	if o := e.G.Obj(id); o == nil || o.Face() == nil || o.Face().Name != "Master's Councillors" || o.Zone != state.ZBattlefield {
		t.Fatalf("Master's Councillors precondition: %+v", o)
	}
	if p, tf := e.Derived(id).Power, e.Derived(id).Toughness; p != 1 || tf != 3 {
		t.Fatalf("printed precondition: %d/%d, want 1/3", p, tf)
	}
	hobGraveyardFiller(t, e, 0, 7)
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 7 {
		t.Fatalf("graveyard precondition: %d cards, want 7", got)
	}
	if p := e.Derived(id).Power; p != 3 {
		t.Fatalf("Master's Councillors power = %d, want 3 (base 1 + 2 for one graveyard holding >=7 cards, CR 604.3)", p)
	}
}

// TestSetAudit_hob_TheMasterOfLakeTown_DrawsForGraveyardsWithSevenPlus is
// regression coverage (it PASSES): the un-suffixed
// `PlayerCountPlayers$HasPropertyHasCardsInGraveyard_Card_GE7` head is read
// correctly, so a full graveyard makes the dies trigger draw one.
func TestSetAudit_hob_TheMasterOfLakeTown_DrawsForGraveyardsWithSevenPlus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, hobCard(t, reg, "The Master of Lake-town"))
	id := hobPut(t, e, 0, hobCard(t, reg, "The Master of Lake-town"))
	if o := e.G.Obj(id); o == nil || o.Face() == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("The Master of Lake-town precondition: %+v", o)
	}
	hobGraveyardFiller(t, e, 0, 7)
	before := len(e.G.Zone(state.ZHand, 0))
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	e.putTriggersOnStack()
	e.resolveTop()
	passUntilStackEmpty(t, e, 60)
	if got := len(e.G.Zone(state.ZHand, 0)); got != before+1 {
		t.Fatalf("hand after the dies trigger = %d, want %d (one draw for the graveyard holding >=7 cards)", got, before+1)
	}
}

// TestSetAudit_hob_CantankerousKeepers_AffinityForElvesReducesCost is
// regression coverage (it PASSES): Affinity for Elves (CR 702.41) reduces the
// generic cost by {1} per Elf controlled, so five Elves take {5}{G} to {G}.
func TestSetAudit_hob_CantankerousKeepers_AffinityForElvesReducesCost(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, hobCard(t, reg, "Cantankerous Keepers"))
	elf := card(t, "Name:Elf Scout\nTypes:Creature Elf Scout\nPT:1/1\nOracle:x\n")
	for i := 0; i < 5; i++ {
		hobPut(t, e, 0, elf)
	}
	var id state.ObjID
	for _, h := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(h); o != nil && o.Face() != nil && o.Face().Name == "Cantankerous Keepers" {
			id = h
		}
	}
	if id == 0 {
		t.Fatal("precondition: Cantankerous Keepers is not in hand")
	}
	if got := len(e.G.Zone(state.ZBattlefield, 0)); got != 5 {
		t.Fatalf("Elf precondition: %d on the battlefield, want 5", got)
	}
	if red := reduceOf(t, e, 0, id); red != 5 {
		t.Fatalf("Affinity for Elves reduction with 5 Elves = %d, want 5", red)
	}
}

// TestSetAudit_hob_NastyLittleRabbit_FerociousGate is regression coverage (it
// PASSES): Nasty Little Rabbit's Ferocious intervening-if
// (`IsPresent$ Creature.YouCtrl+powerGE4`, CR 603.4) must fire the
// beginning-of-combat counter only while a power-4-or-greater creature is
// controlled.
func TestSetAudit_hob_NastyLittleRabbit_FerociousGate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, hobCard(t, reg, "Nasty Little Rabbit"))
	rabbit := hobPut(t, e, 0, hobCard(t, reg, "Nasty Little Rabbit"))
	if o := e.G.Obj(rabbit); o == nil || o.Face() == nil || !hobHasType(o.Face(), "Creature") || o.Zone != state.ZBattlefield {
		t.Fatalf("Nasty Little Rabbit precondition: %+v", o)
	}
	e.G.Active = 0
	e.G.Step = state.StepMain1
	e.askPriority(0)
	// No power-4 creature: the intervening-if fails, no counter.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	e.putTriggersOnStack()
	for i := 0; i < 10 && len(e.G.Stack) > 0; i++ {
		e.resolveTop()
	}
	if c := e.G.Obj(rabbit).Counter("P1P1"); c != 0 {
		t.Fatalf("Ferocious with no power-4 creature: rabbit has %d +1/+1 counters, want 0", c)
	}
	big := card(t, "Name:Set-Audit Beast\nTypes:Creature Beast\nPT:4/4\nOracle:x\n")
	hobPut(t, e, 0, big)
	if p := e.Derived(e.G.Zone(state.ZBattlefield, 0)[len(e.G.Zone(state.ZBattlefield, 0))-1]).Power; p < 4 {
		t.Fatalf("power-4 precondition: the Beast has power %d", p)
	}
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	e.putTriggersOnStack()
	for i := 0; i < 10 && len(e.G.Stack) > 0; i++ {
		e.resolveTop()
	}
	if c := e.G.Obj(rabbit).Counter("P1P1"); c != 1 {
		t.Fatalf("Ferocious with a power-4 creature: rabbit has %d +1/+1 counters, want 1", c)
	}
}
