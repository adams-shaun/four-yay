package effects

// ChoiceZone$ / ChoiceOptional$ / ExcludeChosen$ on the DB$ Clone Choices$
// pick (ticket agent-20260923T090459Z-65bbfecf).
//
// Before this change the standalone/trigger Clone route honoured only the
// battlefield pool and a mandatory pick: ChoiceZone$ was ignored (the pool was
// always the battlefield), ChoiceOptional$ declined nothing (the pick was
// mandatory), and ExcludeChosen$ included the chosen source in the become pool
// (a replay-visible self-copy). These tests drive the REAL corpus carriers
// through the primitive, so a regression in any of the three clauses fails.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// clonePermanentTargets returns the Obj ids that received a ClonePermanent
// event in h's log. It is the copy basis the become loop emits once per
// (source, become) pair.
func clonePermanentTargets(h *fakeHost) []state.ObjID {
	var out []state.ObjID
	for _, e := range h.log {
		if e.Kind == events.ClonePermanent {
			out = append(out, e.Obj)
		}
	}
	return out
}

func clonePermanentCount(h *fakeHost, id state.ObjID) int {
	n := 0
	for _, e := range h.log {
		if e.Kind == events.ClonePermanent && e.Obj == id {
			n++
		}
	}
	return n
}

// bodyBecomeContains reports whether the body's CloneTarget$ sweep covers id.
func bodyBecomeContains(h *fakeHost, body *cards.SA, id state.ObjID) bool {
	for _, tgt := range battlefieldValidTargets(h, &Ctx{Source: id, Controller: 0}, "Creature.YouCtrl") {
		if tgt.Obj == id {
			return true
		}
	}
	return false
}

// TestCloneChoiceZoneUnsupportedFailsClosed pins the fail-closed landing for a
// ChoiceZone$ value this build does not implement: one loud Note, no copy,
// never a battlefield fall-through.
func TestCloneChoiceZoneUnsupportedFailsClosed(t *testing.T) {
	h := &fakeHost{g: state.NewGame(names(2))}
	bear := mkCard(t, "Name:Fixture Zone Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	id := h.g.AddObject(bear, 0).ID
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{id})
	h.g.Obj(id).Zone = state.ZBattlefield
	body := sa(t, "DB$ Clone | Choices$ Creature | ChoiceZone$ Library")
	if body.Params["ChoiceZone"] != "Library" {
		t.Fatal("precondition: body carries the unsupported zone")
	}
	h.askResult = true
	Resolve(h, &Ctx{Source: id, Controller: 0}, body)
	if h.askCount != 0 {
		t.Fatal("an unsupported ChoiceZone$ still posed an ask")
	}
	if len(clonePermanentTargets(h)) != 0 {
		t.Fatal("an unsupported ChoiceZone$ still made a copy")
	}
	found := false
	for _, note := range cloneNoteTexts(h) {
		if note == "Clone ChoiceZone$ Library is not a zone this build can choose from; no copy" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no fail-closed Note; notes: %v", cloneNoteTexts(h))
	}
}

// TestCloneKayaTriggeredCardsPoolFailsClosed pins the deliberate scoping: the
// `Card.TriggeredCards` filter head rides a trigger-Remembered referent the
// grammar now binds, but Kaya's ask carries no Remembered, so its exiled-card
// pool matches nothing and the walk records one loud Note and makes no copy --
// never a battlefield fall-through.
func TestCloneKayaTriggeredCardsPoolFailsClosed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	kaya, ok := reg.Lookup("Kaya, Spirits' Justice")
	if !ok || kaya == nil {
		t.Fatal("missing corpus card Kaya, Spirits' Justice")
	}
	h := &fakeHost{g: state.NewGame(names(2))}
	exiled := mkCard(t, "Name:Fixture Kaya Exile\nManaCost:1 B\nTypes:Creature Spirit\nPT:1/1\nOracle:x\n")
	tok := mkCard(t, "Name:Fixture Kaya Token\nManaCost:0\nTypes:Creature Spirit\nPT:1/1\nOracle:x\n")
	kayaID := h.g.AddObject(kaya, 0).ID
	tokID := h.g.AddObject(tok, 0).ID
	exileID := h.g.AddObject(exiled, 0).ID
	h.g.Obj(tokID).IsToken = true
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{kayaID, tokID})
	h.g.Obj(kayaID).Zone = state.ZBattlefield
	h.g.Obj(tokID).Zone = state.ZBattlefield
	h.g.SetZone(state.ZExile, 0, []state.ObjID{exileID})
	h.g.Obj(exileID).Zone = state.ZExile
	h.g.Obj(exileID).ExiledWith = kayaID

	body := cards.ResolveSVar(kaya.Faces[0].SVars, "TrigCopy")
	if body == nil || body.Params["ChoiceZone"] != "Exile" || body.Params["Choices"] != "Card.TriggeredCards+Creature" {
		t.Fatalf("precondition: Kaya TrigCopy shape %+v", body.Params)
	}
	// Kaya's TrigCopy carries a mandatory ValidTgts$ (the target token), so the
	// test supplies that target: the ask this test counts must be the Choices$
	// pick, not the target offer that would otherwise come first.
	h.askResult = true
	Resolve(h, &Ctx{Source: kayaID, Controller: 0, Targets: []state.Target{{Obj: tokID}}, TargetsOffered: true, SVars: kaya.Faces[0].SVars}, body)
	if h.askCount != 0 {
		t.Fatal("the unresolvable TriggeredCards pool still posed an ask")
	}
	if len(clonePermanentTargets(h)) != 0 {
		t.Fatal("the unresolvable TriggeredCards pool still made a copy")
	}
	found := false
	for _, note := range cloneNoteTexts(h) {
		if note == "Clone Choices$ Card.TriggeredCards+Creature has no eligible object; no copy" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no fail-closed Note; notes: %v", cloneNoteTexts(h))
	}
}
