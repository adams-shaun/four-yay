package paymirror

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Example locates one report for a summary line: the game and the planned
// cast's decision Seq.
type Example struct {
	Spec GameSpec `json:"spec"`
	Seq  uint64   `json:"seq"`
	Card string   `json:"card"`
}

type tally struct {
	N       int
	Example Example
}

// Summary aggregates driver results deterministically (every map is read in
// sorted-key order when printed).
type Summary struct {
	Games, GameErrors         int
	Truncated                 int
	RestViolations            map[string]*tally
	Planned                   int
	Equivalent                int
	EquivalentShapeNotes      int
	EquivalentEventOrder      int
	Mismatched                map[string]*tally
	Unmirrorable              map[string]*tally
	RouteStatus               map[string]int
	Control                   map[string]*tally
	GameErrorClasses          map[string]*tally
	AFallbacks                map[string]*tally
	ActivationHistogram       map[int]int
	SideEffects               map[string]*tally
	MismatchCount, UnmirCount int
}

func NewSummary() *Summary {
	return &Summary{Mismatched: map[string]*tally{}, Unmirrorable: map[string]*tally{}, RouteStatus: map[string]int{},
		Control: map[string]*tally{}, GameErrorClasses: map[string]*tally{}, AFallbacks: map[string]*tally{},
		ActivationHistogram: map[int]int{}, SideEffects: map[string]*tally{}, RestViolations: map[string]*tally{}}
}

func bump(m map[string]*tally, k string, ex Example) {
	t := m[k]
	if t == nil {
		t = &tally{Example: ex}
		m[k] = t
	}
	t.N++
}

// Add folds one game's result into the summary.
func (s *Summary) Add(g GameResult) {
	s.Games++
	if strings.HasPrefix(g.Err, "truncated:") || strings.HasPrefix(g.Err, "stopped:") {
		s.Truncated++
		bump(s.GameErrorClasses, g.Err, Example{Spec: g.Spec})
	} else if g.Err != "" {
		s.GameErrors++
		bump(s.GameErrorClasses, errClass(g.Err), Example{Spec: g.Spec})
	}
	if g.RestViolation != "" {
		key := g.RestViolation
		if i := strings.Index(key, ": "); i >= 0 {
			key = key[i+2:]
		}
		if i := strings.Index(key, " after "); i >= 0 {
			key = key[:i]
		}
		bump(s.RestViolations, key, Example{Spec: g.Spec})
	}
	for _, r := range g.Reports {
		ex := Example{Spec: g.Spec, Seq: r.Seq, Card: r.Card}
		s.Planned++
		s.ActivationHistogram[r.Activations]++
		if r.AFallback != "" {
			bump(s.AFallbacks, r.AFallback, ex)
		}
		for _, se := range r.ASideEffects {
			bump(s.SideEffects, se, ex)
		}
		if r.Control != nil {
			k := string(r.Control.Status)
			if r.Control.Status != Equivalent {
				k += "|" + r.Control.Signature
				if r.Control.Signature == "" {
					k += "|" + r.Control.Reason
				}
			}
			bump(s.Control, k, ex)
		}
		for _, rr := range r.Routes {
			s.RouteStatus[string(rr.Route)+"|"+string(rr.Status)]++
			if rr.Resolved != "" {
				s.RouteStatus[string(rr.Route)+"|resolved:"+failureClass(rr.Resolved)]++
			}
		}
		st, key := r.Verdict()
		switch st {
		case Equivalent:
			s.Equivalent++
			for _, rr := range r.Routes {
				if rr.Status == Equivalent {
					if len(rr.ShapeNotes) > 0 {
						s.EquivalentShapeNotes++
					}
					if rr.EventOrderDiffers {
						s.EquivalentEventOrder++
					}
					break
				}
			}
		case Mismatch:
			s.MismatchCount++
			bump(s.Mismatched, key, ex)
		default:
			s.UnmirCount++
			bump(s.Unmirrorable, key, ex)
		}
	}
}

func errClass(s string) string {
	for _, sep := range []string{": object", " object", "(", "\n"} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

func sortedKeys(m map[string]*tally) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]].N != m[keys[j]].N {
			return m[keys[i]].N > m[keys[j]].N
		}
		return keys[i] < keys[j]
	})
	return keys
}

func (e Example) String() string {
	return fmt.Sprintf("seed=%d decks=%s commander=%t policy=%s seq=%d card=%q",
		e.Spec.Seed, strings.Join(e.Spec.Decks, ","), e.Spec.Commander, e.Spec.Policy, e.Seq, e.Card)
}

// Write prints the summary.
func (s *Summary) Write(w io.Writer) {
	fmt.Fprintf(w, "games: %d (game-level errors: %d, truncated by harness bounds or stopped after a mid-cast corruption: %d)\n", s.Games, s.GameErrors, s.Truncated)
	fmt.Fprintf(w, "planned casts checked: %d\n", s.Planned)
	fmt.Fprintf(w, "  equivalent:   %d (of which decision-shape notes: %d, event order differs: %d)\n",
		s.Equivalent, s.EquivalentShapeNotes, s.EquivalentEventOrder)
	fmt.Fprintf(w, "  mismatched:   %d\n", s.MismatchCount)
	for _, k := range sortedKeys(s.Mismatched) {
		t := s.Mismatched[k]
		fmt.Fprintf(w, "    %6d  %s\n            e.g. %s\n", t.N, k, t.Example)
	}
	fmt.Fprintf(w, "  unmirrorable: %d\n", s.UnmirCount)
	for _, k := range sortedKeys(s.Unmirrorable) {
		t := s.Unmirrorable[k]
		fmt.Fprintf(w, "    %6d  %s\n            e.g. %s\n", t.N, k, t.Example)
	}
	fmt.Fprintf(w, "per route:\n")
	rk := make([]string, 0, len(s.RouteStatus))
	for k := range s.RouteStatus {
		rk = append(rk, k)
	}
	sort.Strings(rk)
	for _, k := range rk {
		fmt.Fprintf(w, "  %-40s %d\n", k, s.RouteStatus[k])
	}
	if len(s.Control) > 0 {
		fmt.Fprintf(w, "control (live run A vs pre-submit clone replaying A):\n")
		for _, k := range sortedKeys(s.Control) {
			t := s.Control[k]
			fmt.Fprintf(w, "  %6d  %s\n", t.N, k)
			if !strings.HasPrefix(k, string(Equivalent)) {
				fmt.Fprintf(w, "          e.g. %s\n", t.Example)
			}
		}
	}
	if len(s.AFallbacks) > 0 {
		fmt.Fprintf(w, "run A payment fallbacks:\n")
		for _, k := range sortedKeys(s.AFallbacks) {
			t := s.AFallbacks[k]
			fmt.Fprintf(w, "  %6d  %s  e.g. %s\n", t.N, k, t.Example)
		}
	}
	if len(s.SideEffects) > 0 {
		fmt.Fprintf(w, "side effects of planned activations (damage/life/counters/draws between a planned tap and the payment):\n")
		for _, k := range sortedKeys(s.SideEffects) {
			t := s.SideEffects[k]
			fmt.Fprintf(w, "  %6d  %s  e.g. %s\n", t.N, k, t.Example)
		}
	}
	hk := make([]int, 0, len(s.ActivationHistogram))
	for k := range s.ActivationHistogram {
		hk = append(hk, k)
	}
	sort.Ints(hk)
	fmt.Fprintf(w, "plan activations histogram:")
	for _, k := range hk {
		fmt.Fprintf(w, " %d:%d", k, s.ActivationHistogram[k])
	}
	fmt.Fprintln(w)
	if len(s.RestViolations) > 0 {
		fmt.Fprintf(w, "games whose first priority decision posed mid-cast / with a choose flow armed:\n")
		for _, k := range sortedKeys(s.RestViolations) {
			t := s.RestViolations[k]
			fmt.Fprintf(w, "  %6d  %s  e.g. %s\n", t.N, k, t.Example)
		}
	}
	if len(s.GameErrorClasses) > 0 {
		fmt.Fprintf(w, "game-level errors and truncations:\n")
		for _, k := range sortedKeys(s.GameErrorClasses) {
			t := s.GameErrorClasses[k]
			fmt.Fprintf(w, "  %6d  %s  e.g. %s\n", t.N, k, t.Example)
		}
	}
}
