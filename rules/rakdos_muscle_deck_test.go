package rules

// The Rakdos, the Muscle deck import's parameter-read regressions. Every
// test here pins one parameter the param census (rules/paramcensus_test.go)
// newly reads for the deck's cards, on the real corpus card where the deck
// plays it, so the read cannot rot back into an unread key.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// battlefieldFixture moves a fixture card straight onto p's battlefield with
// a logged move.
func battlefieldFixture(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), p)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	return o.ID
}

// handFixture moves a fixture card straight into p's hand with a logged move
// (battlefieldFixture's hand sibling — a spell the test will cast).
func handFixture(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), p)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZHand})
	return o.ID
}

// drainUntilAsk drives the engine until a non-priority decision is pending
// (a resolution's ask), draining priority passes and nil windows as it goes.
// It returns that decision, or nil if the game reached a quiet priority state
// within maxSteps — the shape the no-trigger assertions below rely on.
func drainUntilAsk(t *testing.T, e *Engine, maxSteps int) *decision.Decision {
	t.Helper()
	for i := 0; i < maxSteps; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
			if d == nil {
				return nil
			}
		}
		if d.Kind != decision.KPriority {
			return d
		}
		for _, o := range d.Options {
			if o.Kind == "pass" {
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
					t.Fatalf("submit pass: %v", err)
				}
				break
			}
		}
	}
	return e.Pending()
}

// optionOf returns the priority option named by kind (and object, when set).
func optionOf(t *testing.T, e *Engine, kind string, obj state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatalf("no pending decision while looking for a %q option", kind)
	}
	for _, o := range d.Options {
		if o.Kind != kind {
			continue
		}
		if obj != 0 && o.Obj != obj {
			continue
		}
		return o.Index
	}
	return -1
}

// TestRakdosMuscleSacTriggerExilesAndMayPlaysWithAnyTypeMana pins the whole
// commander mechanic: sacrificing another creature exiles cards equal to its
// mana value from the top of a library and grants "you may play those cards,
// mana of any type" — the MayPlayIgnoreType$ rider is what lets a
// colourless-only pool pay the {R} pip of the exiled bolt.
func TestRakdosMuscleSacTriggerExilesAndMayPlaysWithAnyTypeMana(t *testing.T) {
	t.Parallel()
	bolt := card(t, "Name:Lightning Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n")
	e := handEngine(t, bolt)
	rakdos := e.G.AddObject(choiceCorpusCard(t, "Rakdos, the Muscle"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: rakdos.ID, From: state.ZLibrary, To: state.ZBattlefield})
	battlefieldFixture(t, e, 0, "Name:Bear\nManaCost:1 R\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// The bolt is the top card of seat 0's library, so the dig takes it.
	hand := e.G.Zone(state.ZHand, 0)
	e.G.SetZone(state.ZHand, 0, hand[1:])
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{hand[0]}, e.G.Zone(state.ZLibrary, 0)...))
	e.pending = nil

	// Activate Rakdos's pump ability: its Sac<1/Creature.Other> cost is what
	// fires the trigger.
	addMana(t, e, 0, "")
	e.Advance()
	act := optionOf(t, e, "ability", rakdos.ID)
	if act < 0 {
		t.Fatalf("Rakdos's pump ability not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, act)
	sac := e.Pending()
	if sac == nil || sac.Kind != decision.KChoose {
		t.Fatalf("sacrifice cost ask missing: %+v", sac)
	}
	submitChoices(t, e, sac.Options[0].Index)
	// The trigger asks its target player.
	tgt := e.Pending()
	if tgt == nil || tgt.Kind != decision.KTarget {
		t.Fatalf("dig target ask missing: %+v", tgt)
	}
	submitChoices(t, e, tgt.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	if z := e.G.Obj(hand[0]).Zone; z != state.ZExile {
		t.Fatalf("bolt zone = %s, want exile (the dig moved it)", z)
	}

	// The may-play grant is live: a colourless-only pool must still offer the
	// bolt's cast (MayPlayIgnoreType$).
	addMana(t, e, 0, "C")
	cast := optionOf(t, e, "cast", hand[0])
	if cast < 0 {
		t.Fatalf("may-play cast not offered on a colourless-only pool: %+v", e.Pending().Options)
	}
	submitChoices(t, e, cast)
	for e.Pending() != nil && e.Pending().Kind == decision.KPriority {
		passUntilStackEmpty(t, e, 1)
	}
	td := e.Pending()
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("bolt target ask missing: %+v", td)
	}
	submitChoices(t, e, td.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	if e.G.Obj(hand[0]).Zone == state.ZExile {
		t.Fatalf("bolt still in exile — the may-play cast never resolved")
	}
	// The ForgetOnMoved$ Exile sweep: the played card left exile, so the
	// grant no longer remembers it; the OTHER dug card (still in exile, still
	// playable until the grant's end step) stays remembered.
	for _, ce := range e.continuous {
		if ce.MayPlay {
			for _, id := range ce.Remembered {
				if id == hand[0] {
					t.Fatalf("may-play grant still remembers the played card: %+v", ce.Remembered)
				}
			}
		}
	}
}

// TestCharmingScoundrelWickedRoleAttaches pins Token.AttachedTo$: the created
// Role token enters attached to the targeted creature.
func TestCharmingScoundrelWickedRoleAttaches(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{choiceCorpusCard(t, "Charming Scoundrel")}, nil)
	bear := battlefieldFixture(t, e, 0, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	moveByName(t, e, 0, "Charming Scoundrel", state.ZHand)
	for _, s := range []string{"R", "C"} {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: s, Amount: 1})
	}
	e.priorityRound()
	castNamed(t, e, "Charming Scoundrel")
	passToKind(t, e, decision.KModes)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("Charming Scoundrel's modes not asked: %+v", d)
	}
	modeIdx := -1
	for _, o := range d.Options {
		if o.Label == "Create a Wicked Role token attached to target creature you control." {
			modeIdx = o.Index
		}
	}
	if modeIdx < 0 {
		t.Fatalf("token mode not offered: %+v", d.Options)
	}
	submitChoices(t, e, modeIdx)
	// The chosen mode's target ask pops when the trigger resolves.
	td := drainUntilAsk(t, e, 30)
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("role target ask missing: %+v", td)
	}
	bearIdx := -1
	for _, o := range td.Options {
		if o.Obj == bear {
			bearIdx = o.Index
		}
	}
	if bearIdx < 0 {
		t.Fatalf("the bear is not a legal role target: %+v", td.Options)
	}
	submitChoices(t, e, bearIdx)
	passUntilStackEmpty(t, e, 30)
	var token state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o.IsToken && o.AttachedTo == bear {
			token = id
		}
	}
	if token == 0 {
		t.Fatalf("no Wicked Role token attached to the bear")
	}
	if e.Power(bear) != 3 || e.Toughness(bear) != 3 {
		t.Fatalf("enchanted bear = %d/%d, want 3/3", e.Power(bear), e.Toughness(bear))
	}
}

// TestNameStickerGoblinExcludedOrigins pins trig:ChangesZone.ExcludedOrigins$
// (and the IsPresent$ AND IsPresent2$ clause pair): the die roll fires on a battlefield entry
// from hand, never on one from the graveyard or exile.
func TestNameStickerGoblinExcludedOrigins(t *testing.T) {
	t.Parallel()
	// The die roll fires only for the hand entry, once the trigger resolves:
	// exactly one roll note and a mana event in the log (the pool itself may
	// already have been cleared by a later step the drain crossed).
	gob := choiceCorpusCard(t, "\"Name Sticker\" Goblin")
	for _, from := range []state.Zone{state.ZHand, state.ZGraveyard, state.ZExile} {
		e := handEngine(t)
		o := e.G.AddObject(gob, 0)
		o.Zone = from // the zone list and the object's own zone must agree, or the move leaves it in both lists
		e.G.SetZone(from, 0, []state.ObjID{o.ID})
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: from, To: state.ZBattlefield})
		e.Advance()
		d := drainUntilAsk(t, e, 30)
		_ = d
		rolls, mana := 0, 0
		for _, ev := range e.L.Events {
			if ev.Kind == events.Note && strings.Contains(ev.Text, "rolls a d") {
				rolls++
			}
			if ev.Kind == events.ManaAdd && ev.Player == 0 && ev.Amount > 0 {
				mana++
			}
		}
		if from == state.ZHand {
			if rolls != 1 || mana == 0 {
				t.Fatalf("hand entry: rolls=%d mana events=%d, want one roll and mana", rolls, mana)
			}
			continue
		}
		if rolls != 0 || mana != 0 {
			t.Fatalf("entry from %s fired the trigger anyway: rolls=%d mana=%d", from, rolls, mana)
		}
	}
}

// TestWishclawActivationOnlyOnYourTurn pins ChangeZone.PlayerTurn$ on an
// activated ability: the option exists on the controller's turn, never on an
// opponent's.
func TestWishclawActivationOnlyOnYourTurn(t *testing.T) {
	t.Parallel()
	run := func(active state.PlayerID) bool {
		e := handEngine(t)
		wish := e.G.AddObject(choiceCorpusCard(t, "Wishclaw Talisman"), 0)
		e.emit(events.Event{Kind: events.MoveZone, Obj: wish.ID, From: state.ZLibrary, To: state.ZBattlefield})
		e.emit(events.Event{Kind: events.CounterChange, Obj: wish.ID, Counter: "WISH", Amount: 3})
		e.G.Active, e.G.Priority = active, 0
		e.pending = nil
		addMana(t, e, 0, "1")
		return optionOf(t, e, "ability", wish.ID) >= 0
	}
	if !run(0) {
		t.Fatal("Wishclaw's search must be offered on its controller's turn")
	}
	if run(1) {
		t.Fatal("Wishclaw's search must not be offered on the opponent's turn")
	}
}

// TestManaVaultDrawStepDamageScopedToSelf pins trig:Phase.PresentDefined$:
// the "if this artifact is tapped" condition counts the vault itself, not
// every tapped permanent.
func TestManaVaultDrawStepDamageScopedToSelf(t *testing.T) {
	t.Parallel()
	run := func(vaultTapped bool) int32 {
		e := handEngine(t)
		vault := e.G.AddObject(choiceCorpusCard(t, "Mana Vault"), 0)
		e.emit(events.Event{Kind: events.MoveZone, Obj: vault.ID, From: state.ZLibrary, To: state.ZBattlefield})
		bear := battlefieldFixture(t, e, 0, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		e.emit(events.Event{Kind: events.Tap, Obj: bear, Player: 0, Text: "test setup"})
		if vaultTapped {
			e.emit(events.Event{Kind: events.Tap, Obj: vault.ID, Player: 0, Text: "test setup"})
		}
		life := e.G.Players[0].Life
		// Turn 1's draw step is already past (handEngine starts at main1), so
		// the damage ask is turn 2's. The drive crosses the vault's own upkeep
		// offer ("you may pay {4}") — decline it — and the untap step, whose
		// CantHappen replacement keeps a tapped vault tapped.
		driveToDrawStep2(t, e)
		if vaultTapped && !e.G.Obj(vault.ID).Tapped {
			t.Fatal("the tapped vault untapped at the draw step (CantHappen unread)")
		}
		// The draw-step trigger resolves once the drive's outstanding priority
		// passes drain.
		d := e.Pending()
		for i := 0; i < 20 && d != nil && d.Kind == decision.KPriority; i++ {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					_ = e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}})
				}
			}
			d = e.Pending()
			if d == nil {
				e.Advance()
				d = e.Pending()
			}
		}
		return life - e.G.Players[0].Life
	}
	if got := run(true); got != 1 {
		t.Fatalf("tapped vault took %d damage in its draw step, want 1", got)
	}
	if got := run(false); got != 0 {
		t.Fatalf("untapped vault took %d damage (some other tapped permanent leaked into PresentDefined), want 0", got)
	}
}

// driveToDrawStep2 drives from wherever the test is to seat 0's turn-2 draw
// step, answering only what the drive itself crosses: priority passes and
// Mana Vault's upkeep "you may pay {4}" offer (declined).
func driveToDrawStep2(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		if e.G.Active == 0 && e.G.Step == state.StepDraw && e.G.Turn >= 2 {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended during the drive (turn %d)", e.G.Turn)
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority with no pass: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit: %v", err)
			}
		case decision.KTriggerOptional:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "no" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("optional ask with no decline: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit: %v", err)
			}
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		default:
			t.Fatalf("unexpected %+v while driving to the draw step", d)
		}
	}
	t.Fatal("did not reach the draw step")
}

// TestMasterOfDarkRitesRestrictsSpend pins Mana.RestrictValid$: the
// sac-fuelled {B}{B}{B} is spendable only on Vampire/Cleric/Demon spells.
func TestMasterOfDarkRitesRestrictsSpend(t *testing.T) {
	t.Parallel()
	cleric := card(t, "Name:Cleric Charm\nManaCost:B\nTypes:Instant Cleric\nOracle:x\n")
	plain := card(t, "Name:Plain Charm\nManaCost:B\nTypes:Instant\nOracle:x\n")
	e := handEngine(t, cleric, plain)
	rites := e.G.AddObject(choiceCorpusCard(t, "Master of Dark Rites"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: rites.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.G.Obj(rites.ID).SummonSick = false
	bear := battlefieldFixture(t, e, 0, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.Advance()
	act := optionOf(t, e, "activate", rites.ID)
	if act < 0 {
		t.Fatalf("Master of Dark Rites's mana ability not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, act)
	// The Sac<1/Creature.Other> cost has exactly one eligible permanent (the
	// bear — Rakdos himself is excluded by Other), so the sacrifice ask is
	// never posed: eligible == Amount$ takes the whole set silently (the
	// effDiscard strict-supersets rule the sacrifice path shares). The mana
	// ability resolves immediately; the restricted pool is live NOW, before
	// any priority pass can step the turn and clear it.
	if sac := e.Pending(); sac != nil && sac.Kind == decision.KChoose {
		submitChoices(t, e, sac.Options[0].Index)
	}
	if e.G.Obj(bear).Zone == state.ZBattlefield {
		t.Fatalf("bear not sacrificed")
	}
	pool := e.G.Players[0].Pool
	if pool[state.MB] != 3 {
		t.Fatalf("pool after the activation = %+v, want three {B}", pool)
	}
	// The restricted {B}{B}{B} pays the Cleric spell, never the plain one.
	if pay.CostPayable(asPayer(e), 0, e.G.Zone(state.ZHand, 0)[0], false, ParseCost("B")) != true {
		t.Fatal("the Cleric spell must be payable with the restricted mana")
	}
	if pay.CostPayable(asPayer(e), 0, e.G.Zone(state.ZHand, 0)[1], false, ParseCost("B")) {
		t.Fatal("the plain spell must not be payable with the restricted mana")
	}
}

// TestOppositionAgentExilesOpponentSearchFinds pins the whole agent pair:
// the search pick's decision is redirected to the agent's controller
// (ControlOpponentsSearchingLibrary$) and the found card is exiled instead
// of taken (repl:Moved FoundSearchingLibrary$), with the may-play grant live.
func TestOppositionAgentExilesOpponentSearchFinds(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	agent := e.G.AddObject(choiceCorpusCard(t, "Opposition Agent"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: agent.ID, From: state.ZLibrary, To: state.ZBattlefield})
	// Seat 1 casts a tutor. Its library is mountains: the found card is a
	// basic Mountain. The spell must sit in seat 1's HAND to be castable.
	tutor := handFixture(t, e, 1, "Name:Dark Tutor\nManaCost:B\nTypes:Instant\nA:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Card | ChangeNum$ 1\nOracle:x\n")
	addMana(t, e, 1, "B")
	// Seat 0 (the active player) has nothing to do: pass to seat 1.
	if p := e.Pending(); p != nil && p.Player == 0 && p.Kind == decision.KPriority {
		submitChoicePass(t, e)
	}
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 1 {
		t.Fatalf("seat 1's priority with the tutor missing: %+v", d)
	}
	castIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == tutor {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("tutor cast not offered: %+v", d.Options)
	}
	submitChoices(t, e, castIdx)
	// The tutor names no ValidTgts$: it searches its controller's own library,
	// so the flow's next ask is the search pick itself — which the agent's
	// ControlOpponentsSearchingLibrary$ static redirects to seat 0. It pops
	// when the spell resolves, after both players pass.
	d = drainUntilAsk(t, e, 30)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("search pick ask missing: %+v", d)
	}
	if d.Player != 0 {
		t.Fatalf("search pick posed to seat %d, want the agent's controller (0)", d.Player)
	}
	found := d.Options[0].Obj
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	if z := e.G.Obj(found).Zone; z != state.ZExile {
		t.Fatalf("found card zone = %s, want exile (the agent's replacement)", z)
	}
	// The agent's controller may play it from exile: a basic Mountain comes
	// through the may-play land walk.
	addMana(t, e, 0, "")
	if optionOf(t, e, "play_land", found) < 0 {
		t.Fatalf("the exiled Mountain is not playable through the agent's grant: %+v", e.Pending().Options)
	}
}

// TestFlamekinDeclinedSearchDoesNotShuffle pins ChangeZone.ShuffleNonMandatory$:
// the DECIDED-Against search (the optional trigger declined) shuffles
// nothing — the search never ran — while the ACCEPTED search shuffles even
// when its pick found nothing (CR 701.23b's fail-to-find still shuffles,
// which is what Squadron Hawk's committed pin holds too).
func TestFlamekinDeclinedSearchDoesNotShuffle(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	flame := e.G.AddObject(choiceCorpusCard(t, "Flamekin Harbinger"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: flame.ID, From: state.ZHand, To: state.ZBattlefield})
	// The search needs an eligible Elemental on top, or the zero-eligible
	// search completes silently (the empty-answer tripwire) and never asks.
	ember := e.G.AddObject(card(t, "Name:Ember\nManaCost:R\nTypes:Creature Elemental\nPT:1/1\nOracle:x\n"), 0)
	ember.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{ember.ID}, e.G.Zone(state.ZLibrary, 0)...))
	e.Advance()
	d := drainUntilAsk(t, e, 30)
	// Flamekin's search is OPTIONAL ("you may search"): the optional-trigger
	// ask comes first; accepting it opens the search pick.
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("Flamekin's optional-trigger ask missing: %+v", d)
	}
	yesIdx := -1
	for _, o := range d.Options {
		if o.Kind == "yes" {
			yesIdx = o.Index
		}
	}
	submitChoices(t, e, yesIdx)
	d = drainUntilAsk(t, e, 30)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("Flamekin's optional search not asked: %+v", d)
	}
	before := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	shuffleCount := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffleCount++
		}
	}
	submitChoices(t, e) // the empty answer: find none (CR 701.23b)
	// searchmay1: the fail-to-find shape now poses the ShuffleNonMandatory$
	// may-shuffle confirm. Accept it, so the pinned "the fail-to-find still
	// shuffles" count below holds.
	d = drainUntilAsk(t, e, 30)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search_mayshuffle" {
		t.Fatalf("fail-to-find did not pose the may-shuffle confirm: %+v", d)
	}
	yes, _ := mayShuffleConfirm(t, e, 0)
	submitChoices(t, e, yes)
	if after := e.G.Zone(state.ZLibrary, 0); len(after) != len(before) {
		t.Fatalf("library size changed on a fail-to-find search")
	}
	now := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			now++
		}
	}
	if now != shuffleCount+1 {
		t.Fatalf("fail-to-find shuffles = %d, want the search's one shuffle (had %d before)", now-shuffleCount, shuffleCount)
	}
	// The Ember stays in the library (nothing was taken); its position may
	// have moved with the shuffle.
	found := false
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if id == ember.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the fail-to-find search moved the eligible card anyway")
	}
}

// TestFlareOfDuplicationCopiesItsTarget pins the unset-Defined$ CopySpellAbility
// default: a targeted copy copies its TARGET, never the resolving spell
// itself. The old default made Flare (ValidTgts$ Instant,Sorcery, no
// Defined$) copy itself — its copy again a Flare whose copy was again a
// Flare — an unbounded self-copy loop no bot game could finish (measured:
// seed 1 of the deck-mirror mtgsim run never terminated).
func TestFlareOfDuplicationCopiesItsTarget(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{choiceCorpusCard(t, "Flare of Duplication")}, nil)
	moveByName(t, e, 0, "Flare of Duplication", state.ZHand)
	bolt := card(t, "Name:Lightning Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n")
	bo := e.G.AddObject(bolt, 0)
	bo.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), bo.ID))
	for _, s := range []string{"R", "R", "R", "R"} {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: s, Amount: 1})
	}
	e.priorityRound()
	castNamed(t, e, "Lightning Bolt")
	// The Bolt's own target ask: answer it, keeping the Bolt on the stack.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Bolt target ask missing: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			submitChoices(t, e, o.Index)
			break
		}
	}
	// Hold priority: cast the Flare before the Bolt resolves.
	d = e.Pending()
	flareIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Label == "Cast Flare of Duplication" {
			flareIdx = o.Index
		}
	}
	if flareIdx < 0 {
		t.Fatalf("Flare not offered: %+v", d.Options)
	}
	submitChoices(t, e, flareIdx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Flare target ask missing: %+v", d)
	}
	boltIdx := -1
	for _, o := range d.Options {
		if o.Obj == bo.ID {
			boltIdx = o.Index
		}
	}
	if boltIdx < 0 {
		t.Fatalf("the Bolt is not a Flare target: %+v", d.Options)
	}
	submitChoices(t, e, boltIdx)
	passUntilStackEmpty(t, e, 30)
	copies := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.StackCopy {
			copies++
		}
	}
	if copies != 1 {
		t.Fatalf("%d StackCopy events, want exactly one (the Bolt copy; a second would be Flare copying itself)", copies)
	}
}
