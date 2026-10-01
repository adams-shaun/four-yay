package rules

import (
	"github.com/adams-shaun/gorge/effects"
)

// engineTurnLedger groups the Engine's per-turn ledger state. It is
// embedded by value in Engine (rules/engine_struct.go), so every field
// keeps its documented contract comment and every existing e.<field>
// access keeps compiling unchanged through Go's field promotion. Clone's
// per-field copy classes (rules/clone.go) are unchanged by the move.
type engineTurnLedger struct {
	// turnsTaken caches the TurnChange census used by Count$TurnsThisGame.
	// turnsTakenEpoch is the log length represented by the cache; emit advances
	// both together, while an Engine assembled around an existing log lazily
	// rebuilds on its first query.
	turnsTaken      []int32
	turnsTakenEpoch int

	// turnStartTurns caches, per player, the sorted turn numbers at which that
	// player's turn began (rules.turnStartsFor). delayedRegistrationLive reads
	// it for the next-turn lifetime boundary; turnStartEpoch is the log length
	// it represents, so an Engine assembled around an existing log lazily
	// rebuilds it once rather than rescanning the log per registration. Clone
	// copies it like turnsTaken.
	turnStartTurns [][]int32
	turnStartEpoch int

	// combatHitsThisTurn is the per-turn combat-damage-to-players ledger
	// captured at the combat-damage site (rules/combat.go's
	// runCombatAssignments). It is engine-side, NO-EVENT state -- deliberately
	// not a new events.Kind, which would move every chain head and diverge
	// every STORED log at its first combat assignment. Every rebuild path
	// (replay, undo, DVR, restart) re-executes the engine and so re-derives
	// the same slice, and emit clears it on TurnChange (the turnsTaken
	// cache-advance site below). It carries only damage that LANDED and only
	// damage to a PLAYER; the object branch of runCombatAssignments records
	// nothing. See effects.Host's CombatDamageToPlayersThisTurn.
	combatHitsThisTurn  []effects.CombatDamageHit
	counterAddsThisTurn []counterAddedThisTurn
	// activationsThisTurn and crimeSeatsThisTurn are two more per-turn
	// NO-EVENT ledgers of the same kind (rules/turn_ledgers.go): this turn's
	// activated-ability stack objects with the targets they chose, and the
	// seats that committed a crime (CR 700.13). Re-derived by every rebuild,
	// cleared on TurnChange, copied by Clone.
	activationsThisTurn []activationThisTurn
	crimeSeatsThisTurn  uint64
	bendSeatsThisTurn   [64]uint8
}
