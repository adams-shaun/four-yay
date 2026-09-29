package mbtest

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"

	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// Mode selects the mock client's answer strategy. Both modes answer strictly
// from the PromptMessage alone -- never from the decision or the view a
// real ManaBrew client could not see -- so a mapping gap the translator has
// is found the same way a real client would find it.
type Mode int

const (
	// ModeFirstLegal always answers with the first legal choice the prompt
	// offers: the first advertised action, the smallest offered count, the
	// deny side of a boolean, the identity order. It is deterministic and
	// exists so TestFirstLegalNeverRefused has a client that must never be
	// refused: every answer it produces is drawn from what the prompt itself
	// advertised, so a rejection means the translator's own prompt and
	// parser disagree about what is legal.
	ModeFirstLegal Mode = iota
	// ModeSeededRandom answers with a seeded pseudo-random choice among the
	// legal shapes the prompt allows, so the census exercises more of the
	// option space (multiple mana sources, optional attacks/blocks,
	// mulligans) than the deterministic client ever reaches.
	ModeSeededRandom
)

// MockClient is a deterministic, in-process stand-in for a real ManaBrew
// client: it answers every prompt type the wire protocol defines, reading
// only the PromptMessage handed to it (never the underlying gorge decision
// or view -- those are not on the wire and a real client cannot see them).
type MockClient struct {
	mode Mode
	rng  *rand.Rand
}

// NewFirstLegalClient returns a MockClient in ModeFirstLegal.
func NewFirstLegalClient() *MockClient { return &MockClient{mode: ModeFirstLegal} }

// NewSeededRandomClient returns a MockClient in ModeSeededRandom, seeded so
// two runs at the same seed answer identically.
func NewSeededRandomClient(seed uint64) *MockClient {
	return &MockClient{mode: ModeSeededRandom, rng: rand.New(rand.NewPCG(uint64(seed), 0))}
}

// ErrNoResponse is returned when a prompt type carries nothing this client
// knows how to answer (gameOver, or a future protocol addition this client
// has not been taught).
var ErrNoResponse = fmt.Errorf("mbtest: mock client has no answer for this prompt type")

// Answer builds the ClientMessage that answers msg, strictly from what msg
// itself carries. It is the one entry point TranslatingSeat calls; every
// prompt-type-specific policy lives in the answerXxx helpers below.
func (c *MockClient) Answer(msg mb.PromptMessage) (mb.ClientMessage, error) {
	if msg.Input.Value == nil {
		return mb.ClientMessage{}, fmt.Errorf("mbtest: prompt carries no input")
	}
	var out mb.PromptOutputValue
	switch in := msg.Input.Value.(type) {
	case mb.ChooseActionInput:
		out = c.answerChooseAction(in)
	case mb.ChooseBoardTargetsInput:
		out = c.answerBoardTargets(in)
	case mb.ChooseAttackersInput:
		out = c.answerAttackers(in)
	case mb.ChooseBlockersInput:
		out = c.answerBlockers(in)
	case mb.MulliganInput:
		out = c.answerMulligan(in)
	case mb.MulliganPutBackInput:
		out = c.answerMulliganPutBack(in)
	case mb.ChooseNumberInput:
		out = c.answerChooseNumber(in)
	case mb.ChooseCardsInput:
		out = c.answerChooseCards(in)
	case mb.ChooseColorInput:
		out = c.answerChooseColor(in)
	case mb.ChooseBooleanInput:
		out = c.answerChooseBoolean(in)
	case mb.ChooseFromSelectionInput:
		out = c.answerChooseFromSelection(in)
	case mb.ScryInput:
		out = c.answerScry(in)
	case mb.ReorderInput:
		out = c.answerReorder(in)
	case mb.PayManaCostInput:
		out = c.answerPayManaCost(in)
	case mb.RevealCardsInput:
		out = mb.RevealCardsAcknowledged{}
	case mb.DiceRolledInput:
		out = mb.DiceRolledAcknowledged{}
	case mb.ChooseDamageAssignmentOrderInput:
		out = c.answerDamageAssignmentOrder(in)
	case mb.ChooseCombatDamageAssignmentInput:
		out = c.answerCombatDamageAssignment(in)
	case mb.GameOverInput:
		return mb.ClientMessage{}, ErrNoResponse
	default:
		return mb.ClientMessage{}, fmt.Errorf("%w: %T", ErrNoResponse, in)
	}
	return mb.ClientMessage{Value: mb.ClientResponse{
		Kind:     "response",
		PromptID: msg.PromptID,
		Action:   mb.PromptOutput{Type: msg.Input.Value.PromptType(), Output: mb.PromptOutputData{Value: out}},
	}}, nil
}

// --- index-picking helpers, shared by every prompt kind below ---

// distinct returns count distinct indices in [0,n): the smallest-first run
// in ModeFirstLegal, a random sample (no repeats) in ModeSeededRandom. count
// is clamped to [0,n].
func (c *MockClient) distinct(n, count int) []int {
	if count > n {
		count = n
	}
	if count <= 0 {
		return nil
	}
	if c.mode == ModeFirstLegal {
		out := make([]int, count)
		for i := range out {
			out[i] = i
		}
		return out
	}
	perm := c.rng.Perm(n)
	out := append([]int(nil), perm[:count]...)
	sort.Ints(out)
	return out
}

// repeated returns count indices in [0,n), repeats allowed: index 0 every
// time in ModeFirstLegal, an independent random draw each time otherwise.
func (c *MockClient) repeated(n, count int) []int {
	if n <= 0 || count <= 0 {
		return nil
	}
	out := make([]int, count)
	for i := range out {
		if c.mode == ModeFirstLegal {
			out[i] = 0
		} else {
			out[i] = c.rng.IntN(n)
		}
	}
	return out
}

// permutation returns a full ordering of [0,n): identity in ModeFirstLegal,
// a random permutation otherwise.
func (c *MockClient) permutation(n int) []int {
	if c.mode == ModeFirstLegal {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	return c.rng.Perm(n)
}

// boolAnswer is the boolean policy: deny (false) in ModeFirstLegal (both
// sides of a chooseBoolean prompt are always legal, so this is a fixed,
// deterministic pick, not a "the correct legal answer" claim); a coin flip
// otherwise.
func (c *MockClient) boolAnswer() bool {
	if c.mode == ModeFirstLegal {
		return false
	}
	return c.rng.IntN(2) == 0
}

// coin reports a weighted true with probability num/den in ModeSeededRandom,
// and always false in ModeFirstLegal (the deterministic client never opts
// into an optional branch it does not have to).
func (c *MockClient) coin(num, den int) bool {
	if c.mode == ModeFirstLegal {
		return false
	}
	return c.rng.IntN(den) < num
}

// --- per-prompt-type policies ---

// answerChooseAction answers chooseAction: act on an advertised action when
// one exists, pass otherwise. ModeSeededRandom picks among the advertised
// actions at random instead of always the first, so different mana sources
// and abilities get exercised across a run.
func (c *MockClient) answerChooseAction(in mb.ChooseActionInput) mb.PromptOutputValue {
	if len(in.Actions) == 0 {
		return mb.PassOutput{}
	}
	idx := 0
	if c.mode == ModeSeededRandom {
		// Sometimes pass even when actions are advertised: an always-act
		// policy livelocks on a repeatable zero-cost ability (MBX-6 smoke,
		// Ghost Town's "{0}: becomes a creature" with an empty hand -- the
		// one advertised action, re-offered at every priority window).
		// Passing priority is always legal (CR 117.3b), so the extra
		// out-of-range draw meaning "pass" keeps the answer in-policy.
		if c.rng.IntN(len(in.Actions)+1) == len(in.Actions) {
			return mb.PassOutput{}
		}
		idx = c.rng.IntN(len(in.Actions))
	}
	return mb.ActOutput{ActionID: in.Actions[idx].ID}
}

// answerBoardTargets picks MinTargets candidates -- the smallest legal
// count, since MinTargets is on the wire (unlike a scry ask's Min).
// A set-level target constraint the wire cannot express per candidate (spec
// gap G-4) arrives as the translator's sentence in presentation.description
// (prompt_target_set.go's targetSetSentences), and the translator orders the
// candidates so the first MinTargets form one legal set (orderTargetOptions).
// For such an ask this client takes the offered prefix in order, in BOTH
// modes -- the same "a constraint-bound ask's legal answer is near-forced"
// stance answerChooseCards takes for its budget sentences; the random draw
// would otherwise pick candidates that violate a constraint it cannot
// evaluate. An unconstrained ask keeps the random/first policy below.
func (c *MockClient) answerBoardTargets(in mb.ChooseBoardTargetsInput) mb.PromptOutputValue {
	if setConstraintBound(in.Presentation.Description) && in.MinTargets > 0 && len(in.Candidates) >= in.MinTargets {
		chosen := make([]mb.TargetRef, 0, in.MinTargets)
		for _, cand := range in.Candidates[:in.MinTargets] {
			chosen = append(chosen, cand)
		}
		return mb.BoardTargetsDecision{Chosen: chosen}
	}
	idxs := c.distinct(len(in.Candidates), in.MinTargets)
	chosen := make([]mb.TargetRef, 0, len(idxs))
	for _, i := range idxs {
		chosen = append(chosen, in.Candidates[i])
	}
	return mb.BoardTargetsDecision{Chosen: chosen}
}

// setConstraintBound reports whether a prompt presentation description
// carries one of the set-level target constraint sentences
// prompt_target_set.go's targetSetSentences renders. The wording is a
// CONTRACT between the translator and this client (and any real client that
// reads the constraint the same way), like chooseConstraint's budget
// sentences; rewording either side changes what the other can honour. The
// contract covers BOTH group wordings: the cap-1 exclusivity sentence and
// the raised-cap sentences ("may be chosen together" appears in both), so a
// cap-aware ask is still answered by the translator's ordered prefix rather
// than a random distinct selection that can violate the cap.
func setConstraintBound(desc string) bool {
	return strings.Contains(desc, "must share one controller") ||
		strings.Contains(desc, "must share a property") ||
		strings.Contains(desc, "may share a property") ||
		strings.Contains(desc, "may be chosen together") ||
		strings.Contains(desc, "mutually exclusive")
}

// answerAttackers assigns every MustAttack creature to its first (or a
// random) valid target and, in ModeSeededRandom, sometimes attacks with an
// optional attacker too -- declaring no attacks at all is always legal
// (CR 508.1a), so ModeFirstLegal only ever satisfies the forced attacks.
func (c *MockClient) answerAttackers(in mb.ChooseAttackersInput) mb.PromptOutputValue {
	var assignments []mb.AttackerAssignment
	for _, a := range in.Attackers {
		if len(a.ValidTargetIDs) == 0 {
			continue
		}
		if !a.MustAttack && !c.coin(1, 2) {
			continue
		}
		target := a.ValidTargetIDs[0]
		if c.mode == ModeSeededRandom {
			target = a.ValidTargetIDs[c.rng.IntN(len(a.ValidTargetIDs))]
		}
		assignments = append(assignments, mb.AttackerAssignment{AttackerID: a.AttackerID, TargetID: target})
	}
	return mb.DeclareAttackersDecision{Assignments: assignments}
}

// answerBlockers assigns every MustBeBlocked attacker a blocker not already
// used, and, in ModeSeededRandom, sometimes blocks an optional attacker too.
// Declaring no blocks at all is always legal (CR 509.1b) unless a specific
// attacker forces one. Within those bounds it honours, from the prompt's
// own fields:
// CR 509.1c's must-be-blocked flag (the attacker needs at least one, and at
// least MinBlockers, legal blockers), CR 509.1a's MinMaxBlocker bounds (an
// optional attacker with MinBlockers > 1 accepts either no blocker or at
// least MinBlockers -- a 1-blocker answer is illegal, so the optional
// policy assigns max(1, MinBlockers) whenever it chooses to block at all),
// MaxBlockers as the ceiling, and one blocker per attacker (a blocker is
// consumed once used). Assignments are all-or-nothing per attacker: if the
// wanted count cannot be filled from the still-unused valid blockers, the
// attacker takes none -- but the mandatory attackers go first so an
// optional coin cannot consume a blocker a mandatory ask still needs.
func (c *MockClient) answerBlockers(in mb.ChooseBlockersInput) mb.PromptOutputValue {
	used := map[string]bool{}
	var assignments []mb.BlockerAssignment
	assign := func(a mb.BlockableAttackerDto, want int) {
		if a.MaxBlockers != nil && want > *a.MaxBlockers {
			want = *a.MaxBlockers
		}
		if a.MustBeBlocked && want < 1 {
			// CR 509.1c: a required attacker needs at least one blocker even
			// when the wire omitted MinBlockers (prompt_combat.go publishes it
			// only above zero, so MustBeBlocked=true,MinBlockers=0 is a legal
			// wire shape). The quota still caps the fill via MaxBlockers and
			// the valid-blocker pool.
			want = 1
		}
		if want < 1 {
			return
		}
		var batch []mb.BlockerAssignment
		for _, bid := range a.ValidBlockerIDs {
			if len(batch) == want {
				break
			}
			if used[bid] {
				continue
			}
			batch = append(batch, mb.BlockerAssignment{BlockerID: bid, AttackerID: a.AttackerID})
			used[bid] = true
		}
		if len(batch) < want {
			// Roll back: a partial fill under MinBlockers would itself be an
			// illegal declaration.
			for _, ba := range batch {
				delete(used, ba.BlockerID)
			}
			return
		}
		assignments = append(assignments, batch...)
	}
	for _, a := range in.Attackers {
		if a.MustBeBlocked {
			assign(a, a.MinBlockers)
		}
	}
	for _, a := range in.Attackers {
		if a.MustBeBlocked {
			continue
		}
		if c.coin(1, 2) {
			// Optional: one blocker, or the full MinBlockers quota when the
			// bounds demand it.
			assign(a, a.MinBlockers)
		}
	}
	return mb.DeclareBlockersDecision{Assignments: assignments}
}

// answerMulligan keeps the opening hand in ModeFirstLegal (the simplest
// always-legal answer) and occasionally mulligans in ModeSeededRandom, so a
// run's census also exercises mulliganPutBack.
func (c *MockClient) answerMulligan(in mb.MulliganInput) mb.PromptOutputValue {
	return mb.MulliganDecision{Keep: !c.coin(1, 5)}
}

// answerMulliganPutBack bottoms the first Count hand cards (a random subset
// in ModeSeededRandom).
func (c *MockClient) answerMulliganPutBack(in mb.MulliganPutBackInput) mb.PromptOutputValue {
	idxs := c.distinct(len(in.HandCardIDs), in.Count)
	ids := make([]string, 0, len(idxs))
	for _, i := range idxs {
		ids = append(ids, in.HandCardIDs[i])
	}
	return mb.MulliganPutBackDecision{CardIDs: ids}
}

// answerChooseNumber picks Min (a random value in [Min,Max] otherwise).
func (c *MockClient) answerChooseNumber(in mb.ChooseNumberInput) mb.PromptOutputValue {
	n := in.Min
	if c.mode == ModeSeededRandom && in.Max > in.Min {
		n = in.Min + c.rng.IntN(in.Max-in.Min+1)
	}
	return mb.NumberDecision{ChosenNumber: &n}
}

// answerChooseCards picks the smallest legal number of offered cards (Min).
// A value constraint the prompt carries (chooseConstraint's
// "Total value must be at least N." / "Total value must not exceed N."
// sentences -- the spec's G-4 fence made machine-readable; rules/cast.go's
// tapcost asks pose that floor over Option.Value=power, which the
// translator renders as the cards' power) is honoured from the cards'
// power: the smallest count in [Min, Max] for which some subset's power sum
// lies in [floor, ceiling], strongest cards first so a floor is reached with
// as few cards as possible. Both bounds are honoured together -- a
// strongest-first pick that clears the floor is rejected when it overshoots
// the ceiling and the search backs off to a cheaper card. The deterministic
// modes pick the same way -- a constraint-bound ask's legal answer is
// near-forced. (A stat:TapPowerValue static that trades power for toughness
// -- Tapestry Warden -- is invisible on the wire: the prompt's power is the
// view's layer-derived power, so such an ask can still be under-shot; that
// lossiness is spec gap G-4.)
func (c *MockClient) answerChooseCards(in mb.ChooseCardsInput) mb.PromptOutputValue {
	n := len(in.Cards)
	if n == 0 {
		return mb.ChooseCardsDecision{ChosenCardIDs: []string{}}
	}
	floor, hasFloor := constraintNumber(in.Presentation.Description, "Total value must be at least ")
	ceiling, hasCeiling := constraintNumber(in.Presentation.Description, "Total value must not exceed ")
	if hasFloor || hasCeiling {
		order := make([]int, n)
		for i := range order {
			order[i] = i
		}
		val := func(i int) int {
			if in.Cards[i].Power == nil {
				return 0
			}
			p, _ := strconv.Atoi(*in.Cards[i].Power)
			return p
		}
		// Strongest first: reaching a floor with the fewest cards, and the
		// deterministic tie order the earlier policy used.
		sort.SliceStable(order, func(a, b int) bool { return val(order[a]) > val(order[b]) })
		lo, hi := in.Min, in.Max
		if lo < 0 {
			lo = 0
		}
		if hi > n {
			hi = n
		}
		if lo > hi {
			lo = hi
		}
		for k := lo; k <= hi; k++ {
			if idxs := chooseValueSubset(order, val, k, floor, hasFloor, ceiling, hasCeiling); idxs != nil {
				ids := make([]string, 0, len(idxs))
				for _, i := range idxs {
					ids = append(ids, in.Cards[i].ID)
				}
				return mb.ChooseCardsDecision{ChosenCardIDs: ids}
			}
		}
		// No subset at any legal count satisfies both bounds. The engine poses
		// such an ask only when one exists; still answer the smallest legal
		// count (strongest first) so the response is well formed and any
		// rejection reads as the translator's own prompt/parser disagreement.
		count := lo
		if count > n {
			count = n
		}
		ids := make([]string, 0, count)
		for _, i := range order[:count] {
			ids = append(ids, in.Cards[i].ID)
		}
		return mb.ChooseCardsDecision{ChosenCardIDs: ids}
	}
	idxs := c.distinct(n, in.Min)
	ids := make([]string, 0, len(idxs))
	for _, i := range idxs {
		ids = append(ids, in.Cards[i].ID)
	}
	return mb.ChooseCardsDecision{ChosenCardIDs: ids}
}

// chooseValueSubset returns exactly k of order's indices whose val sum lies
// in [floor, ceiling] (a bound applies only when its has flag is set), or
// nil when none does. order is expected strongest-first; the walk tries
// indices in that order and returns the first legal subset, so a floor is
// met with the strongest cards first. A budget bounds the worst case (a
// large k over many cards); reaching it returns nil, exactly as an ask with
// no legal subset does.
func chooseValueSubset(order []int, val func(int) int, k, floor int, hasFloor bool, ceiling int, hasCeiling bool) []int {
	if k < 0 || k > len(order) {
		return nil
	}
	chosen := make([]int, 0, k)
	budget := chooseCardsSearchBudget
	var walk func(start, sum int) bool
	walk = func(start, sum int) bool {
		if len(chosen) == k {
			return (!hasFloor || sum >= floor) && (!hasCeiling || sum <= ceiling)
		}
		if budget <= 0 || len(chosen)+(len(order)-start) < k {
			return false
		}
		budget--
		for i := start; i < len(order); i++ {
			chosen = append(chosen, order[i])
			if walk(i+1, sum+val(order[i])) {
				return true
			}
			chosen = chosen[:len(chosen)-1]
		}
		return false
	}
	if walk(0, 0) {
		return chosen
	}
	return nil
}

// chooseCardsSearchBudget caps the subset walk so a pathological ask cannot
// stall the mock. It is far above the combinations any posed choose-cards
// ask offers.
const chooseCardsSearchBudget = 200000

// constraintNumber reads the integer after the given constraint sentence
// prefix in a prompt presentation description (chooseConstraint's exact
// wording, see its doc). ok is false when the sentence is absent or its
// number unparseable; a PRESENT "...at least 0."/"...not exceed 0." reports
// ok true, because a zero budget (Decision.Budgeted with MaxSum 0) is a real
// bound the mock must honour, not an absent one.
func constraintNumber(desc, prefix string) (int, bool) {
	i := strings.Index(desc, prefix)
	if i < 0 {
		return 0, false
	}
	rest := desc[i+len(prefix):]
	j := 0
	for j < len(rest) && (rest[j] < '0' || rest[j] > '9') {
		j++
	}
	k := j
	for k < len(rest) && rest[k] >= '0' && rest[k] <= '9' {
		k++
	}
	if k == j {
		return 0, false
	}
	n, _ := strconv.Atoi(rest[j:k])
	return n, true
}

// answerChooseColor spends Amount across ValidColors, repeating the first
// colour when RepeatAllowed and spreading one-per-colour across the first
// Amount colours otherwise -- both always legal by the prompt's own fields.
func (c *MockClient) answerChooseColor(in mb.ChooseColorInput) mb.PromptOutputValue {
	chosen := map[string]int{}
	if len(in.ValidColors) == 0 || in.Amount <= 0 {
		return mb.ColorDecision{ChosenColors: chosen}
	}
	if in.RepeatAllowed {
		color := in.ValidColors[0]
		if c.mode == ModeSeededRandom {
			color = in.ValidColors[c.rng.IntN(len(in.ValidColors))]
		}
		chosen[color] = in.Amount
		return mb.ColorDecision{ChosenColors: chosen}
	}
	idxs := c.distinct(len(in.ValidColors), in.Amount)
	for _, i := range idxs {
		chosen[in.ValidColors[i]]++
	}
	return mb.ColorDecision{ChosenColors: chosen}
}

// answerChooseBoolean answers with the fixed/random boolean policy.
func (c *MockClient) answerChooseBoolean(in mb.ChooseBooleanInput) mb.PromptOutputValue {
	return mb.BooleanDecision{Value: c.boolAnswer()}
}

// answerChooseFromSelection picks the smallest legal count (MinTotal),
// repeating index 0 when any option allows repeats and spreading across the
// first MinTotal options otherwise.
func (c *MockClient) answerChooseFromSelection(in mb.ChooseFromSelectionInput) mb.PromptOutputValue {
	if in.MinTotal <= 0 || len(in.Options) == 0 {
		return mb.SelectionDecision{ChosenIndices: []int{}}
	}
	repeatable := false
	for _, o := range in.Options {
		if o.CanRepeat {
			repeatable = true
			break
		}
	}
	var idxs []int
	if repeatable {
		idxs = c.repeated(len(in.Options), in.MinTotal)
	} else {
		idxs = c.distinct(len(in.Options), in.MinTotal)
	}
	return mb.SelectionDecision{ChosenIndices: idxs}
}

// answerScry keeps nothing on top: ZoneCardIDs[0] (pile A / library top) is
// empty and every offered card goes to Zones[1]. This is always legal for
// every scry/surveil-style split ask gorge poses (Min is 0 in every such
// ask; effects/cardflow.go's effLookAndArrange builds Min:0, Max:k) even
// though the wire ScryInput carries no min/max field at all -- there is no
// way for a real ManaBrew client to learn the lower bound except by this
// same reasoning or a rejection, which is worth flagging if it is ever
// wrong (see the MB-8 report). ModeSeededRandom instead splits each card
// independently by coin flip, still legal under the same Min:0 fact, and
// exercises the "keep something on top" path the deterministic client never
// reaches.
func (c *MockClient) answerScry(in mb.ScryInput) mb.PromptOutputValue {
	if len(in.Zones) < 2 {
		// A malformed/degenerate prompt (fewer than two zones): everything
		// goes to the only zone offered.
		ids := make([]string, len(in.Cards))
		for i, card := range in.Cards {
			ids[i] = card.ID
		}
		return mb.ScryDecision{ZoneCardIDs: [][]string{ids}}
	}
	var pileA, pileB []string
	for _, card := range in.Cards {
		if c.coin(1, 2) {
			pileA = append(pileA, card.ID)
		} else {
			pileB = append(pileB, card.ID)
		}
	}
	return mb.ScryDecision{ZoneCardIDs: [][]string{pileA, pileB}}
}

// answerReorder keeps the offered order (a random permutation in
// ModeSeededRandom); every KArrange/KTriggerOrder reorder ask this prompt
// type answers requires ordering every offered item (Min == Max ==
// len(Options)), so any permutation is legal.
func (c *MockClient) answerReorder(in mb.ReorderInput) mb.PromptOutputValue {
	perm := c.permutation(len(in.Items))
	ids := make([]string, len(perm))
	for i, p := range perm {
		ids[i] = in.Items[p].ID
	}
	return mb.ReorderDecision{OrderedIDs: ids}
}

// answerPayManaCost prefers an activation action (advancing the payment one
// step, the engine re-prompts after each), then the wire "autofill" action
// via pay{auto:true}, then confirming from the pool (pay{auto:false}), and
// cancels only when none of those is offered. "autofill"-typed entries in
// Actions cannot be sent through act (only mana/activate/undoMana can); this
// client tells them apart by PaymentAction.Type, exactly as G-3/the payment
// doc requires of a well-behaved client.
func (c *MockClient) answerPayManaCost(in mb.PayManaCostInput) mb.PromptOutputValue {
	var stepActions, autofillActions []mb.PaymentAction
	for _, a := range in.Actions {
		switch a.Type {
		case "autofill":
			autofillActions = append(autofillActions, a)
		case "activateAbility", "undoMana":
			stepActions = append(stepActions, a)
		}
	}
	if len(stepActions) > 0 {
		idx := 0
		if c.mode == ModeSeededRandom {
			idx = c.rng.IntN(len(stepActions))
		}
		return mb.ActOutput{ActionID: stepActions[idx].ID}
	}
	if len(autofillActions) > 0 {
		return mb.PayOutput{Auto: true}
	}
	if in.CanConfirmFromPool {
		return mb.PayOutput{Auto: false}
	}
	return mb.CancelOutput{}
}

// answerDamageAssignmentOrder keeps the offered blocker order. Not reachable
// through internal/manabrew today (no dispatch builds this prompt type),
// kept so the client answers every protocol prompt type.
func (c *MockClient) answerDamageAssignmentOrder(in mb.ChooseDamageAssignmentOrderInput) mb.PromptOutputValue {
	perm := c.permutation(len(in.BlockerIDs))
	ids := make([]string, len(perm))
	for i, p := range perm {
		ids[i] = in.BlockerIDs[p]
	}
	return mb.DamageAssignmentOrderDecision{OrderedBlockerIDs: ids}
}

// answerCombatDamageAssignment dumps all damage on the first blocker (or the
// defender when there are none). Not reachable through internal/manabrew
// today; kept for the same reason as answerDamageAssignmentOrder.
func (c *MockClient) answerCombatDamageAssignment(in mb.ChooseCombatDamageAssignmentInput) mb.PromptOutputValue {
	if len(in.BlockerIDs) == 0 {
		if in.DefenderID == "" {
			return mb.DamageAssignmentDecision{}
		}
		return mb.DamageAssignmentDecision{Assignments: []mb.DamageAssignment{{AssigneeID: in.DefenderID, Damage: in.TotalDamage}}}
	}
	return mb.DamageAssignmentDecision{Assignments: []mb.DamageAssignment{{AssigneeID: in.BlockerIDs[0], Damage: in.TotalDamage}}}
}
