package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// fixtureDeckSize is the per-seat card count both runners pad a setup to
// with Wastes (rules/oracle_run.go oracleFiller; the XMage driver's
// ScenarioReplay filler). Every named setup card counts toward it.
const fixtureDeckSize = 40

// ShufflesBackAndDraws reports whether the face carries a Timetwister-style
// "each player may shuffle their hand and graveyard into their library, then
// draws" choice (DB$ GenericChoice | AILogic$ Timetwister: Turtles in Time).
func ShufflesBackAndDraws(f *cards.Face) bool {
	for _, body := range f.SVars {
		p := svarParams(body)
		if p["DB"] == "GenericChoice" && strings.EqualFold(p["AILogic"], "Timetwister") {
			return true
		}
	}
	return false
}

// ShufflesThenDraws reports a face whose Oracle text shuffles cards into a
// library and then draws from it. Those draws are random in XMage, so generated
// scenarios should use a uniform source and library when the setup permits it.
func ShufflesThenDraws(f *cards.Face) bool {
	if f == nil {
		return false
	}
	// Keep the draw causally attached to the shuffle. A later draw elsewhere
	// in the Oracle text may be unrelated (for example, a search that shuffles
	// before an activated draw ability). These shuffle-then-draw instructions
	// use the explicit sequencing phrase in one sentence.
	for _, sentence := range strings.FieldsFunc(strings.ToLower(f.Oracle), func(r rune) bool {
		return r == '.' || r == '\n'
	}) {
		shuffle := strings.Index(sentence, "shuffle")
		if shuffle < 0 {
			continue
		}
		tail := sentence[shuffle+len("shuffle"):]
		then := strings.Index(tail, "then")
		if then >= 0 && strings.Contains(tail[then+len("then"):], "draw") {
			return true
		}
	}
	return false
}

// UniformShuffleLibraries makes a shuffle-back-and-draw deterministic on both
// engines. Gorge's shuffle is seeded and XMage's is not, so a seat that
// shuffles a returned card into a library of Wastes and draws seven may or
// may not draw it back in XMage (Turtles in Time: p1's Grizzly Bears, ~1 in 5
// agreeing). When every card that can reach a seat's library this way has the
// same name, fill that seat's library with copies of it: the drawn hand is
// then the same whatever the order. cast is the spell being cast from p0's
// hand (it is on the stack, never shuffled). A seat with no such card keeps
// the Wastes filler (its draws are already uniform); a seat with several
// distinct names is left alone.
func UniformShuffleLibraries(fx *Fixture, cast string) bool {
	return uniformShuffle([]*Seat{fx.P0(), fx.P1()}, cast)
}

// UniformShuffleSetup is UniformShuffleLibraries over a built scenario's
// setup, after every fixture card (targets, opponents' creatures) is placed.
func UniformShuffleSetup(sc *Scenario, cast string) bool {
	p0, ok0 := sc.Setup["p0"]
	p1, ok1 := sc.Setup["p1"]
	uniform := uniformShuffle([]*Seat{&p0, &p1}, cast)
	if ok0 {
		sc.Setup["p0"] = p0
	}
	if ok1 {
		sc.Setup["p1"] = p1
	}
	return uniform
}

func uniformShuffle(seats []*Seat, cast string) bool {
	uniform := true
	for i, s := range seats {
		var names []string
		// Only hand and graveyard cards are returned by Timetwister-style
		// effects. A different permanent on the battlefield does not make the
		// random draw non-uniform.
		names = append(names, s.Graveyard...)
		skipped := false
		for _, n := range s.Hand {
			if i == 0 && !skipped && n == cast {
				skipped = true
				continue
			}
			names = append(names, n)
		}
		distinct := map[string]bool{}
		for _, n := range names {
			distinct[n] = true
		}
		if len(distinct) > 1 || len(s.Library) > 0 || len(s.LibraryTop) > 0 {
			uniform = false
			continue
		}
		if len(distinct) == 0 {
			continue
		}
		named := len(s.Battlefield) + len(s.Hand) + len(s.Graveyard) + len(s.Exile)
		if named >= fixtureDeckSize {
			uniform = false
			continue
		}
		s.Library = Repeat(names[0], fixtureDeckSize-named)
	}
	return uniform
}
