package searchprobe

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// PublicGame has intentionally no original seed, intents, log, or engine.
// Definitions are immutable corpus data for the declared public deck lists.
type PublicGame struct {
	Names        []string
	Decks        [][]*cards.Card
	Tokens       map[string]*cards.Card
	StartingLife int32
}
type History struct {
	Actor   state.PlayerID
	Frames  []Frame
	Answers map[int][]Action
}
type SampleOptions struct {
	Seed                         uint64
	Attempts, Worlds, MaxSubmits int
	// MinESS is the effective-sample-size gate for resampling Worlds. Zero
	// keeps the calibration contract (ESS >= Worlds). A positive value lets a
	// caller (the search-teacher spike) resample Worlds with replacement from a
	// thinner accepted pool; duplicates are then counted in Duplicates.
	MinESS float64
	// NoLandExclusion turns off the declined-land-drop exclusion
	// (recordExtraLandExclusion). It exists to MEASURE that exclusion: the
	// soundness test samples without it and checks that no accepted world
	// falls in the region it removes. Production callers leave it false.
	NoLandExclusion bool
	// ComparePotentialActions restores the pre-optimisation replay: every
	// scratch capture spends the seat's potential-action walk
	// (rules.PotentialActions) and the observed board is compared with that
	// field intact. It exists to MEASURE the skip -- with it set,
	// BoardPotentialActionsOnly counts the rejections the field alone decided,
	// and a run that reports zero says the skip accepts exactly the same
	// worlds. Production callers leave it false.
	ComparePotentialActions bool
	// Parallelism is how many goroutines run the frozen sampling phase's
	// attempts (<=1: sequential). It changes wall clock only, never the
	// result. A caller that already saturates its cores with whole games
	// (cmd/searchteacher -workers) leaves it at 1; a live seat answering one
	// decision at a time is what it is for.
	Parallelism int
	// KnownCards turns on the known-card constraint (pn21): the History's
	// known-card projection (ProjectKnownCards) is computed once, and a world
	// whose replay does not place every known card in its known zone and
	// library position is rejected (SampleResult.KnownCardViolations). A world
	// that replays the whole observed history already honours every claim
	// the projection makes, so on the rejection sampler the constraint is a
	// soundness check that should never fire; it exists so a proposal that
	// is NOT a full replay (a redeal) can be held to the same knowledge.
	// Zero value off: the sampler and its output are byte-identical to
	// before.
	KnownCards bool
	// Redeal, when non-nil, turns on the redeal fallback (pn21 stretch; see
	// RedealBase): when the rejection sampler ends without Worlds worlds, the
	// result's worlds are instead Worlds uniform redeals of Redeal.Engine's
	// hidden cards the seat does not know, pinning every known card. Nil is
	// off and changes nothing.
	Redeal *RedealBase
}
type World struct {
	Config   rules.Config
	Engine   *rules.Engine
	Observer *Collector
}
type SampleResult struct {
	Worlds                                                                                 []World `json:"-"`
	Attempts, Accepted, PrefixRejected, BudgetExhausted, Submits, Duplicates               int
	ESS                                                                                    float64
	FirstRejection                                                                         string
	Rejections                                                                             []RejectionBucket
	GuidedGenesis, GuidedLater, ArrangeWindows, UnguidedConstraints, IncompatibleProposals int
	WeightDiagnostics                                                                      WeightDiagnostics
	HandToStackCauses                                                                      HandToStackCauses
	StackRejectionContexts                                                                 []StackRejectionContext
	// CompetitionExclusions counts PolicyCompetition rejections whose competing
	// card was taught to the exclusion store (probe phases);
	// CompetitionResidual counts competition rejections during the frozen
	// sampling phase, which are counted but never taught (the store must stay
	// put so the phase's proposals share one distribution); CompetitionUnguided
	// counts hand_to_stack rejections no exclusion can express.
	CompetitionExclusions, CompetitionResidual, CompetitionUnguided int
	// LandExclusions counts declined-land-drop rejections taught to the
	// exclusion store (recordExtraLandExclusion); LandResidual those met while
	// the store was frozen; LandUnguided those the inference could not soundly
	// express.
	LandExclusions int `json:",omitempty"`
	LandResidual   int `json:",omitempty"`
	LandUnguided   int `json:",omitempty"`
	// BoardPotentialActionsOnly counts, under SampleOptions.
	// ComparePotentialActions only, the board rejections that vanish once the
	// seat's potential_actions projection is stripped from both sides -- a
	// world the replay rejected on nothing but that derived field. It is the
	// measurement that licenses skipping the projection during replay: a
	// non-zero count says the field decides acceptances and the skip would
	// change which worlds are accepted.
	BoardPotentialActionsOnly int `json:",omitempty"`
	// KnownCardClaims is the number of placement claims in the History's
	// known-card projection and KnownCardViolations the accepted replays that
	// contradicted one, both only under SampleOptions.KnownCards.
	KnownCardClaims     int `json:",omitempty"`
	KnownCardViolations int `json:",omitempty"`
	// Redealt is the number of worlds the redeal fallback supplied;
	// RedealRefused names why a starved sample could not be redealt. Both
	// only under SampleOptions.Redeal.
	Redealt       int    `json:",omitempty"`
	RedealRefused string `json:",omitempty"`
}
type RejectionBucket struct {
	Frame            int
	Component, Shape string
	Count            int
}

type HandToStackCauses struct {
	PolicyCompetition, ObserverReference, ObservedCastMissing, HypotheticalExtraCast, Other int
}

type StackRejectionContext struct {
	Cause, Step, ExpectedAction, Constraint string
	Count                                   int
}

// probeAttemptBudget is the total number of probe attempts Sample may spend
// learning exclusions: the historical 2x4 structure as a floor, raised to a
// quarter of the attempt budget so probing can continue past the first two
// rounds while each round still teaches something. Rounds are
// budget/probesPerRound; the frozen sampling phase always keeps the budget's
// last attempt regardless of what probing learned.
func probeAttemptBudget(attempts int) int {
	const probesPerRound, baseRounds = 4, 2
	cap := baseRounds * probesPerRound
	if c := attempts / 4; c > cap {
		cap = c
	}
	return cap
}

func Sample(setup PublicGame, h History, opts SampleOptions) (out SampleResult, err error) {
	defer func() {
		var f *Failure
		if err != nil && !errors.As(err, &f) {
			err = fail("invariant", "%v", err)
		}
	}()
	var result SampleResult
	defer func() {
		out = result
	}()
	if len(setup.Names) != len(setup.Decks) || len(setup.Names) < 2 || int(h.Actor) >= len(setup.Names) || len(h.Frames) == 0 || opts.Attempts < 1 || opts.Worlds < 1 || opts.MaxSubmits < 1 {
		return result, fmt.Errorf("invalid sampling configuration/history")
	}
	for _, deck := range setup.Decks {
		if len(deck) < 7 {
			return result, fail("unsupported", "undersized genesis deck")
		}
		for _, card := range deck {
			if card == nil || len(card.Faces) == 0 || card.Faces[0] == nil {
				return result, fmt.Errorf("invalid public card definition")
			}
		}
	}
	digest, err := historyDigest(h)
	if err != nil {
		return result, err
	}
	epochs, err := compileEpochs(h)
	if err != nil {
		return result, err
	}
	if err := validateGenesis(setup, epochs); err != nil {
		return result, err
	}
	// The replay compares against the observed frames with potential_actions
	// stripped, because captureScratch does not spend the walk that produces
	// it: each compared frame's board digest is its Stripped one. The digest
	// above was taken from the UNstripped history, so the sampler's seeds --
	// and every label they reach -- are untouched.
	compare := h.Frames
	if !opts.ComparePotentialActions {
		compare = make([]Frame, len(h.Frames))
		for i, frame := range h.Frames {
			compare[i] = frame
			compare[i].Board.Sum = frame.Board.Stripped
		}
	}
	stackConstraints := stackConstraintContexts(h, epochs)
	var known KnownCards
	if opts.KnownCards || opts.Redeal != nil {
		if known, err = ProjectKnownCards(h); err != nil {
			return result, err
		}
		result.KnownCardClaims = known.Count()
	}
	// starved ends a sample that could not supply Worlds worlds: empty, or
	// redealt when the fallback is on.
	starved := func() (SampleResult, error) {
		if opts.Redeal == nil {
			return result, nil
		}
		worlds, refused := redealWorlds(setup, h, known, opts.Redeal, opts.Worlds, func(i int) [2]uint64 {
			return taggedSeed(opts.Seed, digest, i, seedRedeal)
		})
		if refused != "" {
			result.RedealRefused = refused
			return result, nil
		}
		result.Redealt = len(worlds)
		result.Worlds = worlds
		return result, nil
	}
	tape, tossWeight, err := publicToss(setup, h)
	if err != nil {
		return result, err
	}
	var proposals []World
	var logs []float64
	plans := newConstraintPlanCache()
	store := newExclusionStore()
	drawStates := frameDrawStates(h)
	prefixEvents := observedPrefixEvents(h)
	// runAttempt replays the observed history once under one proposal. store
	// supplies the PolicyCompetition exclusions this attempt's plans honour;
	// staging collects the exclusions its rejections teach (nil: the store is
	// frozen and a competition rejection is only counted). The world is
	// returned uncommitted: the caller decides whether it joins the pool, and
	// the decision below keeps every kept world's proposal -- and therefore
	// its importance weight -- drawn from ONE frozen exclusion set, because a
	// pool that mixes proposals from different exclusion sets concentrates the
	// weights and collapses the ESS gate (measured: covered decisions fell
	// 185 -> 114 when exclusions accumulated attempt by attempt).
	// spare recycles a rejected attempt's engine arrays (its event log and
	// object arena, ~0.7 MB) into the next attempt's genesis: most attempts
	// are rejected, and a rejected world's engine is referenced by nothing
	// (frames hold owned values, never engine memory). Reuse is invisible to
	// the game (rules.Spare, TestSpareReuseIsInvisible). Like the plan cache,
	// a spare is owned by one goroutine: the probe rounds and frozen worker 0
	// share this one, every other frozen worker owns its own.
	var spare rules.Spare
	runAttempt := func(res *SampleResult, plans *constraintPlanCache, spare *rules.Spare, store *exclusionStore, staging *exclusionStore, attempt int) (_ World, _ float64, kept bool, _ error) {
		res.Attempts++
		seed := taggedSeed(opts.Seed, digest, attempt, seedEngine)
		cfg := rules.Config{Seed: seed[0], Names: setup.Names, Decks: setup.Decks, Tokens: setup.Tokens, StartingLife: setup.StartingLife}
		observer := NewCollector(h.Actor)
		proposal := &proposalState{epochs: epochs, logWeight: tossWeight, base: opts.Seed, history: digest, attempt: attempt, observer: observer, result: res, plans: plans, exclusions: store, staging: staging, noLandExclusion: opts.NoLandExclusion}
		// The Spare rides a private copy: a kept World's Config must not
		// alias the recycling slot.
		hcfg := cfg
		hcfg.Spare = spare
		e, err := rules.NewHypotheticalPlanned(hcfg, tape, proposal.plan)
		if e != nil {
			// Decisions this replay poses are read only before their answer
			// is submitted; a rejected engine's die at its Release, and a kept
			// world is never released here, so its arena stays valid.
			e.SetDecisionArena(true)
			defer func() {
				if !kept {
					*spare = e.Release()
				}
			}()
		}
		if errors.Is(err, errIncompatibleProposal) {
			return World{}, 0, false, nil
		}
		if err != nil {
			return World{}, 0, false, err
		}
		e.L.Reserve(prefixEvents)
		if err := e.AdvanceHypothetical(); err != nil {
			if errors.Is(err, errIncompatibleProposal) {
				return World{}, 0, false, nil
			}
			return World{}, 0, false, err
		}
		bots := make([]*rand.Rand, len(setup.Names))
		for i := range bots {
			seed := taggedSeed(opts.Seed, digest, attempt, seedOpponent, uint64(i))
			bots[i] = rand.New(rand.NewPCG(seed[0], seed[1]))
		}
		boards := newOpponentBoards(len(setup.Names))
		pos, submits := 0, 0
		accepted := true
		knownGot := make(map[uint32]Identity)
		knownWant := make(map[uint32]Identity)
		for i, want := range compare {
			got, err := observer.captureScratch(e, e.L.Events[pos:], opts.ComparePotentialActions)
			if err != nil {
				return World{}, 0, false, err
			}
			for _, identity := range got.Identities {
				knownGot[identity.ID] = identity
			}
			for _, identity := range want.Identities {
				knownWant[identity.ID] = identity
			}
			if !sameFrame(got, want) {
				res.PrefixRejected++
				bucket := rejectionBucket(i, got, want)
				addRejection(res, bucket)
				if bucket.Component == "identities" && bucket.Shape == "hand_to_stack" {
					cause := handToStackCause(got, want, knownGot, knownWant)
					res.HandToStackCauses.add(cause)
					addStackRejectionContext(res, stackRejectionContext(got, want, knownGot, knownWant, stackConstraints[i]))
					// Only a genuine competition is excludable: the replay cast a
					// DIFFERENT card from the observed one. An observer-reference
					// rejection names the observed card itself (excluding it would
					// contradict the deadline) and an extra/missing cast has no
					// competing preferred card to exclude.
					if cause.PolicyCompetition > 0 {
						proposal.recordCompetitionExclusion(i, got, knownGot, drawStates)
					} else {
						res.CompetitionUnguided++
					}
				}
				if opts.ComparePotentialActions && bucket.Component == "board" && got.Board.Stripped == want.Board.Stripped {
					res.BoardPotentialActionsOnly++
				}
				if bucket.Component == "identities" && bucket.Shape == "hand_to_battlefield" {
					proposal.recordExtraLandExclusion(i, got, want, knownGot, drawStates)
				}
				if res.FirstRejection == "" {
					res.FirstRejection = frameDifference(i, got, want)
				}
				accepted = false
				break
			}
			if i == len(compare)-1 {
				break
			}
			if submits >= opts.MaxSubmits {
				res.BudgetExhausted++
				accepted = false
				break
			}
			d := e.Pending()
			if d == nil {
				res.PrefixRejected++
				addRejection(res, RejectionBucket{Frame: i, Component: "decision", Shape: "missing"})
				if res.FirstRejection == "" {
					res.FirstRejection = fmt.Sprintf("frame %d missing pending decision", i)
				}
				accepted = false
				break
			}
			var in decision.Intent
			if d.Player == h.Actor {
				actions, ok := h.Answers[i]
				if !ok {
					return World{}, 0, false, fmt.Errorf("missing actor answer at frame %d", i)
				}
				in, err = observer.Match(d, actions)
				if err != nil {
					res.PrefixRejected++
					addRejection(res, RejectionBucket{Frame: i, Component: "action", Shape: "mismatch"})
					if res.FirstRejection == "" {
						res.FirstRejection = fmt.Sprintf("frame %d action mismatch: %v", i, err)
					}
					accepted = false
					break
				}
			} else {
				in = botpolicy.Decide(opponentBoard(e, d.Player, boards), d, bots[d.Player])
			}
			pos = len(e.L.Events)
			submits++
			res.Submits++
			if err := e.SubmitHypothetical(in); err != nil {
				if errors.Is(err, errIncompatibleProposal) {
					accepted = false
					break
				}
				return World{}, 0, false, fmt.Errorf("reconstruction submit: %w", err)
			}
		}
		if !accepted {
			return World{}, 0, false, nil
		}
		if opts.KnownCards {
			if err := known.holds(e, observer); err != nil {
				res.KnownCardViolations++
				addRejection(res, RejectionBucket{Frame: len(compare) - 1, Component: "known_cards", Shape: "violation"})
				if res.FirstRejection == "" {
					res.FirstRejection = fmt.Sprintf("frame %d known cards: %v", len(compare)-1, err)
				}
				return World{}, 0, false, nil
			}
		}
		e.ClearHypotheticalPlanner()
		return World{Config: cfg, Engine: e, Observer: observer}, proposal.logWeight, true, nil
	}
	// Probe rounds learn the competing names. Each round runs a few full
	// replays under the exclusions learned so far, collects what its
	// rejections teach into a staging store, and merges; the round is frozen
	// while it runs, so a round's attempts share one proposal. A round that
	// teaches nothing new ends the probing -- the last exclusion set is the
	// frozen one, and every remaining attempt samples under it.
	//
	// The probe budget is adaptive: baseRounds*probesPerRound (the historical
	// 2x4 structure) is the floor, raised to a quarter of the attempt budget
	// so that rounds may keep probing while each one still teaches something
	// -- the deeper preference layers the first rounds cannot observe. The
	// inner loop also never spends the budget's last attempt: the frozen
	// sampling phase always keeps at least one.
	const probesPerRound, baseRounds = 4, 2
	probeCap := probeAttemptBudget(opts.Attempts)
	probeRounds := probeCap / probesPerRound
	attemptsUsed := 0
	var probeWorlds []World
	var probeLogs []float64
	// keepFrom records where the final (teach-nothing) round's accepted probe
	// worlds begin. That round ran under the FINAL store -- the same
	// distribution as the frozen phase -- so its accepted worlds are
	// homogeneous with the frozen pool and are kept even when the store is
	// non-empty. Rounds that exited at the cap taught something; their
	// accepted worlds were proposed under an earlier, smaller store and keep
	// the old drop rule (keepFrom stays -1).
	keepFrom := -1
	for round := 0; round < probeRounds && attemptsUsed < probeCap; round++ {
		staging := newExclusionStore()
		before := store.size()
		roundStart := len(probeWorlds)
		for i := 0; i < probesPerRound && attemptsUsed < probeCap && opts.Attempts-attemptsUsed > 1; i++ {
			world, lw, accepted, err := runAttempt(&result, plans, &spare, store, staging, attemptsUsed)
			if err != nil {
				return result, err
			}
			attemptsUsed++
			if accepted {
				probeWorlds = append(probeWorlds, world)
				probeLogs = append(probeLogs, lw)
			}
		}
		store.merge(staging)
		if store.size() == before {
			keepFrom = roundStart
			break
		}
	}
	// The frozen phase. Every attempt is a pure function of (history, seed,
	// attempt index, frozen store): nothing it reads is written by another
	// attempt, so the attempts may run on several goroutines. Each writes its
	// own SampleResult and the results are folded in ATTEMPT ORDER below, which
	// makes the output byte-identical to the sequential loop whatever the
	// scheduling (pinned by the "Parallelism is wall clock only" check in
	// TestSampleRealDeckGolden and by TestTeacherChoiceLeafIndependentOfParallelism).
	type frozenOutcome struct {
		res      SampleResult
		world    World
		lw       float64
		accepted bool
		err      error
		panicked any
	}
	frozen := make([]frozenOutcome, opts.Attempts-attemptsUsed)
	// The plan cache memoises as it samples, so it is not shared between
	// goroutines: each worker owns one (worker 0 inherits the probe rounds').
	// A cache only saves rebuilding a plan; a plan is a pure function of its
	// constraints, so which cache served an attempt cannot change its draw.
	runFrozen := func(i int, plans *constraintPlanCache, sp *rules.Spare) {
		o := &frozen[i]
		defer func() {
			if p := recover(); p != nil {
				o.panicked = p
			}
		}()
		o.world, o.lw, o.accepted, o.err = runAttempt(&o.res, plans, sp, store, nil, attemptsUsed+i)
	}
	if workers := min(opts.Parallelism, len(frozen)); workers > 1 {
		var wg sync.WaitGroup
		var next atomic.Int64
		for w := 0; w < workers; w++ {
			workerPlans, workerSpare := plans, &spare
			if w > 0 {
				workerPlans, workerSpare = newConstraintPlanCache(), new(rules.Spare)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := int(next.Add(1)) - 1
					if i >= len(frozen) {
						return
					}
					runFrozen(i, workerPlans, workerSpare)
				}
			}()
		}
		wg.Wait()
	} else {
		for i := range frozen {
			runFrozen(i, plans, &spare)
			if frozen[i].err != nil || frozen[i].panicked != nil {
				break
			}
		}
	}
	for i := range frozen {
		o := &frozen[i]
		if o.panicked != nil {
			// Surface a worker's panic on the caller's goroutine, at the
			// attempt the sequential loop would have met it.
			panic(o.panicked)
		}
		result.mergeAttempt(o.res)
		if o.err != nil {
			return result, o.err
		}
		if o.accepted {
			result.Accepted++
			proposals = append(proposals, o.world)
			logs = append(logs, o.lw)
		}
	}
	if keepFrom >= 0 {
		for i := keepFrom; i < len(probeWorlds); i++ {
			result.Accepted++
			proposals = append(proposals, probeWorlds[i])
			logs = append(logs, probeLogs[i])
		}
	}
	if len(proposals) == 0 {
		return starved()
	}
	weights, ess, err := normalizeWeights(logs)
	if err != nil {
		return result, err
	}
	result.ESS = ess
	result.WeightDiagnostics = summarizeWeightDiagnostics(logs, weights)
	minESS := float64(opts.Worlds)
	if opts.MinESS > 0 {
		minESS = opts.MinESS
	} else if len(proposals) < opts.Worlds {
		return starved()
	}
	if ess+1e-10 < minESS {
		return starved()
	}
	seed := taggedSeed(opts.Seed, digest, 0, seedResampling)
	r := rand.New(rand.NewPCG(seed[0], seed[1]))
	indices, err := weightedIndices(weights, opts.Worlds, r)
	if err != nil {
		return result, err
	}
	seen := make(map[int]bool)
	for _, i := range indices {
		if seen[i] {
			result.Duplicates++
		}
		seen[i] = true
		w := proposals[i]
		w.Engine = w.Engine.Clone()
		w.Observer = w.Observer.clone()
		result.Worlds = append(result.Worlds, w)
	}
	return result, nil
}

func observedPrefixEvents(h History) int {
	total := 0
	for _, frame := range h.Frames {
		total += len(frame.Events)
	}
	return total
}

// frameDrawStates is the public draw ledger the exclusion derivation reads:
// for every observed frame and seat, which shuffle that seat is drawing from
// (its ordinal), how many cards it has drawn from the current shuffle, and
// how many cards each already-COMPLETED shuffle yielded (completed[e] is the
// total draws epoch e ever produced). Every input is an observed event
// (Shuffle and Draw carry the seat), so the ledger is available to the
// sampler without reading any hidden zone.
type frameDrawState struct {
	ordinal   int
	draws     int
	completed []int
}

func frameDrawStates(h History) []map[state.PlayerID]frameDrawState {
	out := make([]map[state.PlayerID]frameDrawState, len(h.Frames))
	current := make(map[state.PlayerID]frameDrawState)
	for i, frame := range h.Frames {
		for _, ev := range frame.Events {
			s := current[ev.Player]
			switch ev.Kind {
			case events.Shuffle:
				if s.ordinal > 0 {
					// The shuffle closes epoch s.ordinal-1: everything drawn
					// from it is now a finished, known count.
					s.completed = append(s.completed, s.draws)
				}
				s.ordinal++
				s.draws = 0
			case events.Draw:
				s.draws++
			}
			current[ev.Player] = s
		}
		snapshot := make(map[state.PlayerID]frameDrawState, len(current))
		for player, s := range current {
			snapshot[player] = s
		}
		out[i] = snapshot
	}
	return out
}

// recordCompetitionExclusion turns a PolicyCompetition rejection into
// constraints for the attempts that follow. The card the replay bot cast
// INSTEAD of the observed one must not reach that seat's hand before the
// observed cast, so it is excluded from the cast's own shuffle up to the
// observed draw count and from every EARLIER shuffle across that shuffle's
// whole drawn window: a card drawn in a finished shuffle was in hand at the
// cast point and the replay would deterministically have preferred it there,
// which is exactly the divergence being repaired. Epochs whose compiled
// constraints already place the named card in hand by an observation (a
// deadline -- the seat was observed casting or publicly discarding it in that
// shuffle) are skipped: excluding there would contradict the history, not
// guide it. The competing card is the bot's own choice at the diverging
// decision (it is the card the replay preferred), so the policy's ranking is
// read from the engine that produced it rather than re-derived.
//
// Shapes that fall back to today's behaviour and are counted: a rejection
// whose owner has not observed a shuffle, one whose competing card has no
// observable name, one owned by the actor seat (the actor's frames replay
// exactly, so no hand-completion choice is being repaired), and one learned
// while the store is frozen (residual -- counted, never taught). So does a
// plan whose exclusion turns out to be infeasible, which the counter reports
// as an incompatible proposal.
func (p *proposalState) recordCompetitionExclusion(frameIndex int, got Frame, known map[uint32]Identity, drawStates []map[state.PlayerID]frameDrawState) {
	if p.staging == nil {
		p.result.CompetitionResidual++
		return
	}
	name, owner, ok := competingCast(got, known)
	if !ok || owner == p.observer.actor {
		p.result.CompetitionUnguided++
		return
	}
	if !p.teachExclusion(frameIndex, name, owner, drawStates) {
		p.result.CompetitionUnguided++
		return
	}
	p.result.CompetitionExclusions++
}

// teachExclusion stages "name must not be in owner's hand at frameIndex" as
// shuffle constraints: excluded from the current shuffle up to the observed
// draw count and from every finished shuffle across its whole drawn window,
// each capped at the copies that shuffle's deadlines already require. It
// reports false when the seat has not observed a shuffle.
func (p *proposalState) teachExclusion(frameIndex int, name string, owner state.PlayerID, drawStates []map[state.PlayerID]frameDrawState) bool {
	state, ok := drawStates[frameIndex][owner]
	if !ok || state.ordinal <= 0 {
		return false
	}
	for ordinal := 0; ordinal < state.ordinal-1; ordinal++ {
		if ordinal >= len(state.completed) {
			break
		}
		key := epochKey{Player: owner, Ordinal: ordinal}
		ep := p.epochs[key]
		p.staging.add(key, exclusionConstraint{Through: state.completed[ordinal], Name: name, Cap: epochDeadlineCountFor(ep, name)})
	}
	ep := p.epochs[epochKey{Player: owner, Ordinal: state.ordinal - 1}]
	p.staging.add(epochKey{Player: owner, Ordinal: state.ordinal - 1}, exclusionConstraint{Through: state.draws, Name: name, Cap: epochDeadlineCountFor(ep, name)})
	return true
}

// recordExtraLandExclusion teaches the NEGATIVE observation a declined land
// drop carries. The replay's opponent played a land in a frame where the
// observed opponent made no hand-to-battlefield move at all. The public state
// up to that frame is identical in every world that gets this far (the prefix
// matched), so the land drop the replay took was open to the observed seat
// too -- and the replay policy never passes main-phase priority while a
// play_land option is offered (botpolicy decide: chooseTap, then chooseLand,
// both before any pass). Every world that holds a spare copy of that land at
// this frame is therefore rejected with probability one, and removing those
// worlds from the proposal leaves the accepted-world distribution exactly
// what it was: the importance weight of each surviving world is computed
// under the constrained plan, the same way a cast exclusion's is.
//
// "Spare" is what Cap expresses: the copies the shuffle's deadlines already
// place in hand (observed plays of the same land) stay allowed. A Cap that
// counts a play observed AFTER this frame only loosens the constraint, which
// costs acceptance, never correctness.
//
// It fails closed -- counted, never taught -- when the inference is not
// airtight: the frozen phase, a frame where the observed seat DID move a card
// from hand to the battlefield (a different-land competition depends on
// chooseLand's hand-dependent ranking, not on a fixed preference), a replay
// move that was not a land play, the actor's own seat, and any shuffle of
// that seat carrying an unguided library mutation (a hidden hand-to-library
// channel such as Brainstorm could have put the land back, so "drawn" would
// no longer imply "in hand").
func (p *proposalState) recordExtraLandExclusion(frameIndex int, got, want Frame, known map[uint32]Identity, drawStates []map[state.PlayerID]frameDrawState) {
	if p.noLandExclusion {
		return
	}
	if p.staging == nil {
		p.result.LandResidual++
		return
	}
	for _, event := range want.Events {
		if event.From == state.ZHand && event.To == state.ZBattlefield {
			p.result.LandUnguided++
			return
		}
	}
	landPlayed := false
	var obj uint32
	for _, event := range got.Events {
		if event.Kind == events.LandPlayed {
			landPlayed = true
		}
		if obj == 0 && event.From == state.ZHand && event.To == state.ZBattlefield {
			obj = event.Obj
		}
	}
	identity, ok := known[obj]
	if !landPlayed || !ok || identity.Name == "" || identity.Owner == p.observer.actor {
		p.result.LandUnguided++
		return
	}
	for key, ep := range p.epochs {
		if key.Player == identity.Owner && len(ep.Unguided) > 0 {
			p.result.LandUnguided++
			return
		}
	}
	if !p.teachExclusion(frameIndex, identity.Name, identity.Owner, drawStates) {
		p.result.LandUnguided++
		return
	}
	p.result.LandExclusions++
}

// epochDeadlineCountFor is the number of copies of the named card the epoch's
// compiled deadlines already require in hand by an observation -- the seat was
// seen casting, discarding or otherwise publicly exiting that card in this
// shuffle. Zero when the card was never observed exiting hand there. An
// exclusion for such an epoch carries this count as its Cap instead of 0, so
// it means "exactly the observed copies, no further draws of the card" rather
// than contradicting the deadline outright.
func epochDeadlineCountFor(ep epochConstraints, name string) int {
	count := 0
	for _, deadline := range ep.Deadlines {
		if deadline.Name == name && deadline.Count > count {
			count = deadline.Count
		}
	}
	return count
}

// competingCast is the observed frame's hand-to-stack card: its name and the
// seat that cast it. It fails closed when the frame holds no such move or the
// identity was never observed, so an unnameable rejection is counted, never
// guessed.
func competingCast(frame Frame, known map[uint32]Identity) (string, state.PlayerID, bool) {
	for _, event := range frame.Events {
		if event.From != state.ZHand || event.To != state.ZStack {
			continue
		}
		identity, ok := known[event.Obj]
		if !ok || identity.Name == "" {
			return "", 0, false
		}
		return identity.Name, identity.Owner, true
	}
	return "", 0, false
}

func newOpponentBoards(players int) []botpolicy.Board {
	boards := make([]botpolicy.Board, players)
	for i := range boards {
		boards[i] = botpolicy.NewBoard(players)
	}
	return boards
}

func opponentBoard(e *rules.Engine, player state.PlayerID, boards []botpolicy.Board) botpolicy.Board {
	return botpolicy.BoardFromGameInto(e.G, e, player, &boards[player])
}

func (c *HandToStackCauses) add(other HandToStackCauses) {
	c.PolicyCompetition += other.PolicyCompetition
	c.ObserverReference += other.ObserverReference
	c.ObservedCastMissing += other.ObservedCastMissing
	c.HypotheticalExtraCast += other.HypotheticalExtraCast
	c.Other += other.Other
}

func handToStackCause(got, want Frame, knownGot, knownWant map[uint32]Identity) HandToStackCauses {
	for _, identity := range got.Identities {
		knownGot[identity.ID] = identity
	}
	for _, identity := range want.Identities {
		knownWant[identity.ID] = identity
	}
	stackName := func(frame Frame, known map[uint32]Identity) (string, bool) {
		for _, event := range frame.Events {
			if event.From == state.ZHand && event.To == state.ZStack {
				identity, ok := known[event.Obj]
				if !ok || identity.Name == "" {
					return "", false
				}
				return identity.Name, true
			}
		}
		return "", false
	}
	gotName, gotOK := stackName(got, knownGot)
	wantName, wantOK := stackName(want, knownWant)
	if gotOK && wantOK {
		if gotName == wantName {
			return HandToStackCauses{ObserverReference: 1}
		}
		return HandToStackCauses{PolicyCompetition: 1}
	}
	if wantOK && !gotOK {
		return HandToStackCauses{ObservedCastMissing: 1}
	}
	if gotOK && !wantOK {
		return HandToStackCauses{HypotheticalExtraCast: 1}
	}
	return HandToStackCauses{Other: 1}
}

func stackRejectionContext(got, want Frame, knownGot, knownWant map[uint32]Identity, constraint string) StackRejectionContext {
	cause := handToStackCause(got, want, knownGot, knownWant)
	causeName := "other"
	switch {
	case cause.PolicyCompetition > 0:
		causeName = "policy_competition"
	case cause.ObserverReference > 0:
		causeName = "observer_reference"
	case cause.ObservedCastMissing > 0:
		causeName = "observed_cast_missing"
	case cause.HypotheticalExtraCast > 0:
		causeName = "hypothetical_extra_cast"
	}
	if constraint == "" {
		if cause.HypotheticalExtraCast > 0 {
			constraint = "no_observed_cast"
		} else {
			constraint = "unknown"
		}
	}
	step := "unknown"
	if want.Board.Step != "" {
		step = want.Board.Step
	}
	return StackRejectionContext{Cause: causeName, Step: step, ExpectedAction: frameActionShape(want), Constraint: constraint, Count: 1}
}

func frameActionShape(frame Frame) string {
	for _, event := range frame.Events {
		if event.From == state.ZHand && event.To == state.ZStack {
			return "cast"
		}
	}
	for _, event := range frame.Events {
		switch event.Kind {
		case events.LandPlayed:
			return "land_play"
		case events.AbilityPush:
			return "ability"
		case events.DeclareAttackers, events.DeclareBlockers:
			return "combat"
		case events.Priority:
			return "pass"
		case events.Resolve:
			return "resolve"
		}
	}
	return "other"
}

func stackConstraintContexts(h History, epochs map[epochKey]epochConstraints) []string {
	out := make([]string, len(h.Frames))
	ordinals := make(map[state.PlayerID]int)
	names := make(map[uint32]Identity)
	for frameIndex, frame := range h.Frames {
		for _, identity := range frame.Identities {
			names[identity.ID] = identity
		}
		for _, event := range frame.Events {
			if event.Kind == events.Shuffle {
				ordinals[event.Player]++
				continue
			}
			if event.From != state.ZHand || event.To != state.ZStack {
				continue
			}
			identity, ok := names[event.Obj]
			if !ok || identity.Name == "" || ordinals[identity.Owner] == 0 {
				out[frameIndex] = "unknown"
				break
			}
			if identity.Owner == h.Actor {
				out[frameIndex] = "actor"
				break
			}
			epoch := epochs[epochKey{Player: identity.Owner, Ordinal: ordinals[identity.Owner] - 1}]
			out[frameIndex] = "unconstrained"
			for _, deadline := range epoch.Deadlines {
				if deadline.Name == identity.Name {
					out[frameIndex] = "supported"
					break
				}
			}
			if out[frameIndex] == "unconstrained" && len(epoch.Unguided) > 0 {
				out[frameIndex] = "unguided"
			}
			break
		}
	}
	return out
}

func addStackRejectionContext(result *SampleResult, bucket StackRejectionContext) {
	for i := range result.StackRejectionContexts {
		got := &result.StackRejectionContexts[i]
		if got.Cause == bucket.Cause && got.Step == bucket.Step && got.ExpectedAction == bucket.ExpectedAction && got.Constraint == bucket.Constraint {
			got.Count++
			return
		}
	}
	result.StackRejectionContexts = append(result.StackRejectionContexts, bucket)
	sort.Slice(result.StackRejectionContexts, func(i, j int) bool {
		a, b := result.StackRejectionContexts[i], result.StackRejectionContexts[j]
		if a.Cause != b.Cause {
			return a.Cause < b.Cause
		}
		if a.Step != b.Step {
			return a.Step < b.Step
		}
		if a.ExpectedAction != b.ExpectedAction {
			return a.ExpectedAction < b.ExpectedAction
		}
		return a.Constraint < b.Constraint
	})
}

func addRejection(result *SampleResult, bucket RejectionBucket) {
	for i := range result.Rejections {
		got := &result.Rejections[i]
		if got.Frame == bucket.Frame && got.Component == bucket.Component && got.Shape == bucket.Shape {
			got.Count++
			return
		}
	}
	bucket.Count = 1
	result.Rejections = append(result.Rejections, bucket)
	sort.Slice(result.Rejections, func(i, j int) bool {
		a, b := result.Rejections[i], result.Rejections[j]
		if a.Frame != b.Frame {
			return a.Frame < b.Frame
		}
		if a.Component != b.Component {
			return a.Component < b.Component
		}
		return a.Shape < b.Shape
	})
}

// sameFrame is the replay's frame equality: reflect.DeepEqual over the
// identities, events and decision, and the board by its Sum digest (the
// caller puts the digest to compare there; see Sample's compare frames).
// The history link is bookkeeping, not observation, and is not compared.
func sameFrame(got, want Frame) bool {
	return got.Board.Sum == want.Board.Sum && reflect.DeepEqual(got.Identities, want.Identities) && reflect.DeepEqual(got.Events, want.Events) && reflect.DeepEqual(got.Decision, want.Decision)
}

func rejectionBucket(frame int, got, want Frame) RejectionBucket {
	if !reflect.DeepEqual(got.Identities, want.Identities) {
		return RejectionBucket{Frame: frame, Component: "identities", Shape: identityDifferenceShape(got, want)}
	}
	if !reflect.DeepEqual(got.Events, want.Events) {
		limit := len(got.Events)
		if len(want.Events) < limit {
			limit = len(want.Events)
		}
		for i := 0; i < limit; i++ {
			if !reflect.DeepEqual(got.Events[i], want.Events[i]) {
				return RejectionBucket{Frame: frame, Component: "events", Shape: got.Events[i].Kind.String() + "_to_" + want.Events[i].Kind.String()}
			}
		}
		return RejectionBucket{Frame: frame, Component: "events", Shape: "count"}
	}
	if got.Board.Sum != want.Board.Sum {
		return RejectionBucket{Frame: frame, Component: "board", Shape: "state"}
	}
	return RejectionBucket{Frame: frame, Component: "decision", Shape: "fields"}
}

func identityDifferenceShape(got, want Frame) string {
	declarations := [2]map[uint32]Identity{make(map[uint32]Identity), make(map[uint32]Identity)}
	frames := []Frame{want, got}
	for i, f := range frames {
		for _, id := range f.Identities {
			declarations[i][id.ID] = id
		}
	}
	for i, f := range frames {
		for _, identity := range f.Identities {
			if other, exists := declarations[1-i][identity.ID]; exists && other == identity {
				continue
			}
			for _, ev := range f.Events {
				if ev.Obj != identity.ID || ev.From != state.ZHand {
					continue
				}
				switch ev.To {
				case state.ZBattlefield:
					return "hand_to_battlefield"
				case state.ZStack:
					return "hand_to_stack"
				case state.ZExile:
					return "hand_to_exile"
				default:
					return "hand_to_other"
				}
			}
		}
	}
	return "other"
}

func frameDifference(i int, got, want Frame) string {
	part := "decision"
	if !reflect.DeepEqual(got.Identities, want.Identities) {
		part = "identities"
	} else if !reflect.DeepEqual(got.Events, want.Events) {
		part = "events"
	} else if got.Board.Sum != want.Board.Sum {
		part = "board"
	}
	return fmt.Sprintf("frame %d %s", i, part)
}

// mergeAttempt folds one attempt's private counters into r. Folding attempts
// in attempt order reproduces exactly what the attempts would have written to
// one shared result in sequence: the counters add, FirstRejection keeps the
// earliest, and the two bucket lists are keyed sets kept sorted by key.
func (r *SampleResult) mergeAttempt(a SampleResult) {
	r.Attempts += a.Attempts
	r.PrefixRejected += a.PrefixRejected
	r.BudgetExhausted += a.BudgetExhausted
	r.Submits += a.Submits
	if r.FirstRejection == "" {
		r.FirstRejection = a.FirstRejection
	}
	for _, b := range a.Rejections {
		for n := 0; n < b.Count; n++ {
			addRejection(r, b)
		}
	}
	r.GuidedGenesis += a.GuidedGenesis
	r.GuidedLater += a.GuidedLater
	r.ArrangeWindows += a.ArrangeWindows
	r.UnguidedConstraints += a.UnguidedConstraints
	r.IncompatibleProposals += a.IncompatibleProposals
	r.HandToStackCauses.add(a.HandToStackCauses)
	for _, c := range a.StackRejectionContexts {
		for n := 0; n < c.Count; n++ {
			one := c
			one.Count = 1
			addStackRejectionContext(r, one)
		}
	}
	r.CompetitionExclusions += a.CompetitionExclusions
	r.CompetitionResidual += a.CompetitionResidual
	r.CompetitionUnguided += a.CompetitionUnguided
	r.LandExclusions += a.LandExclusions
	r.LandResidual += a.LandResidual
	r.LandUnguided += a.LandUnguided
	r.BoardPotentialActionsOnly += a.BoardPotentialActionsOnly
	r.KnownCardViolations += a.KnownCardViolations
}
