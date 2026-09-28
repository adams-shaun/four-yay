package searchprobe

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// competitionHistory builds the PolicyCompetition fixture: seat 1's library is
// forced so its weak spell A sits in the opening hand (and is the observed cast
// at the root) while the strictly stronger B is still deep in the library. The
// observed history is therefore reproducible only by a sampled world that also
// leaves B out of seat 1's hand until the observed cast; a world that draws B
// early makes the replay bot cast B instead and diverge.
func competitionHistory(t *testing.T) (PublicGame, History) {
	t.Helper()
	weak := syntheticCard(t, "Name:Weak Fixture\nManaCost:0\nTypes:Creature Goblin\nPT:1/1\nOracle:Fixture.\n")
	strong := syntheticCard(t, "Name:Strong Fixture\nManaCost:0\nTypes:Creature Goblin\nPT:9/9\nOracle:Fixture.\n")
	land := syntheticCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:Fixture.\n")
	decks := [][]*cards.Card{repeatCard(land, 20), repeatCard(land, 20)}
	// Seat 1: one weak (opening hand) and one strong (late) spell.
	decks[1][0], decks[1][1] = weak, strong
	setup := PublicGame{Names: []string{"a", "b"}, Decks: decks}
	// Force each seat's order: seat 1 = weak first, strong last; seat 0 = its
	// own deck untouched so only seat 1 carries the competition.
	planner := func(ctx rules.ShuffleContext) ([]state.ObjID, error) {
		order := make([]state.ObjID, len(ctx.Library))
		for i, card := range ctx.Library {
			order[i] = card.ID
		}
		if ctx.Player == 1 && ctx.Ordinal == 0 {
			var weakID, strongID, rest []state.ObjID
			for _, card := range ctx.Library {
				switch card.Name {
				case "Weak Fixture":
					weakID = append(weakID, card.ID)
				case "Strong Fixture":
					strongID = append(strongID, card.ID)
				default:
					rest = append(rest, card.ID)
				}
			}
			order = append(order[:0], weakID...)
			order = append(order, rest...)
			order = append(order, strongID...)
		}
		return order, nil
	}
	e, err := rules.NewHypotheticalPlanned(rules.Config{Seed: 1, Names: setup.Names, Decks: decks}, nil, planner)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	c := NewCollector(0)
	h := History{Actor: 0, Answers: make(map[int][]Action)}
	r := rand.New(rand.NewPCG(4, 8))
	pos := 0
	for i := 0; i < 500; i++ {
		frame, err := c.Capture(e, e.L.Events[pos:])
		if err != nil {
			t.Fatal(err)
		}
		h.Frames = append(h.Frames, frame)
		if observedWeakCast(h) {
			return setup, h
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("fixture ended before seat 1 cast its weak spell")
		}
		in := botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, r)
		if d.Player == 0 {
			h.Answers[i], err = c.Actions(d, in)
			if err != nil {
				t.Fatal(err)
			}
		}
		pos = len(e.L.Events)
		if err := e.SubmitHypothetical(in); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("fixture never observed seat 1's weak cast")
	return setup, h
}

// observedWeakCast reports whether the last observed frame contains seat 1's
// weak card moving from hand to the stack, which is the decision the sampler
// must reproduce. The identity dictionary is per-frame, so it is rebuilt here.
func observedWeakCast(h History) bool {
	last := h.Frames[len(h.Frames)-1]
	weak := make(map[uint32]bool)
	for _, id := range last.Identities {
		if id.Name == "Weak Fixture" {
			weak[id.ID] = true
		}
	}
	for _, ev := range last.Events {
		if ev.From == state.ZHand && ev.To == state.ZStack && weak[ev.Obj] {
			return true
		}
	}
	return false
}

func TestSamplerCompetitionExclusionKeepsPreferredCardOutOfHand(t *testing.T) {
	setup, h := competitionHistory(t)
	result, err := Sample(setup, h, SampleOptions{Seed: 4242, Attempts: 24, Worlds: 4, MaxSubmits: 500})
	if err != nil {
		t.Fatal(err)
	}
	// The probe rounds run unconstrained first: each rejecting probe teaches
	// the competing card (Strong Fixture, which the replay bot casts instead
	// of the observed Weak Fixture) into the exclusion store, and the frozen
	// sampling phase that follows is accepted attempt after attempt. At 24
	// attempts the adaptive probe budget is still the historical 2x4 floor,
	// and the second round -- holding the exclusion, accepting every probe
	// and teaching nothing -- is the teach-nothing exit, so its 4 accepted
	// probes join the pool beside the remaining 16 frozen attempts: exactly
	// 24-4 = 20 worlds. (Before the final-round keep rule this pin was
	// Attempts-8: the teach-nothing round's probes were dropped even though
	// they were proposed under the final store.)
	if result.CompetitionExclusions == 0 || result.HandToStackCauses.PolicyCompetition == 0 {
		t.Fatalf("fixture never produced a policy competition: excl=%d causes=%+v", result.CompetitionExclusions, result.HandToStackCauses)
	}
	if result.CompetitionExclusions > 4 || result.CompetitionUnguided != 0 || result.CompetitionResidual != 0 {
		t.Fatalf("unexpected teaching shape: excl=%d unguided=%d residual=%d", result.CompetitionExclusions, result.CompetitionUnguided, result.CompetitionResidual)
	}
	if result.Accepted != result.Attempts-4 {
		t.Fatalf("probe keep + frozen sampling did not recover its attempts: accepted=%d attempts=%d excl=%d unguided=%d rejected=%d first=%s",
			result.Accepted, result.Attempts, result.CompetitionExclusions, result.CompetitionUnguided, result.PrefixRejected, result.FirstRejection)
	}
	// The observed cast must never be reproduced by a world that also holds
	// the card the bot prefers there, so no accepted world may carry the
	// stronger card in seat 1's hand at the root.
	for _, w := range result.Worlds {
		for _, id := range w.Engine.G.Zone(state.ZHand, 1) {
			o := w.Engine.G.Obj(id)
			if o == nil || o.Card == nil || len(o.Card.Faces) == 0 {
				continue
			}
			if o.Card.Faces[0].Name == "Strong Fixture" {
				t.Fatal("accepted world put the preferred card in the opponent hand before the observed cast")
			}
		}
	}
}

// TestSamplerCompetitionTinyBudgetKeepsAWorld pins the sampling-phase
// reserve plus the final-round keep rule at a budget too small for the
// historical structure: with Attempts=6 the probes spend at most 5 attempts
// (the last is reserved for the frozen phase), so the frozen loop always
// runs at least once, and the final round's accepted probes join the pool --
// today's pre-reserve sampler returned zero worlds at this budget when the
// store grew. MinESS 1 mirrors the teacher's thin-pool contract; the default
// calibration gate (ESS >= Worlds) could not be met by a 2-world pool.
func TestSamplerCompetitionTinyBudgetKeepsAWorld(t *testing.T) {
	setup, h := competitionHistory(t)
	result, err := Sample(setup, h, SampleOptions{Seed: 4242, Attempts: 6, Worlds: 1, MinESS: 1, MaxSubmits: 500})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted < 1 || len(result.Worlds) < 1 {
		t.Fatalf("tiny budget sampled nothing: accepted=%d worlds=%d excl=%d rejected=%d",
			result.Accepted, len(result.Worlds), result.CompetitionExclusions, result.PrefixRejected)
	}
	if result.CompetitionExclusions == 0 {
		t.Fatalf("tiny budget never learned an exclusion: causes=%+v", result.HandToStackCauses)
	}
}

// TestProbeAttemptBudget pins the adaptive probe-cap arithmetic: the 2x4
// historical floor holds for small budgets, and larger budgets spend a
// quarter of their attempts probing (24 stays at the floor because 24/4 = 6
// < 8, so the existing fixture shapes keep today's structure).
func TestProbeAttemptBudget(t *testing.T) {
	for attempts, want := range map[int]int{1: 8, 7: 8, 8: 8, 24: 8, 31: 8, 32: 8, 33: 8, 128: 32, 512: 128} {
		if got := probeAttemptBudget(attempts); got != want {
			t.Fatalf("probeAttemptBudget(%d) = %d, want %d", attempts, got, want)
		}
	}
}

// TestFeasibleExclusionsClampsAndRelaxes pins the plan-time translation of
// stored exclusions: a Through past the library size clamps to the whole
// shuffle, and a windowed exclusion whose copies cannot all fit beyond the
// window relaxes by exactly the overflow (cap = copies beyond the tail's
// room) instead of turning every order infeasible.
func TestFeasibleExclusionsClampsAndRelaxes(t *testing.T) {
	cards := []proposalCard{{ID: 1, Name: "C"}, {ID: 2, Name: "C"}, {ID: 3, Name: "C"}, {ID: 4, Name: "C"}, {ID: 5, Name: "L"}}
	out := feasibleExclusions([]exclusionConstraint{
		{Through: 99, Name: "C", Cap: 0},
		{Through: 1, Name: "C", Cap: 0},
		{Through: 2, Name: "C", Cap: 0},
	}, cards, nil)
	if out[0].Through != 5 || out[0].Cap != 4 {
		t.Fatalf("past-end Through did not clamp: %+v", out[0])
	}
	if out[1].Through != 1 || out[1].Cap != 0 {
		t.Fatalf("windowed exclusion with room relaxed: %+v", out[1])
	}
	if out[2].Through != 2 || out[2].Cap != 1 {
		t.Fatalf("windowed exclusion without room did not relax: %+v", out[2])
	}
	// A deadline-derived Cap is reduced by the copies the pre-shuffle hand
	// already holds -- the same reduction the deadline itself gets. The two
	// library copies stay consistent with the hand copy (deck accounting), so
	// no relaxation overrides the reduction.
	cards2 := []proposalCard{{ID: 1, Name: "C"}, {ID: 2, Name: "C"}, {ID: 3, Name: "L"}, {ID: 4, Name: "L"}, {ID: 5, Name: "L"}}
	out = feasibleExclusions([]exclusionConstraint{{Through: 3, Name: "C", Cap: 1}}, cards2, map[string]int{"C": 1})
	if out[0].Through != 3 || out[0].Cap != 0 {
		t.Fatalf("hand copies did not reduce the deadline-derived cap: %+v", out[0])
	}
}

// TestRecordCompetitionExclusionSpansPriorEpochsAndSkipsDeadlines pins the
// learning rule: the competing card is excluded from every earlier shuffle
// across that shuffle's whole drawn window, from the cast's own shuffle up to
// the observed draw count, and an epoch whose compiled deadlines already
// place the card in hand by an observation gets the deadline's count as its
// Cap -- "exactly the observed copies, no further draws" -- instead of a
// contradictory Cap 0 (which would make every plan infeasible). A frozen
// store (nil staging) teaches nothing and counts a residual.
func TestRecordCompetitionExclusionSpansPriorEpochsAndSkipsDeadlines(t *testing.T) {
	epochs := map[epochKey]epochConstraints{
		{Player: 1, Ordinal: 1}: {Deadlines: []deadlineConstraint{{Through: 9, Name: "Strong Fixture", Count: 1}}},
		{Player: 1, Ordinal: 0}: {Deadlines: []deadlineConstraint{{Through: 3, Name: "Other Fixture", Count: 1}}},
	}
	drawStates := []map[state.PlayerID]frameDrawState{{1: {ordinal: 3, draws: 5, completed: []int{7, 4}}}}
	frame := Frame{Events: []ObservedEvent{{From: state.ZHand, To: state.ZStack, Obj: 77}}}
	known := map[uint32]Identity{77: {ID: 77, Name: "Strong Fixture", Owner: 1}}
	result := &SampleResult{}
	p := &proposalState{observer: NewCollector(0), result: result, epochs: epochs, staging: newExclusionStore()}
	p.recordCompetitionExclusion(0, frame, known, drawStates)
	if result.CompetitionExclusions != 1 || result.CompetitionUnguided != 0 {
		t.Fatalf("teaching miscounted: %+v", result)
	}
	want := map[epochKey][]exclusionConstraint{
		// Epoch 0's shuffle drew 7 cards in its lifetime and the observation
		// never exited this card there: none of them may be this card.
		{Player: 1, Ordinal: 0}: {{Through: 7, Name: "Strong Fixture", Cap: 0}},
		// Epoch 1's deadlines already require one copy (observed exit): the
		// exclusion is exactly that copy, forbidding further draws.
		{Player: 1, Ordinal: 1}: {{Through: 4, Name: "Strong Fixture", Cap: 1}},
		// The cast's own shuffle has no observed exit of the card: excluded up
		// to the observed draw count.
		{Player: 1, Ordinal: 2}: {{Through: 5, Name: "Strong Fixture", Cap: 0}},
	}
	got := map[epochKey][]exclusionConstraint(p.staging.byEpoch)
	if len(got) != len(want) {
		t.Fatalf("staging epochs = %v, want %v", got, want)
	}
	for key, list := range want {
		if !slices.Equal(got[key], list) {
			t.Fatalf("staging[%v] = %v, want %v", key, got[key], list)
		}
	}
	// The comment above covers every epoch; the deadline epoch (ordinal 1) is
	// present with its cap, so nothing is skipped away silently.
	if _, ok := got[epochKey{Player: 1, Ordinal: 1}]; !ok {
		t.Fatal("deadline epoch lost its cap exclusion")
	}
	// The actor seat is never taught: its frames replay exactly.
	result2 := &SampleResult{}
	actorFrame := Frame{Events: []ObservedEvent{{From: state.ZHand, To: state.ZStack, Obj: 78}}}
	knownActor := map[uint32]Identity{78: {ID: 78, Name: "Weak Fixture", Owner: 0}}
	p2 := &proposalState{observer: NewCollector(0), result: result2, epochs: epochs, staging: newExclusionStore()}
	p2.recordCompetitionExclusion(0, actorFrame, knownActor, drawStates)
	if result2.CompetitionUnguided != 1 || p2.staging.size() != 0 {
		t.Fatalf("actor-owned rejection was taught: %+v store=%v", result2, p2.staging.byEpoch)
	}
	// A frozen store counts a residual and teaches nothing.
	result3 := &SampleResult{}
	p3 := &proposalState{observer: NewCollector(0), result: result3, epochs: epochs, staging: nil}
	p3.recordCompetitionExclusion(0, frame, known, drawStates)
	if result3.CompetitionResidual != 1 || result3.CompetitionExclusions != 0 {
		t.Fatalf("frozen rejection miscounted: %+v", result3)
	}
}

// TestExclusionConstraintPrunesAndDownWeights is the constraint-level check:
// an exclusion is a hard feasibility constraint (the sampled orders never
// violate it) and the sampler's existing weight machinery down-weights the
// constrained space by exactly the fraction of orders it admitted (CR's
// importance weight, the same bookkeeping every deadline already uses).
func TestExclusionConstraintPrunesAndDownWeights(t *testing.T) {
	cards := []proposalCard{{ID: 1, Name: "W"}, {ID: 2, Name: "S"}, {ID: 3, Name: "L"}, {ID: 4, Name: "L"}}
	exclusions := []exclusionConstraint{{Through: 1, Name: "S", Cap: 0}}
	plan, err := newConstrainedPermutation(cards, nil, nil, exclusions)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(18); plan.total.Int64() != want {
		t.Fatalf("constrained total = %s, want %d", plan.total, want)
	}
	if want := math.Log(18.0 / 24.0); math.Abs(plan.weight-want) > 1e-12 {
		t.Fatalf("weight = %g, want %g", plan.weight, want)
	}
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 64; i++ {
		order, _, compatible, err := plan.sample(r)
		if err != nil || !compatible {
			t.Fatalf("sample %d: compatible=%v err=%v", i, compatible, err)
		}
		if order[0] == 2 {
			t.Fatalf("sample %d placed the excluded card first: %v", i, order)
		}
	}
}

func TestSamplerCompetitionExclusionIsWorkerDeterministic(t *testing.T) {
	setup, h := competitionHistory(t)
	first, err := Sample(setup, h, SampleOptions{Seed: 99, Attempts: 24, Worlds: 4, MaxSubmits: 500})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sample(setup, h, SampleOptions{Seed: 99, Attempts: 24, Worlds: 4, MaxSubmits: 500})
	if err != nil {
		t.Fatal(err)
	}
	if first.Accepted != second.Accepted || first.CompetitionExclusions != second.CompetitionExclusions || first.ESS != second.ESS || first.PrefixRejected != second.PrefixRejected {
		t.Fatalf("sampling depended on run: first=%+v second=%+v", first, second)
	}
	if len(first.Worlds) != len(second.Worlds) {
		t.Fatalf("world counts differ: %d vs %d", len(first.Worlds), len(second.Worlds))
	}
	for i := range first.Worlds {
		if first.Worlds[i].Engine.L.Head() != second.Worlds[i].Engine.L.Head() || first.Worlds[i].Config.Seed != second.Worlds[i].Config.Seed {
			t.Fatalf("world %d differs across runs", i)
		}
	}
}
