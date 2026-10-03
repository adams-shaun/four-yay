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
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
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

func ctrHas(ids []state.ObjID, id state.ObjID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
