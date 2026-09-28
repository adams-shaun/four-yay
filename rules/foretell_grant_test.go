package rules

// The ft1 closure (AGENTS.md row "(ft1) `withForetell`/`withoutForetell`
// remain unread ..."): the foretell predicates are readable in the ordinary
// filter grammar, Cosmos Charger's any-turn timing grant is live, and the
// Effect-delivered free-cast MayPlay static is registered and consumed.
// Pinned on the real corpus carriers: Dream Devourer (the withoutForetell
// grant), Niko Defies Destiny (the withForetell target spec), Cosmos Charger
// (the any-turn player keyword and the Static.Foretelling ReduceCost), and
// Dauthi Voidwalker (the Effect-delivered MayPlayWithoutManaCost$ grant).

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// grantedForetellBear is a plain creature card: no printed foretell, the
// exact shape Dream Devourer's grant is for.
const grantedForetellBear = "Name:Grizzly Fast\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// anyTurnSeer carries the printed K:Foretell line the {2} action needs.
const anyTurnSeer = "Name:Seer of the Divide\nManaCost:2 U\nTypes:Creature\nK:Foretell:2 U\nPT:1/1\nOracle:x\n"

// foretellKeywordCount counts the Foretell-headed entries of a keyword list.
func foretellKeywordCount(kws []string) int {
	n := 0
	for _, k := range kws {
		if strings.EqualFold(cards.KeywordHead(k), "Foretell") {
			n++
		}
	}
	return n
}

// dreamDevourerOut puts seat 0's corpus Dream Devourer on the battlefield.
func dreamDevourerOut(t *testing.T, e *Engine) {
	t.Helper()
	dd := e.G.AddObject(corpusAlternativeCard(t, "Dream Devourer"), 0)
	if d := dd.Card.Link(); len(d) != 0 {
		t.Fatalf("link Dream Devourer: %v", d)
	}
	dd.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), dd.ID))
	e.emit(events.Event{Kind: events.ClockTick})
}

// driveToLaterTurnMain drives to main1 of turn 3 with seat 0 active,
// answering the combat asks Dream Devourer (a creature on the battlefield)
// poses on the way with empty declarations -- a real, expected ask.
func driveToLaterTurnMain(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn >= 3 && e.G.Active == 0 && e.G.Step == state.StepMain1 {
			return
		}
		if answerIfDiscard(t, e) {
			continue
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
				t.Fatalf("priority with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		case decision.KAttackers, decision.KBlockers:
			// An empty declaration: no attackers, no blockers.
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player}); err != nil {
				t.Fatalf("submit empty combat declaration: %v", err)
			}
		default:
			t.Fatalf("unexpected ask %+v while driving to turn 3", d)
		}
	}
	t.Fatal("did not reach turn 3 seat 0 main1")
}

func TestWithoutForetellGrantOffersForetellActionAndCheapCast(t *testing.T) {
	e := handEngine(t, card(t, grantedForetellBear))
	dreamDevourerOut(t, e)
	id := e.G.Zone(state.ZHand, 0)[0]
	o := e.G.Obj(id)
	// Preconditions the grant's correctness rests on: the bear is in seat
	// 0's hand and has NO printed foretell of its own.
	if o == nil || o.Zone != state.ZHand || o.Owner != 0 {
		t.Fatalf("bear not in seat 0's hand: %+v", o)
	}
	if _, ok := o.Face().KeywordParam("Foretell"); ok {
		t.Fatal("precondition: bear already carries a printed Foretell")
	}
	if got := foretellKeywordCount(e.Derived(id).Keywords); got != 1 {
		t.Fatalf("Dream Devourer's withoutForetell grant did not reach the hand card: %d Foretell keywords in %v",
			got, e.Derived(id).Keywords)
	}
	// CR 702.126a: the action pays {2} from the pool.
	e.G.Players[0].Pool[state.MC] = 2
	if opt := optionByMode(t, e.legalActions(0), "foretell"); opt.Obj != id {
		t.Fatalf("foretell option for the wrong object: %+v", opt)
	}
	foretellIt(t, e, id)
	if got := e.G.Players[0].Pool[state.MC]; got != 0 {
		t.Fatalf("foretell action left %d generic in the pool, want 0", got)
	}
	// The granted foretell cost: mana cost {1}{G} less {2} generic = {G}.
	// Funded with exactly one green, the later turn's cast is offered,
	// resolves, and puts the bear on the battlefield.
	driveToLaterTurnMain(t, e)
	e.G.Players[0].Pool[state.MC] = 0
	e.G.Players[0].Pool[state.MG] = 1
	submitOption(t, e, "foretell_cast", "Cast Grizzly Fast (foretold)")
	finishCast(t, e, id)
	if got := e.G.Obj(id); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("foretold cast did not put the bear on the battlefield: %+v", got)
	}
}

func TestForetellGrantExcludesPrintedForetellAndOpponentsCards(t *testing.T) {
	// Haunting Voyage in seat 0's hand: printed K:Foretell:5 B B, beside a
	// plain bear -- the positive half of the exclusion (the bear IS granted)
	// is what makes the negatives below able to fail.
	e := handEngine(t, corpusAlternativeCard(t, "Haunting Voyage"), card(t, grantedForetellBear))
	dreamDevourerOut(t, e)
	ids := e.G.Zone(state.ZHand, 0)
	voyID, bearID := ids[0], ids[1]
	if _, ok := e.G.Obj(voyID).Face().KeywordParam("Foretell"); !ok {
		t.Fatal("precondition: Haunting Voyage lost its printed Foretell")
	}
	if _, ok := e.G.Obj(bearID).Face().KeywordParam("Foretell"); ok {
		t.Fatal("precondition: bear already carries a printed Foretell")
	}
	if got := foretellKeywordCount(e.Derived(bearID).Keywords); got != 1 {
		t.Fatalf("the plain hand card was not granted foretell: %d Foretell keywords in %v",
			got, e.Derived(bearID).Keywords)
	}
	// withoutForetell must exclude the printed-foretell card: exactly the
	// printed entry, never a second granted one.
	if got := foretellKeywordCount(e.Derived(voyID).Keywords); got != 1 {
		t.Fatalf("printed-foretell card got a duplicate grant: %d Foretell keywords in %v",
			got, e.Derived(voyID).Keywords)
	}
	// YouOwn excludes seat 1's hand card: seat 1 foretells nothing.
	other := e.G.AddObject(card(t, grantedForetellBear), 1)
	other.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{other.ID})
	if got := foretellKeywordCount(e.Derived(other.ID).Keywords); got != 0 {
		t.Fatalf("opponent's hand card was granted foretell: %v", e.Derived(other.ID).Keywords)
	}
}

func TestWithForetellDrivesGraveyardTargetSpec(t *testing.T) {
	e := handEngine(t, card(t, grantedForetellBear))
	niko := corpusAlternativeCard(t, "Niko Defies Destiny")
	src := e.G.AddObject(niko, 0)
	if d := src.Card.Link(); len(d) != 0 {
		t.Fatalf("link Niko Defies Destiny: %v", d)
	}
	src.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), src.ID))
	e.emit(events.Event{Kind: events.ClockTick})
	// The real corpus line: Niko's chapter III ChangeZone SVar names the
	// ValidTgts$ spec the target walk evaluates.
	sa := cards.ResolveSVar(niko.Faces[0].SVars, "DBChangeZone")
	if sa == nil {
		t.Fatal("Niko Defies Destiny lost its DBChangeZone SVar")
	}
	spec, ok := sa.Params["ValidTgts"]
	if !ok || !strings.Contains(spec, "withForetell") {
		t.Fatalf("precondition: ValidTgts$ = %q, want a withForetell spec", spec)
	}
	// A foretell card and a plain card in seat 0's graveyard; the target
	// walk must offer only the one with foretell.
	foretold := seedGraveCard(t, e, 0, "Name:Foretold Bloom\nManaCost:2 U\nTypes:Instant\nK:Foretell:2 U\nOracle:x\n")
	plain := seedGraveCard(t, e, 0, "Name:Plain Bloom\nManaCost:2 U\nTypes:Instant\nOracle:x\n")
	if !e.matchesSpecFrom(spec, foretold, 0, src.ID) {
		t.Fatal("withForetell target spec did not match a foretell card in the graveyard")
	}
	if e.matchesSpecFrom(spec, plain, 0, src.ID) {
		t.Fatal("withForetell target spec matched a card with no foretell")
	}
	// YouOwn excludes an opponent-owned foretell card.
	opp := seedGraveCard(t, e, 1, "Name:Opp Bloom\nManaCost:2 U\nTypes:Instant\nK:Foretell:2 U\nOracle:x\n")
	if e.matchesSpecFrom(spec, opp, 0, src.ID) {
		t.Fatal("withForetell+YouOwn target spec matched an opponent's card")
	}
}

// seedGraveCard creates a card object and places it in p's graveyard (a
// fixture placement, the same shape mayplay_test uses).
func seedGraveCard(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), p)
	o.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, p, append(e.G.Zone(state.ZGraveyard, p), o.ID))
	return o.ID
}

func TestCosmosChargerWidensForetellToAnyPlayersTurn(t *testing.T) {
	e := handEngine(t, card(t, anyTurnSeer))
	id := e.G.Zone(state.ZHand, 0)[0]
	if _, ok := e.G.Obj(id).Face().KeywordParam("Foretell"); !ok {
		t.Fatal("precondition: hand card lost its printed Foretell")
	}
	e.G.Players[0].Pool[state.MC] = 2
	// Baseline CR 702.126a: during the OPPONENT's turn, seat 0 foretells
	// nothing.
	e.G.Active = 1
	if optionByLabel(e.legalActions(0), "Foretell Seer of the Divide") >= 0 {
		t.Fatal("precondition: foretell offered off-turn with no any-turn grant")
	}
	// Cosmos Charger on the battlefield: the Affected$ You player keyword
	// widens the window to any player's turn, and the ReduceCost$ {1} cuts
	// the {2} action to {1} -- the offer needs only one generic mana.
	cc := e.G.AddObject(corpusAlternativeCard(t, "Cosmos Charger"), 0)
	if d := cc.Card.Link(); len(d) != 0 {
		t.Fatalf("link Cosmos Charger: %v", d)
	}
	cc.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), cc.ID))
	e.emit(events.Event{Kind: events.ClockTick})
	e.G.Players[0].Pool[state.MC] = 1
	if opt := optionByMode(t, e.legalActions(0), "foretell"); opt.Obj != id {
		t.Fatalf("off-turn foretell not offered under the any-turn grant (pool {1}): %+v", opt)
	}
	// Unfunded {1}: no offer -- the action still has to be payable.
	e.G.Players[0].Pool[state.MC] = 0
	if optionByLabel(e.legalActions(0), "Foretell Seer of the Divide") >= 0 {
		t.Fatal("foretell offered with an unpayable reduced {1}")
	}
	// Dream Devourer's PLAIN AddKeyword$ Foretell grant (no any-turn rider)
	// must not widen the timing: on the holder's own turn the granted action
	// is offered; on the opponent's turn it is not.
	e2 := handEngine(t, card(t, anyTurnSeer))
	dreamDevourerOut(t, e2)
	gid := e2.G.Zone(state.ZHand, 0)[0]
	e2.G.Players[0].Pool[state.MC] = 2
	e2.G.Active = 1
	if optionByLabel(e2.legalActions(0), "Foretell Seer of the Divide") >= 0 {
		t.Fatal("plain foretell grant widened the timing to the opponent's turn")
	}
	e2.G.Active = 0
	if opt := optionByMode(t, e2.legalActions(0), "foretell"); opt.Obj != gid {
		t.Fatalf("granted foretell action missing on the holder's own turn: %+v", opt)
	}
}

func TestEffectDeliveredFreeCastMayPlay(t *testing.T) {
	e := handEngine(t, card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"))
	dauthi := e.G.AddObject(corpusAlternativeCard(t, "Dauthi Voidwalker"), 0)
	if d := dauthi.Card.Link(); len(d) != 0 {
		t.Fatalf("link Dauthi Voidwalker: %v", d)
	}
	dauthi.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), dauthi.ID))
	e.emit(events.Event{Kind: events.ClockTick})
	// The chosen card: an opponent-owned creature in exile with a void
	// counter (Dauthi's own replacement would put it there; the fixture
	// places it directly -- the grant under test starts at the ability).
	victim := e.G.AddObject(card(t, "Name:Riot Sprite\nManaCost:1 G\nTypes:Creature Faerie\nPT:1/1\nOracle:x\n"), 1)
	victim.Zone = state.ZExile
	victim.Counters = []state.Counter{{Kind: "VOID", N: 1}}
	e.G.SetZone(state.ZExile, 1, []state.ObjID{victim.ID})
	if victim.Zone != state.ZExile || victim.Counters[0].N != 1 {
		t.Fatal("precondition: victim not exiled with a void counter")
	}
	// Dauthi's {T}, sacrifice ability: tap+sac is payable with no mana.
	e.pending = nil
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v, want priority for seat 0", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == dauthi.ID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Dauthi's ability not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit activation: %v", err)
	}
	// The ChooseCard ask over the eligible exiled card; answer it.
	ask := passUntilAsk(t, e)
	if ask == nil || ask.Kind != decision.KChoose {
		t.Fatalf("expected the ChooseCard ask, got %+v", ask)
	}
	cidx := -1
	for _, o := range ask.Options {
		if o.Obj == victim.ID {
			cidx = o.Index
		}
	}
	if cidx < 0 {
		t.Fatalf("victim not offered in the choice ask: %+v", ask.Options)
	}
	submitChoices(t, e, cidx)
	// The effect-delivered free-cast grant: the exiled card is offered as a
	// mayplay cast. The pool is EMPTY, so only the MayPlayWithoutManaCost$
	// free read can make the offer exist at all.
	e.priorityRound()
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v, want priority after the ability resolved", d)
	}
	midx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == victim.ID && o.Mode == "mayplay" {
			midx = o.Index
		}
	}
	if midx < 0 {
		t.Fatalf("free-cast mayplay offer missing (pool empty): %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{midx}}); err != nil {
		t.Fatalf("submit free cast: %v", err)
	}
	finishCast(t, e, victim.ID)
	if got := e.G.Obj(victim.ID); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("free cast did not put the card on the battlefield: %+v", got)
	}
}
