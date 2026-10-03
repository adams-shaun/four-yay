package main

// tape.go is cardfuzz's dual-run harness for the W3 resolution kernel
// (rules/resolve; lasagna spec §7.7): -tape plays every game on the kernel
// and verifies its replay on the kernel, -tape-worlds forks a
// redealt hypothetical world at every posed tape decision, and the
// predicate-miss census classes every resolution the ask-free predicate
// exempted that asked anyway (the census ratchet is TestTapeMissCensus).

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/state"
)

// tapeDual is -tape; tapeWorlds is -tape-worlds.
var tapeDual, tapeWorlds bool

var tapeWorldCount, tapeWorldSteps, tapeWorldErrs atomic.Int64

// missCensus counts predicate misses by class (resolving shape -> ask).
type missCensus struct {
	mu sync.Mutex
	by map[string]int
}

func (m *missCensus) add(class string) {
	m.mu.Lock()
	if m.by == nil {
		m.by = map[string]int{}
	}
	m.by[class]++
	m.mu.Unlock()
}

// rows is the census sorted by count, then class.
func (m *missCensus) rows() []missRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]missRow, 0, len(m.by))
	for k, v := range m.by {
		out = append(out, missRow{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].class < out[j].class
	})
	return out
}

type missRow struct {
	class string
	n     int
}

var tapeMisses, tapeLegacy missCensus

// startTapeCensus installs the process-wide predicate-miss and legacy-ask
// observers.
func startTapeCensus() {
	rules.SetTapeMissObserver(tapeMisses.add)
	rules.SetTapeLegacyObserver(tapeLegacy.add)
}

// printTapeReport prints the kernel counters, the world probe's counts and
// the miss census.
func printTapeReport() {
	fmt.Printf("== resolution kernel ==\n%+v\n", resolve.ReadStats())
	if tapeWorlds {
		fmt.Printf("tape worlds: %d forked at posed tape decisions, %d world intents, %d worlds with no accepted intent\n",
			tapeWorldCount.Load(), tapeWorldSteps.Load(), tapeWorldErrs.Load())
	}
	rows := tapeMisses.rows()
	total := 0
	for _, r := range rows {
		total += r.n
	}
	fmt.Printf("== predicate miss census: %d misses in %d classes ==\n", total, len(rows))
	for _, r := range rows {
		fmt.Printf("%6d  %s\n", r.n, r.class)
	}
	rows = tapeLegacy.rows()
	total = 0
	for _, r := range rows {
		total += r.n
	}
	fmt.Printf("== legacy asks ending a tape run: %d in %d classes ==\n", total, len(rows))
	for _, r := range rows {
		fmt.Printf("%6d  %s\n", r.n, r.class)
	}
}

// tapeWorldProbe forks a hypothetical world at a posed tape decision,
// redeals every other seat's hand and library through Secret events (the
// searchprobe redealer's mechanism) and drives it a few random intents
// (spec §7.3 at scale). A prefix divergence panics.
func tapeWorldProbe(e *rules.Engine, r *rand.Rand) {
	d := e.Pending()
	w := e.CloneHypothetical(r.Uint64())
	for p := range w.G.Players {
		pid := state.PlayerID(p)
		if pid == d.Player {
			continue
		}
		hand := append([]state.ObjID(nil), w.G.Zone(state.ZHand, pid)...)
		pool := append(append([]state.ObjID(nil), hand...), w.G.Zone(state.ZLibrary, pid)...)
		r.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
		for _, id := range hand {
			events.Emit(w.G, w.L, events.Event{Kind: events.MoveZone, Player: pid, Obj: id, From: state.ZHand, To: state.ZLibrary, Secret: true})
		}
		for _, id := range pool[:len(hand)] {
			events.Emit(w.G, w.L, events.Event{Kind: events.MoveZone, Player: pid, Obj: id, From: state.ZLibrary, To: state.ZHand, Secret: true})
		}
		events.Emit(w.G, w.L, events.Event{Kind: events.LibraryOrder, Player: pid, IDs: pool[len(hand):], Secret: true})
	}
	tapeWorldCount.Add(1)
	for i := 0; i < 6 && !w.G.Over && w.Pending() != nil; i++ {
		wd := w.Pending()
		ok := false
		for try := 0; try < 8 && !ok; try++ {
			in := decision.Intent{Seq: wd.Seq, Player: wd.Player}
			if len(wd.Options) > 0 {
				n := min(max(wd.Min, 1), len(wd.Options))
				in.Choices = append(in.Choices, r.Perm(len(wd.Options))[:n]...)
			}
			ok = w.SubmitHypothetical(in) == nil
		}
		if !ok {
			tapeWorldErrs.Add(1)
			return
		}
		tapeWorldSteps.Add(1)
	}
}

// tapeDualContext prints the recorded (kernel) and replayed (legacy) events
// around a dual-run divergence at seq.
func tapeDualContext(tape, legacy *rules.Engine, seq int) string {
	var b strings.Builder
	line := func(ev events.Event) string {
		name := ""
		if o := tape.G.Obj(ev.Obj); o != nil && o.Card != nil && len(o.Card.Faces) > 0 {
			name = o.Card.Faces[0].Name
		}
		return fmt.Sprintf("%v obj=%d(%s) p=%d amt=%d ctr=%q text=%q ids=%v from=%v to=%v", ev.Kind, ev.Obj, name, ev.Player, ev.Amount, ev.Counter, ev.Text, ev.IDs, ev.From, ev.To)
	}
	for i := max(seq-30, 0); i < seq+6; i++ {
		t, l := "-", "-"
		if i < len(tape.L.Events) {
			t = line(tape.L.Events[i])
		}
		if i < len(legacy.L.Events) {
			l = line(legacy.L.Events[i])
		}
		mark := "  "
		if t != l {
			mark = "!!"
		}
		fmt.Fprintf(&b, "%s %d\n   tape   %s\n   legacy %s\n", mark, i, t, l)
	}
	return b.String()
}

// tapeLockstep replays log's intents on a kernel and a legacy engine in
// lockstep and reports the first intent after which their pending decisions
// differ, with the kernel's activity per intent up to there.
func tapeLockstep(cfg rules.Config, log *events.Log) string {
	tcfg, lcfg := cfg, cfg
	tcfg.TapeKernel, lcfg.TapeKernel = true, false
	te, le := rules.New(tcfg), rules.New(lcfg)
	te.Advance()
	le.Advance()
	optStr := func(e *rules.Engine) string {
		d := e.Pending()
		if d == nil {
			return "<nil>"
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "%s p%d min%d max%d:", d.Kind, d.Player, d.Min, d.Max)
		for _, o := range d.Options {
			fmt.Fprintf(&sb, " [%s %d %q]", o.Kind, o.Obj, o.Label)
		}
		return sb.String()
	}
	var hist []string
	for i, in := range log.Intents {
		before := resolve.ReadStats()
		posedBefore := rules.TapePosed(te)
		terr, lerr := te.Submit(in), le.Submit(in)
		st := resolve.ReadStats().Sub(before)
		if st.Checkpoints != 0 || st.Reruns != 0 {
			hist = append(hist, fmt.Sprintf("intent %d (%v): posedBefore=%v posedAfter=%v %+v",
				i, in.Choices, posedBefore, rules.TapePosed(te), st))
		}
		if (terr == nil) != (lerr == nil) || optStr(te) != optStr(le) || len(te.L.Events) != len(le.L.Events) {
			if n := len(hist); n > 12 {
				hist = hist[n-12:]
			}
			return fmt.Sprintf("LOCKSTEP: first difference after intent %d (tape err %v, legacy err %v)\n tape   %s\n legacy %s\n%s",
				i, terr, lerr, optStr(te), optStr(le), strings.Join(hist, "\n"))
		}
	}
	return "LOCKSTEP: no decision difference"
}
