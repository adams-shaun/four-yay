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
// the two scalar heads this phase also claims:
//
//   - TriggerRememberAmount is Forge's per-trigger remembered Integer: a
//     spawning ImmediateTrigger's RememberSVarAmount$ rider evaluates the
//     named SVar in its own resolution context (New Way Forward's
//     X:ReplaceCount$DamageAmount) and records the result on the reflexive
//     ability's TriggerContext.TriggerRememberedAmount, which rides the same
//     per-stack-instance triggerContexts map TriggerAmount does to the head's
//     read. Zero when nothing bound one -- Forge's default remembered amount.
//   - LastStateBattlefieldWithFallback is Forge's battlefield count read from
//     the last known-state snapshot (castSA.getLastStateBattlefield) with a
//     current-battlefield fallback. The snapshot is the source spell's frozen
//     as-cast battlefield (state.Object.CastBattlefield, folded from the
//     events.CastBattlefield event the cast flow emits); only a source with
//     no snapshot (a bare context, an uncast source) reads the CURRENT
//     battlefield through the ordinary Valid zone scan.
//
// Both register in effects.modelledValueHeads so the coverage gate stops
// calling their carriers unsupported.
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
	case evalCountBodyCostTriggerRememberAmount:
		// The spawning ability's RememberSVarAmount$ binding, carried on the
		// trigger's TriggerContext (never Ctx.TriggerAmount, which is the
		// causing EVENT's magnitude -- a different channel). A bare head; an
		// argument is a shape this build does not model, so fail the head's
		// own verdict rather than ignore it. An unbound context reads zero,
		// Forge's default remembered amount (and what keeps this head
		// "modelled" for the evaluator's bare-context probe).
		if arg != "" {
			return 0, false, true
		}
		return c.TriggerRememberedAmount, true, true
	case evalCountBodyCostCrewSize:
		// Forge's Count$CrewSize (Luxurious Locomotive: "a Treasure token for
		// each creature that crewed it this turn"): the creatures that paid a
		// Crew cost for the host this turn -- the pairing events.Apply's Crew
		// fold records, the same read Creature.CrewedThisTurn makes. A bare
		// head; an argument is a shape this build does not model. No host, or
		// no crewer, is a legitimate zero.
		if arg != "" {
			return 0, false, true
		}
		var n int32
		if c != nil && c.Source != 0 {
			for i := range g.Objs {
				if pairedWithSourceThisTurn(g, &g.Objs[i], c.Source) {
					n++
				}
			}
		}
		return n, true, true
	case evalCountBodyCostLastStateBattlefieldWithFallback:
		// The battlefield as the spell was cast, from its frozen snapshot,
		// else the live battlefield (see evalCastBattlefieldCount).
		return evalCastBattlefieldCount(h, c, g, arg, depth)
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
