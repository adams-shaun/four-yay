package rules

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task approx-player-shroud: the AGENTS.md "Known approximations" row —
// PLAYER shroud (`Affected$ You` + `AddKeyword$ Shroud`, True Believer,
// Ivory Mask) was unread: players had no ObjID and no keyword machinery, so
// a player with granted shroud stayed targetable by everything. The fix
// gives players a derived keyword surface (rules/playerkeywords.go,
// playerKeywords over the shared layer walk) and wires CR 702.18 shroud and
// CR 702.11 hexproof into both targeting gates — candidatesFor's player arm
// (the offer census) and legalTargets's player arm (the CR 608.2b resolution
// recheck) — exactly where the same keywords bind a permanent.
//
// Every test here is driven by the real corpus cards the row names (True
// Believer, Ivory Mask) or by the same-shaped player-hexproof carriers
// (Leyline of Sanctity); each asserts its own precondition — the granting
// card IS on the battlefield and the grant DID reach the player keyword
// surface, and the compared values actually differ (the control engine
// without the grant still offers and resolves the player target).

const sparkOpponentScript = "Name:Spark\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ DealDamage | ValidTgts$ Opponent | NumDmg$ 2\nOracle:x\n"

// sparkMixScript targets a creature OR an opponent, so a cast stays offered
// while a shrouded seat is withheld at the OPTION level (the Hill Giant is
// still a legal target).
const sparkMixScript = "Name:Spark Mix\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ DealDamage | ValidTgts$ Creature,Opponent | NumDmg$ 2\nOracle:x\n"

// sparkPlayerScript targets ANY player, so the controller's own seat is a
// legal target of their own spell — the asymmetry leaf for player hexproof.
const sparkPlayerScript = "Name:Spark Player\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ DealDamage | ValidTgts$ Player | NumDmg$ 2\nOracle:x\n"

func addScriptToHand(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), p)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, p, append(e.G.Zone(state.ZHand, p), o.ID))
	return o.ID
}

// wantPlayerKeyword asserts the precondition every shroud/hexproof assertion
// below stands on: the granting card's static was parsed, emitted by the
// layer walk and matched the seat as a PLAYER spec — without it the cast
// would be withheld for the wrong reason (or the test would pass with the
// whole feature unregistered).
func wantPlayerKeyword(t *testing.T, e *Engine, p state.PlayerID, head string) {
	t.Helper()
	for _, kw := range e.playerKeywords(p) {
		if strings.EqualFold(strings.TrimSpace(kw), head) {
			return
		}
	}
	t.Fatalf("player %d's keyword surface lacks %q: %v", p, head, e.playerKeywords(p))
}

// TestTrueBelieverPlayerShroudWithholdsTargetOffer pins the row's headline
// (CR 702.18): seat 0's True Believer ("You have shroud") makes seat 0 an
// illegal target of EVERY spell or ability. Engine A (control, no grant):
// seat 1's Spark is offered, the target decision offers the seat-0 PLAYER,
// and resolving it takes 2 life — the player-target path demonstrably worked
// before the grant. Engine B (True Believer on board): the opponent's
// opponent-only Spark is not even offered (the CR 601.2c cast-withhold arm —
// its only possible target is the shrouded seat), and a creature-or-opponent
// Spark that stays castable loses the player OPTION while keeping the
// creature one. Shroud is symmetric, so the grant binds seat 0's own
// opponent-directed targeting no differently — but it must not BLEED to
// seat 1, so that is asserted too.
func TestTrueBelieverPlayerShroudWithholdsTargetOffer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// Engine A — control: no grant, the player target is offered and bites.
	eA, _ := linkBoard(t, reg, []string{"Grizzly Bears"}, []string{"Hill Giant"})
	if len(eA.playerKeywords(0)) != 0 {
		t.Fatalf("control engine granted seat 0 player keywords: %v", eA.playerKeywords(0))
	}
	before := eA.G.Players[0].Life
	spA := addScriptToHand(t, eA, 1, sparkOpponentScript)
	addMana(t, eA, 1, "R")
	eA.askPriority(1)
	if d := eA.Pending(); !castOffered(eA, spA) {
		t.Fatalf("control engine: Spark not offered at the un-shrouded player: %+v", d.Options)
	}
	castFirst(t, eA, "cast")
	targetPlayer(t, eA, 0)
	passUntilStackEmpty(t, eA, 20)
	if got := eA.G.Players[0].Life; got != before-2 {
		t.Fatalf("control engine: player target did not bite — life %d, want %d", got, before-2)
	}

	// Engine B — the real corpus card on board: the grant reaches the player
	// keyword surface, only seat 0, and the opponent's targeting stops.
	eB, _ := linkBoard(t, reg, []string{"True Believer", "Grizzly Bears"}, []string{"Grizzly Bears"})
	tb := findOnBoard(t, eB, 0, "True Believer")
	if eB.G.Obj(tb) == nil || eB.G.Obj(tb).Zone != state.ZBattlefield {
		t.Fatal("True Believer is not on the battlefield — precondition broken")
	}
	wantPlayerKeyword(t, eB, 0, "Shroud")
	if eB.playerShroudBlocksTarget(1) {
		t.Fatalf("shroud bled to seat 1 — the Affected$ You grant matched the wrong seat: %v", eB.playerKeywords(1))
	}
	spB := addScriptToHand(t, eB, 1, sparkOpponentScript)
	addMana(t, eB, 1, "R")
	eB.askPriority(1)
	if d := eB.Pending(); castOffered(eB, spB) {
		t.Fatalf("shrouded seat 0 still offered as the opponent's Spark target (cast should be withheld): %+v", d.Options)
	}

	// Option-level bite: with a legal creature target too, the cast is
	// offered but the shrouded seat is not in the target decision. The
	// control creature is a 2/2 Grizzly Bears — Spark's 2 damage kills it,
	// so the resolution leg proves the cast actually went through.
	spMix := addScriptToHand(t, eB, 1, sparkMixScript)
	addMana(t, eB, 1, "R")
	eB.askPriority(1)
	if d := eB.Pending(); !castOffered(eB, spMix) {
		t.Fatalf("creature-or-opponent Spark not offered although Hill Giant is a legal target: %+v", d.Options)
	}
	castFirst(t, eB, "cast")
	d := eB.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Spark target decision, got %+v", d)
	}
	giant := findOnBoard(t, eB, 1, "Grizzly Bears")
	sawGiant := false
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 0 {
			t.Errorf("shrouded seat 0 offered as a target option to the opponent")
		}
		if o.Obj == giant {
			sawGiant = true
		}
	}
	if !sawGiant {
		t.Errorf("Hill Giant not offered — the player-shroud gate over-bit: %+v", d.Options)
	}
	targetObject(t, eB, giant)
	passUntilStackEmpty(t, eB, 20)
	if eB.G.Players[0].Life != 20 {
		t.Fatalf("seat 0 lost life although shroud withheld it: %d", eB.G.Players[0].Life)
	}
	if eB.G.Obj(giant).Zone == state.ZBattlefield {
		t.Fatal("control creature survived a resolved Spark — the cast never resolved")
	}
}

// TestIvoryMaskPlayerShroudWithholdsTargetOffer pins the row's second
// carrier: Ivory Mask ("You have shroud") grants its CONTROLLER's seat the
// same CR 702.18 protection. The opponent's opponent-only Spark is not
// offered — its only possible target is the shrouded seat — while the
// precondition leg proves the static actually reached the player keyword
// surface (an unregistered feature must fail here, not pass silently).
func TestIvoryMaskPlayerShroudWithholdsTargetOffer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Ivory Mask"}, []string{"Hill Giant"})
	mask := findOnBoard(t, e, 0, "Ivory Mask")
	if e.G.Obj(mask) == nil || e.G.Obj(mask).Zone != state.ZBattlefield {
		t.Fatal("Ivory Mask is not on the battlefield — precondition broken")
	}
	wantPlayerKeyword(t, e, 0, "Shroud")
	sp := addScriptToHand(t, e, 1, sparkOpponentScript)
	addMana(t, e, 1, "R")
	e.askPriority(1)
	if d := e.Pending(); castOffered(e, sp) {
		t.Fatalf("shrouded seat 0 still offered as the opponent's Spark target (Ivory Mask unread): %+v", d.Options)
	}
	// The enchantment's own resolution left no unimplemented-API Note: the
	// static is a continuous grant, read live, not a resolution-time API.
	for _, ev := range e.L.Events {
		if strings.Contains(ev.Text, "unimplemented") {
			t.Fatalf("Ivory Mask's grant left an unimplemented note on the log: %+v", ev)
		}
	}
}

// TestLeylineOfSanctityPlayerHexproof pins the player half of CR 702.11 on a
// real corpus carrier: Leyline of Sanctity's "You have hexproof" withholds
// its controller's seat from OPPONENTS' targeting (the opponent-only Spark is
// not offered) but not from their OWN — hexproof is asymmetric (CR 702.11b),
// so seat 0's Player-targeting Spark still offers seat 0 itself and resolves
// against them.
func TestLeylineOfSanctityPlayerHexproof(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Leyline of Sanctity"}, []string{"Hill Giant"})
	ley := findOnBoard(t, e, 0, "Leyline of Sanctity")
	if e.G.Obj(ley) == nil || e.G.Obj(ley).Zone != state.ZBattlefield {
		t.Fatal("Leyline of Sanctity is not on the battlefield — precondition broken")
	}
	wantPlayerKeyword(t, e, 0, "Hexproof")
	if !e.playerHexproofBlocksTarget(0, 1, 0) {
		t.Fatal("player hexproof did not withhold seat 0 from seat 1's targeting")
	}
	if e.playerHexproofBlocksTarget(0, 0, 0) {
		t.Fatal("player hexproof withheld its own controller (CR 702.11b asymmetry broken)")
	}

	// The opponent cannot cast at the hexproof seat (sole target).
	sp := addScriptToHand(t, e, 1, sparkOpponentScript)
	addMana(t, e, 1, "R")
	e.askPriority(1)
	if d := e.Pending(); castOffered(e, sp) {
		t.Fatalf("hexproof seat 0 still offered as the opponent's Spark target: %+v", d.Options)
	}

	// The controller's own Player-targeting Spark still offers seat 0 and
	// bites — the compared values (withheld vs offered) actually differ.
	sp2 := addScriptToHand(t, e, 0, sparkPlayerScript)
	addMana(t, e, 0, "R")
	e.askPriority(0)
	if d := e.Pending(); !castOffered(e, sp2) {
		t.Fatalf("controller's own Spark not offered at the hexproof player (asymmetry over-bit): %+v", d.Options)
	}
	castFirst(t, e, "cast")
	targetPlayer(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != 18 {
		t.Fatalf("controller's self-targeted Spark did not resolve — life %d, want 18", got)
	}
}

// TestPlayerShroudRecheckDropsPlayerTarget pins the RECHECK leg
// (CR 608.2b through legalTargets's player arm): a player target that is
// legal at announcement is dropped at resolution once the shroud grant is
// live, exactly as the object arm drops a permanent that gained shroud. The
// control engine (no grant) keeps the same target. The direct legalTargets
// call is the same shape hexproof_recheck_test.go uses for the object arm.
func TestPlayerShroudRecheckDropsPlayerTarget(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sa := card(t, sparkPlayerScript).Faces[0].Abilities[0]
	targets := []state.Target{{Player: 0, IsPlayer: true}}

	// Control: without the grant the recheck keeps the player target.
	eCtl, _ := linkBoard(t, reg, []string{"Grizzly Bears"}, []string{"Hill Giant"})
	if len(eCtl.playerKeywords(0)) != 0 {
		t.Fatalf("control engine granted seat 0 player keywords: %v", eCtl.playerKeywords(0))
	}
	if got := eCtl.legalTargets(targets, sa, targetZones(sa), 1, 0, 0); len(got) != 1 {
		t.Fatalf("control recheck dropped an un-shrouded player target: %v", got)
	}

	// With True Believer on board the same recheck drops it.
	e, _ := linkBoard(t, reg, []string{"True Believer"}, []string{"Hill Giant"})
	wantPlayerKeyword(t, e, 0, "Shroud")
	if got := e.legalTargets(targets, sa, targetZones(sa), 1, 0, 0); len(got) != 0 {
		t.Fatalf("recheck kept a player target whose seat gained shroud: %v", got)
	}
}

// TestPlayerKeywordGatesGuardRails pins the two negative contracts the new
// helpers must keep: a seat with no live player-keyword grant blocks nothing
// (the empty default — every pre-existing cast offer is unchanged), and the
// affected census (targeting=false) still ignores the player gates the way
// it ignores a permanent's.
func TestPlayerKeywordGatesGuardRails(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Grizzly Bears"}, []string{"Hill Giant"})
	if e.playerShroudBlocksTarget(0) || e.playerShroudBlocksTarget(1) {
		t.Fatal("player shroud reported on a seat with no grant")
	}
	if e.playerHexproofBlocksTarget(0, 1, 0) || e.playerHexproofBlocksTarget(1, 0, 0) {
		t.Fatal("player hexproof reported on a seat with no grant")
	}
	// The affected census (Overload's affectedCandidates, targeting=false)
	// must still offer the shrouded seat: shroud only stops targeting.
	e2, _ := linkBoard(t, reg, []string{"True Believer"}, []string{"Hill Giant"})
	wantPlayerKeyword(t, e2, 0, "Shroud")
	sp := card(t, sparkPlayerScript).Faces[0].Abilities[0]
	if !slices.ContainsFunc(e2.affectedCandidates(1, 0, 0, sp), func(c targetCandidate) bool {
		return c.kind == "player" && c.player == 0
	}) {
		t.Fatalf("affected census (targeting=false) withheld the shrouded seat: %v", e2.affectedCandidates(1, 0, 0, sp))
	}
}
