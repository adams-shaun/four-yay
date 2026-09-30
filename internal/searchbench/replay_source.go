package searchbench

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// ReplayGame is the complete public 17lands aggregate row required by the
// native reconstruction attempt. It preserves Arena ids until the card and
// ability resolvers classify them; no lossy name guess occurs at ingestion.
type ReplayGame struct {
	ID, DraftID string
	OnPlay      bool
	Deck        []DeckCard
	OpeningHand []string
	Turns       []ReplayTurn
}

type DeckCard struct {
	Name  string
	Count int
}

type ReplayTurn struct {
	Number         int
	User, Opponent TurnActions
}

type TurnActions struct {
	Lands, Creatures, NonCreatures, Instants, Abilities []string
	Attackers, Blockers                                 []string
}

// ReplayGamesCSV reads eligible FDN Premier Draft rows. A row with malformed
// action ids is refused: reconstruction must fail closed, not replay a
// different human game.
func ReplayGamesCSV(path string, f SourceFilter) ([]ReplayGame, error) {
	in, close, err := sourceReader(path)
	if err != nil {
		return nil, err
	}
	defer close()
	r := csv.NewReader(in)
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		return nil, err
	}
	ix := headerIndex(head)
	for _, name := range []string{"expansion", "event_type", "draft_id", "build_index", "match_number", "game_number", "on_play", "opening_hand", "user_game_win_rate_bucket", "user_n_games_bucket"} {
		if _, ok := ix[name]; !ok {
			return nil, fmt.Errorf("searchbench: source has no %q column", name)
		}
	}
	deckCols := make([]DeckCard, 0)
	deckAt := make([]int, 0)
	for i, name := range head {
		if strings.HasPrefix(name, "deck_") {
			deckAt, deckCols = append(deckAt, i), append(deckCols, DeckCard{Name: strings.TrimPrefix(name, "deck_")})
		}
	}
	var out []ReplayGame
	for rowNo := 2; ; rowNo++ {
		row, err := r.Read()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("searchbench: reading source row %d: %w", rowNo, err)
		}
		if len(row) != len(head) || row[ix["expansion"]] != "FDN" || row[ix["event_type"]] != "PremierDraft" || !eligible(row, ix, f) {
			continue
		}
		g, err := replayGame(row, ix, deckAt, deckCols)
		if err != nil {
			return nil, fmt.Errorf("searchbench: source row %d: %w", rowNo, err)
		}
		out = append(out, g)
	}
}

func replayGame(row []string, ix map[string]int, deckAt []int, deckCols []DeckCard) (ReplayGame, error) {
	g := ReplayGame{DraftID: row[ix["draft_id"]], OnPlay: row[ix["on_play"]] == "True"}
	g.ID = strings.Join([]string{g.DraftID, row[ix["build_index"]], row[ix["match_number"]], row[ix["game_number"]]}, ":")
	g.OpeningHand = splitIDs(row[ix["opening_hand"]])
	for _, id := range g.OpeningHand {
		if _, err := strconv.Atoi(id); err != nil {
			return ReplayGame{}, fmt.Errorf("opening hand id %q", id)
		}
	}
	for i, at := range deckAt {
		if row[at] == "" || row[at] == "0" {
			continue
		}
		n, err := strconv.Atoi(row[at])
		if err != nil || n < 1 {
			return ReplayGame{}, fmt.Errorf("deck count %q for %q", row[at], deckCols[i].Name)
		}
		g.Deck = append(g.Deck, DeckCard{Name: deckCols[i].Name, Count: n})
	}
	sort.Slice(g.Deck, func(i, j int) bool { return g.Deck[i].Name < g.Deck[j].Name })
	for turn := 1; turn <= 20; turn++ {
		u := actionsAt(row, ix, turn, "user")
		o := actionsAt(row, ix, turn, "oppo")
		if !u.empty() || !o.empty() || turn <= 12 {
			g.Turns = append(g.Turns, ReplayTurn{Number: turn, User: u, Opponent: o})
		}
	}
	return g, nil
}

func actionsAt(row []string, ix map[string]int, turn int, side string) TurnActions {
	get := func(s string) []string {
		i, ok := ix[fmt.Sprintf("%s_turn_%d_%s", side, turn, s)]
		if !ok {
			return nil
		}
		return splitIDs(row[i])
	}
	return TurnActions{Lands: get("lands_played"), Creatures: get("creatures_cast"), NonCreatures: get("non_creatures_cast"), Instants: get("user_instants_sorceries_cast"), Abilities: get("user_abilities"), Attackers: get("creatures_attacked"), Blockers: get("creatures_blocking")}
}

func splitIDs(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, "|")
}
func (a TurnActions) empty() bool {
	return len(a.Lands)+len(a.Creatures)+len(a.NonCreatures)+len(a.Instants)+len(a.Abilities)+len(a.Attackers)+len(a.Blockers) == 0
}
