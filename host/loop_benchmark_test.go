package host

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// loopBenchmarkGame builds the real Miner line in a hosted match. The fixture
// setup is outside the measured interval; all loop decisions use Pending and
// SubmitIntent, and the subscribed focus session receives ordinary fan-out.
type loopBenchmarkGame struct {
	r                    *Registry
	id                   TableID
	m                    *match
	altar, miner, rakdos state.ObjID
	pilot                *HumanSeat
	session              *Session
	frames               atomic.Int64
}

func newLoopBenchmarkGame(tb testing.TB) *loopBenchmarkGame {
	tb.Helper()
	reg := testutil.CorpusRegistry(tb)
	byName := make(map[string]*cards.Card)
	for _, name := range []string{"Rakdos, the Muscle", "Forsaken Miner", "Phyrexian Altar", "Mountain"} {
		c, ok := reg.Lookup(name)
		if !ok {
			tb.Fatalf("missing corpus card %q", name)
		}
		byName[name] = c
	}
	pilotDeck := make([]*cards.Card, 200)
	oppDeck := make([]*cards.Card, 140)
	for i := range pilotDeck {
		pilotDeck[i] = byName["Mountain"]
	}
	for i := range oppDeck {
		oppDeck[i] = byName["Mountain"]
	}
	pilotDeck = append(pilotDeck, byName["Rakdos, the Muscle"], byName["Forsaken Miner"], byName["Phyrexian Altar"])
	load := func(n string) (Deck, error) {
		switch n {
		case "pilot":
			return Deck{Name: n, Cards: pilotDeck}, nil
		case "opponent":
			return Deck{Name: n, Cards: oppDeck}, nil
		default:
			return Deck{}, ErrNotFound
		}
	}
	r, err := New(Options{LoadDeck: load, Tokens: reg.Tokens, Sleep: func(time.Duration, <-chan struct{}) {}, Ring: 4096,
		Seats: func(_ []string, seed uint64) []seat.Seat { return []seat.Seat{NewHumanSeat(), seat.NewBot(seed ^ 1)} }})
	if err != nil {
		tb.Fatal(err)
	}
	cfg := TableConfig{ID: "loop", Name: "host-loop-benchmark", Seats: 2, Decks: []string{"opponent", "pilot"}, Seed: 77, Pace: 0, Spectator: view.Public, Humans: []int{0}}
	if err := r.AddTable(cfg); err != nil {
		r.Close()
		tb.Fatal(err)
	}
	if err := r.Start(cfg.ID); err != nil {
		r.Close()
		tb.Fatal(err)
	}
	// Wait until the initial human decision is parked, then subscribe before
	// any measured decision so every subsequent burst is projected/fanned out.
	var d *decision.Decision
	deadline := time.Now().Add(10 * time.Second)
	for d == nil {
		d, _ = r.Pending(cfg.ID, 1, 0)
		if time.Now().After(deadline) {
			r.Close()
			tb.Fatal("initial decision did not park")
		}
		time.Sleep(time.Millisecond)
	}
	s := r.OpenSession()
	if err := r.Subscribe(s, cfg.ID, protocol.ModeFocus); err != nil {
		r.Close()
		tb.Fatal(err)
	}
	// The game is at its opening decision. Let the human choose/pass until
	// turn-one priority is active, then seed the three real cards onto the
	// battlefield through the event fold before beginning the loop workload.
	last := ^uint64(0)
	atPilotPriority := false
	for i := 0; i < 20; i++ {
		d = waitLoopPending(tb, r, cfg.ID)
		if d.Seq == last {
			time.Sleep(time.Millisecond)
			i--
			continue
		}
		if d.Kind == decision.KPriority && d.Player == 0 {
			atPilotPriority = true
			break
		}
		_ = r.SubmitIntent(cfg.ID, 1, 0, firstLegal(d))
		last = d.Seq
	}
	if !atPilotPriority {
		r.Close()
		tb.Fatal("did not reach pilot priority before fixture setup")
	}
	r.mu.RLock()
	tab := r.tables[cfg.ID]
	r.mu.RUnlock()
	tab.mu.RLock()
	m := tab.cur
	tab.mu.RUnlock()
	if m == nil {
		r.Close()
		tb.Fatal("missing live match")
	}
	m.mu.Lock()
	var altar, miner, rakdos state.ObjID
	for i := range m.e.G.Objs {
		o := &m.e.G.Objs[i]
		if o.Owner != 0 || o.Zone == state.ZCeased || o.Card == nil || len(o.Card.Faces) == 0 {
			continue
		}
		switch o.Card.Faces[0].Name {
		case "Phyrexian Altar":
			altar = o.ID
		case "Forsaken Miner":
			miner = o.ID
		case "Rakdos, the Muscle":
			rakdos = o.ID
		}
	}
	// Move all three cards using normal event application. The opening
	// priority is then answered once to force the engine to rebuild options.
	for _, name := range []string{"Rakdos, the Muscle", "Forsaken Miner", "Phyrexian Altar"} {
		id := findOwnerCard(m.e.G, 0, name)
		if id == 0 {
			var objs []string
			for i := range m.e.G.Objs {
				o := &m.e.G.Objs[i]
				if o.Card != nil && len(o.Card.Faces) > 0 {
					objs = append(objs, fmt.Sprintf("owner=%d zone=%s name=%s", o.Owner, o.Zone, o.Card.Faces[0].Name))
				}
			}
			m.mu.Unlock()
			r.Close()
			tb.Fatalf("combo card %q not found in game objects: %v", name, objs[:min(8, len(objs))])
		}
		from := m.e.G.Obj(id).Zone
		ev := m.e.L.Append(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZBattlefield})
		events.Apply(m.e.G, ev)
	}
	m.e.L.Append(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 1})
	events.Apply(m.e.G, m.e.L.Events[len(m.e.L.Events)-1])
	m.mu.Unlock()
	g := &loopBenchmarkGame{r: r, id: cfg.ID, m: m, altar: altar, miner: miner, rakdos: rakdos, pilot: m.slots[0].(*HumanSeat), session: s}
	go func() {
		for range s.Out() {
			g.frames.Add(1)
		}
	}()
	tb.Cleanup(func() { r.Close() })
	return g
}

func waitLoopPending(tb testing.TB, r *Registry, id TableID) *decision.Decision {
	tb.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if d, err := r.Pending(id, 1, 0); err == nil {
			return d
		}
		if time.Now().After(deadline) {
			tb.Fatal("no pending host-loop decision")
		}
		time.Sleep(time.Millisecond)
	}
}

func waitFanoutFrames(t *testing.T, g *loopBenchmarkGame, want int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for g.frames.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("subscribed session received %d frames, want at least %d", g.frames.Load(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func findOwnerCard(g *state.Game, p state.PlayerID, name string) state.ObjID {
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Owner == p && o.Zone != state.ZCeased && o.Card != nil && len(o.Card.Faces) > 0 && o.Card.Faces[0].Name == name {
			return o.ID
		}
	}
	return 0
}

func firstLegal(d *decision.Decision) decision.Intent {
	n := d.Min
	if n > len(d.Options) {
		n = len(d.Options)
	}
	choices := make([]int, n)
	for i := 0; i < n; i++ {
		choices[i] = d.Options[i].Index
	}
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
}

func (g *loopBenchmarkGame) runIterations(tb testing.TB, n int) (int, int, int) {
	tb.Helper()
	g.m.mu.RLock()
	startLib := len(g.m.e.G.Zone(state.ZLibrary, 1))
	g.m.mu.RUnlock()
	decisions := 0
	last := ^uint64(0)
	// steps bounds the driven decision count so a stalled line fails loudly
	// (via tb.Fatalf) instead of looping forever on an unchanged Seq. The
	// Miner cycle takes a handful of decisions per iteration, so the cap is
	// generous while still catching a broken driver.
	steps := 0
	stepCap := 40*n + 200
	for {
		if steps >= stepCap {
			tb.Fatalf("loop line stalled: %d decisions without reaching %d library shrink", steps, min(n, startLib))
		}
		g.m.mu.RLock()
		remaining, over := len(g.m.e.G.Zone(state.ZLibrary, 1)), g.m.e.G.Over
		g.m.mu.RUnlock()
		if remaining <= startLib-min(n, startLib) || over {
			break
		}
		d := waitLoopPending(tb, g.r, g.id)
		steps++
		if d.Seq == last {
			time.Sleep(time.Millisecond)
			continue
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Obj == g.altar && o.Kind == "activate" {
					idx = o.Index
					break
				}
			}
			if idx < 0 {
				for _, o := range d.Options {
					if o.Kind == "pass" {
						idx = o.Index
						break
					}
				}
			}
			if idx >= 0 {
				in.Choices = []int{idx}
			} else {
				in = firstLegal(d)
			}
		case decision.KChoose:
			for _, o := range d.Options {
				if o.Obj == g.miner {
					in.Choices = []int{o.Index}
					break
				}
			}
			if len(in.Choices) == 0 {
				in = firstLegal(d)
			}
		case decision.KTarget:
			for _, o := range d.Options {
				if o.Kind == "player" && o.Player == 1 {
					in.Choices = []int{o.Index}
					break
				}
			}
			if len(in.Choices) == 0 {
				in = firstLegal(d)
			}
		default:
			in = firstLegal(d)
		}
		tb.Logf("DBG kind=%s player=%d prompt=%q choices=%v altar=%d miner=%d", d.Kind, d.Player, d.Prompt, in.Choices, g.altar, g.miner)
		if err := g.r.SubmitIntent(g.id, 1, 0, in); err != nil {
			tb.Fatalf("submit %s: %v", d.Kind, err)
		}
		last = d.Seq
		decisions++
	}
	g.m.mu.RLock()
	shrunk := startLib - len(g.m.e.G.Zone(state.ZLibrary, 1))
	g.m.mu.RUnlock()
	return shrunk, decisions, startLib
}

func TestHostLoopRepeat(t *testing.T) {
	for _, n := range []int{20, 100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			g := newLoopBenchmarkGame(t)
			g.m.mu.RLock()
			for _, id := range []state.ObjID{g.rakdos, g.miner, g.altar} {
				if id == 0 || g.m.e.G.Obj(id).Zone != state.ZBattlefield {
					g.m.mu.RUnlock()
					t.Fatalf("combo precondition: object %d is not on battlefield", id)
				}
			}
			initial := len(g.m.e.G.Zone(state.ZLibrary, 1))
			g.m.mu.RUnlock()
			waitFanoutFrames(t, g, 1) // Subscribe's initial snapshot was delivered.
			beforeFanout := g.frames.Load()
			got, _, _ := g.runIterations(t, n)
			want := min(n, initial)
			if got != want {
				t.Fatalf("opponent library shrank %d, want %d (bounded by initial library %d)", got, want, initial)
			}
			waitFanoutFrames(t, g, beforeFanout+1)
			if gotFrames := g.frames.Load(); gotFrames <= beforeFanout {
				t.Fatalf("subscribed session received no loop fan-out: before=%d after=%d", beforeFanout, gotFrames)
			}
		})
	}
}

func BenchmarkHostLoopRepeat(b *testing.B) {
	for _, n := range []int{20, 100} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			var totalDecisions int
			var measured time.Duration
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				g := newLoopBenchmarkGame(b)
				b.StartTimer()
				start := time.Now()
				_, d, _ := g.runIterations(b, n)
				measured += time.Since(start)
				totalDecisions += d
				b.StopTimer()
				g.r.Close()
			}
			if totalDecisions > 0 {
				b.ReportMetric(float64(measured.Nanoseconds())/float64(totalDecisions), "ns/decision")
			}
		})
	}
}
