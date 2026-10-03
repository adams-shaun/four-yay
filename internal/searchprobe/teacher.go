package searchprobe

import (
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Search-teacher spike (2026-09-19). Ensemble determinization in the sense of
// Cowling, Ward & Powley (2012): the deciding seat's observation history is
// turned into K sampled worlds by Sample (which never sees the actual engine),
// every candidate answer is rolled out on every world with the default bot as
// the rollout policy for both seats, and the candidate with the best mean
// outcome is the label. Nothing here is wired into a production seat.

// TeacherOptions bounds one teacher call.
type TeacherOptions struct {
	// Seed fixes the rollout bot randomness; world i uses Seed^(i+1) for every
	// candidate (common random numbers across candidates).
	Seed uint64
	// HorizonTurns stops a rollout this many engine turns after the root and
	// scores the leaf with Leaf (LeafValue when Leaf is nil). Zero rolls every
	// rollout to game end.
	HorizonTurns int32
	// Leaf scores a NON-TERMINAL leaf -- a rollout stopped by HorizonTurns or
	// MaxSubmits -- from the deciding seat's projection of it (redacted, or
	// omniscient under LeafOmniscient), as a win probability for actor. Nil means LeafValue (the frozen material
	// heuristic), and a nil Leaf reproduces the pre-Leaf results bit for bit.
	// A terminal leaf never reaches it: game over stays 1 / 0 / 0.5.
	//
	// Its result is clamped into [0,1]; NaN reads as 0.5, the no-information
	// value. With Parallelism > 1 it is called from several goroutines at once,
	// so it must be safe for concurrent use (policynet.Model.Value is), and it
	// must be a pure function of its arguments or the result stops being
	// independent of Parallelism. v is a rollout worker's reusable
	// projection (view.ProjectInto): it is valid only for the call, and Leaf
	// must not retain it or anything in it.
	Leaf func(v view.View, actor state.PlayerID) float64
	// LeafOmniscient (ticket pn17-a1) hands Leaf the OMNISCIENT projection of
	// a non-terminal leaf (view.ProjectFor with view.Omniscient, viewer =
	// actor: every seat's hand, never library order) instead of the actor's
	// redacted one, so a full-information value model can score it. That is
	// sound only because each world is SAMPLED: the opponent hand the leaf
	// reads is the sampler's, never the real one. It is therefore refused
	// together with Clairvoyant, whose worlds are clones of the real engine.
	// False (the zero value) is the redacted leaf, bit for bit. A nil Leaf
	// ignores it (LeafValue reads only public material either way).
	LeafOmniscient bool
	// MaxSubmits caps each rollout; a capped rollout is scored as a leaf.
	MaxSubmits int
	// Margin is how much a candidate's mean value must exceed candidate 0's
	// (the default bot's answer) before the teacher overrides it.
	Margin float64
	// Clairvoyant marks worlds that are clones of the ACTUAL engine (hidden
	// zones and future chance included). It exists only to measure the ceiling
	// of rollout search at a decision kind; labels made this way cheat.
	Clairvoyant bool
	// Parallelism is how many goroutines run the rollouts (<=1: sequential).
	// Wall clock only: the result is identical whatever it is set to.
	Parallelism int
}

// TeacherResult reports per-candidate mean values in [0,1] from the deciding
// seat's point of view.
type TeacherResult struct {
	Index               int
	Values              []float64
	Rollouts, Submits   int
	Terminal, Capped    int
	WinsByCandidate     []int
	TerminalByCandidate []int
	// WinsOverBaseline/LossesToBaseline retain the paired terminal outcome
	// needed by hindsight confidence intervals. For candidate i, they count
	// sampled worlds where i won and candidate 0 did not, or vice versa.
	// Common worlds and bot seeds make these genuine paired observations.
	WinsOverBaseline []int
	LossesToBaseline []int
}

// LeafValue squashes the frozen LeafScore into (0,1). Terminal states map to
// 1 (win), 0 (loss), 0.5 (draw). The /20 scale is untuned: roughly one
// creature of material or 20 life moves the value from 0.5 to 0.73.
func LeafValue(v view.View, actor state.PlayerID) float64 {
	if v.Over {
		switch {
		case v.Draw:
			return 0.5
		case v.Winner != nil && *v.Winner == actor:
			return 1
		default:
			return 0
		}
	}
	return 1 / (1 + math.Exp(-LeafScore(v, actor)/20))
}

// leafValue scores a rollout's final view: terminal states and a nil leaf go
// to LeafValue, anything else to leaf, clamped into [0,1] (NaN -> 0.5).
func leafValue(leaf func(view.View, state.PlayerID) float64, v view.View, actor state.PlayerID) float64 {
	if leaf == nil || v.Over {
		return LeafValue(v, actor)
	}
	x := leaf(v, actor)
	switch {
	case math.IsNaN(x):
		return 0.5
	case x < 0:
		return 0
	case x > 1:
		return 1
	}
	return x
}

// SemanticIntent is one observer-stable answer. Rest is KArrange's ordered
// complement; it stays separate so the longstanding Action/history encoding
// and sampler seeds remain unchanged.
type SemanticIntent struct {
	Choices []Action
	Rest    []Action
}

// TeacherChoice rolls every candidate on every world. candidates[0] must be
// the default bot's answer; each candidate is the full semantic choice list
// (a multi-select attackers declaration is several Actions).
func TeacherChoice(worlds []World, candidates [][]Action, opts TeacherOptions) (TeacherResult, error) {
	intents := make([]SemanticIntent, len(candidates))
	for i := range candidates {
		intents[i].Choices = candidates[i]
	}
	return TeacherIntentChoice(worlds, intents, opts)
}

// TeacherIntentChoice is TeacherChoice with support for KArrange's Rest.
// A panic inside a rollout is returned as an error (the caller falls back to
// the bot). Measured cause on 2026-09-19: rules/livelock.go detect() indexes
// sigAt(j-p) below zero while a Clone's freshly reset watcher window is short
// and its signatures repeat.
func TeacherIntentChoice(worlds []World, candidates []SemanticIntent, opts TeacherOptions) (res TeacherResult, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("rollout panic: %v", p)
		}
	}()
	res = TeacherResult{
		Values:          make([]float64, len(candidates)),
		WinsByCandidate: make([]int, len(candidates)), TerminalByCandidate: make([]int, len(candidates)),
		WinsOverBaseline: make([]int, len(candidates)), LossesToBaseline: make([]int, len(candidates)),
	}
	if opts.Clairvoyant && opts.LeafOmniscient {
		return res, fmt.Errorf("teacher: an omniscient leaf cannot be combined with the clairvoyant ceiling (its world is the real engine, so the leaf would read the real opponent's hand)")
	}
	if len(candidates) < 2 || len(worlds) == 0 || opts.MaxSubmits < 1 {
		return res, fmt.Errorf("teacher needs >=2 candidates, >=1 world and a submit budget")
	}
	// One rollout per (world, candidate). Each is a pure function of its own
	// engine clone, its candidate and the world's seed, so the rollouts may
	// run on several goroutines; the clones are taken up front on the caller's
	// goroutine (Clone reads the shared source engine) and the outcomes are
	// folded in the sequential loop's own (world, candidate) order, so every
	// float sum and the first error are exactly the sequential ones.
	type rollout struct {
		submits   int
		over, won bool
		capped    bool
		value     float64
		err       error
		panicked  any
		done      bool
	}
	outs := make([]rollout, len(worlds)*len(candidates))
	// lv is the worker's reusable leaf projection (view.ProjectInto): the
	// sequential loop owns one, and every parallel worker its own.
	run := func(k int, e *rules.Engine, lv *view.View) {
		o := &outs[k]
		defer func() {
			if p := recover(); p != nil {
				o.panicked = p
			}
		}()
		o.done = true
		wi, ci := k/len(candidates), k%len(candidates)
		w, cand := worlds[wi], candidates[ci]
		d := e.Pending()
		if d == nil {
			o.err = fmt.Errorf("world has no pending root decision")
			return
		}
		in, err := w.Observer.MatchIntent(d, cand.Choices, cand.Rest)
		if err != nil {
			o.err = fmt.Errorf("candidate %d does not map into world %d: %w", ci, wi, err)
			return
		}
		actor, turn := d.Player, e.G.Turn
		submit := e.SubmitHypothetical
		if opts.Clairvoyant {
			submit = e.Submit
		}
		if o.err = submit(in); o.err != nil {
			return
		}
		o.submits = 1
		rngs := BotRandoms(opts.Seed^uint64(wi+1), len(e.G.Players))
		board := botpolicy.NewBoard(len(rngs))
		for !e.G.Over && o.submits < opts.MaxSubmits && (opts.HorizonTurns == 0 || e.G.Turn < turn+opts.HorizonTurns) {
			pd := e.Pending()
			if pd == nil {
				o.err = fmt.Errorf("rollout has no pending decision")
				return
			}
			rin := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, pd.Player, &board), pd, rngs[pd.Player])
			if o.err = submit(rin); o.err != nil {
				return
			}
			o.submits++
		}
		o.over = e.G.Over
		o.won = e.G.Over && !e.G.Draw && e.G.Winner == actor
		o.capped = !e.G.Over && o.submits >= opts.MaxSubmits
		vis := view.Seat
		if opts.LeafOmniscient {
			vis = view.Omniscient
		}
		view.ProjectForInto(lv, e.G, e, actor, vis, e.Pending())
		o.value = leafValue(opts.Leaf, *lv, actor)
	}
	if workers := min(opts.Parallelism, len(outs)); workers > 1 {
		engines := make([]*rules.Engine, len(outs))
		for k := range engines {
			engines[k] = worlds[k/len(candidates)].Engine.Clone()
		}
		var wg sync.WaitGroup
		var next atomic.Int64
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var lv view.View
				for {
					k := int(next.Add(1)) - 1
					if k >= len(outs) {
						return
					}
					run(k, engines[k], &lv)
					engines[k] = nil
				}
			}()
		}
		wg.Wait()
	} else {
		var lv view.View
		for k := range outs {
			run(k, worlds[k/len(candidates)].Engine.Clone(), &lv)
			if outs[k].err != nil || outs[k].panicked != nil {
				break
			}
		}
	}
	for k := range outs {
		o := &outs[k]
		if o.panicked != nil {
			panic(o.panicked) // recovered into err by the deferred handler above
		}
		if !o.done {
			break
		}
		if o.err != nil {
			return res, o.err
		}
		ci := k % len(candidates)
		res.Rollouts++
		res.Submits += o.submits
		if o.over {
			res.Terminal++
			res.TerminalByCandidate[ci]++
			if o.won {
				res.WinsByCandidate[ci]++
			}
		} else if o.capped {
			res.Capped++
		}
		res.Values[ci] += o.value / float64(len(worlds))
	}
	// Fold paired wins only after every rollout succeeded. outs is laid out
	// world-major, so candidate 0 is the common-world baseline for each row.
	for wi := range worlds {
		base := outs[wi*len(candidates)].won
		for ci := 1; ci < len(candidates); ci++ {
			won := outs[wi*len(candidates)+ci].won
			switch {
			case won && !base:
				res.WinsOverBaseline[ci]++
			case base && !won:
				res.LossesToBaseline[ci]++
			}
		}
	}
	best := 0
	for i := 1; i < len(res.Values); i++ {
		if res.Values[i] > res.Values[best] {
			best = i
		}
	}
	if best != 0 && res.Values[best] <= res.Values[0]+opts.Margin {
		best = 0
	}
	res.Index = best
	return res, nil
}

// AttackCandidates enumerates declarations to compare at a KAttackers root:
// the bot's answer first, then no attack, all-in, and each single-attacker
// toggle of the bot's answer, deduplicated and validated, capped at limit.
// It assumes one option per attacker (two seats); options sharing an
// attacker keep only the first defender for the all-in candidate.
//
// Map- and fmt-free (it runs at every attack decision of every search
// simulation): a declaration is a set of Option.Index values, held as a
// bitset over option POSITIONS (bit i: the set holds Options[i].Index), and
// a duplicate is found by comparing against the candidates already kept.
func AttackCandidates(d *decision.Decision, bot decision.Intent, limit int) []decision.Intent {
	if d == nil || d.Kind != decision.KAttackers || limit < 2 {
		return nil
	}
	var setBuf [16]uint64
	bits, w := optionBits(&setBuf, len(d.Options), 4)
	chosen, none, all, t := bits[:w], bits[w:2*w], bits[2*w:3*w], bits[3*w:]
	markIndicesIn(d, chosen, bot.Choices)
	var choiceBuf [32]int
	// At most limit candidates out of bot + none + all + one toggle per
	// option; kept choice lists share one capped arena.
	out := make([]decision.Intent, 0, min(limit, len(d.Options)+3))
	var arena []int
	add := func(set []uint64) {
		if len(out) >= limit {
			return
		}
		choices := choiceBuf[:0]
		for i, o := range d.Options {
			if bitHas(set, i) {
				// A creature attacks one defender (the engine rejects a
				// declaration naming it twice, which Decision.Validate does
				// not catch): never offer one.
				for j := 0; j < i; j++ {
					if bitHas(set, j) && d.Options[j].Obj == o.Obj {
						return
					}
				}
				choices = append(choices, o.Index)
			}
		}
		// Decision.Validate does not enforce Required; the engine does
		// (CR 508.1d), through the shared quota rule
		// (decision.RequiredQuota), so never offer a declaration short of it.
		if d.RequiredChosen(choices) < d.RequiredQuota() {
			return
		}
		for i := range out {
			if sameChoices(out[i].Choices, choices) {
				return
			}
		}
		if d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}) != nil {
			return
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player}
		if len(choices) > 0 { // an empty declaration stays nil, as it always was
			in.Choices, arena = keepChoices(arena, choices, len(d.Options))
		}
		out = append(out, in)
	}
	add(chosen)
	if len(out) == 0 {
		return nil
	}
	// "No attack" is the least declaration the requirement allows: the
	// shared required core (decision.FitRequired over an empty preference).
	markIndicesIn(d, none, d.FitRequired(nil))
	add(none)
	// All-in: the Index of the first option of each attacker.
	for i, o := range d.Options {
		if firstOfObj(d, i) {
			markIndex(d, all, o.Index)
		}
	}
	add(all)
	for _, o := range d.Options {
		copy(t, chosen)
		for i := range d.Options {
			if d.Options[i].Index != o.Index {
				continue
			}
			on := !bitHas(t, i)
			t[i>>6] ^= 1 << (uint(i) & 63)
			if !on {
				continue
			}
			// Toggling on an attacker's option for another defender (a
			// planeswalker or battle beside the player) moves the creature
			// there: its other option leaves the declaration.
			for j := range d.Options {
				if j != i && d.Options[j].Obj == d.Options[i].Obj && bitHas(t, j) {
					t[j>>6] &^= 1 << (uint(j) & 63)
				}
			}
		}
		add(t)
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// optionBits returns k zeroed option-position bitsets over n options laid
// end to end (w words each, w returned), backed by buf unless the decision
// is past len(buf)*64/k options.
func optionBits(buf *[16]uint64, n, k int) (bits []uint64, w int) {
	w = (n + 63) >> 6
	if w*k <= len(buf) {
		return buf[:w*k], w
	}
	return make([]uint64, w*k), w
}

// markIndicesIn adds every value of vals to the position set b: bit i for
// each option whose Index is one of them.
func markIndicesIn(d *decision.Decision, b []uint64, vals []int) {
	for _, v := range vals {
		markIndex(d, b, v)
	}
}

// markIndex adds the Index value v to the position set b.
func markIndex(d *decision.Decision, b []uint64, v int) {
	for i := range d.Options {
		if d.Options[i].Index == v {
			bitSet(b, i)
		}
	}
}

// firstOfObj reports whether Options[i] is the first option naming its Obj.
func firstOfObj(d *decision.Decision, i int) bool {
	for j := 0; j < i; j++ {
		if d.Options[j].Obj == d.Options[i].Obj {
			return false
		}
	}
	return true
}

// keepChoices copies choices into arena and returns the copy, capped at its
// length so a caller's append can never write into a neighbour, with the
// grown arena. A fresh arena is sized for a few candidates of n options.
func keepChoices(arena, choices []int, n int) ([]int, []int) {
	if cap(arena)-len(arena) < len(choices) {
		arena = make([]int, 0, max(4*n, 2*len(choices), 16))
	}
	start := len(arena)
	arena = append(arena, choices...)
	return arena[start:len(arena):len(arena)], arena
}

func bitSet(b []uint64, i int)      { b[i>>6] |= 1 << (uint(i) & 63) }
func bitHas(b []uint64, i int) bool { return b[i>>6]&(1<<(uint(i)&63)) != 0 }

// BlockCandidates enumerates declarations to compare at a KBlockers root: the
// bot's answer first (index 0, the label contract), then "no blocks" (the
// least declaration the requirement allows, decision.FitRequired over an
// empty preference), then for every option the bot did not choose, the bot's
// answer with that (blocker, attacker) pair added and any other pair of the
// same blocker (same option Group -- one attacker per blocker) removed, then
// the bot's answer minus each of its own pairs. Capped at limit.
//
// Decision.Validate does not see the whole-declaration rules the engine
// enforces (CR 509.1a MinMaxBlocker bounds, CR 509.1b block charges), and one
// candidate the engine rejects makes TeacherChoice fail the WHOLE decision,
// so a candidate is kept only when Validate passes, the Required quota is
// met, decision.FitRequired would leave it unchanged (the repair the bot's
// own Clamp applies), and legal -- the bot's block guard,
// botpolicy.LegalBlockChoices, bound to the deciding seat's board -- returns
// it unchanged. A nil legal skips only that last check.
//
// Candidates keep the bot's declaration order (the engine reads it for
// CR 510.1c damage assignment); an added pair goes last. Duplicates are
// detected on the sorted choice list. Options are visited in index order
// only, so the output is deterministic. It returns nil unless at least two
// candidates survive.
//
// Map- and fmt-free like AttackCandidates: candidates are built in scratch
// and copied only when kept, the kept candidates' sorted lists live in one
// flat arena for the duplicate test, and the bot's membership is a scan of
// its (short) answer.
func BlockCandidates(d *decision.Decision, bot decision.Intent, limit int, legal func([]int) []int) []decision.Intent {
	if d == nil || d.Kind != decision.KBlockers || limit < 2 {
		return nil
	}
	out := make([]decision.Intent, 0, min(limit, len(d.Options)+len(bot.Choices)+2))
	var arena []int
	var sortBuf, keyBuf [64]int
	var endBuf [16]int
	keys, ends := keyBuf[:0], endBuf[:0] // kept candidates' sorted lists, flat
	// add tries one candidate; nilIfEmpty keeps an empty candidate built by
	// copying a nil list nil, exactly as the map-keyed version carried it.
	add := func(choices []int, nilIfEmpty bool) bool {
		if len(out) >= limit {
			return false
		}
		sorted := append(sortBuf[:0], choices...)
		slices.Sort(sorted)
		start := 0
		for _, end := range ends {
			if sameChoices(keys[start:end], sorted) {
				return false
			}
			start = end
		}
		if d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}) != nil {
			return false
		}
		if d.RequiredChosen(choices) < d.RequiredQuota() {
			return false
		}
		if !sameChoices(d.FitRequired(choices), choices) {
			return false
		}
		if legal != nil && !sameChoices(legal(append([]int(nil), choices...)), choices) {
			return false
		}
		keys = append(keys, sorted...)
		ends = append(ends, len(keys))
		in := decision.Intent{Seq: d.Seq, Player: d.Player}
		if len(choices) > 0 {
			in.Choices, arena = keepChoices(arena, choices, len(d.Options))
		} else if !nilIfEmpty {
			in.Choices = []int{}
		}
		out = append(out, in)
		return true
	}
	var baseBuf, nextBuf [32]int
	base := append(baseBuf[:0], bot.Choices...)
	if !add(base, true) {
		return nil
	}
	add(d.FitRequired(nil), true)
	for _, o := range d.Options {
		if slices.Contains(base, o.Index) {
			continue
		}
		next := nextBuf[:0]
		for _, c := range base {
			if o.Group != "" && c >= 0 && c < len(d.Options) && d.Options[c].Group == o.Group {
				continue
			}
			next = append(next, c)
		}
		add(append(next, o.Index), false)
	}
	for i := range base {
		next := append(nextBuf[:0], base[:i]...)
		add(append(next, base[i+1:]...), false)
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// sameChoices reports whether two choice lists are identical, order included.
func sameChoices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SingleTarget reports whether d is the one KTarget shape the teacher
// answers: exactly one object or player to pick (Min == Max == 1), no
// MaxSum/Budgeted budget, and at least two options to choose between. Every
// other KTarget -- multi-choice, budgeted, optional (Min 0) -- is left to the
// bot, whose Clamp repairs shapes a single-option swap cannot keep legal.
// searchseat.Eligible and TargetCandidates share this test so the cheap
// pre-check and the candidate builder cannot disagree.
func SingleTarget(d *decision.Decision) bool {
	return d != nil && d.Kind == decision.KTarget && d.Min == 1 && d.Max == 1 &&
		!d.HasBudget() && len(d.Options) >= 2
}

// TargetCandidates enumerates the answers to compare at a single-choice
// KTarget root (SingleTarget): the bot's own pick first (index 0, the label
// contract), then every other option in index order, each as a one-choice
// intent that d.Validate accepts. Capped at limit; nil unless the decision
// has the single-target shape, the bot answered with exactly one valid
// choice, and at least two candidates survive.
func TargetCandidates(d *decision.Decision, bot decision.Intent, limit int) []decision.Intent {
	if !SingleTarget(d) || limit < 2 || len(bot.Choices) != 1 {
		return nil
	}
	first := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{bot.Choices[0]}}
	if d.Validate(first) != nil {
		return nil
	}
	out := []decision.Intent{first}
	for _, o := range d.Options {
		if len(out) >= limit {
			break
		}
		if o.Index == bot.Choices[0] {
			continue
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}
		if d.Validate(in) != nil {
			continue
		}
		out = append(out, in)
	}
	if len(out) < 2 {
		return nil
	}
	return out
}
