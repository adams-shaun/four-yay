package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// oviyaStaticAffected asserts the real corpus script still carries the
// Affected$ line this ticket closes (a corpus pin move is a fixture break,
// not something to paper over). AffectedDefined$ Self loads normalized into
// a `Card.Self+...` Affected list, which is what the face carries.
func oviyaStaticAffected(t *testing.T, reg *cards.Registry, name, want string) {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus has no %q", name)
	}
	f := c.Faces[0]
	found := false
	for _, s := range f.Statics {
		if s.Params["Affected"] == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("corpus moved: %q carries no static with Affected$ %q", name, want)
	}
}

// TestOviyaAttackingOpponentTrample drives the real Oviya, Automech Artisan
// script ("Each creature that's attacking one of your opponents has
// trample") through the static walk. The rider reads the DEFENDER's seat
// (state.Object.Attacking, CR 508.1) with You = Oviya's controller, so your
// OWN creature attacking an opponent gains trample -- the reading the
// `attacking+Opponent` plus-spelling (opponent-CONTROLLED attacker) would
// miss -- and a creature attacking Oviya's controller gains nothing. The
// static walk re-runs per emitted event, so a creature that is not attacking
// never has the grant.
func TestOviyaAttackingOpponentTrample(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	oviya := lookup(t, reg, "Oviya, Automech Artisan")
	bear := lookup(t, reg, "Grizzly Bears")
	oviyaStaticAffected(t, reg, "Oviya, Automech Artisan", "Creature.attacking Opponent")

	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{oviya, bear}, []*cards.Card{bear})
	oviyaID := moveCorpusCard(t, e, "Oviya, Automech Artisan", 0, state.ZBattlefield)
	bear0 := moveCorpusCard(t, e, "Grizzly Bears", 0, state.ZBattlefield)
	bear1 := moveCorpusCard(t, e, "Grizzly Bears", 1, state.ZBattlefield)
	for _, id := range []state.ObjID{oviyaID, bear0, bear1} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
			t.Fatalf("precondition failed: fixture creature %d is not a faced battlefield permanent (%+v)", id, o)
		}
	}
	// Precondition: nobody has the grant before anyone attacks.
	for _, id := range []state.ObjID{oviyaID, bear0, bear1} {
		if e.HasKeyword(id, "Trample") {
			t.Fatalf("precondition failed: object %d already has Trample before any attack", id)
		}
	}

	// Seat 0's OWN bear attacks seat 1, an opponent of Oviya's controller:
	// the rider must grant it Trample.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{bear0}})
	if !e.G.Obj(bear0).IsAttacking || e.G.Obj(bear0).Attacking != 1 {
		t.Fatal("precondition failed: the bear did not record the attack against seat 1")
	}
	if !e.HasKeyword(bear0, "Trample") {
		t.Error("your own creature attacking an opponent must gain Trample (Oviya reads the defender, not the controller)")
	}
	if e.HasKeyword(oviyaID, "Trample") {
		t.Error("a non-attacking creature (Oviya itself) must not gain Trample")
	}

	// Seat 1's bear attacks seat 0: attacking YOU is not attacking one of
	// your opponents, so no grant.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bear1}})
	if e.HasKeyword(bear1, "Trample") {
		t.Error("a creature attacking Oviya's controller must not gain Trample")
	}
	replayCheck(t, e, cfg)
}

// TestDealtDamageHexproofStatic drives the two real "hasn't dealt damage
// yet" scripts: Karakyk Guardian (Card.!dealtDamagetoAny, any damage) and
// Ruric Thar, Magecrusher (Card.!dealtCombatDamagetoAny, combat only). Both
// read the GAME-LONG source-side record the DamageProvenance fold writes, so
// the grant must hold on a fresh battlefield and DROP once the creature
// itself deals damage of the right class -- non-combat damage dropping only
// the game-long word, combat damage dropping both.
func TestDealtDamageHexproofStatic(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	karakyk := lookup(t, reg, "Karakyk Guardian")
	ruric := lookup(t, reg, "Ruric Thar, Magecrusher")
	oviyaStaticAffected(t, reg, "Karakyk Guardian", "Card.Self+!dealtDamagetoAny")
	oviyaStaticAffected(t, reg, "Ruric Thar, Magecrusher", "Card.Self+!dealtCombatDamagetoAny")

	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{karakyk, ruric}, nil)
	karakykID := moveCorpusCard(t, e, "Karakyk Guardian", 0, state.ZBattlefield)
	ruricID := moveCorpusCard(t, e, "Ruric Thar, Magecrusher", 0, state.ZBattlefield)
	for _, id := range []state.ObjID{karakykID, ruricID} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
			t.Fatalf("precondition failed: fixture creature %d is not a faced battlefield permanent (%+v)", id, o)
		}
		// Precondition: Hexproof is held at setup, when nothing has been
		// dealt -- both readings of "yet" agree there.
		if !e.HasKeyword(id, "Hexproof") {
			t.Fatalf("precondition failed: object %d has no Hexproof on a fresh battlefield", id)
		}
	}

	// Karakyk deals NON-COMBAT damage: the game-long word turns on, so its
	// hexproof drops; Ruric Thar's combat-only gate must survive it.
	e.emit(events.Event{Kind: events.DamageProvenance, Obj: karakykID,
		IDs: []state.ObjID{ruricID}, Amount: 1})
	dealt := e.G.Obj(karakykID)
	if len(dealt.DamageDealtThisTurn) == 0 {
		t.Fatal("precondition failed: the DamageProvenance fold appended no record")
	}
	if !dealt.DealtDamageToAnyGame || dealt.DealtCombatDamageToAnyGame {
		t.Fatalf("precondition failed: non-combat provenance set game=%v combat=%v, want true,false",
			dealt.DealtDamageToAnyGame, dealt.DealtCombatDamageToAnyGame)
	}
	if e.HasKeyword(karakykID, "Hexproof") {
		t.Error("Karakyk Guardian must lose its hexproof once it has dealt (non-combat) damage")
	}
	if !e.HasKeyword(ruricID, "Hexproof") {
		t.Error("Ruric Thar's combat-only gate must survive non-combat damage")
	}

	// Ruric Thar deals COMBAT damage: now its own gate drops too.
	e.emit(events.Event{Kind: events.DamageProvenance, Obj: ruricID,
		IDs: []state.ObjID{karakykID}, Amount: 2, Text: events.DamageProvenanceCombat})
	if dealtCombat := e.G.Obj(ruricID); !dealtCombat.DealtCombatDamageToAnyGame {
		t.Fatal("precondition failed: the combat-classified fold entry set no combat record")
	}
	if e.HasKeyword(ruricID, "Hexproof") {
		t.Error("Ruric Thar must lose its hexproof once it has dealt combat damage")
	}
	// The record is game-long: the TurnChange clear that resets the
	// per-turn DamageDealtThisTurn must not resurrect either grant.
	e.emit(events.Event{Kind: events.TurnChange, Player: 1})
	if e.HasKeyword(karakykID, "Hexproof") || e.HasKeyword(ruricID, "Hexproof") {
		t.Error("the game-long dealt record must survive a TurnChange (the 'yet' window is the whole game)")
	}
	replayCheck(t, e, cfg)
}
