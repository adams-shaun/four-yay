//go:build enginebench_az

package main

import (
	"context"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/searchseat"
)

// az.go links the tree search (internal/azmcts, which a4af596 does not have):
// the eval seat (DefaultSeatConfig: argmax, no noise, heuristic leaf with no
// network) over honest redeals -- every simulation a fresh deal of what seat
// 0 cannot see (World = redeal, Worlds = 0).
func init() {
	azSearch = func(seed uint64, sims int) searchFn {
		cfg := azmcts.DefaultSeatConfig()
		cfg.World = azmcts.WorldRedeal
		cfg.Search.Sims = sims
		s, err := azmcts.NewSeat(seed, nil, cfg)
		if err != nil {
			panic(err)
		}
		return func(env searchseat.Env, d decision.Decision) (searchOutcome, error) {
			var got *azmcts.Diag
			azmcts.Watch = func(dg azmcts.Diag) { got = &dg }
			t0, c0 := time.Now(), cpuNow()
			_, err := s.DecideSearch(context.Background(), env, d)
			ms, cms := float64(time.Since(t0).Nanoseconds())/1e6, (cpuNow()-c0)*1e3
			azmcts.Watch = nil
			if err != nil || got == nil || !got.Searched || got.Refused != "" {
				return searchOutcome{MS: ms, CPUMS: cms}, err
			}
			return searchOutcome{Searched: true, Work: got.Stats.Simulations, Extra: got.Stats.EnvSteps, MS: ms, CPUMS: cms}, nil
		}
	}
}
