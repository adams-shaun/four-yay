package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestBurningMasksDamageSourcesUseControlAtHit(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Case of the Burning Masks")
	if !ok {
		t.Fatal("corpus missing Case of the Burning Masks")
	}
	bear, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatal("corpus missing Grizzly Bears")
	}
	body := card.Faces[0].SVars["X"]
	if body != "Count$NumDamageThisTurn Card.YouCtrl,Emblem.YouCtrl Player,Permanent" {
		t.Fatalf("Case corpus SVar X = %q", body)
	}
	e, ids := pcdrEngine(t, "Case of the Burning Masks", "Grizzly Bears")
	caseID, own := ids["Case of the Burning Masks"], ids["Grizzly Bears"]
	other := onBoardCard(t, e, 1, bear)
	ctx := &effects.Ctx{Controller: 0, Source: caseID}
	if e.G.Obj(caseID).Zone != state.ZBattlefield || e.G.Obj(caseID).Controller != 0 ||
		e.G.Obj(own).Controller != 0 || e.G.Obj(other).Controller != 1 {
		t.Fatal("precondition: Case stays with seat 0; damaging bears start under opposite controllers")
	}
	damageCountEvent(e, own, 0, 1, 2, true)
	damageCountEvent(e, other, 0, 0, 3, true)
	if got := effects.EvalCount(e, ctx, body); got != 1 {
		t.Fatalf("before theft = %d, want one qualifying source", got)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: own, Player: 1})
	if e.G.Obj(own).Controller != 1 || e.G.Obj(caseID).Controller != 0 {
		t.Fatal("precondition: own Bear changed control but Case did not")
	}
	if got := effects.EvalCount(e, ctx, body); got != 1 {
		t.Fatalf("after losing own damaging Bear = %d, want its earlier hit counted", got)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: other, Player: 0})
	if e.G.Obj(other).Controller != 0 || e.G.Obj(caseID).Controller != 0 {
		t.Fatal("precondition: opposing Bear changed control but Case did not")
	}
	if got := effects.EvalCount(e, ctx, body); got != 1 {
		t.Fatalf("after gaining opposing damaging Bear = %d, want only the original own source", got)
	}
	// A new hit by the acquired Bear qualifies, but does not retroactively
	// qualify its earlier hit or the departed Bear's subsequent hit.
	damageCountEvent(e, other, 0, 1, 4, true)
	damageCountEvent(e, own, 0, 1, 5, true)
	if got := effects.EvalCount(e, ctx, body); got != 2 {
		t.Fatalf("after new hits = %d, want two sources with seat-0 hits", got)
	}
}

func TestBurningMasksDoesNotClaimAcquiredSourceHistory(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Case of the Burning Masks")
	if !ok {
		t.Fatal("corpus missing Case of the Burning Masks")
	}
	body := card.Faces[0].SVars["X"]
	if body != "Count$NumDamageThisTurn Card.YouCtrl,Emblem.YouCtrl Player,Permanent" {
		t.Fatalf("Case corpus SVar X = %q", body)
	}
	e, ids := pcdrEngine(t, "Case of the Burning Masks")
	caseID := ids["Case of the Burning Masks"]
	other := onBoardCard(t, e, 1, card)
	ctx := &effects.Ctx{Controller: 0, Source: caseID}
	if e.G.Obj(caseID).Controller != 0 || e.G.Obj(other).Controller != 1 ||
		e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatal("precondition: Case stays under seat 0 and opposing source is in play")
	}
	damageCountEvent(e, other, 0, 0, 3, true)
	if hits := e.G.Obj(other).DamageDealtThisTurn; len(hits) != 1 || hits[0].Amount != 3 || hits[0].SourceControl != 1 {
		t.Fatalf("precondition: opposing source's hit must be recorded under seat 1: %+v", hits)
	}
	if got := effects.EvalCount(e, ctx, body); got != 0 {
		t.Fatalf("opponent source before theft = %d, want zero", got)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: other, Player: 0})
	if e.G.Obj(other).Controller != 0 || e.G.Obj(caseID).Controller != 0 {
		t.Fatal("precondition: acquired source changed control but Case did not")
	}
	if got := effects.EvalCount(e, ctx, body); got != 0 {
		t.Fatalf("opponent source after theft = %d, want zero for its pre-theft hit", got)
	}
}

func TestOjerNonCombatDamageUsesControlAtHit(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Ojer Axonil, Deepest Might")
	if !ok {
		t.Fatal("corpus missing Ojer Axonil, Deepest Might")
	}
	body := card.Faces[1].SVars["X"]
	if body != "Count$NonCombatDamageThisTurn Card.Red+YouCtrl Any" {
		t.Fatalf("Ojer corpus back-face SVar X = %q", body)
	}
	e, ids := pcdrEngine(t, "Ojer Axonil, Deepest Might", "Grizzly Bears")
	own, recipient := ids["Ojer Axonil, Deepest Might"], ids["Grizzly Bears"]
	other := onBoardCard(t, e, 1, card)
	ctx := &effects.Ctx{Controller: 0, Source: own}
	if e.G.Obj(own).Zone != state.ZBattlefield || e.G.Obj(own).Controller != 0 ||
		e.G.Obj(other).Zone != state.ZBattlefield || e.G.Obj(other).Controller != 1 ||
		e.G.Obj(recipient).Zone != state.ZBattlefield {
		t.Fatal("precondition: red Ojer sources on opposite seats and a battlefield recipient")
	}
	damageCountEvent(e, own, recipient, 0, 2, false)
	damageCountEvent(e, other, recipient, 0, 3, false)
	if got := effects.EvalCount(e, ctx, body); got != 2 {
		t.Fatalf("before theft = %d, want own source's 2 damage", got)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: own, Player: 1})
	if e.G.Obj(own).Controller != 1 {
		t.Fatal("precondition: own red source changed control")
	}
	if got := effects.EvalCount(e, ctx, body); got != 2 {
		t.Fatalf("after losing red source = %d, want earlier 2 damage", got)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: other, Player: 0})
	if e.G.Obj(other).Controller != 0 {
		t.Fatal("precondition: opposing red source changed control")
	}
	if got := effects.EvalCount(e, ctx, body); got != 2 {
		t.Fatalf("after acquiring red source = %d, want earlier opposing 3 excluded", got)
	}
	damageCountEvent(e, other, recipient, 0, 4, false)
	damageCountEvent(e, own, recipient, 0, 5, false)
	if got := effects.EvalCount(e, ctx, body); got != 6 {
		t.Fatalf("after new hits = %d, want only 2+4 under seat-0 control", got)
	}
}

// TestOjerNonCombatDamageUsesColourAtHit: Temple of Power counts damage dealt
// by RED sources you controlled this turn, so the colour is the source's at
// the hit (CR 608.2h reads it then). Ojer deals 4 while red, then flips to its
// colourless Temple face: the earlier 4 still count.
func TestOjerNonCombatDamageUsesColourAtHit(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Ojer Axonil, Deepest Might")
	if !ok {
		t.Fatal("corpus missing Ojer Axonil, Deepest Might")
	}
	body := card.Faces[1].SVars["X"]
	if body != "Count$NonCombatDamageThisTurn Card.Red+YouCtrl Any" {
		t.Fatalf("Ojer corpus back-face SVar X = %q", body)
	}
	e, ids := pcdrEngine(t, "Ojer Axonil, Deepest Might", "Grizzly Bears")
	own, recipient := ids["Ojer Axonil, Deepest Might"], ids["Grizzly Bears"]
	ctx := &effects.Ctx{Controller: 0, Source: own}
	if e.ObjectColors(e.G.Obj(own)) != "R" {
		t.Fatalf("precondition: Ojer front face is red, got %q", e.ObjectColors(e.G.Obj(own)))
	}
	damageCountEvent(e, own, recipient, 0, 4, false)
	e.emit(events.Event{Kind: events.FlipFace, Obj: own, Amount: 1})
	if c := e.ObjectColors(e.G.Obj(own)); c != "" {
		t.Fatalf("precondition: Temple face is colourless, got %q", c)
	}
	if got := effects.EvalCount(e, ctx, body); got != 4 {
		t.Fatalf("after flipping to Temple = %d, want the 4 dealt while red", got)
	}
}
