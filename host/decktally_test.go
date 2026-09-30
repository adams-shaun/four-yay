package host

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/protocol"
)

func u8(n uint8) *uint8 { return &n }

func rec(d string, g, w, l, dr int, r float64) protocol.DeckRecord {
	return protocol.DeckRecord{Deck: d, Games: g, Wins: w, Losses: l, Draws: dr, WinRate: r}
}

func fin(winner *uint8, decks ...string) protocol.MatchInfo {
	m := protocol.MatchInfo{State: protocol.MatchFinished, Result: "win", Winner: winner}
	if winner == nil {
		m.Result = "draw"
	}
	for _, d := range decks {
		m.Seats = append(m.Seats, protocol.SeatInfo{Deck: d})
	}
	return m
}

func TestTallyDecks(t *testing.T) {
	human := fin(u8(0), "A", "B")
	human.Seats[1].Human = true
	live := fin(u8(0), "A", "B")
	live.State = protocol.MatchLive
	aborted := fin(u8(0), "A", "B")
	aborted.State = protocol.MatchAborted
	nilWinner := fin(u8(0), "A", "B")
	nilWinner.Winner, nilWinner.Result = nil, "win"
	badWinner := fin(u8(5), "A", "B")

	tests := []struct {
		name    string
		in      []protocol.MatchInfo
		min     int
		counted int
		want    []protocol.DeckRecord
	}{
		{"both seat orders", []protocol.MatchInfo{fin(u8(0), "A", "B"), fin(u8(1), "B", "A"), fin(u8(1), "A", "B")}, 0, 3,
			[]protocol.DeckRecord{rec("A", 3, 2, 1, 0, 2.0/3), rec("B", 3, 1, 2, 0, 1.0/3)}},
		{"draws", []protocol.MatchInfo{fin(nil, "A", "B"), fin(u8(0), "A", "B")}, 0, 2,
			[]protocol.DeckRecord{rec("A", 2, 1, 0, 1, 0.5), rec("B", 2, 0, 1, 1, 0)}},
		{"excluded", []protocol.MatchInfo{human, live, aborted, nilWinner, badWinner}, 0, 0, []protocol.DeckRecord{}},
		{"mirror skipped", []protocol.MatchInfo{fin(u8(0), "A", "A"), fin(u8(0), "A", "B")}, 0, 1,
			[]protocol.DeckRecord{rec("A", 1, 1, 0, 0, 1), rec("B", 1, 0, 1, 0, 0)}},
		{"multiseat", []protocol.MatchInfo{fin(u8(2), "A", "B", "C", "A")}, 0, 1,
			[]protocol.DeckRecord{rec("C", 1, 1, 0, 0, 1), rec("A", 2, 0, 2, 0, 0), rec("B", 1, 0, 1, 0, 0)}},
		{"min games floor", []protocol.MatchInfo{fin(u8(0), "A", "B"), fin(u8(0), "A", "C")}, 2, 2,
			[]protocol.DeckRecord{rec("A", 2, 2, 0, 0, 1)}},
		{"tiebreak games then name", []protocol.MatchInfo{
			fin(u8(0), "Z", "Y"), fin(u8(0), "Z", "Y"), fin(u8(0), "B", "X"), fin(u8(1), "W", "M"), fin(u8(1), "M", "W"), fin(u8(0), "A", "X")}, 0, 6,
			[]protocol.DeckRecord{
				rec("Z", 2, 2, 0, 0, 1), rec("A", 1, 1, 0, 0, 1), rec("B", 1, 1, 0, 0, 1),
				rec("M", 2, 1, 1, 0, 0.5), rec("W", 2, 1, 1, 0, 0.5),
				rec("X", 2, 0, 2, 0, 0), rec("Y", 2, 0, 2, 0, 0)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TallyDecks(tc.in, tc.min)
			if got.MatchesCounted != tc.counted || got.MinGames != tc.min {
				t.Fatalf("counted %d min %d, want %d %d", got.MatchesCounted, got.MinGames, tc.counted, tc.min)
			}
			if !reflect.DeepEqual(got.Decks, tc.want) {
				t.Fatalf("decks\n got %+v\nwant %+v", got.Decks, tc.want)
			}
		})
	}
}
