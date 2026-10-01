package azmcts

import (
	"context"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// landPlayRoot is the first seat-0 priority the bot answers with a land
// play (TestSearchSkipsWhenTheBotPlaysALand's position).
func landPlayRoot(t *testing.T) (*rules.Engine, *decision.Decision, decision.Intent) {
	t.Helper()
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e := rules.New(cfg)
	e.Advance()
	rngs := searchprobe.BotRandoms(cfg.Seed, 2)
	board := botpolicy.NewBoard(2)
	for steps := 0; steps < 2000 && !e.G.Over; steps++ {
		d := e.Pending()
		in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
		if d.Player == 0 && d.Kind == decision.KPriority && len(in.Choices) == 1 && d.Options[in.Choices[0]].Kind == "play_land" {
			return e, d, in
		}
		if err := e.Submit(in); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("seat 0 never played a land")
	return nil, nil, decision.Intent{}
}

// The full root searches a priority whose bot answer is outside the
// vocabulary: a macro is a candidate played as all its steps in every
// simulation; keyed BotKey it is the bot's candidate, otherwise there is no
// bot candidate and Pass is first. A root whose only candidate is Pass is
// still skipped (genuinely single-option).
func TestFullRootSearchesOutsideTheVocabulary(t *testing.T) {
	e, d, land := landPlayRoot(t)
	{
		obs := searchprobe.NewCollector(d.Player)
		opts := DefaultOptions()
		opts.AutoPayment = true
		res, err := Search(context.Background(), Root{Engine: e, Decision: d, Bot: land, Observer: obs, NoBot: true}, &countingSource{}, nil, opts)
		if err != nil {
			t.Fatal(err)
		}
		if res.Stats.Skipped != 1 || res.Stats.KindSkipped[KindPriority][SkipFewCandidates] != 1 {
			t.Fatalf("a pass-only full root: stats %+v, want one few-candidates skip", res.Stats)
		}
	}
	o := d.Options[land.Choices[0]]
	macro := Macro{Key: MacroKeyPrefix + "land", Label: "Play " + o.Label, Steps: []MacroStep{{
		Player: d.Player, Kind: d.Kind, Picks: []rules.ScriptPick{{Kind: o.Kind, Obj: o.Obj, Label: o.Label, ManaSymbol: o.ManaSymbol}},
	}}}
	opts := DefaultOptions()
	opts.Sims, opts.AutoPayment = 12, true
	for name, root := range map[string]Root{
		"no bot":       {Macros: []Macro{macro}},
		"macro is bot": {Macros: []Macro{macro}, BotKey: macro.Key},
	} {
		t.Run(name, func(t *testing.T) {
			obs := searchprobe.NewCollector(d.Player)
			src, err := newTestClairvoyant(e, obs)
			if err != nil {
				t.Fatal(err)
			}
			root.Engine, root.Decision, root.Bot, root.Observer = e, d, land, obs
			res, err := Search(context.Background(), root, src, nil, opts)
			if err != nil {
				t.Fatal(err)
			}
			st := res.Stats
			if st.Skipped != 0 || st.Searched != 1 || st.Completed != opts.Sims || len(res.Keys) < 2 {
				t.Fatalf("stats %+v keys %v, want a searched root", st, res.Keys)
			}
			first := res.Keys[0]
			switch name {
			case "no bot":
				if res.Candidates[0].Choices == nil || d.Options[res.Candidates[0].Choices[0]].Kind != "pass" || res.Keys[len(res.Keys)-1] != macro.Key {
					t.Fatalf("candidates %v, want Pass first and the macro last", res.Labels)
				}
			case "macro is bot":
				if first != macro.Key || res.Labels[0] != macro.Label || res.Visits[0] == 0 {
					t.Fatalf("candidate 0 %s (%s, %d visits), want the visited land macro", first, res.Labels[0], res.Visits[0])
				}
			}
			for _, k := range res.Keys {
				if k == "" {
					t.Fatal("empty key")
				}
			}
			// The answer is the chosen candidate's intent, never the land
			// play unless the land macro was chosen.
			if res.Choice < 0 || (res.Keys[res.Choice] != macro.Key && res.Intent.Choices != nil && len(res.Intent.Choices) == 1 && d.Options[res.Intent.Choices[0]].Kind == "play_land") {
				t.Fatalf("choice %d intent %+v", res.Choice, res.Intent)
			}
		})
	}
}
