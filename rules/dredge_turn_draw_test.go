// Fix round (findings-sol4 MAJOR): the draw step's turn-based draw (CR 504.1)
// can SUSPEND on a Dredge replacement ask (CR 702.55) posed through the same
// DrawFor the Draw primitive shares. advanceStep must then return without
// emitting the step's own Priority event: CR 405.1 grants priority only after
// the turn-based action completes, and the dredge answer's resume path
// (resolution.go's dredge arm -> Advance -> priorityRound) grants that one
// priority itself. Emitting one while the ask is outstanding granted TWO, and
// put a Priority event in the log before the player had answered whether to
// replace the draw at all.
//
// The probe that broke this drove an ACTUAL turn draw (upkeep -> draw through
// the turn structure, not e.drawCard directly) with Golgari Thug in its
// controller's graveyard and failed on the pre-fix code with
// "priority emitted while dredge decision is pending". This test is that
// probe, committed.
package rules
