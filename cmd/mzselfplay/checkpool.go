package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/mzplay"
)

// checkPools is -check-pool: every deck of the given pool files parsed and
// resolved against the whole corpus, without playing anything. It reports
// how many decks hold a card name gorge cannot resolve (a deck the engine
// refuses), how many hold a card the corpus has but the build does not fully
// support (cards.Registry.Unsupported against effects.Supported: the deck
// plays, with that card's unsupported part degraded), and how many are
// short of MinDeckSize.
func checkPools(pools []string, cardsDir string, out io.Writer) int {
	reg, err := cards.SharedCorpus(cardsDir)
	if err != nil {
		fmt.Fprintf(out, "check-pool: card corpus %s: %v\n", cardsDir, err)
		return 2
	}
	supported := effects.Supported()
	unresolved := map[string]int{}      // counted, then sorted
	unsupported := map[string]int{}     // counted, then sorted
	missingPrims := map[string]string{} // lookup only
	cardSupport := map[*cards.Card][]string{}
	status := 0
	for _, pool := range pools {
		paths, err := mzplay.ReadPool(pool)
		if err != nil {
			fmt.Fprintf(out, "check-pool: %v\n", err)
			return 2
		}
		var decks, badName, badSupport, short, unreadable int
		distinct := map[string]bool{}
		for _, path := range paths {
			list, err := mzplay.LoadDCK(path)
			if err != nil {
				unreadable++
				fmt.Fprintf(out, "check-pool: %v\n", err)
				continue
			}
			decks++
			if list.MainCount() < mzplay.MinDeckSize {
				short++
			}
			nameBad, supportBad := false, false
			for _, e := range list.Main {
				distinct[e.Name] = true
				c, ok := reg.Lookup(e.Name)
				if !ok {
					nameBad = true
					unresolved[e.Name]++
					continue
				}
				prims, seen := cardSupport[c]
				if !seen {
					prims = reg.Unsupported(c, supported)
					cardSupport[c] = prims
				}
				if len(prims) > 0 {
					supportBad = true
					unsupported[e.Name]++
					missingPrims[e.Name] = strings.Join(prims, ", ")
				}
			}
			if nameBad {
				badName++
			}
			if supportBad {
				badSupport++
			}
		}
		fmt.Fprintf(out, "check-pool: %s: %d decks, %d distinct card names; %d decks hold a name gorge cannot resolve, %d hold a card not fully supported, %d are under %d cards, %d unreadable\n",
			pool, decks, len(distinct), badName, badSupport, short, mzplay.MinDeckSize, unreadable)
		if badName > 0 || unreadable > 0 {
			status = 1
		}
	}
	report := func(title string, m map[string]int, note map[string]string) {
		names := make([]string, 0, len(m))
		for n := range m {
			names = append(names, n)
		}
		sort.Slice(names, func(i, j int) bool {
			if m[names[i]] != m[names[j]] {
				return m[names[i]] > m[names[j]]
			}
			return names[i] < names[j]
		})
		fmt.Fprintf(out, "check-pool: %s: %d\n", title, len(names))
		for _, n := range names {
			if note != nil {
				fmt.Fprintf(out, "  %6d decks  %s  [%s]\n", m[n], n, note[n])
			} else {
				fmt.Fprintf(out, "  %6d decks  %s\n", m[n], n)
			}
		}
	}
	report("card names the corpus does not resolve", unresolved, nil)
	report("cards the build does not fully support", unsupported, missingPrims)
	return status
}
