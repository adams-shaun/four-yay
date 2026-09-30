package policynet

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/adams-shaun/gorge/decision"
)

// The on-policy corpus (ticket pn13): one JSONL record per decision the
// deployed policynet seat actually SCORED (seat.PolicyNetBot; a decision the
// seat delegated to the default bot is not recorded), written by
// cmd/botbench -onpolicy-corpus and read by LoadOnPolicy for cmd/policytrain
// -ppo. Unlike the label corpus it stores the ENCODED state and options
// (the exact floats the seat scored, so a checkpoint's old scores can be
// recomputed bit for bit — the trainer's π_old consistency check), guarded
// by the encoder hash.
//
// Schema (record_type "onpolicy-v1", schema_version 1), one object per line:
//
//	record_type, schema_version  "onpolicy-v1", 1
//	encoder_hash                 EncoderHash() of the writer, %016x
//	pair, pair_index             botbench deck pair "a:b" and its matrix position
//	game_index, seed             game within the pair and its engine seed
//	seat, deck                   the recording seat's index and deck name
//	kind, turn, sequence         decision kind, view turn, decision Seq
//	subset                       true: per-option Bernoulli π (attackers,
//	                             blockers); false: softmax over in-space options
//	state                        {"dense":[68 floats],"rows":[...],"vals":[...]}
//	options[]                    {"slots_r","slots_v","hashed_r","hashed_v",
//	                              "dense":[24 floats],"bot_pick","in_space"}
//	scores                       π_old full scores (head + residual), per option
//	chosen                       option POSITIONS the seat answered
//	bot_chosen                   option positions of the default bot's answer
//	deviated                     chosen differs from bot_chosen (as sets)
//	logp_old                     log π_old(chosen) (PPOLogProb)
//	value_old, has_value_old     V(s) of the scoring checkpoint, when it has a value head
//	outcome, outcome_known       the recording seat's result: 1 win, 0.5 draw,
//	                             0 loss; unknown for a stalled game
//	admission                    the subset kinds' admission vote the seat
//	                             answered with: "auto" or "sign"
//	                             (seat.SetAdmission)
//
// Optional fields (ticket pn14; omitted when unset, so a greedy v1/mz corpus
// is byte-identical to what the pn13 writer produced):
//
//	temperature                  the sampling temperature T of a stochastic
//	                             collection seat (seat.SetSampling)
//	logp_behaviour               log π_b(chosen): the probability the SAMPLING
//	                             policy gave the recorded answer (TemperedLogProb);
//	                             PPO's ratio reads it instead of logp_old
//	state.cards                  the entity list (FeaturesEntity):
//	                             {"g":group,"raw":[68 floats],"r":[rows],"v":[vals]}
//	options[].ent_a / ent_b      the option's entity references (1 + card index)
//
// The encoder hash is EncoderHashFor(the recording checkpoint's feature set):
// v1 (EncoderHash), mz or entity; LoadOnPolicy accepts any checkpointable set
// and reports which (OnPolicyStats.Features).
//
// Record order is (pair_index, game_index, decision order): deterministic for
// a fixed bench, independent of the worker count.
const (
	OnPolicyRecordType    = "onpolicy-v1"
	OnPolicySchemaVersion = 1
)

// OnPolicyRecord is one scored decision of the on-policy corpus.
type OnPolicyRecord struct {
	RecordType    string           `json:"record_type"`
	SchemaVersion int              `json:"schema_version"`
	EncoderHash   string           `json:"encoder_hash"`
	Pair          string           `json:"pair"`
	PairIndex     int              `json:"pair_index"`
	GameIndex     int              `json:"game_index"`
	Seed          uint64           `json:"seed"`
	Seat          int              `json:"seat"`
	Deck          string           `json:"deck"`
	Kind          decision.Kind    `json:"kind"`
	Turn          int32            `json:"turn"`
	Sequence      uint64           `json:"sequence"`
	Subset        bool             `json:"subset"`
	State         OnPolicyState    `json:"state"`
	Options       []OnPolicyOption `json:"options"`
	Scores        []float32        `json:"scores"`
	Chosen        []int            `json:"chosen"`
	BotChosen     []int            `json:"bot_chosen"`
	Deviated      bool             `json:"deviated"`
	LogPOld       float64          `json:"logp_old"`
	ValueOld      float32          `json:"value_old"`
	HasValueOld   bool             `json:"has_value_old"`
	Outcome       float64          `json:"outcome"`
	OutcomeKnown  bool             `json:"outcome_known"`
	Admission     string           `json:"admission"`
	Temperature   float64          `json:"temperature,omitempty"`
	LogPBehaviour *float64         `json:"logp_behaviour,omitempty"`
}

// OnPolicyState is an encoded State in the corpus's compact form.
type OnPolicyState struct {
	Dense []float32      `json:"dense"`
	Rows  []uint16       `json:"rows"`
	Vals  []float32      `json:"vals"`
	Cards []OnPolicyCard `json:"cards,omitempty"`
}

// OnPolicyCard is an EntityCard in the corpus's compact form.
type OnPolicyCard struct {
	Group uint8     `json:"g"`
	Raw   []float32 `json:"raw"`
	Rows  []uint16  `json:"r,omitempty"`
	Vals  []float32 `json:"v,omitempty"`
}

// OnPolicyOption is an encoded Option in the corpus's compact form, plus the
// residual mark and whether the option is in the scored action space.
type OnPolicyOption struct {
	SlotsR  []uint16  `json:"slots_r"`
	SlotsV  []float32 `json:"slots_v"`
	HashedR []uint16  `json:"hashed_r"`
	HashedV []float32 `json:"hashed_v"`
	Dense   []float32 `json:"dense"`
	BotPick bool      `json:"bot_pick"`
	InSpace bool      `json:"in_space"`
	EntA    int32     `json:"ent_a,omitempty"`
	EntB    int32     `json:"ent_b,omitempty"`
}

func splitFeatures(fs []Feature) ([]uint16, []float32) {
	r := make([]uint16, len(fs))
	v := make([]float32, len(fs))
	for i, f := range fs {
		r[i], v[i] = f.Row, f.Value
	}
	return r, v
}

func joinFeatures(r []uint16, v []float32) ([]Feature, error) {
	if len(r) != len(v) {
		return nil, fmt.Errorf("feature rows %d != values %d", len(r), len(v))
	}
	out := make([]Feature, len(r))
	for i := range r {
		out[i] = Feature{Row: r[i], Value: v[i]}
	}
	return out, nil
}

// EncodeOnPolicyState converts a State to the corpus form.
func EncodeOnPolicyState(st State) OnPolicyState {
	r, v := splitFeatures(st.Sparse)
	out := OnPolicyState{Dense: append([]float32(nil), st.Dense...), Rows: r, Vals: v}
	for _, c := range st.Cards {
		cr, cv := splitFeatures(c.Rows)
		out.Cards = append(out.Cards, OnPolicyCard{Group: c.Group, Raw: append([]float32(nil), c.Raw...), Rows: cr, Vals: cv})
	}
	return out
}

// EncodeOnPolicyOption converts an Option (with its BotPick mark) to the
// corpus form; inSpace marks it part of the scored action space.
func EncodeOnPolicyOption(o Option, inSpace bool) OnPolicyOption {
	sr, sv := splitFeatures(o.Slots)
	hr, hv := splitFeatures(o.Hashed)
	return OnPolicyOption{SlotsR: sr, SlotsV: sv, HashedR: hr, HashedV: hv,
		Dense: append([]float32(nil), o.Dense...), BotPick: o.BotPick, InSpace: inSpace, EntA: o.EntA, EntB: o.EntB}
}

// Example decodes the record into a PPO training example: the encoded state
// and options (in-space options Labelled), the old scores and chosen mask as
// the PPO target, and the outcome. The advantage is left 0 for the trainer.
func (rec *OnPolicyRecord) Example() (Example, error) {
	ex := Example{
		Pair:      rec.Pair,
		GameIndex: rec.GameIndex,
		Seed:      rec.Seed,
		Sequence:  rec.Sequence,
		Kind:      rec.Kind,
		Turn:      rec.Turn,
		Outcome:   rec.Outcome, HasOutcome: rec.OutcomeKnown,
	}
	sp, err := joinFeatures(rec.State.Rows, rec.State.Vals)
	if err != nil {
		return ex, fmt.Errorf("state: %w", err)
	}
	ex.State = State{Dense: append([]float32(nil), rec.State.Dense...), Sparse: sp}
	for i, c := range rec.State.Cards {
		rows, err := joinFeatures(c.Rows, c.Vals)
		if err != nil {
			return ex, fmt.Errorf("state card %d: %w", i, err)
		}
		if len(c.Raw) != EntityRawWidth {
			return ex, fmt.Errorf("state card %d: %d raw floats, want %d", i, len(c.Raw), EntityRawWidth)
		}
		ex.State.Cards = append(ex.State.Cards, EntityCard{Group: c.Group, Raw: append([]float32(nil), c.Raw...), Rows: rows})
	}
	if len(rec.Scores) != len(rec.Options) {
		return ex, fmt.Errorf("%d scores for %d options", len(rec.Scores), len(rec.Options))
	}
	p := &PPOTarget{Subset: rec.Subset, OldScores: append([]float32(nil), rec.Scores...), Chosen: make([]bool, len(rec.Options)),
		SignVote: rec.Admission == "sign"}
	if rec.LogPBehaviour != nil {
		p.BehaviourLogP, p.HasBehaviour = *rec.LogPBehaviour, true
	}
	for _, c := range rec.Chosen {
		if c < 0 || c >= len(rec.Options) {
			return ex, fmt.Errorf("chosen position %d outside %d options", c, len(rec.Options))
		}
		p.Chosen[c] = true
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
			EntA: o.EntA, EntB: o.EntB, Target: OptionTarget{Labelled: o.InSpace, Preferred: false}}
	}
	ex.PPO = p
	return ex, nil
}

// OnPolicyStats counts what LoadOnPolicy saw.
type OnPolicyStats struct {
	Records     int
	WithOutcome int
	Deviated    int
	// Sampled counts records carrying a behaviour log-probability (a
	// stochastic collection seat's decisions).
	Sampled int
	// Features is the feature set the corpus was encoded under (every record
	// must agree).
	Features FeatureSet
}

// LoadOnPolicy reads an on-policy corpus (plain or gzip JSONL) into PPO
// examples, in file order. A wrong record type or schema version, an encoder
// hash no checkpointable feature set of this build produces, or two records
// under different sets, is a hard error: the stored floats are only
// meaningful under the encoder that produced them. The caller checks
// OnPolicyStats.Features against the model it trains.
func LoadOnPolicy(path string) ([]Example, []OnPolicyRecord, OnPolicyStats, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, OnPolicyStats{}, fmt.Errorf("opening on-policy corpus: %w", err)
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var stream io.Reader = r
	if magic, err := r.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(r)
		if err != nil {
			return nil, nil, OnPolicyStats{}, fmt.Errorf("reading gzipped on-policy corpus: %w", err)
		}
		defer zr.Close()
		stream = zr
	}
	want := ""
	dec := json.NewDecoder(stream)
	var out []Example
	var recs []OnPolicyRecord
	var stats OnPolicyStats
	for {
		var rec OnPolicyRecord
		if err := dec.Decode(&rec); err != nil {
			if err == io.EOF {
				break
			}
			return nil, nil, stats, fmt.Errorf("on-policy record %d: %w", stats.Records+1, err)
		}
		stats.Records++
		switch {
		case rec.RecordType != OnPolicyRecordType:
			return nil, nil, stats, fmt.Errorf("on-policy record %d: record_type %q, want %q", stats.Records, rec.RecordType, OnPolicyRecordType)
		case rec.SchemaVersion != OnPolicySchemaVersion:
			return nil, nil, stats, fmt.Errorf("on-policy record %d: schema_version %d, want %d", stats.Records, rec.SchemaVersion, OnPolicySchemaVersion)
		case want != "" && rec.EncoderHash != want:
			return nil, nil, stats, fmt.Errorf("on-policy record %d: encoder hash %s, earlier records' is %s", stats.Records, rec.EncoderHash, want)
		}
		if want == "" {
			fs, ok := onPolicyFeatures(rec.EncoderHash)
			if !ok {
				return nil, nil, stats, fmt.Errorf("on-policy record %d: encoder hash %s is not this build's (v1 %016x)", stats.Records, rec.EncoderHash, EncoderHash())
			}
			want, stats.Features = rec.EncoderHash, fs
		}
		ex, err := rec.Example()
		if err != nil {
			return nil, nil, stats, fmt.Errorf("on-policy record %d: %w", stats.Records, err)
		}
		if rec.OutcomeKnown {
			stats.WithOutcome++
		}
		if rec.Deviated {
			stats.Deviated++
		}
		if rec.LogPBehaviour != nil {
			stats.Sampled++
		}
		out = append(out, ex)
		// The record's bulky encoded payload is already in ex; keep only the
		// metadata the trainer's readouts group by.
		rec.State, rec.Options, rec.Scores = OnPolicyState{}, nil, nil
		recs = append(recs, rec)
	}
	return out, recs, stats, nil
}

// onPolicyFeatures maps a corpus record's %016x encoder hash onto its
// checkpointable feature set.
func onPolicyFeatures(h string) (FeatureSet, bool) {
	for _, fs := range []FeatureSet{FeaturesV1, FeaturesMZ, FeaturesEntity, FeaturesMZOwnLib} {
		if fmt.Sprintf("%016x", EncoderHashFor(fs)) == h {
			return fs, true
		}
	}
	return 0, false
}
