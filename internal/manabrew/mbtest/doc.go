// Package mbtest is the ManaBrew adapter's test support (MB-8, scoping spec
// §5.1/§8.4/§8.5): a deterministic mock ManaBrew client that answers every
// prompt type internal/manabrew's Translator can build, a seat.Seat wrapper
// (TranslatingSeat) that drives decision -> prompt -> mock response -> intent
// in-process, and the census/replay-equivalence tests that exercise both
// against real repo-deck games.
//
// It is test support, not the engine or the wire transport: it imports
// internal/manabrew, protocol/manabrew, seat and internal/bench, the same
// tier the scoping spec's §5.1 architecture table assigns it. HTTP is out of
// scope (host/manabrewhttp, MB-9/MB-10/MB-11).
package mbtest
