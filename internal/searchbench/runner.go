package searchbench

// One arm on one item: rebuild the item's engines from the item store,
// RunArm, and project the answer and the root table onto the item's
// canonical options (Result v2). Timing is the command's (CoreSeconds is
// filled at the command boundary).

import (
	"context"
	"fmt"
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/rules"
)

// RunConfig is one run: an arm, a budget and the tree knobs.
type RunConfig struct {
	Arm          SearchArm
	Name         string // Result.Arm
	Sims         int
	Discount     float64
	DiscountUnit azmcts.DiscountUnit
	// NodeCache is Options.NodeCache: the fixed-world trees' node state
	// cap (0 off). It changes only EnvSteps, never an answer or a table.
	NodeCache int
	// OpponentNodes is Options.OpponentNodes: the tree also branches on
	// the opponent's searched decisions (fixed-world arms only: RunArm
	// refuses is-mcts).
	OpponentNodes bool
	Net           *policynet.Model
	Seed          uint64
	Digest        string // the manifest's
}

// WorldsFor is how many belief worlds an arm reads.
func WorldsFor(a SearchArm) int {
	switch a {
	case ArmPIMC1:
		return 1
	case ArmPIMC4:
		return PIMCWorlds
	case ArmISMCTS:
		return WorldCount
	}
	return 0
}

// ItemSeed is an item's search seed: the run seed and the item ID, so an
// item's answer does not depend on which items run before it or on which
// worker runs it.
func ItemSeed(seed uint64, id string) uint64 {
	h := uint64(0xcbf29ce484222325)
	for i := 0; i < len(id); i++ {
		h = (h ^ uint64(id[i])) * 0x100000001b3
	}
	return splitMix64(seed ^ splitMix64(h))
}

// RunItem answers one item with one arm.
func RunItem(ctx context.Context, reg *cards.Registry, it Item, s *StoreItem, cfg RunConfig) (Result, error) {
	r := Result{ManifestDigest: cfg.Digest, Arm: cfg.Name, ItemID: it.ID}
	if s.ID != it.ID || s.Row != it.Row || s.Type != it.Type {
		return r, fmt.Errorf("searchbench: store item %q does not match manifest item %q", s.ID, it.ID)
	}
	pos, err := PositionItem(reg, s, WorldsFor(cfg.Arm))
	if err != nil {
		return r, fmt.Errorf("searchbench: %s: rebuilding the item: %w", it.ID, err)
	}
	if !slices.Equal(pos.Canon.Labels(), it.Options) || pos.Canon.FocusAlias != it.Focus {
		return r, fmt.Errorf("searchbench: %s: rebuilt options %q (focus %q), manifest %q (focus %q)", it.ID, pos.Canon.Labels(), pos.Canon.FocusAlias, it.Options, it.Focus)
	}
	real := pos.Real.M.Engine
	opts := BenchOptions(cfg.Sims)
	opts.Discount, opts.DiscountUnit = cfg.Discount, cfg.DiscountUnit
	opts.NodeCache = cfg.NodeCache
	opts.OpponentNodes = cfg.OpponentNodes
	r.Seed = ItemSeed(cfg.Seed, it.ID)
	in := ArmInput{Arm: cfg.Arm, Options: opts, Seed: r.Seed, Real: real, Worlds: pos.WorldEngines(), Net: cfg.Net}
	res, err := RunArm(ctx, in)
	if err != nil {
		return r, fmt.Errorf("searchbench: %s: %w", it.ID, err)
	}
	var choice int
	var act bool
	if res.Searched && azmcts.IsMacroKey(res.Key) {
		choice, act, err = pos.Canon.projectMacro(res.Key)
	} else {
		choice, act, err = pos.Canon.Project(real, res.Intent)
	}
	if err != nil && IsManaActivation(real.Pending(), res.Intent) {
		choice, act, err = followBot(pos.Canon, real, res.Intent, splitMix64(r.Seed^0x626f742d61727365))
	}
	if err != nil {
		return r, fmt.Errorf("searchbench: %s: projecting the answer: %w", it.ID, err)
	}
	r.AgentChoices, r.AgentAct = []int{choice}, act
	r.RootMacros, r.RootUnreached = res.RootMacros(), len(res.RootUnreached())
	st := res.Stats
	r.Sims, r.Completed, r.EnvSteps = st.Simulations, st.Completed, st.EnvSteps
	if st.Completed > 0 {
		c := float64(st.Completed)
		r.MeanLeafPlies, r.MeanLeafEdges, r.MeanTurnsCrossed = float64(st.LeafPlies)/c, float64(st.LeafEdges)/c, float64(st.LeafTurns)/c
	}
	if cfg.Arm != ArmNoSearch && !res.Searched {
		r.Fallback = fallbackReason(res)
	}
	if len(res.Table) > 0 {
		if r.Root, err = projectRoot(pos, res.Table); err != nil {
			return r, fmt.Errorf("searchbench: %s: projecting the root table: %w", it.ID, err)
		}
	}
	return r, nil
}

// fallbackReason says why a search arm played the bot's answer.
func fallbackReason(res ArmResult) string {
	st := res.Stats
	switch {
	case st.DeadlineHits > 0:
		return "deadline"
	case st.RedealRefused > 0:
		return "redeal-refused"
	case st.AllFailed > 0 || st.Simulations > 0 && st.Completed == 0:
		return "all-failed"
	case st.Skipped > 0:
		return "skipped"
	case st.FeedStopped > 0:
		return "feed-stopped"
	}
	return "not-searched"
}

// projectRoot sums the root table onto canonical choices: each row's key is
// played on the real engine (the root observer's IntentForKey) and projected
// (Canon.Project); visits add, Q is visit-weighted. A joint attack or block
// declaration counts for the focus creature's answer.
func projectRoot(pos *Positioned, table []azmcts.RootRow) ([]RootOption, error) {
	real := pos.Real.M.Engine
	d := real.Pending()
	obs, err := rootObserver(real, d.Player)
	if err != nil {
		return nil, err
	}
	visits := make([]int, len(pos.Canon.Options))
	qsum := make([]float64, len(pos.Canon.Options))
	seen := make([]bool, len(pos.Canon.Options))
	for _, row := range table {
		var c int
		if azmcts.IsMacroKey(row.Key) {
			if c, _, err = pos.Canon.projectMacro(row.Key); err != nil {
				return nil, fmt.Errorf("key %s: %w", row.Label, err)
			}
		} else {
			in, err := azmcts.IntentForKey(obs, real, d, row.Key)
			if err != nil {
				return nil, fmt.Errorf("key %s: %w", row.Label, err)
			}
			if c, _, err = pos.Canon.Project(real, in); err != nil {
				return nil, fmt.Errorf("key %s: %w", row.Label, err)
			}
		}
		seen[c] = true
		visits[c] += row.Visits
		qsum[c] += row.Q * float64(row.Visits)
	}
	var out []RootOption
	for c := range visits {
		if !seen[c] {
			continue
		}
		o := RootOption{Choice: c, Visits: visits[c]}
		if visits[c] > 0 {
			o.Q = qsum[c] / float64(visits[c])
		}
		out = append(out, o)
	}
	return out, nil
}

// followBot projects a mana activation at the root: the auto-pay bot taps
// mana first and then casts from the pool, so its answer is the first
// non-mana action it takes at the searching seat's priority, played out on
// a hypothetical clone (the bot's own seed, as RunArm's). Passing with mana
// floating is Pass.
func followBot(c *Canon, root *rules.Engine, first decision.Intent, botSeed uint64) (int, bool, error) {
	e := root.CloneHypothetical(1)
	actor := root.Pending().Player
	in := first
	for step := 0; step < 32; step++ {
		if err := e.SubmitHypothetical(in); err != nil {
			return -1, false, fmt.Errorf("following the bot's mana activation: %w", err)
		}
		d := e.Pending()
		if d == nil || e.G.Over || d.Player != actor || len(e.G.Stack) != 0 {
			return -1, false, fmt.Errorf("following the bot's mana activation: left the root priority")
		}
		in = botAnswer(e, botSeed)
		if d.Kind != decision.KPriority { // the mana ability's own ask (a colour)
			continue
		}
		if !IsManaActivation(d, in) {
			return c.ProjectAt(e, d, in)
		}
	}
	return -1, false, fmt.Errorf("following the bot's mana activation: more than 32 activations")
}
