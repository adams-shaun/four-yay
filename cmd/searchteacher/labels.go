package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/traceboard"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// labelSchemaVersion is the schema this writer emits. Schema 2 added
// Outcome/OutcomeKnown; the record type stays "label-v1" (the record's shape
// grew, its meaning did not change), and internal/policynet still loads
// schema 1 corpora.
const labelSchemaVersion = 2

// LabelRecord is one search-teacher decision the teacher COVERED (it scored
// every candidate on worlds and produced values): the deciding seat's board
// and view snapshots, the full offered options, the evaluated candidates and
// the teacher's verdict. Written by -labels, one JSON object per line,
// sorted by pair, game index, decision sequence — so a corpus built with any
// -workers value is byte-identical for fixed flags. Timing is deliberately
// absent (wall-clock has no place in a training corpus; the -out
// GameRecords carry it).
type LabelRecord struct {
	RecordType    string         `json:"record_type"`
	SchemaVersion int            `json:"schema_version"`
	Pair          string         `json:"pair"`
	GameIndex     int            `json:"game_index"`
	Seed          uint64         `json:"seed"`
	Sequence      uint64         `json:"decision_sequence"`
	Seat          state.PlayerID `json:"seat"`
	Kind          decision.Kind  `json:"kind"`
	Turn          int32          `json:"turn"`
	// Board is the deciding seat's redacted board snapshot in botbench's
	// decision-trace board schema (internal/traceboard, board_schema_version
	// 1) — the SAME type the trace records decode into.
	Board traceboard.Board `json:"board"`
	// View is the deciding seat's own view.Project JSON with Decision set to
	// nil (engine continuation state is never serialized; the offered
	// options live in Options below). Redacted by view's CR 400.2 rules: the
	// opponent's hand is null and no opponent hand card name appears.
	View json.RawMessage `json:"view"`
	// Options is the full offered decision.Option list (every wire field:
	// Obj, Player, Attacker, AltCostIndex, Mode, Amount, Ability, plus
	// Label/Required/Group/SVar/Cost — everything a seat itself was offered).
	Options []decision.Option `json:"options"`
	// Candidates is the evaluated candidate set in teacher order: index 0 is
	// always the bot's answer.
	Candidates []LabelCandidate `json:"candidates"`
	// TeacherChoice is the candidate the teacher chose: the argmax candidate
	// index when its mean beats the bot's answer by more than the run's
	// -margin, else 0 (the bot's answer kept). BotIndex is the candidate
	// index carrying the bot's answer (0 by construction).
	//
	// Margin is the BEST candidate mean minus the bot's mean, over EVERY
	// candidate and independent of TeacherChoice. In particular
	// TeacherChoice == 0 does NOT imply Margin == 0: at -margin > 0 a
	// candidate may have a strictly higher mean than the bot's answer, and so
	// a nonzero Margin, while still not clearing the threshold, so the
	// teacher keeps the bot (TeacherChoice == 0). The only direction that
	// holds is TeacherChoice != 0 ⟹ Margin > 0.
	TeacherChoice int     `json:"teacher_choice"`
	BotIndex      int     `json:"bot_index"`
	Margin        float64 `json:"margin"`
	// Worlds is K, the number of sampled worlds every candidate was rolled on;
	// Attempts/Accepted are this decision's sampler proposal attempts and
	// acceptances; Horizon is the rollout horizon in engine turns (0 = game
	// end). Under -oracle NO WORLD IS SAMPLED: Worlds is 1, Attempts and
	// Accepted are 0, and the single world is a CHEATING clone of the actual
	// engine, so a consumer that filters on Attempts > 0 distinguishes the
	// sampled corpus from the oracle ceiling.
	Worlds   int   `json:"worlds"`
	Attempts int   `json:"attempts"`
	Accepted int   `json:"accepted"`
	Horizon  int32 `json:"horizon"`
	// Outcome is the deciding seat's result in the SEARCH game this decision
	// was taken in: 1 win, 0.5 draw, 0 loss. Labels are emitted only for the
	// search seat's own decisions in the search game, so the outcome is always
	// that seat's. OutcomeKnown is false when the search game has no result --
	// it stalled at the turn/step cap or errored -- and Outcome is then 0 and
	// must be ignored. Both are filled in collectLabels once every game has
	// finished (schema 2).
	Outcome      float64 `json:"outcome"`
	OutcomeKnown bool    `json:"outcome_known"`
	// Extras is the pn12 extension (schema 3, -label-extras only); nil and
	// omitted otherwise, so a schema 2 corpus is byte-identical to before.
	Extras *policynet.LabelExtras `json:"extras,omitempty"`
	// RescoreValues (-rescore-worlds only, measurement) are the same
	// candidates' means from an independent search with RescoreWorlds worlds
	// and a different sample seed, recorded after the label is fixed. Nil
	// when the flag is off or that search could not cover the decision.
	RescoreValues []float64 `json:"rescore_values,omitempty"`
	RescoreWorlds int       `json:"rescore_worlds,omitempty"`
}

// LabelCandidate is one evaluated candidate answer: its option set (the
// option indices it selects in Options), its mean rollout value from the
// deciding seat's point of view, the number of worlds it was evaluated on,
// and whether it is the bot's answer.
type LabelCandidate struct {
	Choices []int   `json:"choices"`
	Bot     bool    `json:"bot"`
	Value   float64 `json:"value"`
	Worlds  int     `json:"worlds"`
}

// seatView projects the deciding seat's own view with the decision omitted
// (the internal/searchprobe Capture pattern: never serialize engine
// continuation state).
func seatView(e *rules.Engine, d *decision.Decision) (json.RawMessage, error) {
	v := view.Project(e.G, e, d.Player, d)
	v.Decision = nil
	v.Round = view.RoundOf(e.G, e.L.Events)
	return json.Marshal(v)
}

// checkLabelsDestination fails fast on a -labels path that cannot be
// published, before any game is played.
func checkLabelsDestination(path string) error {
	parent := filepath.Dir(path)
	info, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("label corpus parent: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("label corpus parent %q is not a directory", parent)
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("label corpus destination %q already exists", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking label corpus destination: %w", err)
	}
	return nil
}

// collectLabels flattens the games' label records in the published order:
// pair (lexicographic), game index, decision sequence. Each game is played
// by one goroutine, so its records are already in sequence order; the sort
// is stable belt-and-braces so the corpus contract does not depend on it.
// Every record is stamped with its own game's search outcome (searchOutcome)
// before sorting.
func collectLabels(all []GameRecord) []LabelRecord {
	var out []LabelRecord
	for _, r := range all {
		outcome, known := searchOutcome(r)
		for _, l := range r.Labels {
			l.Outcome, l.OutcomeKnown = outcome, known
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Pair != b.Pair {
			return a.Pair < b.Pair
		}
		if a.GameIndex != b.GameIndex {
			return a.GameIndex < b.GameIndex
		}
		return a.Sequence < b.Sequence
	})
	return out
}

// searchOutcome is the search seat's result in r's search game: 1 win, 0.5
// draw, 0 loss, and known=false (outcome 0) for a game that errored or
// stalled without finishing.
func searchOutcome(r GameRecord) (outcome float64, known bool) {
	switch {
	case r.Error != "" || !r.SearchOver:
		return 0, false
	case r.SearchDraw:
		return 0.5, true
	case r.SearchWon:
		return 1, true
	default:
		return 0, true
	}
}

// writeLabels publishes the corpus to a NEW file atomically: every record is
// encoded into a sibling temporary file, synced, closed, and then linked into
// place without ever overwriting an existing destination — the same pattern
// botbench's decision trace uses (publishTraceNoReplace there).
func writeLabels(path string, records []LabelRecord) (err error) {
	if path == "" {
		return nil
	}
	if err := checkLabelsDestination(path); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("creating label corpus temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()
	var sink io.Writer = tmp
	var gz *gzip.Writer
	if strings.HasSuffix(path, ".gz") {
		// Deterministic: a zero Header (no name, zero ModTime) and a fixed
		// level, so the same records compress to the same bytes.
		gz, _ = gzip.NewWriterLevel(tmp, gzip.BestSpeed)
		sink = gz
	}
	enc := json.NewEncoder(sink)
	for i := range records {
		r := records[i]
		wantSchema := labelSchemaVersion
		if r.Extras != nil {
			wantSchema = labelSchemaExtras
		}
		if r.RecordType != "label-v1" || r.SchemaVersion != wantSchema {
			return fmt.Errorf("invalid label record type %q / schema %d", r.RecordType, r.SchemaVersion)
		}
		if r.Board.SchemaVersion != traceboard.SchemaVersion {
			return fmt.Errorf("label board schema version %d is unsupported", r.Board.SchemaVersion)
		}
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("writing label corpus: %w", err)
		}
	}
	if gz != nil {
		if err = gz.Close(); err != nil {
			return fmt.Errorf("compressing label corpus: %w", err)
		}
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("syncing label corpus: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("closing label corpus: %w", err)
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("label corpus destination %q appeared during run", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("rechecking label corpus destination: %w", err)
	}
	if err := os.Link(tmpName, path); err != nil {
		return fmt.Errorf("publishing label corpus: %w", err)
	}
	if err := os.Remove(tmpName); err != nil {
		// Publication is all-or-nothing: if retiring the temporary name
		// fails, remove the published name and surface the cleanup error.
		if rollbackErr := os.Remove(path); rollbackErr != nil {
			return fmt.Errorf("removing published label corpus after temporary cleanup failed: %v (cleanup error: %w)", rollbackErr, err)
		}
		return fmt.Errorf("publishing label corpus: %w", err)
	}
	return nil
}
