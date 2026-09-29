// Package manabrew is gorge's ManaBrew translator (scoping spec role E, §5.1
// and §6): a pure library that projects a seat-scoped view.View into the
// ManaBrew GameViewDto and maps a pending decision.Decision into the ManaBrew
// prompt of its decision.Kind.
//
// The package imports protocol/manabrew (wire types), view, decision and
// state (types only). It never imports rules, effects, events, host or
// internal/azmcts (archtest rows, MB-2), never imports time, and never ranges
// over a map in a way whose order reaches the output: the maps the DTOs carry
// are encoded by encoding/json, which sorts map keys.
//
// The published ManaBrew protocol specification
// (https://docs.manabrew.app/protocol/) is licensed CC-BY-4.0; this package
// contains independently written Go code. See protocol/manabrew's
// ATTRIBUTION for the full attribution. Nothing here copies ManaBrew's AGPL
// reference implementation.
//
// Layout (spec §9 ticket MB-3):
//
//	translator.go  the Translator: CardText seam, State, Prompt
//	ids.go         the id mint (spec §6.1): players, cards, stack, hidden
//	state.go       view.View → GameViewDto, the step map (spec §6.2)
//	dispatch.go    the full switch over decision.Kinds
//	prompt_*.go    one per-kind stub file; each stub returns ErrUnmapped
//	               until its own MB ticket fills it in. Later tickets edit
//	               only their own stub files, never dispatch.go.
package manabrew
