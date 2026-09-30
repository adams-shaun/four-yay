package host

import (
	"sort"

	"github.com/adams-shaun/gorge/protocol"
)

// AllMatches lists every table's matches, tables in sorted id order (no map
// order), each table's matches in its own ascending order.
func (r *Registry) AllMatches() []protocol.MatchInfo {
	var out []protocol.MatchInfo
	for _, id := range r.ids() {
		ms, err := r.Matches(id)
		if err != nil {
			continue
		}
		out = append(out, ms...)
	}
	return out
}

// TallyDecks aggregates per-deck results over finished bot-vs-bot matches.
//
// Counted: State == finished with no Human seat, and either Result "draw"
// (every seat draws) or a non-nil Winner naming a seat. Excluded: live,
// aborted, crashed, any match with a human seat, and finished matches whose
// outcome is unreadable (nil Winner that is not a draw, or a winner index
// outside the seat list).
//
// Each seat counts for its deck: the winner's deck gets a win, every other
// seat's deck a loss, and a draw is a draw for all seats. A mirror match
// (every seat plays the same deck) is skipped entirely, because it says
// nothing about the deck's strength: it would add one win and one loss for
// the same name. A match that merely has a repeated deck among different
// decks still counts each seat.
//
// Decks with fewer than minGames games are dropped. Output is sorted by win
// rate desc, games desc, name asc; win_rate is wins/games.
func TallyDecks(matches []protocol.MatchInfo, minGames int) protocol.DeckTally {
	type acc struct{ w, l, d int }
	by := map[string]*acc{}
	counted := 0
	for _, m := range matches {
		if m.State != protocol.MatchFinished || len(m.Seats) < 2 {
			continue
		}
		human, mirror := false, true
		for _, s := range m.Seats {
			if s.Human {
				human = true
			}
			if s.Deck != m.Seats[0].Deck {
				mirror = false
			}
		}
		if human || mirror {
			continue
		}
		draw := m.Result == "draw"
		if !draw && (m.Winner == nil || int(*m.Winner) >= len(m.Seats)) {
			continue
		}
		counted++
		for i, s := range m.Seats {
			a := by[s.Deck]
			if a == nil {
				a = &acc{}
				by[s.Deck] = a
			}
			switch {
			case draw:
				a.d++
			case i == int(*m.Winner):
				a.w++
			default:
				a.l++
			}
		}
	}
	out := protocol.DeckTally{Decks: []protocol.DeckRecord{}, MinGames: minGames, MatchesCounted: counted}
	for name, a := range by { // order is erased by the sort below
		g := a.w + a.l + a.d
		if g < minGames || g == 0 {
			continue
		}
		out.Decks = append(out.Decks, protocol.DeckRecord{Deck: name, Games: g, Wins: a.w, Losses: a.l, Draws: a.d, WinRate: float64(a.w) / float64(g)})
	}
	sort.Slice(out.Decks, func(i, j int) bool {
		a, b := out.Decks[i], out.Decks[j]
		if a.WinRate != b.WinRate {
			return a.WinRate > b.WinRate
		}
		if a.Games != b.Games {
			return a.Games > b.Games
		}
		return a.Deck < b.Deck
	})
	return out
}
