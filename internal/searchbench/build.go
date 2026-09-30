package searchbench

// The item-selection loop of upstream's tools/search_bench/items.py
// cmd_build, ported to Go and made sequential: games in the prep games
// file's order, each game's candidates in work()'s order, at most two items
// per game, a turn used once for its non-block item and once for its block
// item, and the need(split, kind) quotas; the build stops when every quota
// is met.
//
// Upstream runs cmd_build's work() on a thread pool, so which items fill a
// quota depends on thread timing. Here the loop is the single-threaded
// reading of it (workers=1), whatever the evaluator's parallelism: an
// evaluation is a pure function of (game, candidate), evaluations are
// computed ahead speculatively on the workers, and the loop consumes them
// in order, computing any it needs that speculation skipped.

import (
	"fmt"
	"sort"
	"sync"
)

// BuildGame is one game of the prep games file.
type BuildGame struct {
	Index      int
	Row        int
	Split      Split
	Partner    *int
	Candidates []BuildCandidate
	// DraftID is the game's draft digest (never the raw 17lands id).
	DraftID string
}

// BuildCandidate is one (user turn, kind) of a game.
type BuildCandidate struct {
	Turn int
	Kind DecisionType
}

// Outcome is one evaluated candidate: an item, or a rejection.
type Outcome struct {
	Item *StoreItem
	// Side is "python" (prep.py's Rejected or exception) or "gorge".
	Side string
	// Key is the rejection's summary text (without the "<kind>: " prefix);
	// Detail its full text.
	Key, Detail string
	// Seconds per stage (not deterministic; build.json only).
	PythonSeconds, GorgeSeconds float64
}

// Evaluator evaluates a candidate on a worker. Calls for one game always
// name the game's worker; the worker serialises them.
type Evaluator func(worker int, g *BuildGame, c BuildCandidate) (Outcome, error)

// Rejection is one rejection-ledger row, in consumption order.
type Rejection struct {
	Row    int          `json:"row"`
	Turn   int          `json:"turn"`
	Kind   DecisionType `json:"kind"`
	Split  Split        `json:"split"`
	Side   string       `json:"side"`
	Key    string       `json:"key"`
	Detail string       `json:"detail,omitempty"`
}

// Summary is the rejection's upstream summary key, "<kind>: <reason[:60]>".
func (r Rejection) Summary() string {
	k := r.Key
	if len(k) > 60 {
		k = k[:60]
	}
	return string(r.Kind) + ": " + k
}

// BuildConfig parameterises Build.
type BuildConfig struct {
	Quota      map[Split]map[DecisionType]int
	MaxPerGame int
	Workers    int
	// Window is how many games ahead of the loop the workers may evaluate
	// (0: 2 x Workers).
	Window int
	// Progress, when set, is called after each consumed game.
	Progress func(gamesDone int, items int, counts map[Split]map[DecisionType]int)
}

// BuildResult is a finished selection.
type BuildResult struct {
	Items       []*StoreItem
	Counts      map[Split]map[DecisionType]int
	Rejections  []Rejection
	GamesTotal  int
	GamesUsed   int // games the loop consumed before the quotas were met
	Evaluations int // candidates the loop consumed (items + rejections)
	Speculative int // evaluations computed and not consumed
	Done        bool
	// PythonSeconds and GorgeSeconds sum over consumed evaluations.
	PythonSeconds, GorgeSeconds float64
}

type gameSlot struct {
	worker int
	ready  chan struct{}
	mu     sync.Mutex
	out    map[BuildCandidate]Outcome
	err    error
}

type buildState struct {
	mu     sync.Mutex
	counts map[Split]map[DecisionType]int
	quota  map[Split]map[DecisionType]int
	cursor int // the game the loop is consuming
}

func (s *buildState) need(split Split, kind DecisionType) bool {
	return s.counts[split][kind] < s.quota[split][kind]
}

func (s *buildState) done() bool {
	for sp, q := range s.quota {
		for k := range q {
			if s.need(sp, k) {
				return false
			}
		}
	}
	return true
}

// Build runs the selection loop over games with eval.
func Build(games []BuildGame, cfg BuildConfig, eval Evaluator) (*BuildResult, error) {
	if cfg.MaxPerGame <= 0 {
		cfg.MaxPerGame = 2
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.Window <= 0 {
		cfg.Window = 2 * cfg.Workers
	}
	st := &buildState{quota: cfg.Quota, counts: map[Split]map[DecisionType]int{}}
	for sp := range cfg.Quota {
		st.counts[sp] = map[DecisionType]int{}
	}
	slots := make([]*gameSlot, len(games))
	for i := range slots {
		slots[i] = &gameSlot{worker: -1, ready: make(chan struct{}), out: map[BuildCandidate]Outcome{}}
	}
	// Speculation: each worker takes the next game within the window and
	// evaluates the candidates the loop is likely to consume, under the
	// loop's rule with the quota snapshot (quotas only fill, so a kind full
	// now is full when the loop gets there).
	var next int
	var nextMu sync.Mutex
	cond := sync.NewCond(&st.mu)
	stop := false
	var wg sync.WaitGroup
	for w := 0; w < cfg.Workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for {
				st.mu.Lock()
				nextMu.Lock()
				for !stop && next < len(games) && next >= st.cursor+cfg.Window {
					nextMu.Unlock()
					cond.Wait()
					nextMu.Lock()
				}
				if stop || next >= len(games) {
					nextMu.Unlock()
					st.mu.Unlock()
					return
				}
				gi := next
				next++
				nextMu.Unlock()
				st.mu.Unlock()
				slot := slots[gi]
				slot.mu.Lock()
				slot.worker = w
				slot.mu.Unlock()
				speculate(st, &games[gi], slot, w, cfg.MaxPerGame, eval, &stop)
				close(slot.ready)
			}
		}(w)
	}
	finish := func() {
		st.mu.Lock()
		stop = true
		cond.Broadcast()
		st.mu.Unlock()
		wg.Wait()
	}
	res := &BuildResult{GamesTotal: len(games)}
	draftSplit := map[string]Split{} // lookup only
	for gi := range games {
		st.mu.Lock()
		st.cursor = gi
		cond.Broadcast()
		d := st.done()
		st.mu.Unlock()
		if d {
			break
		}
		g := &games[gi]
		slot := slots[gi]
		<-slot.ready
		if slot.err != nil {
			finish()
			return nil, slot.err
		}
		res.GamesUsed++
		got := 0
		used := map[[2]int]bool{} // lookup only: (turn, block)
		for _, c := range g.Candidates {
			st.mu.Lock()
			d := st.done()
			skip := !st.need(g.Split, c.Kind)
			st.mu.Unlock()
			if got >= cfg.MaxPerGame || d {
				break
			}
			ut := [2]int{c.Turn, b2i(c.Kind == DecisionBlock)}
			if skip || used[ut] {
				continue
			}
			slot.mu.Lock()
			o, ok := slot.out[c]
			delete(slot.out, c)
			slot.mu.Unlock()
			if !ok {
				var err error
				if o, err = eval(slot.worker, g, c); err != nil {
					finish()
					return nil, fmt.Errorf("searchbench: row %d turn %d %s: %w", g.Row, c.Turn, c.Kind, err)
				}
			}
			res.Evaluations++
			res.PythonSeconds += o.PythonSeconds
			res.GorgeSeconds += o.GorgeSeconds
			if o.Item == nil {
				res.Rejections = append(res.Rejections, Rejection{Row: g.Row, Turn: c.Turn, Kind: c.Kind, Split: g.Split, Side: o.Side, Key: o.Key, Detail: o.Detail})
				continue
			}
			// gorge's manifest keeps a draft in one split (Manifest.validate);
			// upstream splits by game, so a draft whose other game already
			// gave an item to the other split is refused here.
			if sp, ok := draftSplit[g.DraftID]; ok && g.DraftID != "" && sp != g.Split {
				res.Rejections = append(res.Rejections, Rejection{Row: g.Row, Turn: c.Turn, Kind: c.Kind, Split: g.Split, Side: "gorge", Key: "draft crosses split", Detail: "draft " + g.DraftID + " has a " + string(sp) + " item"})
				continue
			}
			st.mu.Lock()
			if !st.need(g.Split, c.Kind) {
				st.mu.Unlock()
				continue
			}
			st.counts[g.Split][c.Kind]++
			st.mu.Unlock()
			draftSplit[g.DraftID] = g.Split
			res.Items = append(res.Items, o.Item)
			got++
			used[ut] = true
		}
		slot.mu.Lock()
		res.Speculative += len(slot.out)
		slot.out = nil
		slot.mu.Unlock()
		if cfg.Progress != nil {
			st.mu.Lock()
			cfg.Progress(gi+1, len(res.Items), st.counts)
			st.mu.Unlock()
		}
	}
	finish()
	// Speculated games past the stop point.
	for _, s := range slots {
		s.mu.Lock()
		res.Speculative += len(s.out)
		s.mu.Unlock()
	}
	st.mu.Lock()
	res.Counts = st.counts
	res.Done = st.done()
	st.mu.Unlock()
	// Upstream's stable ids: by split, type, row, turn.
	sort.SliceStable(res.Items, func(i, j int) bool {
		a, b := res.Items[i], res.Items[j]
		if a.Split != b.Split {
			return a.Split < b.Split
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Row != b.Row {
			return a.Row < b.Row
		}
		return a.Turn < b.Turn
	})
	return res, nil
}

// AssignIDs names the sorted items "<version>-<split>-%04d" by their global
// index, as upstream does.
func AssignIDs(items []*StoreItem, version string) {
	for i, it := range items {
		it.ID = fmt.Sprintf("%s-%s-%04d", version, it.Split, i)
	}
}

func speculate(st *buildState, g *BuildGame, slot *gameSlot, w, maxPerGame int, eval Evaluator, stop *bool) {
	got := 0
	used := map[[2]int]bool{} // lookup only
	for _, c := range g.Candidates {
		st.mu.Lock()
		halt := *stop || st.done()
		full := !st.need(g.Split, c.Kind)
		st.mu.Unlock()
		if got >= maxPerGame || halt {
			return
		}
		ut := [2]int{c.Turn, b2i(c.Kind == DecisionBlock)}
		if full || used[ut] {
			continue
		}
		o, err := eval(w, g, c)
		if err != nil {
			slot.err = fmt.Errorf("searchbench: row %d turn %d %s: %w", g.Row, c.Turn, c.Kind, err)
			return
		}
		slot.mu.Lock()
		slot.out[c] = o
		slot.mu.Unlock()
		if o.Item != nil {
			got++
			used[ut] = true
		}
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// RejectionSummary counts rejections by summary key.
func RejectionSummary(rs []Rejection) map[string]int {
	out := map[string]int{}
	for _, r := range rs {
		out[r.Summary()]++
	}
	return out
}
