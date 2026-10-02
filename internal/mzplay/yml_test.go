package mzplay

import (
	"os"
	"strings"
	"testing"
)

// testdata/game_loop.yml is what draft-zero's loop.game_yml writes
// (yaml.safe_dump of its base game.yml with the per-job keys set), generated
// by running that code path.
func TestParseConfigReadsTheLoopsGameYML(t *testing.T) {
	c, err := LoadConfig("testdata/game_loop.yml")
	if err != nil {
		t.Fatal(err)
	}
	a := PlayerConfig{
		DeckPool: "/run/root/runs/2026-10-02_15-00-00/.pools/train.txt", DeckPoolMode: "random", Type: "mcts",
		OutputFile:       "/run/root/data/FDN_exp2/ver1/testing/session3_A_self.hdf5",
		PriorTemperature: 1.5, SearchBudget: 300, TimeoutMS: 60000, TDDiscount: 0.95, BackpropDiscount: 0.99,
	}
	b := a
	b.DeckPool, b.DeckPoolMode = "/run/root/runs/2026-10-02_15-00-00/.pools/eval_B.txt", "sequential"
	b.OutputFile, b.OfflineMode = "/run/root/runs/2026-10-02_15-00-00/scratch/eval_offline_B.hdf5", true
	want := Config{GoesFirst: "random", GameMode: "normal", A: a, B: b, Games: 4, Threads: 4, MaxTurns: 50, MaxMinutes: 50,
		Host: "localhost", Port: 50052, OpponentPort: 50053}
	if c != want {
		t.Fatalf("config:\n got %+v\nwant %+v", c, want)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("the loop's own game.yml is refused: %v", err)
	}
}

// The base configs/game.yml carries comments, a blank-valued key with a
// comment-only block, and quoted scalars.
func TestParseConfigCommentsDefaultsAndQuotes(t *testing.T) {
	src := `# game.yml
goes_first: player_b  # player_a | player_b | random

player_a:
  deckPath: "xmage/decks/UW Tempo #1.dck"   # a '#' inside quotes is not a comment
  type: mcts
  # output_file: set by runner
  mcts:
    search_budget: 4000
    td_discount: 0.85
  hiddenInfo:
    see_opponent_hand: false
player_b:
  deckPath: 'it''s.dck'
  output_file:
  mcts:
    search_budget: 50.0
training:
  games: 200
server:
  port: 50052
`
	c, err := ParseConfig(src)
	if err != nil {
		t.Fatal(err)
	}
	if c.GoesFirst != "player_b" || c.A.DeckPath != "xmage/decks/UW Tempo #1.dck" || c.B.DeckPath != "it's.dck" {
		t.Fatalf("scalars: %+v", c)
	}
	if c.A.SearchBudget != 4000 || c.A.TDDiscount != 0.85 || c.B.SearchBudget != 50 {
		t.Fatalf("numbers: a %+v b %+v", c.A, c.B)
	}
	// Config.java's defaults for everything absent.
	if c.A.TimeoutMS != 4000 || c.B.TDDiscount != 0.95 || c.A.BackpropDiscount != 0.99 || c.A.DeckPoolMode != "random" ||
		!c.A.Mulligans || c.A.SeeOpponentHand || !c.B.SeeOpponentHand || c.B.OutputFile != "" ||
		c.Threads != 2 || c.MaxTurns != 50 || c.MaxMinutes != 20 || c.Host != "localhost" || c.OpponentPort != 8081 || c.Games != 200 {
		t.Fatalf("defaults: %+v", c)
	}
}

func TestParseConfigRefusesShapesItDoesNotRead(t *testing.T) {
	base := "player_a:\n  type: mcts\nplayer_b:\n  type: mcts\ntraining:\n  games: 1\nserver:\n  port: 1\n"
	for name, tc := range map[string]struct{ src, want string }{
		"sequence":        {base + "decks:\n  - a\n", "sequence"},
		"flow map":        {base + "eval: {pairs: 1}\n", "flow collection"},
		"block scalar":    {base + "note: |\n  text\n", "block scalar"},
		"continuation":    {"player_a:\n  deck_pool: /a long\n    path\nplayer_b:\n  type: mcts\ntraining:\n  games: 1\nserver:\n  port: 1\n", "multi-line"},
		"duplicate key":   {base + "server:\n  port: 2\n", "duplicate key"},
		"bad bool":        {strings.Replace(base, "type: mcts\nplayer_b", "type: mcts\n  mcts:\n    offline_mode: yes\nplayer_b", 1), "true or false"},
		"bad int":         {strings.Replace(base, "games: 1", "games: many", 1), "an integer"},
		"missing block":   {"player_a:\n  type: mcts\ntraining:\n  games: 1\nserver:\n  port: 1\n", "no player_b block"},
		"scalar as block": {"player_a: mcts\nplayer_b:\n  type: mcts\ntraining:\n  games: 1\nserver:\n  port: 1\n", "player_a is not a mapping"},
	} {
		_, err := ParseConfig(tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want one naming %q", name, err, tc.want)
		}
	}
}

// Every switch this engine does not implement is refused by name.
func TestValidateRefusesWhatIsNotImplemented(t *testing.T) {
	raw, err := os.ReadFile("testdata/game_loop.yml")
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct{ old, new, want string }{
		"priority prior": {"    priority: false\n    target: false\n    binary: false\n    opponent: false\n    prior_temperature: 1.5\n  noise:\n    enabled: false\n    dirichlet_noise: 0.15\n    selection_temperature: 2\n  mcts:\n    search_budget: 300\n    timeout_ms: 60000\n    td_discount: 0.95\n    offline_mode: false",
			"    priority: true\n    target: false\n    binary: false\n    opponent: false\n    prior_temperature: 1.5\n  noise:\n    enabled: false\n    dirichlet_noise: 0.15\n    selection_temperature: 2\n  mcts:\n    search_budget: 300\n    timeout_ms: 60000\n    td_discount: 0.95\n    offline_mode: false", "player_a.priors is switched on"},
		"binary prior on b": {"    binary: false\n    opponent: false\n    prior_temperature: 1.5\n  noise:\n    enabled: false\n    dirichlet_noise: 0.15\n    selection_temperature: 2\n  mcts:\n    search_budget: 300\n    timeout_ms: 60000\n    td_discount: 0.95\n    offline_mode: true",
			"    binary: true\n    opponent: false\n    prior_temperature: 1.5\n  noise:\n    enabled: false\n    dirichlet_noise: 0.15\n    selection_temperature: 2\n  mcts:\n    search_budget: 300\n    timeout_ms: 60000\n    td_discount: 0.95\n    offline_mode: true", "player_b.priors is switched on"},
		"minimax":    {"deck_pool_mode: sequential\n  output_file", "type: minimax\n  deck_pool_mode: sequential\n  output_file", ""},
		"mulligans":  {"mulligans_enabled: false", "mulligans_enabled: true", "mulligans"},
		"manual tap": {"manual_tapping: false", "manual_tapping: true", "manual_tapping"},
		"commander":  {"goes_first: random", "goes_first: random\ngame_mode: commander", "game_mode"},
		"noise":      {"enabled: false", "enabled: true", "noise.enabled"},
		"goes first": {"goes_first: random", "goes_first: coin", "goes_first"},
		"pool mode":  {"deck_pool_mode: random", "deck_pool_mode: shuffled", "deck_pool_mode"},
	} {
		src := strings.Replace(string(raw), tc.old, tc.new, 1)
		if src == string(raw) {
			t.Fatalf("%s: the fixture does not hold %q", name, tc.old)
		}
		if name == "minimax" {
			// "type" appears twice in one block: a duplicate key, refused by the reader.
			if _, err := ParseConfig(src); err == nil || !strings.Contains(err.Error(), "duplicate key") {
				t.Errorf("minimax: %v, want a duplicate key error", err)
			}
			src = strings.Replace(string(raw), "player_b:\n  deckPath: ''\n  type: mcts", "player_b:\n  deckPath: ''\n  type: minimax", 1)
			tc.want = "player_b.type"
		}
		c, err := ParseConfig(src)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Validate %v, want an error naming %q", name, err, tc.want)
		}
	}
}
