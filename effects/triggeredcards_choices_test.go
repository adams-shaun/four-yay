package effects

// Card.TriggeredCards as an ORDINARY filter spec (not the Defined$
// TriggeredCards selector and not the ChangeZoneAll ChangeType$ spelling) is
// reached by three real corpus carriers through the shared filter matcher:
//
//   - Kaya, Spirits' Justice `Clone Choices$ Card.TriggeredCards+Creature`
//     via cloneChoiceCandidates -> MatchesObjectCtx;
//   - Dunbarrow Revivalist `ChooseCard Choices$ Card.TriggeredCards` via
//     cardChoices -> choiceMatches -> MatchesObjectCtx.
//
// A trigger resolution binds Remembered on the Ctx (rules/trigger_queue.go
// encodes it, rules/resolution.go restores it), so after the predicate is
// registered those pools are non-empty in real play where they previously
// failed closed. These tests drive the real card SVars through the real
// primitive with a bound Remembered and pin the new pool -- the side effect
// the registration has, asserted rather than assumed away.
//
// Twilight Diviner's `CopyPermanent Choices$ Card.TriggeredCards` is the
// CONTROL: CopyPermanentOf blocks every Choices$ shape except the measured
// ChooserRemembered / SaddledThisTurn families, so Twilight's ask never
// reaches the filter grammar and the registration does NOT touch it.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestTriggeredCardsCloneChoicesPoolMatchesRemembered drives Kaya's real
// TrigCopy (DB$ Clone) with the triggering exile batch bound on
// Ctx.Remembered. The remembered exiled creature is the copy pool: the walk
// poses the KChoose with it offered and copies its face onto the target token.
func TestTriggeredCardsCloneChoicesPoolMatchesRemembered(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	kaya, ok := reg.Lookup("Kaya, Spirits' Justice")
	if !ok || kaya == nil {
		t.Fatal("missing corpus card Kaya, Spirits' Justice")
	}
	body := cards.ResolveSVar(kaya.Faces[0].SVars, "TrigCopy")
	if body == nil || body.Params["Choices"] != "Card.TriggeredCards+Creature" || body.Params["ChoiceZone"] != "Exile" {
		t.Fatalf("precondition: Kaya TrigCopy shape %+v", body.Params)
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
	// Preconditions the assertions depend on: the copy source really is an
	// exiled creature card and the target really is a token on the battlefield.
	if o := h.g.Obj(exileID); o == nil || o.Zone != state.ZExile || !o.Face().IsCreature() {
		t.Fatalf("precondition failed: exile source %+v, want an exiled creature card", o)
	}
	if o := h.g.Obj(tokID); o == nil || o.Zone != state.ZBattlefield || !o.IsToken {
		t.Fatalf("precondition failed: target %+v, want a battlefield token", o)
	}

	h.askResult = true
	Resolve(h, &Ctx{Source: kayaID, Controller: 0, Targets: []state.Target{{Obj: tokID}},
		TargetsOffered: true, Remembered: []state.Target{{Obj: exileID}},
		SVars: kaya.Faces[0].SVars}, body)

	if h.askCount != 1 {
		t.Fatalf("askCount = %d, want 1 (the remembered exiled creature must be offered as the pool)", h.askCount)
	}
	if h.lastAsk == nil || h.lastAsk.Kind != decision.KChoose {
		t.Fatalf("lastAsk = %+v, want a KChoose over the remembered exile", h.lastAsk)
	}
	offered := false
	for _, o := range h.lastAsk.Options {
		if o.Obj == exileID {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("Choices$ pool %+v does not offer the remembered exile %d", h.lastAsk.Options, exileID)
	}
	// The copy basis (ClonePermanent.IDs) is the remembered exile: the pool
	// bound the source, exactly the change the predicate registration makes.
	copied := false
	for _, ev := range h.log {
		if ev.Kind != events.ClonePermanent {
			continue
		}
		for _, id := range ev.IDs {
			if id == exileID {
				copied = true
			}
		}
	}
	if !copied {
		t.Fatalf("no copy made from the remembered exile %d; log %+v", exileID, h.log)
	}
	for _, note := range cloneNoteTexts(h) {
		if note == "Clone Choices$ Card.TriggeredCards+Creature has no eligible object; no copy" {
			t.Fatalf("the remembered pool still failed closed: %q", note)
		}
	}
}

// TestTriggeredCardsChooseCardChoicesPoolMatchesRemembered drives Dunbarrow
// Revivalist's real TrigChoose (DB$ ChooseCard) with the entering-creatures
// batch bound on Ctx.Remembered. The pool is exactly the remembered creature:
// an unrelated battlefield creature is not offered.
func TestTriggeredCardsChooseCardChoicesPoolMatchesRemembered(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	dunbarrow, ok := reg.Lookup("Dunbarrow Revivalist")
	if !ok || dunbarrow == nil {
		t.Fatal("missing corpus card Dunbarrow Revivalist")
	}
	body := cards.ResolveSVar(dunbarrow.Faces[0].SVars, "TrigChoose")
	if body == nil || body.Params["Choices"] != "Card.TriggeredCards" {
		t.Fatalf("precondition: Dunbarrow TrigChoose shape %+v", body.Params)
	}
	h := &fakeHost{g: state.NewGame(names(2))}
	src := h.g.AddObject(dunbarrow, 0).ID
	entered := h.g.AddObject(mkCard(t, "Name:Fixture Entered\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"), 0).ID
	other := h.g.AddObject(mkCard(t, "Name:Fixture Other\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0).ID
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src, entered, other})
	for _, id := range []state.ObjID{src, entered, other} {
		h.g.Obj(id).Zone = state.ZBattlefield
	}
	// Preconditions: both creatures are really on the battlefield, so only
	// remembered-set membership can separate them.
	for _, id := range []state.ObjID{entered, other} {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZBattlefield || !o.Face().IsCreature() {
			t.Fatalf("precondition failed: candidate %d = %+v, want a battlefield creature", id, o)
		}
	}

	c := &Ctx{Source: src, Controller: 0, Remembered: []state.Target{{Obj: entered}}, SVars: dunbarrow.Faces[0].SVars}
	got := cardChoices(h, c, body, 0)
	ids := map[state.ObjID]bool{}
	for _, tgt := range got {
		ids[tgt.Obj] = true
	}
	if !ids[entered] {
		t.Fatalf("Choices$ pool %v does not include the remembered entered creature %d", ids, entered)
	}
	if ids[other] {
		t.Fatalf("Choices$ pool %v includes an object outside the remembered set %d", ids, other)
	}
}

// TestTriggeredCardsCopyPermanentChoicesStaysBlocked is the control for the
// two carriers above: Twilight Diviner's real TrigCopy is
// `CopyPermanent Choices$ Card.TriggeredCards`, but CopyPermanentOf blocks
// every Choices$ shape outside the measured ChooserRemembered / SaddledThisTurn
// families, so the body mints nothing and never reaches the filter grammar --
// the registration does not change this card.
func TestTriggeredCardsCopyPermanentChoicesStaysBlocked(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	twilight, ok := reg.Lookup("Twilight Diviner")
	if !ok || twilight == nil {
		t.Fatal("missing corpus card Twilight Diviner")
	}
	body := cards.ResolveSVar(twilight.Faces[0].SVars, "TrigCopy")
	if body == nil || body.Params["Choices"] != "Card.TriggeredCards" {
		t.Fatalf("precondition: Twilight TrigCopy shape %+v", body.Params)
	}
	cp := CopyPermanentOf(body)
	if !cp.Blocked || cp.SupportsChoice {
		t.Fatalf("precondition: Twilight CopyPermanent must stay blocked (Blocked=%v SupportsChoice=%v); if this changed the control is stale", cp.Blocked, cp.SupportsChoice)
	}

	h := &fakeHost{g: state.NewGame(names(2))}
	src := h.g.AddObject(twilight, 0).ID
	entered := h.g.AddObject(mkCard(t, "Name:Fixture Entered\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"), 0).ID
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src, entered})
	h.g.Obj(src).Zone = state.ZBattlefield
	h.g.Obj(entered).Zone = state.ZBattlefield

	h.askResult = true
	Resolve(h, &Ctx{Source: src, Controller: 0, Remembered: []state.Target{{Obj: entered}},
		SVars: twilight.Faces[0].SVars}, body)
	if h.askCount != 0 {
		t.Fatalf("askCount = %d, want 0 (a blocked Choices$ must not reach the filter grammar)", h.askCount)
	}
	// The handler really ran: effCopyPermanent emits CopyPermanentOf's
	// SkippedNote before its Blocked early-return. Without this the test would
	// pass with the whole primitive unregistered.
	handlerRan := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && ev.Text == cp.SkippedNote && cp.SkippedNote != "" {
			handlerRan = true
		}
	}
	if !handlerRan {
		t.Fatalf("effCopyPermanent did not run (no SkippedNote %q); log %+v", cp.SkippedNote, h.log)
	}
	for i := range h.g.Objs {
		if h.g.Objs[i].IsToken {
			t.Fatalf("a blocked CopyPermanent Choices$ minted a token: %+v", h.g.Objs[i].Face())
		}
	}
}
