// Package pay is the L5 payment-planning layer of the rules engine (lasagna
// spec docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md §9,
// W5 step E7, first slice): the planner's vocabulary -- a source's Alt
// alternatives, the Consequence an activation carries, the lexicographic Rank
// that orders plans, the Witness a chosen plan discloses -- and its bounded,
// rank-aware Search over classes of interchangeable source units.
//
// Everything here is a pure function of its arguments: no event, no state
// write, no RNG. The search settles a complete count vector through Env.Settle,
// the hook package rules implements over its mana solver, so pay never sees
// an Engine. The rest of the payment layer reaches the engine through
// pay.Engine and its role interfaces -- chars.Reader (Engine.Chars), Eval
// (Engine.Eval) and the engine-owned Session -- and never holds an
// effects.Host (lasagna spec §9.2, which also lists what stays in rules).
package pay
