package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// acceptanceHeads pins the chain head of the deterministic acceptance game
// at each seat count (R-14). A change here is a change to what the 12 repo
// decks do; the commit that makes it must name the card behaviour that
// moved it. The seed, bot and deck assignment are TestRepoDecksPlayAtEverySeatCount's
// own (rules/acceptance_test.go's playAcceptance), so the two tests always agree.
// M2d moved all four heads, and owns exactly ONE regeneration -- this one,
// at the milestone merge gate, not once per sub-task. Build-bisected causes
// (FL-76), measured per seat count rather than assumed:
//
//   - M2d-1, the London mulligan (R-M1): moves ALL FOUR. playAcceptance now
//     sets Mulligans: 1, so every acceptance game runs a keep/mulligan round
//     and, for any seat that mulligans, a re-shuffle, a fresh seven and a
//     bottoming round -- all of it emitted, so every head shifts.
//     Intermediate heads after M2d-1 alone: 2 45e0671d07b60d9e,
//     4 dcee545be139ca21, 6 642a239a24d3a1fe, 8 acf4ad4bafda267f.
//
//   - M2d-2, KModes and the mid-resolution ask (R-8): moves 4, 6 and 8 only.
//     It emits the new ModeChosen kind, and counting that kind in each
//     acceptance log gives 2 seats: 0, and 4/6/8 seats: 1 apiece. So the
//     2-seat golden below is M2d-1's value, unchanged by M2d-2.
//
//     An earlier version of this comment explained the 2-seat zero by saying
//     dimir-tempo's Spell Pierce carries UnlessCost$ 2 and is "served through
//     this same kind", just never cast under this seed. That is wrong, and
//     the correction matters more than the head does: effCounter
//     (effects/misc.go) never reads UnlessCost$ at all. Spell Pierce, Mana
//     Leak, Daze and Mausoleum Wanderer counter unconditionally, so casting
//     one would emit no ModeChosen either. M2d-2 closed R-8 for
//     CopySpellAbility only -- see AGENTS.md's Known approximations. The
//     per-seat COUNT above was measured and stands; only the explanation of
//     the zero was wrong.
//
//   - M2d-3, concede (R-M3): moves NOTHING. It only adds an option to every
//     priority decision, and neither bot mirror ever picks it; an offered
//     but untaken option is not an event.
//
// M3's OWN regeneration (Ruling FL-83 allows exactly one per milestone,
// and m34 is that one). The previous golden set (2 45e0671d07b60d9e,
// 4 04950e3969039a7b, 6 d70bc7e30c0fccdd, 8 496784e7fbcf37be) is from the
// B2 era; everything M3 merged on top was verified UNMOVED at every merge
// (m30, m31, m32, m33, m35, m37), so the whole gap between those goldens
// and the pre-m34 computed heads is B3 and B4, and m34 supplies the third
// cause (see the commit body for the regression's full naming). The four
// pre-m34 computed heads, verified at 11fc153 before m34's own change:
//
//	2 1d5dc4e2727cf2d8, 4 1c581d6dfe425410, 6 09345bb7f2a73286,
//	8 1f5644eaaa0fbe40.
//
//	- B3 (e13a3ee / merge 60b8981): the botpolicy targeting heuristic --
//	  never own, board over face, rank by threat, honour Min/Max. Spells
//	  and abilities now pick different targets, so different permanents
//	  die and different life totals remain. Moves ALL FOUR seats.
//	- B4 (d2d4a6e / merge 36be378): cast and play_land ranking in the
//	  priority policy -- which spells are cast and in what order, which
//	  lands are played, hence different boards from the first main phase
//	  on. Moves ALL FOUR seats (including the 2-seat head, which had been
//	  fixed since M2d-1: the 2-seat game now casts differently, so
//	  45e0671d07b60d9e finally moves to 1d5dc4e2727cf2d8).
//	- m34 (fcea152): the free-for-all defender choice (CR 506.2 / CR
//	  903.14). Every KAttackers option is now one (attacker, defender)
//	  pair, so the recorded attacker intents -- and therefore the
//	  DeclareAttackers events, combat damage, blocks and deaths -- change
//	  shape from the first combat on, and an attack can split across two
//	  defenders (one combat in the 4-seat game emits two DeclareAttackers
//	  events, 14 total instead of 13). Also asks one KBlockers decision
//	  per defender instead of one for the whole declaration. Measured, 4
//	  seats: 8 of the game's 13 attacker choices target a defender the
//	  fixed-defender build could not express. Moves 4, 6 and 8 ONLY: the
//	  2-seat game still sees one option per creature at its sole defender,
//	  so m34's own chain contribution is a blank at 2 seats and the golden
//	  below stays 1d5dc4e2727cf2d8, unchanged from the pre-m34 measured
//	  value.
//
// Task D1's OWN single regeneration (the task is this milestone's ONE golden
// regeneration, FL-83; attributed to the commit as a whole per FL-76). D1
// implements CR 514.1 -- the cleanup step discards the active player down to
// seven cards (a new KChoose "discard" decision). Measured per seat count
// (counted hand->grave MoveZone events in each acceptance log plus the moved
// heads themselves), the cleanup discard fires ONCE in the 2-seat game and
// ONCE in the 6-seat game, and never in the 4-seat or 8-seat games -- those
// games end before any player's hand ever exceeds seven (fewer turns per
// seat; the lone hand->grave move in the 8-seat log is a card-driven discard,
// pre-existing and unchanged). So only the 2-seat (1d5dc4e2727cf2d8 ->
// 630573758e2019a8) and 6-seat (9207c51fd1e4ed6a -> 2563530049855042) heads
// move; 4 and 8 stay byte-identical to the golden. This contravenes the
// brief's expectation that every acceptance trajectory changes, and the
// measurement is reported as the honest cause.
//
// The opponents milestone's OWN single regeneration (FL-83; attributed per
// FL-76). ONE cause, and it moves ONE seat count: botpolicy now declines an
// Equip whose equipment is already attached to something, instead of
// re-activating it forever. The no-op detector reads the attachment state
// (Card.AttachedTo != 0) rather than predicting which creature the target
// branch will elect -- the previous attempt re-derived the target with the
// policy's own threat ranking, the two rankings disagreed on the stuck board
// (obj=41 attachedTo=9 bestOwn=8), and the loop survived.
//
// Measured by instrumented replay, not inferred: the 6-seat game's
// death-n-taxes pilot reaches 21 priority decisions offering an Equip on an
// already-attached Sword of Fire and Ice, and the new rule declines every
// one where the position-first block re-activated it. The 2-, 4- and 8-seat
// games reach ZERO attached-equip decisions, so their heads cannot move and
// do not: replaying this build with the old position-first block restores
// all four goldens byte-for-byte. Only the 6-seat head moves
// (2563530049855042 -> 7a043ce3e84f6f39).
//
// This is the second recorded case of a botpolicy change moving a strict
// subset of the heads, and the subset is exactly the seat counts whose games
// reach the changed code path -- the same shape D1 found above, arrived at
// from the opposite direction.
//
// The opponents milestone's SECOND regeneration -- an explicit exception to
// FL-83's one-per-milestone, taken deliberately because the alternative was
// worse: op6 and op7 were both gated and both move heads, so landing them
// separately would have cost TWO further regenerations instead of this one.
// Recorded as an exception rather than dressed up as a milestone boundary.
//
// TWO causes, and the seat counts separate them cleanly:
//
//   - op6, the tap-for-mana gate: the policy now taps a permanent for mana
//     only while some castable card exists whose cost the pool cannot yet
//     pay, instead of tapping whichever permanent was offered first. That
//     changes the ManaAdd event stream in every game, so it moves ALL FOUR
//     heads.
//   - op7, the attack-target tiebreak: equal-tier attack options now prefer
//     the lowest-life defender instead of the lowest seat index. A two-player
//     game offers one defender and therefore has no tie to break, so this
//     cause CANNOT move the 2-seat head, and does not.
//
// Measured, not inferred -- each cause was benched alone before stacking:
//
//	seats   op6 alone           op7 alone           both (this commit)
//	2       0876361619998e2a    unchanged           0876361619998e2a
//	4       355ae7ceb40e9ac9    c4d8a9de11cca37d    d74b8a889f09be48
//	6       9940f5be8b29e4d6    f8d7e167ab30c173    ea3d87a74c4c954d
//	8       25ce1c7603a1bd0f    224fd2dda5a59ff4    5e573c76021a419f
//
// The 2-seat column is the attribution proof: the combined head is
// BYTE-IDENTICAL to op6's, so op7 contributes nothing there, exactly as its
// mechanism predicts. On 4/6/8 both causes compound and neither alone
// reproduces the combined head.
//
// dp2, the colour-aware tap gate, moves all four again. ONE cause, and this
// time the attribution is a bisect rather than a per-cause bench, because the
// branch carried a performance patch that had to be proven inert:
//
//	commit     what it is                        TestHeads
//	63eb3ab    main, before the branch           PASS -- matches the old goldens
//	12f27b3    the tap feature, merged           all four move, to the values below
//	f46ee97    + the load-time production cache  all four IDENTICAL to 12f27b3
//
// So the feature moved every head and the perf patch moved none. The
// mechanism predicts exactly that: chooseTap now prefers a source whose
// produced colour matches a coloured pip the pool still owes, so the ORDER in
// which permanents are tapped changes, which reorders the ManaAdd event stream
// in every game at every seat count -- while deriving that same production at
// load instead of per projection changes WHEN it is computed and never WHAT it
// is. Verified separately rather than assumed: the 12f27b3 and f46ee97 trees'
// -decision-stats output over 400 commander games is byte-identical (md5
// ad75c845c3002bf866f162ca975bbdc9).
//
// This is the regeneration Ruling FL-90 describes: an attributed move, proven
// against the parent tree, is not a spend against FL-83's budget. An
// unattributed one still is.
// dp1b, the policy determinism pass, moves all four. Attribution here is an
// ABLATION rather than a bisect: the branch changes four decision arms at once,
// so each was reverted to main's behaviour in turn and the heads re-measured.
//
//	arm reverted to main            resulting heads (2/4/6/8)
//	(none -- branch as-is)          b984373b 7d178f22 78c5c443 a5ec8770
//	KMulligan bottoming             8c79889e d212ee6d 223856cf 0c85486d  <- all four
//	KTriggerOrder -> Fisher-Yates   fa993f4e 7d178f22 756d4d9e 8074e8b2  <- 2/6/8
//	KTriggerOptional -> coin        b984373b 7d178f22 78c5c443 a3d8d825  <- 8 only
//	KChoose discard/exile/sacrifice b984373b 7d178f22 78c5c443 a5ec8770  <- NOTHING
//
// So the bottoming reroute is the universal driver, trigger-order and
// trigger-optional contribute at the seat counts whose games reach them, and
// the headline discard ranking -- the change the branch is named for -- does
// not touch the acceptance games at all. That last row is worth keeping: it
// says the head churn is paid for by arms other than the one being advertised.
//
// Ruling FL-90: an attributed regeneration is not a spend against FL-83.
// tg1, the stack-targeting fix, moves all four. Counterspells were INERT: a
// "target spell" ask searched the battlefield, so Mana Leak was offered every
// permanent, resolved, and effCounter no-opped on o.Zone != ZStack. They now
// find spells, and a spell is no longer offered as a target of itself
// (CR 115.5).
//
// Measured per seat count, `countered` MoveZone events and Counter resolves:
//
//	tree                      countered (2/4/6/8)   Counter resolves   self-target
//	587f31d before the line   0/1/2/5               0/2/1/3            0/0/0/0
//	e4bfbb9 round 1           0/4/3/8               0/2/1/3            0/0/1/3  <- self!
//	7f89c50 round 2           0/4/2/5               0/2/0/0            0/0/0/0
//
// Round 1 made counterspells work and simultaneously let three of them counter
// THEMSELVES; round 2's exclusion is why the 6/8 resolves fall back to zero —
// those casts were the self-targeting ones and now correctly fizzle.
//
// The 2-seat head moves too, which it did NOT against 587f31d, and the reason
// is a genuine interaction rather than noise: dp1b (0751802) changed the policy
// enough to put a counterspell into the 2-seat game, where it resolved and
// countered nothing (countered=0, the inert mistarget). tg1 makes that same
// cast bite. So the 2-seat move is dp1b and tg1 compounding, and neither alone
// produces it — the same shape as op6/op7 above, arrived at across two merges
// instead of one.
//
// Ruling FL-90: an attributed regeneration is not a spend against FL-83.
// dt1, delayed triggers, moves all four — and here a partial move was never
// available. `playAcceptance` assigns decks as all[i%12], so seat 0 holds
// death-n-taxes at EVERY seat count, and death-n-taxes runs Flickerwisp. The
// gate counted register/push/return-from-exile at 2/4/6/8 as 1/1/1, 1/1/1,
// 3/3/3, 1/1/1: the card fires everywhere, so every head must move.
//
// Before this, `effDelayedTrigger` emitted a Note and stopped. Flickerwisp
// exiled a permanent and never returned it. It now registers into
// state.Game.Delayed via an appended DelayedRegister event and, on entering the
// registered phase, fires through the ORDINARY trigger drain (APNAP, raising a
// real CR 603.3b trigger_order decision when several are waiting) as a
// DelayedPush minted ability.
//
// The append is the part that had to be right: two kinds were appended at
// ordinals 37/38 AFTER CmdDamage, with no existing kind reordered and no
// events.Event field added or retyped. The gate hashed the Append encoding of
// all 37 pre-existing kinds with populated fields on both trees and got
// identical SHA-256, so no historical log's bytes moved.
//
// Ruling FL-90: an attributed regeneration is not a spend against FL-83.
// dc1, the discard ask, moves ONE head — and the single-seat move is the whole
// attribution, traced to one card in one game.
//
// Only the 8-seat game changes. The gate instrumented it: Duress resolves once,
// Thoughtseize / Cabal Therapy / Faithless Looting never do, and the game's one
// ModeChosen belongs to a Charm, not the Duress. So Duress's ask is never even
// posed — its DiscardValid$ Card.nonCreature+nonLand filter finds nothing
// eligible in that hand — and the branch correctly discards NOTHING where the
// old deterministic stand-in took hand[0]. That one card declining to discard
// is the entire move.
//
// The B1/B2/B3 repairs move nothing: the gate measured `3cfab11 + 69cdced` and
// got this identical 8-seat head, so the mover is the RevealYouChoose feature
// itself, not the suspension fix that made it safe.
//
// Ruling FL-90: an attributed regeneration is not a spend against FL-83.
//
// uc1 (Counter's UnlessCost$) moves 2 and 4 and leaves 6 and 8 alone. Both
// movers are one card in one seat: Daze#95, dimir-tempo seat 1, which now poses
// the "pay {1} or it is countered" ask that never existed before. The two seat
// counts move for DIFFERENT reasons, and the difference is the point:
//
//	2 seats  LOG CHURN ONLY. ModeChosen at seq 1172 is followed immediately by
//	         obj 79 "countered" with no mana_add -- the payer had nothing
//	         floating, so the payment FAILED and the spell is countered exactly
//	         as before. The board is identical (countered 1 -> 1); the head
//	         moves because two new events are in the chain.
//	4 seats  A REAL BOARD DIVERGENCE. Same card, same seat, but here the
//	         payment SUCCEEDS: ModeChosen at seq 1907 is followed by
//	         mana_add(-1, "B") at 1908, and a spell that used to be countered
//	         now resolves.
//	6, 8     UNCHANGED, and this is the check that the attribution is honest:
//	         Daze never resolves at those seat counts, so no unless-pay ask is
//	         posed and the only ModeChosen in either game is Warping Wail's,
//	         byte-identical on both trees.
//
// A PARTIAL move is a stronger result than a full one: if all four had moved,
// nothing would distinguish "the feature fired" from "something unrelated
// perturbed every game". Two moving with named causes and two provably not
// moving is the shape that rules the second explanation out.
//
// The gate measured these on top of 63ee22d (the sc1 merge) and got values
// byte-identical to the measurement on e7fec05, so sc1's sacrifice work does
// not interact with these four games.
//
// REGENERATED at the ft1 merge (the bot may aim at a face, by Ruling FL-108's
// three tiers). ONE seat count moved -- 8, from 700f85d871d35367 -- and 2, 4 and
// 6 are byte-identical to the values above. The mover is ONE decision, found by
// instrumenting every effect-path KTarget in all four acceptance games and
// printing each divergence between the effect-aware pick and the effect-blind
// one:
//
//	8 seats  obj 407 Lightning Bolt (DealDamage 3), asked of p6 at 17 life.
//	         p2 is at 3 life, so 3 <= 3 is provably lethal and the bolt is now
//	         aimed at p2's FACE; the effect-blind policy pointed it at p5's
//	         permanent obj 348. This is TIER 2 (an opponent when the damage is
//	         potentially lethal). spare=false, so tier 3 played no part.
//	2, 4, 6  UNCHANGED, and that is the check that the attribution is honest:
//	         across those three games there is not a single divergent target
//	         decision. Tier 1 (a threat that may kill you this round) and tier 3
//	         (the best creature it can kill, given spare mana) fired ZERO times
//	         anywhere in the acceptance corpus.
//
// Ruling FL-107 governs this regeneration: there is no quota, ATTRIBUTION is the
// only requirement, and agents never write this map -- the orchestrator does, at
// the gate, from values the gate measured on branch+current-main. One seat
// moving with a named card and a named tier, and three provably not moving, is
// the strongest shape available.
//
// REGENERATED at the bl1 merge (the land drop picks the colour the hand needs).
// THREE seat counts moved -- 4, 6 and 8 -- and 2 did NOT. Causes were measured
// per seat count by dumping both event logs, locating the FIRST divergent event,
// and then instrumenting chooseLand to print the ranking inputs (unmet / cover /
// basic / flex) for every offered land at exactly that decision:
//
//	2 seats  UNMOVED, a95fd3b1da972438. The event log is byte-identical to
//	         main's at 1353 events, which is the check that the rest of this
//	         attribution is honest.
//	4 seats  ba76d1bb389a3c2b -> 9bd7c9ea7917f512. LEVEL 3, fewest distinct
//	         colours. At seq 1206 the seat holds Cavern of Souls (obj 134) and
//	         Ancient Tomb (obj 122) with no unmet coloured need, so coverage
//	         ties at 0 and neither is basic; Cavern's Any production reports
//	         five distinct colours against Ancient Tomb's zero, so Ancient Tomb
//	         is played where basic-first-then-lowest-index took Cavern.
//	6 seats  baafe0b87f436bec -> 56cad0755f3f8c78. Same level, same two cards,
//	         same objects, first divergence at seq 1995.
//	8 seats  dbe58b710d39e5ec -> 0964cc00aed0c02c. LEVEL 1, colour coverage. At
//	         seq 425 the unmet need is four blue and one black; Underground Sea
//	         (obj 61) covers 2 of it against a basic Island's (obj 65) 1, so
//	         coverage outranks basic-ness and the dual is played. This seat
//	         count now has TWO STACKED CAUSES: ft1's target ranking moved it
//	         from 700f85d871d35367 (above), and the land drop moves it again --
//	         and the land drop's divergence is the EARLIER of the two in the log.
//
// The branch's other change moves NOTHING: TestHeads passes on b4c04ea (the
// non-literal Amount$ honesty fix) alone with all four heads at their pre-branch
// values, so chooseLand owns the whole movement. The gate build-bisected that
// rather than assuming it, and it corrects the branch report's own claim that
// both commits contributed.
// REGENERATED again at the cn1 merge (a CARDNAME sacrifice cost resolves against
// the source object, CR 201.5). THREE seat counts moved -- 2, 4 and 8 -- and 6
// did NOT. One card owns every move: Polluted Delta (obj 70, seat 1,
// dimir-tempo), whose activation costs `T PayLife<1> Sac<1/CARDNAME>`. Before
// this merge the CARDNAME leg matched no object, so the cost was unpayable and
// the fetch was never offered; now it is. Causes were measured per seat count by
// dumping both full event logs and locating the FIRST divergent event:
//
//	2 seats  a95fd3b1da972438 -> 2af07c65b7cafec2. At seq 658 main has seat 1
//	         simply pass priority; the branch instead activates Polluted Delta --
//	         a "choose" decision at 659 for the library search, the payment at
//	         661, tap of obj 70 at 662, its move_zone "sacrificed" at 663 and
//	         ability_push at 664. The log grows 1353 -> 1371 events, and the +18
//	         is that activation and the land it fetches.
//	4 seats  9bd7c9ea7917f512 -> 2cf5359b632bdbd3. First divergence at seq 1817,
//	         where the same seat's priority answer moves from option [2] to [3]:
//	         the fetch activation is now in the offered list, so every later
//	         index shifts. 4387 -> 4413 events.
//	6 seats  UNMOVED, 56cad0755f3f8c78. Seat 1's Polluted Delta is not drawn
//	         into a position where the bot activates it in this game.
//	8 seats  0964cc00aed0c02c -> adb26a03b5daa734. First divergence at seq 8288,
//	         priority answer [0] -> [1], the same option-list shift. The event
//	         COUNT is unchanged at 15132: the game takes a different decision of
//	         the same length rather than a longer one.
//
// The 6-seat non-move is the check that the rest of this attribution is honest:
// the same code is live at every seat count, so a cause that claimed to be
// unconditional would have moved it too.
// REGENERATED again at the ce1 merge (DB$ Effect registers CantTarget and
// CantRegenerate as real continuous effects). ONLY the 8-seat head moves, and
// the cause is a SINGLE EVENT, measured by dumping both full logs on this exact
// base rather than carried over from the review:
//
//	2, 4, 6  UNMOVED.
//	8 seats  adb26a03b5daa734 -> 505822c760a5f85c. Both logs are 15132 events
//	         and exactly ONE differs. At seq 14414, immediately after seq
//	         14413's stack_resolve of Vines of Vastwood (obj 355, seat 0), main
//	         emits note "registers a continuous effect (STCantTarget) for
//	         Permanent" and this build emits clock_tick from AddContinuous's
//	         Timestamp == 0 branch. NO DECISION DIVERGED: every decision_made in
//	         the two logs is byte-identical, so the CantTarget restriction never
//	         actually withheld a target in that game. The head moves because a
//	         Note was replaced by a real registration, nothing more.
//
// The review measured this same move as e3c5d5c3a411e641 against a main two
// merges older. That value is stale and is NOT what is written here: cn1's
// Polluted Delta activation shifted the log underneath it, and the head was
// re-measured on branch+current-main, which is what FL-107 requires.
// fx2 (F13, CR 602.5a) regenerated all four. The summoning-sickness gate no
// longer withholds a noncreature's tap ability on the turn it entered, so a
// land cracked or activated a turn earlier than before. Measured per seat
// count at the merge gate, first divergent event named:
//
//   - 2 seats: Karakas activated at decision seq 1313 (was Pass), event 1315.
//   - 4 seats: Misty Rainforest cracked at decision seq 1886, event 1888.
//   - 6 seats: Karakas again, decision seq 7440, event 7442.
//   - 8 seats: Polluted Delta inserted at option 0 at event 5611. The bot
//     still PASSES here -- the head moves because DecisionMade hash-chains the
//     numeric choice index, so an option inserted ahead of Pass shifts the
//     recorded index. An offered-but-untaken option is only free of the chain
//     when it is appended LAST, which is why M2d-3's concede moved nothing.
//
// fx1 (F45, CR 103.8a/800.7) regenerated 4, 6 and 8. The starting player's
// first draw is no longer skipped in a multiplayer free-for-all, so every
// multiplayer acceptance game deals one more card on turn 1 and every hidden
// hand downstream of it differs.
//
// The 2-seat head is DELIBERATELY unmoved and is the falsifier for this
// change: CR 103.8a's skip is correct at two players, so the predicate is
// keyed to the constructed seat count. A 2-seat head that moved here would
// have meant the fix was wrong, not that the golden was stale.
//
// fx4 (F31, CR 704.5m/702.16c) moved NO head. The attachment SBA now also
// kills an Aura whose bearer gained protection from it, but no acceptance
// game at any seat count reaches that path -- measured, not assumed, by
// running TestHeads on wt/fx4 and getting all four goldens back unchanged.
//
// fx3 (F21, CR 608.2b) regenerated 8 only. legalTargets now applies the same
// continuous CantTarget predicate askTarget applies at offer time, so a
// restriction that arrives between targeting and resolution fizzles the
// spell instead of being ignored. Measured on wt/fx3 rebased onto fx4, so
// this value is the combined state of both round-2 merges:
//
//   - 2, 4, 6 seats: unmoved. Only the 8-seat game races a restriction
//     against a spell already on the stack.
//   - 8 seats: event 14737. Rancor (obj 359) fizzles for want of a legal
//     target instead of resolving and attaching to Monastery Swiftspear
//     (obj 388, controller 6) at event 14738. The restriction became active
//     after Rancor was cast, which is exactly the case askTarget cannot
//     catch and the resolution recheck now does.
//
// fx13 (F43, CR 802.4) regenerated 6 and 8 only. askBlockers built the
// defender census from AliveFrom(0) -- absolute seat order -- so in a game
// whose active player is not seat 0 the defenders declared blocks in the
// wrong order. It now censuses from AliveFrom(e.G.Active), which is APNAP
// turn order; the scope filters either side of it are unchanged, so the SET
// of defenders asked is identical and only the ORDER of the asks moves.
// Measured at the merge gate by dumping both complete acceptance logs and
// diffing them, not by trusting the head alone -- both logs have the same
// event COUNT at every seat count, which is the signature of a pure
// reordering:
//
//   - 2, 4 seats: unmoved (1726 and 5353 events, identical). These games
//     never reach a declare-blockers step whose active player is a seat
//     whose rotation differs from absolute order, so the two censuses agree.
//   - 6 seats: event 7817, both logs 9230 events. After active player 3
//     takes priority at 7816, the old log asks seat 0 for blocks and the new
//     one asks seat 5. Old 7818 is seat 0 declaring blockers:[0] and 7819 is
//     Vampire Lacerator (obj 209, controller 3) being blocked by Flickerwisp
//     (obj 37, controller 0); new 7818 is seat 5 declaring blockers:[] and
//     seat 0's ask slides to 7819. Same blocks, later.
//   - 8 seats: event 12250, both logs 15523 events. Same shape: active 3
//     takes priority at 12249, the old log asks seat 0 and the new one asks
//     seat 6, and seat 0's blockers:[0] against Vampire Lacerator -- blocked
//     by Palace Jailer (obj 48, controller 0) -- moves back one event.
//
// The equal event counts are load-bearing here. A change to WHICH defenders
// are asked, rather than in what order, would have changed the count, and
// that is the failure this fix had to avoid.
//
// fx11 (F33, CR 800.4a) regenerated ALL FOUR, and this is the one head move
// in the series that should be expected rather than checked for: every
// acceptance game ends with at least one player losing, and what happens to
// a departed player's cards is exactly what changed. A seat count that did
// NOT move would have been the surprising result.
//
// Departure used to move the departing player's BATTLEFIELD permanents to
// exile, which is CR 800.4a approximated by the nearest zone this build had.
// Objects now cease: every card-backed object that player OWNS leaves from
// whatever zone it is in, plus every stack object it controls, to ZCeased --
// a zone with no membership list, so a ceased object is in no zone at all
// rather than sitting in exile pretending to be gone. Three things follow,
// and all three show up in the logs:
//
//   - the sweep now touches library, hand and graveyard cards, not just the
//     battlefield, so it emits far MORE events than the exile sweep did;
//   - it walks the arena in dense index order, so the first card it touches
//     is the lowest-id owned object rather than the first battlefield one;
//   - ownership, not control, decides. An opponent-owned card the departing
//     player happened to control stays; the departing player's own card
//     under someone else's control goes.
//
// Measured at the merge gate on this branch rebased onto current main (so
// these values are the combined state of fx13 and fx11), first divergence
// per seat count. Every one of them is the same signature -- the old log
// exiling a battlefield land, the new log ceasing a lower-id library card:
//
//   - 2 seats: event 1718. Base exiled battlefield Island (obj 65); now
//     Underground Sea (obj 61) ceases from the LIBRARY.
//   - 4 seats: event 4497. Base exiled battlefield Underground Sea (obj 63);
//     now Underground Sea (obj 61) ceases from the library.
//   - 6 seats: event 5626. Base exiled battlefield Swamp (obj 69); now
//     Underground Sea (obj 61) ceases from the library.
//   - 8 seats: event 9275. Base exiled battlefield Island (obj 243); now
//     Island (obj 241) ceases from the library.
//
// The 6- and 8-seat values here are NOT the ones fx11 measured on its own
// branch (f95dceca1d442c5e and a53ae4074f80c2e3). Those were taken against a
// main that predates fx13's APNAP blocker order, which moves those same two
// heads. Re-measured on branch+current-main, which is what FL-107 requires,
// and the 2- and 4-seat values are identical either way because fx13 moved
// neither.
//
// fx15 (F42, CR 800.4e -- creatures attacking a departed player are removed
// from combat) moves the 2-seat head to 996edb2512cdab75 and moves NOTHING
// else. That asymmetry is the evidence, not a puzzle: the defect needs a
// player to leave the game while creatures are attacking THEM specifically,
// and in the 4-, 6- and 8-seat games the eliminations happen to fall
// outside a combat aimed at the departing seat. One golden moving is the
// narrow blast radius the fix was scoped to have.
//
// MEASURED on the merged main by dumping both complete 2-seat logs and
// diffing them, rather than by trusting the head alone:
//
//   - 1719  end_combat_reset obj=31  Thalia, Guardian of Thraben
//   - 1720  end_combat_reset obj=37  Flickerwisp
//   - 1721  end_combat_reset obj=48  Palace Jailer
//   - 1780  damage pl=1 amt=3
//   - 1781  damage pl=1 amt=2
//
// 1783 -> 1784 events; 309 intents, 13 turns and the winner are all
// unchanged. Note the net is +1, NOT +3: three of seat 0's attackers are
// removed from combat when seat 1 departs, and the two damage assignments
// seat 1 was still absorbing afterwards stop happening. A reviewer reading
// only the event delta would see a single event and conclude almost nothing
// changed; the delta is small because an addition and a removal nearly
// cancel, which is exactly the case where the count is the wrong thing to
// read. The removed damage is the defect: it was assigned to a seat that
// had already left the game.
// fx18 (F32, CR 511.3) and fx19 (F44, CR 800.6) move these together, and
// the values below are the COMBINED state measured on merged main -- not
// either branch's own numbers, neither of which survives the other's merge.
//
// fx18 moves all four. Combat is now reset as the end of combat step ENDS
// rather than as it begins, so every game loses one EndCombatReset per
// combat from the position where the step was entered. First divergence is
// the first turn's empty combat in every seat count (2: event 96, 4: 172,
// 6: 232, 8: 292): an EndCombatReset becomes a Priority. Event totals fall
// 1784->1771, 5514->5490, 9492->9466, 15403->15366. With EndCombatReset
// events and sequence numbers removed, the before/after streams are
// byte-for-byte identical, so nothing about card behaviour changed -- only
// the schedule of the reset.
//
// fx19 moves 4, 6 and 8 and DELIBERATELY NOT 2. freeMulligans is 0 below
// three seats, so the two-player path cannot be reached by this change; a
// moved 2-seat head would have meant the multiplayer rule leaked. The
// 2-seat head here is exactly the value fx18 measured alone, which is that
// check passing. In each moved game the dimir-tempo seat takes its one
// permitted mulligan and the now-free first mulligan removes the bottoming
// ask, its answer and one card move (first divergence at event 58, 78 and
// 98 respectively: a seat-1 bottoming DecisionOffered becomes TurnChange).
// fx21 (F35 + F36, CR 508.2 / 509.2 / 508.8) moves all four. Combat now
// contains a priority window in each declare step, after the declaration, so
// every combat in every acceptance game gains decisions and the passes that
// answer them. Measured at the gate on merged main; fx21's own branch values
// were identical, since nothing else merged between.
//
// First divergence is the first turn's empty combat at event 94/167/227/287
// for 2/4/6/8 seats: the base jumped StepDeclareAttackers -> StepEndCombat,
// the fix emits the forced empty DeclareAttackers marker and then the CR
// 508.2 priority decisions before end combat.
//
// Intents 309->349, 1227->1359, 1929->2144, 3398->3790. Events 1771->1928,
// 6283->6795, 9466->10318, 15912->17462. WINNERS AND TURN COUNTS ARE
// UNCHANGED at every seat count, and the coverage ratchet stays at 0 of 436 --
// which is the evidence that this is more decisions in the same games, not
// different games. A head move this large with a changed winner would have
// meant a card primitive moved and would not have been mergeable on this
// reasoning.
// Head history, kept as prose rather than as dead maps: jj-f01 moved all
// four before this by making the cast proposal a transaction (601.2a push,
// then the 601.2c target ask, then 601.2h payment), a pure reordering --
// intent and event counts were byte-identical at 2, 6 and 8 seats and only
// 4 seats gained anything, with every winner and turn count unchanged.
// Before that, jj-cmb moved them by adding a CR 510.4 priority round inside
// the combat damage step and dropping a phantom post-game_over `step` event.
var acceptanceHeads = map[int]string{
	// jj-cost2 moved ALL FOUR heads. The cause is one rule and it was found by
	// diffing the two event streams rather than reasoned about: in the 2-seat
	// game the streams are identical for 1257 events and first differ at seq
	// 1258, where main casts Daze and this build does not.
	//
	// Daze pays an ALTERNATIVE cost (return an Island rather than pay its mana
	// cost). Thalia, Guardian of Thraben -- "noncreature spells cost {1} more",
	// a RaiseCost static with Type$ Spell -- resolved onto death-n-taxes'
	// battlefield at seq 284 and was still there. CR 601.2f composes the total
	// cost from whichever cost is being paid PLUS all increases, so Daze's
	// alternative cost is still taxed {1}; the engine used to apply cost
	// increases only to a printed mana cost and let the alternative cost
	// escape them entirely, so it cast Daze for free. The old streams recorded
	// a price the rules do not allow, exactly as the legend-rule note below
	// records boards the rules do not allow.
	//
	// Winners move at 6 seats (mono-green-stompy -> mono-black-aggro) and 8
	// (mono-red-goblins -> death-n-taxes). A winner change is normally grounds
	// to refuse a head, so it was checked rather than accepted: the 2-seat
	// game keeps its winner, its turn count and 1257 of its 1907 events, which
	// is what a single pricing correction looks like -- one divergence, then
	// the bot's answer stream shifts and the game is simply a different one
	// from there. The direction is mixed (4 seats gets shorter, 6 and 8 get
	// longer), which is also right: taxing an alternative cost denies some
	// spells and frees the mana for others.
	//
	// The 601.2g mana window in the same commit is NOT a contributor: measured
	// zero window asks across the 4- and 6-seat acceptance games (the window
	// only opens when the pool alone cannot pay and an untapped source
	// exists), so it moves no head here.
	//
	// `make sim` 20/20 replay OK and the CR lane 71 -> 77 PASS with no leaf
	// regressing. Measured by the controller at the gate on a branch rebased
	// onto this main, NOT taken from the seat's report -- the seat's own
	// numbers predated the hotfix merge.
	// hostb1 moved THREE of the four. The cause is one event that should
	// never have been in the log: handlePriority's pass branch emitted the
	// "priority returns to the active player" marker unconditionally after
	// resolveTop, so a resolution that SUSPENDED on a mid-resolution question
	// -- a modal spell's KModes, an as-enters choose, an unless-pay -- logged
	// a priority grant while the engine was parked on that question and
	// nobody had priority. CR 117.5: no player receives priority in the
	// middle of a resolution. The grant now happens when the resolution
	// actually completes.
	//
	// The 2-seat head does NOT move, and that is the check that this change
	// reaches only what it should: measured at main, the 2-seat game contains
	// zero DecisionAsk-immediately-followed-by-Priority adjacencies, while the
	// 4-, 6- and 8-seat games contain 3, 4 and 4. Only the games with a
	// suspended resolution move.
	//
	// Found by diffing the streams rather than reasoned about. The whole
	// 4-seat diff is ten lines: two markers relocated a few events later, to
	// the resolution's true end, and one deleted outright. The deleted one is
	// the proof the old log was lying -- at main, seq 3953 asks "choose",
	// 3954 records priority, and 3955 is the ANSWER, with the genuine grant
	// arriving afterwards at 3959. The new stream keeps that genuine grant
	// and drops only the marker logged between the question and its answer,
	// which is why the event counts fall by one or two rather than staying
	// level.
	//
	// Winners, turn counts and intent counts are UNCHANGED at every seat
	// count. Nothing about play differs; the engine only stopped recording a
	// priority round that never happened.
	//
	// `make sim` 20/20 replay OK and the CR lane unchanged at 79 PASS / 6
	// FAIL, the same six leaves. Measured by the controller at the gate.
	// f05 moved TWO of the four, 4 and 6 seats; 2 and 8 are untouched. The
	// cause is Ruling F05-2 (CR 733.2): a cast that aborts with no progress
	// used to have its option held out of the rest of the priority window on
	// the FIRST abort, so the seat could not try it again. CR 733.2 says a
	// reversed illegal action may be redone legally, so the first abort now
	// leaves the option offered and only the SECOND identical abort of the
	// same card in the same window holds it out.
	//
	// The card that does it is Dismember, two copies in dimir-tempo, mana
	// cost `1 BP BP`. Phyrexian mana makes the cast abort with no progress
	// when the life payment is not taken, which is exactly the no-progress
	// shape the ruling governs -- and it is a NON-delve abort, which is why
	// the f05 brief's "heads will not move" premise was wrong: the brief
	// reasoned about the Delve shortfall alone, while the mechanism is the
	// whole no-progress abort path. The seat measured and reported this
	// rather than regenerating the golden, which is the rule working.
	//
	// The bot now gets one retry it did not get before, so its play differs
	// from that point on. Measured by the controller at the gate: the two new
	// heads are stable across repeated runs, `make sim` is 20/20 replay OK,
	// and the CR lane goes 4 FAIL / 81 PASS -> 3 FAIL / 82 PASS, the two
	// under-delve leaves flipping red -> green with nothing regressing.
	// b2 moved THREE of the four -- 2, 4 and 8 seats; the 6-seat game is
	// unchanged. The cause is that searching a library is now a real
	// question. A ChangeZone whose Origin is exactly Library used to resolve
	// itself; it now poses a hidden KChoose to the searching player, who may
	// find nothing, and the library is shuffled either way.
	//
	// The 2-seat game is the clearest evidence, because its two decks hold
	// exactly four cards that search: death-n-taxes plays Stoneforge Mystic
	// and Recruiter of the Guard, and dimir-tempo plays Misty Rainforest and
	// Polluted Delta. A fetchland that used to fetch by fiat now asks, and
	// the bot's answer is a real choice, so play diverges from that point.
	//
	// The 6-seat game not moving is the check that this reaches only what it
	// should: it is not a global reshuffle of every game, it is the games
	// whose seats actually search.
	//
	// Measured by the controller at the gate: the three new heads are stable
	// across repeated runs, `make sim` is 20/20 replay OK, and the CR
	// conformance lane is unchanged at 2 FAIL / 83 PASS, the same two F12
	// Charm arms.
	// pc1 moved ONE head, 8 seats. A type word used as a predicate -- the
	// `Goblin` in `Creature.Goblin+Other+YouCtrl` -- was not in the
	// `predicates` map, so it failed closed and matched nothing. Goblin
	// Chieftain's "other Goblin creatures you control get +1/+1 and have
	// haste" therefore applied to no creature at all, and the same was true
	// of every lord whose Affected$ names a creature type.
	//
	// The seat instrumented the divergence rather than guessing it: the
	// 8-seat log is byte-identical up to the attackers event, which goes
	// `[12 13]` -> `[12 14]`, and a board snapshot at that attack shows
	// Goblin Piledriver at 1/2 before and 2/3 with haste after. Different
	// winner, 56 turns against 65.
	//
	// Only 8 seats moves because mono-red-goblins is deck index 6 of the 12,
	// and the 2-, 4- and 6-seat games seat only indices 0..1, 0..3 and 0..5.
	// The one game that draws the deck is the one whose head moves, which is
	// the check that this reaches only what it should.
	//
	// Measured by the controller at the gate on a branch rebased onto this
	// main: the new head is stable across repeated runs, the ratchet is
	// unchanged at 0 of 436, and the CR lane is unchanged at 2 FAIL / 83
	// PASS. The seat's own reported head predates the b2 and f05 merges.
	// f12 moved THREE of the four -- 4, 6 and 8 seats; the 2-seat game is
	// byte-identical. The cause is CR 601.2b: a modal spell now announces its
	// modes after it reaches the stack and BEFORE targets are chosen and costs
	// are paid, instead of choosing them at resolution.
	//
	// Measured by the controller at the gate, not taken from the seat's
	// report: both complete acceptance logs were dumped at every seat count
	// and the first divergent event located. In all three moved games it is
	// the same card and the same shape -- seat 2 (eldrazi-stompy) casting
	// Warping Wail, at seq 2386 (4 seats), 3814 (6) and 6452 (8). The 4-seat
	// pair reads:
	//
	//   before: 2385 stack_push Warping Wail
	//           2386 mana_add -3
	//           2387 priority                 <- resolved with NO mode
	//   after:  2385 stack_push Warping Wail
	//           2386 decision_ask   modes
	//           2387 decision_made  modes:[0]
	//           2388 mode_chosen    "Exile target creature with power or
	//                                toughness 1 or less."
	//           2389 decision_ask   target
	//           2390 decision_made  target:[3]
	//           2391 targets_chosen IDs:[84]
	//           2392 mana_add -3               <- payment now comes last
	//
	// So this is a real board divergence, not log churn: the spell used to be
	// paid for and resolve having chosen nothing and exiled nothing. It now
	// exiles a creature. The 2-seat game never casts Warping Wail, which is
	// the check that this reaches only the games that cast a modal spell.
	//
	// Also measured at the gate: `make sim` 20/20 replay OK, the ratchet
	// unchanged, and the CR conformance lane 2 FAIL / 83 PASS -> 0 FAIL / 85
	// PASS, the two Charm arms this task owned flipping green with nothing
	// regressing. The seat's own reported 8-seat head predates the pc1 and
	// pc2 merges; these are re-measured on a branch rebased onto this main.
	// ml1 moved ALL FOUR heads. The cause is one rule: tapping a permanent
	// with two or more unrestricted mana abilities (any dual/multi-basic-type
	// land -- Underground Sea, Cavern of Souls, and 330 other faces in the
	// corpus) used to resolve every one of them unconditionally on a single
	// tap; it now asks a real KChoose for which one to activate, exactly as
	// CR 305.6 requires (a land with two basic land types has the intrinsic
	// ability of each as two distinct abilities sharing one permanent, never
	// both at once).
	//
	// Measured by the seat and re-confirmed by the controller at the gate:
	// the first newly-posed mana choice is Underground Sea at event sequence
	// 722 (2 seats), 273 (4 seats), and 473 (8 seats); in the 6-seat game it
	// is Cavern of Souls at sequence 3806. The bot deterministically selects
	// option zero (the first-listed ability), so every game's actual mana
	// production is unchanged -- but each choice now adds real
	// DecisionAsk/DecisionMade events that the old, silent "resolve all
	// abilities" path never logged, so every event count and every
	// downstream sequence number after the first dual land shifts.
	//
	// pl1 then moved 2 and 8 seats a second time on top of ml1's move (4 and
	// 6 land back on ml1's own values -- PayLife's fix does not shift either
	// of those two games once ml1's mana-choice fix is also present, even
	// though pl1's own branch, tested WITHOUT ml1, measured 4 seats moving).
	// Fixed PayLife<N> costs change what a fetchland like Polluted Delta
	// actually spends (real life, not phantom generic mana); which games
	// that reaches depends on the exact combined event stream, which is why
	// pl1's own reported 2/4/8 values (measured against its own, ml1-less
	// base) do not equal any of the values below. pl1's branch predated
	// ml1's merge, so the values here are re-measured by the controller
	// directly on this merge (both fixes together), not copied from either
	// seat's report.
	//
	// `make sim` 20/20 replay OK and the CR conformance lane unchanged at
	// 0 FAIL / 89 PASS / 89 leaves both times. TestRepoDecks and
	// TestEveryRepoDeck pass. Winners and turn counts were not re-verified
	// game-by-game beyond the conformance lane and sim replay; if a future
	// seat finds one moved, trace it the same way -- diff the streams, don't
	// assume.
	// 2 seats moved to 582742ecf91aef9d (autonomous orchestrator): resolving fb-20260911T030431Z-e0ebf46f (File as bot engine bug:  bots should deprioritize playing counter spells when...)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 2 seats moved to 738ae4c532453cb7 (autonomous orchestrator): resolving fb-20260914T033246Z-3f1cc033 (delver of secrets was played, but I was not prompted ... "you MAY reveal"... ...)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 2 seats moved to 141e372af24479ff (autonomous orchestrator): resolving fb-20260914T115601Z-cd1c7268 (flooded strand - activation still takes "2 clicks", 1 to tap, 1 to target its...)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 2 seats moved to 57da6b9d46fe0c72 (autonomous orchestrator): resolving fb-20260914T114653Z-3a890b2a (player vs bot should flip a coin / roll a dice to determine who acts first)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 2 seats moved to c969c2c17c67617a (autonomous orchestrator): resolving inbox-engine-gap-event-triggers (Engine: Sacrifice, discard, tap and crime triggers)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 2 seats moved to ce983f701aa41900 (autonomous orchestrator): resolving inbox-rv2a-coinflip-round-clock-and-toss-display (Coin flip follow-up: round clock, pregame active seat, and how the toss is shown)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 2 seats moved to b80cb7ba6b93abc7 (autonomous orchestrator): resolving inbox-paramcensus-final-stragglers (param census: final stragglers (8 unrelated one-offs, closes out the full census)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 2 seats moved to 0ff0aff481758d4b with task inbox-deck-ulalek-eldrazi
	// (rebased onto main, re-measured): the tap gate's satisfiability filter
	// (botpolicy/tap.go bestUnpayable) again -- the identical fix that moved
	// these goldens pre-rebase, now measured against main's newer base. First
	// divergence at event 958 (seat 1's untap-step tap ask at 957): the
	// neutralised build (filter off) picks tap option 0 -- the colourless-only
	// Wasteland toward a {B} pip no offered source produces -- the fixed build
	// declines and taps option 3 instead; with the filter neutralised the
	// stream is byte-identical to main's b80cb7ba6b93abc7 game (2426 events),
	// so the filter is the sole cause at this seat count.
	// 2 seats moved to 6085b337c4a4c0a1 (autonomous orchestrator): resolving inbox-paramcensus-shuffle-nonmandatory (param census: ChangeZone.ShuffleNonMandatory (fetch effects that may decline to )
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 2 seats moved to 938816b6a03a9cfc (task mvts1, the trigger-chain
	// ValidTgts$ pre-ask): Cabal Therapy (death-n-taxes, cast twice in this
	// game). Its ability root is AB$ NameCard -- no ValidTgts$, so the
	// announcement ask never covered it -- and its DB$ Discard | ValidTgts$
	// Player SUB is the "target player" of "target player reveals their hand
	// and discards all cards of that name"; pre-mvts1 that sub read an empty
	// Ctx.Targets and the whole resolution was a silent no-op. It now poses
	// its own KChoose (chosenTargetsFor), the bot answers option 0, and the
	// discard resolution runs for real. Instrumented negative control: this
	// is the ONLY ValidTgts$-carrying sub that poses in any of the four
	// golden games (2/4/6/8-seat instrumented runs), and 4/6/8 are UNMOVED.
	// AR7 promotion (2026-09-19): the default bot now attacks with a creature
	// that is lethal to its defender if unblocked even into an unfavourable
	// block (botpolicy lethal pressure; held-out 2,066-1,934 vs the old bot).
	// 2 seats moved to bc7420d9e4c7d3d2 (task cli-20260922T150843Z-42f8f6dc,
	// NameCard over the full corpus name universe): Cabal Therapy
	// (dimir-tempo) resolves its mid-resolution `A:SP$ NameCard | ValidCards$
	// Card.nonLand` in this game. Before the fix effNameCard named the TOP CARD
	// OF THE CASTER'S OWN LIBRARY and posed no ask: at event seq 657 the base
	// stream emits `choose Ponder` (Obj 116 = the resolving Cabal Therapy). The
	// fixed stream emits `decision_ask choose` at 657 and `decision_made` at
	// 658 (the two extra events), then `choose "Ach! Hans, Run!"` at 659 -- the
	// bot's deterministic first legal nonland name from the full universe. The
	// first divergence is therefore exactly seq 657, and every later choice
	// difference follows from the Therapy name (its `DB$ Discard` then discards
	// a different card). 4, 6 and 8 seats are UNMOVED (no NameCard resolves in
	// those games).
	// 2 seats moved to 19a4893657e5d549 (autonomous orchestrator): resolving cli-20260922T225140Z-68ca4d95 (approx: the CR 704.5j legend rule picks the survivor by battlefield scan positio)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 2 seats moved to a867ac1768ab4993 (autonomous orchestrator): resolving agent-20260920T070405Z-7fec3b8b (trig:SearchedLibrary — "whenever an opponent searches their library" trigger mod)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// CR 103.1 (cli-20260922T225141Z-e771720d): the hosted toss winner
	// answers starting_player before mulligans. DecisionAsk/DecisionMade enter
	// the chain; without that ask this head returns to a867ac1768ab4993.
	// bot-x1 (botpolicy/ability.go equipNoOp): A1's attachment no-op rule
	// is scoped to attach abilities (decision.Option.Attach), promoted from
	// the explore policy's X1, so an attached Aura's/Equipment's own
	// abilities and a creatureless seat's non-attach abilities are no longer
	// declined. Measured: the base commit's heads were green and this change
	// alone moves 2 seats a613e26b20d46eea -> 3ddcc4e3ba5bb799 and 8 seats
	// af5b2fc1561b5f56 -> beac5729b0e63dcc; 4 and 6 seats are unchanged.
	// agent-20260923T114033Z-a57ee463 (game-long damage-by-source provenance)
	// merged into this ticket's branch: it separately moves 2 seats
	// 3ddcc4e3ba5bb799 -> 9b4759db7fe10fdd (see the 8-seat note). The
	// cli-20260924T034908Z-be6139b0 entry-counter fold leaves the 2-seat game
	// at 9b4759db7fe10fdd -- measured on the merged tip with the entry-body
	// absorption disabled, which reproduces 9b4759db7fe10fdd exactly.
	// 2 seats moved to a991478b3edb213c (autonomous orchestrator): resolving agent-20260926T222248Z-9c652605 (Hide the ordinary Optional$ ChangeZone confirmation behind ForgetOtherRemembered)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// CR 601.2c sub-ability targets on cast (wt/cr601-subtargets): 2 seats
	// moves to 6b3129c0f22c5cc2. Measured by diffing the two event streams
	// (the old one reproduced exactly, head a991478b3edb213c, with only the
	// chain announcement and its offer census disabled): the first diverging
	// event is index 432, a DecisionAsk "target" -- dimir-tempo's Cabal
	// Therapy (obj 116, `SP$ NameCard` with a `ValidTgts$ Player` link) now
	// announces "target player" as it is cast, followed by the answer, a
	// TargetsChosen (events.SubTargetNotice) naming death-n-taxes, and the
	// caster's Priority event; the resolution-time "choose" ask/answer pair
	// 13 events later is gone. The same block repeats once for the flashback
	// cast (index 1407). Twelve changed lines in all, 2476 -> 2480 events;
	// the same player is targeted and every other event is identical. The 4,
	// 6 and 8 seat heads are unchanged.
	2: "6b3129c0f22c5cc2",
	// 4 seats moved to c232a4aca592e0f8 (autonomous orchestrator): resolving fb-20260914T033246Z-3f1cc033 (delver of secrets was played, but I was not prompted ... "you MAY reveal"... ...)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to e361b9ce5ce9ef7c (autonomous orchestrator): resolving fb-20260914T063255Z-ce14a643 (gitaxian probe -- prompt should display user's hand to me, but it did not)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to 689b8ab04cd0f9d0 (autonomous orchestrator): resolving fb-20260914T115601Z-cd1c7268 (flooded strand - activation still takes "2 clicks", 1 to tap, 1 to target its...)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to 4ef9d4faf9010d79 (autonomous orchestrator): resolving fb-20260914T114653Z-3a890b2a (player vs bot should flip a coin / roll a dice to determine who acts first)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to 2753ceca0bed344d (autonomous orchestrator): resolving inbox-engine-gap-event-triggers (Engine: Sacrifice, discard, tap and crime triggers)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to b5888e1f7c2ccab9 (autonomous orchestrator): resolving inbox-rv2b-brainstorm-put-back-from-hand (Brainstorm-style "put cards from your hand back" silently does nothing (Jace [0])
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to 1fe7ffc6e9116a60 (autonomous orchestrator): resolving inbox-rv2a-coinflip-round-clock-and-toss-display (Coin flip follow-up: round clock, pregame active seat, and how the toss is shown)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	//
	// 4 seats moved to 65ee3da5473a2c5a with task inbox-botbench-stability-run
	// (prove the expanded bench has no deadlocks, crashes, stalls or loops):
	// the production bot's KChoose mana-payment arm now prefers a phyrexian
	// pip's "Pay 2 life" over its pool colour while the seat has life to
	// spare (botpolicy/policy.go). Measured cause, by printing the arm's
	// firings over the acceptance games: every firing is Dismember ({1}{B/P}
	// {B/P}, the tron deck) -- the old first-offer answer paid the pip with
	// the pool's only black, which can strand the generic {1} and abort the
	// activation with no progress; the life payment never strands anything.
	// 70 firings across the 4-seat game, zero on any other card. The engine
	// changes in the same commit (the F05-2 no-progress suppression now also
	// read by the ability-offer path, the KAttackers Required marking and Max
	// ceiling) are heads-NEUTRAL: measured, reverting the pay arm and the
	// required-attacker overrides with the engine diff in place leaves the
	// four old heads standing, and the remaining botpolicy bits (the required
	// set, the hold-back exclusion, the ceiling cap) are inert in these games
	// because the 12 legacy decks carry no goad, encore, MustAttack or
	// MaxAttackers card (deck census over internal/testutil/decks.go's list).
	// The 2-seat head is unchanged: its game never reaches a Dismember pip
	// ask before the old and new streams would part.
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to 7012c79608844da6 (autonomous orchestrator): resolving inbox-rv2c-cost-modifiers-forge-parity (Engine cost modifiers diverge from Forge (Steelshaper, Rakdos, Ghalta, Color$) a)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to 13ce7a60c8da708f (autonomous orchestrator): resolving inbox-rv2a-starting-player-state-and-mulligan-passes (Engine: record the starting player; CR 103.5 mulligan passes go round the table)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to 7b994506b3e93874 with task
	// inbox-paramcensus-action-condition-checksvar: Vampire Lacerator's upkeep
	// trigger now ENFORCES its ConditionCheckSVar$/ConditionSVarCompare$ gate
	// ("you lose 1 life unless an opponent has 10 or less life", the gate SVar
	// PlayerCountOpponents$LowestLifeTotal GE11). Measured by diffing the
	// complete event streams: the old and new 4-seat streams are identical for
	// 2517 events and first differ at seq 2518, where the vanilla build emitted
	// the `life` loss and this build does not -- seat 1 was at 7 and seat 2 at
	// 6 life, both <=10, so the gate holds and the loss is correctly skipped
	// (the old build ran the gated ability unconditionally, the documented
	// fail-open stand-in this task closes). The 8-seat cause is the same card
	// at seq 6724 (opponent at 3 life). The 2- and 6-seat games never hit a
	// Lacerator upkeep whose gate verdict differs from run-anyway, so those
	// heads stand byte-identical.
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to 832ea9b373fde3ed (autonomous orchestrator): resolving
	// inbox-paramcensus-dig-variants (Dig's Reveal$/ChangeValid$ Permanent/
	// DestinationZone2$ reads, all three on ONE card in this game: seat 2
	// resolves Matter Reshaper's death-trigger dig -- the ChangeValid$
	// Permanent.cmcLE3 window now matches a LIBRARY card (permanentCardSpec),
	// the dug window is publicly revealed (Reveal$) and the unmatched card
	// goes to the hand (DestinationZone2$) instead of the dig silently moving
	// nothing). Measured by four-way bisection of effDig's new reads against
	// the golden: any three of the four neutralised still move the head
	// (832ea9b3 / 5624b869 / 98f580d5 / 2afd4433 per variant), all four
	// neutralised reproduce f5b40f9131bcafbd byte-for-byte; the ChangeNum$
	// Any ask contributes nothing here (any on/off never changed the head --
	// no Any-dig with eligible cards is reached in the golden games, and a
	// bot-answered ask moves the same cards the silent path does). The
	// 2/6/8-seat heads are unchanged.
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to 22cf2bf04aeab3c7 with task inbox-deck-ulalek-eldrazi
	// (rebased onto main, re-measured): the same tap-gate satisfiability
	// filter as the 2-seat entry. First divergence at event 4105 (seat 1's
	// tap ask at 4104, right after an Island tap produced U at 4102): the
	// old gate taps option 0 toward a {B} card with only blue sources
	// offered, the fixed gate declines; the neutralised stream reproduces
	// main's 832ea9b373fde3ed byte-for-byte, so the filter is the sole cause.
	// vote_card1 (2026-09-22): 4 seats moved to 8d2fd67b913e5f1f — the card-
	// ballot vote's bot answer became real. Both Board adapters now carry the
	// public WORTH facts of every other seat's battlefield permanents, so the
	// vote_card policy prices an offered opponent permanent instead of
	// reading zero. Seat 0 (death-n-taxes) casts Council's Judgment at event
	// 3945 and resolves it at 3946; the ballot is [Insectile Aberration,
	// Gurmag Angler, Vampire Lacerator] (seat 1's dimir-tempo board plus
	// seat 3's own Lacerator). First divergence at event 3948, the FIRST
	// vote ask's decision_made: choose:[0] (the zero-fact first entry) ->
	// choose:[1] (the higher-worth Gurmag Angler); the reveal Notes and the
	// exile set follow (a three-way tie exiled all three, now one unanimous
	// vote exiles the Angler). Every earlier event is byte-identical.
	// vote_card_self1 (2026-09-22): 4 seats moved to ce1af3d502cbecb4 — a
	// voter no longer votes its OWN permanent. Council's Judgment's ballot
	// excludes only the CASTER's permanents, so seat 1's own Gurmag Angler
	// (the ballot's global highest) and seat 3's Vampire Lacerator are both
	// offered to voter 1; the option now carries the subject's controller
	// (Option.Player) and the policy ranks non-self options first. First
	// divergence at event 3950, the SECOND vote ask's decision_made
	// (Player 1): choose:[1] (Gurmag Angler, its own) -> choose:[2]
	// (Vampire Lacerator, seat 3's foreign permanent); the tally goes from
	// unanimous Angler to Angler 3 / Lacerator 1, so the same Angler is
	// still exiled. The first ask (voter 0, the caster, whose own are
	// excluded) is unchanged. Every earlier event is byte-identical.
	// RememberRevealed$ writes the source list (2026-09-22): main's side of
	// the merge moved 4 seats to eb8651db1973af36 with task
	// agent-20260919T203859Z-d20caa2e (count:Plus.<SVarName>). Sole mover,
	// bisected on main: the new eventRemember write in effReveal's
	// RememberRevealed$ arm (effects/cardflow.go) -- reverting just that call
	// in a scratch copy restores both goldens, and reverting the count.go
	// operand resolver instead does not. Mechanism: Delver of Secrets'
	// upkeep TrigPeek (DB$ PeekAndReveal | RememberRevealed$ True) now puts
	// the revealed card on the SOURCE object's persistent Remembered list as
	// well as the ctx half, so the chained DBCleanup's ClearRemembered$
	// stops being the measured empty-list no-op and emits a real Choose
	// event. Two extra Choose events per Delver upkeep resolution (10
	// reaches in the golden games, all Delver, seat 1). Delver's own
	// transform decision is unchanged -- it reads the ctx half, which held
	// the reveal before and after.
	// merge re-measure (2026-09-22, cli-20260922T150844Z-dfebd0b5): with BOTH
	// change sets present the measured head is e7cffb892a152493 -- each cause was
	// measured on its own side above; both are bot-visible stream changes in
	// the same golden games, so the merged head is neither side's value.
	// Resolution-time mana colour choice (cli-20260922T225137Z-c670f42d): 4
	// seats moves to 049bd58a7b940fbc. Mana effects that previously recorded
	// the R-9 colourless ManaAdd.Counter now ask and record the bot's selected
	// WUBRG counter; that intentional event payload change is the first stream
	// difference and changes the downstream deterministic game.
	// dig1 Optional$ closure (cli-20260922T225139Z-230e9834): 4 seats moves to
	// 20028059e8c88ec3. An Optional$ True Dig window whose eligible count fits
	// ChangeNum$ no longer silently takes: it poses the "you may" KChoose
	// (CR 701.15/601.2). Sole mover in the golden decks: Matter Reshaper's
	// death trigger (eldrazi-stompy, seat 2's deck at 4 seats) -- the only
	// Dig carrier in the four 4-seat decks, a one-card Optional$ window that
	// previously took silently. Measured cause: reverting ONLY effDig's
	// optionalChoice hunk in a scratch copy restores all four goldens.
	// 4 seats moved to acce7d850cfb176a (autonomous orchestrator): resolving cli-20260922T225141Z-48972afc (approx: a mulligan's REDRAW resolves immediately, interleaved with the rest of t)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to 2114abdd282628ca (autonomous orchestrator): resolving agent-20260920T070405Z-7fec3b8b (trig:SearchedLibrary — "whenever an opponent searches their library" trigger mod)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// CR 103.1: the same toss-choice ask/answer moves the 4-seat chain;
	// disabling only the ask restores 2114abdd282628ca.
	// AR9 swarm lethal (botpolicy/combat.go swarmLethalSubset, cardfuzz
	// bigboard work): 4 seats moves to b3bf75f0c56512ae. The bot now forces the smallest
	// attacking set that is lethal even if the defender blocks its biggest
	// attackers with every untapped creature. Measured cause: disabling only
	// swarmLethalSubset (return nil) restores the previous golden; the
	// block-risk memo, killBlockCost's linear prefix scan and the Ascend
	// pre-filter in the same change are head-neutral.
	// cli-20260924T034908Z-be6139b0 (CR 614.12 entry-counter fold):
	// eldrazi-stompy's K:etbCounter cards now fold their entry counters into
	// the entry MoveZone. Measured over the acceptance games: Endless One
	// (K:etbCounter:P1P1:X, X:Count$xPaid) enters at seq 472 (4 seats), 182
	// (6 seats) and 795 (8 seats) with the move carrying Pairs = one P1P1
	// grant, and its body's CounterChange is now the notification-only
	// EntryCounterNotice instead of a real placement. Round 2 widened
	// absorption to EVERY counter kind (the payload now carries the kind as a
	// UTF-8 tail, events.EntryCounterPairs), so Chalice of the Void's
	// K:etbCounter:CHARGE:X body is absorbed too. Measured per seat, the ONLY
	// further difference between main and this build is one removed event per
	// affected game: Chalice enters from the library (X=0) and its zero-amount
	// CHARGE CounterChange (obj 170, seq 2360/5040/9081 at 4/6/8 seats) is no
	// longer emitted -- its placement is nothing, exactly as the absorbed
	// P1P1 zero case already behaved. The 2-seat game is unchanged because
	// neither death-n-taxes (index 0) nor dimir-tempo (index 1) carries a
	// K:etbCounter card; eldrazi-stompy (index 2) first joins the deck
	// rotation at 4 seats. MERGED-TREE ATTRIBUTION (this ticket merged main's
	// game-long damage-by-source provenance, task agent-20260923T114033Z-
	// a57ee463): the merged 4/6/8-seat heads are the superposition of the two
	// changes. Measured on the merged tip, disabling ONLY the entry-body
	// absorption (entryBodyCounterGrants returns nil) restores main's three
	// goldens exactly -- 4 9b3aab4e0336ba8c, 6 c4ce39421c473963,
	// 8 d2ebb7cbf40ccd12 -- so provenance alone gives main's values and the
	// entry-fold is the only further cause. Provenance alone leaves the 2-seat
	// head at 9b4759db7fe10fdd, unchanged by the entry-fold (the merged tip
	// reads exactly that), matching main.
	// 4 seats moved to d8a1ddfd311237dd (autonomous orchestrator): resolving agent-20260926T222248Z-9c652605 (Hide the ordinary Optional$ ChangeZone confirmation behind ForgetOtherRemembered)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to d4876057e0477830 (autonomous orchestrator): resolving agent-20260927T212305Z-e87e2ff0 (Gray Merchant of Asphodel drains but never gains the life lost)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 4 seats moved to a773fbb9fe06d201 (agent-20260928T214958Z-8421ac45):
	// registering Mode$ Transformed marks the battlefield SetState Mode$
	// Transform FlipFace with Text "Transformed" (CR 701.26), which changes
	// the event bytes for the one transform the golden 4-seat game performs --
	// Delver of Secrets flips once (dimir-tempo), via AB$ SetState | Mode$
	// Transform. Measured as a PURE HASH change, not a behaviour change: the
	// 4-seat event count is identical (5446) with and without the marker, the
	// single FlipFace is Delver's, and only Text differs; 2/6/8 seats are
	// unchanged (they perform no Mode$ Transform transform). Delver carries no
	// Mode$ Transformed trigger, so no trigger fires either way.
	4: "a773fbb9fe06d201",
	// 6 seats moved to c8c36b87e598c090 (autonomous orchestrator): resolving fb-20260914T033246Z-3f1cc033 (delver of secrets was played, but I was not prompted ... "you MAY reveal"... ...)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 6 seats moved to 5500cdf6c3ef69bc (autonomous orchestrator): resolving fb-20260914T063255Z-ce14a643 (gitaxian probe -- prompt should display user's hand to me, but it did not)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 6 seats moved to b5882f44d619a1c5 (autonomous orchestrator): resolving fb-20260914T114653Z-3a890b2a (player vs bot should flip a coin / roll a dice to determine who acts first)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 6 seats moved to ae1e8e5219b49537 (autonomous orchestrator): resolving inbox-rv2b-brainstorm-put-back-from-hand (Brainstorm-style "put cards from your hand back" silently does nothing (Jace [0])
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 6 seats moved to 802424189c08c7d1 (autonomous orchestrator): resolving inbox-rv2a-coinflip-round-clock-and-toss-display (Coin flip follow-up: round clock, pregame active seat, and how the toss is shown)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 6 seats moved to 59f6f1476c8a7f2d with task inbox-botbench-stability-run:
	// the same Dismember phyrexian-pip life preference as the 4-seat entry
	// above (measured: the arm's firings over the acceptance games are all
	// Dismember; the engine-side changes are heads-neutral by the same
	// revert measurement).
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 6 seats moved to 3989bf5e11a95729 (autonomous orchestrator): resolving inbox-rv2c-cost-modifiers-forge-parity (Engine cost modifiers diverge from Forge (Steelshaper, Rakdos, Ghalta, Color$) a)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 6 seats moved to 6ffd29493be18d92 with task inbox-engine-gap-rakdos-params:
	// the Return cost grammar (Return<N/Spec> — return a permanent to hand as
	// a cost) is now real, which reprices Daze's AlternativeCost static
	// (Cost$ Return<1/Island>) from the degraded {1} generic the unmodelled
	// token used to parse to. In the 6-seat golden game the dimir-tempo seat's
	// first divergence vs the old golden is at event 1350: Ponder on the
	// stack, the pool empty, and the alt-cost Daze cast now payable (the cost
	// has no mana part — the Island return IS the payment) where {1} from an
	// empty pool was not, so the option appears and the bot later casts it at
	// event 1639 (target Ponder, Underground Sea returned as the cost, the
	// unless-pay ask served). Winners and turn counts are measured unchanged;
	// the 2-, 4- and 8-seat heads are byte-identical. Measured by dumping both
	// complete 6-seat logs and diffing them.
	// Auto-accepted: the deck acceptance games replay byte-identically and
	// `make sim` 20/20 replay OK, the same proxy this repo has used by hand
	// for every head move — neither check is sensitive to bot-choice quality,
	// only engine correctness.
	// 6 seats moved to ee634ef28a13dfb8 (autonomous orchestrator): resolving inbox-rv2a-starting-player-state-and-mulligan-passes (Engine: record the starting player; CR 103.5 mulligan passes go round the table)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 6 seats moved to 9de9ac3590cae377 (autonomous orchestrator): resolving inbox-paramcensus-counterspell-and-spellcast-misc (param census: counterspell/spellcast-trigger misc (6 unrelated one-offs))
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 6 seats moved to 100db196f20a293a with task inbox-deck-ulalek-eldrazi
	// (rebased onto main, re-measured): the same tap-gate satisfiability
	// filter as the 2-seat entry. First divergence at event 6587 (seat 1's
	// tap ask at 6586, right after a land drop): the old gate taps option 0
	// toward the "6 B B" card's black pips with only blue sources offered,
	// the fixed gate declines; the neutralised stream reproduces main's
	// 9de9ac3590cae377 byte-for-byte, so the filter is the sole cause.
	// AR7 promotion (2026-09-19): the default bot now attacks with a creature
	// that is lethal to its defender if unblocked even into an unfavourable
	// block (botpolicy lethal pressure; held-out 2,066-1,934 vs the old bot).
	// Devotion/threshold count heads (2026-09-22): 6 seats moved to
	// 51ca01ded64c6b74 with task cli-20260922T150843Z-f8b39ebe
	// (feat(effects): Count$Threshold and Count$Devotion heads). Seat 5
	// (mono-green-stompy) casts Aspect of Hydra at event 7087 and resolves it
	// at 7108; the unmodelled Count$Devotion.Green made X read 0, so effPump
	// registered nothing — the fixed engine registers the real +X/+X
	// continuous pump (the first divergence is exactly the resolution's
	// AddContinuous clock_tick at event 7109; every earlier event is byte-
	// identical), the pumped creature then blocks differently, and the rest
	// of the stream follows.
	// vote_card1 (2026-09-22): 6 seats moved to a5d01c6f72adea2a — same card
	// ballot fix as the 4-seat entry. Seat 0 (death-n-taxes) casts Council's
	// Judgment at event 7354 and resolves it at 7428; first divergence at
	// event 7430, the first vote ask's decision_made: choose:[0] ->
	// choose:[3] (Geralf's Messenger, the highest-worth ballot entry; the
	// reveal Notes and the single exile follow the unanimous vote). Every
	// earlier event is byte-identical.
	// vote_card_self1 (2026-09-22): 6 seats moved to 2a545fc0bdd1fbc9 — same
	// card-ballot self-preference fix. First divergence at event 7434, the
	// THIRD vote ask's decision_made (Player 3): choose:[3] (Geralf's
	// Messenger, its own global highest) -> choose:[7] (Leatherback Baloth,
	// a foreign permanent); the Messenger tally still wins the exile. Every
	// earlier event is byte-identical.
	// RememberRevealed$ writes the source list (2026-09-22): main's side of
	// the merge moved 6 seats to 7e9f2574e2d31c90 with task
	// agent-20260919T203859Z-d20caa2e (count:Plus.<SVarName>) -- the same
	// Delver of Secrets PeekAndReveal/ClearRemembered cause as the 4-seat
	// entry above, measured by the same revert bisect. 2 and 8 seats are
	// unchanged (neither game resolves a Delver upkeep reveal under its
	// seed).
	// merge re-measure (2026-09-22, cli-20260922T150844Z-dfebd0b5): with BOTH
	// change sets present the measured head is 5e79231bd056d0fc -- each cause was
	// measured on its own side above.
	// Counter unless-cost SVar fold (2026-09-22,
	// cli-20260922T150843Z-c6c925c4): 6 seats moves to 76f187361778b000.
	// Mausoleum Wanderer (seat 4, obj 269, pushed at event 654) sacrifices
	// itself for its Counter ability (obj 361); its UnlessCost$ X is
	// SVar:X:Sacrificed$CardPower, which the strict parser could not price.
	// The shared fold now resolves it from the captured sacrifice LKI, so the
	// unless-pay ask is labelled with the real amount. First divergence is
	// exactly event 1396, the ModeChosen: "Pay the cost — don't counter" ->
	// "Pay {1} — don't counter"; every earlier event is byte-identical.
	// Measured by reverting the Counter arm of UnlessCostResolved's API gate
	// in a scratch copy: with it reverted this head returns to
	// 5e79231bd056d0fc and 2/4 seats never move at all.
	// unless-pay mana window (cli-20260922T150843Z-daf1bd3e): 6 seats moves on
	// to 400d8d9ae2777ded. Measured by neutralising exactly two switches in a
	// scratch copy (poseUnlessAsk's host payability consult in
	// effects/unless.go, and resumeResolution's unlessCostPayable guard plus
	// the mana-window arm in rules/resolution.go): the neutralised build
	// reproduces 76f187361778b000 byte-for-byte, so this change is the sole
	// mover. Both streams hold 8,732 events and differ in exactly TWO
	// payloads, each a ModeChosen text -- a decision-enumeration move, not a
	// game-outcome move. Event 1396 is the same Mausoleum Wanderer election
	// the entry above describes: the SVar fold prices it at {1}, the offer
	// gate then finds seat 1 has neither floating mana nor a window-eligible
	// source, so "Pay {1} — don't counter" becomes "Don't pay". Event 1671 is
	// seat 1's Daze (obj 98, cast at 1639) asking seat 2 for {1} with the same
	// verdict ("Pay 1 — don't counter" -> "Don't pay"). Both were unreachable
	// before too: in the neutralised stream each recorded pay attempt fails
	// and the spell is countered anyway, so events 1397 and 1672 onward are
	// identical.
	// 6 seats moved to 3bd695df72d9d4c9 (autonomous orchestrator): resolving cli-20260922T225141Z-48972afc (approx: a mulligan's REDRAW resolves immediately, interleaved with the rest of t)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// CR 103.1: the same toss-choice ask/answer moves the 6-seat chain;
	// disabling only the ask restores 3bd695df72d9d4c9.
	// AR9 swarm lethal (botpolicy/combat.go swarmLethalSubset, cardfuzz
	// bigboard work): 6 seats moves to a93593d452866261. The bot now forces the smallest
	// attacking set that is lethal even if the defender blocks its biggest
	// attackers with every untapped creature. Measured cause: disabling only
	// swarmLethalSubset (return nil) restores the previous golden; the
	// block-risk memo, killBlockCost's linear prefix scan and the Ascend
	// pre-filter in the same change are head-neutral.
	// cli-20260924T034908Z-be6139b0 (CR 614.12 entry-counter fold): see the
	// 4-seat note. eldrazi-stompy is in this game too, so Endless One's entry
	// counters fold into the MoveZone and Chalice of the Void's zero-amount
	// CHARGE placement is absorbed (obj 170, seq 5040, one removed event).
	// 6 seats moved to 3ea72bbb6e715b03 (autonomous orchestrator): resolving agent-20260919T183836Z-df01c8be (trig:Evolved — "whenever this creature evolves" does not fire)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// CR 302.6: sick creature {T}/{Q} mana abilities are no longer offered; 6-seat head measured at 16182bb0d97a61c8.
	// 6 seats moved to b4a5d33f302e6bfc (autonomous orchestrator): resolving agent-20260927T212305Z-e87e2ff0 (Gray Merchant of Asphodel drains but never gains the life lost)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 6 seats moved to 1f26f6cda52c1beb (autonomous orchestrator): resolving agent-20260929T011510Z-3648a30d (A crew-animated Vehicle that blinks returns still animated (CR 400.7 / CR 702.12)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	6: "1f26f6cda52c1beb",
	// 8 seats moved to cc022f9ba9f2bf39 with task mana2 (fix(rules): pay mana
	// ability costs and choose colors): mana abilities that spend a Sac cost
	// are now gated on a payable, deterministic sacrifice candidate existing,
	// instead of being offered unconditionally. In the 8-seat acceptance game,
	// Skirk Prospector's `Sac<1/Goblin>` ability has no eligible Goblin from
	// turns 30-46; the old (buggy) engine offered it anyway, the fixed engine
	// correctly withholds it, changing the bot's trajectory from that point.
	//
	// 8 seats moved again to 8552ec43c71b76e5 with task anytgt1 (fix(effects):
	// restrict any targets to damageable card types): CR 115.4's "any target"
	// no longer matches every card type. At seq 14190 seat 6 casts Lightning
	// Bolt (obj 407); the pre-fix target offer had 32 options including 17
	// illegal basic lands, now correctly down to 15. The bot picks the same
	// object (Garruk's Companion, obj 341) in both streams -- verified by
	// diffing the two 15,260-event streams directly: exactly one payload
	// differs (the target-offer's option list), TargetsChosen and every later
	// event are identical. No trajectory change, only the corrected option
	// count that precedes the bot's unchanged pick.
	// 8 seats moved again to 693dccb26b2b09ff with task ba1 (feat(botpolicy):
	// keep a mana reserve for instant-speed plays, I-13 B2): chooseCast now
	// prefers a cast that leaves the pool at or above the cost of the
	// cheapest instant-speed card in hand/command zone over a comparable
	// cast that empties it. This is a real bot-policy trajectory change, not
	// a bug fix to a wrong value, so it was not traced event-by-event the
	// way anytgt1's was above -- verified instead by the CR conformance lane
	// (0 FAIL) and `make sim` (20/20 replay OK), neither of which is
	// sensitive to bot choice quality, only to engine correctness. 2/4/6
	// seats are unmoved: the reserve preference is a tie-break among
	// otherwise-comparable casts, and evidently only the 8-seat acceptance
	// game's board states hit that tie often enough to change course.
	// 8 seats moved to f73802c021dea724 (autonomous orchestrator): resolving fb-20260914T033246Z-3f1cc033 (delver of secrets was played, but I was not prompted ... "you MAY reveal"... ...)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to 6d153fa9cd9b61f4 (autonomous orchestrator): resolving fb-20260914T063255Z-ce14a643 (gitaxian probe -- prompt should display user's hand to me, but it did not)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to d9f960ddd818ded2 (autonomous orchestrator): resolving fb-20260914T084916Z-71c842c2 (goblin piledrive, should get +2/+0 for each other attacking goblin.... doesn'...)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to 83ffd462c2aa6819 (autonomous orchestrator): resolving fb-20260914T114653Z-3a890b2a (player vs bot should flip a coin / roll a dice to determine who acts first)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to 0fc2433539e2f454 (autonomous orchestrator): resolving inbox-engine-gap-event-triggers (Engine: Sacrifice, discard, tap and crime triggers)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to fde5ae50e5f7e31b (autonomous orchestrator): resolving inbox-rv2d-look-at-hand-stays-private (Hidden-information leak: "look at target's hand" is broadcast to every seat and )
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to c54d57bf94915dcb (autonomous orchestrator): resolving inbox-engine-gap-alt-costs-a (Engine: Alternative costs I: evoke, dash, encore, overload, warp, madness)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to 324d66dfb43440ce (autonomous orchestrator): resolving inbox-rv2b-brainstorm-put-back-from-hand (Brainstorm-style "put cards from your hand back" silently does nothing (Jace [0])
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to 46a7d9dcadfdccf7 (autonomous orchestrator): resolving inbox-rv2a-coinflip-round-clock-and-toss-display (Coin flip follow-up: round clock, pregame active seat, and how the toss is shown)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to 0122b7ae46b57cb4 with task inbox-botbench-stability-run:
	// the same Dismember phyrexian-pip life preference as the 4-seat entry
	// above (measured: the arm's firings over the acceptance games are all
	// Dismember; the engine-side changes are heads-neutral by the same
	// revert measurement).
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to cc24946a41837da8 (autonomous orchestrator): resolving inbox-rv2c-cost-modifiers-forge-parity (Engine cost modifiers diverge from Forge (Steelshaper, Rakdos, Ghalta, Color$) a)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to c5487ff2d727e5d8 (autonomous orchestrator): resolving inbox-rv2a-starting-player-state-and-mulligan-passes (Engine: record the starting player; CR 103.5 mulligan passes go round the table)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to e558ffcaab15d987 with task
	// inbox-paramcensus-action-condition-checksvar: the same Vampire Lacerator
	// ConditionCheckSVar$ enforcement as the 4-seat entry above -- measured
	// first divergence at seq 6724, the mono-black-aggro Lacerator trio's
	// upkeep trigger resolving with an opponent (dimir-tempo) at 3 life, so
	// the PlayerCountOpponents$LowestLifeTotal GE11 gate holds and the old
	// build's unconditional `life` loss is correctly skipped.
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to bcc3636853dbc777 (autonomous orchestrator): resolving inbox-paramcensus-counterspell-and-spellcast-misc (param census: counterspell/spellcast-trigger misc (6 unrelated one-offs))
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to 35522b0d8c5d2bf9 with task inbox-deck-ulalek-eldrazi
	// (rebased onto main, re-measured): the same tap-gate satisfiability
	// filter as the 2-seat entry. First divergence at event 2433 (seat 7's
	// tap ask at 2432, the window after Lotus Petal resolved at 2428): the
	// old gate taps Lotus Petal (an Any production, zero known colour slots)
	// toward a {1}{B} card's black pip, the fixed gate falls through and
	// pushes Lion's Eye Diamond at 2435 instead; the neutralised stream
	// reproduces main's bcc3636853dbc777 byte-for-byte, so the filter is the
	// sole cause.
	// AR7 promotion (2026-09-19): the default bot now attacks with a creature
	// that is lethal to its defender if unblocked even into an unfavourable
	// block (botpolicy lethal pressure; held-out 2,066-1,934 vs the old bot).
	// 8 seats moved to 2fe007e9f370364c (autonomous orchestrator): resolving agent-20260920T221158Z-37697abf (Player-target offers ignore the player spec's You/Opponent qualifier)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// Devotion/threshold count heads (2026-09-22): 8 seats moved to
	// 275fb23897e22f6e with task cli-20260922T150843Z-f8b39ebe
	// (feat(effects): Count$Threshold and Count$Devotion heads). Seat 5
	// (mono-green-stompy) casts Aspect of Hydra at event 11823 and resolves a
	// copy at 11858; the unmodelled Count$Devotion.Green made X read 0, so
	// effPump registered nothing — the fixed engine registers the real +X/+X
	// continuous pump (the first divergence is exactly the resolution's
	// AddContinuous clock_tick at 11859; every earlier event is byte-
	// identical, and Cabal Ritual appears in NEITHER stream, so the measured
	// sole first-mover is Aspect of Hydra, as at 6 seats).
	// vote_card1 (2026-09-22): 8 seats moved to cdeed8f4963686ac — same card
	// ballot fix as the 4-seat entry. Seat 0 (death-n-taxes) casts Council's
	// Judgment at event 11127 and resolves it at 11217; first divergence at
	// event 11219, the first vote ask's decision_made: choose:[0] ->
	// choose:[5] (Leatherback Baloth, the highest-worth ballot entry; the
	// reveal Notes and the single exile follow the unanimous vote). Every
	// earlier event is byte-identical.
	// vote_card_self1 (2026-09-22): 8 seats moved to c8dddeb452ccb5b3 — same
	// fix. First divergence at event 11223, the FOURTH vote ask's
	// decision_made (Player 5): choose:[5] (Leatherback Baloth, its own
	// global highest) -> choose:[0] (Vampire Lacerator, a foreign permanent);
	// the Baloth tally still wins the exile. Every earlier event is
	// byte-identical.
	// Mana sacrifice choice (cli-20260922T150843Z-44188ca1): 8 seats moves to
	// b14f1fc52a6835ed. After seat 6 selects Skirk Prospector's mana ability
	// at event 8064, the fixed engine asks at 8066 because more than one Goblin
	// is eligible; the bot chooses option 1 and sacrifices Prospector at 8068.
	// The prior exact-only gate withheld that activation, the first difference
	// at 8065, so this is the authorized >N behaviour.
	// Counter unless-cost SVar fold (2026-09-22,
	// cli-20260922T150843Z-c6c925c4): 8 seats moves to 7c9dbf608ada58b3, the
	// same Mausoleum Wanderer cause as at 6 seats (source obj 269, ability
	// obj 481, payer seat 7). First divergence is event 2388, the same
	// ModeChosen label change to "Pay {1} — don't counter". The payer cannot
	// cover the tax either way, so the target is countered in both streams
	// and the recorded ask label is the sole first difference; the bot's
	// later trajectory follows from the changed log. Attributed by the same
	// scratch revert, which returns this head to b14f1fc52a6835ed.
	// Resolution-time mana colour choice (cli-20260922T225137Z-c670f42d): 8
	// seats moves to 8657a61fc80a8576. The real colour decision replaces the
	// former colourless ManaAdd.Counter stand-in; its selected WUBRG counter
	// is an intentional event payload difference and changes the deterministic
	// downstream game.
	// unless-pay mana window (cli-20260922T150843Z-daf1bd3e): 8 seats moves on
	// to 0b8b0506edbedc2e, same two-switch neutralisation as the 6-seat entry
	// (the neutralised build reproduces 7c9dbf608ada58b3 byte-for-byte). Both
	// streams hold 16,821 events and differ in exactly ONE payload: event
	// 2388, the same Mausoleum Wanderer election (ability obj 481, payer seat
	// 7, against Duress obj 472), whose priced-but-unreachable "Pay {1} —
	// don't counter" is no longer offered and reads "Don't pay". Duress is
	// countered in both streams (event 2389 onward identical).
	// MERGE (both changes present, measured at the merged tip): 8 seats reads
	// 61ac399dda414c21 -- the resolution-time colour choice and the unless-pay
	// mana window together move the same seat-8 game past either single
	// change's value.
	// 8 seats moved to 5e988854bf022347 (autonomous orchestrator): resolving cli-20260922T225140Z-677ee477 (approx: bot target selection is effect-blind except for literal damage — destroy)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to 5c90b1b3a0b38f25 (autonomous orchestrator): resolving cli-20260922T225141Z-48972afc (approx: a mulligan's REDRAW resolves immediately, interleaved with the rest of t)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// CR 103.1: the same toss-choice ask/answer moves the 8-seat chain;
	// disabling only the ask restores 5c90b1b3a0b38f25.
	// mayplay-mfa: 8 seats now tags artifact-produced white mana. A scratch
	// comparison with only the producer tag disabled reproduced the old
	// golden; all 14,889 events have identical ordering and actions, with
	// exactly two changed payloads: events 2441 (W -> ArtifactW, 1 unit)
	// and 2492 (W -> ArtifactW, 3 units from Lion's Eye Diamond).
	// AR9 swarm lethal (botpolicy/combat.go swarmLethalSubset, cardfuzz
	// bigboard work): 8 seats moves to af5b2fc1561b5f56. The bot now forces the smallest
	// attacking set that is lethal even if the defender blocks its biggest
	// attackers with every untapped creature. Measured cause: disabling only
	// swarmLethalSubset (return nil) restores the previous golden; the
	// block-risk memo, killBlockCost's linear prefix scan and the Ascend
	// pre-filter in the same change are head-neutral.
	// bot-x1: 8 seats moves to beac5729b0e63dcc (see the 2-seat note).
	// cli-20260924T034908Z-be6139b0 (CR 614.12 entry-counter fold): see the
	// 4-seat note; eldrazi-stompy is in this game, so Endless One's entry
	// counters fold into the MoveZone and Chalice of the Void's zero-amount
	// CHARGE placement is absorbed (obj 170, seq 9081, one removed event).
	// 8 seats moved to 13ba766ebeeb29e7 (autonomous orchestrator): resolving agent-20260919T183836Z-df01c8be (trig:Evolved — "whenever this creature evolves" does not fire)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// 8 seats moved to 15be7dadc85b47df (autonomous orchestrator): resolving agent-20260926T222248Z-9c652605 (Hide the ordinary Optional$ ChangeZone confirmation behind ForgetOtherRemembered)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	// CR 302.6: sick creature {T}/{Q} mana abilities are no longer offered; 8-seat head measured at 5a7a1a2a41b48fa8.
	// 8 seats moved to b73492c5ccdaadca (autonomous orchestrator): resolving agent-20260927T212305Z-e87e2ff0 (Gray Merchant of Asphodel drains but never gains the life lost)
	// Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,
	// the same proxy this repo has used by hand for every head move -- neither
	// check is sensitive to bot-choice quality, only engine correctness.
	8: "b73492c5ccdaadca",
}

func TestHeads(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	for _, seats := range []int{2, 4, 6, 8} {
		got := acceptanceHead(t, reg, seats)
		if want := acceptanceHeads[seats]; got != want {
			t.Errorf("%d seats: chain head %s, golden %s — if this move is intended, update acceptanceHeads and name the cause in the commit body", seats, got, want)
		}
	}
}
