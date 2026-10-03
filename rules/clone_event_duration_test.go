package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestZygonCloneEndsWhenTargetActuallyUntaps(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Zygon Infiltrator", "Grizzly Bears")
	zygon := searchMoveByName(t, e, "Zygon Infiltrator", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	z, b := e.G.Obj(zygon), e.G.Obj(bear)
	if z == nil || b == nil || z.Zone != state.ZBattlefield || b.Zone != state.ZBattlefield || z.Face().Name == b.Face().Name {
		t.Fatal("clone operands not distinct battlefield permanents")
	}
	sa := cards.ResolveSVar(z.Face().SVars, "DBCopy")
	if sa == nil || sa.API != "Clone" || sa.Params["Duration"] != "UntilTargetedUntaps" {
		t.Fatalf("Zygon clone body missing: %+v", sa)
	}
	e.emit(events.Event{Kind: events.Tap, Obj: bear})
	effects.Resolve(e, &effects.Ctx{Source: zygon, Controller: 0, Targets: []state.Target{{Obj: bear}}, SVars: z.Face().SVars}, sa)
	if e.G.Obj(zygon).Face().Name != "Grizzly Bears" || !e.G.Obj(bear).Tapped {
		t.Fatal("copy or tapped referent missing before expiry")
	}
	// An unrelated untap, or an untap of an already untapped object, does not end the copy.
	e.emit(events.Event{Kind: events.Untap, Obj: zygon})
	if e.G.Obj(zygon).Face().Name != "Grizzly Bears" {
		t.Fatal("unrelated untap expired the copy")
	}
	e.emit(events.Event{Kind: events.Untap, Obj: bear})
	if e.G.Obj(zygon).Face().Name != "Zygon Infiltrator" {
		t.Fatalf("target untapped but copy remained: %s", e.G.Obj(zygon).Face().Name)
	}
	replayCheck(t, e, cfg)
}

func TestZygonCloneEndsWhenTargetLeavesTapped(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Zygon Infiltrator", "Grizzly Bears")
	zygon := searchMoveByName(t, e, "Zygon Infiltrator", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	z, b := e.G.Obj(zygon), e.G.Obj(bear)
	if z == nil || b == nil || z.Zone != state.ZBattlefield || b.Zone != state.ZBattlefield || z.Face().Name == b.Face().Name {
		t.Fatal("copy operands not distinct battlefield permanents")
	}
	sa := cards.ResolveSVar(z.Face().SVars, "DBCopy")
	if sa == nil || sa.Params["Duration"] != "UntilTargetedUntaps" {
		t.Fatalf("Zygon clone body missing: %+v", sa)
	}
	e.emit(events.Event{Kind: events.Tap, Obj: bear})
	effects.Resolve(e, &effects.Ctx{Source: zygon, Controller: 0, Targets: []state.Target{{Obj: bear}}, SVars: z.Face().SVars}, sa)
	if e.G.Obj(zygon).Face().Name != "Grizzly Bears" || !e.G.Obj(bear).Tapped {
		t.Fatal("copy or tapped referent missing before expiry")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(zygon); o.CopyFace != nil || o.Face().Name != "Zygon Infiltrator" {
		t.Fatalf("copied target left while tapped but copy remains: %s", o.Face().Name)
	}
	replayCheck(t, e, cfg)
}
