// Animate's HiddenKeywords$ parameter is Forge's derived-keyword grant. This
// file pins the three measured phrase shapes end to end on the real corpus
// cards:
//
//   - Opportunistic Dragon / Extraction Specialist:
//     `CARDNAME can't attack or block.` -- the compound spelling must impart
//     BOTH restrictions to the stolen/returned permanent while the host
//     remains, and release both when the host leaves (Duration$
//     UntilHostLeavesPlay).
//   - Coward Killer: `CARDNAME can't block.` -- the simple spelling forbids
//     blocking but not attacking.
//   - Elemental Uprising / Vengeant Earth / Disturbed Slumber:
//     `CARDNAME must be blocked if able.` -- a CR 509.1c requirement on the
//     ATTACKER to receive at least one legal blocker, released when no legal
//     pair exists or the animation expires.
//
// The rules readers under test are the derived-keyword ones in statics.go
// (parseHiddenKeyword / hasCantAttackKeyword / hasCantBlockKeyword /
// hasMustBeBlockedKeyword), so a Pump/PumpAll `KW$ <same sentence>` reaches
// the same oracle without hardcoding a card name; the last test proves that
// with a printed `K:` line rather than a HiddenKeywords$ grant.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// castCorpusSpell drives a real corpus instant/sorcery from seat 0's hand,
// chooses `target` at its KTarget ask, and drains the stack. It asserts every
// step so a missing mana source, cast option or target offer is a loud
// failure, not a silently uncast spell.
func castCorpusSpell(t *testing.T, e *Engine, name, symbols string, target state.ObjID) {
	t.Helper()
	ensureInHand(t, e, 0, name)
	addMana(t, e, 0, symbols)
	castCardNow(t, e, name)
	d := passToTargetAsk(t, e)
	idx := -1
	for _, o := range d.Options {
		if o.Obj == target {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("%s target %d not offered: %+v", name, target, d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 40)
}

// ensureInHand moves seat p's named card to hand from wherever it starts
// (hand or library), as a logged MoveZone so replay reproduces it. It is a
// no-op when the card is already in hand -- a hand->hand move would be a
// bogus zone event.
func ensureInHand(t *testing.T, e *Engine, p state.PlayerID, name string) {
	t.Helper()
	for _, id := range e.G.Zone(state.ZHand, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return
		}
	}
	moveByName(t, e, p, name, state.ZHand)
}

// declareAttackersOnly clears summoning sickness on seat `active`'s named
// attackers and emits the engine's own DeclareAttackers event shape (Player
// is the DEFENDER), leaving the step parked at declare-blockers. Unlike
// attackSeat0 it never rewrites e.G.Active, so a replayCheck can reproduce
// the whole board from the event log.
func declareAttackersOnly(t *testing.T, e *Engine, active, defender state.PlayerID, attackers ...state.ObjID) {
	t.Helper()
	for _, id := range attackers {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != active {
			t.Fatalf("precondition: attacker %d not on seat %d's battlefield: %+v", id, active, o)
		}
		o.SummonSick = false
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: defender, IDs: attackers})
	e.G.Step = state.StepDeclareBlockers
}

// TestOpportunisticDragonHiddenKeywords: the real Dragon's theft rider grants
// the compound restriction as a derived layer-6 keyword, so while the Dragon
// lives the stolen Ornithopter can neither attack nor block; killing the host
// releases both. The preconditions make each comparison honest: the object is
// a battlefield creature under seat 0's control, its printed keyword really
// existed, and the Seat-1 attacker really was blockable by it once the
// restriction lifted.
func TestOpportunisticDragonHiddenKeywords(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Opportunistic Dragon"), card(t, "Name:Test SeatZero Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")},
		[]*cards.Card{lookup(t, reg, "Ornithopter"), card(t, "Name:Test SeatOne Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")})

	vid := moveByName(t, e, 1, "Ornithopter", state.ZBattlefield)
	vo := e.G.Obj(vid)
	if vo == nil || vo.Zone != state.ZBattlefield || vo.Controller != 1 {
		t.Fatalf("precondition: Ornithopter not on seat 1's battlefield: %+v", vo)
	}
	if !e.HasKeyword(vid, "Flying") {
		t.Fatalf("precondition: Ornithopter printed keywords = %v, want Flying", e.Derived(vid).Keywords)
	}

	did := moveByName(t, e, 0, "Opportunistic Dragon", state.ZBattlefield)
	if o := e.G.Obj(did); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Dragon not on seat 0's battlefield: %+v", o)
	}
	d := passToTargetAsk(t, e)
	idx := -1
	for _, o := range d.Options {
		if o.Obj == vid {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Ornithopter not offered as the Dragon's theft target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 30)

	// Post-theft preconditions: creature on the battlefield, untapped, under
	// the Dragon's controller -- so a canBlock/canAttack false below is the
	// restriction's doing, not a zone/tap/control gate.
	vo = e.G.Obj(vid)
	if vo.Zone != state.ZBattlefield || vo.Tapped || vo.Controller != 0 || !e.IsCreature(vid) {
		t.Fatalf("precondition: stolen Ornithopter state wrong: zone=%v tapped=%v controller=%d creature=%v",
			vo.Zone, vo.Tapped, vo.Controller, e.IsCreature(vid))
	}
	// The grant really landed (not a silent no-op): the derived list carries
	// the compound sentence.
	found := false
	for _, k := range e.Derived(vid).Keywords {
		if strings.EqualFold(cards.KeywordHead(k), "CARDNAME can't attack or block.") {
			found = true
		}
	}
	if !found {
		t.Fatalf("HiddenKeywords grant missing from the stolen Ornithopter's derived keywords: %v",
			e.Derived(vid).Keywords)
	}

	// The two restrictions the compound spelling must impart.
	if !e.hasCantAttackKeyword(vid) || !e.hasCantBlockKeyword(vid) {
		t.Fatalf("compound keyword did not reach both readers (attack=%v block=%v)",
			e.hasCantAttackKeyword(vid), e.hasCantBlockKeyword(vid))
	}
	// Attacker offers/validation: attackBlocked is the oracle attackOffers and
	// validateAttackers both read.
	if !e.attackBlocked(vid, 1, 0) {
		t.Fatal("stolen Ornithopter is still allowed to attack while the Dragon lives")
	}
	for _, of := range e.attackOffers() {
		if of.id == vid {
			t.Fatalf("stolen Ornithopter still offered as an attacker: %+v", of)
		}
	}
	// Replay the whole Animate/ability-strip grant from the event log BEFORE
	// the combat-setup mutations below (which are direct fields, not logged
	// events), so the grant itself is pinned replay-exact.
	replayCheck(t, e, cfg)
	// Blocker offers/validation: canBlock is the oracle askBlockers and
	// validateBlockers both read. A Seat-1 attacker is declared against seat 0
	// so the pair is otherwise legal (controller/tap/zone preconditions above).
	// The attacker is a seeded deck card moved by a logged event, never a
	// direct onBoardCard placement, so replayCheck below reproduces the board.
	atk := moveByName(t, e, 1, "Test SeatOne Bear", state.ZBattlefield)
	declareAttackersOnly(t, e, 1, 0, atk)
	if e.G.Obj(atk).Controller != 1 || !e.G.Obj(atk).IsAttacking {
		t.Fatalf("precondition: attacker state wrong: %+v", e.G.Obj(atk))
	}
	if e.canBlock(vid, atk) {
		t.Fatal("stolen Ornithopter is still allowed to block while the Dragon lives")
	}
	// The ONLY gate that changed is the derived restriction: the raw
	// block-restriction read agrees, and the object is otherwise a legal
	// blocker (creature, untapped, defender's controller).
	if !e.blockRestricted(vid, atk) {
		t.Fatal("precondition: blockRestricted false -- the canBlock false above came from another gate")
	}

	// Kill the host: UntilHostLeavesPlay ends every half of the grant.
	e.emit(events.Event{Kind: events.MoveZone, Obj: did,
		From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(did); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: the Dragon did not leave: %+v", o)
	}
	if e.hasCantAttackKeyword(vid) || e.hasCantBlockKeyword(vid) {
		t.Fatalf("restriction outlived the Dragon (attack=%v block=%v)",
			e.hasCantAttackKeyword(vid), e.hasCantBlockKeyword(vid))
	}
	// Attacker direction released unconditionally.
	if e.attackBlocked(vid, 1, 0) {
		t.Fatal("stolen Ornithopter still cannot attack after the Dragon left")
	}
	// Blocker direction: control reverts to seat 1 when the host leaves, so
	// the pair is only otherwise-legal when the attacker is seat 0's. Declare
	// one and assert the release through canBlock.
	if e.G.Obj(vid).Controller == 0 {
		if !e.canBlock(vid, atk) {
			t.Fatal("stolen Ornithopter still cannot block after the Dragon left")
		}
	} else {
		// The owner has it back; a seat-0 attacker is the matching direction.
		atk0 := moveByName(t, e, 0, "Test SeatZero Bear", state.ZBattlefield)
		declareAttackersOnly(t, e, 0, 1, atk0)
		if !e.canBlock(vid, atk0) {
			t.Fatalf("returned Ornithopter cannot block a seat-0 attacker (controller=%d)", e.G.Obj(vid).Controller)
		}
	}
}

// TestCowardKillerHiddenKeywords: the real Coward Killer's simple spelling
// forbids blocking but NOT attacking.
func TestCowardKillerHiddenKeywords(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Coward"), card(t, "Name:Test SeatZero Attacker\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")},
		[]*cards.Card{card(t, "Name:Test Blockable Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")})

	bear := moveByName(t, e, 1, "Test Blockable Bear", state.ZBattlefield)
	castCorpusSpell(t, e, "Coward", "1R", bear)

	if !e.hasCantBlockKeyword(bear) {
		t.Fatalf("Coward Killer did not forbid blocking: derived keywords = %v", e.Derived(bear).Keywords)
	}
	if e.hasCantAttackKeyword(bear) {
		t.Fatal("Coward Killer's can't-block spelling wrongly forbade attacking")
	}
	// Replay the cast + grant before the combat-setup mutations below (direct
	// fields, not logged events).
	replayCheck(t, e, cfg)
	// Otherwise-legal block pair: seat 0's attacker declared against seat 1.
	atk := moveByName(t, e, 0, "Test SeatZero Attacker", state.ZBattlefield)
	declareAttackersOnly(t, e, 0, 1, atk)
	if e.canBlock(bear, atk) {
		t.Fatal("Coward Killer's target can still block")
	}
	// It may still attack: the restriction is block-only.
	if e.attackBlocked(bear, 0, 0) {
		t.Fatal("Coward Killer's can't-block spelling wrongly blocked attacking")
	}
}

// TestElementalUprisingMustBeBlocked: the real Elemental Uprising animates a
// seat-0 land into a 4/4 Elemental with haste and a CR 509.1c requirement to
// be blocked if able. One legal blocker exists, so the empty declaration is
// illegal and a block is legal and commits.
func TestElementalUprisingMustBeBlocked(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Elemental Uprising")},
		[]*cards.Card{card(t, "Name:Test Ground Blocker\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")})

	land := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	if o := e.G.Obj(land); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Mountain not on seat 0's battlefield: %+v", o)
	}
	if e.IsCreature(land) {
		t.Fatal("precondition: Mountain is already a creature")
	}
	castCorpusSpell(t, e, "Elemental Uprising", "1G", land)

	if !e.IsCreature(land) || e.Derived(land).Power != 4 {
		t.Fatalf("precondition: Mountain not animated 4/4: %v %d/%d",
			e.Derived(land).Types, e.Derived(land).Power, e.Derived(land).Toughness)
	}
	if !e.hasMustBeBlockedKeyword(land) {
		t.Fatalf("Elemental Uprising did not grant must-be-blocked: derived keywords = %v", e.Derived(land).Keywords)
	}
	// Replay the cast + animation + keyword grant before the combat-setup
	// mutations below (direct fields, not logged events).
	replayCheck(t, e, cfg)
	// A blocker on the defending seat and no other attacker: the requirement
	// has a legal pair, so it must bind.
	blocker := moveByName(t, e, 1, "Test Ground Blocker", state.ZBattlefield)
	// The animated land is seat 0's; declare it attacking seat 1 (the
	// engine's active player is seat 0 on turn 1), so askBlockers poses seat
	// 1's declare-blockers decision.
	declareAttackersOnly(t, e, 0, 1, land)

	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("no blockers decision posed for a must-be-blocked attacker with a legal blocker")
	}
	opt := findBlockOption(d, blocker, land)
	if opt == nil {
		t.Fatalf("legal block pair not offered: %+v", d.Options)
	}
	if !opt.AttackMust {
		t.Fatalf("block option against the must-be-blocked attacker not flagged: %+v", opt)
	}
	if opt.Required || opt.BlockMust {
		t.Fatalf("attacker requirement wrongly marked as a blocker requirement: %+v", opt)
	}
	// The empty declaration must be rejected: the requirement is not met.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err == nil {
		t.Fatal("empty declaration satisfied the must-be-blocked requirement")
	}
	// A block is legal and commits.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("the legal blocking declaration was rejected: %v", err)
	}
	if !blockCommitted(t, e, land, blocker) {
		t.Fatalf("block did not commit: BlockedBy=%v", e.G.Obj(land).BlockedBy)
	}
}

// TestMustBeBlockedNotForcedOntoEveryBlocker pins the CR 509.1c direction:
// with two attackers, only the must-be-blocked one carries a requirement, and
// a single blocker satisfies it by blocking THAT attacker. Blocking only the
// ordinary attacker is illegal (0 of 1); blocking the required one is legal.
func TestMustBeBlockedNotForcedOntoEveryBlocker(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	req := onBoardCard(t, e, 1, card(t, strings.Replace(printedMustBeBlockedSrc, "PT:2/2", "PT:4/4", 1)))
	plain := onBoardCard(t, e, 1, card(t, "Name:Test Plain Attacker\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	blocker := onBoardCard(t, e, 0, card(t, "Name:Test One Blocker\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	attackSeat0(t, e, req, plain)

	// Preconditions: both attackers are on the battlefield and the single
	// blocker can legally block each of them (so the ONLY constraint is the
	// requirement), and exactly the required attacker carries the keyword.
	if e.G.Obj(req).Zone != state.ZBattlefield || e.G.Obj(plain).Zone != state.ZBattlefield {
		t.Fatal("precondition: an attacker is not on the battlefield")
	}
	if !e.canBlock(blocker, req) || !e.canBlock(blocker, plain) {
		t.Fatalf("precondition: blocker cannot block both attackers (req=%v plain=%v)",
			e.canBlock(blocker, req), e.canBlock(blocker, plain))
	}
	if !e.hasMustBeBlockedKeyword(req) || e.hasMustBeBlockedKeyword(plain) {
		t.Fatalf("precondition: keyword on the wrong attacker (req=%v plain=%v)",
			e.hasMustBeBlockedKeyword(req), e.hasMustBeBlockedKeyword(plain))
	}

	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("no blockers decision posed")
	}
	reqOpt := findBlockOption(d, blocker, req)
	plainOpt := findBlockOption(d, blocker, plain)
	if reqOpt == nil || plainOpt == nil {
		t.Fatalf("both pairs must be offered: %+v", d.Options)
	}
	if !reqOpt.AttackMust || plainOpt.AttackMust {
		t.Fatalf("only the required attacker's option must be flagged: req=%+v plain=%+v", reqOpt, plainOpt)
	}
	// Blocking ONLY the ordinary attacker leaves the requirement unmet.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{plainOpt.Index}}); err == nil {
		t.Fatal("blocking only the ordinary attacker satisfied the must-be-blocked requirement")
	}
	// Blocking the required attacker is legal and does NOT force the blocker
	// to also block the plain attacker (one blocker cannot block two).
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{reqOpt.Index}}); err != nil {
		t.Fatalf("blocking the required attacker was rejected: %v", err)
	}
	if !blockCommitted(t, e, req, blocker) {
		t.Fatal("required-attacker block did not commit")
	}
	if len(e.G.Obj(plain).BlockedBy) != 0 {
		t.Fatalf("plain attacker was forced to be blocked too: %v", e.G.Obj(plain).BlockedBy)
	}
}

// TestMustBeBlockedBotAnswerNeverLivelocks runs the actual bot answer through
// Submit on the must-be-blocked board: the shared oracle (blockRequiredCore
// via Clamp/FitRequired) must repair the bot's own answer to a declaration the
// validator accepts, so the deterministic bot cannot re-derive a rejected one
// forever.
func TestMustBeBlockedBotAnswerNeverLivelocks(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	req := onBoardCard(t, e, 1, card(t, strings.Replace(printedMustBeBlockedSrc, "PT:2/2", "PT:4/4", 1)))
	plain := onBoardCard(t, e, 1, card(t, "Name:Test Plain Attacker\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	blocker := onBoardCard(t, e, 0, card(t, "Name:Test One Blocker\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	attackSeat0(t, e, req, plain)
	d := askBlockersFresh(t, e)
	if d == nil || d.RequiredQuota() < 1 {
		t.Fatalf("precondition: decision has no required attacker block: %+v", d)
	}
	bot := newTestBot(11)
	in := bot.answer(e, d)
	if err := e.Submit(in); err != nil {
		t.Fatalf("bot's own answer %v rejected (livelock): %v", in.Choices, err)
	}
	if e.Pending() == d {
		t.Fatal("bot answer did not consume the blockers decision")
	}
	if !blockCommitted(t, e, req, blocker) {
		t.Fatalf("bot answer did not satisfy the must-be-blocked requirement: BlockedBy=%v", e.G.Obj(req).BlockedBy)
	}
}

// TestMustBeBlockedReleasesWhenImpossible pins the "if able" half. It pairs a
// POSITIVE control (a ground must-be-blocked attacker with a legal ground
// blocker, which must carry the requirement) with the impossible case (a
// must-be-blocked FLIER the ground blocker cannot reach, which must not), so
// the impossible half cannot pass vacuously if the wiring is reverted -- the
// positive half fails in the same function.
func TestMustBeBlockedReleasesWhenImpossible(t *testing.T) {
	t.Parallel()
	// Positive control: legal pair binds.
	e := threeSeatEngine(t)
	reqGround := onBoardCard(t, e, 1, card(t, printedMustBeBlockedSrc))
	blocker := onBoardCard(t, e, 0, card(t, "Name:Test Ground Only\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	attackSeat0(t, e, reqGround)
	if !e.hasMustBeBlockedKeyword(reqGround) || !e.canBlock(blocker, reqGround) {
		t.Fatalf("precondition: positive control wrong (kw=%v canBlock=%v)",
			e.hasMustBeBlockedKeyword(reqGround), e.canBlock(blocker, reqGround))
	}
	dPos := askBlockersFresh(t, e)
	pos := findBlockOption(dPos, blocker, reqGround)
	if pos == nil || !pos.AttackMust {
		t.Fatalf("positive control: legal must-be-blocked pair not flagged: %+v", dPos.Options)
	}
	if err := e.Submit(decision.Intent{Seq: dPos.Seq, Player: dPos.Player, Choices: []int{}}); err == nil {
		t.Fatal("positive control: empty declaration satisfied the requirement")
	}

	// Impossible pair: the required attacker is a flier the ground blocker
	// cannot reach, so no offered pair carries the requirement and the empty
	// declaration is legal.
	e2 := threeSeatEngine(t)
	reqFlier := onBoardCard(t, e2, 1, card(t, strings.Replace(printedMustBeBlockedSrc,
		"Types:Creature Elemental\n", "Types:Creature Elemental\nK:Flying\n", 1)))
	blocker2 := onBoardCard(t, e2, 0, card(t, "Name:Test Ground Only\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	attackSeat0(t, e2, reqFlier)

	// Preconditions: the required attacker has the keyword and Flying, the
	// blocker is ground and cannot reach it -- so the sole pair is illegal.
	if !e2.hasMustBeBlockedKeyword(reqFlier) {
		t.Fatal("precondition: fixture did not grant the must-be-blocked keyword")
	}
	if !e2.HasKeyword(reqFlier, "Flying") {
		t.Fatal("precondition: fixture attacker is not a flier")
	}
	if e2.canBlock(blocker2, reqFlier) {
		t.Fatal("precondition: the ground blocker can reach the flier; the case under test needs it illegal")
	}

	// No legal pair -> no option -> no requirement. A decision may still be
	// posed for other reasons, but it must not demand a required block and the
	// forced empty declaration must be legal.
	d := askBlockersFresh(t, e2)
	if d != nil {
		for _, o := range d.Options {
			if o.AttackMust {
				t.Fatalf("requirement marked on an illegal pair: %+v", o)
			}
		}
		if err := e2.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
			t.Fatalf("empty declaration rejected although blocking was impossible: %v", err)
		}
	}
	if len(e2.G.Obj(reqFlier).BlockedBy) != 0 {
		t.Fatalf("an impossible block was somehow committed: %v", e2.G.Obj(reqFlier).BlockedBy)
	}
}

// TestPrintedMustBeBlockedKeywordReadsThroughTheSameReader proves the reader is
// not hardcoded to the three HiddenKeywords$ carriers: a printed `K:` line
// with the identical sentence reaches the same oracle (this is the shape 40
// corpus files use, mostly through Pump/PumpAll `KW$`).
func TestPrintedMustBeBlockedKeywordReadsThroughTheSameReader(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	req := onBoardCard(t, e, 1, card(t, "Name:Printed Must Block\nManaCost:1\nTypes:Creature Bear\nPT:2/2\n"+
		"K:CARDNAME must be blocked if able.\nOracle:x\n"))
	blocker := onBoardCard(t, e, 0, card(t, "Name:Test Ground Blocker\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	if !e.hasMustBeBlockedKeyword(req) {
		t.Fatalf("printed keyword not read: derived keywords = %v", e.Derived(req).Keywords)
	}
	attackSeat0(t, e, req)
	d := askBlockersFresh(t, e)
	opt := findBlockOption(d, blocker, req)
	if opt == nil || !opt.AttackMust {
		t.Fatalf("printed must-be-blocked did not create a block requirement: %+v", d.Options)
	}
}

// printedMustBeBlockedSrc is a fixture creature whose own static reads the
// animated-land sentence directly (the same shape the corpus Pump/PumpAll KW$
// carriers use), so the requirement machinery can be tested without a
// HiddenKeywords$ Animate resolution.
const printedMustBeBlockedSrc = "Name:Fixture Must Block Animated\nManaCost:3\n" +
	"Types:Creature Elemental\nPT:4/4\n" +
	"K:CARDNAME must be blocked if able.\nOracle:x\n"
