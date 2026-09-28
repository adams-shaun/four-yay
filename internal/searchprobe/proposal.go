package searchprobe

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"math/rand/v2"
	"strings"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

const (
	seedEngine uint64 = iota + 1
	seedProposal
	seedOpponent
	seedResampling
	seedRedeal
)

// Each role owns a stream; neither call order nor worker allocation enters it.
func taggedSeed(base uint64, history [32]byte, attempt int, tag uint64, values ...uint64) [2]uint64 {
	h := sha256.New()
	h.Write(history[:])
	var b [8]byte
	for _, v := range append([]uint64{base, uint64(attempt), tag}, values...) {
		binary.LittleEndian.PutUint64(b[:], v)
		h.Write(b[:])
	}
	sum := h.Sum(nil)
	return [2]uint64{binary.LittleEndian.Uint64(sum[:8]), binary.LittleEndian.Uint64(sum[8:16])}
}

var errIncompatibleProposal = errors.New("incompatible hypothetical proposal")

// feasibleExclusions translates each stored exclusion into the strongest
// form the shuffle that will carry it can actually satisfy. A Cap derived
// from a same-epoch deadline (exactly the observed copies) is first reduced
// by the copies already in the pre-shuffle hand -- the same reduction the
// deadline itself gets, so the two constraints stay consistent. Then a
// window whose copies cannot all fit beyond it relaxes to exactly the
// overflow (cap = copies beyond the tail's room): "no copy before Through"
// is infeasible there (a 20-copy land excluded through position 40 of a
// 53-card library), and a hard constraint would turn every attempt into an
// incompatible proposal rather than a redraw. The relaxation keeps the
// guarantee where it is affordable (a single-copy spell, the
// PolicyCompetition shape the brief targets) and degrades a multi-copy
// card's exclusion instead of wedging the counter. Copies already fixed into
// the early positions are still counted by the constraint matcher, which is
// why the relaxation compares against the whole deck slice, not the free
// pool.
func feasibleExclusions(exclusions []exclusionConstraint, cards []proposalCard, hand map[string]int) []exclusionConstraint {
	if len(exclusions) == 0 {
		return nil
	}
	copies := make(map[string]int)
	for _, card := range cards {
		copies[card.Name]++
	}
	out := make([]exclusionConstraint, 0, len(exclusions))
	for _, e := range exclusions {
		cap := e.Cap
		if cap > 0 {
			cap = max(0, cap-hand[e.Name])
		}
		through := e.Through
		if through > len(cards) {
			through = len(cards)
		}
		room := len(cards) - through
		if overflow := copies[e.Name] - room; overflow > cap {
			cap = overflow
		}
		out = append(out, exclusionConstraint{Through: through, Name: e.Name, Cap: cap})
	}
	return out
}

// exclusionStore carries PolicyCompetition exclusions learned by the probe
// attempts into the plans of the attempts that follow. Each rejection names
// one physical card the replay bot preferred over the observed cast; the
// following plans must not let that card reach the offending seat's hand
// before the observed cast, so the card is forbidden from the offending
// epoch's early shuffle positions and from every earlier shuffle entirely
// (recordCompetitionExclusion). The store is frozen before the sampling
// phase: every sampling attempt then shares one proposal, so one kept world's
// importance weight is drawn from the same distribution as the next's and the
// ESS gate sees homogeneous weights. It is attempt-ordered and deterministic
// -- Sample drives attempts sequentially and the seed stream already carries
// the attempt index -- so worker allocation and call order cannot change
// which exclusions exist when a plan runs.
type exclusionStore struct {
	byEpoch map[epochKey][]exclusionConstraint
}

func newExclusionStore() *exclusionStore {
	return &exclusionStore{byEpoch: make(map[epochKey][]exclusionConstraint)}
}

// add records one exclusion. Duplicates (the same card excluded to the same
// depth) collapse so a repeatedly rejected card does not accumulate unbounded
// constraints; a deeper Through tightens an existing entry.
func (s *exclusionStore) add(key epochKey, e exclusionConstraint) {
	if s.byEpoch == nil {
		s.byEpoch = make(map[epochKey][]exclusionConstraint)
	}
	list := s.byEpoch[key]
	for i := range list {
		if list[i].Name != e.Name {
			continue
		}
		if e.Through > list[i].Through {
			list[i].Through = e.Through
		}
		if e.Cap < list[i].Cap {
			list[i].Cap = e.Cap
		}
		return
	}
	s.byEpoch[key] = append(list, e)
}

// size counts the stored constraints; zero means nothing was learned and the
// sampling phase's proposal equals the probe rounds' (deadlines only).
func (s *exclusionStore) size() int {
	if s == nil {
		return 0
	}
	total := 0
	for _, list := range s.byEpoch {
		total += len(list)
	}
	return total
}

// merge folds a probe round's learned exclusions into the store. add already
// deduplicates by (epoch, name), so re-learning the same competing card is a
// no-op and a deeper Through / tighter Cap only tightens.
func (s *exclusionStore) merge(other *exclusionStore) {
	if other == nil {
		return
	}
	for key, list := range other.byEpoch {
		for _, e := range list {
			s.add(key, e)
		}
	}
}

// forEpoch returns the exclusions that apply when key's library is planned. The
// slice is owned by the caller's plan cache (getWithExclusions copies it) and
// must stay nil when nothing was excluded, so an unaffected game's constraint
// problem -- and its weight -- is byte-identical to the pre-fix sampler.
func (s *exclusionStore) forEpoch(player state.PlayerID, ordinal int) []exclusionConstraint {
	if s == nil {
		return nil
	}
	return s.byEpoch[epochKey{Player: player, Ordinal: ordinal}]
}

type proposalState struct {
	epochs     map[epochKey]epochConstraints
	logWeight  float64
	base       uint64
	history    [32]byte
	attempt    int
	observer   *Collector
	result     *SampleResult
	plans      *constraintPlanCache
	exclusions *exclusionStore
	// staging collects this attempt's learned exclusions. Nil means the store
	// is frozen (the sampling phase): a competition rejection is then counted
	// as residual instead of taught.
	staging *exclusionStore
	// noLandExclusion mirrors SampleOptions.NoLandExclusion.
	noLandExclusion bool
}

func publicToss(setup PublicGame, h History) ([]rules.ChanceDraw, float64, error) {
	for _, f := range h.Frames {
		for _, ev := range f.Events {
			if ev.Kind == events.Note && strings.HasSuffix(ev.Text, " won the toss") {
				if int(ev.Player) >= len(setup.Names) {
					return nil, 0, fail("contradictory", "invalid public toss")
				}
				return []rules.ChanceDraw{{Bound: len(setup.Names), Value: int(ev.Player)}}, -math.Log(float64(len(setup.Names))), nil
			}
		}
	}
	return nil, 0, fail("invariant", "missing public genesis toss")
}

// Before there is a hypothetical past, impossible genesis requirements are
// contradictions in the public input, not unlucky proposals.
func validateGenesis(setup PublicGame, epochs map[epochKey]epochConstraints) error {
	for player, deck := range setup.Decks {
		ep := epochs[epochKey{Player: state.PlayerID(player)}]
		cards := make([]proposalCard, len(deck))
		for i, card := range deck {
			cards[i] = proposalCard{ID: state.ObjID(i + 1), Name: card.Faces[0].Name}
		}
		positions := make([]positionConstraint, len(ep.Positions))
		for i, p := range ep.Positions {
			positions[i] = positionConstraint{Index: p.Index, Name: p.Name}
		}
		counter, available, err := newConstraintCounter(cards, positions, ep.Deadlines)
		if err != nil {
			return fail("contradictory", "invalid genesis constraints: %v", err)
		}
		if counter.total(available).Sign() == 0 {
			return fail("contradictory", "genesis constraints have no public-deck completion for player %d", player)
		}
	}
	return nil
}

func (p *proposalState) plan(ctx rules.ShuffleContext) ([]state.ObjID, error) {
	ep := p.epochs[epochKey{Player: ctx.Player, Ordinal: ctx.Ordinal}]
	p.result.UnguidedConstraints += len(ep.Unguided)
	exclusions := p.exclusions.forEpoch(ctx.Player, ctx.Ordinal)
	if len(ep.Positions) == 0 && len(ep.Deadlines) == 0 && len(exclusions) == 0 {
		return nil, nil
	}
	cards := make([]proposalCard, len(ctx.Library))
	byID := make(map[state.ObjID]string, len(ctx.Library))
	for i, card := range ctx.Library {
		// Known is judged on this attempt's own observer at shuffle time: a
		// card it already introduced is a known duplicate, one it has not seen
		// yet can still serve as a first introduction in the replay.
		cards[i] = proposalCard{ID: card.ID, Name: card.Name, Known: p.observer.known[card.ID] != 0}
		byID[card.ID] = card.Name
	}
	positions := make([]positionConstraint, len(ep.Positions))
	used := make(map[state.ObjID]bool)
	for i, pos := range ep.Positions {
		positions[i] = positionConstraint{Index: pos.Index, Name: pos.Name, Unseen: pos.Unseen}
		// Only this attempt's prefix-aligned observer can translate a reference.
		// Future introductions have no mapping yet and remain name constraints.
		positions[i].Obj = p.observer.object(pos.Ref)
		obj := positions[i].Obj
		if pos.Index >= len(cards) || obj != 0 && (byID[obj] != pos.Name || used[obj]) {
			return p.incompatible()
		}
		if obj != 0 {
			used[obj] = true
		}
	}
	// Requirements are cumulative by name, so each deadline subtracts the same
	// pre-shuffle hand count, not a greedily consumed sequence of copies.
	handCounts := make(map[string]int)
	if ctx.Player != p.observer.actor {
		for _, card := range ctx.Hand {
			handCounts[card.Name]++
		}
	}
	deadlines := append([]deadlineConstraint(nil), ep.Deadlines...)
	for i := range deadlines {
		deadlines[i].Count = max(0, deadlines[i].Count-handCounts[deadlines[i].Name])
		if deadlines[i].Through > len(cards) {
			return p.incompatible()
		}
	}
	seed := taggedSeed(p.base, p.history, p.attempt, seedProposal, uint64(ctx.Player), uint64(ctx.Ordinal))
	if p.plans == nil {
		p.plans = newConstraintPlanCache()
	}
	exclusions = feasibleExclusions(exclusions, cards, handCounts)
	plan, err := p.plans.getWithExclusions(cards, positions, deadlines, exclusions)
	if err != nil {
		return nil, err
	}
	order, factor, compatible, err := plan.sample(rand.New(rand.NewPCG(seed[0], seed[1])))
	if err != nil {
		return nil, err
	}
	if !compatible {
		return p.incompatible()
	}
	p.logWeight += factor
	if ctx.Ordinal == 0 {
		p.result.GuidedGenesis++
	} else {
		p.result.GuidedLater++
	}
	p.result.ArrangeWindows += ep.ArrangeWindows
	return order, nil
}

func (p *proposalState) incompatible() ([]state.ObjID, error) {
	p.result.IncompatibleProposals++
	return nil, errIncompatibleProposal
}
