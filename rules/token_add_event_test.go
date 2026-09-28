package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestFixedAddTokenAppliesOncePerCreationEvent(t *testing.T) {
	t.Parallel()
	helm := tokenReplCorpusCard(t, "Worldwalker Helm")
	doubler := tokenReplCorpusCard(t, "Doubling Season")

	mapCounts := make(map[string]int)
	for _, tc := range []struct {
		name         string
		first        string
		wantTreasure int
		wantMap      int
	}{
		{name: "doubler first", first: "Doubling Season", wantTreasure: 2, wantMap: 1},
		{name: "helm first", first: "Worldwalker Helm", wantTreasure: 2, wantMap: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg := tokenReplGame(t, 20260927, helm, doubler)
			helmID := moveSeededCard(t, e, 0, helm, state.ZBattlefield)
			doublerID := moveSeededCard(t, e, 0, doubler, state.ZBattlefield)
			if e.G.Obj(helmID).Zone != state.ZBattlefield || e.G.Obj(doublerID).Zone != state.ZBattlefield {
				t.Fatal("precondition: Helm and Doubling Season must be on the battlefield")
			}
			e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "c_a_treasure_sac"})
			d := e.Pending()
			if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 {
				t.Fatalf("pending = %+v, want seat 0's KReplacement order ask over two replacements", d)
			}
			first := helmID
			if tc.first == "Doubling Season" {
				first = doublerID
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{optionFor(t, d, first)}}); err != nil {
				t.Fatal(err)
			}
			gotTreasure := countTokensNamedOnSeat(t, e, 0, "Treasure Token")
			gotMap := countTokensNamedOnSeat(t, e, 0, "Map Token")
			mapCounts[tc.name] = gotMap
			if gotTreasure != tc.wantTreasure || gotMap != tc.wantMap {
				t.Fatalf("tokens = %d Treasure, %d Map; want %d Treasure, %d Map", gotTreasure, gotMap, tc.wantTreasure, tc.wantMap)
			}
			replayCheck(t, e, cfg)
		})
	}
	if mapCounts["helm first"] == mapCounts["doubler first"] {
		t.Fatalf("precondition: replacement order must produce different Map counts, got %v", mapCounts)
	}
}

func TestChatterfangAddTokenRemainsProportional(t *testing.T) {
	t.Parallel()
	chatterfang := tokenReplCorpusCard(t, "Chatterfang, Squirrel General")
	doubler := tokenReplCorpusCard(t, "Doubling Season")
	e, cfg := tokenReplGame(t, 20260927, chatterfang, doubler)
	chatterfangID := moveSeededCard(t, e, 0, chatterfang, state.ZBattlefield)
	doublerID := moveSeededCard(t, e, 0, doubler, state.ZBattlefield)
	if e.G.Obj(chatterfangID).Zone != state.ZBattlefield || e.G.Obj(doublerID).Zone != state.ZBattlefield {
		t.Fatal("precondition: Chatterfang and Doubling Season must be on the battlefield")
	}
	e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "g_1_1_squirrel"})
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 {
		t.Fatalf("pending = %+v, want seat 0's KReplacement order ask over two replacements", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{optionFor(t, d, doublerID)}}); err != nil {
		t.Fatal(err)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 4 {
		t.Fatalf("Squirrel tokens = %d, want 4 (two doubled mints plus two proportional extras)", got)
	}
	replayCheck(t, e, cfg)
}
