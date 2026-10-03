package rules

// W3 step 2a(ctr): dual-run tests for the counter family and the
// per-target/per-player loop primitives converted onto the resolution
// kernel's tape (PutCounter's elections and picks, RemoveCounter's pick,
// AddOrRemoveCounter, MoveCounter, Proliferate, Empower, TimeTravel, Blight,
// DealDamage's division, TwoPiles, Clash, Demonstrate, Connive, Explore,
// optional Investigate and RollDice's choose-one-result). Each scenario runs
// on legacy and on the kernel and must stay event-, intent-, head- and
// RNG-identical with every converted ask served from the tape.

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/state"
)

// ctrSetup is a dual-run table: seat 0's deck opens with seat0 (moved to
// hand), seat 1's with seat1 (left where the deal put them; scenarios move
// them by name), every other card a Mountain, and the token table tokens.
type ctrSetup struct {
	seats        int
	seed         uint64
	seat0, seat1 []*cards.Card
	tokens       map[string]*cards.Card
}

func ctrFixture(t *testing.T, s ctrSetup, tape bool) (*Engine, Config) {
	t.Helper()
	names := make([]string, s.seats)
	decks := make([][]*cards.Card, s.seats)
	for i := range names {
		names[i] = fmt.Sprintf("p%d", i)
		decks[i] = mountainDeck(t, 40)
	}
	decks[0] = append(append([]*cards.Card(nil), s.seat0...), mountainDeck(t, 40-len(s.seat0))...)
	if s.seats > 1 && len(s.seat1) > 0 {
		decks[1] = append(append([]*cards.Card(nil), s.seat1...), mountainDeck(t, 40-len(s.seat1))...)
	}
	tokens := s.tokens
	if tokens == nil {
		tokens = map[string]*cards.Card{}
	}
	cfg := seatZeroStart(Config{Seed: s.seed, Names: names, Decks: decks, Tokens: tokens})
	cfg.LegacyResume = !tape
	prev := tapeKernelEnv
	tapeKernelEnv = tapeKernelEnv && tape
	e := New(cfg)
	tapeKernelEnv = prev
	e.Advance()
	for _, f := range s.seat0 {
		name := f.Faces[0].Name
		inHand := false
		for _, id := range e.G.Zone(state.ZHand, 0) {
			if o := e.G.Obj(id); o.Face().Name == name {
				inHand = true
			}
		}
		if !inHand {
			moveByName(t, e, 0, name, state.ZHand)
		}
	}
	return e, cfg
}

// ctrDual is tapeDual over a ctrSetup.
func ctrDual(t *testing.T, s ctrSetup, scenario func(t *testing.T, e *Engine)) (*Engine, resolve.Stats) {
	t.Helper()
	legacy, _ := ctrFixture(t, s, false)
	scenario(t, legacy)
	before := resolve.ReadStats()
	tape, cfg := ctrFixture(t, s, true)
	scenario(t, tape)
	st := resolve.ReadStats().Sub(before)
	tapeRequireSameLog(t, legacy, tape)
	replayCheck(t, tape, cfg)
	t.Logf("kernel stats: %+v", st)
	return tape, st
}

// ctrRequireServed holds a scenario to the conversion: at least minServed
// tape answers, no legacy ask ending a run.
func ctrRequireServed(t *testing.T, name string, st resolve.Stats, minServed int64) {
	t.Helper()
	if st.Served < minServed || st.LegacySwitch != 0 || st.Aborts != 0 {
		t.Fatalf("%s: the converted asks were not served from the tape (want >= %d): %+v", name, minServed, st)
	}
}

func ctrCards(t *testing.T, srcs ...string) []*cards.Card {
	t.Helper()
	out := make([]*cards.Card, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, card(t, s))
	}
	return out
}

func ctrSorcery(name, body string) string {
	return "Name:" + name + "\nManaCost:R\nTypes:Sorcery\n" + body + "\nOracle:x\n"
}

const (
	ctrBearSrc  = "Name:Ctr Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	ctrGiantSrc = "Name:Ctr Giant\nManaCost:2 G\nTypes:Creature Giant\nPT:2/2\nOracle:x\n"
	ctrOgreSrc  = "Name:Ctr Ogre\nManaCost:2 R\nTypes:Creature Ogre\nPT:3/3\nOracle:x\n"
)

// ctrBoard puts seat p's named creatures onto the battlefield and gives each
// the listed counters (kind:n pairs per creature, in order).
func ctrBoard(t *testing.T, e *Engine, p state.PlayerID, names []string, counters ...map[string]int32) []state.ObjID {
	t.Helper()
	ids := make([]state.ObjID, 0, len(names))
	for i, n := range names {
		id := moveByName(t, e, p, n, state.ZBattlefield)
		ids = append(ids, id)
		if i < len(counters) {
			for _, k := range []string{"P1P1", "M1M1", "TIME", "LOYALTY", "CHARGE"} {
				if v := counters[i][k]; v != 0 {
					e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: k, Amount: v})
				}
			}
		}
	}
	return ids
}

// ctrSpellCase is one synthetic sorcery cast by seat 0 over a prepared
// board.
type ctrSpellCase struct {
	name, body string
	board      []string           // seat 0's creatures put onto the battlefield
	counters   []map[string]int32 // their counters, in board order
	served     int64
}

func runCtrSpellCases(t *testing.T, seed uint64, cases []ctrSpellCase) {
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srcs := []string{ctrSorcery(tc.name, tc.body), ctrBearSrc, ctrBearSrc, ctrGiantSrc, ctrOgreSrc}
			s := ctrSetup{seats: 2, seed: seed + uint64(i), seat0: ctrCards(t, srcs...)}
			_, st := ctrDual(t, s, func(t *testing.T, e *Engine) {
				ctrBoard(t, e, 0, tc.board, tc.counters...)
				tapeCastAndResolve(t, e, tc.name, "R")
			})
			ctrRequireServed(t, tc.name, st, tc.served)
		})
	}
}

func TestTapeConvertPutCounter(t *testing.T) {
	two := []string{"Ctr Bear", "Ctr Bear"}
	three := []string{"Ctr Bear", "Ctr Bear", "Ctr Giant"}
	runCtrSpellCases(t, 31000, []ctrSpellCase{
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

func TestTapeConvertCounterChoices(t *testing.T) {
	p1 := map[string]int32{"P1P1": 2}
	mixed := map[string]int32{"P1P1": 2, "TIME": 1, "CHARGE": 3}
	runCtrSpellCases(t, 32000, []ctrSpellCase{
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

func TestTapeConvertResolutionLoops(t *testing.T) {
	two := []string{"Ctr Bear", "Ctr Giant"}
	runCtrSpellCases(t, 33000, []ctrSpellCase{
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
func TestTapeConvertTwoPiles(t *testing.T) {
	src := ctrSorcery("Ctr Fiction", "A:SP$ PeekAndReveal | PeekAmount$ 5 | RememberRevealed$ True | NoPeek$ True | SubAbility$ DBTwoPiles\n"+
		"SVar:DBTwoPiles:DB$ TwoPiles | Defined$ You | DefinedCards$ Remembered | Separator$ Opponent | ChosenPile$ DBHand | UnchosenPile$ DBGrave | SubAbility$ DBGain\n"+
		"SVar:DBHand:DB$ ChangeZone | Defined$ Remembered | Origin$ Library | Destination$ Hand\n"+
		"SVar:DBGrave:DB$ ChangeZone | Defined$ Remembered | Origin$ Library | Destination$ Graveyard\n"+
		"SVar:DBGain:DB$ GainLife | LifeAmount$ 2")
	for i, seats := range []int{2, 3} {
		s := ctrSetup{seats: seats, seed: 34000 + uint64(i), seat0: ctrCards(t, src)}
		_, st := ctrDual(t, s, func(t *testing.T, e *Engine) {
			tapeCastAndResolve(t, e, "Ctr Fiction", "R")
		})
		ctrRequireServed(t, "Ctr Fiction", st, 2)
	}
}

// Explore over two creatures with nonland cards on top of the library: each
// explore poses its destination election.
func TestTapeConvertExplore(t *testing.T) {
	src := ctrSorcery("Ctr Explore", "A:SP$ Explore | Defined$ Valid Creature.YouCtrl | Num$ 2 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2")
	s := ctrSetup{seats: 2, seed: 35000, seat0: ctrCards(t, src, ctrBearSrc, ctrGiantSrc, ctrOgreSrc, ctrOgreSrc, ctrOgreSrc, ctrOgreSrc)}
	_, st := ctrDual(t, s, func(t *testing.T, e *Engine) {
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

func ctrHas(ids []state.ObjID, id state.ObjID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// Seat 1's creatures make Blight's Defined$ Player walk ask both players.
func TestTapeConvertBlightEachPlayer(t *testing.T) {
	src := ctrSorcery("Ctr Blight All", "A:SP$ Blight | Defined$ Player | Num$ 1")
	s := ctrSetup{seats: 2, seed: 36000,
		seat0: ctrCards(t, src, ctrBearSrc, ctrGiantSrc),
		seat1: ctrCards(t, ctrOgreSrc, ctrOgreSrc)}
	_, st := ctrDual(t, s, func(t *testing.T, e *Engine) {
		ctrBoard(t, e, 0, []string{"Ctr Bear", "Ctr Giant"})
		ctrBoard(t, e, 1, []string{"Ctr Ogre", "Ctr Ogre"})
		tapeCastAndResolve(t, e, "Ctr Blight All", "R")
	})
	ctrRequireServed(t, "Ctr Blight All", st, 2)
}

// Empower over two Jace tokens: the token pick.
func TestTapeConvertEmpower(t *testing.T) {
	src := ctrSorcery("Ctr Empower", "A:SP$ Token | TokenScript$ ctr_jace | TokenAmount$ 2 | SubAbility$ DBEmpower\nSVar:DBEmpower:DB$ Empower | Type$ Jace | Num$ 2")
	jace := card(t, "Name:Jace Token\nManaCost:no cost\nTypes:Planeswalker Jace\nLoyalty:3\nOracle:\n")
	s := ctrSetup{seats: 2, seed: 37000, seat0: ctrCards(t, src), tokens: map[string]*cards.Card{"ctr_jace": jace}}
	_, st := ctrDual(t, s, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Ctr Empower", "R")
	})
	ctrRequireServed(t, "Ctr Empower", st, 1)
}

// Optional$ Investigate over every player: one election per player.
func TestTapeConvertInvestigateOptional(t *testing.T) {
	src := ctrSorcery("Ctr Investigate", "A:SP$ Investigate | Defined$ Player | Optional$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2")
	clue := card(t, "Name:Clue Token\nManaCost:no cost\nTypes:Artifact Clue\nOracle:\n")
	s := ctrSetup{seats: 3, seed: 38000, seat0: ctrCards(t, src), tokens: map[string]*cards.Card{"c_a_clue_draw": clue}}
	_, st := ctrDual(t, s, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Ctr Investigate", "R")
	})
	ctrRequireServed(t, "Ctr Investigate", st, 3)
}

// Demonstrate (the real Excavation Technique) at three- and four-seat
// tables: the may-copy election, then (on a yes) the opponent pick. The
// variants answer the election differently, so both stages are served.
func TestTapeConvertDemonstrate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tech := searchCorpusCard(t, reg, "Excavation Technique")
	best := int64(0)
	for i, seats := range []int{3, 4, 3, 4} {
		s := ctrSetup{seats: seats, seed: 39000 + uint64(i)*7, seat0: []*cards.Card{tech}, seat1: ctrCards(t, ctrBearSrc), tokens: reg.Tokens}
		_, st := ctrDual(t, s, func(t *testing.T, e *Engine) {
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
