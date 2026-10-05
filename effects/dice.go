package effects

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("AddTurn", effAddTurn)
	Register("SkipTurn", effSkipTurn)
	Register("LosesGame", effLosesGame)
	Register("WinsGame", effWinsGame)
	Register("RollDice", effRollDice)
}

// DieRollNotePrefix is the canonical per-die roll Note's text prefix. ONE
// shared encoding serves every roll: effRollDice below is the only emitter,
// and the trig:RolledDie matcher (rules/trigmatch/actions.go's rolledDieMatches)
// reads it -- so a die rolled by any DB$ RollDice fires "whenever you roll a
// die / roll a 4 or higher" exactly like the roll itself. The Note is a
// replayable event: the random draw is the engine's seeded Rand, and a replay
// folds the Note without re-rolling (the same shape api:FlipCoin's
// FlipCoinNote serves trig:FlippedCoin, and the api:RollDice CAUSE Note
// effRollDice's own range-table code reads).
const DieRollNotePrefix = "rolls a d"

// DieRollNote builds the canonical per-die roll event. Player is the
// roller, Obj the rolling source, Amount the MODIFIED result (die + Modifier$)
// -- the value every roll reader, ValidResult$ included, means by "the
// result". Pairs[0] carries [sides, natural] so a trigger can scope on
// ValidSides$ and Natural$ without parsing Text (Text is the transcript,
// kept byte-identical to the pre-trigger format). A non-zero Amount is a
// real roll; the Note is emitted once per die, so a multi-die roll fires a
// per-die trigger once per die (Forge's own RolledDie cadence, which is what
// Natural$ True and Number$ 3 assume).
func DieRollNote(source state.ObjID, roller state.PlayerID, sides, natural, result int32) events.Event {
	text := DieRollNotePrefix + strconv.FormatInt(int64(sides), 10) + ": " + strconv.FormatInt(int64(natural), 10)
	if result != natural {
		text += " + " + strconv.FormatInt(int64(result-natural), 10) + " = " + strconv.FormatInt(int64(result), 10)
	}
	return events.Event{Kind: events.Note, Player: roller, Obj: source,
		Amount: result, Text: text,
		Pairs: [][2]state.ObjID{{state.ObjID(sides), state.ObjID(natural)}}}
}

// DieRollResult decodes a canonical die-roll Note: the roller (ev.Player),
// the modified result (ev.Amount), the die's sides and the unmodified natural
// roll (ev.Pairs[0]). ok is false for any other Note -- in particular the
// per-roll transcript Notes of another shape.
func DieRollResult(ev events.Event) (roller state.PlayerID, sides, natural, result int32, ok bool) {
	if ev.Kind != events.Note || !strings.HasPrefix(ev.Text, DieRollNotePrefix) || len(ev.Pairs) == 0 {
		return 0, 0, 0, 0, false
	}
	return ev.Player, int32(ev.Pairs[0][0]), int32(ev.Pairs[0][1]), ev.Amount, true
}

// DieRollBatchNotePrefix is the canonical BATCH roll Note's text prefix,
// emitted once per DB$ RollDice resolution after every per-die Note. ONE
// shared encoding serves every roll: effRollDice is the only emitter, and
// the trig:RolledDieOnce matcher (rules/trigmatch/actions.go's
// trigmatch.rolledDieOnceMatches) reads it -- so "whenever you roll one or more dice"
// fires exactly once per roll action regardless of how many dice it rolled,
// the cadence Forge's RolledDieOnce mode has and the per-die RolledDie mode
// does not. One Note per resolution is the batch boundary the per-die Notes
// do not carry, so no engine scratch latch or roll-resolution scope is
// needed to tell a three-die roll from three one-die rolls.
const DieRollBatchNotePrefix = "rolls dice: "

// DieRollBatchNote builds the canonical per-resolution roll event: Player is
// the roller, Obj the rolling source, Amount the batch's reported RESULT (the
// last die's modified result -- Forge's Result for a roll action; every
// corpus RolledDieOnce Result reader rolls exactly one die, so the last and
// the only die coincide), Pairs[0] carries [count, maxResult] so the Once
// matcher and the TriggerCountMax$Result head can scope on the batch without
// parsing Text. It is emitted exactly once per effRollDice resolution, after
// the per-die Notes and before ResultSubAbilities$ resolves.
func DieRollBatchNote(source state.ObjID, roller state.PlayerID, count, maxResult, result int32) events.Event {
	text := DieRollBatchNotePrefix + strconv.FormatInt(int64(count), 10) + " -> " + strconv.FormatInt(int64(result), 10)
	return events.Event{Kind: events.Note, Player: roller, Obj: source,
		Amount: result, Text: text,
		Pairs: [][2]state.ObjID{{state.ObjID(count), state.ObjID(maxResult)}}}
}

// DieRollBatchResult decodes a canonical batch roll Note: the roller
// (ev.Player), the number of dice rolled, the highest modified result in the
// batch (Pairs[0][1], the TriggerCountMax$Result head) and the batch result
// (ev.Amount, the last die's modified result). ok is false for any other Note
// -- in particular a per-die DieRollNote, which the Once matcher must not fire
// on.
func DieRollBatchResult(ev events.Event) (roller state.PlayerID, count, maxResult, result int32, ok bool) {
	if ev.Kind != events.Note || !strings.HasPrefix(ev.Text, DieRollBatchNotePrefix) || len(ev.Pairs) == 0 {
		return 0, 0, 0, 0, false
	}
	return ev.Player, int32(ev.Pairs[0][0]), int32(ev.Pairs[0][1]), ev.Amount, true
}

// effAddTurn implements the extra-turn primitive (CR 500.7; 43 corpus files).
// The grant is one events.ExtraTurn per call: Player is the granted seat,
// Amount the NumTurns$ value (default 1, through the ordinary Num SVar
// indirection). Defined$ names the granted player when present (Rise of the
// Eldrazi's "Defined$ You", Time Walk's targeted form through its
// ValidTgts$ targets); the default is the resolving ability's controller.
//
// The turn structure (rules/turn.go's advanceStep) is what consumes the
// count: at the cleanup step's end, a seat whose ExtraTurns count is positive
// repeats as the active player and emits the -1 consumption, instead of the
// ordinary next-alive-seat rotation. Final Fortune's "At the beginning of
// that turn's end step, you lose the game" rides the same event: the granting
// ability's ExtraTurnDelayedTrigger$/ExtraTurnDelayedTriggerExecute$ pair is
// forwarded on the ExtraTurn event (Counter/Obj), and events.Apply registers
// the delayed Mode$ Phase trigger with the extra turn's number as MinTurn, so
// it skips the granting turn's own end step and fires exactly once, in the
// granted turn. Alchemist's Gambit's "during that turn, damage can't be
// prevented" static is NOT modelled (the CantPreventDamage static mode is an
// unimplemented Effect static -- the Note its Effect body emits says so).
//
// effSkipTurn implements DB$ SkipTurn (13 corpus files): grant the player N
// skipped turns (CR 500.9 — a turn that passes with no untap, upkeep, draw
// or priority for that player). Amount is NumTurns$ (default 1); the bare X
// resolves to the FlipCoin resolution's heads tally, so Ral Zarek, Guest
// Lecturer's "skips their next X turns, where X is the number of heads"
// reads the count its NoCall$ True branch defers to (effects/flipcoin.go).
// Defined$ names the skipped player(s) — Ral Zarek's Targeted opponent; the
// default is the resolving controller (Eater of Days' "you skip your next
// turn"). The grant is one SkipTurn event folded into state.SkipTurns; the
// turn structure consumes one at each would-be turn boundary
// (rules/turn.go's beginTurn).
func effSkipTurn(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "NumTurns", 1)
	if n <= 0 {
		return
	}
	players := []state.PlayerID{c.Controller}
	if DefinedRefOf(sa).Set() {
		players = nil
		for _, target := range Defined(h, c, sa) {
			if target.IsPlayer {
				players = append(players, target.Player)
			}
		}
	}
	for _, player := range players {
		if int(player) >= 0 && int(player) < len(h.Game().Players) && !h.Game().Players[player].Lost {
			h.Emit(events.Event{Kind: events.SkipTurn, Player: player, Amount: n, Obj: c.Source})
		}
	}
}

func effAddTurn(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "NumTurns", 1)
	if n <= 0 {
		return
	}
	player := c.Controller
	if DefinedRefOf(sa).Raw != "" || TargetsOf(sa).Targeted() {
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				player = t.Player
				break
			}
		}
	}
	if int(player) < 0 || int(player) >= len(h.Game().Players) {
		return
	}
	text := "extra turn"
	if strings.EqualFold(sa.ParamStr(cards.PKSkipUntap), "True") {
		// Text is an encoded Event field. The canonical marker is folded by
		// events.Apply into the individual queued grant, so replay retains
		// this turn-specific rider without extending Event's fixed schema.
		text = events.ExtraTurnSkipUntapText
	}
	// ExtraTurnDelayedTrigger$ (Final Fortune's, Last Chance's and
	// Alchemist's Gambit's DelTrig SVar) names the delayed trigger the
	// granted turn registers. The Execute$ name already rides the event's
	// Counter; the trigger's Phase$ rides IDs[0] (the state.Step ordinal,
	// parsed through delayedTriggerSpec — the ONE shared definition-body
	// parser, which reads both the raw "Mode$ Phase | ..." shape and the
	// DB$ DelayedTrigger-headed one — so the two ends cannot disagree), and
	// events.Apply's registration consumes it instead of the hardcoded
	// end step Final Fortune's body happened to name. An unresolvable SVar
	// or an unparseable Phase$ degrades to the old end step (the body's
	// default), which is what every already-logged grant carries — and what
	// the raw-body reader used to degrade to for EVERY carrier before the
	// parse existed (parseSA cannot read a definition body, so the old
	// ResolveSVar path here never fired; Alchemist's Gambit's Upkeep
	// registration is the live shape the reader fixes).
	phase := state.StepEnd
	if name := strings.TrimSpace(sa.ParamStr(cards.PKExtraTurnDelayedTrigger)); name != "" {
		if p, _, ok := delayedTriggerSpec(c, name); ok {
			phase = p
		}
	}
	// NonBasicSpell$ True (Alchemist's Gambit's cleave leg): Forge marks the
	// alternate-cost cast as NONBASIC — the bracketed words are removed, and
	// rules that key on the basic cast (the "cast a spell" triggers that
	// inspect what was cast) see the reduced spell. This build applies no
	// rule that distinguishes a nonbasic cast yet, so the read documents the
	// marker with the Note the MayChooseTarget$ precedent gates.
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKNonBasicSpell)), "True") {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: player,
			Text: "cast nonbasic (cleave): bracketed words removed"})
	}
	h.Emit(events.Event{Kind: events.ExtraTurn, Player: player, Amount: n,
		Obj: c.Source, Counter: sa.ParamStr(cards.PKExtraTurnDelayedTriggerExecute), Text: text,
		IDs: []state.ObjID{state.ObjID(phase)}})
}

// effLosesGame implements DB$ LosesGame (53 corpus files): the Defined$
// player (the resolving ability's controller when the SA names NO Defined$ at
// all -- Final Fortune's "you lose the game") loses the game right now, CR
// 104.2a -- the same PlayerLost event a 0-life elimination emits, so
// state-based actions sweep their permanents and the game-over check runs as
// for any other loss. A seat that has already lost is skipped (losing twice
// is not a second game event).
//
// A PRESENT Defined$ that resolves to no player target acts on NOBODY: the
// controller default above is for the Defined$-less shape only. This is what
// makes the empty-set semantics of a state qualifier's miss (Triskaidekaphobia's
// Player.lifeEQ13 with no seat at 13 life) a no-op rather than killing the
// resolving controller, and it is a behaviour change for any unmodelled
// Defined$ spelling too -- those now fail closed here instead of falling
// back to the controller (the chosen-targets fallback never had a player for
// these lines anyway).
func effLosesGame(h Host, c *Ctx, sa *cards.SA) {
	player := c.Controller
	if DefinedRefOf(sa).Raw != "" {
		found := false
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				player, found = t.Player, true
				break
			}
		}
		if !found {
			return
		}
	}
	g := h.Game()
	if int(player) < 0 || int(player) >= len(g.Players) || g.Players[player].Lost {
		return
	}
	h.EmitPlayerLost(player, "Effect", "lost the game")
}

// effWinsGame implements DB$ WinsGame (40 corpus files): the Defined$ player
// (the resolving ability's controller when the SA names NO Defined$ at all)
// wins the game right now, CR 104.2a -- the GameOver win shape events.Apply's
// GameOver case folds (Amount 0 with a valid Player sets Over and Winner).
// The Text carries the winner's name, exactly as checkGameOver's own win does
// and view/describe.go renders for a GameOver event.
//
// The mirror of effLosesGame above: a PRESENT Defined$ that resolves to no
// player target acts on NOBODY (the controller default is for the
// Defined$-less shape only), and a seat that has already lost or a game that
// has already ended is skipped. WinsGame is the win side of the same
// CR 104.2a family and shares that fail-closed Defined$ resolution. The
// gating ConditionCheckSVar$ / ConditionPresent$ keys are consumed by the
// shared conditionMet gate ahead of the body, not here.
func effWinsGame(h Host, c *Ctx, sa *cards.SA) {
	player := c.Controller
	if DefinedRefOf(sa).Raw != "" {
		found := false
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				player, found = t.Player, true
				break
			}
		}
		if !found {
			return
		}
	}
	g := h.Game()
	if int(player) < 0 || int(player) >= len(g.Players) || g.Players[player].Lost || g.Over {
		return
	}
	h.EmitGameWin(player, g.Players[player].Name)
}

// parseDieRanges splits Forge's ResultSubAbilities$ value into its range
// entries: "1-6:DBMana1,7-14:DBMana2,15-20:DBMana3" or the single-value
// "1:M2,2:Fog,..." or an "Else:DBDiscard" catch-all. The order is the string's
// own order (never a map), so the matching below is deterministic.
type dieRange struct {
	lo, hi int32
	name   string
	isElse bool
}

func parseDieRanges(v string) []dieRange {
	var out []dieRange
	for part := range strings.SplitSeq(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		nameIdx := strings.IndexByte(part, ':')
		if nameIdx < 0 {
			continue
		}
		spec, name := strings.TrimSpace(part[:nameIdx]), strings.TrimSpace(part[nameIdx+1:])
		if strings.EqualFold(spec, "Else") {
			out = append(out, dieRange{name: name, isElse: true})
			continue
		}
		loS, hiS := spec, spec
		if dash := strings.IndexByte(spec, '-'); dash >= 0 {
			loS, hiS = spec[:dash], spec[dash+1:]
		}
		lo, err1 := strconv.ParseInt(strings.TrimSpace(loS), 10, 64)
		hi, err2 := strconv.ParseInt(strings.TrimSpace(hiS), 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, dieRange{lo: int32(lo), hi: int32(hi), name: name})
	}
	return out
}

// retainedDice applies RollDice's result-selection modifiers after every die
// has been rolled and recorded, but before ResultSubAbilities$ is evaluated.
// IgnoreLower$ drops that many lowest results; ties drop earlier dice first so
// the outcome is deterministic. UseHighestRoll$ then retains one highest
// result (the first rolled die when the high result ties). The latter is one
// result, rather than every tied high die: "ignore all but the highest roll"
// has one outcome table to apply, and result ranges do not attach actions to
// individual dice.
func retainedDice(dice []int32, ignoreLower int32, useHighest bool) []int32 {
	if len(dice) == 0 {
		return nil
	}
	if ignoreLower < 0 {
		ignoreLower = 0
	}
	if ignoreLower > int32(len(dice)) {
		ignoreLower = int32(len(dice))
	}
	type indexedResult struct {
		value int32
		index int
	}
	ordered := make([]indexedResult, len(dice))
	for i, value := range dice {
		ordered[i] = indexedResult{value: value, index: i}
	}
	// A stable value sort supplies the documented roll-order tie break without
	// relying on any map iteration.
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].value < ordered[j].value })
	dropped := make([]bool, len(dice))
	for _, result := range ordered[:ignoreLower] {
		dropped[result.index] = true
	}
	kept := make([]int32, 0, len(dice)-int(ignoreLower))
	for i, value := range dice {
		if !dropped[i] {
			kept = append(kept, value)
		}
	}
	if !useHighest || len(kept) < 2 {
		return kept
	}
	highest := kept[0]
	for _, value := range kept[1:] {
		if value > highest {
			highest = value
		}
	}
	return []int32{highest}
}

// effRollDice implements DB$ RollDice (131 corpus files, measured with
// /usr/bin/grep -rlE over .cards/cardsfolder): roll a die with
// Sides$ sides (default 6, the corpus's unmarked "roll a die"), through the
// engine's seeded generator -- the one randomness channel a replay can
// reproduce. The result is recorded as a Note (the transcript's die roll) and
// published for the chained SubAbility$ under the names the parameters
// designate. Every publication lands in Ctx.RollPubs (and the primary one
// mirrors into LastRoll/LastRollName), read through rollPublished by Num's
// bare-name fallback, evalCountExpr's SVar$ head and a bare-name body, and
// Ctx.SpecContext's numeric-RHS resolver:
//
//   - ResultSVar$: the "SVar$<name>" indirection. ONE die publishes that
//     die's own result (Velukan Dragon's "SVar:X:SVar$Result/Minus.1" between
//     rolls and any ConditionCheckSVar$); several dice publish the TOTAL of
//     their results, which is exactly how the corpus's multi-roll readers
//     read it -- Neverwinter Hydra "a number of +1/+1 counters on it equal to
//     the total of those results", Pair o' Dice Lost "total mana value X or
//     less ... where X is the total of those results" -- and Spark Fiend's
//     StoreSVar Expression$ Result reads the same total through the
//     bare-name body path. The r2 review's truncation is gone: multi-roll
//     resolutions publish.
//   - UseDifferenceBetweenRolls$ True (5 corpus lines, all two dice -- the
//     Ungencoded CrankContraption family, Boomflinger et al.): publishes
//     |die1 - die2| instead of the total, so "damage ... equal to the
//     difference between those results" (NumDmg$ Result) and
//     TokenAmount$/Amount$ Result read the difference.
//   - ChosenSVar$ / OtherSVar$ (5 corpus lines, the Endeavor cycle, all two
//     dice): the controller CHOOSES one of the rolled results -- a real
//     KChoose (Min == Max == 1, one "roll" option per die in roll order),
//     answered in place (ResumeKind "roll") with the per-die results carried
//     on the decision (decision.Decision.Rolls), publishing ChosenSVar$ = the
//     picked die's result and
//     OtherSVar$ = the unpicked one's (sums, so the shape generalises past
//     two dice) without re-rolling. A host that cannot ask (the fuzz
//     stand-in, R-9) and botpolicy's clamp fallback both keep the FIRST die
//     -- deterministic and non-wedging. A single-die ChosenSVar$ needs no
//     ask: that die is the chosen result.
//   - MaxRollsResults$ True / EvenOddResults$ True (Luck Bobblehead):
//     publish "MaxRolls" (the count of results equal to the die's maximum,
//     sides+modifier -- "If you rolled 6 exactly seven times") and
//     "EvenResults"/"OddResults" (the counts of even/odd results -- the
//     "create a Treasure token for each even result" sub reads
//     SVar$EvenResults through the ordinary SVar$ indirection).
//
// Amount$ (24 lines: "roll X dice" shapes) rolls that many dice, records a
// Note per roll, then runs the range table once per RETAINED result (each
// die's own result normally matches independently -- the reading "create a
// Treasure for each even result" needs). IgnoreLower$ (one corpus file,
// Berserker's Frenzy) drops the requested number of low rolls; UseHighestRoll$
// True (one corpus file, Iron Mastiff) retains just the high roll. Modifier$
// is added to every roll through the same Num/SVar evaluator
// that reads Sides$ and Amount$ (Wyll's Reversal and Danse Macabre's Y, Song
// of Inspiration's X); the modified result is what ranges match and what
// every publication totals. The unmodified die remains the only random draw.
func effRollDice(h Host, c *Ctx, sa *cards.SA) {
	sides := Num(h, c, sa, "Sides", 6)
	if sides <= 0 {
		sides = 6
	}
	amount := Num(h, c, sa, "Amount", 1)
	if amount < 1 {
		amount = 1
	}
	// IgnoreLower$ is read once here, not at the retainedDice call: the
	// R:Event$ RollDice replacement boundary below needs its value as the
	// held proposal's ignored-low base.
	ignoreLower := Num(h, c, sa, "IgnoreLower", 0)
	// R:Event$ RollDice replacement boundary (CR 614.4): before any die is
	// rolled the action is held out as a synthetic proposal (the Scry hook's
	// discipline -- never logged), and a matching replacement rewrites the
	// dice count and the ignored-low count in place. The proposal seeds the
	// ignored-low base with THIS body's own IgnoreLower$, so a replacement's
	// ReplaceCount$Ignore/Plus.1 is one ADDITIONAL low result; every roll
	// body seeds its own fresh proposal.
	proposal := h.CountReplacementProposed(events.Event{Kind: events.RollDice, Player: c.Controller, Obj: c.Source,
		Amount: amount, Counter: strconv.FormatInt(int64(ignoreLower), 10)})
	amount = proposal.Amount
	if n, err := strconv.Atoi(proposal.Counter); err == nil && n >= 0 {
		ignoreLower = int32(n)
	}
	modifier := Num(h, c, sa, "Modifier", 0)
	chosenName := strings.TrimSpace(sa.ParamStr(cards.PKChosenSVar))
	otherName := strings.TrimSpace(sa.ParamStr(cards.PKOtherSVar))

	publish := func(name string, v int32) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		c.Roll.Pubs = append(c.Roll.Pubs, RollPub{Name: name, Value: v})
		if name == "X" {
			c.X = v
		}
	}

	// publishChosen publishes an answered choose-one-result. The chosen options' Index values name the dice (into rolls, the
	// per-die results the asking pass carried on the decision) the player
	// picked; the chosen value is the sum of the picked dice's results, the
	// other value the sum of the rest -- for the corpus's two-die Endeavor
	// cycle that is exactly "one result" and "the other result".
	publishChosen := func(rolls []int32, pick []int) {
		chosenSum, otherSum, picked := int32(0), int32(0), 0
		isPicked := func(i int) bool {
			for _, idx := range pick {
				if idx == i {
					return true
				}
			}
			return false
		}
		for i, r := range rolls {
			if isPicked(i) {
				chosenSum += r
				picked++
			} else {
				otherSum += r
			}
		}
		if picked == 0 && len(rolls) > 0 {
			// A malformed or empty answer picks nothing: the first die stands
			// in (deterministically, the same die the R-9 and clamp fallbacks
			// keep) rather than publishing a chosen value no real choice
			// could produce.
			chosenSum, otherSum = rolls[0], 0
			for _, r := range rolls[1:] {
				otherSum += r
			}
		}
		publish(chosenName, chosenSum)
		publish(otherName, otherSum)
		if chosenName != "" {
			c.Roll.Last, c.Roll.LastName = chosenSum, chosenName
		}
	}
	// Roll the dice.
	dice := make([]int32, 0, amount)
	ranges := parseDieRanges(sa.ParamStr(cards.PKResultSubAbilities))
	for i := int32(0); i < amount; i++ {
		die := int32(h.Rand(int(sides))) + 1
		result := die + modifier
		dice = append(dice, result)
		h.Emit(DieRollNote(c.Source, c.Controller, sides, die, result))
	}
	// The batch boundary (trig:RolledDieOnce): ONE canonical Note per
	// resolution, carrying the batch's reported result (the last die's, Forge's
	// Result for a roll action) and the highest result in the batch (the
	// TriggerCountMax$Result head -- Farideh's "if any of those results was 10
	// or higher"). Emitted after every per-die Note and before
	// ResultSubAbilities$/the chosen-result ask, so a Once trigger queues at
	// the roll and resolves after the whole action, exactly as the per-die mode
	// does.
	batchMax := dice[0]
	for _, r := range dice[1:] {
		if r > batchMax {
			batchMax = r
		}
	}
	h.Emit(DieRollBatchNote(c.Source, c.Controller, int32(len(dice)), batchMax, dice[len(dice)-1]))

	// The RETAINED set: IgnoreLower$ / UseHighestRoll$ -- and a roll
	// replacement's rewritten Ignore -- drop low results before anything
	// downstream reads them. Every reader below (the range table, the
	// publications, the counts, the choose-one ask) consumes RETAINED dice;
	// the per-die Notes and the batch Note above remain the record of every
	// die ROLLED. With no ignore (every plain corpus roll) the retained set
	// is all dice, so this changes nothing for them.
	retained := retainedDice(dice, ignoreLower, strings.EqualFold(sa.ParamStr(cards.PKUseHighestRoll), "True"))

	// ResultSubAbilities$ is evaluated only after selection: both modifiers
	// name results, not dice to suppress rolling, so every die is still noted
	// and remains available to ResultSVar$/chosen-result publications.
	for _, result := range retained {
		if len(ranges) == 0 {
			continue
		}
		sub := ""
		for _, r := range ranges {
			if r.isElse {
				continue
			}
			if result >= r.lo && result <= r.hi {
				sub = r.name
				break
			}
		}
		if sub == "" {
			for _, r := range ranges {
				if r.isElse {
					sub = r.name
					break
				}
			}
		}
		if sub != "" && c.SVars != nil {
			Resolve(h, c, cards.ResolveSVar(c.SVars, sub))
			if h.Suspended() {
				return
			}
		}
	}

	// Primary result publication (ResultSVar$): one die's own result, the
	// total of several, or the two-die difference -- over the RETAINED dice,
	// so an ignored-low die's result does not count (the reading "ignore the
	// lowest roll" gives every consumer; with no ignore the retained set is
	// all dice, every existing publication unchanged).
	total := int32(0)
	for _, r := range retained {
		total += r
	}
	pub := total
	if strings.EqualFold(sa.ParamStr(cards.PKUseDifferenceBetweenRolls), "True") && len(retained) == 2 {
		pub = retained[0] - retained[1]
		if pub < 0 {
			pub = -pub
		}
	}
	resultName := strings.TrimSpace(sa.ParamStr(cards.PKResultSVar))
	if resultName != "" {
		c.Roll.Last, c.Roll.LastName = pub, resultName
		publish(resultName, pub)
	}
	// MaxRollsResults$ / EvenOddResults$ (Luck Bobblehead): the counts a
	// chained sub reads back through the published names.
	if sa.ParamStr(cards.PKMaxRollsResults) == "True" {
		maxResult := sides + modifier
		n := int32(0)
		for _, r := range retained {
			if r == maxResult {
				n++
			}
		}
		publish("MaxRolls", n)
	}
	if sa.ParamStr(cards.PKEvenOddResults) == "True" {
		even, odd := int32(0), int32(0)
		for _, r := range retained {
			if r%2 == 0 {
				even++
			} else {
				odd++
			}
		}
		publish("EvenResults", even)
		publish("OddResults", odd)
	}

	// The choose-one-result ask (ChosenSVar$/OtherSVar$, the Endeavor
	// cycle): one die of the rolled set becomes the chosen result, the rest
	// the other. Exactly one die is chosen (Min == Max == 1), one "roll"
	// option per die in roll order, answered in place (ResumeKind "roll")
	// with the per-die results carried on the decision.
	if chosenName != "" && len(retained) > 1 {
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
			Min:        1,
			Max:        1,
			Source:     c.Source,
			ResumeKind: "roll",
			ResumeSA:   sa,
			Rolls:      append([]int32(nil), retained...),
			Prompt:     "Choose one rolled result"}
		for i, r := range retained {
			d.Options = append(d.Options, decision.Option{Index: i,
				Kind: "roll", Label: "die " + strconv.Itoa(i+1) + ": result " + strconv.FormatInt(int64(r), 10),
				Player: c.Controller})
		}
		if ans, ok := AskTape(h, d); ok {
			// The "roll" answer in hand: the picked dice by option Index.
			pick := make([]int, 0, len(ans))
			for _, o := range ans {
				pick = append(pick, o.Index)
			}
			publishChosen(d.Rolls, pick)
			return
		}
		// Fuzz/no-engine host (R-9): the deterministic stand-in keeps the
		// first die -- the exact option botpolicy's clamp fallback takes.
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "keeps the first rolled result (no engine host to ask)"})
		other := int32(0)
		for _, r := range retained[1:] {
			other += r
		}
		publish(chosenName, retained[0])
		publish(otherName, other)
		c.Roll.Last, c.Roll.LastName = retained[0], chosenName
		return
	}
	if chosenName != "" && len(retained) == 1 {
		// A single-die choose is vacuous: that die is the chosen result; the
		// other value has no die to name and publishes nothing.
		publish(chosenName, retained[0])
		c.Roll.Last, c.Roll.LastName = retained[0], chosenName
	}
}

// runtimeSVar reports the value an api:StoreSVar write stored under name on
// the context's source object (Forge's Card.getSVar/sa.getSVar read of a
// runtime write). It is the HIGHEST-precedence SVar read: a runtime write
// replaces the printed face default of the same name, so a caller checks this
// BEFORE the printed c.SVars table (a caller that merely falls through to
// runtimePublished after c.SVars would never see a stored value whose name
// also has a printed body -- exactly LifePaidOnETB:Number$0). c.Host is bound
// by effects.Resolve for the whole walk; a test double with no Host, or a
// zero Source, reads nothing (fail closed, the same-as-before verdict).
func runtimeSVar(c *Ctx, name string) (int32, bool) {
	if c == nil || c.Host == nil || c.Source == 0 {
		return 0, false
	}
	o := c.Host.Game().Obj(c.Source)
	if o == nil {
		return 0, false
	}
	v, ok := o.RuntimeSVars[name]
	return v, ok
}

// runtimePublished reports the value a runtime SVar publication of this
// resolution made under name. Two producers publish here, both through the
// same three readers (Num's bare-name fallback, evalCountExpr's SVar$ head
// and a bare-name body, and Ctx.SpecContext's numeric-RHS resolver):
//
//   - DB$ RollDice: the primary ResultSVar$ slot (LastRollName/LastRoll) or
//     one of the Ctx.RollPubs entries (ChosenSVar$/OtherSVar$ and the
//     MaxRollsResults$/EvenOddResults$ counts).
//   - api:Vote's AmountFromVotes$ RepeatEach: the reserved name "Votes"
//     bound to the current loop subject's vote tally (Ctx.VotePublished,
//     the form of Forge's sa.setSVar("Votes", "Number$<n>")).
//
// ok=false on any resolution that published no such name -- the conservative
// same-as-before degrade every unmodelled head applies.
func runtimePublished(c *Ctx, name string) (int32, bool) {
	if c == nil {
		return 0, false
	}
	if v, ok := runtimeSVar(c, name); ok {
		return v, true
	}
	if c.Vote.PublishedSet && name == "Votes" {
		return c.Vote.Published, true
	}
	// A DB$ FlipCoin's per-flip Wins/Losses SVars (Forge's FlipCoinEffect: 1
	// to the side the current flip landed on, 0 to the other), read back by a
	// per-flip WinSubAbility$ (Goblin Traprunner's TokenAmount$ Wins, Crazed
	// Firecat's CounterNum$ Wins, Mirror March's NumCopies$ Wins, Mutalith's
	// NumCards$ Wins) and Yusri's SVar$Losses body.
	if c.FlipMemory != nil && c.FlipMemory.Set {
		switch diceRuntimePublishCodes.Code(string(name)) {
		case diceRuntimePublishWins:
			return c.FlipMemory.CurWin, true
		case diceRuntimePublishLosses:
			return c.FlipMemory.CurLoss, true
		}
	}
	if c.Roll.LastName != "" && c.Roll.LastName == name {
		return c.Roll.Last, true
	}
	for _, p := range c.Roll.Pubs {
		if p.Name == name {
			return p.Value, true
		}
	}
	return 0, false
}

// rollPublished is runtimePublished's DB$ RollDice spelling, kept for the
// callers and tests that name the roll producer explicitly. It is the SAME
// lookup -- a roll name must not be able to resolve differently from any
// other runtime publication -- so the two can never drift.
func rollPublished(c *Ctx, name string) (int32, bool) {
	return runtimePublished(c, name)
}

type diceRuntimePublishCode uint16

const (
	diceRuntimePublishWins diceRuntimePublishCode = iota + 1
	diceRuntimePublishLosses
)

var diceRuntimePublishCodes = state.NewStrCodes(
	state.StrEntry[diceRuntimePublishCode]{Key: "Wins", Val: diceRuntimePublishWins},
	state.StrEntry[diceRuntimePublishCode]{Key: "Losses", Val: diceRuntimePublishLosses},
)
