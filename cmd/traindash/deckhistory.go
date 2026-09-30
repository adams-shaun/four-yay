package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DeckHistory is a deck's ordered win-rate series across candidate runs.
type DeckHistory struct {
	Deck   string             `json:"deck"`
	Points []DeckHistoryPoint `json:"points"`
}

// DeckHistoryPoint is the per-deck result from one candidate ledger.
type DeckHistoryPoint struct {
	Run     string    `json:"run"`
	Order   time.Time `json:"order"`
	Wins    int       `json:"wins"`
	Losses  int       `json:"losses"`
	Games   int       `json:"games"`
	WinRate float64   `json:"win_rate"`
	CI      []float64 `json:"ci"`
}

type candidateMeta struct {
	TS      string `json:"ts"`
	GitHead string `json:"git_head"`
	Spec    string `json:"spec"`
	Key     string `json:"key"`
}

type candidateLedger struct {
	label  string
	order  time.Time
	ledger *DeckLedger
}

// scanDeckHistory reduces only cand/<head>/<spec>/matches.jsonl ledgers. The
// writer's meta timestamp is authoritative; old ledgers fall back to mtime.
func scanDeckHistory(root string) []DeckHistory {
	var runs []candidateLedger
	for _, f := range findDeckLedgers(root) {
		parts := strings.Split(filepath.ToSlash(f.id), "/")
		if len(parts) != 3 || parts[0] != "cand" {
			continue
		}
		b, err := os.ReadFile(f.source)
		if err != nil {
			continue
		}
		fi, err := os.Stat(f.source)
		if err != nil {
			continue
		}
		order := fi.ModTime()
		label := shortHead(parts[1]) + "/" + parts[2]
		var meta candidateMeta
		if mb, err := os.ReadFile(filepath.Join(filepath.Dir(f.source), "meta.json")); err == nil && json.Unmarshal(mb, &meta) == nil {
			if t, err := time.Parse(time.RFC3339Nano, meta.TS); err == nil {
				order = t
			}
			if meta.GitHead != "" && meta.Spec != "" {
				label = shortHead(meta.GitHead) + "/" + meta.Spec
			}
		}
		if ledger := parseDeckLedger(label, f.source, b, ""); ledger != nil {
			runs = append(runs, candidateLedger{label: label, order: order, ledger: ledger})
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].order.Equal(runs[j].order) {
			return runs[i].label < runs[j].label
		}
		return runs[i].order.Before(runs[j].order)
	})
	byDeck := map[string][]DeckHistoryPoint{}
	for _, run := range runs {
		for _, row := range run.ledger.Rows {
			byDeck[row.Deck] = append(byDeck[row.Deck], DeckHistoryPoint{
				Run: run.label, Order: run.order, Wins: row.Wins, Losses: row.Losses,
				Games: row.Games, WinRate: row.WinRate, CI: append([]float64(nil), row.CI...),
			})
		}
	}
	decks := make([]string, 0, len(byDeck))
	for deck := range byDeck {
		decks = append(decks, deck)
	}
	sort.Strings(decks)
	out := make([]DeckHistory, 0, len(decks))
	for _, deck := range decks {
		out = append(out, DeckHistory{Deck: deck, Points: byDeck[deck]})
	}
	return out
}

func shortHead(head string) string {
	if len(head) > 8 {
		return head[:8]
	}
	return head
}
