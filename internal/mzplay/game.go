package mzplay

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/mzbridge"
	"github.com/adams-shaun/gorge/internal/searchbench"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// One self-play game: the counterpart of ParallelDataGenerator.runSingleGame
// with two ComputerPlayerMCTS2 players. Both seats search every decision of
// a searched kind on the REAL engine state (the clairvoyant world: upstream's
// MCTS also searches a copy of the real game, hidden cards included, and
// hides the opponent's hand only from the network's input), and each
// searched decision becomes one or more training records of the deciding
// seat.

// SeatSetup is one seat of one game.
type SeatSetup struct {
	// Deck is the seat's main deck and DeckName its .dck stem.
	Deck     []*cards.Card
	DeckName string
	// Budget is mcts.search_budget: simulations per searched decision.
	Budget int
	// BackpropDiscount is mcts.backprop_discount (0.99): the backup discount
	// per searched tree edge.
	BackpropDiscount float64
	// Lambda is mcts.td_discount, the value target's TD-lambda.
	Lambda float64
	// SeeOpponentHand is hiddenInfo.see_opponent_hand: the recorded states
	// (and a network leaf's) encode both hands.
	SeeOpponentHand bool
	// Leaf is the seat's leaf evaluator: OfflineLeaf (mcts.offline_mode) or
	// a NetLeaf's.
	Leaf azmcts.LeafFunc
}

// GameSetup is one game.
type GameSetup struct {
	Index int
	Seed  uint64
	Seats [2]SeatSetup
	// GoesFirst is game.yml's goes_first: "player_a", "player_b" or
	// "random" (the engine's own seeded toss).
	GoesFirst string
	// MaxTurns is training.max_turns: a game still running when turn
	// MaxTurns ends has no winner.
	MaxTurns int
	// MaxSubmits bounds the engine submits of one game (a livelock guard);
	// 0 is DefaultMaxSubmits.
	MaxSubmits int

	Tokens       map[string]*cards.Card
	NameUniverse []*cards.Card
	Vocab        *mzbridge.Vocab

	// DecisionContext, when set, bounds one searched decision's wall time
	// (mcts.timeout_ms). A search it stops plays the bot's answer and is not
	// recorded. Nil never stops a search: the game is then a pure function
	// of the setup.
	DecisionContext func(seat int) (context.Context, context.CancelFunc)
	// Abort, when set, is polled before every decision; true ends the game
	// as failed (training.max_minutes).
	Abort func() bool
	// Trace, when set, receives one line per searched decision: the turn,
	// the seat, the kind, the answer played and the root table.
	Trace func(line string)
}

// DefaultMaxSubmits is the per-game submit cap.
const DefaultMaxSubmits = 20000

// GameStats counts one game's work.
type GameStats struct {
	Submits  int // engine submits, macro steps included
	Trivial  int // pass-only priority windows, answered without a search
	Searches int // decisions a tree was built for and answered from
	Bot      int // other decisions: the bot's answer was played
	Timeouts int // searches the decision deadline stopped
	// MacroFailed counts chosen macros a later step of which the live engine
	// refused; the game went on from wherever the macro stopped.
	MacroFailed int
	Simulations int
	Completed   int
	SimFailures int // simulations discarded (panics, rejected submits, chance failures)
	// Rows by head, both seats.
	RowsPriority, RowsTarget, RowsUse int
	// The action vocabulary's coverage of what was recorded: root candidates
	// (and the visits on them) whose label has a slot of its own, against
	// all of them. Everything else lands in the hashed tail.
	ActionCands, ActionHits  int
	ActionVisits, ActionHitV int
	TargetCands, TargetHits  int
	TargetVisits, TargetHitV int
	// MissedActions and MissedTargets list the labels with no slot, each
	// once, in first-seen order (capped).
	MissedActions, MissedTargets []string
}

// Add sums o into s (the miss lists are merged, each label once).
func (s *GameStats) Add(o GameStats) {
	s.Submits += o.Submits
	s.Trivial += o.Trivial
	s.Searches += o.Searches
	s.Bot += o.Bot
	s.Timeouts += o.Timeouts
	s.MacroFailed += o.MacroFailed
	s.Simulations += o.Simulations
	s.Completed += o.Completed
	s.SimFailures += o.SimFailures
	s.RowsPriority += o.RowsPriority
	s.RowsTarget += o.RowsTarget
	s.RowsUse += o.RowsUse
	s.ActionCands += o.ActionCands
	s.ActionHits += o.ActionHits
	s.ActionVisits += o.ActionVisits
	s.ActionHitV += o.ActionHitV
	s.TargetCands += o.TargetCands
	s.TargetHits += o.TargetHits
	s.TargetVisits += o.TargetVisits
	s.TargetHitV += o.TargetHitV
	for _, m := range o.MissedActions {
		s.MissedActions = addOnce(s.MissedActions, m)
	}
	for _, m := range o.MissedTargets {
		s.MissedTargets = addOnce(s.MissedTargets, m)
	}
}

const maxMissed = 200

func addOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	if len(list) >= maxMissed {
		return list
	}
	return append(list, s)
}

// GameResult is one finished game.
type GameResult struct {
	Index int
	Seed  uint64
	// First is the seat that took turn 1 (-1 when the game never reached
	// it); Winner the winning seat or NoWinner.
	First, Winner int
	Turns         int
	// Drawn is every card each seat drew, opening hand included, by name
	// (CardsDrawnWatcher).
	Drawn [2][]string
	// Rows are the seats' records, value labels set.
	Rows  [2][]Row
	Stats GameStats
}

// ErrAborted is PlayGame's error for a game its Abort hook ended.
var ErrAborted = errors.New("mzplay: game aborted (training.max_minutes)")

// SearchOptions are the azmcts knobs that stand for upstream's MCTS
// (ComputerPlayerMCTS2 / MCTSNode): PUCT with c = 1 and unvisited children
// valued 0 on upstream's [-1, 1] scale, which on azmcts's [0, 1] scale is
// c = 0.5 and an unvisited value of 0.5 (searchbench.BenchOptions); uniform
// priors; no root noise; the move by visits; the backup discounted per
// searched tree edge (MCTSNode.backpropagate multiplies by backpropDiscount
// once per parent); budget simulations per decision.
//
// What does not match and cannot be set here: the tree holds only the
// searching seat's decisions (the opponent inside a simulation is the
// default bot, where upstream's tree has opponent nodes); there is no
// subtree reuse between decisions (upstream's budget counts the visits the
// reused subtree already holds); PUCT's exploration term reads the count of
// simulations the child was available in plus one, not the parent's visits
// (identical in a fixed world except for the +1); a whole attack or block
// declaration is one node.
func SearchOptions(budget int, backpropDiscount float64, leaf azmcts.LeafFunc) azmcts.Options {
	o := searchbench.BenchOptions(budget)
	o.Discount = backpropDiscount
	o.DiscountUnit = azmcts.DiscountAction
	o.Leaf = leaf
	return o
}

// splitmix is SplitMix64's finaliser.
func splitmix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// passOnly reports whether priority decision d offers nothing but a pass
// (and the concession every priority window carries) and the engine offers
// no payment action: upstream's "playableAbilities.size() < 2: auto pass".
func passOnly(e *rules.Engine, d *decision.Decision) (decision.Intent, bool) {
	if d.Kind != decision.KPriority {
		return decision.Intent{}, false
	}
	pass := -1
	for _, o := range d.Options {
		switch o.Kind {
		case "pass":
			if pass < 0 {
				pass = o.Index
			}
		case "concede":
		default:
			return decision.Intent{}, false
		}
	}
	if pass < 0 || len(e.EnsurePaymentActions()) > 0 {
		return decision.Intent{}, false
	}
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}, true
}

// PlayGame plays one game to its end. The error return is a failed game (an
// engine panic, a rejected intent, the submit cap, an abort): upstream
// ignores such a game, and so must the caller -- no record of it is kept.
func PlayGame(gs GameSetup) (res GameResult, err error) {
	res = GameResult{Index: gs.Index, Seed: gs.Seed, First: -1, Winner: NoWinner}
	if gs.Vocab == nil {
		return res, errors.New("mzplay: PlayGame needs the action vocabulary")
	}
	for i, s := range gs.Seats {
		if s.Leaf == nil || s.Budget < 1 || len(s.Deck) == 0 {
			return res, fmt.Errorf("mzplay: seat %d needs a deck, a budget and a leaf", i)
		}
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("mzplay: game %d (seed %d) panicked: %v\n%s", gs.Index, gs.Seed, p, debug.Stack())
		}
	}()
	cfg := rules.Config{
		Seed:  gs.Seed,
		Names: []string{gs.Seats[0].DeckName, gs.Seats[1].DeckName},
		// PlayerNames are XMage's: the encoder and the target vocabulary
		// never read them, the log does.
		PlayerNames:  []string{"PlayerA", "PlayerB"},
		Decks:        [][]*cards.Card{gs.Seats[0].Deck, gs.Seats[1].Deck},
		Tokens:       gs.Tokens,
		NameUniverse: gs.NameUniverse,
	}
	var e *rules.Engine
	force := -1
	switch gs.GoesFirst {
	case "player_a":
		force = 0
	case "player_b":
		force = 1
	}
	if force >= 0 {
		e = rules.NewStartingPlayerChoice(cfg)
		e.AskStartingPlayer()
	} else {
		e = rules.New(cfg)
	}
	e.Advance()

	maxSubmits := gs.MaxSubmits
	if maxSubmits <= 0 {
		maxSubmits = DefaultMaxSubmits
	}
	rec := [2]*recorder{}
	seatSeed := [2]uint64{}
	opts := [2]azmcts.Options{}
	for i := range gs.Seats {
		s := gs.Seats[i]
		rec[i] = newRecorder(state.PlayerID(i), gs.Vocab, s.SeeOpponentHand, &res.Stats)
		seatSeed[i] = splitmix(gs.Seed ^ (uint64(i)+1)*0x6d7a706c61792d73)
		opts[i] = SearchOptions(s.Budget, s.BackpropDiscount, s.Leaf)
	}
	submit := func(in decision.Intent) error {
		res.Stats.Submits++
		return e.Submit(in)
	}
	for !e.G.Over {
		d := e.Pending()
		if d == nil {
			return res, fmt.Errorf("mzplay: game %d: no pending decision and the game is not over", gs.Index)
		}
		if gs.MaxTurns > 0 && int(e.G.Turn) > gs.MaxTurns {
			break // GameOptions.stopOnTurn: nobody won
		}
		if res.Stats.Submits >= maxSubmits {
			return res, fmt.Errorf("mzplay: game %d: %d submits without an end (turn %d)", gs.Index, res.Stats.Submits, e.G.Turn)
		}
		if gs.Abort != nil && gs.Abort() {
			return res, ErrAborted
		}
		if d.Kind == decision.KStartingPlayer && force >= 0 {
			in, ok := startingPlayerIntent(d, state.PlayerID(force))
			if !ok {
				return res, fmt.Errorf("mzplay: game %d: the starting-player choice does not offer seat %d", gs.Index, force)
			}
			if err := submit(in); err != nil {
				return res, err
			}
			continue
		}
		if int(d.Player) >= 2 {
			return res, fmt.Errorf("mzplay: game %d: decision for seat %d", gs.Index, d.Player)
		}
		if in, ok := passOnly(e, d); ok {
			res.Stats.Trivial++
			if err := submit(in); err != nil {
				return res, err
			}
			continue
		}
		p := int(d.Player)
		o := opts[p]
		o.Seed = azmcts.DecisionSeed(seatSeed[p], d.Seq)
		botSeed := splitmix(seatSeed[p] ^ splitmix(d.Seq^0x626f74))
		ctx, cancel := context.Background(), context.CancelFunc(func() {})
		if gs.DecisionContext != nil {
			ctx, cancel = gs.DecisionContext(p)
		}
		lr, serr := searchbench.SearchLive(ctx, e, botSeed, nil, o)
		cancel()
		if serr != nil {
			return res, fmt.Errorf("mzplay: game %d, decision %d: %w", gs.Index, d.Seq, serr)
		}
		st := lr.Result.Stats
		res.Stats.Simulations += st.Simulations
		res.Stats.Completed += st.Completed
		res.Stats.SimFailures += st.Simulations - st.Completed - st.DeadlineHits
		if st.DeadlineHits > 0 {
			res.Stats.Timeouts++
		}
		if !lr.Chosen() {
			res.Stats.Bot++
			if err := submit(lr.Result.Intent); err != nil {
				return res, fmt.Errorf("mzplay: game %d, decision %d: the bot's answer: %w", gs.Index, d.Seq, err)
			}
			continue
		}
		res.Stats.Searches++
		if gs.Trace != nil {
			gs.Trace(traceLine(e, d, lr))
		}
		rows, rerr := rec[p].rows(e, d, lr)
		if rerr != nil {
			return res, fmt.Errorf("mzplay: game %d, decision %d: %w", gs.Index, d.Seq, rerr)
		}
		res.Rows[p] = append(res.Rows[p], rows...)
		n, perr := lr.Play(e, lr.Result.Choice)
		res.Stats.Submits += n
		if perr != nil {
			if n == 0 {
				return res, fmt.Errorf("mzplay: game %d, decision %d: %w", gs.Index, d.Seq, perr)
			}
			// A macro stopped part-way: its first steps are played and
			// legal, the game goes on from there.
			res.Stats.MacroFailed++
		}
	}
	res.Turns = int(e.G.Turn)
	if gs.MaxTurns > 0 && res.Turns > gs.MaxTurns {
		res.Turns = gs.MaxTurns
	}
	if e.G.Over && !e.G.Draw && int(e.G.Winner) < 2 {
		res.Winner = int(e.G.Winner)
	}
	for _, ev := range e.L.Events {
		switch ev.Kind {
		case events.TurnChange:
			if res.First < 0 {
				res.First = int(ev.Player)
			}
		case events.Draw:
			if int(ev.Player) < 2 {
				if o := e.G.Obj(ev.Obj); o != nil && o.Card != nil && len(o.Card.Faces) > 0 {
					res.Drawn[ev.Player] = append(res.Drawn[ev.Player], o.Card.Faces[0].Name)
				}
			}
		}
	}
	for i := range res.Rows {
		LabelValues(res.Rows[i], SeatWon(i, res.Winner), gs.Seats[i].Lambda)
	}
	return res, nil
}

// startingPlayerIntent answers CR 103.1's choice with seat who.
func startingPlayerIntent(d *decision.Decision, who state.PlayerID) (decision.Intent, bool) {
	for _, o := range d.Options {
		if o.Player == who {
			return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}, true
		}
	}
	return decision.Intent{}, false
}

// recorder turns one seat's searched decisions into records.
type recorder struct {
	seat  state.PlayerID
	vocab *mzbridge.Vocab
	index *actionIndexer
	see   bool
	enc   *mzbridge.Encoder
	fs    *mzbridge.FeatureSet
	stats *GameStats
}

func newRecorder(seat state.PlayerID, vocab *mzbridge.Vocab, see bool, stats *GameStats) *recorder {
	return &recorder{seat: seat, vocab: vocab, index: newActionIndexer(vocab), see: see, enc: mzbridge.NewEncoder(), fs: mzbridge.NewFeatureSet(), stats: stats}
}

// encode is the seat's encoding of the real state at decision d.
func (r *recorder) encode(e *rules.Engine, d *decision.Decision, at mzbridge.ActionType, text string, hist mzbridge.MicroHistory) ([]int32, error) {
	if err := r.enc.Encode(e, r.seat, d, at, text, r.fs, mzbridge.EncodeOptions{SeeOpponentHand: r.see, Player: hist}); err != nil {
		return nil, err
	}
	return withSentinel(r.fs.IDs()), nil
}

// rows are the records of one searched decision (the recording rules of
// ComputerPlayerMCTS2.calculateActions and its callers):
//
//   - priority: one PRIORITY record, the root children's visits at their
//     action indices;
//   - attackers: one CHOOSE_USE record per potential attacker, the
//     projection AttackMarginals; each record's state carries the answers
//     already given for the creatures before it (the encoder's micro-decision
//     history), as upstream's successive chooseUse calls do;
//   - blockers: one CHOOSE_TARGET record per potential blocker
//     (BlockMarginals), likewise carrying the attackers already chosen;
//   - a single target: one CHOOSE_TARGET record by entity name.
//
// Every record of a decision carries the root's mean value as its score.
func (r *recorder) rows(e *rules.Engine, d *decision.Decision, lr searchbench.LiveRoot) ([]Row, error) {
	res := lr.Result
	score := ScoreFromRootValue(res.RootValue)
	switch res.Kind {
	case "priority":
		ids, err := r.encode(e, d, mzbridge.Priority, "priority", mzbridge.MicroHistory{})
		if err != nil {
			return nil, err
		}
		index := make([]int, len(res.Keys))
		for i := range res.Keys {
			var a searchbench.LiveAction
			if i < len(lr.Actions) {
				a = lr.Actions[i]
			}
			rule, flashback := "", ""
			if o := e.G.Obj(a.Obj); o != nil {
				switch {
				case a.Kind == "ability" && a.Ability >= 0:
					rule, _ = r.enc.ActivatedRule(o.Face(), a.Ability)
				case a.Kind == "cast" && a.Mode == "flashback":
					flashback = flashbackCost(o.Face())
				}
			}
			label, known := r.index.resolve(ActionLabel(a, rule, flashback))
			index[i] = r.vocab.ActionIndex(label)
			v := 0
			if i < len(res.Visits) {
				v = res.Visits[i]
			}
			r.stats.ActionCands++
			r.stats.ActionVisits += v
			if known {
				r.stats.ActionHits++
				r.stats.ActionHitV += v
			} else {
				r.stats.MissedActions = addOnce(r.stats.MissedActions, label)
			}
		}
		r.stats.RowsPriority++
		return []Row{{IDs: ids, Policy: policyFromVisits(index, res.Visits), Score: score, Type: mzbridge.Priority}}, nil
	case "attackers":
		var out []Row
		var hist mzbridge.MicroHistory
		for _, m := range AttackMarginals(d, res.Candidates, res.Visits, res.Choice) {
			ids, err := r.encode(e, d, mzbridge.ChooseUse, attackText(objectName(e, m.Obj)), hist)
			if err != nil {
				return nil, err
			}
			out = append(out, Row{IDs: ids, Score: score, Type: mzbridge.ChooseUse,
				Policy: policyFromVisits([]int{mzbridge.UseIndex(false), mzbridge.UseIndex(true)}, []int{m.No, m.Yes})})
			hist.Uses = append(hist.Uses, m.Chosen)
			r.stats.RowsUse++
		}
		return out, nil
	case "blockers":
		var out []Row
		var hist mzbridge.MicroHistory
		for _, m := range BlockMarginals(d, res.Candidates, res.Visits, res.Choice) {
			ids, err := r.encode(e, d, mzbridge.ChooseTarget, blockText(objectName(e, m.Obj)), hist)
			if err != nil {
				return nil, err
			}
			index := []int{r.vocab.TargetIndex(StopChoosing)}
			visits := []int{m.Stop}
			for k, a := range m.Attackers {
				name := objectName(e, a)
				index = append(index, r.vocab.TargetIndex(name))
				visits = append(visits, m.Visits[k])
				r.countTarget(name, m.Visits[k])
			}
			out = append(out, Row{IDs: ids, Score: score, Type: mzbridge.ChooseTarget, Policy: policyFromVisits(index, visits)})
			if m.Chosen != 0 {
				hist.Targets = append(hist.Targets, objectName(e, m.Chosen))
			}
			r.stats.RowsTarget++
		}
		return out, nil
	case "target":
		ids, err := r.encode(e, d, mzbridge.ChooseTarget, targetText(e, d), mzbridge.MicroHistory{})
		if err != nil {
			return nil, err
		}
		index := make([]int, len(res.Candidates))
		for i, c := range res.Candidates {
			name := "null"
			if len(c.Choices) == 1 {
				if o, ok := optionAt(d, c.Choices[0]); ok {
					name = TargetName(e, o, r.seat)
				}
			}
			index[i] = r.vocab.TargetIndex(name)
			v := 0
			if i < len(res.Visits) {
				v = res.Visits[i]
			}
			r.countTarget(name, v)
		}
		r.stats.RowsTarget++
		return []Row{{IDs: ids, Policy: policyFromVisits(index, res.Visits), Score: score, Type: mzbridge.ChooseTarget}}, nil
	}
	return nil, fmt.Errorf("mzplay: no record rule for a searched %q decision", res.Kind)
}

func (r *recorder) countTarget(name string, visits int) {
	r.stats.TargetCands++
	r.stats.TargetVisits += visits
	if r.vocab.KnownTarget(name) {
		r.stats.TargetHits++
		r.stats.TargetHitV += visits
	} else {
		r.stats.MissedTargets = addOnce(r.stats.MissedTargets, name)
	}
}

// traceLine describes one searched decision.
func traceLine(e *rules.Engine, d *decision.Decision, lr searchbench.LiveRoot) string {
	r := lr.Result
	label := func(i int) string {
		if i < len(lr.Actions) && lr.Actions[i].Label != "" {
			return lr.Actions[i].Label
		}
		if i < len(r.Labels) {
			return r.Labels[i]
		}
		return "?"
	}
	s := fmt.Sprintf("turn %d step %d seat %d %s: chose [%s] value %.3f |", e.G.Turn, e.G.Step, d.Player, r.Kind, label(r.Choice), r.RootValue)
	for i := range r.Keys {
		v, q := 0, 0.0
		if i < len(r.Visits) {
			v = r.Visits[i]
		}
		if i < len(r.Q) {
			q = r.Q[i]
		}
		s += fmt.Sprintf(" [%s n=%d q=%.3f]", label(i), v, q)
	}
	return s
}
