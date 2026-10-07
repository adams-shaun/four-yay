package azmcts

import (
	"context"
)

// runCachedWorlds is the honest search under Options.CachedWorlds: PIMC-K
// over fixed worlds. The redeal source deals K worlds once; tree i walks
// world i with one future-chance seed for all its simulations
// (FixedChance), so every tree is a fixed-world search and the node cache (when on)
// resumes its walks below the root instead of re-walking known edges from
// it. Simulations are split as evenly as Sims allows, the first trees
// taking the remainder. The roots are merged by key, which every tree
// shares (the root point is the same): visits, availability and the
// visit-weighted Q summed across trees, the root value visit-weighted. A
// world whose deal fails discards its simulations as NoWorld, as the
// per-simulation source does.
func runCachedWorlds(ctx context.Context, root *Point, rs *RedealSource, cfg *walkConfig, opts Options, st *Stats) (TreeResult, error) {
	k := min(opts.CachedWorlds, opts.Sims)
	out := TreeResult{
		Visits: make([]int, len(root.Keys)), Q: make([]float64, len(root.Keys)),
		Avail: make([]int, len(root.Keys)),
	}
	qw := make([]float64, len(root.Keys))
	rootW, rootN := 0.0, 0
	rs.prepare()
	for i := 0; i < k; i++ {
		share := opts.Sims / k
		if i < opts.Sims%k {
			share++
		}
		if rs.refused != "" {
			st.Simulations += share
			st.NoWorld += share
			continue
		}
		base, reason := rs.r.Deal(RedealSeed(rs.seed, i), nil)
		if reason != "" {
			rs.dealFailed(reason)
			st.Simulations += share
			st.NoWorld += share
			continue
		}
		fc := &FixedChance{Base: base, Observer: rs.obs, Seed: splitmix(rs.seed ^ splitmix(uint64(i)+0x51))}
		local := opts
		local.Sims = share
		var envs EnvSource = &worldEnvs{src: fc, cfg: cfg}
		if opts.NodeCache > 0 {
			envs = &fixedEnvs{worldEnvs: envs.(*worldEnvs)}
		}
		tr, err := RunTree(ctx, root, envs, local, st)
		if err != nil {
			return TreeResult{}, err
		}
		n := 0
		for j := range root.Keys {
			out.Visits[j] += tr.Visits[j]
			out.Avail[j] += tr.Avail[j]
			qw[j] += tr.Q[j] * float64(tr.Visits[j])
			n += tr.Visits[j]
		}
		rootW += tr.RootValue * float64(n)
		rootN += n
		if st.DeadlineHits > 0 {
			break
		}
	}
	for j := range root.Keys {
		if out.Visits[j] > 0 {
			out.Q[j] = qw[j] / float64(out.Visits[j])
		}
	}
	out.RootValue = 0.5
	if rootN > 0 {
		out.RootValue = rootW / float64(rootN)
	}
	return out, nil
}
