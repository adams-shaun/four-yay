package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 303.4g: "If an Aura is entering the battlefield and there is no legal
// object or player for it to enchant, the Aura remains in its current zone,
// unless that zone is the stack." CR 303.4f: an Aura put onto the battlefield
// without being cast has its controller CHOOSE what it enchants as it enters
// -- that is not targeting, so hexproof/shroud do not stop it.
//
// The fuzz-1003 defect: Restoration Seminar (a Paradigm Lesson) returning an
// Aura with no creature on the battlefield put the Aura onto the battlefield
// (firing its ETB trigger) and the CR 704.5m SBA then swept it to the
// graveyard.

const auraHexproofFoeSrc = "Name:Hexproof Foe\nManaCost:1 G\nTypes:Creature Elf\nPT:2/2\nK:Hexproof\nOracle:x\n"

func auraSeminarCast(t *testing.T, e *Engine, auraName string) (aura, seminar state.ObjID) {
	t.Helper()
	toMain1(t, e)
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); aura == 0 && o != nil && o.Face() != nil && o.Face().Name == auraName {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZGraveyard})
				e.pending = nil
				aura = id
			}
		}
	}
	if aura == 0 {
		t.Fatalf("%s not in seat 0's library or hand", auraName)
	}
	e.priorityRound()
	seminar = findAndMoveToHand(t, e, 0, "Restoration Seminar")
	addMana(t, e, 0, "WWWWWWW")
	submitChoices(t, e, castOptionFor(t, e, seminar).Index)
	answerTargetAsk(t, e, []state.ObjID{aura})
	return aura, seminar
}

// TestRestorationSeminarAuraWithNothingToEnchantStaysInGraveyard is the
// fuzz-1003 carrier with the real cards: nothing on the battlefield is a
// creature, so Kenrith's Transformation (Enchant creature, "When it enters,
// draw a card") must remain in the graveyard -- no battlefield entry, no ETB
// draw, no SBA trip.
func TestRestorationSeminarAuraWithNothingToEnchantStaysInGraveyard(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1003, []string{"Restoration Seminar", "Kenrith's Transformation"}, nil, nil)
	aura, _ := auraSeminarCast(t, e, "Kenrith's Transformation")
	mark := len(e.L.Events)
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(aura); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("aura zone = %v, want graveyard (CR 303.4g: it remains in its current zone)", zoneOf(o))
	}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.Obj == aura {
			t.Errorf("aura moved (%v -> %v, %q); CR 303.4g says it never leaves its zone", ev.From, ev.To, ev.Text)
		}
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != libBefore {
		t.Errorf("library %d -> %d: the Aura's ETB draw fired though it never entered", libBefore, got)
	}
	replayCheck(t, e, cfg)
}

// TestRestorationSeminarAuraEnchantsHexproofOpponentCreature pins CR 303.4f's
// non-targeting half: the only creature is an opponent's hexproof creature,
// which the Aura can still be put onto because choosing what it enchants as
// it enters is not targeting.
func TestRestorationSeminarAuraEnchantsHexproofOpponentCreature(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1004, []string{"Restoration Seminar", "Pacifism"}, nil, []string{auraHexproofFoeSrc})
	foe := moveSeeded(t, e, 1, auraHexproofFoeSrc, state.ZBattlefield)
	aura, _ := auraSeminarCast(t, e, "Pacifism")
	passUntilStackEmpty(t, e, 20)

	o := e.G.Obj(aura)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("aura zone = %v, want battlefield (hexproof does not stop a non-target attach)", zoneOf(o))
	}
	if o.AttachedTo != foe {
		t.Fatalf("aura attached to %d, want the hexproof creature %d", o.AttachedTo, foe)
	}
	replayCheck(t, e, cfg)
}

// TestRestorationSeminarAuraChoiceIsAskedOfController pins CR 303.4f's
// chooser: with two legal creatures the Aura's controller is asked which one
// it enchants, and the ask is a choice, never a KTarget ask.
func TestRestorationSeminarAuraChoiceIsAskedOfController(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1005, []string{"Restoration Seminar", "Pacifism"}, nil,
		[]string{auraHexproofFoeSrc, altDragonSrc})
	foe := moveSeeded(t, e, 1, auraHexproofFoeSrc, state.ZBattlefield)
	dragon := moveSeeded(t, e, 1, altDragonSrc, state.ZBattlefield)
	aura, _ := auraSeminarCast(t, e, "Pacifism")

	asked := false
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				break
			}
			submitChoices(t, e, passIndex(t, d))
			continue
		}
		if d.Kind == decision.KTarget {
			t.Fatalf("the Aura's entry posed a KTarget ask (CR 303.4f: not targeting): %+v", d)
		}
		if d.Player != 0 {
			t.Fatalf("entry choice asked of seat %d, want the Aura's controller 0: %+v", d.Player, d)
		}
		idx := -1
		for _, opt := range d.Options {
			if opt.Obj == foe {
				idx = opt.Index
			}
		}
		if idx < 0 {
			t.Fatalf("hexproof creature %d not offered as a bearer: %+v", foe, d.Options)
		}
		hasDragon := false
		for _, opt := range d.Options {
			hasDragon = hasDragon || opt.Obj == dragon
		}
		if !hasDragon {
			t.Fatalf("second creature %d not offered as a bearer: %+v", dragon, d.Options)
		}
		asked = true
		submitChoices(t, e, idx)
	}
	if !asked {
		t.Fatal("no bearer choice was asked of the Aura's controller with two legal creatures (CR 303.4f)")
	}
	if o := e.G.Obj(aura); o == nil || o.Zone != state.ZBattlefield || o.AttachedTo != foe {
		t.Fatalf("aura = %+v, want on the battlefield attached to %d", o, foe)
	}
	replayCheck(t, e, cfg)
}

// auraMoveCorpusCard moves the named corpus card from seat p's library or hand
// to zone `to` through a logged MoveZone (setup, not a game action).
func auraMoveCorpusCard(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				e.pending = nil
				return id
			}
		}
	}
	t.Fatalf("%s not in seat %d's library or hand", name, p)
	return 0
}

// TestRetetherRestrictsTheAurasLegalBearers pins the effect-named half of
// CR 303.4f/g with a real card. Retether ("Return each Aura card from your
// graveyard to the battlefield. Only creatures can be enchanted this way.
// (Aura cards that can't enchant a creature on the battlefield remain in your
// graveyard.)") is a ChangeZone with `AttachedTo$ Creature`: Pacifism enters
// attached to a creature with no choice asked, while Wild Growth (Enchant
// land) stays in the graveyard even though a land is on the battlefield --
// the effect's restriction leaves it nothing legal.
func TestRetetherRestrictsTheAurasLegalBearers(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1006, []string{"Retether", "Pacifism", "Wild Growth"}, []string{altDragonSrc}, nil)
	toMain1(t, e)
	dragon := moveSeeded(t, e, 0, altDragonSrc, state.ZBattlefield)
	land := auraMoveCorpusCard(t, e, 0, "Mountain", state.ZBattlefield)
	pacifism := auraMoveCorpusCard(t, e, 0, "Pacifism", state.ZGraveyard)
	growth := auraMoveCorpusCard(t, e, 0, "Wild Growth", state.ZGraveyard)
	e.priorityRound()
	retether := findAndMoveToHand(t, e, 0, "Retether")
	addMana(t, e, 0, "WWWW")
	submitChoices(t, e, castOptionFor(t, e, retether).Index)
	mark := len(e.L.Events)
	for i := 0; i < 40 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("Retether's returns posed an ask (the effect names the bearer): %+v", d)
		}
		submitChoices(t, e, passIndex(t, d))
	}
	if o := e.G.Obj(pacifism); o == nil || o.Zone != state.ZBattlefield || o.AttachedTo != dragon {
		t.Fatalf("Pacifism = %+v, want on the battlefield attached to the creature %d", o, dragon)
	}
	if o := e.G.Obj(growth); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Wild Growth zone = %v, want graveyard (it can't enchant a creature)", zoneOf(o))
	}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.Obj == growth {
			t.Errorf("Wild Growth moved (%v -> %v) though it never legally enters", ev.From, ev.To)
		}
	}
	if e.G.Obj(land).Zone != state.ZBattlefield {
		t.Fatal("precondition: the land left the battlefield")
	}
	replayCheck(t, e, cfg)
}

// TestReanimatedAnimateDeadEnchantsAGraveyardCreature pins the
// graveyard-enchant family through the same entry: Restoration Seminar
// returns Animate Dead ("Enchant creature card in a graveyard"), which enters
// attached to the only creature card in a graveyard, and its own ETB trigger
// then returns that creature with Animate Dead attached.
func TestReanimatedAnimateDeadEnchantsAGraveyardCreature(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1007, []string{"Restoration Seminar", "Animate Dead"}, []string{altDragonSrc}, nil)
	dragon := moveSeeded(t, e, 0, altDragonSrc, state.ZGraveyard)
	aura, _ := auraSeminarCast(t, e, "Animate Dead")
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(aura)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Animate Dead zone = %v, want battlefield", zoneOf(o))
	}
	if d := e.G.Obj(dragon); d == nil || d.Zone != state.ZBattlefield || o.AttachedTo != dragon {
		t.Fatalf("dragon zone = %v, Animate Dead attached to %d; want the dragon reanimated with Animate Dead on it", zoneOf(d), o.AttachedTo)
	}
	replayCheck(t, e, cfg)
}

// TestReanimatedAnimateDeadWithNoGraveyardCreatureStays: with no creature
// card in any graveyard, Animate Dead has nothing it can legally enchant and
// stays in the graveyard (CR 303.4g).
func TestReanimatedAnimateDeadWithNoGraveyardCreatureStays(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1008, []string{"Restoration Seminar", "Animate Dead"}, nil, nil)
	aura, _ := auraSeminarCast(t, e, "Animate Dead")
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(aura); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Animate Dead zone = %v, want graveyard", zoneOf(o))
	}
	replayCheck(t, e, cfg)
}

// TestRoleTokenIsNotCreatedWhenItsCreatureLeft pins CR 303.4g's token
// sentence with a real card: Cursed Courtier's "When it enters, create a
// Cursed Role token attached to it" resolving after the Courtier has left the
// battlefield creates no Role (an Aura token with nothing it can legally
// enchant isn't created), where it used to mint the Role unattached for the
// CR 704.5m SBA to sweep.
func TestRoleTokenIsNotCreatedWhenItsCreatureLeft(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1009, []string{"Cursed Courtier"}, nil, nil)
	toMain1(t, e)
	courtier := auraMoveCorpusCard(t, e, 0, "Cursed Courtier", state.ZBattlefield)
	if len(e.pendingTriggers) == 0 {
		t.Fatal("precondition: Cursed Courtier's ETB trigger did not queue")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: courtier, From: state.ZBattlefield, To: state.ZGraveyard})
	before := len(e.G.Objs)
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	for i := before; i < len(e.G.Objs); i++ {
		if o := &e.G.Objs[i]; o.IsToken && o.Face() != nil && o.Face().Name == "Cursed Role" {
			t.Fatalf("a Cursed Role was created (zone %v) though its creature had left", o.Zone)
		}
	}
	if !hasNote(e, "isn't created (CR 303.4g)") {
		t.Error("no CR 303.4g note for the withheld Role token")
	}
	replayCheck(t, e, cfg)
}

// TestAuraTokenWithNothingToEnchantIsNotCreated pins the TokenCreate gate:
// a Role token minted with no creature anywhere is not created.
func TestAuraTokenWithNothingToEnchantIsNotCreated(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1010, nil, nil, nil)
	toMain1(t, e)
	if _, ok := e.G.Tokens["role_cursed"]; !ok {
		t.Skip("role_cursed token script not in the corpus")
	}
	before := e.G.NextID
	e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "role_cursed"})
	if e.G.NextID != before {
		t.Fatalf("an Aura token was minted with nothing to enchant (NextID %d -> %d)", before, e.G.NextID)
	}
	if !hasNote(e, "isn't created (CR 303.4g)") {
		t.Error("no CR 303.4g note for the withheld token")
	}
	replayCheck(t, e, cfg)
}

// TestReplenishAurasChooseTheirBearers drives ChangeZoneAll -- Replenish's
// mass return ("Auras with nothing to enchant remain in your graveyard") --
// through the entry: with two creatures on the battlefield the returned
// Pacifisms are asked of their controller, every Pacifism lands attached to
// a legal creature, Wild Growth (no land on the battlefield) stays in the
// graveyard, and the game replays from its log.
//
// Under the resolution kernel (the default) each member's as-enters election
// is answered in place as it enters (W3 step 5, lasagna spec §7.2), so both
// Pacifisms are asked. (The legacy park could pose only the FIRST election
// of a sweep: a later member entering while that ask was outstanding took
// the deterministic first legal bearer.)
func TestReplenishAurasChooseTheirBearers(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1011, []string{"Replenish", "Pacifism", "Pacifism", "Wild Growth"},
		nil, []string{altDragonSrc, auraHexproofFoeSrc})
	toMain1(t, e)
	dragon := moveSeeded(t, e, 1, altDragonSrc, state.ZBattlefield)
	foe := moveSeeded(t, e, 1, auraHexproofFoeSrc, state.ZBattlefield)
	p1 := auraMoveCorpusCard(t, e, 0, "Pacifism", state.ZGraveyard)
	p2 := auraMoveCorpusCard(t, e, 0, "Pacifism", state.ZGraveyard)
	growth := auraMoveCorpusCard(t, e, 0, "Wild Growth", state.ZGraveyard)
	e.priorityRound()
	replenish := findAndMoveToHand(t, e, 0, "Replenish")
	addMana(t, e, 0, "WWWW")
	submitChoices(t, e, castOptionFor(t, e, replenish).Index)
	asks := 0
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				break
			}
			submitChoices(t, e, passIndex(t, d))
			continue
		}
		if d.Kind == decision.KTarget || d.Player != 0 {
			t.Fatalf("unexpected ask during Replenish: %+v", d)
		}
		// Answer the first Pacifism onto the dragon, the second onto the
		// hexproof creature.
		want := dragon
		if asks > 0 {
			want = foe
		}
		idx := -1
		for _, opt := range d.Options {
			if opt.Obj == want {
				idx = opt.Index
			}
		}
		if idx < 0 {
			t.Fatalf("bearer %d not offered: %+v", want, d.Options)
		}
		asks++
		submitChoices(t, e, idx)
	}
	if asks != 2 {
		t.Fatalf("%d bearer asks, want 2 (one per returned Pacifism; see the doc comment)", asks)
	}
	for _, id := range []state.ObjID{p1, p2} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("Pacifism %d zone = %v, want battlefield", id, zoneOf(o))
		}
		if o.AttachedTo != dragon && o.AttachedTo != foe {
			t.Fatalf("Pacifism %d attached to %d, want a creature (%d or %d)", id, o.AttachedTo, dragon, foe)
		}
	}
	bearers := map[state.ObjID]bool{}
	for _, id := range []state.ObjID{p1, p2} {
		bearers[e.G.Obj(id).AttachedTo] = true
	}
	if !bearers[dragon] || !bearers[foe] {
		t.Fatalf("the answered bearers (the dragon and the hexproof creature) do not each carry a Pacifism: %v", bearers)
	}
	if o := e.G.Obj(growth); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Wild Growth zone = %v, want graveyard", zoneOf(o))
	}
	replayCheck(t, e, cfg)
}

// stageAuraEntry is the test-staging entry for an Aura already on the
// battlefield attached to bearer: it moves id from its current zone onto the
// battlefield through a MoveZone marked with bearer as the effect-named
// bearer (events.MarkNamedAttachEntry), so the engine's CR 303.4f/g entry
// gate attaches it as it enters -- the logged Attach a replay reproduces --
// instead of posing the "what does it enchant" choice (or withholding the
// entry) a bare staging move now gets. bearer must already be a legal bearer:
// an object on the battlefield, or a seat via state.PlayerRef.
func stageAuraEntry(t *testing.T, e *Engine, id, bearer state.ObjID) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil {
		t.Fatalf("stageAuraEntry: no object %d", id)
	}
	ev := events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZBattlefield}
	events.MarkNamedAttachEntry(&ev, []state.ObjID{bearer})
	e.emit(ev)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("staged Aura %d did not enter attached to %d (zone %v)", id, bearer, o.Zone)
	}
	// A non-Aura attachment (an Equipment) is not settled by the entry: it
	// takes the plain logged Attach a staging fixture always used.
	if p, isPlayer := bearer.PlayerRef(); isPlayer {
		if !o.HasAttachedPlayer || o.AttachedPlayer != p {
			e.emit(events.Event{Kind: events.Attach, Obj: id, Player: p, Text: "attach to player"})
		}
	} else if o.AttachedTo != bearer {
		e.emit(events.Event{Kind: events.Attach, Obj: id, IDs: []state.ObjID{bearer}})
	}
}

// stageAuraCard stages seat p's seeded copy of c (library or hand) onto the
// battlefield attached to bearer through stageAuraEntry.
func stageAuraCard(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card, bearer state.ObjID) state.ObjID {
	t.Helper()
	toMain1(t, e)
	name := c.Faces[0].Name
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				stageAuraEntry(t, e, id, bearer)
				e.pending = nil
				return id
			}
		}
	}
	t.Fatalf("seeded card %q not found in seat %d's library or hand", name, p)
	return 0
}

// stageRawEntry folds a hand-built battlefield placement of id straight into
// the game (events.Emit, the raw fold a log replay uses): no CR 303.4f/g entry
// gate, no replacement, no trigger. It is for a fixture that builds a board
// state no game action could reach in one step -- an Aura sitting unattached,
// or attached by a later hand-written Attach to a bearer its printed enchant
// would not admit at entry (Animate Dead's post-reanimate state).
func stageRawEntry(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil {
		t.Fatalf("stageRawEntry: no object %d", id)
	}
	events.Emit(e.G, e.L, events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZBattlefield})
}
