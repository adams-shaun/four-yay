package v1agent

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ObjectRef is a v1 object reference (spec Section 5). CardName is empty
// for a hidden identity (null on the wire).
type ObjectRef struct {
	ObjectID       string  `json:"object_id"`
	CardName       *string `json:"card_name"`
	OwnerSeat      string  `json:"owner_seat"`
	ControllerSeat string  `json:"controller_seat"`
	Zone           string  `json:"zone"`
}

// Name returns the card name or "" when hidden.
func (o *ObjectRef) Name() string {
	if o == nil || o.CardName == nil {
		return ""
	}
	return *o.CardName
}

// TargetRef is exactly one of a player seat or an object.
type TargetRef struct {
	Player *string    `json:"player,omitempty"`
	Object *ObjectRef `json:"object,omitempty"`
}

// SeatSummary is one seat's public counts.
type SeatSummary struct {
	Seat             string `json:"seat"`
	Life             int    `json:"life"`
	HandCount        int    `json:"hand_count"`
	LibraryCount     int    `json:"library_count"`
	GraveyardCount   int    `json:"graveyard_count"`
	BattlefieldCount int    `json:"battlefield_count"`
}

// StateSummary is the whole neutral v1 observation (spec 7.3).
type StateSummary struct {
	Turn         int           `json:"turn"`
	PhaseStep    string        `json:"phase_step"`
	ActiveSeat   string        `json:"active_seat"`
	PrioritySeat string        `json:"priority_seat"`
	Seats        []SeatSummary `json:"seats"`
	StackCount   int           `json:"stack_count"`
}

// Seat returns the summary row for seat (zero value when absent).
func (s *StateSummary) Seat(seat string) SeatSummary {
	for _, r := range s.Seats {
		if r.Seat == seat {
			return r
		}
	}
	return SeatSummary{Seat: seat}
}

// Group is a decision's substep position (spec Section 8).
type Group struct {
	GroupID      int64 `json:"group_id"`
	SubstepIndex int   `json:"substep_index"`
	SubstepCount int   `json:"substep_count"`
}

// Semantic is a candidate's tagged semantic object. Fields are decoded
// lazily from Raw; Raw is echoed verbatim (re-canonicalized) as
// semantic_echo.
type Semantic struct {
	Kind   string
	Fields map[string]json.RawMessage
	Raw    json.RawMessage
}

// Obj decodes an object-reference field (nil when absent or null).
func (s *Semantic) Obj(field string) *ObjectRef {
	raw, ok := s.Fields[field]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var o ObjectRef
	if json.Unmarshal(raw, &o) != nil {
		return nil
	}
	return &o
}

// Objs decodes an array-of-object-reference field.
func (s *Semantic) Objs(field string) []ObjectRef {
	var out []ObjectRef
	_ = json.Unmarshal(s.Fields[field], &out)
	return out
}

// Target decodes a target-reference field.
func (s *Semantic) Target(field string) *TargetRef {
	raw, ok := s.Fields[field]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var t TargetRef
	if json.Unmarshal(raw, &t) != nil {
		return nil
	}
	return &t
}

// Bool reads a boolean field (false when absent).
func (s *Semantic) Bool(field string) bool {
	var b bool
	_ = json.Unmarshal(s.Fields[field], &b)
	return b
}

// Int reads an integer field (0 when absent).
func (s *Semantic) Int(field string) int64 {
	var n int64
	_ = json.Unmarshal(s.Fields[field], &n)
	return n
}

// Str reads a string field ("" when absent or null).
func (s *Semantic) Str(field string) string {
	var v string
	_ = json.Unmarshal(s.Fields[field], &v)
	return v
}

// Has reports whether field is present.
func (s *Semantic) Has(field string) bool { _, ok := s.Fields[field]; return ok }

// Source is the "source" object reference, the most common field.
func (s *Semantic) Source() *ObjectRef { return s.Obj("source") }

// Candidate is one offered choice.
type Candidate struct {
	ID          int
	Semantic    Semantic
	DisplayText *string
}

// Kind is the candidate's semantic kind.
func (c *Candidate) Kind() string { return c.Semantic.Kind }

// Decision is one v1 decision for this seat (spec 7.3).
type Decision struct {
	GameID     string
	Step       int64
	ActingSeat string
	Group      Group
	Summary    StateSummary
	Candidates []Candidate
	// Kernel is the decoded x_kernel_v5 extension, nil when the engine
	// did not attach one (or it failed to decode; KernelErr says why).
	Kernel    *KernelView
	KernelErr error
	// Extensions keeps every extension raw, for policies that read others.
	Extensions map[string]json.RawMessage
}

// Opponent is the other seat.
func (d *Decision) Opponent() string { return OtherSeat(d.ActingSeat) }

// OtherSeat maps p0<->p1.
func OtherSeat(seat string) string {
	if seat == "p0" {
		return "p1"
	}
	return "p0"
}

type wireCandidate struct {
	CandidateID int             `json:"candidate_id"`
	Semantic    json.RawMessage `json:"semantic"`
	DisplayText *string         `json:"display_text"`
}

type wireDecision struct {
	ResponseType string                     `json:"response_type"`
	GameID       string                     `json:"game_id"`
	Step         int64                      `json:"step"`
	ActingSeat   string                     `json:"acting_seat"`
	Group        Group                      `json:"group"`
	StateSummary StateSummary               `json:"state_summary"`
	Candidates   []wireCandidate            `json:"candidates"`
	Extensions   map[string]json.RawMessage `json:"extensions"`
}

// ParseDecision decodes the decision object of a choose request.
func ParseDecision(raw []byte) (*Decision, error) {
	var w wireDecision
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("decision: %w", err)
	}
	if w.ResponseType != "decision" {
		return nil, fmt.Errorf("decision.response_type must be \"decision\"")
	}
	if len(w.Candidates) == 0 {
		return nil, fmt.Errorf("decision.candidates must be nonempty")
	}
	d := &Decision{
		GameID: w.GameID, Step: w.Step, ActingSeat: w.ActingSeat, Group: w.Group,
		Summary: w.StateSummary, Extensions: w.Extensions,
	}
	for i, wc := range w.Candidates {
		if wc.CandidateID != i {
			return nil, fmt.Errorf("decision.candidates[%d].candidate_id is %d", i, wc.CandidateID)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(wc.Semantic, &fields); err != nil || fields == nil {
			return nil, fmt.Errorf("decision.candidates[%d].semantic must be an object", i)
		}
		var kind string
		if err := json.Unmarshal(fields["kind"], &kind); err != nil || kind == "" {
			return nil, fmt.Errorf("decision.candidates[%d].semantic.kind must be a string", i)
		}
		d.Candidates = append(d.Candidates, Candidate{
			ID:          i,
			Semantic:    Semantic{Kind: kind, Fields: fields, Raw: wc.Semantic},
			DisplayText: wc.DisplayText,
		})
	}
	if raw, ok := w.Extensions["x_kernel_v5"]; ok {
		d.Kernel, d.KernelErr = decodeKernel(raw, len(d.Candidates))
	}
	return d, nil
}
