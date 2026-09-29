package searchprobe

import (
	"encoding/json"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func TestCandidatesRetainBaselinePassAndCap(t *testing.T) {
	d := &ObservedDecision{Kind: decision.KPriority}
	for i := 1; i <= 12; i++ {
		d.Options = append(d.Options, ObservedOption{Action: Action{Kind: "cast", Obj: uint32(i)}})
	}
	pass := Action{Kind: "pass"}
	d.Options = append(d.Options, ObservedOption{Action: pass})
	baseline := d.Options[11].Action
	c := Candidates(d, baseline, 8)
	if len(c) != 8 || c[0] != baseline || c[1] != pass {
		t.Fatalf("candidates %v", c)
	}
	if c := Candidates(d, Action{Kind: "play_land"}, 8); len(c) != 0 {
		t.Fatal("intervened on land handling")
	}
}

func TestLeafAndStaticScoresUseVisibleMaterial(t *testing.T) {
	v := view.View{Players: []view.PlayerView{{ID: 0, Life: 20, HandSize: 3, Battlefield: []view.CardView{{Types: "Creature", Power: 2, Toughness: 3}}}, {ID: 1, Life: 15, HandSize: 2}}}
	// life diff 5, hand-count diff 2, creature 10+2*(2+3) = 27.
	if got := LeafScore(v, 0); got != 27 {
		t.Fatalf("score %v", got)
	}
	v.Over = true
	winner := state.PlayerID(1)
	v.Winner = &winner
	if got := LeafScore(v, 0); got != -100000 {
		t.Fatalf("terminal %v", got)
	}
	v.Over = false
	v.Players[0].Hand = []view.CardView{{ID: 1, Types: "Creature", Power: 2, Toughness: 3}, {ID: 2, Types: "Instant"}}
	board, _ := json.Marshal(v)
	candidates := []Action{{Kind: "pass"}, {Kind: "cast", Obj: 2}, {Kind: "cast", Obj: 1}}
	choice, err := StaticChoice(Frame{Board: board}, candidates)
	if err != nil || choice != 2 {
		t.Fatalf("static choice %d %v", choice, err)
	}
}

func TestSearchReplaysWorldsAndFallsBackWithoutCoverage(t *testing.T) {
	setup, h := samplingHistory(t)
	result, err := Sample(setup, h, SampleOptions{Seed: 44, Attempts: 8, Worlds: 4, MaxSubmits: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Worlds) == 0 {
		// The sampler can legitimately accept zero worlds when every attempt
		// is prefix-rejected (observed history disagrees with the replayed
		// frame). Fail with the SampleResult diagnostics instead of panicking
		// on the index below.
		t.Fatalf("sample accepted no worlds: Attempts:%d Accepted:%d PrefixRejected:%d BudgetExhausted:%d FirstRejection:%q",
			result.Attempts, result.Accepted, result.PrefixRejected, result.BudgetExhausted, result.FirstRejection)
	}
	d := result.Worlds[0].Engine.Pending()
	a, err := result.Worlds[0].Observer.Actions(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}})
	if err != nil {
		t.Fatal(err)
	}
	choice, err := SearchChoice(result.Worlds, a, 999, 5000)
	if err != nil || choice.Index != 0 || choice.Fallback != "" || choice.Replays != 4 {
		t.Fatalf("choice %+v %v", choice, err)
	}
	choice, err = SearchChoice(nil, a, 999, 5000)
	if err != nil || choice.Index != 0 || choice.Fallback == "" {
		t.Fatalf("missing fallback %+v %v", choice, err)
	}
}
