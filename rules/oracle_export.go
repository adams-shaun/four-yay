package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/cards"
)

// OracleResult is one scenario's outcome for the compliance pipeline: the
// runner's mismatches against the scenario's own expectations, its
// transcript, and the checkpoint snapshots and decisions the comparator
// diffs against XMage.
type OracleResult struct {
	Fails      []string         `json:"fails,omitempty"`
	Transcript []string         `json:"transcript,omitempty"`
	Snapshots  []OracleSnapshot `json:"snapshots"`
	Decisions  []OracleDecision `json:"decisions"`
}

// decodeOracleScenario decodes one scenario object (the elements of an
// oracle file's "scenarios" array), rejecting unknown fields as the file
// loader does.
func decodeOracleScenario(raw []byte) (oracleScenario, error) {
	var sc oracleScenario
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sc); err != nil {
		return oracleScenario{}, fmt.Errorf("oracle scenario: %v", err)
	}
	return sc, nil
}

// RunOracleScenarioJSON plays one scenario against the corpus in reg.
func RunOracleScenarioJSON(reg *cards.Registry, raw []byte) (OracleResult, error) {
	sc, err := decodeOracleScenario(raw)
	if err != nil {
		return OracleResult{}, err
	}
	sc.xmageFixture = true
	sp := oracleSparePool.Get().(*Spare)
	fails, transcript, run := runOracleScenarioSpare(reg, sc, false, sp)
	res := OracleResult{
		Fails: fails, Transcript: transcript,
		Snapshots: append([]OracleSnapshot{}, run.snaps...),
		Decisions: append([]OracleDecision{}, run.decisions...),
	}
	// The result holds only strings and fresh slices, never the engine's
	// arrays, so the finished game's storage goes back to the pool. A run
	// that panicked mid-emit keeps its engine (it is simply dropped): only
	// a cleanly returned engine is released.
	if run.e != nil && !oracleRunPanicked(fails) {
		*sp = run.e.Release()
		run.e = nil
	}
	oracleSparePool.Put(sp)
	return res, nil
}

// oracleSparePool recycles engine storage (Spare) across
// RunOracleScenarioJSON calls. The compliance generator and its audits play
// tens of thousands of short two-seat scenarios back to back; without it
// every probe and every scenario allocated and zeroed a full game's event
// log and object arena. Reuse never changes a game (Spare), and a
// sync.Pool keeps at most a handful live per P, released under GC pressure.
var oracleSparePool = sync.Pool{New: func() any { return new(Spare) }}

func oracleRunPanicked(fails []string) bool {
	for _, f := range fails {
		if strings.HasPrefix(f, "harness: panic: ") {
			return true
		}
	}
	return false
}
