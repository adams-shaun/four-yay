package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// knownUnsupported is the M1 coverage RATCHET (Ruling P12/D2-a): the exact
// card -> missing-primitive set TestEveryRepoDeckIsFullySupported measured
// on its first run against the 12 repo decks, checked in by hand. The test
// below asserts the MEASURED set equals this table EXACTLY, in both
// directions -- a newly-missing card is a regression, a table entry that is
// now fully supported is stale and must be deleted -- so, for a fixed deck
// catalogue, this table only ever shrinks, and only by implementing a real
// primitive (Ruling W2: this project does not grow the "supported" set just
// to make the ratchet green). Adding a newly imported deck can extend the
// measured worklist; its entries must be measured from the compiled corpus,
// never marked supported without an implementation.
//
// Measured 2026-09-04 against the compiled IR cache at .cards/ir.gob.gz
// (corpus master @ 95f04e8a04c8925fa97cb226fc3341cabcc90a53): originally 35
// of the 136 distinct cards across the 12 decks needed at least one
// primitive this build did not implement -- overwhelmingly individual
// keywords (kw:Equip, kw:Flash, kw:Kicker, kw:Delve, kw:Undying,
// kw:etbCounter, kw:Storm, and so on) rather than whole missing APIs. The
// M2r plan's "Ratchet schedule" table
// (docs/superpowers/plans/2026-09-05-gorge-m2r-ratchet-to-zero.md) is the
// authority on which task retires each entry, down to 0; this table stood
// at 31 after Task 3 (kw:Flash, kw:Indestructible and kw:Devoid retire
// Snapcaster Mage, Spectral Sailor, Ulamog and World Breaker), at 29 after
// Task 13 (api:Token registers, deleting Wurmcoil Engine's and Young
// Pyromancer's entries outright and shrinking Batterskull's, Empty the
// Warrens' and Entreat the Angels' entries to whatever else they still need
// -- kw:Living Weapon's own Attach/Equip is Task 14's, Storm and
// CopySpellAbility and Miracle are each a separate, still-open primitive),
// and now stands at 22 (Task 9: kw:Kicker, kw:Surge, kw:Flashback and
// kw:Delve retire Gatekeeper of Malakir, Goblin Bushwhacker, Vines of
// Vastwood, Reckless Bushwhacker, Cabal Therapy, Gurmag Angler and
// Tombstalker), and at 18 after Task 16 (kw:Undying, kw:Evolve, kw:Exalted
// and kw:Prowess register: Geralf's Messenger, Strangleroot Geist, Experiment
// One and Monastery Swiftspear retire outright, and Knight of Infamy shrinks
// to just its still-open kw:Protection from white). Every unimplemented
// ability on these
// cards is inert for the acceptance run (Ruling U13's Sword of Fire and Ice
// note is this same shape, one card up): the point of Task 26 is that the
// games terminate, invariants hold and replay is exact with these cards
// shuffled in, not that every card plays with full fidelity yet -- that is
// M4's coverage work, and this table is its worklist.
//
// Task 14 (attachments) registered api:Attach, kw:Equip, kw:Enchant and
// kw:Living Weapon, deleting Batterskull, Rancor, Sword of Fire and Ice and
// Umezawa's Jitte outright -- the four entries whose only gaps were those
// primitives.
//
// Merge wt/r14 <- main (Task 14 fix round 1): main's M2r tasks retired nine
// of the eighteen post-Task-16 entries before this branch merged -- Task 18's
// kw:Miracle (Entreat the Angels, Terminus), Task 12's ETB replacement +
// as-enters choice machinery (api:ChooseType/api:ChooseNumber/kw:ETBReplacement
// /kw:etbCounter: Cavern of Souls, Phyrexian Revoker, Pithing Needle, Sanctum
// Prelate, Chalice of the Void, Endless One, Walking Ballista) -- and Task 14
// above retired the four attachment entries, so the merged table is exactly
// the five entries BOTH sides still listed: the two CopySpellAbility+Storm
// pairs, and the two Protection entries, nothing else. It now stands at 5.
//
// Task 17 (Storm and CopySpellAbility) registered api:CopySpellAbility and
// kw:Storm, deleting Chain Lightning, Empty the Warrens and Tendrils of Agony
// outright -- three more of the post-Task-16 entries -- leaving only the two
// Protection entries.
//
// Task 15 (protection) registered the five kw:Protection from <colour>
// keywords, retiring Goblin Piledriver (Protection from blue) and Knight of
// Infamy (Protection from white) -- the last two entries on this table -- so
// the ratchet became EMPTY for the original Legacy and interim Commander
// decks. The Hearthhull and Valgavoth imports add 54 measured gaps across 579
// distinct cards in the pinned corpus; entries retire only when their
// primitives are implemented. The untap/mana wave (api:Untap, api:ManaReflected,
// stat:ManaConvert, stat:UntapOtherPlayer and kw:Cumulative upkeep)
// retired Fabled Passage, Exotic Orchard, Baloth Prime, Horizon Explorer and
// Chromatic Orrery outright (every entry's primitives are implemented). The
// one recorded remainder for Horizon Explorer is the param-level gap
// `param:api:Untap.ETB` in paramcensus_test.go's knownUnsupportedParams.
//
// Task altcosts (the alternative-cost keyword family) retired the one entry
// the merged decks carried: kw:Dash (Ragavan, Nimble Pilferer), implemented
// to CR 702's dash shape in rules/altcast.go with a named proof test in
// rules/altcast_test.go. kw:Evoke, kw:Encore, kw:Overload, kw:Warp,
// kw:Madness and kw:AlternateAdditionalCost registered in the same task for
// the commander decks then held: Rakdos Scam.exe and Ulalek Eldrazi have
// since been imported and measured fully supported (no entry of theirs ever
// landed here); the Vivi cEDH deck the task also named was never imported.
var knownUnsupported = map[string][]string{
	// The avengers-assemble Commander deck import (the Marvel Super Heroes
	// Commander precon, measured 2026-09-17) held "Avengers Quinjet":
	// {"kw:Crew"}. The entry was deleted when kw:Crew registered
	// (cards/kw_crew.go, effects/crew.go): the Vehicle crews through the
	// tapXType<Any/Creature.Other+withTotalPowerGE<N>> cost the expansion
	// mints, whose set-level power floor the tap-cost machinery enforces --
	// the real card test rules/crew_test.go's
	// TestCrewAvengersQuinjetAnimatesUntilEndOfTurn drives the activation
	// end to end, which is what licensed the shrink. Everything else the
	// deck exposed (kw:Crew's siblings trig:AttackersDeclared, trig:Cycled,
	// trig:CounterAdded, trig:AttackerBlocked, the PresentZone$ clause) was
	// implemented and is pinned in rules/msh_commander_trigger_test.go.
	// Captain Marvel, Apex Avenger's trig:CounterPlayerAddedAll entry was
	// deleted when the mode was registered (trigmatch_counters.go's
	// counterPlayerAddedAllMatches) -- its own trigger's ValidObject$
	// Creature...+nonKree spec still fails closed on the unknown nonKree
	// predicate (a filter-vocabulary gap, ledgered), so the trigger is
	// primitive-supported but silent; the card's OTHER gap
	// (param:api:PutCounter.TriggeredCounterMap) stays in the param census.
	// Speed, Young Avenger's api:ImmediateTrigger entry was deleted when the
	// API was registered (effects/immediate.go): the real card test
	// rules/forum_filibuster_test.go's TestSpeedYoungAvengerImmediateTrigger
	// pays its AB Cost$ 1 through the triggered-cost window and executes
	// TrigEffect, which is what licensed the shrink.
	//
	// The pro-shaper player-submitted Commander import (2026-09-18): Earthbend
	// (7 cards) and Clone (2) were unregistered APIs; Devour (1) is an
	// unregistered primitive. api:Earthbend's seven carriers left this table
	// when the primitive registered (effects/earthbend.go): the real card
	// tests rules/earthbend_test.go drive Ba Sing Se's own AB$ Earthbend and
	// the trigger-Execute carriers end to end, which is what licensed the
	// shrink. Clone's two left earlier (effects/clone.go). api:GenericChoice
	// left when the primitive registered (effects/misc.go,
	// GenericChoice -> effCharm) -- Tireless Provisioner and Torment of
	// Hailfire are fully supported now, pinned in
	// rules/generic_choice_test.go. kw:Hexproof was implemented in the same
	// change (rules/protection.go hexproofBlocksTarget, pinned in
	// rules/hexproof_test.go), so its two carriers -- Lotus Field and Tectonic
	// Split -- are deliberately absent here: they are fully supported now.
	// Shifting Woodland and Vesuva's api:Clone entries were deleted when
	// api:Clone was registered (effects/clone.go): the real card test
	// rules/clone_api_test.go's TestMirageMirrorBecomesACopyOfTargetCreature
	// drives the standalone DB$ Clone through the layer-1 CopyFace basis,
	// its Duration$ expiry and the NewName$/GainThisAbility$ riders, which
	// is what licensed the shrink, together with
	// rules/clone_overlap_test.go's TestShiftingWoodlandCopiesAGraveyardCard,
	// which drives Shifting Woodland's own standalone A:AB$ Clone (the
	// TgtZone$ Graveyard copy source the battlefield sweep never reaches).
	// Vesuva reaches api:Clone ONLY through the still-unimplemented
	// ETB-replacement route (the open ETB-copy ticket), so its shrink is the
	// mechanically forced one: the measured gap set no longer holds it
	// (the primitive it names IS registered) and leaving the entry in place
	// would fail the ratchet as stale. That narrowing is recorded in
	// AGENTS.md's api:Clone row.
	// The pro-shaper/Commander cards whose gap was previously invisible
	// because Primitive() walked the Sub chain only: an SVar-naming
	// parameter (Charm's Choices$, Repeat's RepeatSubAbility$) resolved
	// the body at runtime, so Face.Primitives never surfaced the API
	// (prims1). Vision, Synthezoid Avenger's entry was deleted when
	// api:Phases was registered (effects/phases.go, task phases1): its
	// Charm branch reaches `DB$ Phases | Defined$ Self`, and the real card
	// tests rules/phases_test.go (Talon Gates of Madara, Guardian of
	// Faith) drive the primitive end to end, so the measured gap set no
	// longer holds it and leaving the entry would fail the ratchet as
	// stale.
	// Raw Effect child census correction surfaced these existing unsupported
	// capabilities; the underlying primitives were not changed.
	"Incinerate":        {"stat:CantRegenerate"},
	"Vines of Vastwood": {"stat:CantTarget"},
	// fuzz-cov3's registration-honesty gate (cards.Registry.Unsupported now
	// checks the count heads a card's referenced value SVars read): Temple of
	// Power's transform gate reads Count$NonCombatDamageThisTurn Card.Red+
	// YouCtrl Any, a source-filtered noncombat damage tally the log cannot
	// answer (a Damage event does not record its source). The gate failed
	// OPEN (an AB's CheckSVar$), so the land transformed unconditionally; it
	// was never fully supported, only counted so. Also held by
	// count_head_ratchet_test.go's knownUnmodelledCountHeads.
	"Ojer Axonil, Deepest Might": {"count:NonCombatDamageThisTurn"},
}

// TestEveryRepoDeckIsFullySupported is the M1 coverage ratchet: every card
// across every deck file (the 12 Legacy decks and the m38 commander decks)
// is either fully playable, or is named in
// knownUnsupported with the exact primitives it is missing. A card missing
// something knownUnsupported does not list is a regression (fails); a card
// knownUnsupported lists that is now fully supported is a stale entry that
// must be deleted from the table (also fails) -- the table can drift out of
// sync in either direction as the engine grows, and both directions are a
// bug in the ratchet, not something to silently tolerate.
func TestEveryRepoDeckIsFullySupported(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	measured := map[string][]string{}
	total := 0
	seen := map[string]bool{}
	for _, name := range testutil.RepoDeckNames() {
		for _, c := range testutil.RepoDeck(t, reg, name) {
			cardName := c.Faces[0].Name
			if seen[cardName] {
				continue
			}
			seen[cardName] = true
			total++
			if m := reg.Unsupported(c, supported); len(m) > 0 {
				measured[cardName] = m
			}
		}
	}
	t.Logf("ratchet: %d of %d distinct cards across the repo decks are not fully supported",
		len(measured), total)

	for card, gotPrims := range measured {
		want, ok := knownUnsupported[card]
		if !ok {
			t.Errorf("%s needs %v, which is not in knownUnsupported -- new gap, add it to the ratchet table", card, gotPrims)
			continue
		}
		if !sameSet(want, gotPrims) {
			t.Errorf("%s: knownUnsupported says %v, measured %v -- update the ratchet table to match", card, want, gotPrims)
		}
	}
	for card, want := range knownUnsupported {
		if _, stillMissing := measured[card]; !stillMissing {
			t.Errorf("%s is fully supported now (was missing %v) -- delete it from knownUnsupported", card, want)
		}
	}
}

// sameSet reports whether a and b hold the same strings, order-independent
// and duplicate-independent -- cards.Card.Primitives (and therefore
// Registry.Unsupported) already returns a sorted, deduplicated slice, so in
// practice this only ever needs to reject a genuine content difference, not
// tolerate reordering, but checking it this way keeps the comparison
// correct even if that guarantee ever loosens.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	am := map[string]int{}
	for _, s := range a {
		am[s]++
	}
	for _, s := range b {
		am[s]--
	}
	for _, n := range am {
		if n != 0 {
			return false
		}
	}
	return true
}

// playAcceptance plays one deterministic acceptance game -- seats seats,
// round-robined across the 12 pinned Legacy decks from
// testutil.LegacyDeckNames() in order starting from deck 0, seed 42 driven
// by newTestBot(7) -- to completion, replays it from its own recorded
// (Config, Log) through the package-local replayFor helper, and Fatals if
// the two chain Heads disagree. step, when non-nil, is called once right
// after Advance (n==0, before any intent), once every 997 intents
// thereafter, and once more after the game loop exits, so a caller that
// wants per-checkpoint work (invariant checks, logging) can hook into the
// one game loop instead of keeping a second copy of it.
//
// TestRepoDecksPlayAtEverySeatCount and acceptanceHead (rules/heads_test.go's
// TestHeads) both call this, so the games the invariant/replay guarantees
// are checked against and the games the chain-head goldens pin can never
// silently drift apart from each other.
//
// The pool is LegacyDeckNames, never RepoDeckNames (which also lists the
// five interim commander decks, Ruling M38-P): the golden heads are
// byte-identical games over the 12 constructed decks, and seating them by
// index over a directory that grew would move every head for no behavioural
// reason.
func playAcceptance(t *testing.T, reg *cards.Registry, seats int, step func(e *Engine, n int)) string {
	t.Helper()
	all := testutil.LegacyDeckNames()
	names := make([]string, seats)
	decks := make([][]*cards.Card, seats)
	for i := 0; i < seats; i++ {
		names[i] = all[i%len(all)]
		decks[i] = testutil.RepoDeck(t, reg, all[i%len(all)])
	}
	cfg := Config{Seed: 42, Names: names, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Cards,
		// Ruling R-M1: the mulligan is NOT configurable off for the acceptance
		// decks -- a mulligan the suite never exercises is a mulligan nobody
		// tests. Mulligans = 1 makes the keep/mulligan and bottoming round run
		// in every acceptance game (M2d-1), which is why the four chain heads
		// in rules/heads_test.go move; standalone fixture Configs never set it,
		// so the zero value leaves them byte-identical to before (R-8.4).
		Mulligans: 1}
	// CR 103.1's winner-chooses ask: the choice constructor defers the
	// pregame rounds, and the pose lets the bot answer it, the same decision
	// a real table conveys to the toss winner.
	e := NewStartingPlayerChoice(cfg)
	b := newTestBot(7)
	e.AskStartingPlayer()
	e.Advance()
	if step != nil {
		step(e, 0)
	}
	n := 0
	for !e.G.Over && e.Pending() != nil && n < 400000 {
		// answer builds the board off the engine itself (botpolicy.BoardFromGame);
		// see rules/testbot_test.go's own doc.
		if err := e.Submit(b.answer(e, e.Pending())); err != nil {
			t.Fatalf("%d seats, intent %d: %v", seats, n, err)
		}
		if step != nil && n%997 == 0 {
			step(e, n)
		}
		n++
	}
	if step != nil {
		step(e, n)
	}
	if !e.G.Over {
		t.Fatalf("%d-seat game did not finish (turn %d, %d intents)", seats, e.G.Turn, n)
	}
	re, err := replayFor(cfg, e.L)
	if err != nil {
		t.Fatalf("%d seats: %v", seats, err)
	}
	if re.L.Head() != e.L.Head() {
		t.Fatalf("%d seats: chain %s, replay %s", seats, e.L.Head(), re.L.Head())
	}
	return e.L.Head()
}

// acceptanceHead is playAcceptance with no per-checkpoint hook: just the
// finished game's chain Head, for rules/heads_test.go's TestHeads to pin.
func acceptanceHead(t *testing.T, reg *cards.Registry, seats int) string {
	return playAcceptance(t, reg, seats, nil)
}

// TestRepoDecksPlayAtEverySeatCount is the M1 acceptance gate: the 12 repo
// decks, round-robined across 2, 4, 6 and 8 seats, must each play a
// complete game -- termination under budget, every invariant holding
// throughout -- with the cards knownUnsupported lists still shuffled in and
// simply inert (Ruling: this task is about the engine surviving real Forge
// data at scale, not about every card's fidelity, which is M4's separate
// worklist).
func TestRepoDecksPlayAtEverySeatCount(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	for _, seats := range []int{2, 4, 6, 8} {
		seenStart := false
		playAcceptance(t, reg, seats, func(e *Engine, n int) {
			label := fmt.Sprintf("%d-seat mid", seats)
			switch {
			case !seenStart:
				label = fmt.Sprintf("%d-seat start", seats)
				seenStart = true
			case e.G.Over:
				label = fmt.Sprintf("%d-seat end", seats)
				// Ruling P14: Draw before Winner -- Winner's zero value is
				// seat 0, a real seat, so reading it unconditionally would
				// misreport a drawn game as "seat 0 won".
				result := "draw"
				if !e.G.Draw {
					result = e.G.Players[e.G.Winner].Name
				}
				t.Logf("%d seats: %6d intents, %6d events, %3d turns, winner=%s, chain=%s",
					seats, n, len(e.L.Events), e.G.Turn, result, e.L.Head())
			}
			testutil.CheckInvariants(t, e.G, e.Pending(), label)
		})
	}
}

// TestRepoDeckGamesReplayExactly ties acceptance to the replay guarantee:
// five seeded 4-seat games over the pinned Legacy decks (the same 12
// LegacyDeckNames the golden seat-count games and heads use, Ruling M38-P),
// each re-run from its own recorded (Config, Log) through the package-local
// replayFor helper, must reach the same chain Head as the original run.
func TestRepoDeckGamesReplayExactly(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	all := testutil.LegacyDeckNames()
	for seed := uint64(0); seed < 5; seed++ {
		names := make([]string, 4)
		decks := make([][]*cards.Card, 4)
		for i := 0; i < 4; i++ {
			names[i] = all[(int(seed)+i)%len(all)]
			decks[i] = testutil.RepoDeck(t, reg, names[i])
		}
		cfg := Config{Seed: seed, Names: names, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Cards,
			// R-8.4: Mulligans must travel in the same Config replay is handed;
			// this replay-exactness test exercises the round so a concession of
			// mutating it silently would be caught here (M2d-1).
			Mulligans: 1}
		e := New(cfg)
		b := newTestBot(seed)
		e.Advance()
		n := 0
		for !e.G.Over && e.Pending() != nil && n < 400000 {
			if err := e.Submit(b.answer(e, e.Pending())); err != nil {
				t.Fatalf("seed %d, intent %d: %v", seed, n, err)
			}
			n++
		}
		if !e.G.Over {
			t.Fatalf("seed %d did not terminate after %d intents (turn %d)", seed, n, e.G.Turn)
		}
		re, err := replayFor(cfg, e.L)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if re.L.Head() != e.L.Head() {
			t.Fatalf("seed %d chain %s, replay %s", seed, e.L.Head(), re.L.Head())
		}
		t.Logf("seed %d: %d intents, chain %s, replay OK", seed, n, e.L.Head())
	}
}

// replayFor is a thin, package-local mirror of replay.Replay (Task 24):
// this test file is package rules and the replay package imports rules, so
// importing it here would be a cycle (Ruling in the supplement's ¶0). It
// reruns l's recorded Intents against a fresh Engine built from cfg -- the
// same (Config, Log) contract replay.Replay documents -- and returns the
// resulting engine; the replay package's own exported API and its own
// tests are what every other caller uses.
func replayFor(cfg Config, l *events.Log) (*Engine, error) {
	cfg.Seed = l.Seed
	// CR 103.1's winner-chooses ask is conditional: a log that recorded it
	// carries a DecisionAsk for the kind, so mirror replay.Replay's own
	// conditional constructor here (this is the package-local mirror, per
	// the doc above).
	var e *Engine
	asked := startingPlayerAsked(l)
	if asked {
		e = NewStartingPlayerChoice(cfg)
	} else {
		e = New(cfg)
	}
	if asked {
		// Re-pose the recorded ask, before Advance's R-9 fallback would
		// default it, so the recorded choice Intent lands on the decision it
		// answered.
		if e.AskStartingPlayer() == nil {
			return e, fmt.Errorf("replayFor: log recorded a starting_player ask but none is available")
		}
	}
	e.Advance()
	for i := 0; i < len(l.Intents); i++ {
		if e.G.Over {
			break
		}
		if e.Pending() == nil {
			return e, fmt.Errorf("replayFor: no decision pending at intent %d", i)
		}
		if err := e.Submit(l.Intents[i]); err != nil {
			return e, fmt.Errorf("replayFor: intent %d rejected: %w", i, err)
		}
	}
	return e, nil
}
