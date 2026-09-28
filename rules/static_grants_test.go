package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// The static-grant family (task inbox-paramcensus-static-grant-misc): one
// leaf per Continuous grant sub-kind the parameter census named, each on a
// real corpus card driven through a real engine. The census labels these
// tests retire: Continuous.CharacteristicDefining (Master of Etherium),
// Continuous.AddStaticAbility (Exploration Broodship), Continuous.AddTrigger
// (Hearthhull, the Worldseed), Continuous.AddSVar (Sword of Fire and Ice),
// Continuous.MayLookAt (Oracle of Mul Daya).

// TestMasterOfEtheriumCDAIsItsArtifactCount is Continuous.Characteristic-
// Defining's leaf: the P/T-setting CDA sets the layer-7a base in EVERY zone
// (CR 604.3/208.2, CR 613.4a) -- the battlefield-only static scan can never
// express that, so cdaSetPT reads it directly off the face in derivedScalar.
// The value is the Count$Valid SVar, priced by the evaluator, NOT the
// statInt("X") zero the old emission degraded to.
func TestMasterOfEtheriumCDAIsItsArtifactCount(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	moe := lookup(t, reg, "Master of Etherium")
	thopter := lookup(t, reg, "Ornithopter")
	sol := lookup(t, reg, "Sol Ring")
	e := corpusEngine(t, reg, []*cards.Card{moe, thopter, sol}, nil)
	moeID := moveByName(t, e, 0, "Master of Etherium", state.ZBattlefield)
	thopterID := moveByName(t, e, 0, "Ornithopter", state.ZBattlefield)
	moveByName(t, e, 0, "Sol Ring", state.ZBattlefield)
	// Three artifacts you control (the CDA's count includes the card itself);
	// the +1/+1 the OTHER static grants reaches Ornithopter (another artifact
	// creature), never the CDA itself.
	if got := e.Power(moeID); got != 3 || e.Toughness(moeID) != 3 {
		t.Fatalf("Master of Etherium on the battlefield = %d/%d, want 3/3", e.Power(moeID), e.Toughness(moeID))
	}
	if got := e.Power(thopterID); got != 1 || e.Toughness(thopterID) != 3 {
		t.Fatalf("Ornithopter = %d/%d, want 1/3 (the other-artifact-creature buff)", e.Power(thopterID), e.Toughness(thopterID))
	}
	// All zones: in the graveyard the CDA still sets P/T to the artifacts you
	// control -- now two (the battlefield ones; a graveyard card controls
	// nothing).
	e.emit(events.Event{Kind: events.MoveZone, Obj: moeID, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.Power(moeID); got != 2 || e.Toughness(moeID) != 2 {
		t.Fatalf("Master of Etherium in the graveyard = %d/%d, want 2/2 (CDA applies in every zone)", e.Power(moeID), e.Toughness(moeID))
	}
}

// TestExplorationBroodshipGrantsItsStationStatic is Continuous.AddStatic-
// Ability's leaf: the STATION 3+ static's AddStaticAbility$ body is itself a
// Mode$ Continuous static (an AdjustLandPlays$ 1 to You), granted for
// exactly as long as the outer static's Affected$ matches the host. Below
// three charge counters the grant is not live.
func TestExplorationBroodshipGrantsItsStationStatic(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	brood := lookup(t, reg, "Exploration Broodship")
	beast := card(t, "Name:BigBear\nManaCost:2 G\nTypes:Creature Bear\nPT:3/3\nOracle:x\n")
	e := corpusEngine(t, reg, []*cards.Card{brood, beast}, nil)
	broodID := moveByName(t, e, 0, "Exploration Broodship", state.ZBattlefield)
	beastID := moveByName(t, e, 0, "BigBear", state.ZBattlefield)
	if got := e.adjustLandPlays(0); got != 0 {
		t.Fatalf("adjustLandPlays before STATION 3+ = %d, want 0 (no grant below the counters)", got)
	}
	stationTo(t, e, broodID, beastID)
	if got := e.G.Obj(broodID).Counter("CHARGE"); got != 3 {
		t.Fatalf("the spacecraft has %d charge counters, want 3", got)
	}
	// The granted static is live: one additional land drop for its
	// controller, and the ordinary grant emission the printed AdjustLandPlays
	// statics share produced it.
	if got := e.adjustLandPlays(0); got != 1 {
		t.Fatalf("adjustLandPlays at STATION 3+ = %d, want 1 (the granted AdjustLandPlays$ 1)", got)
	}
	if got := e.adjustLandPlays(1); got != 0 {
		t.Fatalf("adjustLandPlays for the opponent = %d, want 0", got)
	}
}

// TestHearthhullGrantSacrificeTriggerFires is Continuous.AddTrigger's leaf:
// the STATION 8+ static's AddTrigger$ body is a T:-shaped trigger, granted
// to the spacecraft itself; sacrificing a land queues it (a GrantTriggerPush
// whose stack ability is the Execute$ SVar body) and it resolves, costing
// each opponent 2 life.
func TestHearthhullGrantSacrificeTriggerFires(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	hh := lookup(t, reg, "Hearthhull, the Worldseed")
	beast := card(t, "Name:BigBeast\nManaCost:4 G\nTypes:Creature Beast\nPT:8/8\nOracle:x\n")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{hh, beast}, nil)
	hhID := moveByName(t, e, 0, "Hearthhull, the Worldseed", state.ZBattlefield)
	beastID := moveByName(t, e, 0, "BigBeast", state.ZBattlefield)
	stationTo(t, e, hhID, beastID)
	if got := e.G.Obj(hhID).Counter("CHARGE"); got != 8 {
		t.Fatalf("the spacecraft has %d charge counters, want 8", got)
	}
	if !e.HasKeyword(hhID, "Flying") {
		t.Fatal("the STATION 8+ AddKeyword$ grant is not live")
	}
	// The granted trigger, not a printed one: it queues off a sacrifice.
	land := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	e.emit(events.Sacrifice(land))
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.GrantTriggerPush }); n != 1 {
		t.Fatalf("the granted trigger pushed %d GrantTriggerPush event(s), want 1", n)
	}
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("opponent life = %d, want 18 (the granted 'each opponent loses 2 life')", got)
	}
	replayCheck(t, e, cfg)
}

// TestVerdantEmbraceGrantTriggerFires is the CROSS-OBJECT AddTrigger$ leaf:
// Verdant Embrace's "Enchanted creature ... has 'At the beginning of each
// upkeep, create a 1/1 green Saproling creature token'" carries the Execute$
// SVar (VerdantToken) on the AURA's face while the Affected$ is the BEARER,
// so the grant only fires if the queue gate and events.Apply resolve the body
// from the GRANTOR (the Aura) and not the affected object. The Aura's own id
// rides the event's Amount; the minted stack object's Source stays the bearer.
func TestVerdantEmbraceGrantTriggerFires(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	embrace := lookup(t, reg, "Verdant Embrace")
	bears := lookup(t, reg, "Grizzly Bears")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{embrace, bears}, nil)
	bearID := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	auraID := attachCorpusAura(t, e, 0, embrace, bearID)
	// The grant is live on the bearer (the +3/+3 half pins the Affected$
	// match; the trigger half is what this leaf is about).
	if got := e.Derived(bearID).Toughness; got != 5 {
		t.Fatalf("bearer toughness = %d, want 5 (the +3/+3 grant)", got)
	}
	// Drive to the next upkeep: the granted "at the beginning of each
	// upkeep" trigger fires for the bearer's controller. Stop the instant the
	// grant event lands so the stack object it minted is still on the stack.
	grant := driveToGrantTrigger(t, e)
	if got := state.ObjID(grant.Amount); got != auraID {
		t.Fatalf("grantor Amount = %d, want the Aura %d", got, auraID)
	}
	if grant.Obj != bearID {
		t.Fatalf("grant recipient Obj = %d, want the bearer %d", grant.Obj, bearID)
	}
	// The minted stack object's Source is the RECIPIENT, never the grantor.
	var minted *state.Object
	for _, id := range e.G.Stack {
		if o := e.G.Obj(id); o != nil && o.Ability != nil && o.Ability.Line != "" {
			minted = o
		}
	}
	if minted == nil {
		t.Fatal("the granted trigger minted no stack object")
	}
	if minted.Source != bearID {
		t.Fatalf("minted Source = %d, want the bearer %d", minted.Source, bearID)
	}
	// Resolve the trigger: one Saproling token under the bearer's controller.
	passUntilStackEmpty(t, e, 30)
	if got := countTokensNamedOnSeat(t, e, 0, "Saproling Token"); got != 1 {
		t.Fatalf("granted trigger made %d Saproling Token(s), want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestVerdantEmbraceGrantEndsWhenAuraLeaves is the negative half: once the
// Aura has left the battlefield its static no longer grants the trigger, so
// the next upkeep produces neither a GrantTriggerPush nor a token.
func TestVerdantEmbraceGrantEndsWhenAuraLeaves(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	embrace := lookup(t, reg, "Verdant Embrace")
	bears := lookup(t, reg, "Grizzly Bears")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{embrace, bears}, nil)
	bearID := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	auraID := attachCorpusAura(t, e, 0, embrace, bearID)
	// The Aura leaves before any upkeep: its static stops matching the bearer.
	e.emit(events.Event{Kind: events.MoveZone, Obj: auraID, From: state.ZBattlefield, To: state.ZGraveyard})
	drivePastNextUpkeep(t, e)
	passUntilStackEmpty(t, e, 30)
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.GrantTriggerPush }); n != 0 {
		t.Fatalf("a departed Aura still pushed %d GrantTriggerPush event(s), want 0", n)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Saproling Token"); got != 0 {
		t.Fatalf("a departed Aura still made %d Saproling Token(s), want 0", got)
	}
	replayCheck(t, e, cfg)
}

// drivePastNextUpkeep answers decisions (pass for priority, first option
// otherwise) until the next turn's Main1 -- far enough past the upcoming
// upkeep that a Verdant Embrace-style "at the beginning of each upkeep" grant
// has fired. The upkeep step itself is never an observable stop (beginTurn
// runs untap, upkeep and the opening priority inside one emit chain), so the
// stop is Main1, the first resting point after it.
func drivePastNextUpkeep(t *testing.T, e *Engine) {
	t.Helper()
	turn := e.G.Turn
	for i := 0; i < 4000; i++ {
		if e.G.Over {
			t.Fatalf("game ended before reaching Main1 after turn %d", turn)
		}
		if e.G.Turn > turn && e.G.Step == state.StepMain1 {
			return
		}
		d := e.Pending()
		if d == nil {
			// The setup emits (a MoveZone/Attach) consume the pending the
			// fixture left; a fresh priority round is what reintroduces one.
			e.priorityRound()
			continue
		}
		if d.Kind == decision.KPriority {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("submit pass: %v", err)
					}
					break
				}
			}
			continue
		}
		if len(d.Options) == 0 {
			t.Fatalf("empty non-priority decision %+v while driving to upkeep", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	t.Fatalf("did not reach Main1 after turn %d", turn)
}

// driveToGrantTrigger answers decisions until a GrantTriggerPush event
// appears in the log, then returns it -- the minted stack object is still on
// the stack at that instant (a granted trigger with no targets waits for
// priority before resolving). Bounded like every other driver here.
func driveToGrantTrigger(t *testing.T, e *Engine) events.Event {
	t.Helper()
	turn := e.G.Turn
	for i := 0; i < 4000; i++ {
		if e.G.Over {
			t.Fatalf("game ended before a GrantTriggerPush after turn %d", turn)
		}
		for _, ev := range e.L.Events {
			if ev.Kind == events.GrantTriggerPush {
				return ev
			}
		}
		d := e.Pending()
		if d == nil {
			e.priorityRound()
			continue
		}
		if d.Kind == decision.KPriority {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("submit pass: %v", err)
					}
					break
				}
			}
			continue
		}
		if len(d.Options) == 0 {
			t.Fatalf("empty non-priority decision %+v while driving to a grant", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	t.Fatalf("no GrantTriggerPush after turn %d", turn)
	return events.Event{}
}

// TestSwordOfFireAndIceGrantsSVarToEquipped is Continuous.AddSVar's leaf:
// the AddSVar$ value is Forge's "SVar:<Name>:<Value>" grant shape; the
// equipped creature carries the granted variable while equipped and carries
// nothing once the Equipment detaches. The corpus's granted SVars are
// AI-evaluation hints, so the engine-side surface is Engine.GrantedSVar.
func TestSwordOfFireAndIceGrantsSVarToEquipped(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sword := lookup(t, reg, "Sword of Fire and Ice")
	bears := lookup(t, reg, "Grizzly Bears")
	e := corpusEngine(t, reg, []*cards.Card{sword, bears}, nil)
	sw := moveByName(t, e, 0, "Sword of Fire and Ice", state.ZBattlefield)
	bearID := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if _, ok := e.GrantedSVar(bearID, "MustBeBlocked"); ok {
		t.Fatal("the unequipped creature already carries the granted SVar")
	}
	addMana(t, e, 0, "RR")
	e.Advance()
	submitChoices(t, e, abilityOption(t, e, sw, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || len(d.Options) == 0 || d.Options[0].Obj != bearID {
		t.Fatalf("equip target %+v", d)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(sw).AttachedTo != bearID {
		t.Fatalf("sword attached to %d, want %d", e.G.Obj(sw).AttachedTo, bearID)
	}
	v, ok := e.GrantedSVar(bearID, "MustBeBlocked")
	if !ok || v != "AttackingPlayerConservative" {
		t.Fatalf("granted SVar = (%q, %t), want (AttackingPlayerConservative, true)", v, ok)
	}
	// The grant follows the attachment: detach and it is gone.
	e.emit(events.Event{Kind: events.Attach, Obj: sw, IDs: []state.ObjID{}})
	e.checkStateBased()
	if _, ok := e.GrantedSVar(bearID, "MustBeBlocked"); ok {
		t.Fatal("the detached creature still carries the granted SVar")
	}
}

// TestOracleOfMulDayaRevealsTopCard is Continuous.MayLookAt's leaf: the
// grant's controller may look at the top card of their own library -- the
// seat's own projection carries LibraryTop, every other seat's carries
// nothing (CR 400.2).
func TestOracleOfMulDayaRevealsTopCard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	oracle := lookup(t, reg, "Oracle of Mul Daya")
	e := corpusEngine(t, reg, []*cards.Card{oracle}, nil)
	if e.MayLookAtLibraryTop(0) {
		t.Fatal("the top card is revealed before the Oracle is on the battlefield")
	}
	moveByName(t, e, 0, "Oracle of Mul Daya", state.ZBattlefield)
	if !e.MayLookAtLibraryTop(0) {
		t.Fatal("Oracle of Mul Daya does not reveal the top card to its controller")
	}
	if e.MayLookAtLibraryTop(1) {
		t.Fatal("the grant leaked to a seat it does not affect")
	}
	top := e.G.Zone(state.ZLibrary, 0)[0]
	own := view.Project(e.G, e, 0, e.Pending())
	if own.Players[0].LibraryTop == nil || own.Players[0].LibraryTop.ID != top {
		t.Fatalf("seat 0's view carries no revealed top card (want %d): %+v", top, own.Players[0].LibraryTop)
	}
	other := view.Project(e.G, e, 1, e.Pending())
	if other.Players[0].LibraryTop != nil {
		t.Fatalf("seat 1's view leaks seat 0's revealed top card: %+v", other.Players[0].LibraryTop)
	}
}

// TestNumericSetMaxHandSize pins the numeric SetMaxHandSize$ shape on the
// read maxHandSizeFor already implements (Reliquary Tower's Unlimited landed
// first): five, not the seven-card default. Necrodominance carries the
// printed card shape.
func TestNumericSetMaxHandSize(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, "Name:Necrodominance\nManaCost:B B B\nTypes:Legendary Enchantment\n"+
		"S:Mode$ Continuous | Affected$ You | SetMaxHandSize$ 5 | Description$ Your maximum hand size is five.\nOracle:x\n")
	if got := e.maxHandSizeFor(0); got != 5 {
		t.Fatalf("maxHandSizeFor under SetMaxHandSize$ 5 = %d, want 5", got)
	}
	if got := e.maxHandSizeFor(1); got != 7 {
		t.Fatalf("maxHandSizeFor for the unaffected seat = %d, want the 7 default", got)
	}
}

// TestReliquaryTowerNoCleanupDiscard is the brief's end-to-end leaf for
// S:Mode$ Continuous | Affected$ You | SetMaxHandSize$ Unlimited: with the
// real corpus Reliquary Tower on seat 0's battlefield, the CR 514.1 cleanup
// step must ask NOTHING even though the hand is well over seven, and the
// hand must be unchanged. The sibling control (no Tower) asks the ordinary
// discard, so a regression that made cleanupStep always skip (or always ask)
// fails one half or the other rather than passing vacuously.
func TestReliquaryTowerNoCleanupDiscard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Reliquary Tower"))
	e.G.Active = 0
	e.G.Step = state.StepCleanup

	for i := 0; i < 3; i++ {
		onHand(t, e, 0, "Name:Moor\nManaCost:1 G\nTypes:Land\nOracle:x\n")
	}
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) != 10 {
		t.Fatalf("precondition: hand = %d, want 10 (opening 7 + 3) for this test", len(hand))
	}
	if got := e.maxHandSizeFor(0); got != unlimitedHandSize {
		t.Fatalf("precondition: Reliquary Tower's effective maximum = %d, want unlimited (%d)", got, unlimitedHandSize)
	}

	e.priorityRound()
	if d := e.Pending(); d != nil {
		t.Fatalf("Reliquary Tower's controller was asked to discard at cleanup: %+v", d)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 10 {
		t.Fatalf("hand changed under Reliquary Tower: %d, want 10", got)
	}
}

// TestCleanupStillAsksWithoutReliquaryTower is the negative control for
// TestReliquaryTowerNoCleanupDiscard: the same 10-card hand with no
// no-maximum effect is asked to discard down to seven. Without this control
// the Tower leaf could pass because cleanup stopped asking altogether.
func TestCleanupStillAsksWithoutReliquaryTower(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Active = 0
	e.G.Step = state.StepCleanup
	for i := 0; i < 3; i++ {
		onHand(t, e, 0, "Name:Moor\nManaCost:1 G\nTypes:Land\nOracle:x\n")
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 10 {
		t.Fatalf("precondition: hand = %d, want 10 for this test", got)
	}
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 3 || d.Max != 3 {
		t.Fatalf("control: want a discard-3 decision, got %+v", d)
	}
}

// TestEffectDeliveredSetMaxHandSizeRead is the rules-side read of the
// Effect-delivered route maxHandSizeFor now consults: a registered
// continuous effect carrying SetMaxHandSize (the shape effEffect stores for
// Finale of Revelation's STHandSize) must set the effective maximum, and the
// cleanup gate must then ask nothing for an oversized hand. The printed
// Reliquary Tower read is the other half, pinned by
// TestReliquaryTowerNoCleanupDiscard; this leaf proves the registered route
// reaches the same gate (a fix that only widened activeStatics would leave
// this red).
func TestEffectDeliveredSetMaxHandSizeRead(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Finale of Revelation\nManaCost:X U U\nTypes:Sorcery\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{
		Source: src, Controller: 0, Affects: "You", SetMaxHandSize: "Unlimited",
	})
	if got := e.maxHandSizeFor(0); got != unlimitedHandSize {
		t.Fatalf("Effect-delivered maximum = %d, want unlimited (%d)", got, unlimitedHandSize)
	}
	if got := e.maxHandSizeFor(1); got != maxHandSize {
		t.Fatalf("unaffected seat = %d, want the %d default", got, maxHandSize)
	}

	// End-to-end: the cleanup gate consults the registered effect, so a
	// seat-0 hand over seven is asked nothing.
	e.G.Active = 0
	e.G.Step = state.StepCleanup
	for i := 0; i < 3; i++ {
		onHand(t, e, 0, "Name:Moor\nManaCost:1 G\nTypes:Land\nOracle:x\n")
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 10 {
		t.Fatalf("precondition: hand = %d, want 10 for this test", got)
	}
	e.priorityRound()
	if d := e.Pending(); d != nil {
		t.Fatalf("Effect-delivered no-maximum still asked a discard: %+v", d)
	}
}

// TestHandSizeValueGrammarIsShared pins the one grammar both registration
// routes consult: rules' printed read (maxHandSizeFor) and effects'
// Effect-delivery whitelist (setMaxHandSizeGrantFromLine) must agree on
// Unlimited, a numeric value, and the dynamic values both refuse. The two
// Unlimited constants are separate (neither package may import the other's)
// so this is also the assertion that keeps them equal.
func TestHandSizeValueGrammarIsShared(t *testing.T) {
	t.Parallel()
	if unlimitedHandSize != effects.UnlimitedHandSize {
		t.Fatalf("unlimited constants diverged: rules %d, effects %d", unlimitedHandSize, effects.UnlimitedHandSize)
	}
	for _, tc := range []struct {
		raw  string
		want int
		ok   bool
	}{
		{"Unlimited", unlimitedHandSize, true},
		{"unlimited", unlimitedHandSize, true},
		{"5", 5, true},
		{"0", 0, true},
		{"X", 0, false},
		{"Y", 0, false},
		{"", 0, false},
		{"-1", 0, false},
	} {
		got, ok := effects.HandSizeValueOK(tc.raw)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Fatalf("HandSizeValueOK(%q) = (%d, %v), want (%d, %v)", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

// stationTo is the kw:Station flow the mass-primitives leaves use: the
// sorcery-window station option for craft, answered with the tap pick over
// bigID, leaving its power on craft as charge counters.
func stationTo(t *testing.T, e *Engine, craft, bigID state.ObjID) {
	t.Helper()
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision (got %+v)", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "station" && o.Obj == craft {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no station option for %d: %+v", craft, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit station: %v", err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no tap pick after the station option (got %+v)", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == bigID {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("the station tap pick does not offer %d: %+v", bigID, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
		t.Fatalf("submit tap pick: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
}
