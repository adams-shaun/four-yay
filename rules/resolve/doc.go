// Package resolve is the L6 resolution kernel of the rules engine (lasagna
// spec docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md §7,
// W3; spike results 2026-10-03-s3-spike-results.md): checkpoint at the intent
// boundary that begins a resolution, and answer every decision posed while
// that resolution is in progress by re-executing it from the checkpoint with
// the recorded intents as an answer tape.
//
// The engine is deterministic and event-sourced, and a clone is exact at an
// intent boundary. The kernel builds on exactly that:
//
//   - The priority pass that begins a resolution (the last pass over a
//     non-empty stack) checkpoints the engine first: S0, an ordinary clone,
//     immutable afterwards and shared by pointer with every clone of a posed
//     engine. The ask-free predicate (Board.MayAsk) lets a resolution that
//     provably cannot ask skip the checkpoint; it is a performance hint, and
//     a miss is caught at the ask choke point (OnAsk).
//   - The tape is the intent log from the checkpoint on: the pass, then one
//     intent per answered ask. Nothing else crosses a suspension.
//   - A converted ask site asks through Answer. Inside a tape run it poses
//     the decision exactly as the engine's ask does (the same DecisionAsk),
//     then either serves the next tape intent (the same validation, intent
//     record and DecisionMade a Submit makes) and returns the chosen options
//     to the asking code, which simply continues, or -- the tape exhausted --
//     unwinds the run with a sentinel panic, leaving the decision posed.
//   - Submitting the answer to a posed tape resolution restores S0 into the
//     live engine in place and re-executes the pass and every answer since.
//     The event log's verify window (events.Log.RewindTo) checks every
//     re-executed event of the recorded prefix byte for byte.
//   - An ask site that is NOT converted (it reaches the engine's ask
//     directly) ends the tape run: before any tape ask it simply switches
//     this resolution to the legacy path in place (nothing has been answered
//     from the tape, so the state is the legacy path's); after one, the
//     re-run aborts, restores S0 and replays the tape through the legacy
//     path (the run-time fallback of spec §7.7).
//   - An engine whose every seat is a policy may install a synchronous
//     Answerer instead: a converted ask is then posed and answered inline,
//     and no resolution needs a checkpoint at all (S3b candidate 3).
//
// The kernel holds no engine pointer: it drives the engine through Board,
// which package rules implements over its Engine (rules/resolve_board.go).
// No state is mutated outside events.Apply: the kernel only checkpoints,
// restores, and runs the ordinary engine code.
//
// The kernel is the default; rules.Config.LegacyResume (or
// GORGE_TAPE_KERNEL=0) opts an engine out onto the legacy resume machinery,
// which is also the kernel's run-time fallback for an ask site it cannot
// serve. Park-and-continue asks are answered in place on the kernel (spec
// §7.2), so the two paths differ exactly there.
package resolve
