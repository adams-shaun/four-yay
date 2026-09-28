package v2agent

import (
	"encoding/json"
	"strconv"
)

// The agent-role request payloads of spec Section 10, typed. The agent
// decodes them leniently (spec 4.2): a field of the wrong type is left at
// its zero value and logged, never fatal.

// DeckRow is one decklist row (spec 12.1).
type DeckRow struct {
	Name  string `json:"name"`
	Count uint32 `json:"count"`
}

// Deck is game_start's own_deck or opponent_deck (spec 10.2).
type Deck struct {
	DeckID   string    `json:"deck_id"`
	Name     string    `json:"name"`
	Decklist []DeckRow `json:"decklist"`
}

// CardNameDomain is the public name domain of spec 12.2.
type CardNameDomain struct {
	DomainID string   `json:"domain_id"`
	Names    []string `json:"names"`
}

// Rules is the information-rules object of spec 9.2 and 12.2.
type Rules struct {
	OpponentDecklist string         `json:"opponent_decklist"`
	Mulligan         string         `json:"mulligan"`
	StartingPlayer   string         `json:"starting_player"`
	StartingSeat     *string        `json:"starting_seat"`
	CardNameDomain   CardNameDomain `json:"card_name_domain"`
	Extensions       []string       `json:"extensions"`
	Probe            bool           `json:"probe"`
}

// EngineIdentity is hello_ok.engine (spec 9.1).
type EngineIdentity struct {
	Name             string  `json:"name"`
	Version          string  `json:"version"`
	SourceRevision   *string `json:"source_revision"`
	RulesSnapshotID  string  `json:"rules_snapshot_id"`
	CardPoolIdentity string  `json:"card_pool_identity"`
}

// Extension is one hello_ok.extensions entry (spec 9.1, 14).
type Extension struct {
	Name      string `json:"name"`
	NativeIDs bool   `json:"native_ids"`
}

// RulesSupported is hello_ok.rules_supported (spec 9.1).
type RulesSupported struct {
	Mulligan       []string `json:"mulligan"`
	StartingPlayer []string `json:"starting_player"`
}

// Fairness is hello_ok.fairness (spec 9.1).
type Fairness struct {
	NoninterferenceProbe bool `json:"noninterference_probe"`
}

// EngineProfile is game_start.engine_profile (spec 10.2): what the engine
// declared, so a bot knows which optional observation fields it receives
// (Observation maps the thirteen flags of spec 6.9) and which defaults are in
// force (EngineDefaults: nil value = offered through its kind).
type EngineProfile struct {
	RulesSupported RulesSupported     `json:"rules_supported"`
	Observation    map[string]bool    `json:"observation"`
	DecisionKinds  []string           `json:"decision_kinds"`
	EngineDefaults map[string]*string `json:"engine_defaults"`
	Rewind         bool               `json:"rewind"`
	Fairness       Fairness           `json:"fairness"`
	Extensions     []Extension        `json:"extensions"`
}

// TimeControl is spec 11.4's time_control.
type TimeControl struct {
	StartupMs     int64 `json:"startup_ms"`
	GameStartMs   int64 `json:"game_start_ms"`
	BankMs        int64 `json:"bank_ms"`
	IncrementMs   int64 `json:"increment_ms"`
	MaxDecisionMs int64 `json:"max_decision_ms"`
	EngineStepMs  int64 `json:"engine_step_ms"`
}

// Limits is spec 11.4's limits.
type Limits struct {
	MaxDecisions            int64 `json:"max_decisions"`
	MaxSteps                int64 `json:"max_steps"`
	MaxSeatDecisionsPerTurn int64 `json:"max_seat_decisions_per_turn"`
	MaxSeatDecisionsPerGame int64 `json:"max_seat_decisions_per_game"`
	MaxSeatStepsPerGame     int64 `json:"max_seat_steps_per_game"`
}

// Resources is spec 11.4's resources.
type Resources struct {
	CPUs       int64 `json:"cpus"`
	MemoryMB   int64 `json:"memory_mb"`
	GPU        bool  `json:"gpu"`
	EngineCPUs int64 `json:"engine_cpus"`
}

// GameStart is the game_start payload (spec 10.2). AgentSeed is the raw
// literal (nil when absent or null); Seed reads it the way the python
// builtins do.
type GameStart struct {
	GameID        string          `json:"game_id"`
	Seat          string          `json:"seat"`
	Format        string          `json:"format"`
	OwnDeck       *Deck           `json:"own_deck"`
	OpponentDeck  *Deck           `json:"opponent_deck"`
	Rules         Rules           `json:"rules"`
	Engine        EngineIdentity  `json:"engine"`
	EngineProfile EngineProfile   `json:"engine_profile"`
	TimeControl   TimeControl     `json:"time_control"`
	Limits        Limits          `json:"limits"`
	Resources     Resources       `json:"resources"`
	AgentSeed     json.RawMessage `json:"agent_seed"`

	// seatIsString records whether "seat" was a JSON string (python's
	// _text(): anything else reads as None).
	seatIsString bool
}

// Group is seat_decision.group (spec 8).
type Group struct {
	GroupID      int64  `json:"group_id"`
	SubstepIndex uint32 `json:"substep_index"`
	SubstepCount uint32 `json:"substep_count"`
}

// Context is seat_decision.context (spec 9.3).
type Context struct {
	Kind    string     `json:"kind"`
	Source  *ObjectRef `json:"source"`
	Purpose *string    `json:"purpose"`
	Text    *string    `json:"text"`
	Rewind  bool       `json:"rewind"`
}

// WireCandidate is a candidate as it appears on the wire (spec 7.1): the
// semantic is kept raw, so the echo is the host's own bytes.
type WireCandidate struct {
	CandidateID json.Number     `json:"candidate_id"`
	Semantic    json.RawMessage `json:"semantic"`
	DisplayText *string         `json:"display_text"`
}

// SeatDecision is the validated seat decision the host forwards (spec 9.3).
type SeatDecision struct {
	ActingSeat  string                     `json:"acting_seat"`
	SeatStep    int64                      `json:"seat_step"`
	Group       Group                      `json:"group"`
	Context     Context                    `json:"context"`
	Observation Observation                `json:"observation"`
	Candidates  []WireCandidate            `json:"candidates"`
	Extensions  map[string]json.RawMessage `json:"extensions"`
}

// Clock is choose.clock (spec 10.3).
type Clock struct {
	RemainingMs   int64 `json:"remaining_ms"`
	MaxDecisionMs int64 `json:"max_decision_ms"`
}

// Terminal is game_over.terminal (spec 10.4).
type Terminal struct {
	Outcome        string  `json:"outcome"`
	Classification string  `json:"classification"`
	Winner         *string `json:"winner"`
	Reason         string  `json:"reason"`
	SeatStepCount  int64   `json:"seat_step_count"`
}

// GameOver is the game_over payload (spec 10.4).
type GameOver struct {
	GameID   string   `json:"game_id"`
	Terminal Terminal `json:"terminal"`
}

// Semantic is a candidate's tagged semantic (spec 7.2, 7.3), typed as the
// union of every v2.0 kind's fields. Only the fields of Kind are meaningful.
// Value and Choice are polymorphic on the wire (Value is a number for
// choose_number, a bool for choose_boolean, a string for choose_name;
// Choice is a target reference for select_object and a string for
// choose_cost_option), so they stay raw with typed accessors.
type Semantic struct {
	Kind string `json:"kind"`

	Source       *ObjectRef `json:"source"`
	Face         uint32     `json:"face"`
	Method       *string    `json:"method"`
	AbilityIndex uint32     `json:"ability_index"`
	ManaChoice   *string    `json:"mana_choice"`
	CostTarget   *TargetRef `json:"cost_target"`
	Action       string     `json:"action"`

	Slot          uint32     `json:"slot"`
	Target        *TargetRef `json:"target"`
	SelectedCount uint32     `json:"selected_count"`
	Minimum       int64      `json:"minimum"`
	Maximum       int64      `json:"maximum"`
	CostKind      string     `json:"cost_kind"`
	Candidate     *ObjectRef `json:"candidate"`
	ModeIndex     uint32     `json:"mode_index"`
	ModeCount     uint32     `json:"mode_count"`
	Purpose       *string    `json:"purpose"`
	OptionIndex   uint32     `json:"option_index"`
	OptionCount   uint32     `json:"option_count"`
	OptionLabel   *string    `json:"option_label"`
	Color         string     `json:"color"`

	Value  json.RawMessage `json:"value"`
	Choice json.RawMessage `json:"choice"`

	Cost   string     `json:"cost"`
	Pay    *bool      `json:"pay"`
	Card   *ObjectRef `json:"card"`
	CastIt *bool      `json:"cast_it"`

	HandSize       uint32 `json:"hand_size"`
	MulligansTaken uint32 `json:"mulligans_taken"`
	Keep           *bool  `json:"keep"`

	Item        *OrderItem `json:"item"`
	Position    uint32     `json:"position"`
	Count       uint32     `json:"count"`
	CardIndex   uint32     `json:"card_index"`
	CardCount   uint32     `json:"card_count"`
	Destination string     `json:"destination"`

	Affected          *TargetRef `json:"affected"`
	Event             string     `json:"event"`
	ReplacementSource *ObjectRef `json:"replacement_source"`
	ReplacementIndex  uint32     `json:"replacement_index"`
	ReplacementCount  uint32     `json:"replacement_count"`

	Player   string     `json:"player"`
	Attacker *ObjectRef `json:"attacker"`
	Defender *TargetRef `json:"defender"`
	Blocker  *ObjectRef `json:"blocker"`

	Recipient *TargetRef `json:"recipient"`
	Amount    uint32     `json:"amount"`
	Remaining uint32     `json:"remaining"`

	PileIndex uint32        `json:"pile_index"`
	Piles     [][]ObjectRef `json:"piles"`
}

// OrderItem is order_pick.item (spec 7.3): an object or a trigger.
type OrderItem struct {
	Object  *ObjectRef   `json:"object,omitempty"`
	Trigger *TriggerItem `json:"trigger,omitempty"`
}

// TriggerItem is order_pick.item.trigger (spec 7.3).
type TriggerItem struct {
	Source       *ObjectRef  `json:"source"`
	SourceName   *string     `json:"source_name"`
	AbilityIndex *uint32     `json:"ability_index"`
	EventObjects []ObjectRef `json:"event_objects"`
	Instance     uint32      `json:"instance"`
	Label        *string     `json:"label"`
}

// IntValue reads Value as an integer (choose_number).
func (s *Semantic) IntValue() (int64, bool) {
	n, err := strconv.ParseInt(string(trimJSON(s.Value)), 10, 64)
	return n, err == nil
}

// BoolValue reads Value as a bool (choose_boolean).
func (s *Semantic) BoolValue() (bool, bool) {
	switch string(trimJSON(s.Value)) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// StringValue reads Value as a string (choose_name).
func (s *Semantic) StringValue() (string, bool) {
	var v string
	if json.Unmarshal(s.Value, &v) != nil {
		return "", false
	}
	return v, true
}

// ChoiceTarget reads Choice as a target reference (select_object).
func (s *Semantic) ChoiceTarget() (*TargetRef, bool) {
	var t TargetRef
	if len(s.Choice) == 0 || s.Choice[0] != '{' || json.Unmarshal(s.Choice, &t) != nil {
		return nil, false
	}
	return &t, true
}

// ChoiceString reads Choice as a string (choose_cost_option).
func (s *Semantic) ChoiceString() (string, bool) {
	var v string
	if json.Unmarshal(s.Choice, &v) != nil {
		return "", false
	}
	return v, true
}

func trimJSON(raw json.RawMessage) []byte {
	b := []byte(raw)
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\n' || b[0] == '\r') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
