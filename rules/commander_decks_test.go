package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// repoCommanderGames is the m38 commander play-evidence table: one real
// Commander game per interim deck, each deck once as seat 0 (the seat whose
// commander the assert requires to actually be cast from the command zone).
// Matchups and seeds are pinned, not arbitrary: seeds are what made each
// game representative when this task measured them (a game that reaches a
// winner with the deck's commander cast). In particular the green deck —
// whose commander's own cost-reduction static this build does not read (see
// the AGENTS.md Known approximations / the m38 report), so Ghalta costs a
// flat 12 — is seated against the slowest of the five (Wretched Ranks).
// Fix round 1 measured under m34 free-for-all combat that whether one
// seeded game assembles twelve mana before it ends is close to a coin flip
// (pre-fix ~2/9 of seeds 1000-1040 cast Ghalta; post-fix the majority of
// seeds 1005-1015 cast it, but 1005-1015 still contains no-cast seeds), so
// the green deck carries a small declared seed set and the cast assert is
// existential over it (every game in the set must still reach a winner and
// replay byte-identically).
var repoCommanderGames = []struct {
	file string
	opp  string
	seed uint64
	bot  uint64
	// seeds, when non-empty, replaces seed: every game in the set must reach
	// a winner and replay to its own head, and the cast assert must hold in
	// at least one of them.
	seeds []uint64
}{
	{"foundations-calling-all-angels", "foundations-keen-engineering", 1000, 5000, nil},
	{"foundations-keen-engineering", "foundations-wretched-ranks", 1001, 5001, nil},
	{"foundations-wretched-ranks", "foundations-reign-of-dragons", 1002, 5002, nil},
	{"foundations-reign-of-dragons", "foundations-tramplesaurus-rex", 1003, 5003, nil},
	// 1018 was added when hybrid/Phyrexian costs became real alternative
	// payments and additional sacrifice costs started actually being paid:
	// the three original seeds still play and replay, but none of them ramps
	// into the commander any more, because Momentous Fall / Life's Legacy /
	// Harrow now genuinely require their sacrifices. The capability is intact
	// -- 10 of the 40 seeds in [1000,1040) still cast it -- so this is a
	// fixture that went stale against a correctness fix, not lost coverage,
	// and the "cast at least once" oracle is unchanged. 1018 casts twice,
	// which makes it the least fragile of the ten.
	{"foundations-tramplesaurus-rex", "foundations-wretched-ranks", 1005, 5005, []uint64{1005, 1015, 1009, 1018}},
	// 1008 was added when ForgetChanged$ True became real (the
	// ChangeZone hidden-origin/reveal param task): Troop of Ponies' second
	// leg now correctly sees the post-forget remembered set, its
	// ConditionDefined$ Remembered gate skips it, and the phantom shuffle the
	// skipped-but-running leg used to emit disappears -- the 1006 game's
	// course shifts, and it no longer ramps into its commander (a fixture
	// that went stale against a correctness fix, not lost coverage; 1008
	// casts once and replays).
	{"hearthhull-worldseed-landfall", "foundations-wretched-ranks", 1006, 5006, []uint64{1006, 1008}},
	// The Marvel Super Heroes Commander precon import (measured 2026-09-17):
	// against the slowest of the Foundations precons (the same foe every
	// other UR/WUR entry uses). A probe of [1000,1080) had every game reach
	// a winner and replay, and 44 of the 80 seeds cast the commander; the
	// declared set is three of the casting seeds (two with the deck winning),
	// so the cast assert has margin the way the tramplesaurus entry does
	// when a correctness fix shifts a game's course.
	{"avengers-assemble", "foundations-wretched-ranks", 1002, 5007, []uint64{1002, 1026, 1043}},
	// The Vivi Ornitier cEDH spellslinger-storm import (measured 2026-09-17,
	// the same slowest-foe pairing every UR/WUR entry uses): a probe of
	// [1000,1040) had every game reach a winner and replay, and 9 of the 40
	// seeds cast the commander (the bot does not storm, so the UR tempo deck
	// loses most long games — the cast assert has margin on the declared
	// three, two of them vivi wins). Vivi's own ActivationLimit$ mana
	// ability (the once-per-turn marker the cherry-picked fix records) is
	// live in these games; Rhystic Study/Mystic Remora's pay-or-draw asks
	// run through the unless gate's resolved-on-suspension record.
	{"vivi-ornitier-cedh", "foundations-wretched-ranks", 1019, 5008, []uint64{1019, 1024, 1038}},
	// The Pro Shaper player-submitted Commander import (measured 2026-09-19,
	// against the same slowest-foe pairing): its commander is Hearthhull, the
	// Worldseed -- a legendary Spacecraft with a printed P/T box -- which the
	// engine's old CR 903.3 predicate (no Vehicle/Spacecraft carve-out)
	// rejected at genesis, leaving the command zone empty (CmdCasts length
	// 0). The engine now delegates to deck.IsCommanderEligible, the predicate
	// the deck validator itself uses, so the deck seats; a probe of seeds
	// [1000,1060) with bot 5000+offset had 12/60 cast Hearthhull from the
	// command zone, and the declared three are casting seeds.
	{"pro-shaper", "foundations-wretched-ranks", 1000, 5000, []uint64{1000, 1013, 1019}},
}

// TestRepoCommanderDecksPlayAndCastTheirCommander is the m38 play evidence
// (Ruling M3-P): one real Commander game per interim deck — command zone,
// CR 903.8 tax on, 40 life, London mulligan — driven by the same botpolicy
// bot the acceptance suite uses, to completion, and replayed to the same
// chain head. Each deck must reach a winner (or the intent budget) AND
// cast its commander from the command zone at least once (CmdCasts runs
// parallel to Commanders, one slot per commander, incremented only on a
// command-zone cast — rules/cast.go's recordCmdCast). A deck file that
// never played, or whose commander never came down, fails here.
func TestRepoCommanderDecksPlayAndCastTheirCommander(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)

	for _, g := range repoCommanderGames {
		this, that := g.file, g.opp
		t.Run(this, func(t *testing.T) {
			t.Parallel()
			seeds := []uint64{g.seed}
			if len(g.seeds) > 0 {
				seeds = g.seeds
			}
			deck0 := testutil.RepoDeck(t, reg, this)
			deck1 := testutil.RepoDeck(t, reg, that)
			// The commander's flat position in each resolved deck is the
			// index Config.Commanders wants (genesis moves that object to
			// the command zone); deck.File owns the one shared resolution
			// (deck.CommanderIndex, m39) so gorged cannot disagree with the
			// engine on where a commander sits.
			cmdr0 := testutil.RepoDeckFile(t, this).CommanderIndex()
			cmdr1 := testutil.RepoDeckFile(t, that).CommanderIndex()
			maxCasts := int32(0)
			for _, seed := range seeds {
				cfg := Config{
					Seed:   seed,
					Names:  []string{this, that},
					Decks:  [][]*cards.Card{deck0, deck1},
					Tokens: reg.Tokens,
					Format: FormatCommander,
					// CR 903.9's 40-life start; the m31 CR 903.8 tax and m33
					// 21-damage clock run because FormatCommander is set.
					StartingLife: 40,
					Commanders: [][]int{
						{cmdr0},
						{cmdr1},
					},
					// Mulligans: 1 runs the London keep/mulligan round, as every
					// acceptance game does (R-M1); a commander game that cannot
					// survive its own mulligan round is a deck nobody plays.
					Mulligans: 1,
				}
				e := New(cfg)
				b := newTestBot(g.bot)
				e.Advance()
				n := 0
				for !e.G.Over && e.Pending() != nil && n < 400000 {
					if err := e.Submit(b.answer(e, e.Pending())); err != nil {
						t.Fatalf("%s vs %s, seed %d, intent %d: %v", this, that, seed, n, err)
					}
					n++
				}
				if !e.G.Over {
					t.Fatalf("%s vs %s, seed %d did not finish (turn %d, %d intents)", this, that, seed, e.G.Turn, n)
				}
				if got := e.G.Players[0].CmdCasts[0]; got > maxCasts {
					maxCasts = got
				}
				// Ruling P14: Draw before Winner — Winner's zero value is a real
				// seat (0), so read it only for a non-draw.
				result := "draw"
				if !e.G.Draw {
					result = e.G.Players[e.G.Winner].Name
				}
				t.Logf("%s: seed %d: %6d intents, %6d events, %3d turns, winner=%s, chain=%s",
					this, seed, n, len(e.L.Events), e.G.Turn, result, e.L.Head())

				re, err := replayFor(cfg, e.L)
				if err != nil {
					t.Fatalf("%s: replay: %v", this, err)
				}
				if re.L.Head() != e.L.Head() {
					t.Fatalf("%s: chain %s, replay %s", this, e.L.Head(), re.L.Head())
				}
			}
			if maxCasts < 1 {
				t.Errorf("%s: commander cast from the command zone 0 times across %d seeded game(s), want at least 1", this, len(seeds))
			}
		})
	}
}
