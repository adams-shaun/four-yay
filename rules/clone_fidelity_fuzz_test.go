package rules

import (
	"fmt"
	"math/rand/v2"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// The clone-fidelity fuzz (lasagna spec §5 W1a step 5, the W3 / spike S3
// prerequisite): real-deck games are played with the botpolicy bot (plus a
// seeded share of uniform-random intents, for states the bot never makes),
// and at decision points the engine is cloned and three properties are
// checked:
//
//  1. Lockstep: the clone, fed the same next K intents as the original,
//     accepts exactly the intents the original accepts, poses the same
//     decisions and emits the same events (same chain head and RNG position
//     after every intent, same game state at the end of the window). A
//     divergence is a clone bug or a cache bug -- a cold cache in the clone
//     behaving differently from the warm original.
//  2. Isolation: a second clone driven DOWN A DIFFERENT PATH (a different
//     legal intent, then M random ones) leaves the original untouched: its
//     log, head and RNG position are unchanged on return.
//  3. No action at a distance: the original is shadowed by a reference
//     engine built from the same Config and fed the same intents, which is
//     never cloned from. Any write a clone made into shared state shows up,
//     possibly much later, as the original diverging from the reference.
//
// The default run is one short game prefix per seat count with a clone at
// EVERY decision (a few seconds). GORGE_CLONE_FUZZ=N runs the sweep: N whole
// games over every repo deck (60-card and Commander, 2 and 4 seats),
// sampling one decision in GORGE_CLONE_FUZZ_EVERY (default 4; 1 = every
// decision). GORGE_CLONE_FUZZ_RANDOM sets the percentage of main-game
// intents drawn uniformly at random (default 10). GORGE_CLONE_FUZZ_FIRST
// offsets the game index, so a failing game reproduces alone.

// cloneFuzzOpts sizes one fuzz game.
type cloneFuzzOpts struct {
	maxIntents int // stop the game after this many intents (0: play to the end)
	every      int // clone at one decision in every (1: every decision)
	lockstep   int // K: intents a lockstep clone shadows the original for
	diverge    int // M: random intents the divergent clone takes after its first
	randomPct  int // share (percent) of main-game intents drawn uniformly at random
}

// cloneFuzzStats aggregates across parallel games.
type cloneFuzzStats struct {
	games, intents, clones, divergent atomic.Int64
}

// cloneFuzzConstructed and cloneFuzzCommander partition the repo decks: the
// 60-card decks seat any number of players, the Commander decks seat with
// their commander in the command zone at 40 life.
func cloneFuzzDecks(t *testing.T) (constructed, commander []string) {
	for _, n := range testutil.RepoDeckNames() {
		if testutil.RepoDeckFile(t, n).Format == "commander" {
			commander = append(commander, n)
		} else {
			constructed = append(constructed, n)
		}
	}
	return constructed, commander
}

// cloneFuzzConfig seats game g: even games 2 seats, odd games 4; every third
// game is a Commander game. Decks rotate through the pool by g.
func cloneFuzzConfig(t *testing.T, reg *cards.Registry, g int) (Config, string) {
	t.Helper()
	constructed, commander := cloneFuzzDecks(t)
	seats := 2
	if g%2 == 1 {
		seats = 4
	}
	pool := constructed
	cmdr := g%3 == 2 && len(commander) > 0
	if cmdr {
		pool = commander
	}
	cfg := Config{Seed: uint64(1000 + g), Tokens: reg.Tokens, NameUniverse: reg.Cards, Mulligans: 1}
	for i := 0; i < seats; i++ {
		name := pool[(g*7+i*5)%len(pool)]
		cfg.Names = append(cfg.Names, name)
		cfg.Decks = append(cfg.Decks, testutil.RepoDeck(t, reg, name))
		if cmdr {
			cfg.Commanders = append(cfg.Commanders, []int{testutil.RepoDeckFile(t, name).CommanderIndex()})
		}
	}
	if cmdr {
		cfg.Format, cfg.StartingLife = FormatCommander, 40
	}
	return cfg, fmt.Sprintf("game %d seed %d %v", g, cfg.Seed, cfg.Names)
}

// cloneFuzzRandomIntent draws an answer uniformly (choice count uniform in
// [Min, Max], distinct options in random order), never conceding -- the
// enginebench random player's draw.
func cloneFuzzRandomIntent(d *decision.Decision, r *rand.Rand) decision.Intent {
	pool := make([]int, 0, len(d.Options))
	for i := range d.Options {
		if d.Options[i].Kind != "concede" {
			pool = append(pool, i)
		}
	}
	lo, hi := d.Min, d.Max
	if hi > len(pool) {
		hi = len(pool)
	}
	if lo < 0 {
		lo = 0
	}
	if lo > hi {
		lo = hi
	}
	k := lo + r.IntN(hi-lo+1)
	ch := make([]int, k)
	for i, j := range r.Perm(len(pool))[:k] {
		ch[i] = pool[j]
	}
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}
}

// cloneFuzzFirstDiff names the first event at or after from where two logs
// differ.
func cloneFuzzFirstDiff(a, b *events.Log, from int) string {
	n := len(a.Events)
	if len(b.Events) > n {
		n = len(b.Events)
	}
	for i := from; i < n; i++ {
		var ea, eb events.Event
		if i < len(a.Events) {
			ea = a.Events[i]
		}
		if i < len(b.Events) {
			eb = b.Events[i]
		}
		if cloneFuzzDiff(&ea, &eb, "event") != "" {
			return fmt.Sprintf("first differing event #%d (lens %d/%d):\n    want %+v\n    got  %+v", i, len(a.Events), len(b.Events), ea, eb)
		}
	}
	return fmt.Sprintf("no event differs from #%d (lens %d/%d)", from, len(a.Events), len(b.Events))
}

// cloneFuzzSame reports how c differs from want after both took the same
// intents ("" = identical so far).
func cloneFuzzSame(want, c *Engine, from int) string {
	if want.L.Head() != c.L.Head() || len(want.L.Events) != len(c.L.Events) {
		return "event streams differ: " + cloneFuzzFirstDiff(want.L, c.L, from)
	}
	if want.RNGDraws() != c.RNGDraws() {
		return fmt.Sprintf("RNG draws differ: %d vs %d", want.RNGDraws(), c.RNGDraws())
	}
	wd, cd := want.Pending(), c.Pending()
	if (wd == nil) != (cd == nil) {
		return fmt.Sprintf("pending decisions differ:\n    want %+v\n    got  %+v", wd, cd)
	}
	if wd != nil {
		if d := cloneFuzzDiff(wd, cd, "Pending"); d != "" {
			return "pending decisions differ: " + d
		}
	}
	return ""
}

// cloneFuzzDiff names the first difference between two values of the same
// pointer type (clone_policy_test.go's cloneFirstDiff, which treats a nil
// and an empty slice or map as equal -- a copy written as append(T(nil),
// src...) normalises one to the other, and nothing reads the difference).
func cloneFuzzDiff[T any](a, b *T, path string) string {
	return cloneFirstDiff(reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem(), path, map[[2]uintptr]bool{})
}

type cloneFuzzLive struct {
	c     *Engine
	at    int // the original's intent index at the clone
	from  int // the original's event count at the clone
	left  int
	kind  decision.Kind
	trail []string
}

// playCloneFuzzGame plays one game under the three checks.
func playCloneFuzzGame(t *testing.T, cfg Config, label string, o cloneFuzzOpts, st *cloneFuzzStats) {
	t.Helper()
	e, ref := New(cfg), New(cfg)
	e.Advance()
	ref.Advance()
	bot := newTestBot(cfg.Seed ^ 0x51ed)
	r := rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0xc10e))
	var lives []cloneFuzzLive
	// Recycled storage, the search's own loop (c := root.CloneInto(&sp);
	// play c; sp = c.Release()): divergent and probe clones draw from div,
	// and every other lockstep clone from a spare a finished lockstep clone
	// handed back, so dirty recycled arrays are held to the same checks.
	var div Spare
	var spares []Spare
	n := 0
	for ; !e.G.Over && e.Pending() != nil && (o.maxIntents == 0 || n < o.maxIntents); n++ {
		d := e.Pending()
		if cloneFuzzTapeWorldHook != nil {
			cloneFuzzTapeWorldHook(t, e, r)
		}
		if o.every <= 1 || r.IntN(o.every) == 0 {
			st.clones.Add(1)
			var c *Engine
			if k := len(spares); k > 0 && n%2 == 1 {
				c = e.CloneInto(&spares[k-1])
				spares = spares[:k-1]
			} else {
				c = e.Clone()
			}
			lives = append(lives, cloneFuzzLive{c: c, at: n, from: len(e.L.Events), left: o.lockstep, kind: d.Kind})
			if msg := cloneFuzzDiverge(e, r, o.diverge, &div); msg != "" {
				t.Fatalf("%s: intent %d (%s): ISOLATION: %s", label, n, d.Kind, msg)
			}
			st.divergent.Add(1)
		}
		in := bot.answer(e, d)
		if r.IntN(100) < o.randomPct {
			// A uniform draw, probed on a throwaway clone first so the
			// original never sees a refused intent.
			for try := 0; try < 8; try++ {
				ri := cloneFuzzRandomIntent(d, r)
				probe := e.CloneInto(&div)
				ok := probe.Submit(ri) == nil
				div = probe.Release()
				if ok {
					in = ri
					break
				}
			}
		}
		head := e.L.Head()
		if err := e.Submit(in); err != nil {
			t.Fatalf("%s: intent %d (%s): original refused %+v: %v", label, n, d.Kind, in, err)
		}
		if err := ref.Submit(in); err != nil {
			t.Fatalf("%s: intent %d (%s): the never-cloned reference refused %+v (original accepted): %v", label, n, d.Kind, in, err)
		}
		if msg := cloneFuzzSame(ref, e, 0); msg != "" {
			t.Fatalf("%s: intent %d (%s, head before %s): the ORIGINAL diverged from its never-cloned reference (a clone wrote into shared state, or Clone mutated its source): %s",
				label, n, d.Kind, head, msg)
		}
		keep := lives[:0]
		for _, lv := range lives {
			lv.trail = append(lv.trail, fmt.Sprintf("%s%v", d.Kind, in.Choices))
			err := lv.c.Submit(in)
			if err != nil {
				t.Fatalf("%s: clone taken at intent %d (%s): refused intent %d %+v the original accepted: %v (trail %s)",
					label, lv.at, lv.kind, n, in, err, strings.Join(lv.trail, " "))
			}
			if msg := cloneFuzzSame(e, lv.c, lv.from); msg != "" {
				t.Fatalf("%s: clone taken at intent %d (%s): LOCKSTEP diverged after intent %d: %s (trail %s)",
					label, lv.at, lv.kind, n, msg, strings.Join(lv.trail, " "))
			}
			if lv.left--; lv.left > 0 && !e.G.Over {
				keep = append(keep, lv)
				continue
			}
			if diff := cloneFuzzDiff(e.G, lv.c.G, "G"); diff != "" {
				t.Fatalf("%s: clone taken at intent %d (%s): LOCKSTEP game state differs after %d intents: %s",
					label, lv.at, lv.kind, len(lv.trail), diff)
			}
			if len(spares) < 4 {
				spares = append(spares, lv.c.Release())
			}
		}
		lives = keep
	}
	if o.maxIntents == 0 && !e.G.Over {
		t.Fatalf("%s: game did not finish (turn %d, %d intents)", label, e.G.Turn, n)
	}
	st.games.Add(1)
	st.intents.Add(int64(n))
}

// cloneFuzzTapeWorldHook (the resolution kernel's world probe,
// resolve_kernel_test.go) runs at every decision of a fuzz game when set.
var cloneFuzzTapeWorldHook func(t *testing.T, e *Engine, r *rand.Rand)

// cloneFuzzDiverge clones e, drives the clone down a different path (a
// random accepted intent, then up to m more) and reports how e changed.
func cloneFuzzDiverge(e *Engine, r *rand.Rand, m int, sp *Spare) string {
	head, draws, evs, seq := e.L.Head(), e.RNGDraws(), len(e.L.Events), e.Pending().Seq
	c := e.CloneInto(sp)
	for i := 0; i <= m && !c.G.Over && c.Pending() != nil; i++ {
		d := c.Pending()
		for try := 0; try < 8; try++ {
			if c.Submit(cloneFuzzRandomIntent(d, r)) == nil {
				break
			}
		}
	}
	*sp = c.Release()
	switch {
	case e.L.Head() != head || len(e.L.Events) != evs:
		return fmt.Sprintf("driving a divergent clone changed the original's log (head %s -> %s, events %d -> %d)", head, e.L.Head(), evs, len(e.L.Events))
	case e.RNGDraws() != draws:
		return fmt.Sprintf("driving a divergent clone moved the original's RNG (%d -> %d)", draws, e.RNGDraws())
	case e.Pending() == nil || e.Pending().Seq != seq:
		return "driving a divergent clone changed the original's pending decision"
	}
	return ""
}

func cloneFuzzEnvInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

// TestCloneFidelityShort2Seat and TestCloneFidelityShort4Seat are the
// default-run slice: a 2-seat 60-card game and a 4-seat Commander game, each
// cloned at EVERY decision of its first intents. They are two independent
// tests (the operator's per-test budget, 2026-10-05: 2 GB, 2 vCPU, 1 min)
// rather than two subtests of one.
func TestCloneFidelityShort2Seat(t *testing.T) { cloneFidelityShortGame(t, 0) }
func TestCloneFidelityShort4Seat(t *testing.T) { cloneFidelityShortGame(t, 5) }

func cloneFidelityShortGame(t *testing.T, g int) {
	t.Parallel()
	if os.Getenv("GORGE_CLONE_FUZZ") != "" {
		t.Skip("the GORGE_CLONE_FUZZ sweep is running instead")
	}
	reg := testutil.CorpusRegistry(t)
	o := cloneFuzzOpts{maxIntents: 400, every: 1, lockstep: 4, diverge: 4, randomPct: 10}
	var st cloneFuzzStats
	cfg, label := cloneFuzzConfig(t, reg, g)
	playCloneFuzzGame(t, cfg, label, o, &st)
	t.Logf("clone fidelity: %s, %d intents, %d lockstep clones, %d divergent clones",
		label, st.intents.Load(), st.clones.Load(), st.divergent.Load())
}

// TestCloneFidelitySweep is the long sweep (GORGE_CLONE_FUZZ=N games).
func TestCloneFidelitySweep(t *testing.T) {
	games := cloneFuzzEnvInt("GORGE_CLONE_FUZZ", 0)
	if games == 0 {
		t.Skip("set GORGE_CLONE_FUZZ=N to play N whole games under the clone-fidelity checks")
	}
	reg := testutil.CorpusRegistry(t)
	first := cloneFuzzEnvInt("GORGE_CLONE_FUZZ_FIRST", 0)
	o := cloneFuzzOpts{every: cloneFuzzEnvInt("GORGE_CLONE_FUZZ_EVERY", 4), lockstep: 6, diverge: 6,
		randomPct: cloneFuzzEnvInt("GORGE_CLONE_FUZZ_RANDOM", 10)}
	var st cloneFuzzStats
	t.Run("games", func(t *testing.T) {
		for g := first; g < first+games; g++ {
			g := g
			t.Run(strconv.Itoa(g), func(t *testing.T) {
				t.Parallel()
				cfg, label := cloneFuzzConfig(t, reg, g)
				playCloneFuzzGame(t, cfg, label, o, &st)
			})
		}
	})
	t.Logf("clone fidelity sweep: %d games, %d intents, %d lockstep clones, %d divergent clones",
		st.games.Load(), st.intents.Load(), st.clones.Load(), st.divergent.Load())
}
