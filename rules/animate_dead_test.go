package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The Animate Dead / Dance of the Dead end-to-end pins (the
// api:Animate.RemoveKeywords ticket). The graveyard-enchant Aura family casts
// at a creature card in a graveyard (K:Enchant:Creature.inZoneGraveyard), its
// ETB trigger reanimates the enchanted card, and the reanimate chain's
// `DB$ Animate | RemoveKeywords$ ... | Keywords$ ...` swaps the stale graveyard
// enchant for the granted "enchant creature put onto the battlefield with
// CARDNAME". Four defects kept that chain from ever running; each test here
// pins one home:
//
//   - the cast offer (rules/stack.go targetZones' Attach-scoped inZone census),
//   - the resolution attach onto the off-battlefield bearer
//     (effects/attach.go effAttach),
//   - the CR 704.5m sweep's off-battlefield-bearer exemption for the window
//     between entry and the reanimate trigger (rules/attach.go
//     attachmentSBAs),
//   - the SBA reading the DERIVED enchant, so the post-animate board survives
//     (rules/attach.go auraStillMatchesEnchant) and the stale enchant is gone
//     from the derived list (effects/combatfx.go effAnimate's RemoveKeywords$
//     read, registered through the layer-6 walk).

// graveyardEnchantFixture returns an engine with the named graveyard-enchant
// Aura in seat 0's hand and a Grizzly Bears in seat 0's graveyard, parked at
// seat 0's Main1 priority. Nothing else is on the battlefield, so the cast's
// only legal target is the graveyard bear.
func graveyardEnchantFixture(t *testing.T, seed uint64, auraName string) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	aura := mustCorpusCard(t, reg, auraName)
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, _ := tokenReplGame(t, seed, aura, bear)
	auraID := searchMoveByName(t, e, auraName, state.ZHand)
	bearID := searchMoveByName(t, e, "Grizzly Bears", state.ZGraveyard)
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("fixture precondition: bear zone %v, want graveyard", o.Zone)
	}
	return e, auraID, bearID
}

// castGraveyardEnchant submits the aura's cast option, answers the target ask
// with the graveyard bear, and drains the stack (the resolution attach plus the
// whole ETB reanimate chain). It asserts every step's precondition, so a
// vacuous pass is impossible.
func castGraveyardEnchant(t *testing.T, e *Engine, auraID, bearID state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	optIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == auraID {
			optIdx = o.Index
		}
	}
	if optIdx < 0 {
		t.Fatalf("the graveyard-enchant cast was not offered with a legal graveyard target: %+v", d.Options)
	}
	submitChoices(t, e, optIdx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the attach target ask, got %+v", d)
	}
	bearIdx := indexOfObjOption(d, bearID)
	if bearIdx < 0 {
		t.Fatalf("the graveyard creature is not offered as the attach target: %+v", d.Options)
	}
	submitChoices(t, e, bearIdx)
	passUntilStackEmpty(t, e, 30)
	// Stop at the pre-reanimate window: the Aura has resolved onto the
	// battlefield attached to the still-in-the-graveyard bear, and the ETB
	// trigger is only queued at the NEXT priority round's checkpoint.
}

// driveReanimateChain passes priority until the ETB trigger has been queued at
// a checkpoint and its whole reanimate chain (ChangeZone -> Animate -> Attach)
// has resolved: the enchanted card is back on the battlefield.
func driveReanimateChain(t *testing.T, e *Engine, bearID state.ObjID) {
	t.Helper()
	for i := 0; i < 8; i++ {
		if b := e.G.Obj(bearID); b != nil && b.Zone == state.ZBattlefield {
			return
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("expected a priority decision while waiting for the reanimate chain: %+v", d)
		}
		passPriorityOnce(t, e)
		passPriorityOnce(t, e)
		passUntilStackEmpty(t, e, 30)
	}
	t.Fatalf("the reanimate chain never returned the enchanted card to the battlefield")
}

// assertDerivedEnchantSwapped is Done-3's read: the aura's derived keyword list
// carries the granted IsRemembered enchant and no graveyard enchant.
func assertDerivedEnchantSwapped(t *testing.T, e *Engine, auraID state.ObjID) {
	t.Helper()
	kws := e.Derived(auraID).Keywords
	for _, k := range kws {
		if strings.Contains(k, "inZoneGraveyard") {
			t.Fatalf("stale graveyard enchant still on the derived list: %q (all: %v)", k, kws)
		}
	}
	granted := false
	for _, k := range kws {
		if cards.KeywordHead(k) == "Enchant" && strings.Contains(k, "IsRemembered") {
			granted = true
		}
	}
	if !granted {
		t.Fatalf("the granted IsRemembered enchant is missing from the derived list %v", kws)
	}
}

// TestAnimateDeadCastOfferedTargetsGraveyardCreature is Done-1: with a legal
// target in the graveyard the cast IS offered, targets exactly that creature,
// and resolves -- the ETB reanimate trigger is queued at the drain's own
// checkpoints and resolves with it, so by the end of the drain the bear is
// already back under the caster's control (the pre-reanimate window itself is
// pinned raw in TestAnimateDeadGraveyardWindowSurvivesSBA).
func TestAnimateDeadCastOfferedTargetsGraveyardCreature(t *testing.T) {
	t.Parallel()
	e, auraID, bearID := graveyardEnchantFixture(t, 4711, "Animate Dead")
	addMana(t, e, 0, "BB") // {1}{B}: the generic 1 is payable with black
	castGraveyardEnchant(t, e, auraID, bearID)

	ao := e.G.Obj(auraID)
	if ao == nil || ao.Zone != state.ZBattlefield || ao.AttachedTo != bearID {
		t.Fatalf("Animate Dead zone %s attachedTo %d; want battlefield attached to bear %d",
			ao.Zone, ao.AttachedTo, bearID)
	}
	if b := e.G.Obj(bearID); b == nil || b.Zone != state.ZBattlefield || b.Controller != 0 {
		t.Fatalf("bear zone %s controller %d; want battlefield seat 0 after the reanimate trigger",
			b.Zone, b.Controller)
	}
}

// TestAnimateDeadReanimateChainCompletes is Done-2 and Done-3: the ETB trigger
// returns the bear under the caster's control, the Aura is attached to it, the
// derived keyword list carries the granted IsRemembered enchant and no
// graveyard enchant, and the pair survives a real turn boundary's SBA
// checkpoints.
func TestAnimateDeadReanimateChainCompletes(t *testing.T) {
	t.Parallel()
	e, auraID, bearID := graveyardEnchantFixture(t, 4712, "Animate Dead")
	addMana(t, e, 0, "BB")
	castGraveyardEnchant(t, e, auraID, bearID)
	driveReanimateChain(t, e, bearID)

	b := e.G.Obj(bearID)
	if b == nil || b.Zone != state.ZBattlefield || b.Controller != 0 {
		t.Fatalf("reanimated bear zone %s controller %d; want battlefield seat 0", b.Zone, b.Controller)
	}
	ao := e.G.Obj(auraID)
	if ao == nil || ao.Zone != state.ZBattlefield || ao.AttachedTo != bearID {
		t.Fatalf("aura zone %s attachedTo %d; want battlefield attached to the reanimated bear %d",
			ao.Zone, ao.AttachedTo, bearID)
	}
	assertDerivedEnchantSwapped(t, e, auraID)

	next := state.PlayerID(1 - int(e.G.Active))
	driveToStep(t, e, e.G.Turn+1, next, state.StepMain1)
	if ao := e.G.Obj(auraID); ao == nil || ao.Zone != state.ZBattlefield || ao.AttachedTo != bearID {
		t.Fatalf("after the turn boundary: aura zone %s attachedTo %d; want battlefield still attached to bear %d",
			ao.Zone, ao.AttachedTo, bearID)
	}
}

// TestDanceOfTheDeadReanimateChainCompletes is the same shape on the second
// carrier (the target enters tapped; the enchant swap is identical).
func TestDanceOfTheDeadReanimateChainCompletes(t *testing.T) {
	t.Parallel()
	e, auraID, bearID := graveyardEnchantFixture(t, 4713, "Dance of the Dead")
	addMana(t, e, 0, "BB")
	castGraveyardEnchant(t, e, auraID, bearID)
	driveReanimateChain(t, e, bearID)

	b := e.G.Obj(bearID)
	if b == nil || b.Zone != state.ZBattlefield || b.Controller != 0 {
		t.Fatalf("reanimated bear zone %s controller %d; want battlefield seat 0", b.Zone, b.Controller)
	}
	ao := e.G.Obj(auraID)
	if ao == nil || ao.Zone != state.ZBattlefield || ao.AttachedTo != bearID {
		t.Fatalf("aura zone %s attachedTo %d; want battlefield attached to the reanimated bear %d",
			ao.Zone, ao.AttachedTo, bearID)
	}
	assertDerivedEnchantSwapped(t, e, auraID)

	next := state.PlayerID(1 - int(e.G.Active))
	driveToStep(t, e, e.G.Turn+1, next, state.StepMain1)
	if ao := e.G.Obj(auraID); ao == nil || ao.Zone != state.ZBattlefield || ao.AttachedTo != bearID {
		t.Fatalf("after the turn boundary: aura zone %s attachedTo %d; want battlefield still attached to bear %d",
			ao.Zone, ao.AttachedTo, bearID)
	}
}

// TestAnimateDeadGraveyardWindowSurvivesSBA pins the two attachmentSBAs halves
// on a raw board (no cast, no triggers): the CR 704.5m window attachment
// (Animate Dead attached to a creature card in the graveyard) survives the
// sweep, while an ordinary Aura whose bearer left the battlefield is still
// swept by the same pass (the regression guard that keeps the exemption
// zone-positive).
func TestAnimateDeadGraveyardWindowSurvivesSBA(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	aura := mustCorpusCard(t, reg, "Animate Dead")
	strength := mustCorpusCard(t, reg, "Unholy Strength")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	cub := mustCorpusCard(t, reg, "Bear Cub")
	e, _ := tokenReplGame(t, 9101, aura, bear, cub, strength)

	// The window board is hand-built (stageRawEntry): both Auras are placed
	// first and attached by the hand-written Attach events below.
	adID := searchMoveByName(t, e, "Animate Dead", state.ZHand)
	stageRawEntry(t, e, adID)
	bearGY := searchMoveByName(t, e, "Grizzly Bears", state.ZGraveyard)
	usaID := searchMoveByName(t, e, "Unholy Strength", state.ZHand)
	stageRawEntry(t, e, usaID)
	cubBF := searchMoveByName(t, e, "Bear Cub", state.ZBattlefield)
	if adID == 0 || bearGY == 0 || usaID == 0 || cubBF == 0 {
		t.Fatalf("fixture setup failed: aura=%d bearGY=%d strength=%d cub=%d", adID, bearGY, usaID, cubBF)
	}
	// Precondition: the ordinary Aura IS legally attached right now (a bare
	// battlefield attachment must not be swept by this pass).
	e.emit(events.Event{Kind: events.Attach, Obj: usaID, IDs: []state.ObjID{cubBF}})
	// The window state: the graveyard-enchant Aura attached to its graveyard
	// bearer, exactly as it sits between entry and the reanimate trigger.
	e.emit(events.Event{Kind: events.Attach, Obj: adID, IDs: []state.ObjID{bearGY}})
	e.pending = nil
	if e.attachmentSBAs() {
		t.Fatal("the pre-reanimate window attachment was swept; CR 704.5m must admit the graveyard-enchant bearer")
	}
	if o := e.G.Obj(adID); o == nil || o.Zone != state.ZBattlefield || o.AttachedTo != bearGY {
		t.Fatalf("window state changed: zone %s attachedTo %d; want battlefield attached to graveyard bear %d",
			o.Zone, o.AttachedTo, bearGY)
	}

	// The regression guard: an ordinary Aura whose bearer leaves the
	// battlefield is still swept (spec `Creature` matches a graveyard bear too
	// -- the exemption is zone-POSITIVE and must never fire for it).
	e.emit(events.Event{Kind: events.MoveZone, Obj: cubBF, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	if !e.attachmentSBAs() {
		t.Fatal("the ordinary Aura's illegal attachment was not swept after its bearer left the battlefield")
	}
	if o := e.G.Obj(usaID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Unholy Strength zone %s; want swept to the graveyard", o.Zone)
	}
	if o := e.G.Obj(adID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("the graveyard-window Aura must still survive alongside the swept ordinary Aura")
	}
}

// TestAnimateDeadDerivedEnchantDrivesTheSBA pins the B half and Done-3 on a raw
// board: with the post-DBAnimate layer grant registered exactly as the reanimate
// chain registers it, the CR 704.5m sweep honours the DERIVED enchant -- the
// stale graveyard enchant is gone from the derived list, the granted
// IsRemembered enchant is on it, and the attachment survives the checkpoint.
func TestAnimateDeadDerivedEnchantDrivesTheSBA(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	aura := mustCorpusCard(t, reg, "Animate Dead")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, _ := tokenReplGame(t, 9102, aura, bear)

	// The post-animate board is hand-built (stageRawEntry): its printed
	// graveyard enchant would not admit the battlefield bear at entry.
	adID := searchMoveByName(t, e, "Animate Dead", state.ZHand)
	stageRawEntry(t, e, adID)
	bearID := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	if adID == 0 || bearID == 0 {
		t.Fatalf("fixture setup failed: aura=%d bear=%d", adID, bearID)
	}
	e.emit(events.Event{Kind: events.Attach, Obj: adID, IDs: []state.ObjID{bearID}})
	// The post-DBAnimate state, set up by hand: the layer grant (remove the
	// stale enchant, grant the IsRemembered one -- the exact lists the card's
	// DB$ Animate carries) and the bear durably remembered (the persistent
	// half of RememberChanged$). IsRemembered reads the aura's persistent
	// list, so the remembered entry is a PRECONDITION of the survival
	// assertion: without it the fixed code sweeps too and the test fails.
	e.AddContinuous(state.ContinuousEffect{
		Source: adID, Affects: "Card.Self", Controller: 0,
		Layer:          state.LAbilities,
		AddKeywords:    []string{"Enchant:Creature.IsRemembered:creature put onto the battlefield with Animate Dead"},
		RemoveKeywords: []string{"Enchant:Creature.inZoneGraveyard:creature card in a graveyard"},
		Permanent:      true,
	})
	e.emit(events.Event{Kind: events.Choose, Obj: adID, Counter: "remembered", IDs: []state.ObjID{bearID}})
	e.pending = nil

	assertDerivedEnchantSwapped(t, e, adID)
	if e.attachmentSBAs() {
		t.Fatal("the post-animate raw board was swept; the SBA read the printed enchant instead of the derived one")
	}
	if o := e.G.Obj(adID); o == nil || o.Zone != state.ZBattlefield || o.AttachedTo != bearID {
		t.Fatalf("post-animate state: zone %s attachedTo %d; want battlefield attached to bear %d",
			o.Zone, o.AttachedTo, bearID)
	}
}
