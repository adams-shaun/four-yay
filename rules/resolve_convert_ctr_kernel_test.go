package rules

// Kernel-era restorations of the W3 step 2 dual-run tests of
// resolve_convert_ctr_test.go: one kernel run per scenario (kr7Run), the
// asks served from the tape, the named asks posed, the replay identical.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/state"
)

// kr7CtrRun is kr7Run over a ctrSetup.
func kr7CtrRun(t *testing.T, s ctrSetup, scenario func(t *testing.T, e *Engine)) (*Engine, resolve.Stats) {
	t.Helper()
	return kr7Run(t, kr7Setup(s), scenario)
}

func kr7RunCtrSpellCases(t *testing.T, seed uint64, cases []ctrSpellCase) {
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srcs := []string{ctrSorcery(tc.name, tc.body), ctrBearSrc, ctrBearSrc, ctrGiantSrc, ctrOgreSrc}
			s := ctrSetup{seats: 2, seed: seed + uint64(i), seat0: ctrCards(t, srcs...)}
			_, st := kr7CtrRun(t, s, func(t *testing.T, e *Engine) {
				ctrBoard(t, e, 0, tc.board, tc.counters...)
				tapeCastAndResolve(t, e, tc.name, "R")
			})
			ctrRequireServed(t, tc.name, st, tc.served)
		})
	}
}

func TestKernelAskPutCounter(t *testing.T) {
	two := []string{"Ctr Bear", "Ctr Bear"}
	three := []string{"Ctr Bear", "Ctr Bear", "Ctr Giant"}
	kr7RunCtrSpellCases(t, 31000, []ctrSpellCase{
		{name: "Ctr Maybe Counter", body: "A:SP$ PutCounter | ValidTgts$ Creature | CounterType$ P1P1 | CounterNum$ 2 | Optional$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			board: two, served: 1},
		// The election then the pick; the two variants answer the election
		// differently (tapePick's sequence parity), so both arms run.
		{name: "Ctr Maybe Pick", body: "A:SP$ PutCounter | Choices$ Creature.YouCtrl | CounterType$ P1P1 | CounterNum$ 1 | Optional$ True",
			board: three, served: 1},
		{name: "Ctr Maybe Pick Late", body: "A:SP$ ChooseColor | Defined$ You | SubAbility$ DBPut\nSVar:DBPut:DB$ PutCounter | Choices$ Creature.YouCtrl | CounterType$ P1P1 | CounterNum$ 1 | Optional$ True",
			board: three, served: 2},
		{name: "Ctr Kinds", body: "A:SP$ PutCounter | ValidTgts$ Creature | CounterType$ Menace,Deathtouch,Lifelink | ChooseDifferent$ 2 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			board: two, served: 1},
		{name: "Ctr Kind", body: "A:SP$ PutCounter | ValidTgts$ Creature | CounterType$ P1P1,M1M1 | CounterNum$ 2",
			board: two, served: 1},
		{name: "Ctr Kind Each", body: "A:SP$ PutCounter | Defined$ Valid Creature.YouCtrl | CounterType$ Vigilance,Reach,Trample | CounterTypePerDefined$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			board: three, served: 3},
		{name: "Ctr Distribute", body: "A:SP$ PutCounter | Choices$ Creature.YouCtrl | DividedAsYouChoose$ 3 | CounterNum$ 3 | MinChoiceAmount$ 1 | ChoiceAmount$ 3 | CounterType$ P1P1",
			board: three, served: 1},
		{name: "Ctr Pick", body: "A:SP$ PutCounter | Choices$ Creature.YouCtrl | CounterType$ P1P1 | CounterNum$ 2 | RememberCards$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			board: three, served: 1},
		{name: "Ctr Pick Kind", body: "A:SP$ PutCounter | Choices$ Creature.YouCtrl | CounterType$ P1P1,M1M1 | CounterNum$ 1",
			board: three, served: 2},
		{name: "Ctr Bolster", body: "A:SP$ PutCounter | Bolster$ 2 | CounterType$ P1P1",
			board: three, served: 1},
		{name: "Ctr Support", body: "A:SP$ PutCounter | Support$ 2 | CounterType$ P1P1",
			board: three, served: 1},
	})
}

func TestKernelAskCounterChoices(t *testing.T) {
	p1 := map[string]int32{"P1P1": 2}
	mixed := map[string]int32{"P1P1": 2, "TIME": 1, "CHARGE": 3}
	kr7RunCtrSpellCases(t, 32000, []ctrSpellCase{
		{name: "Ctr Remove Pick", body: "A:SP$ RemoveCounter | Choices$ Creature.YouCtrl | ChoiceOptional$ True | CounterType$ P1P1 | CounterNum$ 1 | RememberAmount$ True",
			board: []string{"Ctr Bear", "Ctr Bear", "Ctr Giant"}, counters: []map[string]int32{p1, p1, p1}, served: 1},
		{name: "Ctr Aor Named", body: "A:SP$ AddOrRemoveCounter | ValidTgts$ Creature | CounterType$ P1P1 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			board: []string{"Ctr Bear"}, counters: []map[string]int32{p1}, served: 1},
		{name: "Ctr Aor Combined", body: "A:SP$ AddOrRemoveCounter | ValidTgts$ Creature | Optional$ True",
			board: []string{"Ctr Bear"}, counters: []map[string]int32{mixed}, served: 1},
		{name: "Ctr Aor Each", body: "A:SP$ AddOrRemoveCounter | ValidTgts$ Creature | EachExistingCounter$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			board: []string{"Ctr Bear"}, counters: []map[string]int32{mixed}, served: 3},
		{name: "Ctr Move Any", body: "A:SP$ MoveCounter | ValidTgts$ Creature | TargetMin$ 2 | TargetMax$ 2 | CounterType$ Any | CounterNum$ Any | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			board: []string{"Ctr Bear", "Ctr Giant"}, counters: []map[string]int32{mixed, mixed}, served: 2},
		{name: "Ctr Proliferate", body: "A:SP$ Proliferate | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			board: []string{"Ctr Bear", "Ctr Giant", "Ctr Ogre"}, counters: []map[string]int32{p1, mixed}, served: 1},
		{name: "Ctr Time Travel", body: "A:SP$ TimeTravel | Amount$ 2 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			board: []string{"Ctr Bear", "Ctr Giant"}, counters: []map[string]int32{{"TIME": 2}, {"TIME": 1}}, served: 3},
	})
}

func TestKernelAskResolutionLoops(t *testing.T) {
	two := []string{"Ctr Bear", "Ctr Giant"}
	kr7RunCtrSpellCases(t, 33000, []ctrSpellCase{
		{name: "Ctr Blight", body: "A:SP$ Blight | Defined$ Player | Num$ 1",
			board: two, served: 1},
		{name: "Ctr Divide", body: "A:SP$ DealDamage | ValidTgts$ Any | TargetMin$ 2 | TargetMax$ 3 | NumDmg$ 4 | DividedAsYouChoose$ 4 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			board: two, served: 1},
		{name: "Ctr Clash", body: "A:SP$ Clash | WinSubAbility$ DBGain | OtherwiseSubAbility$ DBDraw\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2\nSVar:DBDraw:DB$ Draw | NumCards$ 1",
			served: 2},
		{name: "Ctr Connive", body: "A:SP$ Connive | Defined$ Valid Creature.YouCtrl | ConniveNum$ 1 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			board: two, served: 2},
		{name: "Ctr Roll", body: "A:SP$ RollDice | Amount$ 2 | Sides$ 6 | ResultSVar$ Z | ChosenSVar$ X | OtherSVar$ Y | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ X | SubAbility$ DBLose\nSVar:DBLose:DB$ LoseLife | LifeAmount$ Y\nSVar:X:Number$0\nSVar:Y:Number$0",
			served: 1},
	})
}

// Fact or Fiction's shape: the split (an opponent's), then the pick.
func TestKernelAskTwoPiles(t *testing.T) {
	src := ctrSorcery("Ctr Fiction", "A:SP$ PeekAndReveal | PeekAmount$ 5 | RememberRevealed$ True | NoPeek$ True | SubAbility$ DBTwoPiles\n"+
		"SVar:DBTwoPiles:DB$ TwoPiles | Defined$ You | DefinedCards$ Remembered | Separator$ Opponent | ChosenPile$ DBHand | UnchosenPile$ DBGrave | SubAbility$ DBGain\n"+
		"SVar:DBHand:DB$ ChangeZone | Defined$ Remembered | Origin$ Library | Destination$ Hand\n"+
		"SVar:DBGrave:DB$ ChangeZone | Defined$ Remembered | Origin$ Library | Destination$ Graveyard\n"+
		"SVar:DBGain:DB$ GainLife | LifeAmount$ 2")
	for i, seats := range []int{2, 3} {
		s := ctrSetup{seats: seats, seed: 34000 + uint64(i), seat0: ctrCards(t, src)}
		_, st := kr7CtrRun(t, s, func(t *testing.T, e *Engine) {
			tapeCastAndResolve(t, e, "Ctr Fiction", "R")
		})
		ctrRequireServed(t, "Ctr Fiction", st, 2)
	}
}

// Explore over two creatures with nonland cards on top of the library: each
// explore poses its destination election.
func TestKernelAskExplore(t *testing.T) {
	src := ctrSorcery("Ctr Explore", "A:SP$ Explore | Defined$ Valid Creature.YouCtrl | Num$ 2 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2")
	s := ctrSetup{seats: 2, seed: 35000, seat0: ctrCards(t, src, ctrBearSrc, ctrGiantSrc, ctrOgreSrc, ctrOgreSrc, ctrOgreSrc, ctrOgreSrc)}
	_, st := kr7CtrRun(t, s, func(t *testing.T, e *Engine) {
		ctrBoard(t, e, 0, []string{"Ctr Bear", "Ctr Giant"})
		var ogres []state.ObjID
		for i := 0; i < 4; i++ {
			ogres = append(ogres, moveByName(t, e, 0, "Ctr Ogre", state.ZLibrary))
		}
		lib := append([]state.ObjID(nil), ogres...)
		for _, id := range e.G.Zone(state.ZLibrary, 0) {
			if !ctrHas(ogres, id) {
				lib = append(lib, id)
			}
		}
		e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: lib, Secret: true})
		tapeCastAndResolve(t, e, "Ctr Explore", "R")
	})
	ctrRequireServed(t, "Ctr Explore", st, 2)
}

// Seat 1's creatures make Blight's Defined$ Player walk ask both players.
func TestKernelAskBlightEachPlayer(t *testing.T) {
	src := ctrSorcery("Ctr Blight All", "A:SP$ Blight | Defined$ Player | Num$ 1")
	s := ctrSetup{seats: 2, seed: 36000,
		seat0: ctrCards(t, src, ctrBearSrc, ctrGiantSrc),
		seat1: ctrCards(t, ctrOgreSrc, ctrOgreSrc)}
	_, st := kr7CtrRun(t, s, func(t *testing.T, e *Engine) {
		ctrBoard(t, e, 0, []string{"Ctr Bear", "Ctr Giant"})
		ctrBoard(t, e, 1, []string{"Ctr Ogre", "Ctr Ogre"})
		tapeCastAndResolve(t, e, "Ctr Blight All", "R")
	})
	ctrRequireServed(t, "Ctr Blight All", st, 2)
}

// Empower over two Jace tokens: the token pick.
func TestKernelAskEmpower(t *testing.T) {
	src := ctrSorcery("Ctr Empower", "A:SP$ Token | TokenScript$ ctr_jace | TokenAmount$ 2 | SubAbility$ DBEmpower\nSVar:DBEmpower:DB$ Empower | Type$ Jace | Num$ 2")
	jace := card(t, "Name:Jace Token\nManaCost:no cost\nTypes:Planeswalker Jace\nLoyalty:3\nOracle:\n")
	s := ctrSetup{seats: 2, seed: 37000, seat0: ctrCards(t, src), tokens: map[string]*cards.Card{"ctr_jace": jace}}
	_, st := kr7CtrRun(t, s, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Ctr Empower", "R")
	})
	ctrRequireServed(t, "Ctr Empower", st, 1)
}

// Optional$ Investigate over every player: one election per player.
func TestKernelAskInvestigateOptional(t *testing.T) {
	src := ctrSorcery("Ctr Investigate", "A:SP$ Investigate | Defined$ Player | Optional$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2")
	clue := card(t, "Name:Clue Token\nManaCost:no cost\nTypes:Artifact Clue\nOracle:\n")
	s := ctrSetup{seats: 3, seed: 38000, seat0: ctrCards(t, src), tokens: map[string]*cards.Card{"c_a_clue_draw": clue}}
	_, st := kr7CtrRun(t, s, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Ctr Investigate", "R")
	})
	ctrRequireServed(t, "Ctr Investigate", st, 3)
}

// Demonstrate (the real Excavation Technique) at three- and four-seat
// tables: the may-copy election, then (on a yes) the opponent pick. The
// variants answer the election differently, so both stages are served.
func TestKernelAskDemonstrate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tech := searchCorpusCard(t, reg, "Excavation Technique")
	best := int64(0)
	for i, seats := range []int{3, 4, 3, 4} {
		s := ctrSetup{seats: seats, seed: 39000 + uint64(i)*7, seat0: []*cards.Card{tech}, seat1: ctrCards(t, ctrBearSrc), tokens: reg.Tokens}
		_, st := kr7CtrRun(t, s, func(t *testing.T, e *Engine) {
			ctrBoard(t, e, 1, []string{"Ctr Bear"})
			tapeCastAndResolve(t, e, "Excavation Technique", "WWWW")
		})
		ctrRequireServed(t, "Excavation Technique", st, 1)
		best = max(best, st.Served)
	}
	if best < 3 {
		t.Fatalf("no variant reached the opponent pick (best served %d)", best)
	}
}
