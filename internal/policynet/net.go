package policynet

import (
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/adams-shaun/gorge/decision"
)

// This file is the model half of the learned per-option scorer (plan L9b,
// R1 §1.4/§3.2/§3.4): a shared hashed embedding table over the encoder's
// sparse rows, a dense projection of the state's dense vector, and a
// one-hidden-layer ReLU MLP that scores [state ‖ option]. Forward and
// backward are HAND-DERIVED — there is no autodiff (R1 §3.4); the gradients
// are pinned by a finite-difference test (net_test.go).
//
// Shape (all widths fixed by the encoder contract in policynet.go/option.go):
//
//	state trunk  s ∈ R^H   = Σ_(r,v)∈state.sparse E[r]·v + W_sd·state.dense + b_s
//	option vec   o ∈ R^(H+152) = [Σ_(r,v)∈opt.hashed E[r]·v ‖ opt.slots ‖ opt.dense]
//	input        x = [s ‖ o] ∈ R^(2H+152)
//	hidden       a = relu(W_h·x + b_h) ∈ R^hidden
//	score        y = w_o·a + b_o  (scalar — one per offered option)
//
// The option's one-hot slots and 24 dense scalars enter RAW (concatenated),
// not embedded: they are already a fixed-width learned-input space (R1 §1.4's
// 106 categorical slots + 24 dense), and giving each its own embedding row
// would duplicate them. The hashed ids share the STATE's table, so a card
// seen in the state bag and the same card offered as an option land on the
// same embedding row — that is the R1 §1.4 sharing.
//
// Everything is float32; the LOSS arithmetic is float64 (softmax/LSE
// stability), which does not affect weight determinism. No map iteration
// reaches any output; the option list order is the record's order.

// DefaultHuberDelta and DefaultRankWeight are the trainer's default loss
// weights (see LossConfig).
const (
	DefaultHuberDelta = 0.1
	DefaultRankWeight = 1.0
)

// LossMode selects which loss terms train. The measured collapse driver
// (L9b-fix2) is the VALUE term: the teacher's candidate values inside one
// decision differ by a median of one sampled world (0.062 at K=16), so
// regressing the absolute value is minimised by predicting the decision
// mean — every option scores alike and the argmax collapses onto the
// option-order baseline. The modes:
//
//   - LossCE — PURE argmax cross-entropy (the trainer's default, the
//     L9b-fix2 control): only the softmax cross-entropy over the labelled
//     options toward the teacher-preferred set, LSE(z) − LSE(z_pref), with
//     the max-subtracting log-sum-exp; the Huber value term is OFF. The
//     per-example weight is RankWeight alone — NOT scaled by Margin, so a
//     tiny-margin decision trains at full imitation strength. This is what
//     the closest published analogue (LOCM PIMC distillation) trains.
//   - LossHybrid — the pre-fix behaviour: value term + margin-weighted rank
//     term (RankWeight·Margin). LossConfig's zero Mode means hybrid, so
//     every existing caller and test keeps today's geometry.
//   - LossValue — the value term only (the diagnosis arm: the collapse
//     reproduced with the rank term removed).
type LossMode string

const (
	LossCE     LossMode = "ce"
	LossHybrid LossMode = "hybrid"
	LossValue  LossMode = "value"
	// LossBCE is the per-option BINARY logistic loss: every labelled option is
	// a positive example when the teacher's chosen declaration includes it and
	// a negative example otherwise, trained as softplus(z) − t·z. Unlike the
	// softmax cross-entropy (shift-invariant — only the ORDER within a decision
	// is trained) and the value term, BCE trains the option's score LEVEL
	// against an absolute 0/1 target, so score > 0 has a calibrated meaning
	// ("sigmoid > 0.5, the teacher includes this option"). It is what the
	// attackers seat's per-option admission rule assumes; see
	// seat/policynet.go. A decision with a single labelled option, which the
	// softmax loss cannot train at all (a one-option softmax has zero
	// gradient), still contributes a full BCE gradient.
	LossBCE LossMode = "bce"
)

// ParseLossMode maps the trainer flag's spelling onto a LossMode. An empty
// string is the LossConfig zero value and means hybrid (the pre-fix
// default); anything else is an error so a typo'd flag cannot silently train
// the wrong loss.
func ParseLossMode(s string) (LossMode, error) {
	switch LossMode(s) {
	case "":
		return LossHybrid, nil
	case LossCE, LossHybrid, LossValue, LossBCE:
		return LossMode(s), nil
	default:
		return "", fmt.Errorf("unknown loss mode %q (want ce, hybrid, value or bce)", s)
	}
}

// Model is the per-option scorer. The parameter blocks are exactly the four
// the finite-difference test exercises: the embedding table, the state dense
// projection, the hidden layer and the output layer.
type Model struct {
	Rows   int // embedding table rows (TableRows for a checkpoint-rounded model)
	H      int // embedding width
	Hidden int // hidden layer width
	InW    int // hidden input width = 2H + OptionSlotWidth + OptionDenseWidth + ExtraW
	// ExtraW is the experimental per-option augmentation width (Option.Extra).
	// Zero for every encoder-produced model, so InW keeps the pinned geometry;
	// a feature-family experiment sets it through NewModelExtra. Not part of
	// EncoderHash and not checkpointable.
	ExtraW int
	// Features is the feature set (features.go) the model encodes under.
	// FeaturesV1, the zero value, is the pinned encoder every existing
	// checkpoint was trained on. It changes no geometry: the other sets only
	// add hashed rows to the same table. The checkpoint carries it through
	// its encoder hash (EncoderHashFor).
	Features FeatureSet

	Table  []float32 // Rows*H, row-major: Table[r*H+j]
	StateW []float32 // DenseWidth*H, row-major: StateW[d*H+j]
	StateB []float32 // H
	HidW   []float32 // Hidden*InW, row-major: HidW[h*InW+i]
	HidB   []float32 // Hidden
	OutW   []float32 // Hidden
	OutB   float32
	// ResidualW is the fixed weight of the bot-prior residual: an option the
	// bot's own answer contains (Option.BotPick) scores ResidualW higher, so
	// with a large prior the model starts at the bot baseline and the learned
	// head only has to supply the overrides (residual policy learning,
	// L9b-fix2 item 3). It is NOT a trained parameter — it is a prior the
	// trainer sets once from Config.ResidualInit — so the CE gradient can
	// never decay the baseline away. Zero (the default) makes the residual a
	// no-op and reproduces pure CE byte for byte. Not covered by EncoderHash
	// (BotPick is not an encoded feature); the checkpoint carries it.
	ResidualW float32

	// The VALUE head (ticket pn08): V(s) = sigmoid(VOutW·relu(VHidW·s + VHidB)
	// + VOutB), the deciding seat's win probability read off the SAME state
	// trunk s the policy head reads. ValueHidden 0 means the model has no
	// value head (HasValue false; the three slices are empty and VOutB 0) —
	// the geometry of every model built by NewModel/NewModelExtra until
	// InitValue adds one.
	ValueHidden int
	VHidW       []float32 // ValueHidden*H, row-major: VHidW[k*H+j]
	VHidB       []float32 // ValueHidden
	VOutW       []float32 // ValueHidden
	VOutB       float32

	// The ENTITY encoder (ticket pn14, entity.go; FeaturesEntity only):
	// EntK 0 means none, and then the three blocks are empty and InW is the
	// pinned geometry. With EntK > 0 the hidden input grows by 2·EntK (the
	// option's two referenced cards' encodings) and the state trunk gains the
	// pooled projection.
	EntK int
	EntW []float32 // EntK*(EntityRawWidth+H), row-major: EntW[q*entInW+i]
	EntB []float32 // EntK
	EntP []float32 // (EntityGroups*2*EntK)*H, row-major: EntP[p*H+j]

	// Ext, when non-nil, answers Score and Value in place of this model's own
	// weights: an EXTERNAL scorer (for example a network served from another
	// process). Every other field is then unused except Features, which still
	// names the encoding the caller builds. Opt-in: nil (the default) changes
	// nothing.
	Ext External
}

// External is a scorer outside this package: the per-option logits and the
// win probability for an encoded state, as Model.Score and Model.Value.
type External interface {
	Score(st State, opts []Option) []float32
	Value(st State) float32
}

// NewExternal is a Model whose Score and Value are ext's, encoding under fs.
func NewExternal(ext External, fs FeatureSet) *Model { return &Model{Features: fs, Ext: ext} }

// HasValue reports whether the model carries a value head.
func (m *Model) HasValue() bool { return m.ValueHidden > 0 || m.Ext != nil }

// InitValue adds a value head of width valueHidden to m, drawn from rng. The
// trainer passes a rng SEPARATE from the policy's (seeded from the run seed),
// so adding the head moves no policy init draw and no shuffle. valueHidden
// <= 0 removes the head.
func (m *Model) InitValue(valueHidden int, rng *rand.Rand) {
	if valueHidden <= 0 {
		m.ValueHidden, m.VHidW, m.VHidB, m.VOutW, m.VOutB = 0, nil, nil, nil, 0
		return
	}
	m.ValueHidden = valueHidden
	m.VHidW = make([]float32, valueHidden*m.H)
	m.VHidB = make([]float32, valueHidden)
	m.VOutW = make([]float32, valueHidden)
	m.VOutB = 0
	fillUniform(m.VHidW, glorot(m.H, valueHidden), rng)
	fillUniform(m.VOutW, glorot(valueHidden, 1), rng)
}

// valueForward runs the value head on a state trunk s, writing the hidden
// activations into a (len ValueHidden) and returning the pre-sigmoid logit.
// The summation order is fixed (VHidB first, then j ascending; VOutB first,
// then k ascending) and shared by Model.Value, the loss and Scorer.Value, so
// all three read bit-identical logits.
func (m *Model) valueForward(s, a []float32) float32 {
	for k := 0; k < m.ValueHidden; k++ {
		sum := m.VHidB[k]
		row := m.VHidW[k*m.H : (k+1)*m.H]
		for j, sj := range s {
			sum += row[j] * sj
		}
		if sum > 0 {
			a[k] = sum
		} else {
			a[k] = 0
		}
	}
	z := m.VOutB
	for k, ak := range a {
		if ak != 0 {
			z += m.VOutW[k] * ak
		}
	}
	return z
}

// Value is the value head's output V(s) ∈ (0,1): the deciding seat's win
// probability from its own encoded (redacted) view. It allocates its own
// scratch, so it is safe to call concurrently on a shared read-only Model. A
// model without a value head (HasValue false) returns 0.5, the no-information
// answer.
func (m *Model) Value(st State) float32 {
	if m.Ext != nil {
		return m.Ext.Value(st)
	}
	if !m.HasValue() {
		return 0.5
	}
	s := m.StateTrunk(st)
	a := make([]float32, m.ValueHidden)
	return float32(sigmoidFloat(float64(m.valueForward(s, a))))
}

// LossConfig carries the loss weights and the mode. HuberDelta is the Huber
// loss's transition point in value units (the teacher's candidate means live
// in [0,1]); RankWeight scales the ranking term against the value term —
// in hybrid mode through the per-example margin (RankWeight · Example.Margin,
// a decision the teacher covered with a large margin pulls harder), in CE
// mode directly (RankWeight per example, margin not applied). OverrideWeight
// multiplies every example whose label records a teacher OVERRIDE of the bot
// (Example.TeacherChoice != Example.BotIndex): those are the decisions that
// carry the information, while agreeing examples only teach "do what the bot
// does". 1 means no reweighting, >1 up-weights the overrides, <= 0 is
// treated as 1. Mode selects the term mix (see LossMode); the zero value is
// hybrid — the pre-fix geometry — so existing callers keep today's loss.
//
// RankWeight is a TERM-MIX weight, not a step-size knob. In hybrid mode it
// trades the rank term against the value term — two different gradient
// directions — so it survives the per-batch gradient clip (Grads.Clip).
// In pure CE mode (LossCE) the rank term IS the whole loss, so RankWeight
// is a uniform scale on a single gradient direction: when the clip binds
// it is divided back out, and with the clip disabled it is equivalent to
// rescaling the learning rate. Either way it cannot change what the model
// converges to — tune lr and the clip cap instead. RankWeight 0 remains
// meaningful in CE mode: it switches the term (and the training) off
// entirely. The trainer warns when a non-trivial RankWeight is set in CE
// mode (cmd/policytrain Train's log).
type LossConfig struct {
	Mode           LossMode
	HuberDelta     float64
	RankWeight     float64
	OverrideWeight float64
	// ValueWeight scales the value-head term, ValueWeight · BCE(V(s), target):
	// 0 (the zero value) switches it off and the loss is exactly the policy
	// loss. It needs a model with a value head (HasValue); on one without,
	// the term is skipped. ValueBlend b ∈ [0,1] mixes the target:
	// (1-b)·Example.Outcome + b·Example.TeacherValue. An example lacking a
	// piece of the target the blend needs (HasOutcome when b < 1,
	// HasTeacherValue when b > 0) contributes no value term. The term is NOT
	// scaled by OverrideWeight: whether the teacher overrode the bot says
	// nothing about who won.
	ValueWeight float64
	ValueBlend  float64
	// KindModes overrides Mode for specific decision kinds: the two scored
	// kinds take different inference paths (attackers admits options with an
	// absolute per-option vote; priority argmaxes a shift-invariant softmax),
	// so the loss each kind is trained with must match the rule its path
	// assumes. A nil map (the zero value) applies Mode to every kind, so every
	// existing caller keeps today's loss. A kind absent from the map also uses
	// Mode. Lookups are map reads only — no iteration, so no order reaches a
	// gradient.
	KindModes map[decision.Kind]LossMode
	// PPOClip is the PPO surrogate's clip ε and PPOKL the KL anchor's weight
	// β (ppo.go). Read only by an example carrying a PPO target
	// (Example.PPO); a supervised example ignores both.
	PPOClip float64
	PPOKL   float64
	// VDWMMargin is the VDWM hinge margin m (vdwm.go), read only by a PPO
	// example with PPOTarget.VDWM set.
	VDWMMargin float64
}

// modeFor resolves the loss mode for one example: the per-kind override when
// present, else the config's global Mode (with the "" zero value meaning
// hybrid, the pre-fix geometry).
func (lc LossConfig) modeFor(ex Example) LossMode {
	m := lc.Mode
	if o, ok := lc.KindModes[ex.Kind]; ok {
		m = o
	}
	switch m {
	case "":
		return LossHybrid
	case LossCE, LossHybrid, LossValue, LossBCE:
		return m
	default:
		return LossHybrid
	}
}

// Override reports whether the teacher's chosen candidate differed from the
// bot's answer: the only examples in the corpus that teach the model
// anything the default policy did not already do (the writer's contract is
// candidates[BotIndex], BotIndex == 0 by construction, is the bot answer).
func (ex Example) Override() bool { return ex.TeacherChoice != ex.BotIndex }

// exampleWeight is the multiplier applied to an example's whole loss (value
// and ranking terms) — OverrideWeight for an override, 1 otherwise.
func (lc LossConfig) exampleWeight(ex Example) float64 {
	if !ex.Override() {
		return 1
	}
	if lc.OverrideWeight <= 0 {
		return 1
	}
	return lc.OverrideWeight
}

// DefaultLossConfig returns the default loss weights in the DEFAULT mode,
// pure argmax CE (the L9b-fix2 control: the value term is what collapses).
// Note the distinction: the Go zero value of LossConfig (Mode "") is the
// pre-fix hybrid geometry, kept so every caller that does not opt in keeps
// today's loss; the DEFAULT — what the trainer and this constructor hand a
// fresh run — is CE.
func DefaultLossConfig() LossConfig {
	return LossConfig{Mode: LossCE, HuberDelta: DefaultHuberDelta, RankWeight: DefaultRankWeight, OverrideWeight: 1}
}

// LossParts is one example's loss broken into its two terms. Total is the
// quantity the trainer descends: Value + Rank.
type LossParts struct {
	Value float64 // mean Huber over the labelled options
	Rank  float64 // RankWeight·margin·(LSE over labelled − LSE over preferred)
	// ValueHead is ValueWeight·BCE(V(s), target) — the value head's term, 0
	// when it is off or the example has no target (see LossConfig.ValueWeight).
	ValueHead float64
	Total     float64
}

// ValueTarget returns the value head's training target for ex under the
// blend b = ValueBlend: (1-b)·Outcome + b·TeacherValue. ok is false when the
// example lacks a part the blend needs (an outcome for b < 1, a teacher
// value for b > 0), in which case the example trains no value term.
func (lc LossConfig) ValueTarget(ex Example) (t float64, ok bool) {
	b := lc.ValueBlend
	if b < 1 && !ex.HasOutcome {
		return 0, false
	}
	if b > 0 && !ex.HasTeacherValue {
		return 0, false
	}
	switch {
	case b <= 0:
		return ex.Outcome, true
	case b >= 1:
		return ex.TeacherValue, true
	}
	return (1-b)*ex.Outcome + b*ex.TeacherValue, true
}

// StepStat is one example's training-relevant readout: the loss and whether
// the argmax over the LABELLED options landed in the teacher-preferred set
// (top-1 agreement with the teacher; Eligible false when the example has no
// preferred option and so cannot agree or disagree).
type StepStat struct {
	Loss     float64
	Parts    LossParts
	Agree    bool
	Eligible bool
	// PPO is the PPO readout of a PPO example (zero otherwise).
	PPO PPOStep
}

// NewModel builds a model with the given geometry, initialised from a
// caller-seeded rng (deterministic when the seed is; math/rand/v2 with an
// explicit source — the repo forbids the v1 package). Uniform Glorot-style
// limits per block: the table small (a sum-pool of ~200 rows must start near
// zero), the projections at sqrt(6/(fan_in+fan_out)).
func NewModel(rows, h, hidden int, rng *rand.Rand) *Model {
	return NewModelExtra(rows, h, hidden, 0, rng)
}

// NewModelExtra builds a model whose option input carries extraW experimental
// augmentation floats after the encoded Dense block (Option.Extra). extraW 0
// is exactly NewModel. Used only by feature-family experiments; the encoder's
// pinned geometry is unchanged and no such model can be checkpointed (the
// format has no ExtraW field) — it is a measurement vehicle, not a deployable
// scorer.
func NewModelExtra(rows, h, hidden, extraW int, rng *rand.Rand) *Model {
	m := &Model{
		Rows:   rows,
		H:      h,
		Hidden: hidden,
		ExtraW: extraW,
		InW:    2*h + OptionSlotWidth + OptionDenseWidth + extraW,
	}
	m.Table = make([]float32, rows*h)
	m.StateW = make([]float32, DenseWidth*h)
	m.StateB = make([]float32, h)
	m.HidW = make([]float32, hidden*m.InW)
	m.HidB = make([]float32, hidden)
	m.OutW = make([]float32, hidden)
	fillUniform(m.Table, 0.05, rng)
	fillUniform(m.StateW, glorot(DenseWidth, h), rng)
	fillUniform(m.HidW, glorot(m.InW, hidden), rng)
	fillUniform(m.OutW, glorot(hidden, 1), rng)
	return m
}

func glorot(fanIn, fanOut int) float32 {
	return float32(6 / (float64(fanIn) + float64(fanOut)))
}

func fillUniform(w []float32, lim float32, rng *rand.Rand) {
	for i := range w {
		w[i] = (2*rng.Float32() - 1) * lim
	}
}

// StateTrunk computes the state embedding s ∈ R^H: sum-pool of the state's
// sparse hashed rows plus the dense projection. Zero-valued dense inputs are
// skipped (exact: adding w·0 is a no-op for finite weights).
func (m *Model) StateTrunk(st State) []float32 {
	s, _ := m.stateTrunkEnt(st)
	return s
}

// stateTrunkEnt is StateTrunk plus the entity forward it pooled (nil for a
// model without an entity encoder, whose trunk is then exactly the pinned
// sum-pool + dense projection).
func (m *Model) stateTrunkEnt(st State) ([]float32, *entCache) {
	s := make([]float32, m.H)
	copy(s, m.StateB)
	for _, f := range st.Sparse {
		base := int(f.Row) * m.H
		v := f.Value
		row := m.Table[base : base+m.H]
		for j := range s {
			s[j] += row[j] * v
		}
	}
	for d := 0; d < DenseWidth && d < len(st.Dense); d++ {
		v := st.Dense[d]
		if v == 0 {
			continue
		}
		base := d * m.H
		for j := range s {
			s[j] += m.StateW[base+j] * v
		}
	}
	c := m.entForward(st)
	m.entAddTrunk(s, c)
	return s, c
}

// inputVector builds one option's hidden-layer input x = [s ‖ hashed-embed ‖
// slots ‖ dense]. Slots whose Row falls outside OptionSlotWidth are dropped
// (the encoder never emits one).
func (m *Model) inputVector(s []float32, o Option, c *entCache) []float32 {
	x := make([]float32, m.InW)
	copy(x, s)
	off := m.H
	for _, f := range o.Hashed {
		base := int(f.Row) * m.H
		v := f.Value
		for j := 0; j < m.H; j++ {
			x[off+j] += m.Table[base+j] * v
		}
	}
	off += m.H
	for _, f := range o.Slots {
		if r := int(f.Row); r >= 0 && r < OptionSlotWidth {
			x[off+r] += f.Value
		}
	}
	off += OptionSlotWidth
	copy(x[off:], o.Dense)
	off += OptionDenseWidth
	for i := 0; i < m.ExtraW && i < len(o.Extra); i++ {
		x[off+i] = o.Extra[i]
	}
	m.entFillInput(x, o, c)
	return x
}

// forwardHead runs the hidden layer + output scalar on one input vector.
func (m *Model) forwardHead(x []float32) (y float32, a []float32) {
	a = make([]float32, m.Hidden)
	for h := 0; h < m.Hidden; h++ {
		sum := m.HidB[h]
		base := h * m.InW
		for i, xi := range x {
			if xi != 0 {
				sum += m.HidW[base+i] * xi
			}
		}
		if sum > 0 {
			a[h] = sum
		}
	}
	y = m.OutB
	for h, ah := range a {
		if ah != 0 {
			y += m.OutW[h] * ah
		}
	}
	return y, a
}

// residual returns the fixed bot-prior score for one option: Model.ResidualW
// when the option is part of the bot's own answer, else 0. It is added to
// the head's output (never fed through the head), so it is a constant prior
// the learned weights cannot erase.
func (m *Model) residual(o Option) float32 {
	if o.BotPick {
		return m.ResidualW
	}
	return 0
}

// Score scores every offered option (the inference path, L9c's entry point):
// the state trunk is computed once, then each option gets its own head
// forward. The returned slice parallels opts.
func (m *Model) Score(st State, opts []Option) []float32 {
	if m.Ext != nil {
		return m.Ext.Score(st, opts)
	}
	s, c := m.stateTrunkEnt(st)
	out := make([]float32, len(opts))
	for i := range opts {
		x := m.inputVector(s, opts[i], c)
		y, _ := m.forwardHead(x)
		out[i] = y + m.residual(opts[i])
	}
	return out
}

// Grads holds one accumulated gradient per parameter block. Table rows are
// tracked (touched) so Reset between batches costs O(touched) rather than a
// full 2.1M-element memset, and ApplyGrads only walks touched rows.
type Grads struct {
	m *Model

	Table  []float32
	StateW []float32
	StateB []float32
	HidW   []float32
	HidB   []float32
	OutW   []float32
	OutB   float32
	// The value head's blocks (empty when the model has none).
	VHidW []float32
	VHidB []float32
	VOutW []float32
	VOutB float32
	// The entity encoder's blocks (empty when the model has none).
	EntW []float32
	EntB []float32
	EntP []float32

	noted   []byte    // Rows: 1 when the row is in touched
	touched []int32   // table rows written since the last Reset
	dx      []float32 // scratch: hidden-input gradient, one option at a time
	da      []float32 // scratch: hidden activation gradient
	dz      []float32 // scratch: hidden pre-activation gradient
	ds      []float32 // scratch: accumulated state-trunk gradient
	va      []float32 // scratch: value-head hidden activations
}

// NewGrads allocates a zeroed gradient buffer for m.
func (m *Model) NewGrads() *Grads {
	g := &Grads{
		m:      m,
		Table:  make([]float32, len(m.Table)),
		StateW: make([]float32, len(m.StateW)),
		StateB: make([]float32, len(m.StateB)),
		HidW:   make([]float32, len(m.HidW)),
		HidB:   make([]float32, len(m.HidB)),
		OutW:   make([]float32, len(m.OutW)),
		VHidW:  make([]float32, len(m.VHidW)),
		VHidB:  make([]float32, len(m.VHidB)),
		VOutW:  make([]float32, len(m.VOutW)),
		EntW:   make([]float32, len(m.EntW)),
		EntB:   make([]float32, len(m.EntB)),
		EntP:   make([]float32, len(m.EntP)),
		noted:  make([]byte, m.Rows),
		dx:     make([]float32, m.InW),
		da:     make([]float32, m.Hidden),
		dz:     make([]float32, m.Hidden),
		ds:     make([]float32, m.H),
		va:     make([]float32, m.ValueHidden),
	}
	return g
}

// Zero clears every block (a full reset — used by the gradient tests).
func (g *Grads) Zero() {
	zero(g.Table)
	zero(g.StateW)
	zero(g.StateB)
	zero(g.HidW)
	zero(g.HidB)
	zero(g.OutW)
	g.OutB = 0
	g.zeroValue()
	g.touched = g.touched[:0]
	for i := range g.noted {
		g.noted[i] = 0
	}
}

// Reset clears only the table rows touched since the last Reset plus the
// always-dense blocks. Between batches this is the cheap path.
func (g *Grads) Reset() {
	for _, r := range g.touched {
		base := int(r) * g.m.H
		zero(g.Table[base : base+g.m.H])
		g.noted[r] = 0
	}
	g.touched = g.touched[:0]
	zero(g.StateW)
	zero(g.StateB)
	zero(g.HidW)
	zero(g.HidB)
	zero(g.OutW)
	g.OutB = 0
	g.zeroValue()
}

func (g *Grads) zeroValue() {
	zero(g.VHidW)
	zero(g.VHidB)
	zero(g.VOutW)
	g.VOutB = 0
	zero(g.EntW)
	zero(g.EntB)
	zero(g.EntP)
}

// ApplyGrads descends one step: every parameter p ← p − scale·∇p. scale is
// the caller's lr/batchSize.
func (m *Model) ApplyGrads(g *Grads, scale float32) {
	for _, r := range g.touched {
		base := int(r) * m.H
		for j := 0; j < m.H; j++ {
			m.Table[base+j] -= scale * g.Table[base+j]
		}
	}
	for i := range m.StateW {
		m.StateW[i] -= scale * g.StateW[i]
	}
	for i := range m.StateB {
		m.StateB[i] -= scale * g.StateB[i]
	}
	for i := range m.HidW {
		m.HidW[i] -= scale * g.HidW[i]
	}
	for i := range m.HidB {
		m.HidB[i] -= scale * g.HidB[i]
	}
	for i := range m.OutW {
		m.OutW[i] -= scale * g.OutW[i]
	}
	m.OutB -= scale * g.OutB
	if m.HasValue() {
		for i := range m.VHidW {
			m.VHidW[i] -= scale * g.VHidW[i]
		}
		for i := range m.VHidB {
			m.VHidB[i] -= scale * g.VHidB[i]
		}
		for i := range m.VOutW {
			m.VOutW[i] -= scale * g.VOutW[i]
		}
		m.VOutB -= scale * g.VOutB
	}
	for _, blk := range [][2][]float32{{m.EntW, g.EntW}, {m.EntB, g.EntB}, {m.EntP, g.EntP}} {
		for i := range blk[0] {
			blk[0][i] -= scale * blk[1][i]
		}
	}
}

// Norm returns the gradient's global L2 norm over every parameter block.
func (g *Grads) Norm() float64 {
	sum := 0.0
	for _, r := range g.touched {
		base := int(r) * g.m.H
		for j := 0; j < g.m.H; j++ {
			v := float64(g.Table[base+j])
			sum += v * v
		}
	}
	for _, blk := range [][]float32{g.StateW, g.StateB, g.HidW, g.HidB, g.OutW} {
		for _, v := range blk {
			x := float64(v)
			sum += x * x
		}
	}
	x := float64(g.OutB)
	sum += x * x
	// The value blocks come last so a model without a value head (every
	// block empty, VOutB 0) sums exactly what it summed before the head
	// existed: adding +0 to a float64 sum is exact.
	for _, blk := range [][]float32{g.VHidW, g.VHidB, g.VOutW} {
		for _, v := range blk {
			x := float64(v)
			sum += x * x
		}
	}
	x = float64(g.VOutB)
	sum += x * x
	// The entity blocks last, by the same rule: empty for every other model.
	for _, blk := range [][]float32{g.EntW, g.EntB, g.EntP} {
		for _, v := range blk {
			x := float64(v)
			sum += x * x
		}
	}
	return math.Sqrt(sum)
}

// Clip rescales the SUMMED batch gradient as ONE vector so its global L2
// norm (Norm, over every parameter block) is at most maxNorm — a no-op when
// the norm is already inside; maxNorm <= 0 disables. This is the
// deliberate clip semantics, chosen over the alternatives (pinned by this
// package's TestClipSemanticsIsPerBatchNotPerExample; ticket
// policytrain-clip-rankweight-interaction):
//
//   - Per-batch, not per-example: the clip normalises only the batch
//     gradient's MAGNITUDE and preserves its direction — the weighted mean
//     of the per-example directions, so the direction-shaping weights
//     (OverrideWeight per example, RankWeight·Margin in hybrid mode) keep
//     their relative meaning. A per-example cap instead makes every
//     saturating example contribute the same norm regardless of its
//     weight, erasing exactly the per-example weights the loss defines,
//     and multiplies the clip's cost by the batch size.
//   - Global, not per-parameter-group: a per-block cap rescales blocks
//     against each other and so distorts the update direction inside the
//     step; the global cap keeps every block's share fixed.
//   - Applied to the summed gradient BEFORE the LR/batch scale
//     (ApplyGrads' scale), so the step's worst-case norm is
//     lr·maxNorm/batchSize — one scalar the caller controls. A cap is a
//     CAP, not a normaliser: batches whose summed gradient is already
//     under maxNorm step at lr·‖mean grad‖ and see any uniform loss scale
//     through (as an lr rescale); the clip erases a uniform scale exactly
//     when it binds.
//
// The corollary is intentional and documented at LossConfig.RankWeight: a
// UNIFORM scale on the whole loss (a single-term loss's weight) scales the
// summed gradient's magnitude only, so when the clip binds it is divided
// back out — the clip makes a pure-CE run invariant to the CE term's
// absolute weight. Direction-carrying weights survive; magnitude-only
// weights do not, and the magnitude knobs in CE mode are lr and the cap.
func (g *Grads) Clip(maxNorm float64) {
	if maxNorm <= 0 {
		return
	}
	n := g.Norm()
	if n <= maxNorm || n == 0 {
		return
	}
	scale := float32(maxNorm / n)
	for _, r := range g.touched {
		base := int(r) * g.m.H
		for j := 0; j < g.m.H; j++ {
			g.Table[base+j] *= scale
		}
	}
	for _, blk := range [][]float32{g.StateW, g.StateB, g.HidW, g.HidB, g.OutW, g.VHidW, g.VHidB, g.VOutW, g.EntW, g.EntB, g.EntP} {
		for i := range blk {
			blk[i] *= scale
		}
	}
	g.OutB *= scale
	g.VOutB *= scale
}

func zero(w []float32) {
	for i := range w {
		w[i] = 0
	}
}

// addTable accumulates vec·scale into gradient row r, noting the row.
func (g *Grads) addTable(r int, vec []float32, scale float32) {
	if g.noted[r] == 0 {
		g.noted[r] = 1
		g.touched = append(g.touched, int32(r))
	}
	base := r * g.m.H
	for j := range vec {
		g.Table[base+j] += scale * vec[j]
	}
}

// forwardExample computes the state trunk and every labelled option's input
// vector, hidden activations and score. The returned indices parallel ys/xs/as.
func (m *Model) forwardExample(ex Example) (labelled []int, trunk []float32, xs, as [][]float32, ys []float64) {
	labelled, trunk, xs, as, ys, _ = m.forwardExampleEnt(ex)
	return
}

// forwardExampleEnt is forwardExample plus the entity forward (nil without
// an entity encoder).
func (m *Model) forwardExampleEnt(ex Example) (labelled []int, trunk []float32, xs, as [][]float32, ys []float64, c *entCache) {
	s, c := m.stateTrunkEnt(ex.State)
	for i := range ex.Options {
		if !ex.Options[i].Target.Labelled {
			continue
		}
		x := m.inputVector(s, ex.Options[i], c)
		y, a := m.forwardHead(x)
		labelled = append(labelled, i)
		xs = append(xs, x)
		as = append(as, a)
		ys = append(ys, float64(y)+float64(m.residual(ex.Options[i])))
	}
	return labelled, s, xs, as, ys, c
}

// lossFromScores computes the combined loss and the per-labelled-option
// score gradient, given the forward pass.
//
// The value term regresses the WITHIN-DECISION CENTRED target
// (Target.Value − mean of this decision's labelled values) against the raw
// score. That centring is the fix for the constant-score collapse: the raw
// teacher candidate means are near-identical inside one decision (a
// decision's value is dominated by the position, not the choice), so a
// value term anchored to their absolute level is minimised by every option
// scoring the same — a strong local optimum the ranking term alone could not
// escape on the real corpus. Removing the decision's shared level leaves
// only the within-decision value differences, which is exactly the quantity
// an argmax-over-options scorer can use (the absolute level shifts every
// option equally and so cannot change the argmax). When every labelled value
// in a decision is tied the centred targets are all zero, so the value term
// carries no preference signal and is skipped rather than teaching the model
// to flatten every option's score — 42% of the real corpus's covered
// decisions are exactly tied and would otherwise spend their gradient
// erasing option-discriminating features.
//
// The ranking term is a softmax cross-entropy over the LABELLED options
// toward the teacher-preferred set (LSE(labelled) − LSE(preferred)) — for a
// single preferred candidate exactly the argmax CE logsumexp(z) − z_pref the
// L9b-fix2 brief names. Its preferred-side gradient is the uniform −w used
// since the trainer's first version: for a multi-option preferred set (the
// attackers decisions, where the teacher's answer commits several
// creatures) the exact softmax-over-preferred gradient concentrates on the
// currently-highest preferred score and empirically trains worse than the
// uniform push; the uniform form is the deliberate set-preference loss. An
// example with no preferred option, or a single labelled option, contributes
// only the value term (a one-option softmax has zero gradient — the only
// legal answer is the only answer).
//
// The whole example (both terms and every gradient) is scaled by the
// override weight when the teacher overrode the bot.
func lossFromScores(lc LossConfig, ex Example, labelled []int, ys []float64) (parts LossParts, dys []float64) {
	n := len(labelled)

	// The loss mode is per-KIND when the config says so (KindModes), so the
	// attackers kind can train with the binary logistic loss its admission rule
	// assumes while priority keeps argmax CE.
	mode := lc.modeFor(ex)

	// Clamp every score the loss reads to a finite, well-inside-float64 range
	// once, up front, and use the clamped values in every term. A diverged
	// forward pass can hand back ±Inf/NaN; with the raw values, `Inf − Inf` in
	// LSE(labelled) − LSE(preferred) is NaN (and Huber on ±Inf saturates to
	// ±Inf), which then poisons every weight. The clamp keeps the arithmetic
	// finite and the gradient bounded even when the weights are already huge
	// (a finite ±1e6 mask, never ±Inf), and `logSumExp` additionally subtracts
	// the max, so exp never overflows and the CE is computed as
	// logsumexp(z) − z_pref with max subtraction. Together they make every
	// term NaN-free across the rank-weight/lr grid the trainer is run at (see
	// cmd/policytrain's TestTrainerStableAcrossRankGrid,
	// TestTrainerCENoNaNGrid and this package's
	// TestLossFiniteOnDivergedScores).
	clamped := make([]float64, n)
	for k := range clamped {
		clamped[k] = clampScore(ys[k])
	}

	// BCE is a different objective entirely (an absolute per-option target,
	// not a within-decision ranking), so it branches before the value/rank
	// machinery below.
	if mode == LossBCE {
		return lossBCE(lc, ex, labelled, clamped)
	}

	// Within-decision mean of the teacher values (a constant); the value term
	// regresses the centred target against the raw score. In CE mode the value
	// term is OFF entirely (LossCE's contract): the value target is the
	// measured collapse driver, and CE trains only the classification signal.
	var tMean float64
	for _, i := range labelled {
		tMean += ex.Options[i].Target.Value
	}
	tMean /= float64(n)

	// A decision whose labelled values are ALL tied carries no within-decision
	// preference signal; skip the value term for it (see the doc comment).
	tied := true
	for _, i := range labelled {
		if ex.Options[i].Target.Value != ex.Options[labelled[0]].Target.Value {
			tied = false
			break
		}
	}

	dys = make([]float64, n)
	parts.Value = 0
	if mode != LossCE && !tied {
		for k, i := range labelled {
			r := clamped[k] - (ex.Options[i].Target.Value - tMean)
			parts.Value += huber(r, lc.HuberDelta)
			dys[k] = huberGrad(r, lc.HuberDelta) / float64(n)
		}
		parts.Value /= float64(n)
	}

	pref := make([]int, 0, 2)
	for k, i := range labelled {
		if ex.Options[i].Target.Preferred {
			pref = append(pref, k)
		}
	}
	if len(pref) == 0 || n <= 1 {
		ow := lc.exampleWeight(ex)
		parts.Value *= ow
		parts.Total = parts.Value
		scaleGrads(dys, ow)
		return parts, dys
	}
	// The rank term's per-example weight. Hybrid keeps the margin weighting
	// (RankWeight·Margin); CE deliberately does NOT apply the margin — pure
	// imitation trains every labelled decision at RankWeight strength, so a
	// one-world margin cannot silence the CE signal the way it silenced the
	// margin-weighted rank term on the real corpus.
	w := lc.RankWeight
	if mode == LossHybrid {
		w = lc.RankWeight * ex.Margin
	}
	if mode == LossValue || w == 0 {
		ow := lc.exampleWeight(ex)
		parts.Value *= ow
		parts.Total = parts.Value
		scaleGrads(dys, ow)
		return parts, dys
	}
	lseAll := logSumExp(clamped)
	prefYs := make([]float64, len(pref))
	for i, k := range pref {
		prefYs[i] = clamped[k]
	}
	lsePref := logSumExp(prefYs)
	parts.Rank = w * (lseAll - lsePref)
	for k := range dys {
		dys[k] += w * math.Exp(clamped[k]-lseAll)
	}
	for _, k := range pref {
		dys[k] -= w
	}
	ow := lc.exampleWeight(ex)
	parts.Value *= ow
	parts.Rank *= ow
	parts.Total = parts.Value + parts.Rank
	scaleGrads(dys, ow)
	return parts, dys
}

// lossBCE is the per-option binary logistic loss: every labelled option is a
// positive example when the teacher's chosen declaration contains it and a
// negative example otherwise. With z the (clamped) score, t ∈ {0,1} the
// target and w = RankWeight, the per-option loss is
//
//	w · (softplus(z) − t·z),  d/dz = w · (sigmoid(z) − t)
//
// summed over the labelled options and weighted once more by the override
// weight. This trains an ABSOLUTE score level: score 0 is the decision
// boundary (sigmoid 0.5), which is exactly the contract the attackers seat's
// per-option admission rule reads. A single labelled option — the majority of
// the label corpus's attackers decisions, where the softmax CE has zero
// gradient because a one-option softmax is flat — trains here at full
// strength, which is the whole point: the decision "attack with this creature
// or not" is an absolute yes/no the softmax cannot express.
//
// parts.Value carries the BCE total (parts.Rank is 0) so the trainer's loss
// readout stays one number; the gradient is written into dys. The total is
// AVERAGED over the labelled options (unlike the rank term, which is a sum),
// so a decision with many options does not dominate the batch purely by
// option count; the per-option gradient carries RankWeight but not the
// 1/len(labelled) average, matching the rank term's un-normalised gradient.
//
// RankWeight == 0 means UNWEIGHTED here (w = 1), deliberately NOT the "term
// off" convention lossFromScores uses for the rank term. There the rank term
// is one addend beside the value term, so switching it off still leaves a
// loss to train; here BCE is the kind's ENTIRE loss, so honouring a zero as
// "off" would make the attackers head silently untrainable — no loss and no
// gradient — which is the failure mode this whole change exists to prevent.
// A caller that wants the attackers head off selects a different kind mode,
// it does not zero the shared rank weight.
func lossBCE(lc LossConfig, ex Example, labelled []int, clamped []float64) (parts LossParts, dys []float64) {
	dys = make([]float64, len(labelled))
	w := lc.RankWeight
	if w == 0 {
		w = 1
	}
	total := 0.0
	for k, i := range labelled {
		t := 0.0
		if ex.Options[i].Target.Preferred {
			t = 1
		}
		z := clamped[k]
		total += w * (softplus(z) - t*z)
		dys[k] = w * (sigmoidFloat(z) - t)
	}
	parts.Value = total / float64(len(labelled))
	ow := lc.exampleWeight(ex)
	parts.Value *= ow
	parts.Total = parts.Value
	scaleGrads(dys, ow)
	return parts, dys
}

// softplus is log(1 + e^z), computed stably: for large z it is z (so exp
// never overflows), for very negative z it is exp(z) ≈ 0.
func softplus(z float64) float64 {
	if z > 30 {
		return z
	}
	if z < -30 {
		return math.Exp(z)
	}
	return math.Log1p(math.Exp(z))
}

// sigmoidFloat is the logistic function in float64, the BCE branch's own
// helper (the seat package has its own float32 form for inference).
func sigmoidFloat(z float64) float64 {
	if z >= 0 {
		e := math.Exp(-z)
		return 1 / (1 + e)
	}
	e := math.Exp(z)
	return e / (1 + e)
}

// scoreClamp bounds a score entering the loss arithmetic. Scores live in a
// few units for a converged model; 1e6 is far above that and far below
// float64 overflow, so a diverged ±Inf forward pass becomes ±1e6 instead of
// NaN.
const scoreClamp = 1e6

func clampScore(y float64) float64 {
	if math.IsNaN(y) {
		return 0
	}
	if y > scoreClamp {
		return scoreClamp
	}
	if y < -scoreClamp {
		return -scoreClamp
	}
	return y
}

func scaleGrads(dys []float64, w float64) {
	if w == 1 {
		return
	}
	for i := range dys {
		dys[i] *= w
	}
}

// Loss evaluates the example's loss (forward only — no gradients).
func (m *Model) Loss(ex Example, lc LossConfig) StepStat {
	labelled, _, _, _, ys := m.forwardExample(ex)
	if len(labelled) == 0 {
		return StepStat{}
	}
	var parts LossParts
	var ps PPOStep
	switch {
	case ex.PPO != nil:
		parts, _, ps = lossPPO(lc, ex, labelled, ys)
	case ex.Visits != nil:
		parts, _ = lossVisits(ex, labelled, ys)
	default:
		parts, _ = lossFromScores(lc, ex, labelled, ys)
	}
	if t, on := m.valueTermOn(lc, ex); on {
		s := m.StateTrunk(ex.State)
		z := m.valueForward(s, make([]float32, m.ValueHidden))
		parts.ValueHead, _ = valueBCE(lc.ValueWeight, z, t)
		parts.Total += parts.ValueHead
	}
	st := StepStat{Loss: parts.Total, Parts: parts, PPO: ps}
	st.Eligible, st.Agree = agreement(ex, labelled, ys)
	return st
}

// valueTermOn reports whether ex trains the value head under lc, and its
// target.
func (m *Model) valueTermOn(lc LossConfig, ex Example) (float64, bool) {
	if lc.ValueWeight == 0 || !m.HasValue() {
		return 0, false
	}
	return lc.ValueTarget(ex)
}

// valueBCE is the value term w·BCE(sigmoid(z), t) written in logit form,
// w·(softplus(z) − t·z) (equal to −w·[t·log V + (1−t)·log(1−V)] for any soft
// t ∈ [0,1]), and its logit gradient w·(sigmoid(z) − t). The logit is
// clamped like the policy scores so a diverged forward pass stays finite.
func valueBCE(w float64, z32 float32, t float64) (loss, dz float64) {
	z := clampScore(float64(z32))
	return w * (softplus(z) - t*z), w * (sigmoidFloat(z) - t)
}

// ValueLogLoss is the unweighted BCE of the model's value on ex against t
// (the holdout readout's log loss), computed as valueBCE does. 0 on a model
// without a value head.
func (m *Model) ValueLogLoss(st State, t float64) float64 {
	if !m.HasValue() {
		return 0
	}
	z := m.valueForward(m.StateTrunk(st), make([]float32, m.ValueHidden))
	l, _ := valueBCE(1, z, t)
	return l
}

// LossGrad evaluates the example's loss AND accumulates its parameter
// gradients into g (g is NOT reset first — the trainer accumulates batches).
func (m *Model) LossGrad(ex Example, lc LossConfig, g *Grads) StepStat {
	labelled, trunk, xs, as, ys, ec := m.forwardExampleEnt(ex)
	if len(labelled) == 0 {
		return StepStat{}
	}
	var parts LossParts
	var dys []float64
	var ps PPOStep
	switch {
	case ex.PPO != nil:
		parts, dys, ps = lossPPO(lc, ex, labelled, ys)
	case ex.Visits != nil:
		parts, dys = lossVisits(ex, labelled, ys)
	default:
		parts, dys = lossFromScores(lc, ex, labelled, ys)
	}
	st := StepStat{Loss: parts.Total, Parts: parts, PPO: ps}
	st.Eligible, st.Agree = agreement(ex, labelled, ys)

	// Backward through the head, option by option; the state-trunk gradient
	// accumulates across options and is applied once at the end.
	for j := range g.ds {
		g.ds[j] = 0
	}
	var dh [][]float32 // per-card entity gradient (entity models only)
	for k, i := range labelled {
		dy := float32(dys[k])
		if dy == 0 {
			continue
		}
		x, a := xs[k], as[k]
		m.backHead(dy, x, a, g, g.dx)
		// dx[:H] is the state trunk's gradient contribution; dx[H:2H] is the
		// option hashed-embed's; the slots/dense region is raw input.
		for j := 0; j < m.H; j++ {
			g.ds[j] += g.dx[j]
		}
		for _, f := range ex.Options[i].Hashed {
			g.addTable(int(f.Row), g.dx[m.H:m.H+m.H], f.Value)
		}
		if ec != nil {
			// The option's entity inputs route their gradient to the
			// referenced cards' encodings.
			if dh == nil {
				dh = make([][]float32, len(ex.State.Cards))
			}
			off := m.entOffset()
			for slot, ref := range [2]int32{ex.Options[i].EntA, ex.Options[i].EntB} {
				if ref <= 0 || int(ref) > len(dh) {
					continue
				}
				ci := int(ref - 1)
				if dh[ci] == nil {
					dh[ci] = make([]float32, m.EntK)
				}
				for q := 0; q < m.EntK; q++ {
					dh[ci][q] += g.dx[off+slot*m.EntK+q]
				}
			}
		}
	}
	// Value head: forward on the shared trunk, BCE on the logit, backward
	// into the value blocks and (through ds) into the trunk.
	if t, on := m.valueTermOn(lc, ex); on {
		z := m.valueForward(trunk, g.va)
		var dz64 float64
		parts.ValueHead, dz64 = valueBCE(lc.ValueWeight, z, t)
		parts.Total += parts.ValueHead
		st.Loss, st.Parts = parts.Total, parts
		m.backValue(float32(dz64), trunk, g)
	}
	// Entity backward (pool, projection, per-card encoder) off the final
	// trunk gradient, before the trunk's own blocks read it.
	m.entBackward(ec, ex.State, g.ds, dh, g)
	// State trunk backward: dense projection + sparse table rows.
	for j := 0; j < m.H; j++ {
		g.StateB[j] += g.ds[j]
	}
	for d := 0; d < DenseWidth && d < len(ex.State.Dense); d++ {
		v := ex.State.Dense[d]
		if v == 0 {
			continue
		}
		base := d * m.H
		for j := 0; j < m.H; j++ {
			g.StateW[base+j] += v * g.ds[j]
		}
	}
	for _, f := range ex.State.Sparse {
		g.addTable(int(f.Row), g.ds, f.Value)
	}
	return st
}

// backValue propagates the value logit's gradient dz through the value head
// into g's value blocks, and adds the trunk gradient into g.ds (the caller
// applies ds to the trunk blocks once). g.va holds the forward's hidden
// activations.
func (m *Model) backValue(dz float32, s []float32, g *Grads) {
	if dz == 0 {
		return
	}
	g.VOutB += dz
	for k := 0; k < m.ValueHidden; k++ {
		ak := g.va[k]
		if ak <= 0 {
			continue
		}
		g.VOutW[k] += dz * ak
		du := dz * m.VOutW[k]
		g.VHidB[k] += du
		base := k * m.H
		for j, sj := range s {
			g.VHidW[base+j] += du * sj
			g.ds[j] += du * m.VHidW[base+j]
		}
	}
}

// backHead propagates one option's score gradient through the output layer
// and the ReLU hidden layer into g, writing the hidden-input gradient into dx.
func (m *Model) backHead(dy float32, x, a []float32, g *Grads, dx []float32) {
	for i := range dx {
		dx[i] = 0
	}
	g.OutB += dy
	for h := 0; h < m.Hidden; h++ {
		g.da[h] = dy * m.OutW[h]
	}
	for h := 0; h < m.Hidden; h++ {
		d := g.da[h]
		if d == 0 || a[h] <= 0 {
			g.dz[h] = 0
			continue
		}
		g.dz[h] = d
		g.OutW[h] += dy * a[h] // dL/dOutW[h] = dy·a[h] (the missing block would have trained a headless net)
		g.HidB[h] += d
		base := h * m.InW
		for i, xi := range x {
			if xi != 0 {
				g.HidW[base+i] += d * xi
			}
		}
	}
	for h := 0; h < m.Hidden; h++ {
		d := g.dz[h]
		if d == 0 {
			continue
		}
		base := h * m.InW
		for i, xi := range x {
			if xi != 0 {
				dx[i] += m.HidW[base+i] * d
			}
		}
	}
}

// Argmax returns the index (into ex.Options) of the highest-scoring
// LABELLED option, ties to the first, computed exactly as Loss's top-1
// agreement reads it (the same float64 score sum, the same tie rule). ok is
// false when the example has no labelled option. Read-only: a readout for
// the trainer's per-kind holdout table (which option the model would pick,
// so a caller can ask whether it is a bot pick or an override).
func (m *Model) Argmax(ex Example) (idx int, ok bool) {
	labelled, _, _, _, ys := m.forwardExample(ex)
	if len(labelled) == 0 {
		return 0, false
	}
	best := 0
	for k := 1; k < len(ys); k++ {
		if ys[k] > ys[best] {
			best = k
		}
	}
	return labelled[best], true
}

// agreement reports whether the argmax over the labelled options lands in
// the teacher-preferred set. Ties go to the FIRST argmax (the lowest option
// index in the record's order), matching the engine's tie rule.
func agreement(ex Example, labelled []int, ys []float64) (eligible, agree bool) {
	if ex.Visits != nil {
		return visitAgreement(ex, labelled, ys)
	}
	hasPref := false
	for _, i := range labelled {
		if ex.Options[i].Target.Preferred {
			hasPref = true
			break
		}
	}
	if !hasPref {
		return false, false
	}
	best := 0
	for k := 1; k < len(ys); k++ {
		if ys[k] > ys[best] {
			best = k
		}
	}
	return true, ex.Options[labelled[best]].Target.Preferred
}

func huber(e, delta float64) float64 {
	ae := e
	if ae < 0 {
		ae = -ae
	}
	if ae <= delta {
		return 0.5 * e * e
	}
	return delta * (ae - 0.5*delta)
}

func huberGrad(e, delta float64) float64 {
	if e <= -delta {
		return -delta
	}
	if e >= delta {
		return delta
	}
	return e
}

// logSumExp returns log(Σ exp(v[i])) with the max subtracted first, so no
// term overflows: exp(v[i] − max) ≤ 1 always. Non-finite inputs are
// handled explicitly — a diverged forward pass may hand back ±Inf, and the
// naive form would compute exp(Inf − Inf) = NaN and poison the loss. The
// clampScore callers already bound their inputs, so this is defence in
// depth rather than the primary guard.
func logSumExp(v []float64) float64 {
	if len(v) == 0 {
		return math.Inf(-1)
	}
	max := v[0]
	for _, x := range v[1:] {
		if x > max {
			max = x
		}
	}
	if math.IsInf(max, 1) {
		return math.Inf(1)
	}
	if math.IsInf(max, -1) {
		return math.Inf(-1)
	}
	if math.IsNaN(max) {
		return 0
	}
	sum := 0.0
	for _, x := range v {
		sum += math.Exp(x - max)
	}
	return max + math.Log(sum)
}
