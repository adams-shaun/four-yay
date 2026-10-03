package adopt

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Impact is one unsupported primitive's row in the C5 table: the cards it
// keeps from support and the sets implementing it would unlock.
type Impact struct {
	Primitive string `json:"primitive"`
	// NonTournament is true when every card the primitive blocks is outside
	// every tournament target (or it is listed as a non-tournament
	// primitive): it is excluded from the All target and from tickets.
	NonTournament bool     `json:"non_tournament,omitempty"`
	Cards         []string `json:"cards"`         // tournament cards it blocks
	Sole          []string `json:"sole"`          // of Cards, those it alone blocks
	Sets          []string `json:"sets"`          // sets printing one of Cards
	SetsUnlocked  []string `json:"sets_unlocked"` // sets whose only missing primitive it is
	// ByFormat and SoleByFormat count Cards and Sole per Config.Formats
	// entry, in Config order.
	ByFormat           []int    `json:"by_format"`
	SoleByFormat       []int    `json:"sole_by_format"`
	NonTournamentCards []string `json:"non_tournament_cards,omitempty"`
}

// Impact ranks every unsupported primitive by the tournament cards it
// blocks in each declared-target format, in Config order (a primitive
// blocking more Standard cards outranks one blocking more Pioneer cards),
// then by sets unlocked, then by name. Non-tournament primitives sort last.
func (cs *Census) Impact() []Impact {
	nf := len(cs.Config.Formats)
	rows := map[string]*Impact{}
	row := func(p string) *Impact {
		r := rows[p]
		if r == nil {
			r = &Impact{Primitive: p, ByFormat: make([]int, nf), SoleByFormat: make([]int, nf)}
			rows[p] = r
		}
		return r
	}
	sets := map[string]map[string]bool{}
	for _, c := range cs.Cards {
		for _, p := range c.Missing {
			r := row(p)
			if c.NonTournament != "" {
				r.NonTournamentCards = append(r.NonTournamentCards, c.Name)
				continue
			}
			r.Cards = append(r.Cards, c.Name)
			sole := len(c.Missing) == 1
			if sole {
				r.Sole = append(r.Sole, c.Name)
			}
			for i, f := range cs.Config.Formats {
				if contains(c.Formats, f.Name) {
					r.ByFormat[i]++
					if sole {
						r.SoleByFormat[i]++
					}
				}
			}
			if sets[p] == nil {
				sets[p] = map[string]bool{}
			}
			for _, s := range c.Sets {
				sets[p][s] = true
			}
		}
	}
	// A set is unlocked by p when p is the only primitive its tournament
	// cards still miss.
	for _, s := range cs.SetCodes() {
		missing := map[string]bool{}
		for _, c := range cs.setCards[s] {
			if c.NonTournament != "" {
				continue
			}
			for _, p := range c.Missing {
				missing[p] = true
			}
		}
		if len(missing) == 1 {
			for p := range missing {
				row(p).SetsUnlocked = append(row(p).SetsUnlocked, s)
			}
		}
	}
	out := make([]Impact, 0, len(rows))
	for p, r := range rows {
		r.NonTournament = len(r.Cards) == 0 || contains(cs.Config.NonTournamentPrimitives, p)
		for s := range sets[p] {
			r.Sets = append(r.Sets, s)
		}
		sort.Strings(r.Sets)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.NonTournament != b.NonTournament {
			return !a.NonTournament
		}
		for k := range a.ByFormat {
			if a.ByFormat[k] != b.ByFormat[k] {
				return a.ByFormat[k] > b.ByFormat[k]
			}
		}
		if len(a.SetsUnlocked) != len(b.SetsUnlocked) {
			return len(a.SetsUnlocked) > len(b.SetsUnlocked)
		}
		return a.Primitive < b.Primitive
	})
	return out
}

// WriteImpact renders the impact table as Markdown: one row per primitive,
// blocked (sole) cards per format, sets unlocked, and example cards. top
// limits the tournament rows (0 = all); non-tournament primitives are
// summarised in one line, since they are outside every target.
func (cs *Census) WriteImpact(w io.Writer, rows []Impact, top int) {
	var head, rule []string
	head = append(head, "#", "primitive")
	for _, f := range cs.Config.Formats {
		head = append(head, f.Name)
	}
	head = append(head, "sets unlocked", "e.g.")
	for range head {
		rule = append(rule, "---")
	}
	fmt.Fprintf(w, "| %s |\n| %s |\n", strings.Join(head, " | "), strings.Join(rule, " | "))
	n, excluded, excludedCards := 0, 0, 0
	for _, r := range rows {
		if r.NonTournament {
			excluded++
			excludedCards += len(r.NonTournamentCards)
			continue
		}
		n++
		if top > 0 && n > top {
			continue
		}
		cells := []string{fmt.Sprint(n), "`" + r.Primitive + "`"}
		for i := range cs.Config.Formats {
			cells = append(cells, fmt.Sprintf("%d (%d)", r.ByFormat[i], r.SoleByFormat[i]))
		}
		cells = append(cells, fmt.Sprintf("%d %s", len(r.SetsUnlocked), clip(r.SetsUnlocked, 4)), clip(r.Cards, 3))
		fmt.Fprintf(w, "| %s |\n", strings.Join(cells, " | "))
	}
	fmt.Fprintf(w, "\n%d tournament primitives (cells: blocked cards (of which it alone blocks)); %d non-tournament primitives excluded from every target (%d cards).\n", n, excluded, excludedCards)
}

func clip(xs []string, n int) string {
	if len(xs) <= n {
		return strings.Join(xs, ", ")
	}
	return strings.Join(xs[:n], ", ") + fmt.Sprintf(", +%d", len(xs)-n)
}
