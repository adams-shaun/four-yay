package rules

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
