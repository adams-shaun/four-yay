package rules

// The ChangeZone hidden-origin param work (task inbox-paramcensus-changezone-
// hidden-reveal): Forge's ChangeZoneEffect reads five parameters this engine
// used to leave unread, grouped by shared code path. Every test here drives a
// real corpus card through the deck-engine fixture style; no Forge script
// text is committed.
//
//   - Hidden$ True routes a PUBLIC-origin ChangeZone with no Defined$ through
//     the hidden-origin pick (Forge's changeHiddenOriginResolve): the fetch
//     list is the origin zones' cards matching ChangeType$, the chooser picks
//     ChangeNum$ of them. Kor Skyfisher, Temur Sabertooth, Relic of
//     Progenitus (DefinedPlayer$/Chooser$ Targeted) and Burning Wish
//     (Origin$ Sideboard) all use this path.
//   - Reveal$ True (and the quality-search default) publicly reveal the found
//     cards: one Note carrying the moved ids, the Reveal primitive's payload.
//   - NoLooking$ True means the search's options carry no card names ("a
//     card") UNLESS the choosing player already legitimately learned the
//     card's identity in this chain -- a public reveal published it, or an
//     earlier named ask this player answered offered it. Forge's NoLooking$
//     protects a player who never looked, not one who already saw the cards
//     (task fb-20260922T235033Z). The placement legs of the Cultivate family
//     are the named case; a genuinely unseen card stays blind.
//   - ForgetChanged$ True drops each moved card from the remembered state
//     (ctx and the source's event-backed list, Choose "forget-remembered").
//   - DifferentNames$ True restricts the pick to one card per name: one
//     option Group per card name, enforced by Decision.Validate, with an
//     apply-side dedup for hosts that bypass the wire.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// chooseByObj submits the pending decision's option carrying obj.
func chooseByObj(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	for _, o := range d.Options {
		if o.Obj == obj {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no option for object %d in %+v", obj, d.Options)
}

// changeZoneAbilityIndexBy finds the activated ability index whose ChangeZone
// SA matches origin, fatal when absent.
func changeZoneAbilityIndexBy(t *testing.T, e *Engine, id state.ObjID, origin string) int {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("source %d has no face", id)
	}
	for i, sa := range o.Face().Abilities {
		if sa.Kind == "AB" && sa.API == "ChangeZone" && sa.Params["Origin"] == origin {
			return i
		}
	}
	t.Fatalf("%s has no ChangeZone ability from %s", o.Face().Name, origin)
	return -1
}

// castFixtureNamed casts the named corpus card from hand at the current
// priority (the caller has already funded the pool) and leaves the game at
// whatever the cast left pending.
func castFixtureNamed(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %s: %+v", name, d.Options)
	}
	submitChoices(t, e, idx)
	return id
}

// TestChangeZoneSearchRevealsTheFoundCards is the Reveal$ concern: Cultivate's
// search head (Reveal$ True) publicly reveals the cards it found -- one Note
// carrying the chosen ids, the same payload effReveal's public reveal emits --
// while the search's own options still carry names (the searching player is
// allowed to look at their own library).
func TestChangeZoneSearchRevealsTheFoundCards(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Cultivate")
	start := len(e.L.Events)
	_, d := castSearchSpell(t, e, "Cultivate")
	if d.Kind != decision.KChoose || d.Min != 0 || d.Max != 2 {
		t.Fatalf("head search = %v %d..%d, want KChoose 0..2", d.Kind, d.Min, d.Max)
	}
	if len(d.Options) < 2 {
		t.Fatalf("head search has %d options, need 2", len(d.Options))
	}
	for _, o := range d.Options {
		if o.Label == "a card" {
			t.Fatalf("a search with no NoLooking$ offered a blind option: %+v", o)
		}
	}
	picked := []decision.Option{d.Options[0], d.Options[1]}
	submitChoices(t, e, picked[0].Index, picked[1].Index)
	// The head's reveal Note carries exactly the two chosen ids, publicly
	// (no Secret marker; Player names the revealing seat).
	var reveal *events.Event
	for i := range e.L.Events[start:] {
		ev := e.L.Events[start+i]
		if ev.Kind == events.Note && ev.Player == 0 && len(ev.IDs) == 2 &&
			ev.IDs[0] == picked[0].Obj && ev.IDs[1] == picked[1].Obj {
			reveal = &ev
		}
	}
	if reveal == nil {
		t.Fatal("no reveal Note for the two found basic lands")
	}
	// The resolution completes: one basic enters tapped, the other reaches
	// the hand, and the chain replays byte-identically.
	d1 := e.Pending()
	if d1 == nil || d1.Kind != decision.KChoose {
		t.Fatalf("battlefield leg pending = %+v, want a KChoose", d1)
	}
	submitChoices(t, e, d1.Options[0].Index)
	if d2 := e.Pending(); d2 != nil && d2.Kind == decision.KChoose {
		submitChoices(t, e, d2.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 30)
	battlefield, hand := 0, 0
	for _, p := range picked {
		o := e.G.Obj(p.Obj)
		switch o.Zone {
		case state.ZBattlefield:
			battlefield++
		case state.ZHand:
			hand++
		}
	}
	if battlefield != 1 || hand != 1 {
		t.Fatalf("picked lands settled to battlefield=%d hand=%d, want 1/1", battlefield, hand)
	}
	replayCheck(t, e, cfg)
}

// TestChangeZoneHiddenPickAsksForTheBouncedPermanent is the Hidden$ concern
// on its public-origin trigger shape: Kor Skyfisher's ETB trigger (Hidden$
// True, ChangeType$ Permanent.YouCtrl, no Defined$) used to fall through to
// the object path and bounce ITSELF silently; it now poses the pick and moves
// the CHOSEN permanent.
func TestChangeZoneHiddenPickAsksForTheBouncedPermanent(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Kor Skyfisher")
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	_, d := castSearchSpell(t, e, "Kor Skyfisher")
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hidden_pick" {
		t.Fatalf("pending = %+v, want a hidden_pick KChoose", d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("Mandatory$ pick = %d..%d, want 1..1", d.Min, d.Max)
	}
	var bearOpt *decision.Option
	for i := range d.Options {
		o := &d.Options[i]
		if o.Obj == bear {
			bearOpt = o
		}
	}
	if bearOpt == nil || len(d.Options) != 2 {
		t.Fatalf("pick options = %+v, want exactly the bear and the fisher", d.Options)
	}
	start := len(e.L.Events)
	submitChoices(t, e, bearOpt.Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZHand {
		t.Fatalf("chosen bear = %+v, want in hand", e.G.Obj(bear))
	}
	fisher := e.G.Obj(d.Source)
	if fisher == nil || fisher.Zone != state.ZBattlefield {
		t.Fatalf("Kor Skyfisher left the battlefield: %+v", fisher)
	}
	moved := false
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.Obj == bear && ev.From == state.ZBattlefield && ev.To == state.ZHand {
			moved = true
		}
	}
	if !moved {
		t.Fatal("no MoveZone for the chosen permanent")
	}
	replayCheck(t, e, cfg)
}

// TestChangeZoneHiddenPickReturnsAnotherCreature exercises the activated
// ability shape: Temur Sabertooth's Hidden$ pick (ChangeType$
// Creature.YouCtrl+Other) must offer only OTHER creatures, never the source.
func TestChangeZoneHiddenPickReturnsAnotherCreature(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Temur Sabertooth")
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	addMana(t, e, 0, "GGCC")
	id := castFixtureNamed(t, e, "Temur Sabertooth")
	passUntilStackEmpty(t, e, 20)
	toMain1(t, e)
	addMana(t, e, 0, "GG")
	idx := changeZoneAbilityIndexBy(t, e, id, "Battlefield")
	submitChoices(t, e, abilityOption(t, e, id, idx).Index)
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hidden_pick" {
		t.Fatalf("pending = %+v, want a hidden_pick KChoose", d)
	}
	if d.Min != 0 || d.Max != 1 {
		t.Fatalf("optional pick = %d..%d, want 0..1", d.Min, d.Max)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != bear {
		t.Fatalf("pick options = %+v, want only the OTHER creature", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZHand {
		t.Fatalf("returned creature = %+v, want in hand", e.G.Obj(bear))
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Temur Sabertooth left the battlefield: %+v", e.G.Obj(id))
	}
	replayCheck(t, e, cfg)
}

// TestChangeZoneHiddenPickTargetsThePlayerGraveyard is the DefinedPlayer$/
// Chooser$ Targeted shape: Relic of Progenitus's "Target player exiles a card
// from their graveyard" used to be a silent no-op (the player fetchers were
// skipped); the pick now asks the TARGETED player over their own graveyard.
func TestChangeZoneHiddenPickTargetsThePlayerGraveyard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Relic of Progenitus")
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZGraveyard)
	relic := searchMoveByName(t, e, "Relic of Progenitus", state.ZBattlefield)
	idx := changeZoneAbilityIndexBy(t, e, relic, "Graveyard")
	submitChoices(t, e, abilityOption(t, e, relic, idx).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the player target ask", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Player == 0 {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("no self target offered: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hidden_pick" {
		t.Fatalf("pending = %+v, want the hidden_pick ask", d)
	}
	if d.Player != 0 {
		t.Fatalf("chooser = %d, want the targeted player 0", d.Player)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != bear {
		t.Fatalf("pick options = %+v, want the graveyard card", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZExile {
		t.Fatalf("exiled card = %+v, want in exile", e.G.Obj(bear))
	}
	replayCheck(t, e, cfg)
}

// TestChangeZoneWishFindsSideboard pins Burning Wish's real Origin$ Sideboard
// search against a compiled-corpus sideboard card. The sideboard is private
// to its owner and the wish's own SubAbility$ still runs after the pick.
func TestChangeZoneWishFindsSideboard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Burning Wish")
	cfg.Sideboards = [][]*cards.Card{{searchCorpusCard(t, reg, "Empty the Warrens")}, nil}
	e = New(cfg)
	e.Advance()
	toMain1(t, e)
	start := len(e.L.Events)
	addMana(t, e, 0, "CR")
	id := searchMoveByName(t, e, "Burning Wish", state.ZHand)
	d := castFixture(t, e, id, -1)
	if d == nil || len(d.Options) != 1 || d.Options[0].Label != "Empty the Warrens" {
		t.Fatalf("Burning Wish sideboard options = %+v, want the owner's Empty the Warrens", d)
	}
	// Precondition for the self-exile chain below: the wish is still on the
	// stack while the sideboard choice is pending. The choice's submission
	// drives the resolution (including DBChange's stack->exile) to
	// completion synchronously, so this is the pre-resolution boundary the
	// moveResolvedOffStack guard reads.
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack {
		t.Fatalf("Burning Wish before resolution completion = %+v, want on the stack", o)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	// The wish's own SubAbility$ (DBChange: Origin$ Stack → Destination$
	// Exile) runs while the spell is on the stack. Completion must not add a
	// trailing stack→graveyard move.
	exiled := false
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZStack && ev.To == state.ZExile {
			exiled = true
		}
	}
	if !exiled {
		t.Fatal("the wish's SubAbility$ self-exile did not run")
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
		t.Fatalf("Burning Wish final zone = %+v, want exile", o)
	}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZStack && ev.To == state.ZGraveyard {
			t.Fatalf("Burning Wish had a trailing stack-to-graveyard completion move: %+v", ev)
		}
	}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unrecognised ChangeZone Origin") {
			t.Fatalf("sideboard origin was treated as unknown: %+v", ev)
		}
	}
	if got := e.G.Obj(d.Options[0].Obj); got == nil || got.Zone != state.ZHand || got.EnteredFrom != state.ZSideboard {
		t.Fatalf("wished card = %+v, want owner's sideboard card in hand", got)
	}
	replayCheck(t, e, cfg)
}

// TestChangeZoneNoLookingLegsSeeRevealedCards is the NoLooking$ concern
// refined (task fb-20260922T235033Z): NoLooking$ is Forge's "do not look
// again", not a command to erase what the chooser already saw. Cultivate's
// head publicly reveals its finds and offers them by name, so the same
// resolution's NoLooking$ placement legs MUST carry the real basic-land
// names -- the old blanket "a card" placeholder hid information the chooser
// already held. A genuinely unseen card still stays blind
// (TestSearchLegStaysBlindWhenUnknown).
func TestChangeZoneNoLookingLegsSeeRevealedCards(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Cultivate")
	_, d := castSearchSpell(t, e, "Cultivate")
	picked := []decision.Option{d.Options[0], d.Options[1]}
	submitChoices(t, e, picked[0].Index, picked[1].Index)
	for _, leg := range []string{"battlefield", "hand"} {
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
			t.Fatalf("%s leg pending = %+v, want a search KChoose", leg, d)
		}
		if len(d.Options) == 0 {
			t.Fatalf("%s leg offered nothing", leg)
		}
		for _, o := range d.Options {
			if o.Label == "a card" {
				t.Fatalf("%s leg option is the blind placeholder: %+v", leg, o)
			}
			// Precondition: each option really is a named corpus card, so a
			// vacuous setup fails loudly rather than passing on a blank face.
			obj := e.G.Obj(o.Obj)
			if obj == nil || obj.Face() == nil || obj.Face().Name == "" {
				t.Fatalf("%s leg option %d object %d has no named printed face", leg, o.Index, o.Obj)
			}
			if o.Label != obj.Face().Name {
				t.Fatalf("%s leg option label %q != printed name %q", leg, o.Label, obj.Face().Name)
			}
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 30)
	replayCheck(t, e, cfg)
}

// TestChangeZoneForgetChangedDropsTheMovedCard is the ForgetChanged$ concern:
// each card the ForgetChanged$ legs move leaves the remembered state (the
// Choose "forget-remembered" event, the persistent half), which Cultivate --
// whose legs carry no ForgetChanged$ -- never emits for its own moved cards.
func TestChangeZoneForgetChangedDropsTheMovedCard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Troop of Ponies")
	troop := searchMoveByName(t, e, "Troop of Ponies", state.ZBattlefield)
	// Summoning sickness (CR 302.6): the tap cost cannot be paid the turn
	// Troop entered, so drive to seat 0's next main phase (turn 3) first.
	driveToStep(t, e, 3, 0, state.StepMain1)
	addMana(t, e, 0, "CC")
	idx := changeZoneAbilityIndexBy(t, e, troop, "Library")
	submitChoices(t, e, abilityOption(t, e, troop, idx).Index)
	// The CARDNAME sacrifice cost is a real KChoose over one option.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.ResumeKind != "search" {
		if len(d.Options) != 1 || d.Options[0].Obj != troop {
			t.Fatalf("unexpected activation cost choice: %+v", d)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("head search pending = %+v, want a search KChoose", d)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	// Leg 1 (battlefield, NoLooking$ + ForgetChanged$): the head Reveal$ed
	// these finds, so the leg is named (task fb-20260922T235033Z); picking one
	// then forgets it.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("leg1 pending = %+v, want a search KChoose", d)
	}
	for _, o := range d.Options {
		if o.Label == "a card" {
			t.Fatalf("NoLooking$ leg is blind though the head revealed: %+v", o)
		}
		obj := e.G.Obj(o.Obj)
		if obj == nil || obj.Face() == nil || obj.Face().Name == "" {
			t.Fatalf("NoLooking$ leg option %d object %d has no named printed face", o.Index, o.Obj)
		}
		if o.Label != obj.Face().Name {
			t.Fatalf("NoLooking$ leg label %q != printed name %q", o.Label, obj.Face().Name)
		}
	}
	submitChoices(t, e, d.Options[0].Index)
	// Leg 2 runs directly off the post-forget Remembered set: no ask.
	passUntilStackEmpty(t, e, 30)
	forgets := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Counter == "forget-remembered" && ev.Obj == troop {
			forgets++
		}
	}
	if forgets != 2 {
		t.Fatalf("%d forget-remembered events on the source, want 2 (both legs)", forgets)
	}
	// The two basics settled one battlefield (tapped) and one hand.
	battlefield, hand := 0, 0
	for _, id := range troopPicks(e, troop) {
		o := e.G.Obj(id)
		switch o.Zone {
		case state.ZBattlefield:
			battlefield++
		case state.ZHand:
			hand++
		}
	}
	if battlefield != 1 || hand != 1 {
		t.Fatalf("Troop's picks settled battlefield=%d hand=%d, want 1/1", battlefield, hand)
	}
	replayCheck(t, e, cfg)
}

// troopPicks collects the two basic-land ids the legs moved (battlefield
// entry or hand), identified by their MoveZone events out of the library --
// the head's own Library→Library "found" moves carry To=Library and are
// excluded.
func troopPicks(e *Engine, troop state.ObjID) []state.ObjID {
	var out []state.ObjID
	for _, ev := range e.L.Events {
		if ev.Kind != events.MoveZone || ev.From != state.ZLibrary || ev.To == state.ZLibrary || ev.Obj == troop {
			continue
		}
		out = append(out, ev.Obj)
	}
	return out
}

// TestChangeZoneDifferentNamesRestrictsToOnePerName is the DifferentNames$
// concern: Realms Uncharted's search options carry one Group per card name,
// Validate refuses a same-name pair, and a distinct-name answer drives the
// whole chain (opponent-chosen graveyard half, hand half).
func TestChangeZoneDifferentNamesRestrictsToOnePerName(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Realms Uncharted")
	_, d := castSearchSpell(t, e, "Realms Uncharted")
	if d.Kind != decision.KChoose || d.Min != 0 || d.Max != 4 {
		t.Fatalf("head search = %v %d..%d, want KChoose 0..4", d.Kind, d.Min, d.Max)
	}
	if len(d.Options) < 4 {
		t.Fatalf("head search has %d options, need 4 lands", len(d.Options))
	}
	for _, o := range d.Options {
		if o.Group == "" {
			t.Fatalf("DifferentNames$ option carries no Group: %+v", o)
		}
		if o.Group != o.Label {
			t.Fatalf("option Group %q does not name the card (%q)", o.Group, o.Label)
		}
	}
	// Two same-named lands must exist among 16 basics for at least one name;
	// Validate refuses to select both.
	dup := -1
	for _, o := range d.Options {
		if o.Label == d.Options[0].Label && o.Index != d.Options[0].Index {
			dup = o.Index
		}
	}
	if dup < 0 {
		t.Fatalf("fixture offered no duplicate %q option", d.Options[0].Label)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{d.Options[0].Index, dup}}); err == nil {
		t.Fatal("Validate accepted two same-named picks")
	}
	var forest, mountain *decision.Option
	for i := range d.Options {
		switch d.Options[i].Label {
		case "Forest":
			forest = &d.Options[i]
		case "Mountain":
			mountain = &d.Options[i]
		}
	}
	if forest == nil || mountain == nil {
		t.Fatalf("fixture lacks the two named basics: %+v", d.Options)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{forest.Index, mountain.Index}}); err != nil {
		t.Fatalf("distinct-name answer rejected: %v", err)
	}
	submitChoices(t, e, forest.Index, mountain.Index)
	// Leg 1: the OPPONENT chooses which two go to the graveyard. The head
	// Reveal$ed the finds, so the opponent's NoLooking$ leg is named too
	// (task fb-20260922T235033Z).
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 1 {
		t.Fatalf("leg1 pending = %+v, want the opponent's search ask", d)
	}
	if len(d.Options) < 2 {
		t.Fatalf("opponent pick has %d options, need the two found lands", len(d.Options))
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	// Leg 2: the rest reach the hand -- both were chosen for the graveyard,
	// so the remaining eligible set is empty and no ask is posed.
	passUntilStackEmpty(t, e, 30)
	for _, id := range []state.ObjID{forest.Obj, mountain.Obj} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("Realms card %d = %+v, want in the graveyard", id, e.G.Obj(id))
		}
	}
	replayCheck(t, e, cfg)
}
