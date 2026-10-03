// Package trigmatch is the trigger-mode matching layer of the rules engine:
// the Mode$ registry (one matcher per trigger mode, registered from each
// mode's file) and the per-mode predicates that decide whether one compiled
// cards.Trigger fires for one events.Event.
//
// It is layer L5 of the rules-engine lasagna (spec
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md §3, W5
// step E3). A matcher never holds a *rules.Engine: it reads the game through
// Board, a narrow read-only view rules implements, and returns a verdict.
// Nothing in this package emits an event or writes engine state; every
// side effect of a trigger check -- queueing the instance, the batch and
// once-per-turn latches, the diagnostic Notes -- stays with the caller in
// package rules (checkTriggers / checkFaceTriggers / triggerMatches), which
// still routes every mutation through events.Apply.
//
// internal/archtest pins the import direction (this package never imports
// rules or a sibling L5/L6 package) and internal/codeshape ratchets Board's
// size, which may only shrink.
package trigmatch
