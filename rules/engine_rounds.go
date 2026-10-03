package rules

import (
	"github.com/adams-shaun/gorge/state"
)

// engineRounds groups the Engine's genesis, pregame and
// combat-continuation round state. It is embedded by value in Engine
// (rules/engine_struct.go), so every field keeps its documented contract
// comment and every existing e.<field> access keeps compiling unchanged
// through Go's field promotion. Clone's per-field copy classes
// (rules/clone.go) are unchanged by the move.
type engineRounds struct {
	// pregame is true while the London mulligan round runs, between the
	// opening deal and turn 1. startPostDealSetup sets it when
	// Config.Mulligans > 0 (at New for a plain constructor; for the
	// CR 103.1 choice constructor at the choice's resolution -- see
	// tossChoice); step() dispatches to stepPregame (rules/mulligan.go)
	// while it is true, and the round's end clears it and hands to
	// beginTurn. Bool field, so Clone copies it like every other value field.
	pregame bool `clone:"deep"`
	// coloring is true while the CR 903.4b commander colour-choice round runs,
	// BEFORE the London mulligan round (the choice is made "before the game
	// begins", and the mulligan round is also pregame). New sets it only when
	// a seat's commander carries the characteristic-defining chosen-colour
	// static; step() dispatches to stepColorRound (rules/commander_color.go)
	// while it is true, and the round's end opens the mulligan/opening round
	// exactly as if the colour round were absent. Bool field, so Clone copies
	// it like pregame does.
	coloring bool `clone:"deep"`
	// colorRound is the colour round's plain-value state (rules/
	// commander_color.go): one qualifying (seat, commander) ask per entry and
	// a cursor. Never a closure, so Clone copies it like the mulligan round.
	colorRound colorRound `clone:"share"`
	// tossChoice is CR 103.1's second half's plain-value state (rules/
	// starting_player_choice.go): the toss winner may still choose who takes
	// the first turn. Only a tossAsk constructor (NewStartingPlayerChoice)
	// sets it; plain New folds the resolved toss and runs startPostDealSetup
	// exactly as the pre-choice engine did. active marks that the pregame
	// rounds are still deferred until the choice is answered or defaulted.
	// Never a closure, so Clone copies it.
	tossChoice tossChoice `clone:"deep"`
	// mulligan is the round's plain-value state (rules/mulligan.go) -- seats,
	// kept/taken counts and the phase cursor. Never a closure, so Clone copies
	// it like cast/choosing.
	mulligan mulliganRound `clone:"deep"`
	// opening is the optional opening-hand effects round, after the London
	// mulligan round (a Gemstone Caverns may not be used from a hand its owner
	// later mulliganed away) and before turn one. It holds only object IDs and parsed SVar names, so replay and
	// Clone reproduce the same pregame choices without ambient state.
	// Impatient Iguana's accepted BecomeStartingPlayer$ Reveal resolves here
	// and folds the designation into state.Game through events.StartingPlayer
	// Change (effects/cardflow.go), so Count$StartingPlayer and the view's
	// pregame projection read it before turn one.
	opening openingRound `clone:"deep"`
	// blockerRound is the declare-blockers step's per-defender cursor
	// (rules/combat.go, Task m34): an attack may be split across several
	// defending players, and each declares its own blocks, one KBlockers
	// decision at a time. Plain-value state (a defender list plus an index),
	// never a closure, so Clone copies it like the mulligan round.
	blockerRound blockerRound `clone:"share"`

	// exertAskState is the declare-attackers exert election's resumable
	// state (rules/combat.go, task exert1): the deterministic offer list
	// (attacking creatures carrying an offerable stat:OptionalAttackCost
	// static, in declaration option order) plus the cursor of the ask
	// currently outstanding. Plain-value state, so Clone copies it like
	// blockerRound; a log-driven replay re-derives the same list when it
	// re-runs the recorded KAttackers answer through handleAttackers.
	exertAskState exertAsk `clone:"share"`

	// enlistAskState is the declare-attackers enlist election's resumable
	// state (rules/enlist.go, task enlist1): the answered KAttackers
	// declaration, the declaring player, the deterministic offer list
	// (attacking creatures with `K:Enlist` that have at least one eligible
	// creature to tap, in declaration option order) plus the cursor of the
	// ask currently outstanding. Plain value, so Clone copies it like
	// exertAskState; a log-driven replay re-derives the same list when it
	// re-runs the recorded KAttackers answer through handleAttackers.
	enlistAskState enlistAsk `clone:"share"`

	// stationing is the spacecraft a pending Station tap pick (rules/
	// station.go) belongs to: the "station" priority option's object, held
	// across the KChoose so the answer's charge counters land on the right
	// permanent. Plain value, so Clone copies it like blockerRound; zero
	// whenever no station ask is outstanding.
	stationing state.ObjID `clone:"deep"`

	// combatRound is the combat damage step's continuation state
	// (rules/combat.go, Task jj-cmb): which damage passes are done, and any
	// controller damage-division choices still being collected or awaiting an
	// answer (CR 510.1c multi-block division, CR 510.3/4 double-strike).
	// Plain-value data (slices plus scalars), never a closure, so Clone
	// copies it like blockerRound and a log-driven replay re-derives the same
	// branch. Zero whenever the combat damage step is not in progress.
	combatRound combatRound `clone:"deep"`
}
