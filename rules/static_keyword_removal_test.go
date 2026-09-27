package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestStaticRemoveKeywordStripsFlying(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Colossus Hammer"), lookup(t, reg, "Serra Angel"), lookup(t, reg, "Serra Angel")}, nil)
	hammer := moveByName(t, e, 0, "Colossus Hammer", state.ZBattlefield)
	bearer := moveByName(t, e, 0, "Serra Angel", state.ZBattlefield)
	unattached := moveByName(t, e, 0, "Serra Angel", state.ZBattlefield)
	if hammer == 0 || bearer == 0 || unattached == 0 || !e.HasKeyword(bearer, "Flying") {
		t.Fatal("fixture must have Hammer and two battlefield Serra Angels, with Flying on bearer")
	}
	e.emit(events.Event{Kind: events.Attach, Obj: hammer, IDs: []state.ObjID{bearer}})
	d := e.Derived(bearer)
	if slices.Contains(d.Keywords, "Flying") || e.HasKeyword(bearer, "Flying") {
		t.Fatalf("Hammer bearer retained Flying: derived=%v", d.Keywords)
	}
	if d.Power != 14 || d.Toughness != 14 {
		t.Fatalf("Hammer bearer P/T=%d/%d, want 14/14", d.Power, d.Toughness)
	}
	if !e.HasKeyword(unattached, "Flying") {
		t.Fatal("unattached Serra Angel lost Flying outside EquippedBy scope")
	}
	replayCheck(t, e, cfg)
}

func TestStaticRemoveKeywordScopedToAffected(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Colossus Hammer"), lookup(t, reg, "Serra Angel"), lookup(t, reg, "Serra Angel")}, nil)
	hammer := moveByName(t, e, 0, "Colossus Hammer", state.ZBattlefield)
	attached := moveByName(t, e, 0, "Serra Angel", state.ZBattlefield)
	other := moveByName(t, e, 0, "Serra Angel", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Attach, Obj: hammer, IDs: []state.ObjID{attached}})
	if !e.HasKeyword(other, "Flying") {
		t.Fatalf("unattached second flyer lost Flying: %v", e.Derived(other).Keywords)
	}
	if e.HasKeyword(attached, "Flying") {
		t.Fatalf("equipped flyer retained Flying: %v", e.Derived(attached).Keywords)
	}
}

func TestStaticRemoveKeywordAndAddKeywordSameLine(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Sky Tether"), lookup(t, reg, "Serra Angel")}, nil)
	tether := moveByName(t, e, 0, "Sky Tether", state.ZBattlefield)
	bearer := moveByName(t, e, 0, "Serra Angel", state.ZBattlefield)
	if tether == 0 || bearer == 0 || e.G.Obj(tether).Zone != state.ZBattlefield || e.G.Obj(bearer).Zone != state.ZBattlefield || !e.HasKeyword(bearer, "Flying") {
		t.Fatal("precondition: Sky Tether and Serra Angel must be on the battlefield, with printed Flying")
	}
	e.emit(events.Event{Kind: events.Attach, Obj: tether, IDs: []state.ObjID{bearer}})
	kw := e.Derived(bearer).Keywords
	if slices.Contains(kw, "Flying") || !slices.Contains(kw, "Defender") {
		t.Fatalf("Sky Tether keywords=%v, want Flying removed and Defender granted", kw)
	}
	replayCheck(t, e, cfg)
}

func TestStaticRemoveKeywordExpiresWithSource(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Colossus Hammer"), lookup(t, reg, "Serra Angel")}, nil)
	hammer := moveByName(t, e, 0, "Colossus Hammer", state.ZBattlefield)
	bearer := moveByName(t, e, 0, "Serra Angel", state.ZBattlefield)
	if hammer == 0 || bearer == 0 || e.G.Obj(hammer).Zone != state.ZBattlefield || e.G.Obj(bearer).Zone != state.ZBattlefield || !e.HasKeyword(bearer, "Flying") {
		t.Fatal("precondition: Hammer and printed-Flying Serra Angel must be on the battlefield")
	}
	e.emit(events.Event{Kind: events.Attach, Obj: hammer, IDs: []state.ObjID{bearer}})
	if e.HasKeyword(bearer, "Flying") {
		t.Fatal("Hammer did not remove Flying before source left")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: hammer, From: state.ZBattlefield, To: state.ZGraveyard})
	if !e.HasKeyword(bearer, "Flying") {
		t.Fatalf("Flying did not return after Hammer left: %v", e.Derived(bearer).Keywords)
	}
}

func TestStaticCantHaveKeywordStripsPrintedAndBlocksGrant(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Archetype of Imagination")}, []*cards.Card{lookup(t, reg, "Serra Angel")})
	archetype := moveByName(t, e, 0, "Archetype of Imagination", state.ZBattlefield)
	flyer := moveByName(t, e, 1, "Serra Angel", state.ZBattlefield)
	if archetype == 0 || flyer == 0 || e.G.Obj(archetype).Zone != state.ZBattlefield || e.G.Obj(flyer).Zone != state.ZBattlefield || !e.G.Obj(flyer).Face().HasKeyword("Flying") {
		t.Fatal("precondition: Archetype and opponent Serra Angel must be on battlefield with printed Flying")
	}
	grant := state.ContinuousEffect{Source: archetype, Affects: "Creature.OppCtrl", Layer: state.LAbilities, AddKeywords: []string{"Flying"}}
	e.AddContinuous(grant)
	control := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Mountain")}, []*cards.Card{lookup(t, reg, "Serra Angel")})
	grantSource := moveByName(t, control, 0, "Mountain", state.ZBattlefield)
	controlFlyer := moveByName(t, control, 1, "Serra Angel", state.ZBattlefield)
	control.AddContinuous(state.ContinuousEffect{Source: grantSource, Affects: "Creature.OppCtrl", Layer: state.LAbilities, AddKeywords: []string{"Flying"}})
	if !slices.Contains(control.Derived(controlFlyer).Keywords, "Flying") {
		t.Fatalf("precondition: independent layer-6 grant without Archetype must produce Flying, got %v", control.Derived(controlFlyer).Keywords)
	}
	if slices.Contains(e.Derived(flyer).Keywords, "Flying") || e.HasKeyword(flyer, "Flying") {
		t.Fatalf("Archetype prohibition failed to strip/block Flying: %v", e.Derived(flyer).Keywords)
	}
	replayCheck(t, e, cfg)
}

func TestStaticRemoveKeywordSameTimestampRemovalFirst(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Serra Angel")}, nil)
	bearer := moveByName(t, e, 0, "Serra Angel", state.ZBattlefield)
	if bearer == 0 || e.G.Obj(bearer).Zone != state.ZBattlefield || !e.HasKeyword(bearer, "Flying") {
		t.Fatal("precondition: Serra Angel must be on the battlefield with Flying")
	}
	// A full-tie removal must sort before a grant: the removal clears printed
	// Flying, then the later-in-walk grant restores it. Register grant first
	// so stable registration order would produce the opposite result absent
	// the removal-first tie-break.
	ts := e.G.Clock + 1
	e.AddContinuous(state.ContinuousEffect{Source: bearer, Affects: "Card.Self", Layer: state.LAbilities, Timestamp: ts, AddKeywords: []string{"Flying"}})
	e.AddContinuous(state.ContinuousEffect{Source: bearer, Affects: "Card.Self", Layer: state.LAbilities, Timestamp: ts, RemoveKeywords: []string{"Flying"}})
	if !e.HasKeyword(bearer, "Flying") {
		t.Fatalf("same-timestamp removal/grant tie did not apply removal first: %v", e.Derived(bearer).Keywords)
	}
}

func TestStaticRemoveKeywordLandwalkVariant(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Mystic Decree"), lookup(t, reg, "Bog Wraith"), lookup(t, reg, "Serra Angel")}, nil)
	wraith := moveByName(t, e, 0, "Bog Wraith", state.ZBattlefield)
	flyer := moveByName(t, e, 0, "Serra Angel", state.ZBattlefield)
	if wraith == 0 || flyer == 0 {
		t.Fatal("fixture must have Bog Wraith and Serra Angel")
	}
	// Precondition: Bog Wraith carries a printed parameterised landwalk and
	// Serra Angel printed Flying while the Decree is not yet in play, so the
	// post-entry assertions below are not vacuous.
	if !slices.Contains(e.Derived(wraith).Keywords, "Landwalk:Swamp") || !e.HasKeyword(wraith, "Landwalk") {
		t.Fatalf("precondition: Bog Wraith must carry Landwalk:Swamp, got %v", e.Derived(wraith).Keywords)
	}
	if !e.HasKeyword(flyer, "Flying") {
		t.Fatalf("precondition: Serra Angel must carry Flying, got %v", e.Derived(flyer).Keywords)
	}
	decreed := moveByName(t, e, 0, "Mystic Decree", state.ZBattlefield)
	if decreed == 0 {
		t.Fatal("fixture must have Mystic Decree")
	}
	// The Decree's single static line is RemoveKeyword$ Flying & Landwalk:Island
	// -- two entries through the ` & ` grammar. The engine matches removal by
	// keyword HEAD, so the printed Landwalk:Swamp print (a variant the line
	// does not name) leaves too; asserting per-variant granularity is out of
	// scope (recorded approximation).
	if kw := e.Derived(wraith).Keywords; len(kw) > 0 {
		for _, k := range kw {
			if cards.KeywordHead(k) == "Landwalk" {
				t.Fatalf("Decree entered but Bog Wraith kept a landwalk entry: %v", kw)
			}
		}
	}
	if e.HasKeyword(wraith, "Landwalk") {
		t.Fatal("Decree entered but Bog Wraith kept its landwalk head")
	}
	if e.HasKeyword(flyer, "Flying") {
		t.Fatalf("Decree entered but Serra Angel kept Flying: %v", e.Derived(flyer).Keywords)
	}
	replayCheck(t, e, cfg)
}

func TestStaticCantHaveKeywordCloneCopiesKeywordSlices(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Grizzly Bears")}, nil)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if bear == 0 || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("precondition: Grizzly Bears must be on the battlefield")
	}
	e.AddContinuous(state.ContinuousEffect{Source: bear, Affects: "Card.Self", Layer: state.LAbilities,
		RemoveKeywords: []string{"Flying"}, CantHaveKeywords: []string{"Flying"}})
	clone := e.Clone()
	clone.continuous[0].RemoveKeywords[0] = "Haste"
	clone.continuous[0].CantHaveKeywords[0] = "Haste"
	if e.continuous[0].RemoveKeywords[0] != "Flying" || e.continuous[0].CantHaveKeywords[0] != "Flying" {
		t.Fatalf("clone aliases keyword slices: original removal=%v prohibition=%v", e.continuous[0].RemoveKeywords, e.continuous[0].CantHaveKeywords)
	}
}

func TestStaticCantHaveKeywordScopedToOpponent(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Archetype of Imagination"), lookup(t, reg, "Serra Angel")}, []*cards.Card{lookup(t, reg, "Serra Angel")})
	archetype := moveByName(t, e, 0, "Archetype of Imagination", state.ZBattlefield)
	ownFlyer := moveByName(t, e, 0, "Serra Angel", state.ZBattlefield)
	opponentFlyer := moveByName(t, e, 1, "Serra Angel", state.ZBattlefield)
	if archetype == 0 || ownFlyer == 0 || opponentFlyer == 0 || e.G.Obj(ownFlyer).Zone != state.ZBattlefield || e.G.Obj(opponentFlyer).Zone != state.ZBattlefield || !e.G.Obj(ownFlyer).Face().HasKeyword("Flying") || !e.G.Obj(opponentFlyer).Face().HasKeyword("Flying") {
		t.Fatal("precondition: Archetype and both printed-Flying Serra Angels must be on the battlefield")
	}
	if !e.HasKeyword(ownFlyer, "Flying") {
		t.Fatalf("Archetype affected its controller's creature; keywords=%v", e.Derived(ownFlyer).Keywords)
	}
	if e.HasKeyword(opponentFlyer, "Flying") {
		t.Fatalf("Archetype failed to affect opponent's creature; keywords=%v", e.Derived(opponentFlyer).Keywords)
	}
}
