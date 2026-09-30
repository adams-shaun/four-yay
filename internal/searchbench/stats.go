package searchbench

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// Bootstrap is the confidence-interval method: upstream analyze.py's
// bootstrap(), a game-cluster percentile bootstrap. Each resample draws as
// many games as there are, with replacement (rng.choice(games) per game, from
// random.Random(Seed)), and scores the concatenation of the drawn games'
// items; a game's items go together because they are correlated. The 95% CI
// is vals[int(0.025*k)] and vals[int(0.975*k)-1] of the k sorted resample
// values that are defined. Every metric of a run, and every paired
// difference, is read off the same resamples, as upstream's (seed 0 for
// every metric) are.
//
// Upstream draws 1,000 resamples for A_set and the paired contrasts and 200
// for the balanced score; DefaultBootstrap uses 1,000 for everything.
type Bootstrap struct {
	Resamples int
	Seed      uint64
}

var DefaultBootstrap = Bootstrap{Resamples: 1000, Seed: 0}

// Estimate is a point value with its 95% CI. Value is nil when the metric is
// undefined on the full sample; CI is nil when no resample defines it or
// resampling is off.
type Estimate struct {
	Value *float64    `json:"value"`
	CI    *[2]float64 `json:"ci,omitempty"`
}

// clusters maps each item to its game cluster (Item.Row), clusters numbered
// by first appearance in item order.
func clusters(items []Item) ([]int, int) {
	idx := make(map[int]int)
	out := make([]int, len(items))
	for i, it := range items {
		c, ok := idx[it.Row]
		if !ok {
			c = len(idx)
			idx[it.Row] = c
		}
		out[i] = c
	}
	return out, len(idx)
}

// resample calls fn once per resample with each cluster's draw count.
func (b Bootstrap) resample(nClusters int, fn func(counts []int)) {
	rng := newPyRandom(b.Seed)
	counts := make([]int, nClusters)
	for r := 0; r < b.Resamples; r++ {
		clear(counts)
		for i := 0; i < nClusters; i++ {
			counts[rng.below(nClusters)]++
		}
		fn(counts)
	}
}

func percentileCI(vals []float64) *[2]float64 {
	if len(vals) == 0 {
		return nil
	}
	sort.Float64s(vals)
	lo := int(0.025 * float64(len(vals)))
	hi := int(0.975*float64(len(vals))) - 1
	if hi < 0 {
		hi = 0
	}
	return &[2]float64{vals[lo], vals[hi]}
}

func ptr(v float64) *float64 { return &v }

func scoreRun(run Run, cl []int) []scored {
	out := make([]scored, len(run.Items))
	for i, it := range run.Items {
		out[i] = scoreItem(it, run.Results[i], cl[i])
	}
	return out
}

func weighted(s []scored, counts []int) metricVec {
	var t tally
	for _, x := range s {
		w := 1
		if counts != nil {
			w = counts[x.cluster]
		}
		t.add(x, w)
	}
	return t.metrics()
}

// Estimates scores one run and bootstraps every metric.
func (b Bootstrap) Estimates(run Run) map[string]Estimate {
	cl, nc := clusters(run.Items)
	s := scoreRun(run, cl)
	point := weighted(s, nil)
	var vals [numMetrics][]float64
	b.resample(nc, func(counts []int) {
		m := weighted(s, counts)
		for k := 0; k < numMetrics; k++ {
			if m.ok[k] {
				vals[k] = append(vals[k], m.v[k])
			}
		}
	})
	out := make(map[string]Estimate, numMetrics)
	for k := 0; k < numMetrics; k++ {
		var e Estimate
		if point.ok[k] {
			e.Value = ptr(point.v[k])
		}
		e.CI = percentileCI(vals[k])
		out[MetricNames[k]] = e
	}
	return out
}

// sameItems checks that two runs are over the same items in the same order.
func sameItems(a, b Run) error {
	if len(a.Items) != len(b.Items) {
		return fmt.Errorf("searchbench: runs have %d and %d items", len(a.Items), len(b.Items))
	}
	for i := range a.Items {
		if a.Items[i].ID != b.Items[i].ID {
			return errors.New("searchbench: runs are not over the same items")
		}
	}
	return nil
}

// Paired is a − b for every metric on the same items, with the CI read off
// the same resampled games for both runs (analyze.py paired()), plus the
// share of items on which the two runs chose the same option (diagnose.py
// §5), overall ("same_choice") and per type ("same_choice_<type>").
func (b Bootstrap) Paired(a, bRun Run) (map[string]Estimate, error) {
	if err := sameItems(a, bRun); err != nil {
		return nil, err
	}
	cl, nc := clusters(a.Items)
	sa, sb := scoreRun(a, cl), scoreRun(bRun, cl)
	same := func(counts []int) (v [5]float64, ok [5]bool) {
		var num, den [5]int
		for i := range sa {
			w := 1
			if counts != nil {
				w = counts[sa[i].cluster]
			}
			for _, k := range [2]int{0, 1 + sa[i].typ} {
				den[k] += w
				if sa[i].choice == sb[i].choice {
					num[k] += w
				}
			}
		}
		for k := range den {
			if den[k] > 0 {
				v[k], ok[k] = float64(num[k])/float64(den[k]), true
			}
		}
		return v, ok
	}
	diff := func(counts []int) metricVec {
		ma, mb := weighted(sa, counts), weighted(sb, counts)
		var d metricVec
		for k := 0; k < numMetrics; k++ {
			if ma.ok[k] && mb.ok[k] {
				d.v[k], d.ok[k] = ma.v[k]-mb.v[k], true
			}
		}
		return d
	}
	point := diff(nil)
	pointSame, pointSameOK := same(nil)
	var vals [numMetrics][]float64
	var sameVals [5][]float64
	b.resample(nc, func(counts []int) {
		d := diff(counts)
		for k := 0; k < numMetrics; k++ {
			if d.ok[k] {
				vals[k] = append(vals[k], d.v[k])
			}
		}
		v, ok := same(counts)
		for k := range v {
			if ok[k] {
				sameVals[k] = append(sameVals[k], v[k])
			}
		}
	})
	out := make(map[string]Estimate, numMetrics+5)
	for k := 0; k < numMetrics; k++ {
		var e Estimate
		if point.ok[k] {
			e.Value = ptr(point.v[k])
		}
		e.CI = percentileCI(vals[k])
		out[MetricNames[k]] = e
	}
	for k, name := range [5]string{"same_choice", "same_choice_spell", "same_choice_hold", "same_choice_attack", "same_choice_block"} {
		var e Estimate
		if pointSameOK[k] {
			e.Value = ptr(pointSame[k])
		}
		e.CI = percentileCI(sameVals[k])
		out[name] = e
	}
	return out, nil
}

// median is diagnose.py's gaps[k // 2]: the upper median for an even count.
func median(sorted []float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	return sorted[len(sorted)/2]
}
