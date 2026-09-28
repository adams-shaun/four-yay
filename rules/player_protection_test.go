package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task player-protection (CR 702.16c follow-up to approx-player-shroud): the
// PLAYER half of a protection grant was dead -- `Affected$ You | AddKeyword$
// Protection:<quality>` statics (Gor Muldrak, Absolute Virtue) emitted their
// ContinuousEffect like every other static, playerKeywords already returned
// the grant, but NO target walk consulted it: candidatesFor's player offer
// arm and legalTargets's player recheck both read only playerShroudBlocksTarget
// and playerHexproofBlocksTarget. A player with matching protection stayed an
// offered, legal target while a permanent with the same grant was withheld.
//
// Every test here asserts its own precondition: the granting card IS on the
// battlefield, the grant DID reach the player keyword surface (wantPlayerKeyword),
// and the compared values actually differ -- the same-shaped control engine
// without the carrier still offers AND resolves the player target. The
// ChosenType/ChosenName carriers (Serra's Emissary, Runed Halo) pin the
// deliberate fail-closed direction: a quality the shared evaluator cannot
// resolve never withholds, exactly as hexproofQuality's unresolvable arm.

// testSalamanderScript is the synthetic carrier the Gor Muldrak test needs:
// a creature whose TYPE is the protected quality the corpus card binds
// (`Protection:Salamander`). sourceHasQuality sends the bare type word through
// matchesSpec's cbType arm, so this is the exact quality the object grammar
// answers. It targets a player, so the player-target path is exercised. Flash
// (CR 702.8) makes the creature castable at instant speed during seat 0's
// turn, so seat 1's source can be placed without advancing the turn.
const testSalamanderScript = "Name:Test Salamander\nManaCost:U\nTypes:Creature Salamander\nK:Flash\n" +
	"A:SP$ DealDamage | ValidTgts$ Player | NumDmg$ 2\nOracle:x\n"

// wantPlayerKeywordPrefix asserts the precondition every protection assertion
// stands on: a grant whose keyword string starts with head reached seat p's
// player keyword surface (the read half of the feature). The corpus grants
// carry trailing reminder text for the parameterised spelling
// (`Protection:Player.Opponent:each of your opponents`), so a prefix match is
// the correct precondition -- without it the cast would be withheld for the
// wrong reason (or the test would pass with the whole feature unregistered).
func wantPlayerKeywordPrefix(t *testing.T, e *Engine, p state.PlayerID, head string) {
	t.Helper()
	for _, kw := range e.playerKeywords(p) {
		if strings.HasPrefix(kw, head) {
			return
		}
	}
	t.Fatalf("player %d's keyword surface lacks a grant starting %q: %v", p, head, e.playerKeywords(p))
}

// answerETBChoice submits the first option of the ETB replacement ask a
// carrier's entry poses (Serra's Emissary's ChooseType, Runed Halo's
// NameCard), so the carrier actually lands on the battlefield and its static
// goes live. The guard-rail assertion below does not depend on WHICH value is
// chosen -- the flat keyword string is `Protection:ChosenType`/`ChosenName`
// either way -- only on the grant being live so the fail-closed direction is
// distinguishable from a missing grant.
func answerETBChoice(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" {
		return
	}
	submitChoices(t, e, 0)
}

// firstPlayerTarget returns the player target option offered for seat, or
// (0,false) when the seat is withheld.
func playerTargetOption(d *decision.Decision, seat state.PlayerID) (state.PlayerID, bool) {
	if d == nil {
		return 0, false
	}
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == seat {
			return o.Player, true
		}
	}
	return 0, false
}

// TestGorMuldrakPlayerProtection is the headline: seat 0's Gor Muldrak
// ("You and permanents you control have protection from Salamanders") makes
// seat 0 an illegal target of a Salamander creature-spell. Engine A (control,
// no carrier): seat 1's Salamander targeting seat 0 is offered and bites.
// Engine B (carrier on board): the seat-0 PLAYER option is withheld while the
// caster's own seat stays offerable -- and the grant does not bleed to seat 1
// (the player half of `Affected$ You,Permanent.YouCtrl` is `You` only).
func TestGorMuldrakPlayerProtection(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// Engine A -- control: no carrier, the player target is offered and bites.
	eA, _ := linkBoard(t, reg, []string{"Grizzly Bears"}, []string{"Hill Giant"})
	if len(eA.playerKeywords(0)) != 0 {
		t.Fatalf("control engine granted seat 0 player keywords: %v", eA.playerKeywords(0))
	}
	before := eA.G.Players[0].Life
	salamanderA := addScriptToHand(t, eA, 1, testSalamanderScript)
	addMana(t, eA, 1, "U")
	eA.askPriority(1)
	if d := eA.Pending(); !castOffered(eA, salamanderA) {
		t.Fatalf("control engine: Salamander not offered at the unprotected player: %+v", d.Options)
	}
	castFirst(t, eA, "cast")
	targetPlayer(t, eA, 0)
	passUntilStackEmpty(t, eA, 20)
	if got := eA.G.Players[0].Life; got != before-2 {
		t.Fatalf("control engine: player target did not bite -- life %d, want %d", got, before-2)
	}

	// Engine B -- the real corpus card on board: the grant reaches seat 0's
	// player keyword surface, only seat 0, and the Salamander's player option
	// for seat 0 is withheld (the caster's own seat remains legal).
	eB, _ := linkBoard(t, reg, []string{"Gor Muldrak, Amphinologist", "Grizzly Bears"}, []string{"Grizzly Bears"})
	gm := findOnBoard(t, eB, 0, "Gor Muldrak, Amphinologist")
	if eB.G.Obj(gm) == nil || eB.G.Obj(gm).Zone != state.ZBattlefield {
		t.Fatal("Gor Muldrak is not on the battlefield -- precondition broken")
	}
	wantPlayerKeywordPrefix(t, eB, 0, "Protection:Salamander")
	if eB.playerProtectedFrom(1, gm) {
		t.Fatalf("protection bled to seat 1 -- the Affected$ You grant matched the wrong seat: %v", eB.playerKeywords(1))
	}
	salamanderB := addScriptToHand(t, eB, 1, testSalamanderScript)
	addMana(t, eB, 1, "U")
	eB.askPriority(1)
	if d := eB.Pending(); !castOffered(eB, salamanderB) {
		t.Fatalf("Salamander not offered although the caster's own seat is a legal target: %+v", d.Options)
	}
	castFirst(t, eB, "cast")
	d := eB.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Salamander target decision, got %+v", d)
	}
	if _, ok := playerTargetOption(d, 0); ok {
		t.Errorf("protected seat 0 offered as a Salamander target option: %+v", d.Options)
	}
	if _, ok := playerTargetOption(d, 1); !ok {
		t.Errorf("caster's own seat 1 not offered -- the player-protection gate over-bit: %+v", d.Options)
	}
	targetPlayer(t, eB, 1)
	passUntilStackEmpty(t, eB, 20)
	if eB.G.Players[0].Life != 20 {
		t.Fatalf("seat 0 lost life although protection withheld it: %d", eB.G.Players[0].Life)
	}
}

// TestAbsoluteVirtuePlayerProtection pins the PLAYER-RELATIVE quality
// (`Protection:Player.Opponent`, which protectionQuality cuts to
// `Player.Opponent`): seat 0's Absolute Virtue grants "protection from each of
// your opponents", so a source controlled by an OPPONENT of seat 0 is
// withheld, while seat 0's own source still targets seat 0. The quality is not
// an object quality at all -- it is judged through the shared player filter
// against the source's controller.
func TestAbsoluteVirtuePlayerProtection(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Absolute Virtue", "Grizzly Bears"}, []string{"Hill Giant"})
	av := findOnBoard(t, e, 0, "Absolute Virtue")
	if e.G.Obj(av) == nil || e.G.Obj(av).Zone != state.ZBattlefield {
		t.Fatal("Absolute Virtue is not on the battlefield -- precondition broken")
	}
	wantPlayerKeywordPrefix(t, e, 0, "Protection:Player.Opponent")
	if e.playerProtectedFrom(1, av) {
		t.Fatalf("protection bled to seat 1 -- the Affected$ You grant matched the wrong seat: %v", e.playerKeywords(1))
	}

	// Opponent-controlled source: seat 0's player option is withheld.
	opp := addScriptToHand(t, e, 1, sparkPlayerScript)
	addMana(t, e, 1, "R")
	e.askPriority(1)
	if d := e.Pending(); !castOffered(e, opp) {
		t.Fatalf("opponent's Player-targeting Spark not offered although seat 1 itself is legal: %+v", d.Options)
	}
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Spark target decision, got %+v", d)
	}
	if _, ok := playerTargetOption(d, 0); ok {
		t.Errorf("seat 0 offered to an OPPONENT-controlled source despite protection from opponents: %+v", d.Options)
	}
	if _, ok := playerTargetOption(d, 1); !ok {
		t.Errorf("caster's own seat 1 not offered -- the Player.Opponent gate over-bit: %+v", d.Options)
	}
	targetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 20)
	if e.G.Players[0].Life != 20 {
		t.Fatalf("seat 0 lost life to an opponent's spell despite protection: %d", e.G.Players[0].Life)
	}

	// Seat 0's OWN source still targets seat 0 -- protection from opponents
	// does not withhold a source an opponent does not control.
	if e.playerProtectedFrom(0, av) {
		t.Fatal("playerProtectedFrom withheld seat 0 from its own protected-from-opponents grant")
	}
	own := addScriptToHand(t, e, 0, sparkPlayerScript)
	addMana(t, e, 0, "R")
	e.askPriority(0)
	if d := e.Pending(); !castOffered(e, own) {
		t.Fatalf("controller's own Spark not offered despite opponent-only protection: %+v", d.Options)
	}
	castFirst(t, e, "cast")
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected own Spark target decision, got %+v", d)
	}
	if _, ok := playerTargetOption(d, 0); !ok {
		t.Errorf("seat 0 not offered to its own source despite opponent-only protection: %+v", d.Options)
	}
	targetPlayer(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	if e.G.Players[0].Life != 18 {
		t.Fatalf("controller's self-targeted Spark did not resolve -- life %d, want 18", e.G.Players[0].Life)
	}
}

// TestPlayerProtectionRecheck pins the RECHECK leg (CR 608.2b through
// legalTargets's player arm): a player target legal at announcement is
// dropped at resolution once matching protection is live, exactly as the
// object arm drops a permanent that gained protection. The direct legalTargets
// call is the same shape TestPlayerShroudRecheckDropsPlayerTarget uses for
// the player arm; the control engine keeps the same target. The source is a
// Salamander object so sourceHasQuality resolves the quality.
func TestPlayerProtectionRecheck(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sa := card(t, testSalamanderScript).Faces[0].Abilities[0]
	targets := []state.Target{{Player: 0, IsPlayer: true}}

	// Control: without the carrier the recheck keeps the player target.
	eCtl, _ := linkBoard(t, reg, []string{"Grizzly Bears"}, []string{"Hill Giant"})
	if len(eCtl.playerKeywords(0)) != 0 {
		t.Fatalf("control engine granted seat 0 player keywords: %v", eCtl.playerKeywords(0))
	}
	srcCtl := addScriptToHand(t, eCtl, 1, testSalamanderScript)
	if got := eCtl.legalTargets(targets, sa, targetZones(sa), 1, srcCtl, 0); len(got) != 1 {
		t.Fatalf("control recheck dropped an unprotected player target: %v", got)
	}

	// With Gor Muldrak on board the same recheck drops it.
	e, _ := linkBoard(t, reg, []string{"Gor Muldrak, Amphinologist"}, []string{"Hill Giant"})
	wantPlayerKeywordPrefix(t, e, 0, "Protection:Salamander")
	src := addScriptToHand(t, e, 1, testSalamanderScript)
	if got := e.legalTargets(targets, sa, targetZones(sa), 1, src, 0); len(got) != 0 {
		t.Fatalf("recheck kept a player target whose seat gained matching protection: %v", got)
	}
}

// TestPlayerProtectionChosenQualityFailsClosed is the guard rail: the
// Chosen-bound qualities (`ChosenType`, Serra's Emissary; `ChosenName`, Runed
// Halo) resolve against the GRANTING static's own source object, which the
// flat playerKeywords string list does not carry. The helper must fail
// CLOSED -- never withholding -- so the target stays offered. The test only
// asserts that direction; it does not depend on the ETB ChooseType/ChooseName
// ask machinery resolving.
func TestPlayerProtectionChosenQualityFailsClosed(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// The read half is not broken: the grant DOES reach the player surface
	// (so an unregistered-quality failure is distinguishable from a missing
	// grant, and this test cannot pass with the whole feature unregistered).
	for _, tc := range []struct {
		carrier string
		kw      string
	}{
		{"Serra's Emissary", "Protection:ChosenType"},
		{"Runed Halo", "Protection:ChosenName"},
	} {
		e, _ := linkBoard(t, reg, []string{tc.carrier}, []string{"Hill Giant"})
		answerETBChoice(t, e)
		carrier := findOnBoard(t, e, 0, tc.carrier)
		if e.G.Obj(carrier) == nil || e.G.Obj(carrier).Zone != state.ZBattlefield {
			t.Fatalf("%s is not on the battlefield -- precondition broken", tc.carrier)
		}
		wantPlayerKeywordPrefix(t, e, 0, tc.kw)

		// The unresolvable quality never withholds: the seat stays a legal
		// target of an opponent's Player-targeting spell.
		sp := addScriptToHand(t, e, 1, sparkPlayerScript)
		addMana(t, e, 1, "R")
		e.askPriority(1)
		if d := e.Pending(); !castOffered(e, sp) {
			t.Fatalf("%s: unresolved %s wrongly withheld the cast: %+v", tc.carrier, tc.kw, d.Options)
		}
		castFirst(t, e, "cast")
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("%s: expected Spark target decision, got %+v", tc.carrier, d)
		}
		if _, ok := playerTargetOption(d, 0); !ok {
			t.Errorf("%s: unresolved %s withheld the player target (must fail closed): %+v", tc.carrier, tc.kw, d.Options)
		}
		// The feature's handler ran rather than being unregistered: the grant
		// is on the surface and no unimplemented-API Note was left.
		for _, ev := range e.L.Events {
			if strings.Contains(ev.Text, "unimplemented") {
				t.Fatalf("%s left an unimplemented note: %+v", tc.carrier, ev)
			}
		}
	}
}
