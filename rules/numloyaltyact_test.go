package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the Effect-delivered Mode$ NumLoyaltyAct route (task
// pw-numloyaltyact): an api:Effect body that grants extra loyalty-ability
// activations per turn must register into the continuous registry (effects'
// effEffect) and be read by rules' loyaltyAbilityLimit alongside the printed
// S: route, so CR 606.3's once-per-turn limit is lifted exactly as the card
// text says. The real corpus carriers are Kaito, Dancing Shadow's PWTwice
// (Twice$ True, ValidCard$ Card.EffectSource), Comet, Stellar Pup's
// LoyaltyAbs (Additional$ 2, ValidCard$ Card.EffectSource) and Urza Assembles
// the Titans' PWTwice (Twice$ True, ValidCard$ Planeswalker.YouCtrl).

// TestEffectDeliveredNumLoyaltyActTwiceGrantsSecondActivation drives the real
// Kaito, Dancing Shadow Effect body and asserts BOTH halves of the fix: the
// registration is real (no unimplemented Note) and the granted limit allows a
// second activation while withholding a third (CR 606.3).
func TestEffectDeliveredNumLoyaltyActTwiceGrantsSecondActivation(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	kaito := mustCorpusCard(t, reg, "Kaito, Dancing Shadow")
	e, _, kaitoID := walkerBoard(t, reg, "Kaito, Dancing Shadow")

	// Preconditions: Kaito is a battlefield planeswalker whose only
	// NumLoyaltyAct source is the Effect body (its PWTwice is an SVar, never
	// a printed S: line), so the plain CR 606.3 limit is 1 before the grant.
	if o := e.G.Obj(kaitoID); o.Zone != state.ZBattlefield || !faceHasType(o, "Planeswalker") {
		t.Fatalf("precondition: Kaito is not a battlefield planeswalker (%s)", o.Zone)
	}
	if got := e.loyaltyAbilityLimit(kaitoID); got != 1 {
		t.Fatalf("precondition: limit before the effect = %d, want 1", got)
	}
	sa := cards.ResolveSVar(kaito.Faces[0].SVars, "TrigLoyalty")
	if sa == nil || sa.API != "Effect" || sa.Params["StaticAbilities"] != "PWTwice" {
		t.Fatalf("fixture: Kaito's TrigLoyalty body changed: %+v", sa)
	}

	before := len(e.L.Events)
	e.resolveAbility(kaitoID, 0, nil, sa, kaito.Faces[0].SVars)
	// The handler must have RUN: effEffect reports an unreadable body as an
	// unimplemented Note, so a Note here means the registration never
	// happened (the box-measured "test passes with the feature unregistered"
	// trap).
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented") {
			t.Fatalf("EffEffect did not register Kaito's NumLoyaltyAct body: %q", ev.Text)
		}
	}
	if got := e.loyaltyAbilityLimit(kaitoID); got != 2 {
		t.Fatalf("Effect-delivered Twice$ True limit = %d, want 2", got)
	}

	// The limit is observable through the offer, not just the helper: after
	// one activation the same [0] ability is offered again, and after the
	// second it is withheld.
	zero := jaceAbility(t, kaito, "AddCounter", 0)
	e.Advance()
	if !loyaltyAbilityOffered(e, 0, kaitoID, zero) {
		t.Fatal("precondition: [0] not offered at all")
	}
	opt := abilityOption(t, e, kaitoID, zero)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if !loyaltyAbilityOffered(e, 0, kaitoID, zero) {
		t.Fatal("[0] not offered a second time under the Effect-delivered Twice grant")
	}
	opt = abilityOption(t, e, kaitoID, zero)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if loyaltyAbilityOffered(e, 0, kaitoID, zero) {
		t.Fatal("[0] offered a third time (the Twice grant allows exactly two)")
	}
}

// TestEffectDeliveredNumLoyaltyActAdditionalRaisesLimit drives the real
// Comet, Stellar Pup Effect body, whose Additional$ 2 raises the limit to 3
// (base 1 + 2). It is the second parameter of the combination rule, so it
// gets its own real corpus assertion rather than being inferred from Twice.
func TestEffectDeliveredNumLoyaltyActAdditionalRaisesLimit(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	comet := mustCorpusCard(t, reg, "Comet, Stellar Pup")
	e, _, cometID := walkerBoard(t, reg, "Comet, Stellar Pup")

	if got := e.loyaltyAbilityLimit(cometID); got != 1 {
		t.Fatalf("precondition: limit before the effect = %d, want 1", got)
	}
	sa := cards.ResolveSVar(comet.Faces[0].SVars, "DBEffect")
	if sa == nil || sa.API != "Effect" || sa.Params["StaticAbilities"] != "LoyaltyAbs" {
		t.Fatalf("fixture: Comet's DBEffect body changed: %+v", sa)
	}
	before := len(e.L.Events)
	e.resolveAbility(cometID, 0, nil, sa, comet.Faces[0].SVars)
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented") {
			t.Fatalf("EffEffect did not register Comet's NumLoyaltyAct body: %q", ev.Text)
		}
	}
	if got := e.loyaltyAbilityLimit(cometID); got != 3 {
		t.Fatalf("Effect-delivered Additional$ 2 limit = %d, want 3", got)
	}
}

// TestEffectDeliveredNumLoyaltyActControllerScopeTwice drives the real Urza
// Assembles the Titans Effect body, whose ValidCard$ Planeswalker.YouCtrl
// scopes the Twice grant to a DIFFERENT permanent (the walker the Saga's
// controller controls), proving the read resolves ValidCard$ against the
// effect's own source and controller rather than the permanent's.
func TestEffectDeliveredNumLoyaltyActControllerScopeTwice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	urza := mustCorpusCard(t, reg, "Urza Assembles the Titans")
	e, _, jaceID := walkerBoard(t, reg, "Jace, the Mind Sculptor", urza)

	var urzaID state.ObjID
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Card == urza && o.Zone == state.ZBattlefield {
			urzaID = o.ID
		}
	}
	if urzaID == 0 {
		t.Fatal("fixture: Urza Assembles the Titans is not on the battlefield")
	}
	if got := e.loyaltyAbilityLimit(jaceID); got != 1 {
		t.Fatalf("precondition: Jace's limit before the effect = %d, want 1", got)
	}
	sa := cards.ResolveSVar(urza.Faces[0].SVars, "DBLoyalty")
	if sa == nil || sa.API != "Effect" || sa.Params["StaticAbilities"] != "PWTwice" {
		t.Fatalf("fixture: Urza's DBLoyalty body changed: %+v", sa)
	}
	before := len(e.L.Events)
	e.resolveAbility(urzaID, 0, nil, sa, urza.Faces[0].SVars)
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented") {
			t.Fatalf("EffEffect did not register Urza's NumLoyaltyAct body: %q", ev.Text)
		}
	}
	// Jace is the OTHER permanent the effect's ValidCard$ selects.
	if got := e.loyaltyAbilityLimit(jaceID); got != 2 {
		t.Fatalf("Planeswalker.YouCtrl Twice grant on another permanent = %d, want 2", got)
	}
}

// TestEffectDeliveredNumLoyaltyActChainVeilActivatedAbility drives The Chain
// Veil's REAL activated ability (A:AB$ Effect | StaticAbilities$ LoyaltyAbs,
// Additional$ 1), which is the activated-ability delivery route rather than a
// DB$ sub-ability, and covers the Additional$ 1 value on ValidCard$
// Planeswalker.YouCtrl. Before the fix the ability emitted the
// NumLoyaltyAct unimplemented Note and granted nothing.
func TestEffectDeliveredNumLoyaltyActChainVeilActivatedAbility(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	veil := mustCorpusCard(t, reg, "The Chain Veil")
	e, _, jaceID := walkerBoard(t, reg, "Jace, the Mind Sculptor", veil)

	var veilID state.ObjID
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Card == veil && o.Zone == state.ZBattlefield {
			veilID = o.ID
		}
	}
	if veilID == 0 {
		t.Fatal("fixture: The Chain Veil is not on the battlefield")
	}
	if got := e.loyaltyAbilityLimit(jaceID); got != 1 {
		t.Fatalf("precondition: Jace's limit before the ability = %d, want 1", got)
	}
	sa := veil.Faces[0].Abilities[0]
	if sa == nil || sa.API != "Effect" || sa.Params["StaticAbilities"] != "LoyaltyAbs" {
		t.Fatalf("fixture: The Chain Veil's activated body changed: %+v", sa)
	}
	before := len(e.L.Events)
	// Resolve the real ability body without the {4}, {T} payment gates --
	// the registration path is what this ticket changes.
	e.resolveAbility(veilID, 0, nil, sa, veil.Faces[0].SVars)
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented") {
			t.Fatalf("EffEffect did not register The Chain Veil's NumLoyaltyAct body: %q", ev.Text)
		}
	}
	if got := e.loyaltyAbilityLimit(jaceID); got != 2 {
		t.Fatalf("The Chain Veil's Additional$ 1 limit = %d, want 2", got)
	}
}

// TestEffectDeliveredNumLoyaltyActUnreadParamFailsClosed pins the readable
// gate: an Effect-delivered body carrying a scoping term this build does not
// evaluate must report unimplemented and register nothing, rather than
// granting blanket. Precondition is the positive sibling above, which proves
// the same registration path DOES go live for a readable body.
func TestEffectDeliveredNumLoyaltyActUnreadParamFailsClosed(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	src := card(t, "Name:BadGrant\nTypes:Creature\nPT:1/1\n"+
		"SVar:DBEffect:DB$ Effect | StaticAbilities$ Bad\n"+
		"SVar:Bad:Mode$ NumLoyaltyAct | ValidCard$ Card.EffectSource | Twice$ True | IsPresent$ Creature\n"+
		"Oracle:x\n")
	e, _, jaceID := walkerBoard(t, reg, "Jace, the Mind Sculptor", src)

	var srcID state.ObjID
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Card == src && o.Zone == state.ZBattlefield {
			srcID = o.ID
		}
	}
	if srcID == 0 {
		t.Fatal("fixture: the granting creature is not on the battlefield")
	}
	sa := cards.ResolveSVar(src.Faces[0].SVars, "DBEffect")
	if sa == nil || sa.API != "Effect" {
		t.Fatalf("fixture: BadGrant's DBEffect body changed: %+v", sa)
	}
	before := len(e.L.Events)
	e.resolveAbility(srcID, 0, nil, sa, src.Faces[0].SVars)
	sawNote := false
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "NumLoyaltyAct unimplemented") {
			sawNote = true
		}
	}
	if !sawNote {
		t.Fatal("an unreadable NumLoyaltyAct body did not report unimplemented")
	}
	if got := e.loyaltyAbilityLimit(jaceID); got != 1 {
		t.Fatalf("unreadable NumLoyaltyAct body raised the limit to %d, want 1", got)
	}
}
