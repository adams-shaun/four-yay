package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestSuperAdaptoidDoubleStrikeCondition executes the card's compiled SVar,
// not a reconstructed condition. Its target lacks Double Strike, so the
// conditional counter leg must not put a Double Strike counter on
// Super-Adaptoid.
func TestSuperAdaptoidDoubleStrikeCondition(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Super-Adaptoid")
	if !ok {
		t.Fatal("corpus has no Super-Adaptoid")
	}
	var sourceFace *cards.Face
	for _, face := range card.Faces {
		if _, ok := face.SVars["DBPutCounterDoubleStrike"]; ok {
			sourceFace = face
			break
		}
	}
	if sourceFace == nil {
		t.Fatal("Super-Adaptoid has no DBPutCounterDoubleStrike SVar")
	}
	body := cards.ResolveSVar(sourceFace.SVars, "DBPutCounterDoubleStrike")
	if body == nil || body.API != "PutCounter" || body.Params["CounterType"] != "Double Strike" {
		t.Fatalf("precondition: resolved SVar = %#v, want its real Double Strike PutCounter body", body)
	}

	h := newHost(t, 2)
	addBattlefield := func(c *cards.Card) state.ObjID {
		o := h.g.AddObject(c, 0)
		o.Zone = state.ZBattlefield
		return o.ID
	}
	sourceID := addBattlefield(card)
	targetCard, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatal("corpus has no Grizzly Bears")
	}
	targetID := addBattlefield(targetCard)
	source, target := h.g.Obj(sourceID), h.g.Obj(targetID)
	if source.Zone != state.ZBattlefield || target.Zone != state.ZBattlefield {
		t.Fatalf("precondition: source/target zones = %s/%s, want Battlefield/Battlefield", source.Zone, target.Zone)
	}
	if objectHasKeyword(source, "Double Strike") {
		t.Fatal("precondition: Super-Adaptoid must lack Double Strike")
	}
	targetIsCreature := false
	if target.Face() != nil {
		for _, typ := range target.Face().Types {
			if typ == "Creature" {
				targetIsCreature = true
				break
			}
		}
	}
	if !targetIsCreature {
		t.Fatal("precondition: target must be a creature")
	}
	if objectHasKeyword(target, "Double Strike") {
		t.Fatal("precondition: target creature must lack Double Strike")
	}
	if got := source.Counter("Double Strike"); got != 0 {
		t.Fatalf("precondition: Super-Adaptoid already has %d Double Strike counters", got)
	}
	if got := target.Counter("Double Strike"); got != 0 {
		t.Fatalf("precondition: target already has %d Double Strike counters", got)
	}

	ctx := &Ctx{Source: sourceID, Controller: 0, SVars: sourceFace.SVars,
		Targets: []state.Target{{Obj: targetID}}}
	if len(ctx.Targets) != 1 || ctx.Targets[0].Obj != targetID {
		t.Fatalf("precondition: resolving target binding = %+v, want Grizzly Bears (%d)", ctx.Targets, targetID)
	}
	Resolve(h, ctx, body)
	if got := source.Counter("Double Strike"); got != 0 {
		t.Fatalf("Super-Adaptoid received %d Double Strike counter(s) from its gated SVar, want none", got)
	}
	if got := target.Counter("Double Strike"); got != 0 {
		t.Fatalf("target received %d Double Strike counter(s), want none", got)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
			t.Fatalf("real PutCounter handler did not run: %q", ev.Text)
		}
		if ev.Kind == events.CounterChange && ev.Counter == "Double Strike" {
			t.Fatalf("unexpected Double Strike CounterChange: %+v", ev)
		}
	}
}
