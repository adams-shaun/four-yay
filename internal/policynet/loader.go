package policynet

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/traceboard"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// LabelSchemaVersion and LabelRecordType mirror cmd/searchteacher/labels.go's
// writer constants. The record type itself is mirrored below (the writer is
// package main and cannot be imported); a drift between the two shapes is a
// decoder error on the fields that matter, not a silent misread.
//
// LabelSchemaVersion is the version the writer EMITS (schema 2 added
// outcome/outcome_known); AcceptedLabelSchemaVersions is every version Load
// reads. A schema 1 record loads with Example.HasOutcome false.
const (
	LabelSchemaVersion = 2
	LabelRecordType    = "label-v1"
)

// AcceptedLabelSchemaVersions is the allow-list of label schema versions a
// reader accepts, in ascending order. Callers must treat it as read-only.
//
// Schema 3 (pn12) is schema 2 plus the optional "extras" object
// (LabelExtras, written by cmd/searchteacher -label-extras): Load reads a
// schema 3 record exactly as schema 2 and ignores the extras; LoadWith reads
// them.
var AcceptedLabelSchemaVersions = []int{1, 2, 3}

// LabelSchemaAccepted reports whether v is in AcceptedLabelSchemaVersions.
func LabelSchemaAccepted(v int) bool {
	for _, a := range AcceptedLabelSchemaVersions {
		if a == v {
			return true
		}
	}
	return false
}

// labelRecord mirrors cmd/searchteacher/labels.go's LabelRecord: the merged
// search-teacher label corpus record, one JSON object per line. Field names
// and JSON tags must match the writer's exactly.
type labelRecord struct {
	RecordType    string            `json:"record_type"`
	SchemaVersion int               `json:"schema_version"`
	Pair          string            `json:"pair"`
	GameIndex     int               `json:"game_index"`
	Seed          uint64            `json:"seed"`
	Sequence      uint64            `json:"decision_sequence"`
	Seat          state.PlayerID    `json:"seat"`
	Kind          decision.Kind     `json:"kind"`
	Turn          int32             `json:"turn"`
	Board         traceboard.Board  `json:"board"`
	View          json.RawMessage   `json:"view"`
	Options       []decision.Option `json:"options"`
	Candidates    []labelCandidate  `json:"candidates"`
	TeacherChoice int               `json:"teacher_choice"`
	BotIndex      int               `json:"bot_index"`
	Margin        float64           `json:"margin"`
	Worlds        int               `json:"worlds"`
	Attempts      int               `json:"attempts"`
	Accepted      int               `json:"accepted"`
	Horizon       int32             `json:"horizon"`
	// Outcome/OutcomeKnown are schema 2: the deciding (search) seat's game
	// result, 1 win / 0.5 draw / 0 loss, meaningful only when OutcomeKnown.
	// Absent in a schema 1 record, where they decode as zero/false.
	Outcome      float64 `json:"outcome"`
	OutcomeKnown bool    `json:"outcome_known"`
}

// labelCandidate mirrors cmd/searchteacher/labels.go's LabelCandidate.
type labelCandidate struct {
	Choices []int   `json:"choices"`
	Bot     bool    `json:"bot"`
	Value   float64 `json:"value"`
	Worlds  int     `json:"worlds"`
}

// Example is one loaded training example: the encoded state, the encoded
// offered options (parallel to the record's option list, each carrying its
// target), and the record's identifying metadata.
type Example struct {
	Pair      string
	GameIndex int
	Seed      uint64
	Sequence  uint64
	Seat      state.PlayerID
	Kind      decision.Kind
	Turn      int32
	// Margin is the record's margin: the best candidate mean minus the bot's
	// mean over EVERY candidate. TeacherChoice != 0 implies Margin > 0, but
	// not the converse (see the writer's doc).
	Margin        float64
	TeacherChoice int
	BotIndex      int
	// Outcome is the deciding seat's game result (1 win, 0.5 draw, 0 loss)
	// and is meaningful only when HasOutcome. HasOutcome is false for a
	// schema 1 record and for a game that stalled or errored.
	Outcome    float64
	HasOutcome bool
	// TeacherValue is the teacher-chosen candidate's rollout mean
	// (candidates[TeacherChoice].Value), the value head's second, lower-
	// variance target; HasTeacherValue is false when TeacherChoice is out of
	// the candidate range.
	TeacherValue    float64
	HasTeacherValue bool
	// TDTarget is a value target computed over the example's whole trajectory
	// (VisitTDTargets, cmd/policytrain -visits-td-lambda). When HasTDTarget is
	// set it IS the value head's target, whatever LossConfig.ValueBlend says;
	// false (every loader's default) keeps the blend.
	TDTarget    float64
	HasTDTarget bool
	State       State
	Options     []Option
	// PPO is the on-policy PPO target (ticket pn13, LoadOnPolicy): non-nil
	// makes the example train the PPO objective instead of the supervised
	// loss. nil for every label-corpus example.
	PPO *PPOTarget
	// Visits is the visit-distribution target (M1b, LoadVisits): non-nil
	// makes the example train the soft candidate cross-entropy against the
	// search's visits instead of the supervised loss. nil otherwise.
	Visits *VisitTarget
	// JointCard is the joint (card, target) action encoding's option map
	// (pn12, LoadWith with Joint): JointCard[k] is the index in the RECORD's
	// option list of the card Options[k] casts. nil for the split encoding,
	// where Options parallels the record's options one to one.
	JointCard []int
}

// Stats counts what Load saw.
type Stats struct {
	// Records is every label record read, skipped ones included.
	Records int
	// Skipped is the records whose candidates list was empty (the writer
	// never emits one, but a consumer must not train on them).
	Skipped int
	// WithOutcome is the loaded (not skipped) examples whose HasOutcome is
	// true.
	WithOutcome int
}

// Load streams a label corpus (plain or gzip-compressed JSONL, detected by
// magic bytes) into decoded, encoded Examples without holding the whole file
// in memory: records are read one at a time and encoded as they arrive.
//
// A record whose record_type is not LabelRecordType or whose schema_version
// is not in AcceptedLabelSchemaVersions is a hard error (a wrong-shape corpus must not train anything).
// A record whose candidates list is empty is skipped and counted in
// Stats.Skipped.
func Load(path string) ([]Example, Stats, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, Stats{}, fmt.Errorf("opening label corpus: %w", err)
	}
	defer f.Close()

	r := bufio.NewReader(f)
	var stream io.Reader = r
	if magic, err := r.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(r)
		if err != nil {
			return nil, Stats{}, fmt.Errorf("reading gzipped label corpus: %w", err)
		}
		defer zr.Close()
		stream = zr
	}

	dec := json.NewDecoder(stream)
	var out []Example
	var stats Stats
	for {
		var rec labelRecord
		if err := dec.Decode(&rec); err != nil {
			if err == io.EOF {
				break
			}
			return nil, stats, fmt.Errorf("label corpus record %d: %w", stats.Records+1, err)
		}
		stats.Records++
		if rec.RecordType != LabelRecordType {
			return nil, stats, fmt.Errorf("label corpus record %d: wrong record_type %q, want %q", stats.Records, rec.RecordType, LabelRecordType)
		}
		if !LabelSchemaAccepted(rec.SchemaVersion) {
			return nil, stats, fmt.Errorf("label corpus record %d: unsupported schema_version %d, want one of %v", stats.Records, rec.SchemaVersion, AcceptedLabelSchemaVersions)
		}
		if len(rec.Candidates) == 0 {
			stats.Skipped++
			continue
		}
		if len(rec.View) == 0 || bytes.Equal(bytes.TrimSpace(rec.View), []byte("null")) {
			return nil, stats, fmt.Errorf("label corpus record %d: missing view", stats.Records)
		}
		var v view.View
		if err := json.Unmarshal(rec.View, &v); err != nil {
			return nil, stats, fmt.Errorf("label corpus record %d: decoding view: %w", stats.Records, err)
		}
		targets := optionTargets(&rec)
		botPicks := botPicks(&rec)
		ex := Example{
			Pair:          rec.Pair,
			GameIndex:     rec.GameIndex,
			Seed:          rec.Seed,
			Sequence:      rec.Sequence,
			Seat:          rec.Seat,
			Kind:          rec.Kind,
			Turn:          rec.Turn,
			Margin:        rec.Margin,
			TeacherChoice: rec.TeacherChoice,
			BotIndex:      rec.BotIndex,
			State:         EncodeState(v, rec.Seat),
			Options:       make([]Option, len(rec.Options)),
		}
		if rec.SchemaVersion >= 2 && rec.OutcomeKnown {
			ex.Outcome, ex.HasOutcome = rec.Outcome, true
			stats.WithOutcome++
		}
		if rec.TeacherChoice >= 0 && rec.TeacherChoice < len(rec.Candidates) {
			ex.TeacherValue, ex.HasTeacherValue = rec.Candidates[rec.TeacherChoice].Value, true
		}
		for i := range rec.Options {
			ex.Options[i] = EncodeOption(v, rec.Seat, rec.Kind, rec.Options[i], i, len(rec.Options))
			ex.Options[i].Target = targets[i]
			ex.Options[i].BotPick = botPicks[i]
		}
		out = append(out, ex)
	}
	return out, stats, nil
}

// botPicks marks the options the BOT's own answer contains (candidate
// BotIndex, which the writer always emits first, so BotIndex is 0 by
// construction — but the field is read rather than assumed, so a future
// writer that reorders candidates cannot silently mark the wrong option).
// An out-of-range index marks nothing. Deterministic: one pass over the
// bot candidate's Choices, never a map.
func botPicks(rec *labelRecord) []bool {
	out := make([]bool, len(rec.Options))
	if rec.BotIndex < 0 || rec.BotIndex >= len(rec.Candidates) {
		return out
	}
	for _, j := range rec.Candidates[rec.BotIndex].Choices {
		if j >= 0 && j < len(out) {
			out[j] = true
		}
	}
	return out
}

// optionTargets computes one OptionTarget per offered option. The teacher's
// chosen candidate (candidates[teacher_choice], when in range) is visited
// FIRST, so an option the teacher's own answer contains takes the TEACHER's
// evaluated value and its Preferred mask describes that same candidate;
// the remaining candidates then fill in any still-unlabelled option in
// record order. This matters because candidate 0 is always the bot's answer
// (the writer's contract), so a plain record-order first-wins would hand a
// teacher-preferred option the bot's value and leave Target.Value and
// Target.Preferred describing different candidates. Deterministic — record
// order, never a map.
func optionTargets(rec *labelRecord) []OptionTarget {
	out := make([]OptionTarget, len(rec.Options))
	take := func(c labelCandidate) {
		for _, j := range c.Choices {
			if j < 0 || j >= len(out) {
				continue
			}
			if !out[j].Labelled {
				out[j] = OptionTarget{Labelled: true, Value: c.Value}
			}
		}
	}
	preferred := -1
	if rec.TeacherChoice >= 0 && rec.TeacherChoice < len(rec.Candidates) {
		preferred = rec.TeacherChoice
		take(rec.Candidates[preferred])
	}
	for i := range rec.Candidates {
		if i == preferred {
			continue
		}
		take(rec.Candidates[i])
	}
	if preferred >= 0 {
		for _, j := range rec.Candidates[preferred].Choices {
			if j >= 0 && j < len(out) {
				out[j].Preferred = true
			}
		}
	}
	return out
}
