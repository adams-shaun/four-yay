package v2agent

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// Candidate is one offered candidate as a policy reads it.
type Candidate struct {
	// ID is the candidate_id the answer carries.
	ID int64
	// Raw is the semantic exactly as the host sent it (echoed verbatim).
	Raw json.RawMessage
	// Fields is the semantic's top-level fields, raw; nil when the semantic
	// is not a JSON object. The python-parity policies read only this, so a
	// field of an unexpected type behaves as it does in the python builtins.
	Fields map[string]json.RawMessage
	// Semantic is the typed decode (best effort; see Agent's diagnostics).
	Semantic Semantic
	// DisplayText is the human-facing label, or nil.
	DisplayText *string
}

// Kind is the semantic's kind, or "" when it is absent or not a string.
func (c *Candidate) Kind() string {
	s, _ := c.stringField("kind")
	return s
}

// stringField reads a top-level string field; ok is false when the field is
// absent or not a JSON string.
func (c *Candidate) stringField(name string) (string, bool) {
	raw, present := c.Fields[name]
	if !present {
		return "", false
	}
	raw = trimJSON(raw)
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// isNull reports whether a field is absent or JSON null (python's
// semantic.get(name) is None).
func (c *Candidate) isNull(name string) bool {
	raw, present := c.Fields[name]
	return !present || string(trimJSON(raw)) == "null"
}

// isTrue reports whether a field is the JSON literal true (python's
// semantic.get(name) is True).
func (c *Candidate) isTrue(name string) bool {
	raw, present := c.Fields[name]
	return present && string(trimJSON(raw)) == "true"
}

// Decision is a choose request as a policy reads it (spec 10.3).
type Decision struct {
	GameID     string
	Seat       *SeatDecision
	Candidates []Candidate
	Clock      Clock
	// SeatStep is the decision's seat_step when it is an integer (echoed).
	SeatStep *int64
}

// Observation is the decision's observation (never nil).
func (d *Decision) Observation() *Observation { return &d.Seat.Observation }

// Policy picks one candidate per decision. Choose returns an index into
// d.Candidates (not a candidate_id). GameStart is called once per game
// before any Choose, GameOver once after the last.
type Policy interface {
	GameStart(g *GameStart)
	Choose(d *Decision) (int, error)
	GameOver(g *GameOver)
}

// First always answers the first candidate: pass whenever passing is legal
// (spec 7.1, 10.6). It matches python/spellbench/builtins/first.py.
type First struct{}

func (First) GameStart(*GameStart) {}
func (First) GameOver(*GameOver)   {}

// Choose returns 0.
func (First) Choose(d *Decision) (int, error) { return 0, nil }

// Heuristic is the python v2 heuristic builtin
// (python/spellbench/builtins/heuristic.py), choice for choice. It answers
// the first candidate of the most preferred kind:
//
//  0. mulligan with keep: true
//  1. play_land
//  2. cast_spell
//  3. activate_mana_ability or activate_ability
//  4. declare_attack with a non-null defender
//  5. declare_block with a null attacker
//  6. choose_starting_player naming its own seat
//  7. anything else (so candidate 0, pass when legal, if nothing matches)
//
// It reads only the semantics' raw fields, with python's lenient meaning: a
// missing field is null, and only the JSON literal true is true.
type Heuristic struct {
	seat    string
	hasSeat bool
}

// GameStart records the seat (python's HeuristicBot._seat).
func (h *Heuristic) GameStart(g *GameStart) {
	h.seat, h.hasSeat = g.Seat, g.seatIsString
}

// GameOver forgets the seat.
func (h *Heuristic) GameOver(*GameOver) { h.seat, h.hasSeat = "", false }

// Choose returns the first candidate of minimal rank.
func (h *Heuristic) Choose(d *Decision) (int, error) {
	best, bestRank := 0, heuristicRanks+1
	for i := range d.Candidates {
		if r := h.rank(&d.Candidates[i]); r < bestRank {
			best, bestRank = i, r
		}
	}
	return best, nil
}

// heuristicRanks is the number of preferences; a candidate matching none
// ranks heuristicRanks.
const heuristicRanks = 7

// HeuristicRank exposes the rank of c for seat (tests and tools).
func HeuristicRank(c *Candidate, seat string, hasSeat bool) int {
	h := Heuristic{seat: seat, hasSeat: hasSeat}
	return h.rank(c)
}

func (h *Heuristic) rank(c *Candidate) int {
	kind, isString := c.stringField("kind")
	if !isString {
		return heuristicRanks
	}
	switch {
	case kind == "mulligan" && c.isTrue("keep"):
		return 0
	case kind == "play_land":
		return 1
	case kind == "cast_spell":
		return 2
	case kind == "activate_mana_ability" || kind == "activate_ability":
		return 3
	case kind == "declare_attack" && !c.isNull("defender"):
		return 4
	case kind == "declare_block" && c.isNull("attacker"):
		return 5
	case kind == "choose_starting_player" && h.hasSeat:
		if p, ok := c.stringField("player"); ok && p == h.seat {
			return 6
		}
	}
	return heuristicRanks
}

// Random answers a uniformly random candidate. Its stream is the python
// uniform builtin's (python/spellbench/builtins/uniform.py, derivation
// spellbench-arena-uniform-v2): at game_start the stream is
// SplitMix64((agent_seed XOR Seed) mod 2^64), a missing agent_seed reading
// as 0, and each choose answers candidate next() mod len(candidates). With
// the same Seed (the python bot's --seed) it therefore makes the same picks
// as the python uniform bot for every game.
type Random struct {
	Seed   uint64
	stream *SplitMix64
}

// GameStart seeds the game's stream from agent_seed.
func (r *Random) GameStart(g *GameStart) {
	r.stream = NewSplitMix64(g.Seed() ^ r.Seed)
}

// GameOver drops the stream.
func (r *Random) GameOver(*GameOver) { r.stream = nil }

// Choose draws the next index.
func (r *Random) Choose(d *Decision) (int, error) {
	if r.stream == nil {
		return 0, fmt.Errorf("choose before game_start")
	}
	return int(r.stream.Next() % uint64(len(d.Candidates))), nil
}

// SplitMix64 is the generator of python/spellbench/builtins/uniform.py
// (ported there from mtg-kernel's determinism.py).
type SplitMix64 struct{ state uint64 }

// NewSplitMix64 seeds a generator.
func NewSplitMix64(seed uint64) *SplitMix64 { return &SplitMix64{state: seed} }

// Next returns the next 64-bit value.
func (s *SplitMix64) Next() uint64 {
	s.state += 0x9E3779B97F4A7C15
	z := s.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Seed is agent_seed as the python builtins read it: an integer literal,
// reduced mod 2^64 (two's complement for a negative one); 0 when absent,
// null or not an integer.
func (g *GameStart) Seed() uint64 {
	text := string(trimJSON(g.AgentSeed))
	n, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return 0
	}
	mod := new(big.Int).Lsh(big.NewInt(1), 64)
	n.Mod(n, mod) // Euclidean: a negative value maps to its two's complement
	return n.Uint64()
}

// NewPolicy builds a builtin policy by name: first, heuristic or random.
// seed is the random policy's extra seed (XORed into agent_seed).
func NewPolicy(name string, seed uint64) (Policy, error) {
	switch strings.ToLower(name) {
	case "first":
		return First{}, nil
	case "heuristic":
		return &Heuristic{}, nil
	case "random", "uniform":
		return &Random{Seed: seed}, nil
	}
	return nil, fmt.Errorf("unknown policy %q (want random, heuristic or first)", name)
}
