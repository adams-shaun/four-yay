package v1agent

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
)

// First always answers candidate 0 (python arena/bots/first.py).
type First struct{}

func (First) GameStart(*GameStart) {}
func (First) GameOver(*Terminal)   {}
func (First) Choose(*Decision) int { return 0 }

// Heuristic is python arena/bots/heuristic.py, choice for choice: the first
// play_land, else the first cast_spell, else the first
// activate_mana_ability/activate_ability, else the first attacker
// inclusion with include=true, else the first blocker inclusion with
// include=false, else candidate 0.
type Heuristic struct{}

func (*Heuristic) GameStart(*GameStart) {}
func (*Heuristic) GameOver(*Terminal)   {}

// Choose implements Policy.
func (*Heuristic) Choose(d *Decision) int { return HeuristicPick(d) }

// HeuristicPick is the python heuristic's answer for d.
func HeuristicPick(d *Decision) int {
	first := func(pred func(c *Candidate) bool) int {
		for i := range d.Candidates {
			if pred(&d.Candidates[i]) {
				return i
			}
		}
		return -1
	}
	for _, kind := range []string{"play_land", "cast_spell"} {
		if i := first(func(c *Candidate) bool { return c.Kind() == kind }); i >= 0 {
			return i
		}
	}
	if i := first(func(c *Candidate) bool {
		return c.Kind() == "activate_mana_ability" || c.Kind() == "activate_ability"
	}); i >= 0 {
		return i
	}
	if i := first(func(c *Candidate) bool {
		return c.Kind() == "choose_attacker_inclusion" && c.Semantic.Bool("include")
	}); i >= 0 {
		return i
	}
	if i := first(func(c *Candidate) bool {
		return c.Kind() == "choose_blocker_inclusion" && !c.Semantic.Bool("include")
	}); i >= 0 {
		return i
	}
	return 0
}

// Uniform is python arena/bots/uniform.py (derivation
// spellbench-arena-uniform-v1): per game, SplitMix64 seeded with
// seed ^ big-endian first 8 bytes of sha256(game_id); each choose draws
// next() % len(candidates).
type Uniform struct {
	Seed   uint64
	stream *v2agent.SplitMix64
}

// GameStart seeds the per-game stream.
func (u *Uniform) GameStart(g *GameStart) {
	u.stream = v2agent.NewSplitMix64(UniformGameSeed(u.Seed, g.GameID))
}

// GameOver drops the stream.
func (u *Uniform) GameOver(*Terminal) { u.stream = nil }

// Choose implements Policy.
func (u *Uniform) Choose(d *Decision) int {
	if u.stream == nil {
		u.stream = v2agent.NewSplitMix64(UniformGameSeed(u.Seed, d.GameID))
	}
	return int(u.stream.Next() % uint64(len(d.Candidates)))
}

// UniformGameSeed is game_stream_seed from uniform.py.
func UniformGameSeed(seed uint64, gameID string) uint64 {
	sum := sha256.Sum256([]byte(gameID))
	return seed ^ binary.BigEndian.Uint64(sum[:8])
}

// NewPolicy builds a named policy: first, heuristic, uniform (seeded) or
// tactical.
func NewPolicy(name string, seed uint64) (Policy, error) {
	return NewPolicyWith(name, seed, TacticalOptions{})
}

// NewPolicyWith is NewPolicy with tactical options.
func NewPolicyWith(name string, seed uint64, topts TacticalOptions) (Policy, error) {
	switch name {
	case "first":
		return First{}, nil
	case "heuristic":
		return &Heuristic{}, nil
	case "uniform":
		return &Uniform{Seed: seed}, nil
	case "tactical":
		return NewTactical(topts), nil
	}
	return nil, fmt.Errorf("unknown policy %q (first, heuristic, uniform, tactical)", name)
}
