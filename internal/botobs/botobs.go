// Package botobs is the observability checklist the reward loop's obs axis
// scores against (docs/superpowers/specs/2026-09-29-reward-loop-and-seed-agent-design.md
// §6): a denominator for "how much of what a human player can see does a bot
// seat actually see".
//
// checklist.json enumerates each fact a human player has access to. `exposed`
// is a RECORDED claim; the ratchet in checklist_test.go is the MEASUREMENT --
// it probes the real observation path (the projected view, the deck manifest
// and the bot policy's decision router) and fails if a recorded flag and the
// measured one disagree in either direction. The checklist alone cannot
// disagree with itself, so the probe, not this loader, is what makes the file
// a ratchet.
//
// This package is measurement only: it exposes nothing and changes no policy.
// scripts/reward_collect.py's collect_obs reads the same facts list and sums
// `exposed` for the obs axis rows observable_facts_exposed/observable_facts_total.
package botobs

import (
	"encoding/json"
	"fmt"
	"os"
)

// Fact is one human-visible fact and the recorded state of its exposure.
type Fact struct {
	// ID is the stable snake_case identifier the ratchet's probe table keys on.
	ID string `json:"id"`
	// Description is the human-readable fact.
	Description string `json:"description"`
	// Where names the engine-side accessor a bot could read the fact from, or
	// nil when no accessor exists at all. It is a navigation aid, not the
	// measurement: a non-nil Where for an unexposed fact (deck.File.Archetype)
	// is exactly the gap this checklist is meant to name.
	Where *string `json:"where"`
	// Exposed records whether a bot view carries the fact today. The ratchet
	// measures this from the code rather than trusting it.
	Exposed bool `json:"exposed"`
}

// Checklist is the parsed checklist.json.
type Checklist struct {
	Version int    `json:"version"`
	Facts   []Fact `json:"facts"`
}

// Load reads and decodes a checklist document. It does not validate the IDs
// or probe them; the ratchet owns that, so a malformed checklist fails the
// same test that would have measured it.
func Load(path string) (Checklist, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Checklist{}, fmt.Errorf("botobs: read checklist: %w", err)
	}
	var c Checklist
	if err := json.Unmarshal(raw, &c); err != nil {
		return Checklist{}, fmt.Errorf("botobs: parse checklist: %w", err)
	}
	return c, nil
}
