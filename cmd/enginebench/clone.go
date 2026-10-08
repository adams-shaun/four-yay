package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

// rootTurns are the mid-game turns docs/015 copied at (turns 5-8).
var rootTurns = []int32{5, 6, 7, 8}

type rootInfo struct {
	Seed       uint64
	Turn       int32
	Kind       string
	Perms      [2]int // permanents per controller
	NonLand    [2]int
	Events     int
	Candidates int // step row: accepted intents cycled through
}

// botStep answers the pending decision with the per-seat heuristic bot and
// submits it.
func botStep(e *rules.Engine, bots []*seat.Bot, board *botpolicy.Board) (decision.Intent, error) {
	d := e.Pending()
	b := botpolicy.BoardFromGameInto(e.G, e, d.Player, board)
	in, err := bots[d.Player].DecideBoard(context.Background(), b, *d)
	if err != nil {
		return in, err
	}
	return in, e.Submit(in)
}

func permanents(g *state.Game, p state.PlayerID) (all, nonland int) {
	for _, id := range g.Zone(state.ZBattlefield, p) {
		o := g.Obj(id)
		if o == nil {
			continue
		}
		all++
		land := false
		if o.Card != nil && len(o.Card.Faces) > 0 {
			for _, t := range o.Card.Faces[0].Types {
				if t == "Land" {
					land = true
				}
			}
		}
		if !land {
			nonland++
		}
	}
	return
}

// choicePoint reports a decision that is more than "tap a land or pass": a
// non-priority decision, or a priority decision offering a land play, a cast
// or any option other than pass, concede and mana activation.
func choicePoint(d *decision.Decision) bool {
	if d.Kind != decision.KPriority {
		return true
	}
	for _, o := range d.Options {
		switch o.Kind {
		case "pass", "concede", "activate":
		default:
			return true
		}
	}
	return false
}

// findRoots plays FDN pair A with the heuristic bot on both seats from seed
// upward and returns, for each of turns 5-8, a clone of the engine at the
// first choice point (choicePoint) of that turn -- taken only from a game in
// which every such root has a nonland permanent on both sides.
func findRoots(w workload, base uint64) ([]*rules.Engine, []rootInfo, error) {
	decks, names, err := w.decks("A")
	if err != nil {
		return nil, nil, err
	}
	for g := 0; g < 64; g++ {
		cfg := w.config(decks, names, base, g)
		e := rules.New(cfg)
		e.Advance()
		bots := []*seat.Bot{seat.NewBot(cfg.Seed ^ 1), seat.NewBot(cfg.Seed ^ 2)}
		board := botpolicy.NewBoard(2)
		var roots []*rules.Engine
		var infos []rootInfo
		next := 0
		good := true
		for !e.G.Over && e.Pending() != nil && next < len(rootTurns) && good {
			if e.G.Turn > rootTurns[next] {
				good = false // turn passed with no real choice point
				break
			}
			if e.G.Turn == rootTurns[next] && choicePoint(e.Pending()) {
				ri := rootInfo{Seed: cfg.Seed, Turn: e.G.Turn, Kind: string(e.Pending().Kind), Events: len(e.L.Events)}
				for p := 0; p < 2; p++ {
					ri.Perms[p], ri.NonLand[p] = permanents(e.G, state.PlayerID(p))
					if ri.NonLand[p] == 0 {
						good = false
					}
				}
				roots = append(roots, e.Clone())
				infos = append(infos, ri)
				next++
				continue
			}
			if _, err := botStep(e, bots, &board); err != nil {
				good = false
			}
		}
		if good && next == len(rootTurns) {
			return roots, infos, nil
		}
	}
	return nil, nil, fmt.Errorf("no pair-A game from seed %d reached turns 5-8 with nonland permanents on both sides", base)
}

// stepCandidates are the intents a one-step search may expand at root: every
// single option a clone accepts (a priority decision), else up to 16
// accepted uniform draws. The timed loop cycles through them.
func stepCandidates(root *rules.Engine) []decision.Intent {
	d := root.Pending()
	var out []decision.Intent
	try := func(in decision.Intent) {
		c := root.Clone()
		if c.Submit(in) == nil {
			out = append(out, in)
		}
	}
	if d.Min == 1 && d.Max == 1 {
		for i := range d.Options {
			if d.Options[i].Kind == "concede" {
				continue
			}
			try(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}})
		}
		return out
	}
	r := rand.New(rand.NewPCG(1, 2))
	var scratch intentScratch
	for i := 0; i < 256 && len(out) < 16; i++ {
		try(randomIntent(d, r, &scratch))
	}
	return out
}

type cloneDetail struct {
	Roots             []rootInfo
	USPer, USPerCPU   []float64
	AllocBPer         []float64
	MallocsPer        []float64
	RetainedPer       []float64
	RetainedN, Copies int
	PerOption         []stepOption `json:",omitempty"`
	USPerCPUWithGC    []float64
	GCsWithGC         []uint32
}

const gcFreeBatch = 250

type measured struct {
	wall, cpu, alloc, mallocs float64
	gcs                       uint32
}

// measure times n calls of op. gcFree runs them in batches of gcFreeBatch
// with the collector off and an untimed forced collection before each batch.
func measure(op func(int), n int, gcFree bool) measured {
	var m measured
	var m0, m1 runtime.MemStats
	runtime.GC()
	for k := 0; k < n; {
		b := n - k
		if gcFree {
			b = min(b, gcFreeBatch)
			runtime.GC()
			debug.SetGCPercent(-1)
		}
		runtime.ReadMemStats(&m0)
		t0, c0 := time.Now(), cpuNow()
		for j := 0; j < b; j++ {
			op(k + j)
		}
		m.wall += time.Since(t0).Seconds()
		m.cpu += cpuNow() - c0
		runtime.ReadMemStats(&m1)
		if gcFree {
			debug.SetGCPercent(100)
		}
		m.alloc += float64(m1.TotalAlloc - m0.TotalAlloc)
		m.mallocs += float64(m1.Mallocs - m0.Mallocs)
		m.gcs += m1.NumGC - m0.NumGC
		k += b
	}
	return m
}

type stepOption struct {
	Root        int
	Kind, Label string
	USPerCPU    float64
}

var sink *rules.Engine

// runClone times Engine.Clone (step=false) or Clone + one Submit (step=true)
// at each root, copies times.
func runClone(r *result, w workload, base uint64, copies int, step bool) error {
	roots, infos, err := findRoots(w, base)
	if err != nil {
		return err
	}
	det := cloneDetail{Roots: infos, Copies: copies}
	var totalEl, totalCPU float64
	for i, root := range roots {
		var cands []decision.Intent
		if step {
			cands = stepCandidates(root)
			det.Roots[i].Candidates = len(cands)
			if len(cands) == 0 {
				return fmt.Errorf("root %d: no accepted intent", i)
			}
		}
		op := func(k int) {
			c := root.Clone()
			if step {
				if err := c.Submit(cands[k%len(cands)]); err != nil {
					panic(err)
				}
			}
			sink = c
		}
		for k := 0; k < copies/10+10; k++ { // warm-up
			op(k)
		}
		// GC-free: batches of gcFreeBatch with the collector off, a forced
		// (untimed) collection between batches -- the copy's own cost.
		m := measure(op, copies, true)
		totalEl += m.wall
		totalCPU += m.cpu
		det.USPer = append(det.USPer, m.wall*1e6/float64(copies))
		det.USPerCPU = append(det.USPerCPU, m.cpu*1e6/float64(copies))
		det.AllocBPer = append(det.AllocBPer, m.alloc/float64(copies))
		det.MallocsPer = append(det.MallocsPer, m.mallocs/float64(copies))
		// GC-amortized: the same copies at the default GOGC, so the
		// collections they cause (each marking the loaded corpus) are paid.
		g := measure(op, copies, false)
		det.USPerCPUWithGC = append(det.USPerCPUWithGC, g.cpu*1e6/float64(copies))
		det.GCsWithGC = append(det.GCsWithGC, g.gcs)
		if step {
			// Per-option breakdown: which expansion the mean is made of.
			d := root.Pending()
			for _, in := range cands {
				o := d.Options[in.Choices[0]]
				const n = 500
				pm := measure(func(int) {
					c := root.Clone()
					if err := c.Submit(in); err != nil {
						panic(err)
					}
					sink = c
				}, n, true)
				det.PerOption = append(det.PerOption, stepOption{Root: i, Kind: o.Kind, Label: o.Label, USPerCPU: pm.cpu * 1e6 / n})
			}
		}
		if !step {
			// Retained: the live heap a kept copy adds (shared immutable
			// structure counted once, in the root).
			const keepN = 500
			keep := make([]*rules.Engine, keepN)
			sink = nil
			runtime.GC()
			runtime.GC()
			var h0, h1 runtime.MemStats
			runtime.ReadMemStats(&h0)
			for k := range keep {
				keep[k] = root.Clone()
			}
			runtime.GC()
			runtime.GC()
			runtime.ReadMemStats(&h1)
			det.RetainedPer = append(det.RetainedPer, (float64(h1.HeapAlloc)-float64(h0.HeapAlloc))/keepN)
			det.RetainedN = keepN
			runtime.KeepAlive(keep)
		}
	}
	r.Secs, r.CPU = totalEl, totalCPU
	r.USPer = mean(det.USPer)
	r.USPerCPU = mean(det.USPerCPU)
	r.USPerCPUGC = mean(det.USPerCPUWithGC)
	r.AllocBPer = mean(det.AllocBPer)
	r.MallocsPer = mean(det.MallocsPer)
	if !step {
		r.RetainedPer = mean(det.RetainedPer)
	} else {
		r.Row = "step"
	}
	r.Roots = len(roots)
	r.Detail = det
	return nil
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}
