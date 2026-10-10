package corpuscov

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/decision"
)

// Gap classes.
const (
	ClassNeverInDeck  = "never_in_deck"    // the slot's card never reached its zone
	ClassStructural   = "structural"       // IN_DECK, never OFFERED, never POTENTIAL
	ClassPoolPriced   = "structural_pool"  // IN_DECK, never OFFERED, but POTENTIAL (payable after tapping)
	ClassAvoided      = "policy_avoided"   // OFFERED, never CHOSEN (by the policy)
	ClassCovered      = "covered"          // CHOSEN at least once
	ClassExploredOnly = "explore_only"     // chosen only by forced exploration
	ClassDynamic      = "dynamic_unchosen" // a non-universe row (sub-decision option, token) never chosen
)

// Summary is the run-level funnel.
type Summary struct {
	Games, Slots, InDeck, Offered, Chosen int
	ExploreDecisions                      int
	ByClass                               map[string]int
	DecisionKinds                         []KindCount
}

// KindCount is decision.Kind coverage.
type KindCount struct {
	Kind  string
	Asked int
}

// Rows returns every row, classified, sorted by (card, key).
func (c *Census) Rows() []Row {
	out := make([]Row, 0, len(c.rows))
	for _, r := range c.rows {
		rr := *r
		rr.Decks = len(c.decks[r.Card])
		rr.Class = classify(&rr)
		out = append(out, rr)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Card != out[j].Card {
			return out[i].Card < out[j].Card
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func classify(r *Row) string {
	switch {
	case r.Chosen > 0:
		return ClassCovered
	case r.ExploreChosen > 0:
		return ClassExploredOnly
	case !r.Universe:
		return ClassDynamic
	case r.Offered > 0:
		return ClassAvoided
	case r.InDeckGames > 0 && r.Potential > 0:
		return ClassPoolPriced
	case r.InDeckGames > 0:
		return ClassStructural
	}
	return ClassNeverInDeck
}

// Summarize computes the funnel over the universe rows.
func (c *Census) Summarize() Summary {
	s := Summary{Games: c.Games, ExploreDecisions: c.exploreN, ByClass: map[string]int{}}
	for _, r := range c.Rows() {
		if !r.Universe {
			continue
		}
		s.Slots++
		if r.InDeckGames > 0 {
			s.InDeck++
		}
		if r.Offered > 0 {
			s.Offered++
		}
		if r.Chosen > 0 {
			s.Chosen++
		}
		s.ByClass[r.Class]++
	}
	for _, k := range decision.Kinds {
		s.DecisionKinds = append(s.DecisionKinds, KindCount{Kind: string(k), Asked: c.asked[k]})
	}
	return s
}

// WriteJSON writes {summary, rows}.
func (c *Census) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	return enc.Encode(struct {
		Summary Summary
		Rows    []Row
	}{c.Summarize(), c.Rows()})
}

// HintTally counts structural rows by hint, the classifier's first cut at a
// cause (a row carries several hints; each is counted).
func HintTally(rows []Row, class string) []KindCount {
	m := map[string]int{}
	for _, r := range rows {
		if r.Class != class {
			continue
		}
		if len(r.Hints) == 0 {
			m["(none)"]++
		}
		for _, h := range r.Hints {
			m[h]++
		}
	}
	out := make([]KindCount, 0, len(m))
	for k, v := range m {
		out = append(out, KindCount{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Asked != out[j].Asked {
			return out[i].Asked > out[j].Asked
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// WriteText writes the human report: funnel, decision kinds, class/hint
// tallies, and the top-n rows of each gap class.
func (c *Census) WriteText(w io.Writer, top int) {
	s := c.Summarize()
	rows := c.Rows()
	pct := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return 100 * float64(a) / float64(b)
	}
	fmt.Fprintf(w, "corpus coverage census: %d games, %d explore-forced decisions\n", s.Games, s.ExploreDecisions)
	fmt.Fprintf(w, "funnel over %d (card, ability) slots:\n", s.Slots)
	fmt.Fprintf(w, "  IN_DECK  %5d (%.1f%%)\n  OFFERED  %5d (%.1f%% of slots, %.1f%% of in-deck)\n  CHOSEN   %5d (%.1f%% of slots, %.1f%% of offered)\n",
		s.InDeck, pct(s.InDeck, s.Slots), s.Offered, pct(s.Offered, s.Slots), pct(s.Offered, s.InDeck), s.Chosen, pct(s.Chosen, s.Slots), pct(s.Chosen, s.Offered))
	fmt.Fprintf(w, "classes:")
	for _, k := range []string{ClassCovered, ClassExploredOnly, ClassAvoided, ClassPoolPriced, ClassStructural, ClassNeverInDeck} {
		fmt.Fprintf(w, " %s=%d", k, s.ByClass[k])
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "decision kinds asked:")
	for _, k := range s.DecisionKinds {
		fmt.Fprintf(w, " %s=%d", k.Kind, k.Asked)
	}
	fmt.Fprintln(w)
	for _, cl := range []string{ClassPoolPriced, ClassStructural, ClassAvoided} {
		fmt.Fprintf(w, "%s hints:", cl)
		for _, h := range HintTally(rows, cl) {
			fmt.Fprintf(w, " %s=%d", h.Kind, h.Asked)
		}
		fmt.Fprintln(w)
	}
	// Slot-key shapes per class (cast vs ability vs mana).
	for _, cl := range []string{ClassPoolPriced, ClassStructural, ClassAvoided} {
		m := map[string]int{}
		for _, r := range rows {
			if r.Class == cl {
				m[keyShape(r.Key)]++
			}
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(w, "%s by slot shape:", cl)
		for _, k := range keys {
			fmt.Fprintf(w, " %s=%d", k, m[k])
		}
		fmt.Fprintln(w)
	}
	sel := func(cl string, less func(a, b Row) bool) []Row {
		var out []Row
		for _, r := range rows {
			if r.Class == cl {
				out = append(out, r)
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
		if len(out) > top {
			out = out[:top]
		}
		return out
	}
	fmt.Fprintf(w, "\ntop pool-priced gaps (IN_DECK, payable after tapping, never OFFERED), by potential games:\n")
	for _, r := range sel(ClassPoolPriced, func(a, b Row) bool { return a.PotentialGames > b.PotentialGames }) {
		fmt.Fprintf(w, "  %4d games  %-36s %-18s [%s]\n", r.PotentialGames, r.Card, r.Key, strings.Join(r.Hints, ","))
	}
	fmt.Fprintf(w, "\ntop structural gaps (IN_DECK, never OFFERED), by in-deck games:\n")
	for _, r := range sel(ClassStructural, func(a, b Row) bool { return a.InDeckGames > b.InDeckGames }) {
		fmt.Fprintf(w, "  %4d games  %-36s %-18s [%s]\n", r.InDeckGames, r.Card, r.Key, strings.Join(r.Hints, ","))
	}
	fmt.Fprintf(w, "\ntop policy-avoided (OFFERED, never CHOSEN), by offers:\n")
	for _, r := range sel(ClassAvoided, func(a, b Row) bool { return a.Offered > b.Offered }) {
		fmt.Fprintf(w, "  %6d offers %4d games  %-36s %-18s [%s]\n", r.Offered, r.OfferedGames, r.Card, r.Key, strings.Join(r.Hints, ","))
	}
}

func keyShape(k string) string {
	if i := strings.IndexAny(k, "#@:"); i >= 0 {
		k = k[:i]
	}
	if i := strings.Index(k, "/"); i >= 0 {
		return k[:i] + "/alt-or-mode"
	}
	return k
}
