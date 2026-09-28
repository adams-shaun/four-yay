package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestCopyPermanentAtEOTTrigSacrificeAndCopy(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := tokenRememberedBoard(t, reg, "Electroduplicate", "Grizzly Bears")
	spell, target := ids["Electroduplicate"], ids["Grizzly Bears"]
	if e.G.Obj(spell).Zone != state.ZBattlefield || e.G.Obj(target).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Electroduplicate and target must be on battlefield")
	}
	sa := e.G.Obj(spell).Face().SpellAbility()
	if sa == nil || sa.API != "CopyPermanent" || sa.Params["AtEOTTrig"] != "Sacrifice" {
		t.Fatalf("precondition: real Electroduplicate CopyPermanent/AtEOTTrig ability missing: %+v", sa)
	}

	mint := func(target state.ObjID, ability *cards.SA) state.ObjID {
		effects.Resolve(e, &effects.Ctx{Source: spell, Controller: 0,
			Targets: []state.Target{{Obj: target}}, TargetsOffered: true,
			SVars: e.G.Obj(spell).Face().SVars}, ability)
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			o := e.G.Obj(id)
			if o != nil && o.IsToken && o.ID != target && o.AtEOTTrigBody != "" {
				return id
			}
		}
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.ID != 0 {
				t.Logf("obj %d zone=%v token=%v body=%q ctrl=%d", o.ID, o.Zone, o.IsToken, o.AtEOTTrigBody, o.Controller)
			}
		}
		t.Fatalf("CopyPermanent did not mint an AtEOTTrig token; bf0=%v events: %+v", e.G.Zone(state.ZBattlefield, 0), e.L.Events)
		return 0
	}

	first := mint(target, sa)
	if !e.G.Obj(first).IsToken || e.G.Obj(first).AtEOTTrigBody == "" {
		t.Fatalf("precondition: first mint lacks token/body: %+v", e.G.Obj(first))
	}
	if !e.hasKeywordH(first, kwhHaste) {
		t.Fatal("Electroduplicate token lacks haste")
	}
	if _, ok := e.attackableCreature(first); !ok {
		t.Fatalf("hasty token cannot attack this turn (turn %d, active %d)", e.G.Turn, e.G.Active)
	}

	// The second CopyPermanent omits AtEOTTrig$: its token must inherit the
	// first token's copiable trigger body from the CopyToken source snapshot.
	copyAbility := *sa
	copyAbility.Params = make(map[string]string, len(sa.Params))
	for k, v := range sa.Params {
		if k != "AtEOTTrig" {
			copyAbility.Params[k] = v
		}
	}
	second := mint(first, &copyAbility)
	if e.G.Obj(second).AtEOTTrigBody != e.G.Obj(first).AtEOTTrigBody {
		t.Fatalf("copied token body %q, source body %q", e.G.Obj(second).AtEOTTrigBody, e.G.Obj(first).AtEOTTrigBody)
	}

	// A StepChange to the end step fires both objects' own copied triggers.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if len(e.pendingTriggers) < 2 {
		t.Fatalf("end step queued %d triggers, want both token triggers", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	answerTriggerOrders(t, e)
	for i := 0; i < 2; i++ {
		if len(e.G.Stack) == 0 {
			t.Fatalf("expected both end-step triggers on the stack; events: %+v", e.L.Events)
		}
		e.resolveTop()
		e.putTriggersOnStack()
		answerTriggerOrders(t, e)
	}
	for _, id := range []state.ObjID{first, second} {
		if got := e.G.Obj(id); got == nil || got.Zone != state.ZGraveyard {
			t.Fatalf("AtEOTTrig token %d not sacrificed at end step: %+v", id, got)
		}
	}
	replayCheck(t, e, cfg)
}

// TestCopyPermanentAtEOTTrigExile pins the corpus's other implemented
// AtEOTTrig$ body: Heat Shimmer's "at the beginning of the end step, exile
// this permanent" (the __cpAtEOTExile builtin, not the Sacrifice one). The
// exiled token ceases to exist (CR 111.7 / SBA), so it is asserted to be
// either in exile or already ceased.
func TestCopyPermanentAtEOTTrigExile(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := tokenRememberedBoard(t, reg, "Heat Shimmer", "Grizzly Bears")
	spell, target := ids["Heat Shimmer"], ids["Grizzly Bears"]
	sa := e.G.Obj(spell).Face().SpellAbility()
	if sa == nil || sa.API != "CopyPermanent" || sa.Params["AtEOTTrig"] != "Exile" {
		t.Fatalf("precondition: real Heat Shimmer CopyPermanent/AtEOTTrig Exile ability missing: %+v", sa)
	}
	effects.Resolve(e, &effects.Ctx{Source: spell, Controller: 0,
		Targets: []state.Target{{Obj: target}}, TargetsOffered: true,
		SVars: e.G.Obj(spell).Face().SVars}, sa)
	tok := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.ID != target {
			tok = id
		}
	}
	if tok == 0 {
		t.Fatalf("Heat Shimmer minted no token; events: %+v", e.L.Events)
	}
	if got := e.G.Obj(tok).AtEOTTrigBody; got != "__cpAtEOTExile" {
		t.Fatalf("token body %q, want the Exile builtin (precondition)", got)
	}

	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if len(e.pendingTriggers) == 0 {
		t.Fatal("end step queued no AtEOTTrig Exile trigger")
	}
	e.putTriggersOnStack()
	answerTriggerOrders(t, e)
	if len(e.G.Stack) == 0 {
		t.Fatalf("Exile trigger never reached the stack; events: %+v", e.L.Events)
	}
	e.resolveTop()
	e.putTriggersOnStack()
	answerTriggerOrders(t, e)
	if o := e.G.Obj(tok); o != nil && o.Zone != state.ZExile && o.Zone != state.ZCeased {
		t.Fatalf("AtEOTTrig Exile token in zone %v, want exile/ceased: %+v", o.Zone, o)
	}
	replayCheck(t, e, cfg)
}

// TestCopyPermanentAtEOTTrigUnknownValueIsLoud pins the fail-loud contract for
// an AtEOTTrig$ body this build does not model (Gut Fanatical Priestess'
// `You_Sacrifice` is the only corpus spelling outside Sacrifice/Exile): the
// copy still mints, carries NO end-step trigger, and exactly one Note names
// the unread value -- never a silent free permanent. The handler itself is
// proven to have run by the mint assertion, so a registration revert cannot
// pass this test vacuously.
func TestCopyPermanentAtEOTTrigUnknownValueIsLoud(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := tokenRememberedBoard(t, reg, "Electroduplicate", "Grizzly Bears")
	spell, target := ids["Electroduplicate"], ids["Grizzly Bears"]
	base := e.G.Obj(spell).Face().SpellAbility()
	if base == nil || base.API != "CopyPermanent" {
		t.Fatalf("precondition: real Electroduplicate CopyPermanent ability missing: %+v", base)
	}
	sa := *base
	sa.Params = make(map[string]string, len(base.Params))
	for k, v := range base.Params {
		sa.Params[k] = v
	}
	sa.Params["AtEOTTrig"] = "You_Sacrifice"

	effects.Resolve(e, &effects.Ctx{Source: spell, Controller: 0,
		Targets: []state.Target{{Obj: target}}, TargetsOffered: true,
		SVars: e.G.Obj(spell).Face().SVars}, &sa)
	tok := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.ID != target {
			tok = id
		}
	}
	if tok == 0 {
		t.Fatalf("handler did not run: no token minted; events: %+v", e.L.Events)
	}
	if got := e.G.Obj(tok).AtEOTTrigBody; got != "" {
		t.Fatalf("unmodelled AtEOTTrig$ body %q carried onto the token, want none", got)
	}
	want := "AtEOTTrig$ You_Sacrifice is not implemented"
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, want) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no loud Note %q; events: %+v", want, e.L.Events)
	}
	replayCheck(t, e, cfg)
}
