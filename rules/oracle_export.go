package rules

import (
	"bytes"
	"encoding/json"
	"fmt"

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
	fails, transcript, run := runOracleScenario(reg, sc)
	return OracleResult{
		Fails: fails, Transcript: transcript,
		Snapshots: append([]OracleSnapshot{}, run.snaps...),
		Decisions: append([]OracleDecision{}, run.decisions...),
	}, nil
}
