package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestBaskingCapybaraDescendFour(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Basking Capybara", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Lightning Bolt")
	capy := searchMoveByName(t, e, "Basking Capybara", state.ZBattlefield)
	if got := e.Derived(capy); got.Power != 1 || got.Toughness != 3 {
		t.Fatalf("precondition: printed Capybara starts 1/3, got %d/%d", got.Power, got.Toughness)
	}
	moveOne := func(name string, to state.Zone) state.ObjID {
		t.Helper()
		for _, zone := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(zone, 0) {
				o := e.G.Obj(id)
				if o != nil && o.Face() != nil && o.Face().Name == name {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: zone, To: to})
					e.pending = nil
					e.priorityRound()
					return id
				}
			}
		}
		t.Fatalf("precondition: %s available in owner hand/library", name)
		return 0
	}
	// An opponent's permanent card must not substitute for p0's fourth.
	var opponentPermanent state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 1) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == "Mountain" {
			opponentPermanent = id
			break
		}
	}
	if opponentPermanent == 0 {
		t.Fatal("precondition: opponent has a permanent card in library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: opponentPermanent, From: state.ZLibrary, To: state.ZGraveyard})
	bolt := moveOne("Lightning Bolt", state.ZGraveyard)
	for i := 0; i < 3; i++ {
		moveOne("Grizzly Bears", state.ZGraveyard)
	}
	assertCount := func(want int) {
		t.Helper()
		perm, nonperm := 0, 0
		for _, id := range e.G.Zone(state.ZGraveyard, 0) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			if o.Face().IsPermanent() {
				perm++
			} else {
				nonperm++
			}
		}
		if perm != want || nonperm != 1 || e.G.Obj(bolt).Zone != state.ZGraveyard {
			t.Fatalf("precondition: graveyard permanent/nonpermanent=%d/%d want %d/1", perm, nonperm, want)
		}
		got := e.Derived(capy)
		power := int32(1)
		if want >= 4 {
			power += int32(want - 1)
		}
		if got.Power != power || got.Toughness != 3 {
			t.Fatalf("with %d graveyard permanents got %d/%d, want %d/3", want, got.Power, got.Toughness, power)
		}
	}
	assertCount(3)
	moveOne("Grizzly Bears", state.ZGraveyard)
	assertCount(4)
	for _, id := range e.G.Zone(state.ZGraveyard, 0) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZHand})
			e.pending = nil
			e.priorityRound()
			break
		}
	}
	assertCount(3)
	replayCheck(t, e, cfg)
}

func TestPresentZonePermanentCardCounts(t *testing.T) {
	for _, zone := range []state.Zone{state.ZGraveyard, state.ZExile, state.ZHand, state.ZLibrary} {
		if got := presentSpecForZone("Permanent,Creature.YouCtrl", zone); got != "PermanentCard,Creature.YouCtrl" {
			t.Fatalf("zone %s adaptation=%q", zone, got)
		}
	}
	if got := presentSpecForZone("Permanent,Creature", state.ZBattlefield); got != "Permanent,Creature" {
		t.Fatalf("battlefield changed: %q", got)
	}
	if got := presentSpecForZone("Permanent,Creature", state.ZStack); got != "Permanent,Creature" {
		t.Fatalf("stack changed: %q", got)
	}
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Grizzly Bears", "Grizzly Bears", "Lightning Bolt")
	move := func(name string) state.ObjID {
		t.Helper()
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(z, 0) {
				o := e.G.Obj(id)
				if o != nil && o.Face() != nil && o.Face().Name == name {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZGraveyard})
					return id
				}
			}
		}
		t.Fatalf("missing %s", name)
		return 0
	}
	move("Grizzly Bears")
	move("Grizzly Bears")
	move("Lightning Bolt")
	ctx := e.specCtx(0, 0)
	if got := e.presentZoneCountCtx(state.ZGraveyard, "Permanent", 0, 0, ctx); got != 2 {
		t.Fatalf("shared count=%d want 2 permanent cards", got)
	}
	if got := e.presentZoneCountCtx(state.ZGraveyard, "PermanentCard", 0, 0, ctx); got != 2 {
		t.Fatalf("control=%d want 2", got)
	}
	static := staticView{Source: e.G.Zone(state.ZGraveyard, 0)[0], Controller: 0, Params: map[string]string{"PresentZone": "Battlefield,Graveyard"}}
	wantMixed := e.countPresentCtx("Permanent", static.Source, 0, e.staticSpecCtx(static)) + 2
	if got := e.countStaticPresent(static, "Permanent"); got != wantMixed {
		t.Fatalf("mixed Battlefield/Graveyard count=%d want %d without double counting", got, wantMixed)
	}
	if e.matchesSpec("Permanent", e.G.Zone(state.ZGraveyard, 0)[0], ctx) {
		t.Fatal("raw Permanent matcher must remain battlefield-only")
	}
}

func TestUchbenbakDescendAbilityGate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	fixtures := []string{"Uchbenbak, the Great Mistake"}
	for i := 0; i < 8; i++ {
		fixtures = append(fixtures, "Grizzly Bears")
	}
	e, _ := searchEngine(t, reg, fixtures...)
	id := searchMoveByName(t, e, "Uchbenbak, the Great Mistake", state.ZBattlefield)
	var abilityIndex = -1
	for i, ab := range e.G.Obj(id).Face().Abilities {
		if ab.ParamStr(cards.PKIsPresent) != "" {
			abilityIndex = i
			break
		}
	}
	if abilityIndex < 0 {
		t.Fatal("precondition: Uchbenbak has a PresentZone activation gate")
	}
	ab := e.G.Obj(id).Face().Abilities[abilityIndex]
	if ab.ParamStr(cards.PKPresentZone) != "Graveyard" {
		t.Fatalf("precondition: expected graveyard gate, got %q", ab.ParamStr(cards.PKPresentZone))
	}
	moveBear := func() {
		t.Helper()
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, bid := range e.G.Zone(z, 0) {
				o := e.G.Obj(bid)
				if o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
					e.emit(events.Event{Kind: events.MoveZone, Obj: bid, From: z, To: state.ZGraveyard})
					return
				}
			}
		}
		t.Fatal("precondition: Grizzly Bears available")
	}
	for i := 0; i < 7; i++ {
		moveBear()
	}
	if e.abilityPresentHolds(0, id, ab) {
		t.Fatal("seven graveyard permanents must not satisfy Uchbenbak's eight-card gate")
	}
	moveBear()
	if n := e.presentZoneCount(state.ZGraveyard, "Permanent", id, 0); n != 8 {
		t.Fatalf("precondition: shared path counts %d, want eight", n)
	}
	if !e.abilityPresentHolds(0, id, ab) {
		t.Fatal("eight graveyard permanents must satisfy Uchbenbak's gate")
	}
}
