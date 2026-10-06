// walkstats.go implements enginebench's -walkstats observer: the reuse oracle
// for the priority legal-action walk (docs/superpowers/specs/2026-10-06-legal-walk-design.md
// §1.5, step S0).
//
// At every posed priority decision it compares the offered option list with the
// SAME SEAT's previous priority decision and classifies the kinds of the events
// logged in between. The output is the input to the later steps' kill
// criteria: S4/S5 read how often a seat's consecutive walks see the same state,
// and how much of that is reachable through the engine's existing inert-suffix
// machinery.
//
// It uses only the exported API (Engine.Pending, Engine.L.Events, Engine.G), so
// it lives in the command, not in rules/. The observer's counters are a
// per-process aggregate over every game the row plays (warm-up included); the
// report goes to stderr while the row's JSON line goes to stdout unchanged.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// walkStatsFlag is set from -walkstats in main. When false the hooks and the
// observer cost nothing (playRandom/playBot skip reset and observe).
var walkStatsFlag bool

// prevAsk is one seat's previous priority ask.
type prevAsk struct {
	ok      bool
	opts    string
	ev      int
	sorcery bool
	step    state.Step
	turn    int32
	stack   int
	pools   [8]int32
}

type sigStat struct{ n, same int }

type walkStats struct {
	asks, trans, same          int
	quiet, quietSame           int
	timing, timingSame         int
	timingSorc, timingSorcSame int
	timingPool, timingPoolSame int
	onlyPassOffered            int
	opts, hand, bf             int
	sigs                       map[string]*sigStat
	kindIn, kindInSame         [256]int
	prev                       [8]prevAsk
}

var wm = walkStats{sigs: map[string]*sigStat{}}

func (m *walkStats) reset() { m.prev = [8]prevAsk{} }

// quietKinds are the bookkeeping events every posed priority decision writes:
// the kinds the engine's existing inert-suffix machinery (layerInertSince) can
// prove leave the walk's inputs untouched.
var quietKinds = [256]bool{events.DecisionAsk: true, events.DecisionMade: true, events.Priority: true}

// timingKinds are the quiet kinds plus the ones that change only the timing
// window (or clear a pool), the largest class a whole-walk memo could serve
// without a full dependency proof.
var timingKinds = [256]bool{
	events.DecisionAsk: true, events.DecisionMade: true, events.Priority: true,
	events.StepChange: true, events.ManaClear: true, events.Note: true,
}

func (m *walkStats) observe(e *rules.Engine, d *decision.Decision) {
	if d == nil || d.Kind != decision.KPriority {
		return
	}
	p := d.Player
	m.asks++
	b, _ := json.Marshal(d.Options)
	var pools [8]int32
	for i := range e.G.Players {
		pools[i] = e.G.Players[i].Pool.Total()
	}
	cur := prevAsk{ok: true, opts: string(b), ev: len(e.L.Events),
		sorcery: e.G.Active == p && e.G.Step.IsMain() && len(e.G.Stack) == 0,
		step:    e.G.Step, turn: e.G.Turn, stack: len(e.G.Stack), pools: pools}
	m.opts += len(d.Options)
	m.hand += len(e.G.Zone(state.ZHand, p))
	m.bf += len(e.G.Zone(state.ZBattlefield, p))
	if len(d.Options) == 2 {
		m.onlyPassOffered++
	}
	pv := m.prev[p]
	if pv.ok && pv.ev <= cur.ev {
		m.trans++
		same := pv.opts == cur.opts
		if same {
			m.same++
		}
		var ks []events.Kind
		var seen [256]bool
		allQuiet, allTiming := true, true
		for _, ev := range e.L.Events[pv.ev:cur.ev] {
			k := ev.Kind
			if !seen[k] {
				seen[k] = true
				ks = append(ks, k)
			}
			if !quietKinds[k] {
				allQuiet = false
			}
			if !timingKinds[k] {
				allTiming = false
			}
		}
		for _, k := range ks {
			m.kindIn[k]++
			if same {
				m.kindInSame[k]++
			}
		}
		if allQuiet {
			m.quiet++
			if same {
				m.quietSame++
			}
		}
		if allTiming {
			m.timing++
			if same {
				m.timingSame++
			}
			if pv.sorcery == cur.sorcery {
				m.timingSorc++
				if same {
					m.timingSorcSame++
				}
				if pv.pools == cur.pools {
					m.timingPool++
					if same {
						m.timingPoolSame++
					}
				}
			}
		}
		sort.Slice(ks, func(i, j int) bool { return ks[i] < ks[j] })
		var sb strings.Builder
		for i, k := range ks {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(k.String())
		}
		s := sb.String()
		st := m.sigs[s]
		if st == nil {
			st = &sigStat{}
			m.sigs[s] = st
		}
		st.n++
		if same {
			st.same++
		}
	}
	m.prev[p] = cur
}

// report prints the §1.5 transition table to stderr. Every list is ordered
// deterministically (the top-K signature list breaks ties by signature text),
// so two runs of the same row produce the same report.
func (m *walkStats) report() {
	pct := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return 100 * float64(a) / float64(b)
	}
	w := os.Stderr
	fmt.Fprintf(w, "walkstats: priority asks %d; mean options %.2f, mean hand %.2f, mean own battlefield %.2f; pass+concede only %d (%.1f%%)\n",
		m.asks, float64(m.opts)/float64(m.asks), float64(m.hand)/float64(m.asks), float64(m.bf)/float64(m.asks), m.onlyPassOffered, pct(m.onlyPassOffered, m.asks))
	fmt.Fprintf(w, "walkstats: same-seat transitions %d; identical option list %d (%.1f%%)\n", m.trans, m.same, pct(m.same, m.trans))
	fmt.Fprintf(w, "walkstats:   only {DecisionAsk,DecisionMade,Priority} between: %d (%.1f%% of transitions), identical %d (%.1f%%)\n", m.quiet, pct(m.quiet, m.trans), m.quietSame, pct(m.quietSame, m.quiet))
	fmt.Fprintf(w, "walkstats:   only quiet+{StepChange,ManaClear,Note}: %d (%.1f%%), identical %d (%.1f%%)\n", m.timing, pct(m.timing, m.trans), m.timingSame, pct(m.timingSame, m.timing))
	fmt.Fprintf(w, "walkstats:     ... and same sorcery-speed bit: %d (%.1f%%), identical %d (%.1f%%)\n", m.timingSorc, pct(m.timingSorc, m.trans), m.timingSorcSame, pct(m.timingSorcSame, m.timingSorc))
	fmt.Fprintf(w, "walkstats:     ... and all pools equal: %d (%.1f%%), identical %d (%.1f%%)\n", m.timingPool, pct(m.timingPool, m.trans), m.timingPoolSame, pct(m.timingPoolSame, m.timingPool))
	fmt.Fprintf(w, "walkstats: per-kind presence (transitions containing kind: n, identical%%):\n")
	for k := 0; k < 256; k++ {
		if m.kindIn[k] > 0 {
			fmt.Fprintf(w, "walkstats:   %-18s %8d %6.1f%%\n", events.Kind(k).String(), m.kindIn[k], pct(m.kindInSame[k], m.kindIn[k]))
		}
	}
	type kv struct {
		k string
		v *sigStat
	}
	all := make([]kv, 0, len(m.sigs))
	for k, v := range m.sigs {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].v.n != all[j].v.n {
			return all[i].v.n > all[j].v.n
		}
		return all[i].k < all[j].k
	})
	fmt.Fprintf(w, "walkstats: top kind-set signatures (n, %% of transitions, identical%%):\n")
	for i, x := range all {
		if i == 10 {
			break
		}
		fmt.Fprintf(w, "walkstats:   %8d %5.1f%% %6.1f%%  {%s}\n", x.v.n, pct(x.v.n, m.trans), pct(x.v.same, x.v.n), x.k)
	}
}
