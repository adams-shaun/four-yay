package statespec

import (
	"encoding/json"
	"fmt"
)

// LabelAct is one recorded land drop, cast or activation (reconstruct's
// label rows). Key is the XMage/MageZero option label ("Play Swamp",
// "Cast Stab", an ability's rule text); Source names an activation's
// source card.
type LabelAct struct {
	Name         string `json:"name"`
	Key          string `json:"key"`
	Idx          int    `json:"idx"`
	VocabExact   bool   `json:"vocab_exact"`
	Zone         string `json:"zone"`
	Family       string `json:"family"`
	ID           int    `json:"id"`
	Source       string `json:"source"`
	SourceUnique bool   `json:"source_unique"`
}

// BlockLabel is one [blocker, attacker|null, exact] row: Attacker is ""
// for "did not block".
type BlockLabel struct {
	Blocker, Attacker string
	Exact             bool
}

func (b *BlockLabel) UnmarshalJSON(raw []byte) error {
	var row []json.RawMessage
	if err := json.Unmarshal(raw, &row); err != nil {
		return err
	}
	if len(row) != 3 {
		return fmt.Errorf("statespec: block label row of %d fields", len(row))
	}
	var blocker string
	var attacker *string
	var exact bool
	if err := json.Unmarshal(row[0], &blocker); err != nil {
		return err
	}
	if err := json.Unmarshal(row[1], &attacker); err != nil {
		return err
	}
	if err := json.Unmarshal(row[2], &exact); err != nil {
		return err
	}
	b.Blocker, b.Exact = blocker, exact
	if attacker != nil {
		b.Attacker = *attacker
	}
	return nil
}

// DecideFrom is labels.bridge.decideFrom.
type DecideFrom struct {
	Turn int    `json:"turn"`
	Step string `json:"step"`
}

// Bridge is labels.bridge: the request options that reach the labelled
// decision.
type Bridge struct {
	DecisionPlayer string      `json:"decisionPlayer"`
	DecideFrom     *DecideFrom `json:"decideFrom"`
}

// Labels types the label keys the search benchmark reads. Upstream's labels
// are an open dict, so other keys are ignored here (and preserved raw in
// Spec.Labels).
type Labels struct {
	Lands        []LabelAct      `json:"lands"`
	Casts        []LabelAct      `json:"casts"`
	Activations  []LabelAct      `json:"activations"`
	Attacks      map[string]bool `json:"attacks"`
	Blocks       []BlockLabel    `json:"blocks"`
	BlockPairing string          `json:"block_pairing"`
	Bridge       Bridge          `json:"bridge"`
	UserTurn     int             `json:"user_turn"`
	GlobalTurn   int             `json:"global_turn"`
	// HasLands/HasCasts/HasOppTurn record whether the key was present.
	// Upstream's 17lands resolver tells a turn label (labels.turn_label,
	// which always sets lands and casts) from an after-turn label by the
	// presence of lands/casts; to_dict prunes empty lists, so a serialized
	// turn label with no land and no cast has neither key. TurnLabel reads
	// the after-turn label's own opp_turn key instead.
	HasLands, HasCasts, HasOppTurn bool
}

// TurnLabel reports whether the labels describe the user's own turn
// (labels.turn_label) rather than the opponent's next half-turn
// (labels.after_turn_label, which carries opp_turn).
func (l Labels) TurnLabel() bool { return l.HasLands || l.HasCasts || !l.HasOppTurn }

// ParseLabels types s.Labels.
func (s *Spec) ParseLabels() (Labels, error) {
	var l Labels
	if len(s.Labels) == 0 {
		return l, nil
	}
	raw, err := json.Marshal(s.Labels)
	if err != nil {
		return l, err
	}
	if err := json.Unmarshal(raw, &l); err != nil {
		return l, fmt.Errorf("statespec: labels: %w", err)
	}
	_, l.HasLands = s.Labels["lands"]
	_, l.HasCasts = s.Labels["casts"]
	_, l.HasOppTurn = s.Labels["opp_turn"]
	return l, nil
}

// Acts is the turn's keyed casts and activations (items.py's `acts`).
func (l Labels) Acts() []LabelAct {
	var out []LabelAct
	for _, x := range append(append([]LabelAct(nil), l.Casts...), l.Activations...) {
		if x.Key != "" {
			out = append(out, x)
		}
	}
	return out
}

// KeyedLands is the turn's keyed land drops.
func (l Labels) KeyedLands() []LabelAct {
	var out []LabelAct
	for _, x := range l.Lands {
		if x.Key != "" {
			out = append(out, x)
		}
	}
	return out
}

// Attacked reports whether any recorded attack is true.
func (l Labels) Attacked() bool {
	for _, v := range l.Attacks {
		if v {
			return true
		}
	}
	return false
}
