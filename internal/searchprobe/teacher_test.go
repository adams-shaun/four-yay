package searchprobe

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func TestAttackCandidatesBotFirstDedupedAndValid(t *testing.T) {
	d := &decision.Decision{Kind: decision.KAttackers, Player: 0, Min: 0, Max: 3, Options: []decision.Option{
		{Index: 0, Kind: "attacker", Obj: 10, Player: 1},
		{Index: 1, Kind: "attacker", Obj: 11, Player: 1},
		{Index: 2, Kind: "attacker", Obj: 12, Player: 1, Required: true},
	}}
	bot := decision.Intent{Player: 0, Choices: []int{0, 2}}
	got := AttackCandidates(d, bot, 8)
	if len(got) < 3 || len(got[0].Choices) != 2 || got[0].Choices[0] != 0 || got[0].Choices[1] != 2 {
		t.Fatalf("bot answer must come first: %+v", got)
	}
	seen := map[string]bool{}
	for _, in := range got {
		if err := d.Validate(in); err != nil {
			t.Fatalf("invalid candidate %v: %v", in.Choices, err)
		}
		key := ""
		for _, c := range in.Choices {
			key += string(rune('a' + c))
		}
		if seen[key] {
			t.Fatalf("duplicate candidate %v", in.Choices)
		}
		seen[key] = true
		// The required attacker is in every legal candidate.
		has := false
		for _, c := range in.Choices {
			has = has || c == 2
		}
		if !has {
			t.Fatalf("candidate omits required attacker: %v", in.Choices)
		}
	}
	if capped := AttackCandidates(d, bot, 2); len(capped) != 2 {
		t.Fatalf("limit ignored: %d", len(capped))
	}
	if AttackCandidates(&decision.Decision{Kind: decision.KPriority}, bot, 8) != nil {
		t.Fatal("non-attackers decision produced candidates")
	}
}

// TestAttackCandidatesOneDefenderPerAttacker: with a planeswalker beside the
// player, a creature has one option per defender. No candidate may name it
// twice (the engine rejects "attacker declared against more than one
// defender" and Decision.Validate does not catch it); toggling on its other
// option moves it to that defender. Before the fix the toggles produced the
// double declaration, and a search that kept picking that never-visitable
// candidate discarded almost every simulation.
func TestAttackCandidatesOneDefenderPerAttacker(t *testing.T) {
	d := &decision.Decision{Kind: decision.KAttackers, Player: 0, Min: 0, Max: 4, Options: []decision.Option{
		{Index: 0, Kind: "attacker", Obj: 10, Player: 1},
		{Index: 1, Kind: "attacker", Obj: 10, Player: 1, Battle: 77},
		{Index: 2, Kind: "attacker", Obj: 11, Player: 1},
		{Index: 3, Kind: "attacker", Obj: 11, Player: 1, Battle: 77},
	}}
	got := AttackCandidates(d, decision.Intent{Player: 0, Choices: []int{0, 2}}, 16)
	moved := false
	for _, in := range got {
		objs := map[state.ObjID]bool{}
		for _, c := range in.Choices {
			o := d.Options[c].Obj
			if objs[o] {
				t.Fatalf("candidate %v declares attacker %d twice", in.Choices, o)
			}
			objs[o] = true
		}
		if len(in.Choices) == 2 && in.Choices[0] == 1 && in.Choices[1] == 2 {
			moved = true
		}
	}
	if !moved {
		t.Fatalf("no candidate moves attacker 10 onto the planeswalker: %+v", got)
	}
}

func TestLeafValueTerminalAndSquashed(t *testing.T) {
	w := state.PlayerID(0)
	if LeafValue(view.View{Over: true, Winner: &w}, 0) != 1 || LeafValue(view.View{Over: true, Winner: &w}, 1) != 0 || LeafValue(view.View{Over: true, Draw: true}, 0) != 0.5 {
		t.Fatal("terminal values")
	}
	v := view.View{Players: []view.PlayerView{{ID: 0, Life: 20}, {ID: 1, Life: 20}}}
	if LeafValue(v, 0) != 0.5 {
		t.Fatal("even position must be 0.5")
	}
	v.Players[1].Life = 10
	if x := LeafValue(v, 0); x <= 0.5 || x >= 1 {
		t.Fatalf("ahead position %v", x)
	}
}

func TestTeacherChoiceRollsEveryCandidateOnSampledWorlds(t *testing.T) {
	setup, h := samplingHistory(t)
	result, err := Sample(setup, h, SampleOptions{Seed: 44, Attempts: 8, Worlds: 2, MaxSubmits: 5000})
	if err != nil || len(result.Worlds) != 2 {
		t.Fatalf("sample %d worlds, %v", len(result.Worlds), err)
	}
	d := result.Worlds[0].Engine.Pending()
	var cands [][]Action
	for i := range d.Options {
		a, err := result.Worlds[0].Observer.Actions(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}})
		if err != nil {
			t.Fatal(err)
		}
		cands = append(cands, a)
		if len(cands) == 2 {
			break
		}
	}
	if len(cands) < 2 {
		t.Skip("fixture root offers a single option")
	}
	opts := TeacherOptions{Seed: 3, HorizonTurns: 1, MaxSubmits: 5000}
	got, err := TeacherChoice(result.Worlds, cands, opts)
	if err != nil || got.Rollouts != 4 || len(got.Values) != 2 {
		t.Fatalf("teacher %+v %v", got, err)
	}
	if len(got.WinsOverBaseline) != 2 || len(got.LossesToBaseline) != 2 || got.WinsOverBaseline[1]+got.LossesToBaseline[1] > 2 {
		t.Fatalf("paired outcomes do not describe the two common worlds: %+v", got)
	}
	again, err := TeacherChoice(result.Worlds, cands, opts)
	if err != nil || again.Index != got.Index || again.Values[0] != got.Values[0] || again.Values[1] != got.Values[1] {
		t.Fatalf("teacher not deterministic: %+v vs %+v", got, again)
	}
	// An unreachable margin keeps the bot's answer.
	opts.Margin = 2
	if kept, err := TeacherChoice(result.Worlds, cands, opts); err != nil || kept.Index != 0 {
		t.Fatalf("margin did not keep candidate 0: %+v %v", kept, err)
	}
	if _, err := TeacherChoice(nil, cands, opts); err == nil {
		t.Fatal("no worlds must be an error")
	}
}

func TestSampleMinESSRelaxesTheResamplingGate(t *testing.T) {
	setup, h := samplingHistory(t)
	strict, err := Sample(setup, h, SampleOptions{Seed: 44, Attempts: 2, Worlds: 64, MaxSubmits: 5000})
	if err != nil || len(strict.Worlds) != 0 {
		t.Fatalf("strict gate should refuse 64 worlds from 2 attempts: %d %v", len(strict.Worlds), err)
	}
	relaxed, err := Sample(setup, h, SampleOptions{Seed: 44, Attempts: 2, Worlds: 64, MaxSubmits: 5000, MinESS: 1})
	if err != nil {
		t.Fatal(err)
	}
	if relaxed.Accepted > 0 && len(relaxed.Worlds) != 64 {
		t.Fatalf("relaxed gate returned %d worlds from %d accepted", len(relaxed.Worlds), relaxed.Accepted)
	}
}

// TestTeacherRolloutsAreRecyclingInvariant pins that the parallel rollout path
// scores each candidate identically to the sequential one whether or not the
// two paths recycle their clone storage.
func TestTeacherRolloutsAreRecyclingInvariant(t *testing.T) {
	setup, h := samplingHistory(t)
	result, err := Sample(setup, h, SampleOptions{Seed: 44, Attempts: 8, Worlds: 2, MaxSubmits: 5000})
	if err != nil || len(result.Worlds) != 2 {
		t.Fatalf("sample %d worlds, %v", len(result.Worlds), err)
	}
	d := result.Worlds[0].Engine.Pending()
	var cands [][]Action
	for i := range d.Options {
		a, err := result.Worlds[0].Observer.Actions(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}})
		if err != nil {
			t.Fatal(err)
		}
		cands = append(cands, a)
		if len(cands) == 2 {
			break
		}
	}
	if len(cands) < 2 {
		t.Skip("fixture root offers a single option")
	}
	opts := TeacherOptions{Seed: 3, HorizonTurns: 1, MaxSubmits: 5000}
	seq, err := TeacherChoice(result.Worlds, cands, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Parallelism = 4
	par, err := TeacherChoice(result.Worlds, cands, opts)
	if err != nil {
		t.Fatal(err)
	}
	if seq.Index != par.Index || len(seq.Values) != len(par.Values) {
		t.Fatalf("sequential %+v != parallel %+v", seq, par)
	}
	for i := range seq.Values {
		if seq.Values[i] != par.Values[i] {
			t.Fatalf("value %d: sequential %v != parallel %v", i, seq.Values[i], par.Values[i])
		}
	}
}
