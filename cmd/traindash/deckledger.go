// Per-deck win-rate ledgers, read-only.
//
// cmd/botbench -spellbench writes a SpellBench match ledger (matches.jsonl,
// schema spellbench-match-ledger/v1) into its -spellbench-out directory. Every
// row carries the deck both seats played and the name of the policy in each
// seat, so the same file that feeds the pooled Bradley-Terry rating also holds
// a per-deck win rate for any policy that played in it. scripts/sb-gauntlet.sh
// keeps one such ledger per reference cache (gauntlet/ref/<key>/) and throws
// the per-candidate ledgers away, but the reference ledgers are exactly the
// per-deck material a deck-scoped dashboard needs -- at zero new game cost.
//
// This file is the reader the plan was missing: it turns any directory of
// matches.jsonl into a per-deck win rate with a 95% Wilson interval, keyed by
// the focus policy. It never writes, and it never plays a game.
package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DeckRate is one deck's recorded games for the focus policy.
type DeckRate struct {
	Deck    string    `json:"deck"`
	Wins    int       `json:"wins"`
	Losses  int       `json:"losses"`
	Draws   int       `json:"draws"`
	Games   int       `json:"games"`
	WinRate float64   `json:"win_rate"`
	CI      []float64 `json:"ci"`
}

// DeckLedger is one matches.jsonl ledger reduced to per-deck rates for one
// focus policy. Focus is the policy name whose win rate each row is; Games is
// the number of natural games the focus policy played in this ledger.
type DeckLedger struct {
	ID     string     `json:"id"`
	Source string     `json:"source"`
	Focus  string     `json:"focus"`
	Games  int        `json:"games"`
	Rows   []DeckRate `json:"rows"`
}

// deckLedgerRow is the subset of a spellbench-match-ledger/v1 row the per-deck
// reader needs. Unknown fields (engine, step_count, ...) are ignored.
type deckLedgerRow struct {
	Schema string `json:"schema"`
	Decks  []struct {
		CatalogID string `json:"catalog_id"`
	} `json:"decks"`
	Seats []struct {
		Name string `json:"name"`
	} `json:"seats"`
	Winner  string `json:"winner"`
	Outcome string `json:"outcome"`
}

// parseDeckLedger reads matches.jsonl content and returns per-deck rates for
// focus. When focus is empty the policy with the most seat appearances is
// used, so a candidate's own ledger reports the candidate without a flag.
// A row counts only when it is a natural win or loss for the focus policy:
// draws, truncations and halts are excluded from both counts, exactly as the
// rating excludes them. Returns nil when no focus policy appears.
func parseDeckLedger(id, source string, b []byte, focus string) *DeckLedger {
	var rows []deckLedgerRow
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r deckLedgerRow
		if json.Unmarshal([]byte(line), &r) != nil {
			continue // a mid-write truncation: skip the line, never fatal
		}
		if r.Schema != "spellbench-match-ledger/v1" || len(r.Decks) == 0 || len(r.Seats) < 2 {
			continue
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		return nil
	}
	if focus == "" {
		focus = dominantPolicy(rows)
	}
	byDeck := map[string]*DeckRate{}
	for _, r := range rows {
		deck := ""
		for _, d := range r.Decks {
			if d.CatalogID != "" {
				deck = d.CatalogID
				break
			}
		}
		if deck == "" {
			continue
		}
		seat := -1
		for i, s := range r.Seats {
			if s.Name == focus {
				seat = i
				break
			}
		}
		if seat < 0 {
			continue
		}
		dr := byDeck[deck]
		if dr == nil {
			dr = &DeckRate{Deck: deck}
			byDeck[deck] = dr
		}
		switch {
		case r.Winner == "p0" && (r.Outcome == "natural" || r.Outcome == "p0_win"):
			if seat == 0 {
				dr.Wins++
			} else {
				dr.Losses++
			}
		case r.Winner == "p1" && (r.Outcome == "natural" || r.Outcome == "p1_win"):
			if seat == 1 {
				dr.Wins++
			} else {
				dr.Losses++
			}
		default:
			dr.Draws++ // draw, truncated or halted: recorded, not scored
		}
	}
	if len(byDeck) == 0 {
		return nil
	}
	led := &DeckLedger{ID: id, Source: source, Focus: focus}
	for _, dr := range byDeck {
		dr.Games = dr.Wins + dr.Losses
		dr.WinRate = wilsonPoint(dr.Wins, dr.Games)
		lo, hi := wilson95(dr.Wins, dr.Games)
		dr.CI = []float64{lo, hi}
		led.Rows = append(led.Rows, *dr)
		led.Games += dr.Wins + dr.Losses + dr.Draws
	}
	sort.Slice(led.Rows, func(i, j int) bool { return led.Rows[i].Deck < led.Rows[j].Deck })
	return led
}

// dominantPolicy returns the seat name that appears in the most rows (ties
// broken lexically), so a ledger with one non-reference policy reports it.
func dominantPolicy(rows []deckLedgerRow) string {
	count := map[string]int{}
	for _, r := range rows {
		for _, s := range r.Seats {
			if s.Name != "" {
				count[s.Name]++
			}
		}
	}
	best, bestN := "", -1
	for name, n := range count {
		if n > bestN || (n == bestN && name < best) {
			best, bestN = name, n
		}
	}
	return best
}

// wilsonPoint is the raw observed rate, or 0 with no scored games.
func wilsonPoint(wins, n int) float64 {
	if n <= 0 {
		return 0
	}
	return float64(wins) / float64(n)
}

// wilson95 is the Wilson score interval for a binomial rate (the same form
// internal/hindsight.Wilson95 uses; duplicated so the dashboard keeps its
// zero-engine-dependency build).
func wilson95(wins, n int) (lo, hi float64) {
	if n <= 0 {
		return 0, 1
	}
	const z = 1.959963984540054
	p := float64(wins) / float64(n)
	z2 := z * z
	den := 1 + z2/float64(n)
	center := (p + z2/(2*float64(n))) / den
	half := z * math.Sqrt((p*(1-p)+z2/(4*float64(n)))/float64(n)) / den
	return math.Max(0, center-half), math.Min(1, center+half)
}

// deckLedgerFile is one discovered matches.jsonl.
type deckLedgerFile struct {
	id     string
	source string
}

// findDeckLedgers walks root for matches.jsonl files, deepest-last, and skips
// the directories traindash already skips. The id is the path relative to
// root, so a gauntlet ref cache reads as ref/<key>.
func findDeckLedgers(root string) []deckLedgerFile {
	var out []deckLedgerFile
	filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil {
			return nil
		}
		if fi.IsDir() {
			base := filepath.Base(p)
			if p != root && (skipDirs[base] || strings.HasPrefix(base, "bin-")) {
				return filepath.SkipDir
			}
			return nil
		}
		if fi.Name() != "matches.jsonl" {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return nil
		}
		out = append(out, deckLedgerFile{id: filepath.ToSlash(rel), source: p})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}
