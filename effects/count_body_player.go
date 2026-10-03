package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// evalCountBodyPlayer evaluates the player-counter and PlayerCount* head
// family (YourCounters, PlayerCountPlayers/Opponents/HasLost/Defined*/
// PropertyYou, OppGreatestLifeTotal, StartingPlayer.) claimed next.
func evalCountBodyPlayer(h Host, c *Ctx, g *state.Game, head, arg string, depth int) (int32, bool, bool) {
	// Count$YourCounters<KIND> — the resolving controller's own player
	// counters of KIND (Forge's Count$YourCounters* family, measured 31
	// corpus files at the current pin: YourCountersExperience 15 lines,
	// YourCountersEnergy 15, YourCountersRAD 1). The suffix upper-cased is
	// Forge's counter-kind name; the stored kinds are the CounterType$ text
	// the granting script wrote ("ENERGY" from Razorfield Ripper,
	// "Experience" from Otharri, "RAD" from Radaway), so the read matches
	// case-insensitively and sums any same-kind entries — deterministic
	// either way, since the slice is insertion order and a kind is written
	// one way per card. The read is a plain read of Player.Counters, which
	// events.PlayerCounterChange folds, so it is event-backed and
	// replay-derivable; no new event or provenance is needed. Razorfield
	// Ripper's SVar:X:Count$YourCountersEnergy drives its attack pump,
	// Localized Destruction's and Aether Refinery's DB$ ChooseNumber
	// Max$ Count$YourCountersEnergy bounds their may-pay-{E} ask. A
	// controller out of range (no resolution in flight) reads 0.
	if rest, ok := strings.CutPrefix(head, "YourCounters"); ok {
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, true, true
		}
		n := int32(0)
		for _, pc := range g.Players[c.Controller].Counters {
			if strings.EqualFold(pc.Kind, rest) {
				n += pc.N
			}
		}
		return n, true, true
	}

	// PlayerCount<Players|Opponents|RegisteredOpponents>$<Property> — per-
	// group extreme properties. "Players" spans every living player,
	// "Opponents" every living player but the resolving controller, the same
	// groups the bare PlayerCountPlayers/PlayerCountOpponents heads count
	// (RegisteredOpponents is Forge's game-start opponent set, read here as
	// the same living-opponent group). Two property families are resolvable:
	// the life-total extremes (Vampire Lacerator's ConditionCheckSVar$
	// OpponentSmallest: PlayerCountOpponents$LowestLifeTotal, GE11 — "you
	// lose 1 life unless an opponent has 10 or less life") and the
	// count/counted-quantity extremes playerCountExtreme answers below. Any
	// other property is NOT resolvable: (0, false) — the same verdict
	// Count$Valid's UnknownPredicates takes — so a gate over one fails per
	// its caller's documented direction rather than enforcing a fake zero.
	// An empty group also fails unresolvable for a life extreme (lifeExtreme
	// reports no extreme), for the same reason: a threshold compared against
	// an absent extreme is not readable either.
	//
	// The `Amount` property IS resolvable on the living groups (and the
	// Registered spellings, which read the same living sets): Forge's
	// property `Amount` counts 1 per group member, so
	// PlayerCountOpponents$Amount is the opponent count — the dominant
	// "one each" bound the corpus names SVar:OneEach (99 raw Opponents +
	// 52 raw Players lines at the pfpe1 gate) and the per-player target
	// bound the TargetsForEachPlayer$ shape needs (Havoc Eater's
	// TargetMax$ X with SVar:X:PlayerCountOpponents$Amount).
	if rest, ok := strings.CutPrefix(head, "PlayerCountPlayers$"); ok {
		if strings.HasPrefix(rest, "Counters.") {
			kind := strings.TrimPrefix(rest, "Counters.")
			var total int32
			for _, p := range g.AliveFrom(0) {
				if kind == "ALL" {
					for _, counter := range g.Players[p].Counters {
						total += counter.N
					}
				} else {
					total += g.Players[p].Counter(strings.ToUpper(kind))
				}
			}
			return total, true, true
		}
		if n, ok2 := playerGroupCount(g.AliveFrom(0), rest); ok2 {
			return n, true, true
		}
		if n, ok2 := lifeExtreme(g, g.AliveFrom(0), rest); ok2 {
			return n, true, true
		}
		if n, ok2 := hasPropertyLostLifeCount(h, g.AliveFrom(0), rest); ok2 {
			return n, true, true
		}
		if n, ok2 := hasPropertyStateBacked(h, g, c, g.AliveFrom(0), rest, arg); ok2 {
			return n, true, true
		}
		if n, ok2 := playerCountCondition(h, g, c, g.AliveFrom(0), rest, arg); ok2 {
			return n, true, true
		}
		av, aok := playerCountExtreme(h, g, c, g.AliveFrom(0), rest, arg)
		return av, aok, true
	}
	if rest, ok := strings.CutPrefix(head, "PlayerCountRegisteredOpponents$"); ok {
		if n, ok2 := playerGroupCount(opponentGroup(g, c), rest); ok2 {
			return n, true, true
		}
		if n, ok2 := playerCountCondition(h, g, c, opponentGroup(g, c), rest, arg); ok2 {
			return n, true, true
		}
		// Forge's REGISTERED opponents — the opponents registered at game
		// start (Bloodchief Ascension's "if an opponent lost 2 or more life
		// this turn" gate). No registered-membership list survives a replay
		// here, so the group reads as the same living-opponent set
		// PlayerCountOpponents$ counts; the property dispatch below is shared
		// with PlayerCountDefinedRegistered.Other$ (same group), so the
		// HasPropertywasDealtCombatDamageThisTurnBy carriers on this group
		// (Blitzball's legendary creature, Estinien Varlineau's
		// Card.Self,Dragon) resolve through the same code.
		if n, ok2 := hasPropertyStateBacked(h, g, c, opponentGroup(g, c), rest, arg); ok2 {
			return n, true, true
		}
		av, aok := playerCountDefinedRegistered(h, g, c, opponentGroup(g, c), rest, arg)
		return av, aok, true
	}
	if head == "OppGreatestLifeTotal" {
		av, aok := lifeExtreme(g, opponentGroup(g, c), "HighestLifeTotal")
		return av, aok, true
	}
	if rest, ok := strings.CutPrefix(head, "PlayerCountOpponents$"); ok {
		if n, ok2 := playerGroupCount(opponentGroup(g, c), rest); ok2 {
			return n, true, true
		}
		if n, ok2 := lifeExtreme(g, opponentGroup(g, c), rest); ok2 {
			return n, true, true
		}
		if n, ok2 := hasPropertyLostLifeCount(h, opponentGroup(g, c), rest); ok2 {
			return n, true, true
		}
		if rest == "HasPropertycontrolsCreature.powerGE4" && arg == "" {
			// Yojimbo's chapter IV counts opponents, not creatures. Read the
			// battlefield and derived power (including continuous effects).
			seen := make(map[state.PlayerID]bool)
			for i := range g.Objs {
				o := &g.Objs[i]
				if o.Zone == state.ZBattlefield && o.Controller != c.Controller &&
					h.IsCreature(o.ID) && h.Power(o.ID) >= 4 {
					seen[o.Controller] = true
				}
			}
			var n int32
			for _, p := range opponentGroup(g, c) {
				if seen[p] {
					n++
				}
			}
			return n, true, true
		}
		if n, ok2 := hasPropertyStateBacked(h, g, c, opponentGroup(g, c), rest, arg); ok2 {
			return n, true, true
		}
		if n, ok2 := playerCountCondition(h, g, c, opponentGroup(g, c), rest, arg); ok2 {
			return n, true, true
		}
		av, aok := playerCountExtreme(h, g, c, opponentGroup(g, c), rest, arg)
		return av, aok, true
	}
	// PlayerCountHasLost$<Property> — Forge's group of players who have LOST
	// the game (CR 104.2-3; a concession counts). `Amount` counts them, so
	// Hot Pursuit's `CheckSVar$ PlayerCountHasLost$Amount | SVarCompare$
	// GE2` is "if two or more players have lost the game". Read off the live
	// seat set (state.Player.Lost), which events.Apply's PlayerLost fold sets
	// and Clone copies, so a replay derives the same count. Any other
	// property fails closed: the head has no other corpus reader (measured:
	// only hot_pursuit and rampant_frogantua carry it).
	if rest, ok := strings.CutPrefix(head, "PlayerCountHasLost$"); ok {
		// The /Op count suffix applies like every other property head's
		// (Rampant Frogantua's `PlayerCountHasLost$Amount/Times.10` — its
		// +10/+10-per-lost-player SVar). Split before the name check so the
		// suffix does not make the exact-name compare miss.
		name, op, hasOp := strings.Cut(rest, "/")
		if strings.TrimSpace(name) == "Amount" {
			var n int32
			for i := range g.Players {
				if g.Players[i].Lost {
					n++
				}
			}
			if hasOp {
				n = applyCountOp(n, op)
			}
			return n, true, true
		}
		return 0, false, true
	}
	// PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$<Property> —
	// Forge's per-player property group whose "defined player" is the player
	// the property is being evaluated FOR (the relative-player read). The
	// only corpus property is StartingLife, with the /Op suffix
	// (Anya, Merciless Angel's SVar:Z and Game Over's SVar:Y both spell
	// `...StartingLife/HalfDown` — "half THEIR starting life total"). The
	// engine carries no PER-seat starting total, so the read is the one
	// game-wide opening total effects.Host.StartingLife reports; every corpus
	// carrier is a Constructed/Commander game where all seats open equal, so
	// the game-wide value IS each player's starting life. A caller that
	// evaluates this head with Controller = the member (playerCountCondition's
	// per-member SVar resolution, and the ordinary static/effect reads for a
	// self-targeting carrier) therefore gets the right member's threshold.
	if rest, ok := strings.CutPrefix(head, "PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$"); ok {
		av, aok := relativePlayerProperty(h, rest)
		return av, aok, true
	}
	// PlayerCountDefinedRegistered$<Property> — the group of registered
	// players. The two spellings are matched EXACTLY (never the wider
	// PlayerCountDefined prefix: the corpus's other PlayerCountDefined*
	// groups — DefinedRememberedOwner, DefinedNonTriggeredTarget,
	// DefinedActivePlayer, … — need referent machinery this head does not
	// build and stay fail-closed at the fallthrough below):
	//
	//   - PlayerCountDefinedRegistered$: every living player, the controller
	//     INCLUDED (Knight of the Ebon Legion and Y'shtola say "if a PLAYER
	//     lost 4 or more life this turn", not "an opponent"). No
	//     registered-membership list survives a replay, so the group reads as
	//     the same living set PlayerCountPlayers$ counts — the reading the
	//     RegisteredOpponents$ arm above already documents.
	//   - PlayerCountDefinedRegistered.Other$: living players minus the
	//     resolving controller (Ludevic, Necro-Alchemist's "a player other
	//     than you lost life this turn").
	if rest, ok := strings.CutPrefix(head, "PlayerCountDefinedRegistered.Other$"); ok {
		if n, ok2 := playerGroupCount(opponentGroup(g, c), rest); ok2 {
			return n, true, true
		}
		if n, ok2 := playerCountCondition(h, g, c, opponentGroup(g, c), rest, arg); ok2 {
			return n, true, true
		}
		av, aok := playerCountDefinedRegistered(h, g, c, opponentGroup(g, c), rest, arg)
		return av, aok, true
	}
	if rest, ok := strings.CutPrefix(head, "PlayerCountDefinedRegistered$"); ok {
		if n, ok2 := playerGroupCount(g.AliveFrom(0), rest); ok2 {
			return n, true, true
		}
		if n, ok2 := playerCountCondition(h, g, c, g.AliveFrom(0), rest, arg); ok2 {
			return n, true, true
		}
		av, aok := playerCountDefinedRegistered(h, g, c, g.AliveFrom(0), rest, arg)
		return av, aok, true
	}

	// PlayerCountPropertyYou$<Property> — the supported members of Forge's
	// PlayerCountProperty<group>$<Property> family. HasPropertyActive reads 1
	// when the resolving controller is active, else 0 — Starting Town's
	// ETB gate reads SVar:Y:PlayerCountPropertyYou$HasPropertyActive and
	// feeds Count$Compare Y GE1.Z.4, so X is YourTurns on your turn and 4
	// off it, tapped only when X > 3. The per-turn properties below read the
	// existing replay-derived host tallies or the event-backed LandsPlayed
	// state. Unsupported properties and all other group spellings retain the
	// fail-closed unresolvable verdict.
	if rest, ok := strings.CutPrefix(head, "PlayerCountPropertyYou$"); ok {
		switch strings.TrimSpace(rest) {
		case "HasPropertyActive":
			if c.Controller == g.Active {
				return 1, true, true
			}
			return 0, true, true
		case "CardsDiscardedThisTurn":
			// The log-derived discard count (trigcost2): how many cards the
			// RESOLVING controller discarded this turn — every
			// events.IsDiscard move naming p since the last TurnChange, the
			// cost form included (Ambergris Citadel Agent's
			// "SVar:X:PlayerCountPropertyYou$CardsDiscardedThisTurn" behind a
			// Cost$ Discard<1/Hand> Draw<2/You> body reads the paid discard).
			// Derived from the event log like LifeLostThisTurn, so a replay
			// derives the same number. The OTHER group spellings of the same
			// property (PlayerCountPlayers$/Opponents$/TargetedPlayer$) keep
			// the fail-closed verdict below — no group machinery here prices
			// them, and a fake zero is worse.
			return h.CardsDiscardedThisTurn(c.Controller), true, true
		case "LifeLostThisTurn":
			return h.LifeLostThisTurn(c.Controller), true, true
		case "LandsPlayed":
			if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
				return 0, false, true
			}
			return g.Players[c.Controller].LandsPlayed, true, true
		case "RingTemptedYou":
			// The resolving controller's own "the Ring has tempted you" count
			// (CR 701.54a, folded by events.Apply's RingTemptsYou case): what
			// Frodo, Adventurous Hobbit / Frodo, Sauron's Bane's
			// ConditionCheckSVar$ NumRingTempted reads (GE2 / GE4 level-ability
			// gates). The raw count is never capped, so a gate compares, and
			// a zero means "not yet tempted" — a real read, never a fake one.
			return g.Players[c.Controller].RingTempted, true, true
		}
		return 0, false, true
	}

	// ThisTurnCast_<spec> is handled ABOVE the head/space split — a spec
	// can carry a space; see the comment at the top of this function.

	// StartingPlayer.<yes>.<no> is Forge's two-branch opening designation
	// count. Desert Cenote's StartingPlayer.0.1 feeds LT1, so only the
	// starting player gets its tapped-entry replacement; the other corpus
	// cards use different numeric branches. Parse the grammar rather than a
	// card-specific literal so every branch pair follows the current, replayed
	// designation (including an opening effect that changes it).
	if branches, ok := strings.CutPrefix(head, "StartingPlayer."); ok {
		return dotBranch(h, c, branches, g.IsStartingPlayer(c.Controller), depth), true, true
	}
	return 0, false, false
}
