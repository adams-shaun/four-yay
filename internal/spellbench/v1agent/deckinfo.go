package v1agent

import (
	"github.com/adams-shaun/gorge/internal/spellbench"
)

// deckCounts is our own decklist (the catalog deck game_start names), by
// folded card name. nil when the catalog does not know the id.
func deckCounts(catalogID string) map[string]int {
	f, err := spellbench.File(spellbench.PauperKernel, catalogID)
	if err != nil {
		return nil
	}
	m := map[string]int{}
	for _, e := range f.Cards {
		m[normName(e.Name)] += e.Count
	}
	return m
}

// libraryCounts estimates what is left in our library: the decklist minus
// every card of ours the board shows outside it (battlefield, graveyard,
// hand, exile, stack). Tokens are not in the list and fall out.
func libraryCounts(deck map[string]int, b *Board) map[string]int {
	if deck == nil || b.Thin {
		return nil
	}
	left := map[string]int{}
	for n, c := range deck {
		left[n] = c
	}
	take := func(name string) {
		n := normName(name)
		if left[n] > 0 {
			left[n]--
		}
	}
	for _, c := range b.Mine {
		if c.Stable.Owner == b.Seat && !c.IsToken {
			take(c.Name)
		}
	}
	for _, c := range b.Theirs {
		if c.Stable.Owner == b.Seat && !c.IsToken {
			take(c.Name)
		}
	}
	for _, c := range b.MyGrave {
		take(c.Name)
	}
	for _, h := range b.Hand {
		take(h.Name)
	}
	for _, c := range b.Exile {
		if c.Stable.Owner == b.Seat {
			take(c.Name)
		}
	}
	for _, s := range b.Stack {
		if s.Source.Owner == b.Seat && s.Kind == "spell" && !s.IsCopy {
			if k := KernelCardByID(s.Source.CardDBID); k != nil {
				take(k.Name)
			}
		}
	}
	return left
}

func countWhere(m map[string]int, pred func(*KernelCard) bool) int {
	n := 0
	for name, c := range m {
		if k := KernelCardByName(name); k != nil && pred(k) {
			n += c
		}
	}
	return n
}

func isLandCard(k *KernelCard) bool     { return k.IsType("Land") }
func isCreatureCard(k *KernelCard) bool { return k.IsType("Creature") }
