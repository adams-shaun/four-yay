// Package v2agent is gorge's agent-role implementation of the SpellBench
// protocol v2 ("spellbench/v2", spec/SPELLBENCH_PROTOCOL_V2.md in the
// spellbench repository): an NDJSON server over stdin/stdout that answers
// hello, game_start, choose and game_over (spec Sections 2, 4, 10).
//
// The package is the agent role only. It never speaks the environment role
// and never needs gorge's engine: every decision arrives already validated by
// the host (spec 11.3), carrying the acting seat's observation (spec 6) and
// an ordered candidate list (spec 7). A Policy picks one candidate.
//
// Reading is lenient, as spec 4.2 licenses agents: unknown fields are ignored
// everywhere, and the typed decode of the observation, the seat decision and
// game_start fails soft -- a mistyped or unknown field is logged once to the
// diagnostics writer (stderr for cmd/sbagent) and play continues. Only what
// the agent needs to answer is required: the envelope (spec 4.1), the game a
// request names, and integer candidate ids. Anything it cannot serve is
// answered with an agent error (spec 10.5).
//
// Writing is canonical JSON (RFC 8785, spec 4.3), so the host's strict reader
// always accepts it. A choice echoes the decision's seat_step and the chosen
// candidate's semantic verbatim (spec 10.3), which turns any disagreement
// between what the agent read and what the host offered into a loud
// invalid_selection instead of a silently wrong move.
//
// The three builtin policies mirror the python v2 builtins on the
// protocol-v2 branch (python/spellbench/builtins): First and Heuristic make
// exactly the python bots' choices, and Random uses the python uniform bot's
// SplitMix64 derivation (spellbench-arena-uniform-v2), so it reproduces the
// python uniform bot's picks for the same agent_seed and seed as well.
//
// The agent keeps the latest decoded Observation (Agent.Observation), the
// typed foundation a search agent builds on.
package v2agent
