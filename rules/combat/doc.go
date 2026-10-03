// Package combat is attack and block legality: the CR 508.1/509.1
// declaration predicates (CanAttack, CanAttackPair, CanBlock) and the
// requirement and restriction evaluation they depend on -- the CR 508.1d
// "attacks if able" requirement set (AttackRequirements), goad (CR
// 701.38b), the CantAttack/CantBlock/CantBlockBy/CanAttackDefender
// restriction walks (AttackBlocked, BlockRestricted,
// AttackAllowedThroughDefender), the textual hidden-keyword grants, the
// AttackRestrict and MinMaxBlocker count bounds and the MustBlock duty.
//
// It is an L5 subsystem package of the rules-engine lasagna (spec
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md §3, W5
// step E5). It never holds a *rules.Engine: every predicate reads the game
// through Board, a narrow read-only interface package rules implements
// (rules/combat_board.go). Nothing here emits an event or writes game state;
// the declaration flow -- the KAttackers/KBlockers asks, their validators,
// the attack and block charges and the events -- stays in package rules,
// which calls these predicates.
//
// internal/archtest TestCombatImportsStayBelowRules pins this package's
// import set (it never imports rules or a sibling subsystem package), and
// TestCombatBoardOnlyShrinks ratchets Board's method count.
package combat
