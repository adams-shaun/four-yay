package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// evalCountBodySimple evaluates the "plain" per-head arms that read a fact the
// engine already records and need no state of their own. They live in their
// own phase evaluator rather than appended to evalCountBodyPaid /
// evalCountBodyCost, both of which are already over the long-function ceiling.
// It is claimed after every existing arm and before the zone arm, so no head
// another evaluator already owns is preempted.
//
// Each arm follows the dispatch's verdict convention: a recognised head is
// modelled (ok true) even when it legitimately counts zero, and only a
// malformed argument this build cannot parse fails the head's own verdict.
func evalCountBodySimple(h Host, c *Ctx, g *state.Game, head, arg string, depth int) (int32, bool, bool) {
	switch countBodySimpleCodes.Code(string(head)) {
	case countBodySimpleIsPrime:
		// Forge's Count$IsPrime <SVar>.<True>.<False> (DSK Zimone,
		// All-Questioning): evaluate <SVar>, then answer the <True> operand
		// when the value is prime and <False> otherwise. The corpus splits as
		// `l[0].split(".")` -> ["IsPrime Y", "Z", "0"], so the SVar name is
		// the token after the space and the two branches are the remaining
		// dot-separated tokens -- exactly Forge's compString[1] plus sq[1]/sq[2].
		// Both branches resolve through the ordinary operand grammar
		// (evalCountOperand), so a numeric literal, an SVar name and an inline
		// Count$ body all read the same way evalCompare's branches do. A
		// missing branch fails the head's verdict rather than guessing.
		if arg == "" {
			return 0, false, true
		}
		src, after, found := strings.Cut(arg, ".")
		src = strings.TrimSpace(src)
		if !found || src == "" {
			return 0, false, true
		}
		trueTok, falseTok, found := strings.Cut(after, ".")
		if !found {
			return 0, false, true
		}
		value := evalCountOperand(h, c, src, depth)
		if isPrime(value) {
			return evalCountOperand(h, c, trueTok, depth), true, true
		}
		return evalCountOperand(h, c, falseTok, depth), true, true
	case countBodySimpleImprintedSize:
		// Forge's Count$ImprintedSize is c.getImprintedCards().size(): the
		// cards Imprint$/ImprintCards$ associated with the ability's host. In
		// this engine those associations are state.Object.Imprinted, written
		// by foldImprint for both spellings (DSK Oblivious Bookworm's
		// TurnFaceUp `ImprintCards$ TriggeredCard`, cleared at the upkeep by
		// ClearImprinted$; TDM Unexpected Conversion / Grizzled Huntmaster's
		// ChangeZone `Imprint$ True`). c.Source is that host, so the read is
		// the same object's persistent list. A missing source reads a
		// legitimate zero.
		if arg != "" {
			return 0, false, true
		}
		if o := g.Obj(c.Source); o != nil {
			return int32(len(o.Imprinted)), true, true
		}
		return 0, true, true
	case countBodySimpleFinishedEndOfTurnsThisTurn:
		// Forge's Count$FinishedEndOfTurnsThisTurn (FIN Y'shtola Rhul's "if
		// it's the first end step of the turn" gate): getNumEndOfTurn() minus
		// one while the walk is currently IN an end step. state.Game's
		// EndStepsThisTurn is the event-folded count of end-step entries this
		// turn, so the read is g.EndStepsThisTurn minus the in-step
		// adjustment. A turn that has not reached an end step reads zero.
		if arg != "" {
			return 0, false, true
		}
		n := g.EndStepsThisTurn
		if g.Step == state.StepEnd {
			n--
		}
		if n < 0 {
			n = 0
		}
		return n, true, true
	}
	return 0, false, false
}

// isPrime reports whether n is a prime number (a natural number greater than 1
// with no positive divisors other than 1 and itself). Forge delegates to
// Guava's IntMath.isPrime, which throws for n <= 1; this build answers false
// for those instead of panicking, the fail-safe reading of "is this count
// prime" (a zero or negative count is not). The trial division is bounded by
// i <= n/i so no integer overflow can loop forever.
func isPrime(n int32) bool {
	if n < 2 {
		return false
	}
	for i := int32(2); i <= n/i; i++ {
		if n%i == 0 {
			return false
		}
	}
	return true
}

type countBodySimpleCode uint16

const (
	countBodySimpleIsPrime countBodySimpleCode = iota + 1
	countBodySimpleImprintedSize
	countBodySimpleFinishedEndOfTurnsThisTurn
)

var countBodySimpleCodes = state.NewStrCodes(
	state.StrEntry[countBodySimpleCode]{Key: "IsPrime", Val: countBodySimpleIsPrime},
	state.StrEntry[countBodySimpleCode]{Key: "ImprintedSize", Val: countBodySimpleImprintedSize},
	state.StrEntry[countBodySimpleCode]{Key: "FinishedEndOfTurnsThisTurn", Val: countBodySimpleFinishedEndOfTurnsThisTurn},
)
