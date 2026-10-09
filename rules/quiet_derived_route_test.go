package rules

// The derived-keyword route fixtures (findings-t2 MAJOR): the proof's
// graveyard, exile and hand-route blockers must read the object's DERIVED
// keyword list, not only the printed face. A layer-6 AddKeyword$ grant
// (Snapcaster Mage's Flashback, Underworld Breach's Escape, Dream Devourer's
// Foretell) reaches an object whose printed face lacks the head, and the walk
// offers the recast -- so a printed-only proof calls the window quiet while
// the walk offers, and the verify arm panics (the TestKr8WorldsInFuzzGames
// Brainstorm-flashback class). Each fixture asserts the printed face does NOT
// carry the head, the derived list DOES, the proof blocks with the route
// blocker, and -- where the walk can offer -- the offer is real.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// quietFlashbackGrantSrc is a battlefield static that grants Flashback to a
// card in its owner's graveyard: the exact layer-6 shape Snapcaster Mage
// leaves, with no printed Flashback on the affected card.
const quietFlashbackGrantSrc = "Name:Quiet Flashback Grant\nTypes:Artifact\n" +
	"S:Mode$ Continuous | Affected$ Card.YouOwn+nonLand | AffectedZone$ Graveyard | AddKeyword$ Flashback:U | Description$ x\n" +
	"Oracle:x\n"

// quietWarpGrantSrc is the exile twin: a battlefield static that grants Warp
// to a card its owner has in exile.
const quietWarpGrantSrc = "Name:Quiet Warp Grant\nTypes:Artifact\n" +
	"S:Mode$ Continuous | Affected$ Card.YouOwn | AffectedZone$ Exile | AddKeyword$ Warp:1 R | Description$ x\n" +
	"Oracle:x\n"

// TestQuietDerivedGraveRouteBlocksGrantedFlashback is the Brainstorm-flashback
// class: a graveyard instant/sorcery with no printed Flashback, granted one by
// a layer-6 static. The walk offers the flashback cast; the proof must block
// on the derived route (qbGraveRoute), not call the window quiet.
func TestQuietDerivedGraveRouteBlocksGrantedFlashback(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	spell := lookup(t, reg, "Divination")
	if spell.Faces[0].HasKeyword("Flashback") {
		t.Fatal("precondition: Divination already prints Flashback")
	}
	grant := card(t, quietFlashbackGrantSrc)
	e := quietBaseLandWith(t, reg, "Island", []*cards.Card{grant, spell})
	addZone(t, e, 0, grant, state.ZBattlefield)
	id := addZone(t, e, 0, spell, state.ZGraveyard)
	// The fixture is only meaningful when the printed facts miss the route but
	// the derived list carries it -- otherwise the old printed-only blocker
	// would pass the row for the wrong reason.
	if ff := e.walkFaceFactsOf(e.G.Obj(id).Face()); ff == nil || ff.quiet.recastKW {
		t.Fatal("precondition: the printed face already carries a recast keyword")
	}
	if !e.hasKeywordH(id, kwhFlashback) {
		t.Fatal("precondition: the layer-6 Flashback grant did not reach the graveyard card")
	}
	if got := e.quietBlocker(0); got != qbGraveRoute {
		t.Fatalf("quietBlocker = %s, want %s (the derived Flashback route must block)",
			quietBlockerNames[got], quietBlockerNames[qbGraveRoute])
	}
	// The walk prices the floating pool (not untapped lands), so fund the
	// granted flashback instance's cost ({U}, the derived keyword's parameter)
	// to make the offer real.
	e.G.Players[0].Pool[state.MU] = 1
	// Verify arm live (the rules test binary sets derivedMemoVerify): this
	// call runs priorityOptions, which panics if the proof called the window
	// quiet while the walk offered. Then the offer is asserted real, so a
	// proof that became quiet again for the wrong reason fails loudly.
	e.priorityRound()
	if !hasMode(e.legalActions(0), "flashback") {
		t.Fatalf("the walk did not offer the granted flashback cast: %v", optKinds(e.legalActions(0)))
	}
}

// TestQuietDerivedHandRouteBlocksGrantedForetell is Dream Devourer's shape: a
// plain creature in hand with no printed Foretell, granted one by a layer-6
// effect. The walk offers the {2} foretell action; the proof must block on the
// derived route (qbHandLand).
func TestQuietDerivedHandRouteBlocksGrantedForetell(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	bear := lookup(t, reg, "Grizzly Bears")
	dd := lookup(t, reg, "Dream Devourer")
	if bear.Faces[0].HasKeyword("Foretell") {
		t.Fatal("precondition: Grizzly Bears prints Foretell")
	}
	// Dream Devourer is seated from the deck (not AddObject'd after New) so its
	// face has compiled facts: otherwise the mana ceiling goes unbounded and
	// the hand-spell blocker masks the keyword-action hole this row tests.
	e := quietBaseWith(t, reg, []*cards.Card{dd, bear})
	id := addHand(t, e, 0, bear)
	addZone(t, e, 0, dd, state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: the bear is not in hand: %+v", o)
	}
	if _, ok := e.G.Obj(id).Face().KeywordParam("Foretell"); ok {
		t.Fatal("precondition: the hand card already prints Foretell")
	}
	if !e.hasKeywordH(id, kwhForetell) {
		t.Fatal("precondition: Dream Devourer's grant did not reach the hand card")
	}
	if got := e.quietBlocker(0); got != qbHandLand {
		t.Fatalf("quietBlocker = %s, want %s (the granted foretell action must block)",
			quietBlockerNames[got], quietBlockerNames[qbHandLand])
	}
	// Fund the {2} action so the walk's offer is real (the proof blocks on the
	// route regardless of affordability, which is the point).
	e.G.Players[0].Pool[state.MC] = 2
	e.priorityRound()
	if !hasMode(e.legalActions(0), "foretell") {
		t.Fatalf("the walk did not offer the granted foretell action: %v", optKinds(e.legalActions(0)))
	}
}

// hasMode reports whether opts contains a cast option with the given mode.
func hasMode(opts []decision.Option, mode string) bool {
	for i := range opts {
		if opts[i].Kind == "cast" && opts[i].Mode == mode {
			return true
		}
	}
	return false
}

// TestQuietDerivedExileRouteBlocksGrantedRoute is the exile twin of the
// graveyard fixture: an exiled card with no printed exile-recast keyword,
// granted Warp by a layer-6 static. The proof must block on the derived exile
// route (qbExileRoute) rather than read only the printed face.
func TestQuietDerivedExileRouteBlocksGrantedRoute(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	bears := lookup(t, reg, "Grizzly Bears")
	if bears.Faces[0].HasKeyword("Warp") {
		t.Fatal("precondition: Grizzly Bears prints Warp")
	}
	grant := card(t, quietWarpGrantSrc)
	e := quietBaseWith(t, reg, []*cards.Card{grant, bears})
	addZone(t, e, 0, grant, state.ZBattlefield)
	id := addZone(t, e, 0, bears, state.ZExile)
	if ff := e.walkFaceFactsOf(e.G.Obj(id).Face()); ff == nil || ff.quiet.exileCastKW {
		t.Fatal("precondition: the printed face already carries an exile-recast keyword")
	}
	if !e.hasKeywordH(id, kwhWarp) {
		t.Fatal("precondition: the layer-6 Warp grant did not reach the exiled card")
	}
	if got := e.quietBlocker(0); got != qbExileRoute {
		t.Fatalf("quietBlocker = %s, want %s (the derived exile route must block)",
			quietBlockerNames[got], quietBlockerNames[qbExileRoute])
	}
}
