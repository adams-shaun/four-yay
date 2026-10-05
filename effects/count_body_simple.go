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
// The head vocabulary is evalCountBodyCostCodes (shared because the codeshape
// ratchet caps the number of StrCodes tables); evalCountBodyCost's own switch
// has no case for these codes, so its matched verdict stays false and the
// dispatch falls through to here.
//
// Each arm follows the dispatch's verdict convention: a recognised head is
// modelled (ok true) even when it legitimately counts zero, and only a
// malformed argument this build cannot parse fails the head's own verdict.
//
// Count$TriggerRememberAmount and Count$LastStateBattlefieldWithFallback are
// deliberately NOT here. Both name state this engine does not carry:
//
//   - TriggerRememberAmount is Forge's per-trigger remembered-Integer sum
//     (ImmediateTriggerEffect addRemembered()s the RememberSVarAmount$ value;
//     a DelayedTrigger RememberNumber$ copies the chain's rememberedNumber).
//     Ctx.TriggerAmount is NOT that channel: rules only writes it from a real
//     triggering event's magnitude, and the replacement/reflexive path every
//     carrier uses (replCtx -> DBImmediateTrigger -> QueueReflexiveTrigger)
//     never assigns it, so the three seeds the 22 carriers actually use
//     (RememberSVarAmount$ X/Result/NumTimes, RememberCounteredCMC$,
//     RememberNumber$) do not reach the read. Registering the head would
//     unsupport the compliance gate while the cards still resolve to zero.
//   - LastStateBattlefieldWithFallback is a CAST-TIME battlefield snapshot
//     (castSA.getLastStateBattlefield) with a current-battlefield fallback.
//     No cast-time snapshot exists anywhere in state/rules/effects, so the
//     fallback would be the whole read and a permanent that entered after the
//     cast but before resolution would be miscounted.
//
// Both stay out of effects.modelledValueHeads and out of the evaluator until
// the remembered-amount channel and the cast-time snapshot exist; the callers
// keep their pre-existing degrade-to-zero behaviour.
func evalCountBodySimple(h Host, c *Ctx, g *state.Game, head, arg string, depth int) (int32, bool, bool) {
	switch evalCountBodyCostCodes.Code(string(head)) {
	case evalCountBodyCostManaPoolAll, evalCountBodyCostManaPoolGreen:
		if c == nil || int(c.Controller) >= len(g.Players) {
			return 0, false, true
		}
		pool := g.Players[c.Controller].Pool
		if evalCountBodyCostCodes.Code(string(head)) == evalCountBodyCostManaPoolAll {
			return pool.Total(), true, true
		}
		return pool[state.MG], true, true
	case evalCountBodyCostIsPrime:
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
	case evalCountBodyCostImprintedSize:
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
	case evalCountBodyCostFinishedEndOfTurnsThisTurn:
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
