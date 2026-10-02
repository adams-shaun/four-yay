package mzplay

import (
	"math"

	"github.com/adams-shaun/gorge/internal/mzbridge"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// SentinelID is a feature id carried by EVERY state this package records and
// every state it sends for evaluation.
//
// Why. MageZero's inference server (server.py, pinned 521a8bd) drops the ids
// its model's vocabulary does not hold, and when every bag of a batch is
// empty after that its only worker thread dies: every later /evaluate then
// hangs while /healthz keeps answering ok. The vocabulary is built by
// train.py from the training window (vocab.kept_feature_ids): an id is kept
// when it is active in more than 10 states, and of the ids active in exactly
// the same set of states only the SMALLEST is kept. An id present in every
// recorded state is active in all of them; the smallest such id is 0, the
// sentinel; so any model trained on more than 10 of this package's states
// holds id 0, and every bag this package sends holds id 0. No bag can then
// be empty for the server, whatever the state.
//
// Cost: one constant feature. It replaces, as the kept representative, the
// other always-on ids (the root's Stack/Exile/Player/Opponent nodes), which
// the same rule would have collapsed to one anyway; the network sees the
// same information. Upstream's own data has no such id (a real feature
// hashes to 0 with probability 2^-31).
//
// A model trained on 10 states or fewer (a degenerate smoke run) has an
// empty vocabulary and still kills its server; NetLeaf then falls back to
// the offline evaluator (see Dead).
const SentinelID int32 = 0

// withSentinel prepends the sentinel to an ascending, de-duplicated id list.
func withSentinel(ids []int32) []int32 {
	if len(ids) > 0 && ids[0] == SentinelID {
		return ids
	}
	out := make([]int32, 0, len(ids)+1)
	out = append(out, SentinelID)
	return append(out, ids...)
}

// Evaluator is the inference server as the leaf needs it: one state's ids
// in, the network's output out. The command wraps mzclient.Client (and its
// request timeout) behind it; this package imports no network client.
type Evaluator interface {
	Evaluate(ids []int32) (mzbridge.Evaluation, error)
}

// maxNetFailures is how many evaluations in a row may fail before the
// server is taken for dead and no longer asked.
const maxNetFailures = 3

// NetLeaf is the network leaf of one seat of one game: it encodes the leaf
// position as that seat's encoder would (the seat is "Player"; a hand is
// encoded only for the player deciding there unless SeeOpponentHand), sends
// it to the seat's inference server and maps the value head, which is the
// encoded "Player"'s value in [-1, 1] (MCTSNode2.evaluate stores out.value
// as the searching player's score; the trainer's target is the recording
// seat's result), onto the searching seat's [0, 1].
//
// Not safe for concurrent use: one per seat per game.
type NetLeaf struct {
	Eval Evaluator
	// SeeOpponentHand is game.yml's hiddenInfo.see_opponent_hand.
	SeeOpponentHand bool

	enc *mzbridge.Encoder
	fs  *mzbridge.FeatureSet

	// Calls counts leaf evaluations asked for; Fallbacks the ones answered
	// by the offline evaluator instead (an encode error, a failed or timed
	// out request, a NaN value, or a dead server).
	Calls, Fallbacks int
	// Dead is set once maxNetFailures requests in a row failed: the server
	// is not asked again and every later leaf is the offline evaluator's.
	// LastErr is the last failure.
	Dead    bool
	LastErr error
	fails   int
}

// NewNetLeaf builds the leaf for one seat of one game.
func NewNetLeaf(ev Evaluator, seeOpponentHand bool) *NetLeaf {
	return &NetLeaf{Eval: ev, SeeOpponentHand: seeOpponentHand, enc: mzbridge.NewEncoder(), fs: mzbridge.NewFeatureSet()}
}

// Leaf is the azmcts.LeafFunc.
func (n *NetLeaf) Leaf(e *rules.Engine, actor state.PlayerID) float64 {
	n.Calls++
	if n.Dead {
		n.Fallbacks++
		return OfflineLeaf(e, actor)
	}
	d := e.Pending()
	at, text := decisionHead(e, d)
	if err := n.enc.Encode(e, actor, d, at, text, n.fs, mzbridge.EncodeOptions{SeeOpponentHand: n.SeeOpponentHand}); err != nil {
		n.LastErr = err
		n.Fallbacks++
		return OfflineLeaf(e, actor)
	}
	ev, err := n.Eval.Evaluate(withSentinel(n.fs.IDs()))
	v := float64(ev.Value)
	if err != nil || math.IsNaN(v) {
		if err != nil {
			n.LastErr = err
			n.fails++
			if n.fails >= maxNetFailures {
				n.Dead = true
			}
		}
		n.Fallbacks++
		return OfflineLeaf(e, actor)
	}
	n.fails = 0
	return (v + 1) / 2
}
