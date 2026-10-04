package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCloneResumeChainIndependenceKernel clones an engine while a nested
// mid-resolution ask is posed (a Charm's Repeat whose repeated Discard asks,
// with continuations Inn -1, Mid -2, Out -3 behind it) and answers the same
// pending decision on both engines, the clone first: each must run every
// continuation exactly once and both must land on the same chain head.
func TestCloneResumeChainIndependenceKernel(t *testing.T) {
	charm := "Name:PiNCLONE\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ Charm | Choices$ DoRepeat,DoGain | SubAbility$ Out\n" +
		"SVar:DoRepeat:SP$ Repeat | RepeatSubAbility$ DoDiscard | RepeatNum$ 1 | SubAbility$ Mid\n" +
		"SVar:DoDiscard:DB$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 1 | SubAbility$ Inn\n" +
		"SVar:DoGain:DB$ GainLife | Defined$ You | LifeAmount$ 5\n" +
		"SVar:Out:DB$ LoseLife | Defined$ You | LifeAmount$ 3\n" +
		"SVar:Mid:DB$ LoseLife | Defined$ You | LifeAmount$ 2\n" +
		"SVar:Inn:DB$ LoseLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 93, charm)
	addMana(t, e, 0, "R")
	life := e.G.Players[0].Life
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the Charm mode KModes ask, got %+v", d)
	}
	submitChoices(t, e, 0)
	passUntilNonPriority(t, e, 20)
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the Discard's KModes ask, got %+v", d)
	}
	c := e.Clone()
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}
	if err := c.Submit(in); err != nil {
		t.Fatalf("clone rejected the pending nested answer: %v", err)
	}
	passUntilStackEmpty(t, c, 20)
	if got := countLoseLife(c, 0); got != 3 || c.G.Players[0].Life != life-6 {
		t.Fatalf("clone lose-life events = %d life %d, want 3 and %d", got, c.G.Players[0].Life, life-6)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("original rejected the pending nested answer: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	if got := countLoseLife(e, 0); got != 3 || e.G.Players[0].Life != life-6 {
		t.Fatalf("original lose-life events = %d life %d, want 3 and %d", got, e.G.Players[0].Life, life-6)
	}
	if got, want := e.L.Head(), c.L.Head(); got != want {
		t.Fatalf("chain heads differ after the same nested answer: %s vs %s", got, want)
	}
}

// TestClonePolicyHoldsOnAMidGameEngineKernel checks the clone policy tags
// against what Clone actually does, on a real mid-game engine whose
// otherwise-empty fields are filled by reflection (see clonePolicyChecker):
// deep fields equal and own their storage, shared fields are dropped or
// shared, reset fields never alias, hook fields are zero -- on Clone and on
// CloneInto with a recycled Spare.
func TestClonePolicyHoldsOnAMidGameEngineKernel(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 3, Names: names, Decks: decks})
	e.Advance()
	drive(t, e, newTestBot(3), 30)
	seedInternalQueues(t, e)
	cloneFillPolicyFields(reflect.ValueOf(e).Elem(), engineType, 0)
	e.staticVersion, e.atkOffersVer = e.continuousVersion, e.continuousVersion
	e.ManaAbilityHook = func(state.PlayerID, state.ObjID, *cards.SA) {}

	k := &clonePolicyChecker{t: t, zeroSpare: true, nonNilRef: map[string]bool{}}
	c := e.Clone()
	k.check("Engine.", reflect.ValueOf(e).Elem(), reflect.ValueOf(c).Elem(), engineType, 0)

	sp := e.Clone().Release()
	k2 := &clonePolicyChecker{t: t, nonNilRef: map[string]bool{}}
	c2 := e.CloneInto(&sp)
	k2.check("Engine(CloneInto).", reflect.ValueOf(e).Elem(), reflect.ValueOf(c2).Elem(), engineType, 0)
	for _, p := range []string{"deep", "share", "reset", "hook"} {
		if !k.nonNilRef[p] {
			t.Errorf("the filled fixture holds no non-nil %s reference field: the fill did not run", p)
		}
	}
}

// TestCloneOwnsTheSuspendedResolutionsFlipMemoryKernel clones Flip Ask Flip
// at its mid-resolution ChoosePlayer ask (between two flips into one shared
// tally) and drives the clone first: the original then still reaches the
// same chain head as an uncloned reference, so the clone's post-ask flip
// never wrote into the original's memory.
func TestCloneOwnsTheSuspendedResolutionsFlipMemoryKernel(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	asker := card(t, flipAskFlipSrc)
	exercised := false
	for seed := uint64(1); seed <= 12; seed++ {
		ref := flipAskFlipAtAsk(t, reg, asker, seed)
		cloneMemDrain(t, ref, 100)

		e := flipAskFlipAtAsk(t, reg, asker, seed)
		c := e.Clone()
		cloneMemDrain(t, c, 100)
		cloneMemDrain(t, e, 100)
		if e.L.Head() != ref.L.Head() || c.L.Head() != ref.L.Head() {
			t.Fatalf("seed %d: heads original %s clone %s uncloned reference %s", seed, e.L.Head(), c.L.Head(), ref.L.Head())
		}
		if e.G.Players[0].Life != ref.G.Players[0].Life || c.G.Players[0].Life != ref.G.Players[0].Life {
			t.Fatalf("seed %d: life original %d clone %d reference %d", seed, e.G.Players[0].Life, c.G.Players[0].Life, ref.G.Players[0].Life)
		}
		if n := flipNotes(ref); len(n) == 2 && n[1] {
			exercised = true
		}
	}
	if !exercised {
		t.Fatal("precondition: no seed's post-ask flip was a win, so the shared tally was never written")
	}
}

// TestCloneOwnsTheSuspendedExchangeLifeMemoryKernel clones Mister Negative's
// exchange at the Lich-replaced gain's dredge ask: the clone's rider draws 5
// (its transaction and rider share one memory), and the original afterwards
// still matches an uncloned reference.
func TestCloneOwnsTheSuspendedExchangeLifeMemoryKernel(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	ref := misterNegativeAtDredge(t, reg)
	hand0 := len(ref.G.Zone(state.ZHand, 0))
	cloneMemDrain(t, ref, 200)
	if got := len(ref.G.Zone(state.ZHand, 0)) - hand0; got != 5 {
		t.Fatalf("precondition: the uncloned rider drew %d, want 5", got)
	}
	e := misterNegativeAtDredge(t, reg)
	c := e.Clone()
	cHand0 := len(c.G.Zone(state.ZHand, 0))
	cloneMemDrain(t, c, 200)
	if got := len(c.G.Zone(state.ZHand, 0)) - cHand0; got != 5 {
		t.Fatalf("the clone's rider drew %d, want 5", got)
	}
	eHand0 := len(e.G.Zone(state.ZHand, 0))
	cloneMemDrain(t, e, 200)
	if got := len(e.G.Zone(state.ZHand, 0)) - eHand0; got != 5 {
		t.Fatalf("the original's rider drew %d after the clone settled, want 5", got)
	}
	if e.L.Head() != ref.L.Head() || c.L.Head() != ref.L.Head() {
		t.Fatalf("heads original %s clone %s uncloned reference %s", e.L.Head(), c.L.Head(), ref.L.Head())
	}
}

// TestVesuvanShapeshifterCloneEndsOnTurnFaceDownKernel: Vesuvan
// Shapeshifter's DBCopy (Duration$ UntilFacedown) copies the picked Bear, and
// resolving its own VesShapeTurn (SetState TurnFaceDown) ends the copy.
func TestVesuvanShapeshifterCloneEndsOnTurnFaceDownKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Vesuvan Shapeshifter", "Grizzly Bears")
	mimic := searchMoveByName(t, e, "Vesuvan Shapeshifter", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	m := e.G.Obj(mimic)
	svars := m.Card.Faces[0].SVars
	sa := cards.ResolveSVar(svars, "DBCopy")
	turnDown := cards.ResolveSVar(svars, "VesShapeTurn")
	if sa == nil || sa.API != "Clone" || sa.Params["Duration"] != "UntilFacedown" || turnDown == nil || turnDown.API != "SetState" || turnDown.Params["Mode"] != "TurnFaceDown" {
		t.Fatalf("Vesuvan copy/turn-down bodies missing: %+v / %+v", sa, turnDown)
	}
	kr4Resolve(e, func() *effects.Ctx { return &effects.Ctx{Source: mimic, Controller: 0, SVars: svars} }, sa)
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		if !kr4Offers(d, bear) {
			t.Fatalf("the clone pick does not offer the Bear: %+v", d)
		}
		submitChoices(t, e, kr4Option(t, d, bear))
	}
	if o := e.G.Obj(mimic); o.Face().Name != "Grizzly Bears" || o.FaceDown {
		t.Fatalf("live face-up copy missing before expiry: %q facedown %v", o.Face().Name, o.FaceDown)
	}
	kr4Resolve(e, func() *effects.Ctx { return &effects.Ctx{Source: mimic, Controller: 0, SVars: svars} }, turnDown)
	if o := e.G.Obj(mimic); !o.FaceDown || o.CopyFace != nil || o.Face().Name != "Vesuvan Shapeshifter" {
		t.Fatalf("turn-down did not clear copy: facedown %v copyface %v name %q", o.FaceDown, o.CopyFace != nil, o.Face().Name)
	}
	replayCheck(t, e, cfg)
}
