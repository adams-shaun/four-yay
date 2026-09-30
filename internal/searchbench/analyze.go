package searchbench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
)

// RootOption is one canonical option's row in the search's root table: the
// azmcts root children projected onto the item's canonical choices and summed
// (a joint attack declaration contributes to "attack with Focus" or not).
type RootOption struct {
	Choice int
	Visits int
	Q      float64
}

// Result is one arm's answer to one item: one JSON object per line, one line
// per item per arm. AgentChoices holds exactly one canonical option index and
// AgentAct is exactly AgentChoices[0] != 0. CoreSeconds is measured by the
// runner at the command boundary; Fallback is empty, or why the bot's answer
// was played instead of a searched one.
type Result struct {
	ManifestDigest, Arm, ItemID string
	Seed                        uint64
	AgentChoices                []int
	AgentAct                    bool
	Sims, Completed             int
	Root                        []RootOption
	MeanLeafPlies               float64
	MeanLeafEdges               float64
	MeanTurnsCrossed            float64
	EnvSteps                    int
	Fallback                    string
	CoreSeconds                 float64
}

func finite(v ...float64) bool {
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}

// validate checks one result against its item.
func (r Result) validate(it Item) error {
	if len(r.AgentChoices) != 1 {
		return errors.New("needs exactly one agent choice")
	}
	c := r.AgentChoices[0]
	if c < 0 || c >= len(it.Options) {
		return fmt.Errorf("choice %d is outside %d options", c, len(it.Options))
	}
	if r.AgentAct != (c != 0) {
		return errors.New("AgentAct must be exactly choice != 0")
	}
	if r.Sims < 0 || r.Completed < 0 || r.Completed > r.Sims || r.EnvSteps < 0 {
		return errors.New("invalid simulation counters")
	}
	if !finite(r.MeanLeafPlies, r.MeanLeafEdges, r.MeanTurnsCrossed, r.CoreSeconds) || r.MeanLeafPlies < 0 || r.MeanLeafEdges < 0 || r.MeanTurnsCrossed < 0 || r.CoreSeconds < 0 {
		return errors.New("invalid depth or timing")
	}
	for i, o := range r.Root {
		if o.Choice < 0 || o.Choice >= len(it.Options) || (i > 0 && o.Choice <= r.Root[i-1].Choice) {
			return errors.New("root options must be distinct canonical choices in ascending order")
		}
		if o.Visits < 0 || !finite(o.Q) {
			return errors.New("invalid root visits or Q")
		}
	}
	return nil
}

// Run is one result file bound to the items of one split of its manifest, in
// manifest order.
type Run struct {
	Arm     string
	Items   []Item
	Results []Result
}

// SplitItems returns the manifest's items of one split, in manifest order.
func SplitItems(m Manifest, split Split) []Item {
	var out []Item
	for _, it := range m.Items {
		if it.Split == split {
			out = append(out, it)
		}
	}
	return out
}

// BindResults binds one result file to its sealed manifest. Every row must
// name the manifest's digest and one of its items, once, under a single arm,
// and every item of the analysed split must be answered; rows for the other
// split are validated and then left out. An incomplete run never turns into
// a flattering score.
func BindResults(m Manifest, split Split, rows []Result) (Run, error) {
	if err := m.Validate(); err != nil {
		return Run{}, err
	}
	if split != SplitDev && split != SplitTest {
		return Run{}, fmt.Errorf("searchbench: unknown split %q", split)
	}
	items := make(map[string]Item, len(m.Items))
	for _, it := range m.Items {
		items[it.ID] = it
	}
	byID := make(map[string]Result, len(rows))
	arm := ""
	for i := range rows {
		r := rows[i]
		if r.ManifestDigest != m.Digest {
			return Run{}, fmt.Errorf("searchbench: result %d names manifest %q, want %q", i, r.ManifestDigest, m.Digest)
		}
		if r.Arm == "" || r.ItemID == "" {
			return Run{}, fmt.Errorf("searchbench: result %d is missing its arm or item", i)
		}
		if arm == "" {
			arm = r.Arm
		} else if arm != r.Arm {
			return Run{}, fmt.Errorf("searchbench: one result file mixes arms %q and %q", arm, r.Arm)
		}
		it, ok := items[r.ItemID]
		if !ok {
			return Run{}, fmt.Errorf("searchbench: result names unknown item %q", r.ItemID)
		}
		if _, ok := byID[r.ItemID]; ok {
			return Run{}, fmt.Errorf("searchbench: duplicate result for item %q", r.ItemID)
		}
		if err := r.validate(it); err != nil {
			return Run{}, fmt.Errorf("searchbench: result for item %q: %w", r.ItemID, err)
		}
		byID[r.ItemID] = r
	}
	run := Run{Arm: arm, Items: SplitItems(m, split)}
	if len(run.Items) == 0 {
		return Run{}, fmt.Errorf("searchbench: manifest has no %s items", split)
	}
	for _, it := range run.Items {
		r, ok := byID[it.ID]
		if !ok {
			return Run{}, fmt.Errorf("searchbench: no result for %s item %q", split, it.ID)
		}
		run.Results = append(run.Results, r)
	}
	return run, nil
}

// ReadResults reads one JSON object per line and rejects unknown fields,
// blank lines and trailing values.
func ReadResults(path string) ([]Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64<<10), 16<<20)
	var out []Result
	line := 0
	for s.Scan() {
		line++
		if len(bytes.TrimSpace(s.Bytes())) == 0 {
			return nil, fmt.Errorf("searchbench: %s: blank result line %d", path, line)
		}
		dec := json.NewDecoder(bytes.NewReader(s.Bytes()))
		dec.DisallowUnknownFields()
		var r Result
		if err := dec.Decode(&r); err != nil {
			return nil, fmt.Errorf("searchbench: %s line %d: %w", path, line, err)
		}
		if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("searchbench: %s line %d has trailing JSON", path, line)
		}
		out = append(out, r)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// WriteResults writes rows as JSON lines.
func WriteResults(path string, rows []Result) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			_ = f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Baseline arm names.
const (
	BaselinePassive = "baseline-passive"
	BaselineActive  = "baseline-active"
	BaselineRandom  = "baseline-random"
)

// Baselines answers every manifest item three ways, scored exactly like a
// search arm:
//
//   - always passive: option 0 (docs/016 §8.1 "pass, don't attack, don't
//     block"; analyze.py references() "passive").
//   - always active: option 1, the first non-passive option in canonical
//     order. docs/016 §8.1's "always act: cast, attack, block" row does not
//     say which cast or which attacker to block, and upstream's code has no
//     implementation of it, so its spell and block figures are not
//     reproducible exactly; the first canonical active option is our
//     definition.
//   - uniform random: a uniform pick over the item's distinct options from a
//     seeded PCG stream in manifest order; its expected A_set is Chance.
func Baselines(m Manifest, seed uint64) (map[string][]Result, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	rng := rand.New(rand.NewPCG(seed, 0x5eed5bec4b0a11e5))
	out := map[string][]Result{}
	for _, it := range m.Items {
		pick := rng.IntN(len(it.Options))
		for arm, c := range map[string]int{BaselinePassive: 0, BaselineActive: 1, BaselineRandom: pick} {
			s := uint64(0)
			if arm == BaselineRandom {
				s = seed
			}
			out[arm] = append(out[arm], Result{ManifestDigest: m.Digest, Arm: arm, ItemID: it.ID, Seed: s, AgentChoices: []int{c}, AgentAct: c != 0})
		}
	}
	return out, nil
}

// BaselineArms lists the baseline arms in a fixed order.
func BaselineArms() []string { return []string{BaselinePassive, BaselineActive, BaselineRandom} }
