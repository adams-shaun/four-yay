package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The planechase verbs (CR 901, task planar-verbs) pinned end to end on the
// REAL corpus roll carrier (Fractured Powerstone, api:RollPlanarDice) with a
// planar deck in play: a kept planeswalk face walks the roller to the next
// plane, and a kept chaos face makes T:Mode$ ChaosEnsues fire on the current
// plane. Helpers (tokenReplGameSeats, moveSeededCard, addMana, abilityOption,
// submitChoices, passUntilStackEmpty, replayCheck, planeDeckConfig) come from
// the shared harness in this package.

// probePlaneCard is a plane whose chaos ability draws a card, so a test can
// observe that the trigger FIRED (the draw event) rather than only that the
// marker was logged. It is a synthetic plane (never committed as a Forge
// script) because the probe body must be observable without depending on a
// real card's board effect: the trigger machinery -- Mode$ ChaosEnsues, the
// Command-zone plane, the Execute$ SVar -- is the same one every corpus
// carrier uses. The body is a real registered primitive (api:Draw), not a
// test-only API, so the trigger path is exercised end to end.
func probePlaneCard(t *testing.T) *cards.Card {
	t.Helper()
	c, err := cards.ParseBytes("probe_plane.txt", []byte(
		"Name:Probe Plane\nManaCost:no cost\nTypes:Plane Probe\n"+
			"T:Mode$ ChaosEnsues | TriggerZones$ Command | Execute$ RolledChaos | TriggerDescription$ Whenever chaos ensues, draw a card.\n"+
			"SVar:RolledChaos:DB$ Draw | NumCards$ 1\nOracle:Whenever chaos ensues, draw a card.\n"))
	if err != nil {
		t.Fatalf("parse probe plane: %v", err)
	}
	c.Link()
	return c
}

// planeDeckConfig builds a 2-seat Config whose seat 0 has the given planar
// deck and seat 1 has none, with the stone in seat 0's library so it can be
// moved to the battlefield through a logged move. The returned cfg travels to
// replayCheck.
func planeDeckConfig(t *testing.T, seed uint64, stone *cards.Card, planes ...*cards.Card) Config {
	t.Helper()
	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{
				append([]*cards.Card{stone}, mountainDeck(t, 39)...),
				mountainDeck(t, 40),
			},
			PlanarDecks: [][]*cards.Card{planes},
		}
	}
	return seatZeroStart(build(seed))
}

// rollPlanarDieOnce activates the stone's roll ability and returns the single
// kept result. It fails the test if the activation produced no roll.
func rollPlanarDieOnce(t *testing.T, e *Engine, stone state.ObjID) int32 {
	t.Helper()
	addMana(t, e, 0, "")
	opt := abilityOption(t, e, stone, 1)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	rolls := planarRolls(e)
	if len(rolls) != 1 || len(rolls[0].IDs) != 1 {
		t.Fatalf("want exactly one kept planar-die result, got %+v", rolls)
	}
	return int32(rolls[0].IDs[0])
}

// foundFace is one seed/engine pair whose roll landed on want, from a
// deterministic seed search. It is used so a test proves its own precondition
// (the face was actually rolled) instead of hoping a fixed seed works.
type foundFace struct {
	e    *Engine
	cfg  Config
	face int32
}

// searchForFace builds games from seedBase upward until the stone's roll
// lands on want, over at most maxSeeds seeds. It returns nil if none did.
func searchForFace(t *testing.T, seedBase uint64, maxSeeds int, stone *cards.Card, planes []*cards.Card, want int32) *foundFace {
	t.Helper()
	for i := 0; i < maxSeeds; i++ {
		c := planeDeckConfig(t, seedBase+uint64(i), stone, planes...)
		g := New(c)
		g.Advance()
		id := moveSeededCard(t, g, 0, stone, state.ZBattlefield)
		if got := rollPlanarDieOnce(t, g, id); got == want {
			return &foundFace{e: g, cfg: c, face: got}
		}
	}
	return nil
}

// TestChaosFaceFiresThePlanesChaosTrigger is the required pin: a chaos face on
// the planar die fires the current plane's Mode$ ChaosEnsues ability. The seed
// is searched deterministically (no ambient randomness) so the test proves its
// own precondition -- a chaos face was actually rolled -- and fails loudly if
// the engine never produces one.
func TestChaosFaceFiresThePlanesChaosTrigger(t *testing.T) {
	stone := tokenReplCorpusCard(t, "Fractured Powerstone")
	probe := probePlaneCard(t)

	found := searchForFace(t, 70000, 64, stone, []*cards.Card{probe}, planarDieChaos)
	if found == nil {
		t.Fatal("no seed in the search range rolled a chaos face: precondition not met")
	}
	e, cfg := found.e, found.cfg
	if found.face != planarDieChaos {
		t.Fatalf("rolled face %d, want the chaos face %d", found.face, planarDieChaos)
	}
	// PRECONDITION: the plane really is the face-up current plane in the
	// planar deck.
	plane := currentPlaneOfTest(t, e, 0)
	if plane == 0 {
		t.Fatal("no face-up current plane after genesis: precondition not met")
	}
	if !zoneContainsID(e.G.Zone(state.ZPlanarDeck, 0), plane) {
		t.Fatalf("probe plane %d is not in seat 0's planar deck", plane)
	}
	// The ChaosEnsues marker must be on the log (the roll dispatch emitted it)
	// and the plane's trigger must have RUN -- a Draw AFTER the roll record is
	// the evidence, not merely the marker landing (and not the turn's own draw
	// step draw, which precedes the roll).
	rollIdx := -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.PlanarRoll {
			rollIdx = i
		}
	}
	if rollIdx < 0 {
		t.Fatal("no PlanarRoll event on the log: precondition not met")
	}
	var marker, drew bool
	for _, ev := range e.L.Events[rollIdx+1:] {
		if ev.Kind == events.ChaosEnsues && ev.Obj == plane && ev.Player == 0 {
			marker = true
		}
		if ev.Kind == events.Draw && ev.Player == 0 {
			drew = true
		}
	}
	if !marker {
		t.Fatalf("no ChaosEnsues marker for plane %d after the roll", plane)
	}
	if !drew {
		t.Fatal("chaos face rolled but the plane's Mode$ ChaosEnsues trigger never drew")
	}
	replayCheck(t, e, cfg)
}

// TestChaosVerbResolvesTheCurrentPlane pins DB$ ChaosEnsues (the Will of the
// Planeswalkers cycle's verb) against a real planar deck: resolving it emits a
// ChaosEnsues marker for the controller's current plane and fires that plane's
// trigger, rather than the "no planar deck" Note the verb degrades to without
// one.
func TestChaosVerbResolvesTheCurrentPlane(t *testing.T) {
	stone := tokenReplCorpusCard(t, "Fractured Powerstone")
	probe := probePlaneCard(t)
	cfg := planeDeckConfig(t, 81001, stone, probe)
	e := New(cfg)
	e.Advance()

	plane := currentPlaneOfTest(t, e, 0)
	if plane == 0 {
		t.Fatal("no face-up current plane: precondition not met")
	}
	sa, err := cards.ParseBytes("verb.txt", []byte(
		"Name:Chaos Verb Probe\nTypes:Sorcery\nA:SP$ ChaosEnsues\nOracle:x\n"))
	if err != nil {
		t.Fatalf("parse verb: %v", err)
	}
	sa.Link()
	before := len(e.L.Events)
	effects.Resolve(e, &effects.Ctx{Controller: 0, Source: plane}, sa.Faces[0].Abilities[0])
	// Drain the trigger the marker queued: putTriggersOnStack places it, then
	// the deterministic drain resolves its Draw body.
	for i := 0; i < 20 && len(e.pendingTriggers) > 0; i++ {
		e.putTriggersOnStack()
	}
	for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
		passUntilStackEmpty(t, e, 1)
	}

	var marker, drew bool
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.ChaosEnsues && ev.Obj == plane && ev.Player == 0 {
			marker = true
		}
		if ev.Kind == events.Draw && ev.Player == 0 {
			drew = true
		}
		if ev.Kind == events.Note && strings.Contains(ev.Text, "no planar deck") {
			t.Fatalf("ChaosEnsues verb still degraded to the no-planar-deck Note: %q", ev.Text)
		}
	}
	if !marker {
		t.Fatalf("ChaosEnsues verb emitted no marker for plane %d", plane)
	}
	if !drew {
		t.Fatal("ChaosEnsues verb resolved but the plane's trigger never drew")
	}
}

// TestPlaneswalkFaceWalksToTheNextPlane pins CR 901.4's planeswalk consequence
// on a real planar-dice roll: a kept planeswalk face rotates the roller's
// planar deck, revealing the next plane.
func TestPlaneswalkFaceWalksToTheNextPlane(t *testing.T) {
	stone := tokenReplCorpusCard(t, "Fractured Powerstone")
	planeA := tokenReplCorpusCard(t, "Aretopolis")
	planeB := tokenReplCorpusCard(t, "Pools of Becoming")

	found := searchForFace(t, 72000, 64, stone, []*cards.Card{planeA, planeB}, planarDiePlaneswalk)
	if found == nil {
		t.Fatal("no seed in the search range rolled a planeswalk face: precondition not met")
	}
	e, cfg := found.e, found.cfg
	if found.face != planarDiePlaneswalk {
		t.Fatalf("rolled face %d, want the planeswalk face %d", found.face, planarDiePlaneswalk)
	}
	// The roll's PlanarWalk must be on the log.
	var walked bool
	for _, ev := range e.L.Events {
		if ev.Kind == events.PlanarWalk && ev.Player == 0 {
			walked = true
		}
	}
	if !walked {
		t.Fatal("planeswalk face rolled but no PlanarWalk event was emitted")
	}
	// PRECONDITION: the deck still has both planes; after the walk the top
	// plane is face up and is the one that was at the bottom.
	ids := e.G.Zone(state.ZPlanarDeck, 0)
	if len(ids) != 2 {
		t.Fatalf("planar deck has %d cards, want 2", len(ids))
	}
	top, bottom := e.G.Obj(ids[0]), e.G.Obj(ids[1])
	if top == nil || bottom == nil || top.FaceDown || top.Face() == nil || bottom.Face() == nil {
		t.Fatalf("after the walk top=%+v bottom=%+v, want a face-up top and a face-down bottom", top, bottom)
	}
	if top.Face().Name == bottom.Face().Name {
		t.Fatal("privacy/determinism fixture needs two distinct planes")
	}
	replayCheck(t, e, cfg)
}

// currentPlaneOfTest is the test-side read of one seat's face-up current
// plane (the top of its planar deck), mirroring rules/planar.go's
// currentPlane.
func currentPlaneOfTest(t *testing.T, e *Engine, p state.PlayerID) state.ObjID {
	t.Helper()
	ids := e.G.Zone(state.ZPlanarDeck, p)
	if len(ids) == 0 {
		return 0
	}
	o := e.G.Obj(ids[0])
	if o == nil || o.FaceDown || o.Zone != state.ZPlanarDeck {
		return 0
	}
	return o.ID
}

// zoneContainsID reports whether id is in the given zone list.
func zoneContainsID(ids []state.ObjID, id state.ObjID) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}
