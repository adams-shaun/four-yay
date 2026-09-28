package policynet

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// The visit corpus (M1b, AZ spec ticket 3's "azgen" corpus): one JSONL
// record per decision the az seat SEARCHED, written by cmd/botbench
// -az-corpus and read by LoadVisits for cmd/policytrain -visits-corpus. It
// is the teacher-student distillation corpus: the TARGET is the search's
// visit distribution (whatever world source the teacher searched --
// clairvoyant clones of the real engine, or honest sampled worlds), while
// the INPUTS are the searching seat's redacted view only, encoded under a
// checkpointable feature set (mz). The one exception is the diagnostic
// diag_rows field below, which a student may read only under the
// measurement-only FeaturesMZOppHand set (never checkpointed).
//
// Schema (record_type "azvisits-v1", schema_version 1), one object per line:
//
//	record_type, schema_version  "azvisits-v1", 1
//	encoder_hash                 EncoderHashFor(features), %016x (mz or entity)
//	world, sims                  the teacher's world source and budget
//	game_id, deck, seed          the game (SpellBench schedule id), its deck and engine seed
//	seat, opponent               the recording seat's index and the opponent policy
//	kind, turn, sequence         decision kind, game turn, decision Seq
//	subset                       attackers/blockers: a candidate is an option SET
//	state                        the encoded redacted state (OnPolicyState)
//	diag_rows, diag_vals         the sparse rows FeaturesMZOppHand adds to the
//	                             mz state (the opponent's hand); diagnostic only
//	options[]                    the encoded options any candidate references
//	                             (OnPolicyOption), in decision order
//	cands[]                      per candidate, its option POSITIONS in options[];
//	                             candidate 0 is the default bot's answer
//	visits, prior, q             per candidate: root visit count, the search's
//	                             prior (before root noise), mean value
//	root_value, choice           the root's mean value; the candidate played
//	outcome, outcome_known       the recording seat's result (1, 0.5, 0);
//	                             unknown for a truncated or halted game
const (
	VisitRecordType    = "azvisits-v1"
	VisitSchemaVersion = 1
)

// VisitRecord is one searched decision of the visit corpus.
type VisitRecord struct {
	RecordType    string           `json:"record_type"`
	SchemaVersion int              `json:"schema_version"`
	EncoderHash   string           `json:"encoder_hash"`
	World         string           `json:"world"`
	Sims          int              `json:"sims"`
	GameID        string           `json:"game_id"`
	Deck          string           `json:"deck"`
	Seed          uint64           `json:"seed"`
	Seat          int              `json:"seat"`
	Opponent      string           `json:"opponent"`
	Kind          decision.Kind    `json:"kind"`
	Turn          int32            `json:"turn"`
	Sequence      uint64           `json:"sequence"`
	Subset        bool             `json:"subset"`
	State         OnPolicyState    `json:"state"`
	DiagRows      []uint16         `json:"diag_rows,omitempty"`
	DiagVals      []float32        `json:"diag_vals,omitempty"`
	Options       []OnPolicyOption `json:"options"`
	Cands         [][]int          `json:"cands"`
	Visits        []int            `json:"visits"`
	Prior         []float64        `json:"prior"`
	Q             []float64        `json:"q"`
	RootValue     float64          `json:"root_value"`
	Choice        int              `json:"choice"`
	Outcome       float64          `json:"outcome"`
	OutcomeKnown  bool             `json:"outcome_known"`
}

// VisitTarget is a visit-corpus example's policy target: a distribution Pi
// over candidate answers, each candidate a set of option positions
// (Example.Options indices). A single-choice kind's candidate is one option.
type VisitTarget struct {
	Cands [][]int
	Pi    []float64
	// Teacher is the teacher's argmax candidate (most visits, ties on the
	// lowest index, which is the bot's answer): the top-1 readout's label.
	Teacher int
}

// EncodeDiagRows is the sparse rows the diagnostic feature set fs adds to
// the checkpointable mz state for seat's view v and the hidden information
// diag -- exactly what EncodeStateWith(fs, v, seat, diag) appends beyond
// EncodeStateWith(FeaturesMZ, v, seat, nil). Nil for a non-diagnostic fs or
// a nil diag. A recorder stores them beside the redacted state so a
// measurement-only student can read the opponent's hand; no seat ever can.
func EncodeDiagRows(fs FeatureSet, v view.View, seat state.PlayerID, diag *Diag) []Feature {
	if !fs.Diagnostic() || diag == nil {
		return nil
	}
	var extra []Feature
	mzDiag(v, seat, fs, diag, func(s string, val float32) { extra = append(extra, Feature{Row: hashID(s), Value: val}) })
	return extra
}

// VisitLabel selects which policy target LoadVisits builds.
type VisitLabel string

const (
	// VisitLabelTeacher is the search's visit distribution (distillation).
	VisitLabelTeacher VisitLabel = "teacher"
	// VisitLabelBot is a one-hot on candidate 0, the default bot's answer:
	// behaviour cloning of bot on exactly the same states and candidates.
	VisitLabelBot VisitLabel = "bot"
)

// ParseVisitLabel maps a flag spelling onto a VisitLabel.
func ParseVisitLabel(s string) (VisitLabel, error) {
	switch VisitLabel(s) {
	case VisitLabelTeacher, VisitLabelBot:
		return VisitLabel(s), nil
	}
	return "", fmt.Errorf("unknown visit label %q (want teacher or bot)", s)
}

// VisitLoadOptions are LoadVisits' knobs.
type VisitLoadOptions struct {
	Label VisitLabel
	// Diag loads the state under FeaturesMZOppHand (the stored diag rows
	// merged in): a measurement-only student. The corpus must be mz.
	Diag bool
	// MaxGames > 0 keeps only records from the first MaxGames distinct games
	// in file order (the equal-data-volume cap).
	MaxGames int
	// Temp sharpens the teacher target: pi_c ∝ visits_c^(1/Temp). 0 or 1 is
	// the raw visit distribution; a small Temp approaches the argmax.
	Temp float64
}

// VisitStats counts what LoadVisits saw.
type VisitStats struct {
	Records, Loaded, Games int
	WithOutcome            int
	// Overrides counts records whose teacher argmax is not the bot's answer.
	Overrides int
	Features  FeatureSet
	World     string
	Sims      int
}

// VisitTeacher is the teacher's argmax candidate over visits, ties on the
// lowest index (the bot's answer wins ties, as in the search's choose).
func VisitTeacher(visits []int) int {
	best := 0
	for i, v := range visits {
		if v > visits[best] {
			best = i
		}
	}
	return best
}

// Example decodes the record into a training example under label.
// Options are all Labelled (the corpus stores only the options a candidate
// references); the teacher argmax candidate's options are marked Preferred
// so the generic per-kind readouts stay meaningful. TeacherValue is the
// root value (the TD half of the value target).
func (rec *VisitRecord) Example(label VisitLabel, diag bool, temp float64) (Example, error) {
	ex := Example{
		// Pair/Seed/GameIndex group a by-game holdout: both games of a
		// seat-swapped SpellBench pair share one seed and deck (common random
		// numbers), so the PAIR is the unit, never one of its games.
		Pair: rec.Deck, Seed: rec.Seed, GameIndex: 0, Sequence: rec.Sequence,
		Seat: state.PlayerID(rec.Seat), Kind: rec.Kind, Turn: rec.Turn,
		Outcome: rec.Outcome, HasOutcome: rec.OutcomeKnown,
		TeacherValue: rec.RootValue, HasTeacherValue: true,
	}
	n := len(rec.Cands)
	if n < 2 || len(rec.Visits) != n {
		return ex, fmt.Errorf("%d candidates with %d visit counts", n, len(rec.Visits))
	}
	sp, err := joinFeatures(rec.State.Rows, rec.State.Vals)
	if err != nil {
		return ex, fmt.Errorf("state: %w", err)
	}
	if diag {
		extra, err := joinFeatures(rec.DiagRows, rec.DiagVals)
		if err != nil {
			return ex, fmt.Errorf("diag rows: %w", err)
		}
		sp = append(sp, extra...)
		sortFeaturesStable(sp)
	}
	ex.State = State{Dense: append([]float32(nil), rec.State.Dense...), Sparse: sp}
	for i, c := range rec.State.Cards {
		rows, err := joinFeatures(c.Rows, c.Vals)
		if err != nil {
			return ex, fmt.Errorf("state card %d: %w", i, err)
		}
		ex.State.Cards = append(ex.State.Cards, EntityCard{Group: c.Group, Raw: append([]float32(nil), c.Raw...), Rows: rows})
	}
	ex.Options = make([]Option, len(rec.Options))
	for i, o := range rec.Options {
		slots, err := joinFeatures(o.SlotsR, o.SlotsV)
		if err != nil {
			return ex, fmt.Errorf("option %d slots: %w", i, err)
		}
		hashed, err := joinFeatures(o.HashedR, o.HashedV)
		if err != nil {
			return ex, fmt.Errorf("option %d hashed: %w", i, err)
		}
		ex.Options[i] = Option{Slots: slots, Hashed: hashed, Dense: append([]float32(nil), o.Dense...), BotPick: o.BotPick,
			EntA: o.EntA, EntB: o.EntB, Target: OptionTarget{Labelled: true}}
	}
	vt := &VisitTarget{Cands: make([][]int, n), Pi: make([]float64, n)}
	total := 0
	for c, opts := range rec.Cands {
		if !rec.Subset && len(opts) != 1 {
			return ex, fmt.Errorf("single-choice candidate %d has %d options", c, len(opts))
		}
		for _, p := range opts {
			if p < 0 || p >= len(ex.Options) {
				return ex, fmt.Errorf("candidate %d option position %d outside %d options", c, p, len(ex.Options))
			}
		}
		vt.Cands[c] = append([]int(nil), opts...)
		if rec.Visits[c] < 0 {
			return ex, fmt.Errorf("candidate %d has %d visits", c, rec.Visits[c])
		}
		total += rec.Visits[c]
	}
	vt.Teacher = VisitTeacher(rec.Visits)
	switch label {
	case VisitLabelBot:
		vt.Pi[0] = 1
	default:
		if total == 0 {
			return ex, errors.New("no candidate was visited")
		}
		if temp <= 0 || temp == 1 {
			for c := range vt.Pi {
				vt.Pi[c] = float64(rec.Visits[c]) / float64(total)
			}
			break
		}
		// Normalise by the max first so the power cannot overflow.
		hi := float64(rec.Visits[VisitTeacher(rec.Visits)])
		sum := 0.0
		for c := range vt.Pi {
			if rec.Visits[c] > 0 {
				vt.Pi[c] = math.Pow(float64(rec.Visits[c])/hi, 1/temp)
				sum += vt.Pi[c]
			}
		}
		for c := range vt.Pi {
			vt.Pi[c] /= sum
		}
	}
	for _, p := range vt.Cands[vt.Teacher] {
		ex.Options[p].Target.Preferred = true
	}
	ex.TeacherChoice, ex.BotIndex = vt.Teacher, 0
	ex.Visits = vt
	return ex, nil
}

// sortFeaturesStable sorts rows ascending, stably (EncodeStateWith's order).
func sortFeaturesStable(fs []Feature) {
	// insertion sort keeps the dependency surface tiny; the diag rows are few
	// and the base rows already sorted.
	for i := 1; i < len(fs); i++ {
		for j := i; j > 0 && fs[j].Row < fs[j-1].Row; j-- {
			fs[j], fs[j-1] = fs[j-1], fs[j]
		}
	}
}

// LoadVisits reads a visit corpus (plain or gzip JSONL) into examples, in
// file order. A wrong record type or schema version, an encoder hash no
// checkpointable feature set produces, or a corpus mixing feature sets, is a
// hard error. The loaded examples' Features (stats.Features) is the corpus
// set, or FeaturesMZOppHand under o.Diag.
func LoadVisits(path string, o VisitLoadOptions) ([]Example, []VisitRecord, VisitStats, error) {
	var stats VisitStats
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, stats, fmt.Errorf("opening visit corpus: %w", err)
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var stream io.Reader = r
	if magic, err := r.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(r)
		if err != nil {
			return nil, nil, stats, fmt.Errorf("reading gzipped visit corpus: %w", err)
		}
		defer zr.Close()
		stream = zr
	}
	label := o.Label
	if label == "" {
		label = VisitLabelTeacher
	}
	dec := json.NewDecoder(stream)
	want := ""
	games := map[string]bool{} // membership only
	var out []Example
	var recs []VisitRecord
	for {
		var rec VisitRecord
		if err := dec.Decode(&rec); err != nil {
			if err == io.EOF {
				break
			}
			return nil, nil, stats, fmt.Errorf("visit record %d: %w", stats.Records+1, err)
		}
		stats.Records++
		switch {
		case rec.RecordType != VisitRecordType:
			return nil, nil, stats, fmt.Errorf("visit record %d: record_type %q, want %q", stats.Records, rec.RecordType, VisitRecordType)
		case rec.SchemaVersion != VisitSchemaVersion:
			return nil, nil, stats, fmt.Errorf("visit record %d: schema_version %d, want %d", stats.Records, rec.SchemaVersion, VisitSchemaVersion)
		case want != "" && rec.EncoderHash != want:
			return nil, nil, stats, fmt.Errorf("visit record %d: encoder hash %s, earlier records' is %s", stats.Records, rec.EncoderHash, want)
		}
		if want == "" {
			fs, ok := onPolicyFeatures(rec.EncoderHash)
			if !ok {
				return nil, nil, stats, fmt.Errorf("visit record %d: encoder hash %s is not a checkpointable feature set of this build", stats.Records, rec.EncoderHash)
			}
			if o.Diag && fs != FeaturesMZ {
				return nil, nil, stats, fmt.Errorf("the diagnostic opp-hand student needs an mz corpus, this one is %s", fs)
			}
			want, stats.Features, stats.World, stats.Sims = rec.EncoderHash, fs, rec.World, rec.Sims
			if o.Diag {
				stats.Features = FeaturesMZOppHand
			}
		}
		gk := fmt.Sprintf("%s/%d", rec.GameID, rec.Seed) // ids repeat across base seeds
		if !games[gk] {
			if o.MaxGames > 0 && len(games) >= o.MaxGames {
				continue
			}
			games[gk] = true
		}
		ex, err := rec.Example(label, o.Diag, o.Temp)
		if err != nil {
			return nil, nil, stats, fmt.Errorf("visit record %d: %w", stats.Records, err)
		}
		stats.Loaded++
		if rec.OutcomeKnown {
			stats.WithOutcome++
		}
		if ex.Visits.Teacher != 0 {
			stats.Overrides++
		}
		out = append(out, ex)
		rec.State, rec.Options, rec.DiagRows, rec.DiagVals = OnPolicyState{}, nil, nil, nil
		recs = append(recs, rec)
	}
	stats.Games = len(games)
	return out, recs, stats, nil
}

// CandidateLogits are the visit loss's candidate logits from the per-option
// scores ys (parallel to Example.Options): a candidate's logit is the SUM of
// its options' scores. For a single-choice kind that is the one option's
// score; for a subset kind it is CandidateScore's Bernoulli log-likelihood
// up to a constant shared by every candidate (log sigmoid(s) - log(1 -
// sigmoid(s)) = s), so the softmax over candidates is exactly the search
// prior's (azmcts priors) and the student's greedy pick.
func CandidateLogits(cands [][]int, ys []float64) []float64 {
	z := make([]float64, len(cands))
	for c, opts := range cands {
		for _, p := range opts {
			z[c] += ys[p]
		}
	}
	return z
}

// lossVisits is the soft cross-entropy of the candidate softmax against the
// visit distribution: sum_c pi_c (LSE(z) - z_c), whose logit gradient is
// q_c - pi_c, routed to every option of candidate c. labelled must be every
// option position (the loader labels them all); ys parallel labelled.
func lossVisits(ex Example, labelled []int, ys []float64) (parts LossParts, dys []float64) {
	full := make([]float64, len(ex.Options))
	pos := make([]int, len(ex.Options))
	for i := range pos {
		pos[i] = -1
	}
	for k, i := range labelled {
		full[i] = clampScore(ys[k])
		pos[i] = k
	}
	vt := ex.Visits
	z := CandidateLogits(vt.Cands, full)
	lse := logSumExp(z)
	dys = make([]float64, len(labelled))
	for c := range z {
		q := math.Exp(z[c] - lse)
		if vt.Pi[c] > 0 {
			parts.Rank += vt.Pi[c] * (lse - z[c])
		}
		g := q - vt.Pi[c]
		for _, p := range vt.Cands[c] {
			if k := pos[p]; k >= 0 {
				dys[k] += g
			}
		}
	}
	parts.Total = parts.Rank
	return parts, dys
}

// visitAgreement is the visit example's top-1 readout: the candidate
// softmax's argmax (ties on the lowest index) equals the teacher's argmax.
func visitAgreement(ex Example, labelled []int, ys []float64) (eligible, agree bool) {
	full := make([]float64, len(ex.Options))
	for k, i := range labelled {
		full[i] = ys[k]
	}
	z := CandidateLogits(ex.Visits.Cands, full)
	best := 0
	for c := range z {
		if z[c] > z[best] {
			best = c
		}
	}
	return true, best == ex.Visits.Teacher
}

// CandidateProbs is the model's candidate distribution for a visit example.
func (m *Model) CandidateProbs(ex Example) []float64 {
	ys := m.Score(ex.State, ex.Options)
	full := make([]float64, len(ys))
	for i, y := range ys {
		full[i] = clampScore(float64(y))
	}
	z := CandidateLogits(ex.Visits.Cands, full)
	lse := logSumExp(z)
	p := make([]float64, len(z))
	for c := range z {
		p[c] = math.Exp(z[c] - lse)
	}
	return p
}
