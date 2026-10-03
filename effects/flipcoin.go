package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("FlipCoin", effFlipCoin)
}

// FlipNotePrefix is the canonical coin-flip result Note's text prefix. ONE
// shared encoding serves every flip: api:FlipCoin (effFlipCoin below) and the
// cumulative-upkeep FlipCoin cost action (rules/cumulative.go, which calls
// FlipCoinNote) both emit it, and the trig:FlippedCoin matcher
// (rules/trigmatch/actions.go's flippedCoinMatches) reads it — so a cost-side flip
// fires "whenever you win/lose a coin flip" exactly like an effect-side one
// (Karplusan Minotaur is the corpus card whose only missing primitive was the
// trigger). The Note is a replayable event: the random draw is the engine's
// seeded Rand, and a replay folds the Note without re-flipping, the same
// shape RollDice's per-roll Notes use.
const FlipNotePrefix = "flips a coin: "

// FlipCoinNote builds the canonical coin-flip result event: Player is the
// flipper, Obj the flipping source, Amount 1 = win/heads and 0 = lose/tails
// (Forge treats heads as win and tails as lose, including every NoCall$ line),
// and Text names the outcome for the transcript and the view.
func FlipCoinNote(source state.ObjID, flipper state.PlayerID, win bool) events.Event {
	if win {
		return events.Event{Kind: events.Note, Player: flipper, Obj: source,
			Amount: 1, Text: FlipNotePrefix + "heads"}
	}
	return events.Event{Kind: events.Note, Player: flipper, Obj: source,
		Amount: 0, Text: FlipNotePrefix + "tails"}
}

// FlipNoteResult reports whether ev is a canonical coin-flip result Note and
// which side it landed: the flipper (ev.Player), the win flag (ev.Amount == 1).
// Shared by the FlippedCoin trigger matcher; a non-flip Note (any other text)
// is not one.
func FlipNoteResult(ev events.Event) (flipper state.PlayerID, win bool, ok bool) {
	if ev.Kind != events.Note || !strings.HasPrefix(ev.Text, FlipNotePrefix) {
		return 0, false, false
	}
	return ev.Player, ev.Amount == 1, true
}

// flipperPlayers resolves the players who flip, in one shared resolver for
// both spellings the corpus uses: Defined$ (Mana Crypt's "Defined$ You") and
// Flipper$ ("that player flips" selectors — Remembered for the RepeatEach
// iteration subject, TriggeredActivator for a cast/tap trigger's actor,
// Player for "each player"). Everything resolves through
// knownDefinedTargets — the ONE defined-target resolver — so a Flipper$
// spelling can never disagree with the same spelling under Defined$. A
// present-but-unresolvable spec names nobody: ok=false, fail closed.
func flipperPlayers(h Host, c *Ctx, sa *cards.SA) (players []state.Target, named bool) {
	spec := strings.TrimSpace(sa.Params["Flipper"])
	if spec == "" {
		spec = strings.TrimSpace(sa.ParamStr(cards.PKDefined))
	}
	if spec == "" {
		return nil, false
	}
	ts, ok := knownDefinedTargets(h, c, spec)
	if !ok {
		return nil, true
	}
	return playersOf(ts), true
}

// flipRememberKind normalises a RememberNumber$ value to the sided tally it
// names: "Wins"/"Losses" (case-insensitive). ok=false for a value this build
// does not model, so the caller can be loud about it rather than silently
// counting the wrong side.
func flipRememberKind(v string) (string, bool) {
	switch {
	case strings.EqualFold(v, "Wins"):
		return "Wins", true
	case strings.EqualFold(v, "Losses"):
		return "Losses", true
	default:
		return "", false
	}
}

// flipRecord appends one flip to the resolution's flip memory (Ctx.FlipMemory,
// allocated lazily) and publishes the per-flip Wins/Losses SVars plus the
// cumulative RememberNumber$ tally. The memory is a shared pointer mutated in
// place, so a Ctx copy (a RepeatEach iteration, a resume rebuild) and the
// pointer rules' Ask captured onto a pending resume point all see the flip.
// player is the flipper and win the outcome (heads = win); rememberResult
// controls the separate RememberResult$ result list, while rememberKind is the
// normalised RememberNumber$ side (empty means no number was remembered).
func flipRecord(h Host, c *Ctx, player state.PlayerID, win, rememberResult bool, rememberKind string) {
	m := c.FlipMemory
	if m == nil {
		m = &FlipMemory{}
		c.FlipMemory = m
		// Re-publish through the optional seam so an ask later in this chain
		// captures the allocated pointer onto its resume point.
		if fh, ok := h.(flipMemoryHost); ok {
			fh.SetResolutionFlipMemory(m)
		}
	}
	if rememberResult {
		m.Results = append(m.Results, FlipResult{Player: player, Heads: win})
	}
	m.Set = true
	m.CurWin, m.CurLoss = 0, 0
	if win {
		m.CurWin = 1
	} else {
		m.CurLoss = 1
	}
	if rememberKind != "" {
		m.RememberNumberKind = rememberKind
		if (rememberKind == "Wins") == win {
			m.RememberNumber++
		}
	}
}

// forEachPlayerFlippers resolves a FlipCoin ForEachPlayer$ group to the
// players who each flip once. "Opponent" (Mutalith Vortex Beast) is every
// living opponent of the resolving controller; "True"/"Player"/"All" is
// every living player; any other value is resolved through the shared
// knownDefinedTargets resolver, and a present-but-unresolvable spec names
// nobody (ok=false, fail closed). Every group enumerates in the SAME
// controller-relative AliveFrom(c.Controller) order the shared defined
// resolvers use (definedSpec's "Opponent" and "Player" cases), so a
// ForEachPlayer$ spelling can never disagree with the same spelling under
// Defined$ — in a 3+ seat game whose controller is not seat 0, the flips (and
// the deterministic RNG outcomes bound to them) follow the controller's turn
// order, not seat number order.
func forEachPlayerFlippers(h Host, c *Ctx, spec string) ([]state.PlayerID, bool) {
	g := h.Game()
	switch strings.ToLower(strings.TrimSpace(spec)) {
	case "opponent", "opponents":
		var out []state.PlayerID
		for _, p := range g.AliveFrom(c.Controller) {
			if p != c.Controller {
				out = append(out, p)
			}
		}
		return out, true
	case "true", "player", "players", "all":
		return g.AliveFrom(c.Controller), true
	}
	ts, ok := knownDefinedTargets(h, c, spec)
	if !ok {
		return nil, false
	}
	var ids []state.PlayerID
	for _, t := range playersOf(ts) {
		if t.IsPlayer {
			ids = append(ids, t.Player)
		}
	}
	return ids, true
}

// effFlipCoin implements DB$/SP$/AB$ FlipCoin (82 corpus files): flip a coin
// through the engine's seeded generator (heads = win, tails = lose), record
// each result on the canonical Note (FlipCoinNote — the encoding the
// FlippedCoin trigger matcher reads), and resolve the outcome branch per
// flip. The branch names are resolved through ResolveSVar exactly as
// RollDice's ResultSubAbilities$ are: the base SA carries no auto-link to
// WinSubAbility$/LoseSubAbility$ (cards/link.go links only SubAbility$), so
// this primitive resolves the name itself; the base's own SubAbility$ chain
// (after the branches) is the enclosing Resolve loop's ordinary walk.
//
// Parameters read:
//   - WinSubAbility$ (heads) / LoseSubAbility$ (tails), and the NoCall$
//     lines' spellings HeadsSubAbility$ / TailsSubAbility$ — same heads/tails
//     mapping. A NoCall$ True line defers its branches until the flip loop is
//     complete. Branches that reference X run once with the total for their
//     side; branches independent of X run once per matching flip (Urza
//     Academy Headmaster's literal one-extra-turn grant per head).
//   - Amount$ (default 1): that many independent flips, each running its own
//     branch (TrigFlipCoins' "flip three coins" shape). An unresolvable value
//     degrades to one flip, the RollDice convention — a flip that happened
//     once is closer to "flip X coins" than a silent no-op.
//   - Defined$ / Flipper$: who flips, through one knownDefinedTargets
//     resolver (flipperPlayers). Absent, the resolving controller flips; a
//     present spec that resolves to no player (or an unmodelled selector)
//     flips nothing — the effLosesGame fail-closed convention.
//   - ForEachPlayer$ (one corpus line: Mutalith Vortex Beast's "flip a coin
//     for each opponent"): the flipper set is the named group — "Opponent"
//     is every living opponent of the resolving controller, resolved through
//     forEachPlayerFlippers. Each flipper flips once per iteration and the
//     chained sub sees Ctx.Remembered = that flipper, so the lose branch's
//     Defined$ Remembered deals "to that player".
//   - RememberResult$ True (Goblin Assassin, Mana Clash): every flip is
//     appended to Ctx.FlipMemory.Results, so a chained Defined$ FlippedHeads
//     / FlippedTails (and ValidPlayers$ of the same spelling) resolves to the
//     real flippers of that side. Absent or False leaves that result list
//     empty; the independent per-flip Wins/Losses publication still occurs.
//   - RememberNumber$ Wins/Losses (Goblin Traprunner, Yusri): the sided tally
//     is published (Ctx.FlipWins/FlipLosses) for a chained bare "Wins"/"Losses"
//     parameter and for Count$RememberedNumber. An unrecognised value is loud
//     and counts wins, matching the Wins default.
//   - RememberLoser$ True (Unleash the Flux): the losers are remembered as
//     player targets (Ctx.Remembered).
//   - FlipUntilYouLose$ True (5 corpus lines: Okaun, Zndrsplt, Toothy and
//     Zndrsplt, Crazed Firecat, Mirror March): flip until the first tails,
//     running the win branch per winning flip and the lose branch once on
//     the losing flip. A win branch that suspends on an ask does NOT abandon
//     the loop: the remaining cursor rides SuspendFlipRest and the host
//     re-enters this primitive once the answered ask's chain completes.
func abilityReferencesX(sa *cards.SA) bool {
	for cur := sa; cur != nil; cur = cur.Sub {
		for _, value := range cur.Params {
			for _, token := range strings.FieldsFunc(value, func(r rune) bool {
				return !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
					(r >= '0' && r <= '9') || r == '_')
			}) {
				if token == "X" {
					return true
				}
			}
		}
	}
	return false
}

func effFlipCoin(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	untilLose := strings.EqualFold(sa.Params["FlipUntilYouLose"], "True")
	winName := strings.TrimSpace(sa.Params["WinSubAbility"])
	if winName == "" {
		winName = strings.TrimSpace(sa.Params["HeadsSubAbility"])
	}
	loseName := strings.TrimSpace(sa.Params["LoseSubAbility"])
	if loseName == "" {
		loseName = strings.TrimSpace(sa.Params["TailsSubAbility"])
	}
	rememberLoser := strings.EqualFold(sa.Params["RememberLoser"], "True")
	rememberResult := strings.EqualFold(sa.Params["RememberResult"], "True")
	forEach := strings.TrimSpace(sa.Params["ForEachPlayer"]) != ""
	rememberKind := ""
	if raw := strings.TrimSpace(sa.Params["RememberNumber"]); raw != "" {
		if k, ok := flipRememberKind(raw); ok {
			rememberKind = k
		} else {
			rememberKind = "Wins"
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "FlipCoin RememberNumber$ " + raw + " unread: counting wins"})
		}
	}

	// Resume cursor (fx-style scoping): consumed and cleared here, so a
	// nested FlipCoin poses its own loop.
	rest := c.FlipRest
	c.FlipRest = nil

	var players []state.PlayerID
	var playerIndex int
	var iter int32
	amount := int32(1)
	if rest != nil {
		players, playerIndex, iter = rest.Players, rest.PlayerIndex, rest.Iter
		amount, untilLose = rest.Amount, rest.UntilLose
	} else {
		// X for a coin-flip resolution: the per-flip outcome branches see the
		// heads tally so far (reset here, one win each). A NoCall$ True branch
		// ignores this running tally — its deferred call below sets X to the
		// side's final total before it fires.
		c.X = 0
		if spec := strings.TrimSpace(sa.Params["ForEachPlayer"]); forEach {
			ps, ok := forEachPlayerFlippers(h, c, spec)
			if !ok {
				return // present but unresolvable: fail closed, nobody flips
			}
			players = ps
		} else {
			flippers, named := flipperPlayers(h, c, sa)
			if len(flippers) == 0 {
				if named {
					return
				}
				flippers = []state.Target{{Player: c.Controller, IsPlayer: true}}
			}
			for _, t := range flippers {
				if t.IsPlayer {
					players = append(players, t.Player)
				}
			}
		}
		amount = Num(h, c, sa, "Amount", 1)
		if amount < 1 {
			amount = 1
		}
	}

	wins, losses := int32(0), int32(0)
	noCall := strings.EqualFold(sa.Params["NoCall"], "True")
	for pi := playerIndex; pi < len(players); pi++ {
		p := players[pi]
		if int(p) < 0 || int(p) >= len(g.Players) || g.Players[p].Lost {
			continue
		}
		start := int32(0)
		if pi == playerIndex {
			start = iter
		}
		for i := start; untilLose || i < amount; i++ {
			win := h.Rand(2) == 0
			h.Emit(FlipCoinNote(c.Source, p, win))
			flipRecord(h, c, p, win, rememberResult, rememberKind)
			if win {
				c.X++
				wins++
			} else {
				losses++
			}
			if forEach || rememberLoser {
				// The per-player loop binds the current flipper for the chained
				// sub; RememberLoser$ remembers only the losing flipper, so a win
				// with no per-player loop leaves Remembered untouched.
				if forEach || !win {
					c.Remembered = []state.Target{{Player: p, IsPlayer: true}}
				}
			}
			name := loseName
			if win {
				name = winName
			}
			if !noCall && name != "" && c.SVars != nil {
				Resolve(h, c, cards.ResolveSVar(c.SVars, name))
				if h.Suspended() {
					// Only a suspension with flips still owed needs a cursor. A
					// losing flip of an until-lose loop ends it, and the last
					// iteration of an Amount$ loop is the last; if no later
					// flipper remains, there is nothing to resume and the host
					// gets no frame (the pre-existing shape for a terminal
					// suspension).
					moreIter := (untilLose && win) || (!untilLose && i+1 < amount)
					nextPlayer, nextIter := pi, i+1
					if untilLose && !win {
						// A losing until-lose flip finishes this player's loop. If
						// its lose branch asked, resume at the NEXT ForEachPlayer$
						// flipper, not at this player with Iter+1 (untilLose would
						// otherwise ignore that bound and flip the loser again).
						nextPlayer, nextIter = pi+1, 0
					}
					if moreIter || pi+1 < len(players) {
						h.SuspendFlipRest(sa, FlipRest{
							Players:     append([]state.PlayerID(nil), players...),
							PlayerIndex: nextPlayer,
							Iter:        nextIter,
							Amount:      amount,
							UntilLose:   untilLose,
						})
					}
					return
				}
			}
			if untilLose && !win {
				break
			}
		}
	}
	// NoCall$ True defers its outcome branches until all flips are known.
	// X-dependent branches need one call with the final tally (e.g. Ral
	// Zarek's NumTurns$ X); branches independent of X are still per-outcome
	// effects (e.g. Urza Academy Headmaster grants one extra turn per head).
	// A suspended branch resumes as an ordinary chained resolution: all flips
	// already happened, so no FlipRest cursor is owed.
	if noCall && c.SVars != nil {
		resolveOutcome := func(name string, count int32) bool {
			if name == "" || count == 0 {
				return false
			}
			branch := cards.ResolveSVar(c.SVars, name)
			if branch == nil {
				return false
			}
			calls := count
			if abilityReferencesX(branch) {
				calls = 1
				c.X = count
			}
			for i := int32(0); i < calls; i++ {
				Resolve(h, c, branch)
				if h.Suspended() {
					return true
				}
			}
			return false
		}
		if resolveOutcome(winName, wins) {
			return
		}
		if resolveOutcome(loseName, losses) {
			return
		}
	}
}
