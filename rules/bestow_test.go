package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Bestow (CR 702.114), pinned end to end on real corpus cards: the bestowed
// cast (the alternative "Aura spell with enchant creature" half) is offered
// beside the plain creature cast, pays the bestow cost instead of the mana
// cost, asks a creature target through the synthesized attach SA, resolves
// attached via a real events.Attach, and reverts to a creature when the
// attachment ends. The fixture style is rules/attached_trigger_test.go's
// (the ordinary Aura cast path these tests extend).

const bestowBearerSrc = "Name:Bestow Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n"

// bestowedCastOption returns the "cast" option with Mode "bestowed" for the
// given hand card.
func bestowedCastOption(t *testing.T, e *Engine, id state.ObjID) decision.Option {
	t.Helper()
	for _, o := range castOptions(t, e) {
		if o.Obj == id && o.Mode == "bestowed" {
			return o
		}
	}
	t.Fatalf("no bestowed cast option for card %d in %+v", id, castOptions(t, e))
	return decision.Option{}
}

// plainCastOption returns the ordinary (no-mode) cast option for the card.
func plainCastOption(t *testing.T, e *Engine, id state.ObjID) decision.Option {
	t.Helper()
	for _, o := range castOptions(t, e) {
		if o.Obj == id && o.Mode == "" {
			return o
		}
	}
	t.Fatalf("no plain cast option for card %d in %+v", id, castOptions(t, e))
	return decision.Option{}
}

// bestowCastOnto submits the bestowed cast option, answers the creature
// target ask with bearer, and drains the stack.
func bestowCastOnto(t *testing.T, e *Engine, opt decision.Option, bearer state.ObjID) {
	t.Helper()
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("bestowed cast target decision: %+v", d)
	}
	idx := indexOfObjOption(d, bearer)
	if idx < 0 {
		t.Fatalf("bestowed target ask does not offer the bearer %d: %+v", bearer, d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
}

// sawBestowedCastInfo reports whether the log carries a pay-time CastInfo
// for id whose flags include state.FlagBestowed.
func sawBestowedCastInfo(t *testing.T, e *Engine, id state.ObjID) bool {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == id && events.FlagsFrom(ev.Counter)&state.FlagBestowed != 0 {
			return true
		}
	}
	return false
}

// sawDetach reports whether the log carries the Unattached detach event for
// id. Its first ID preserves the former bearer for trigger referents.
func sawDetach(t *testing.T, e *Engine, id state.ObjID) bool {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind == events.Unattached && ev.Obj == id && len(ev.IDs) > 0 {
			return true
		}
	}
	return false
}

// TestCelestialArchonBestowedCastAttachesAuraToBearer is the filing card:
// the priority decision offers BOTH casts; the bestowed cast costs
// {5}{W}{W}, asks a creature target, and resolves the Archon attached to the
// chosen bearer via a real events.Attach. While attached the Archon is an
// Aura, not a creature (it cannot attack), and its "Enchanted creature gets
// +4/+4 and has flying and first strike" static is live on the bearer.
func TestCelestialArchonBestowedCastAttachesAuraToBearer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	archon := mustCorpusCard(t, reg, "Celestial Archon")
	e, cfg := tokenReplGame(t, 141, archon)
	archonID := moveSeededCard(t, e, 0, archon, state.ZHand)
	bear := putToken(t, e, 0, bestowBearerSrc, state.ZBattlefield)
	addMana(t, e, 0, "WWWWWWW") // {5}{W}{W} as seven white

	opts := castOptions(t, e)
	if o := plainCastOption(t, e, archonID); o.Label != "Cast Celestial Archon" {
		t.Fatalf("plain cast label %q", o.Label)
	}
	if o := bestowedCastOption(t, e, archonID); o.Label != "Cast Celestial Archon (bestowed)" {
		t.Fatalf("bestowed cast label %q", o.Label)
	}
	if len(opts) != 2 {
		t.Fatalf("cast options = %d, want exactly the plain and bestowed pair: %+v", len(opts), opts)
	}

	bestowCastOnto(t, e, bestowedCastOption(t, e, archonID), bear)

	o := e.G.Obj(archonID)
	if o.Zone != state.ZBattlefield || o.AttachedTo != bear {
		t.Fatalf("archon zone %s attached %d, want battlefield/bear %d", o.Zone, o.AttachedTo, bear)
	}
	if e.IsCreature(archonID) {
		t.Fatal("bestowed-attached Archon must not be a creature (CR 702.114e)")
	}
	if e.canAttack(archonID) {
		t.Fatal("a bestowed-attached Aura must not attack")
	}
	// The bestowed static is live on the bearer: +4/+4 and flying/first strike.
	if p := e.Power(bear); p != 5 || e.Toughness(bear) != 5 {
		t.Fatalf("bearer power %d tough %d, want 5/5 under the bestowed static", e.Power(bear), e.Toughness(bear))
	}
	if !e.HasKeyword(bear, "Flying") || !e.HasKeyword(bear, "First Strike") {
		t.Fatalf("bearer keywords missing flying/first strike (derived: %v)", e.Derived(bear).Keywords)
	}
	if !sawBestowedCastInfo(t, e, archonID) {
		t.Fatal("no FlagBestowed CastInfo event on the bestowed cast")
	}
	replayCheck(t, e, cfg)
}

// TestCelestialArchonBearerLeavesBecomesCreatureAgain: CR 702.114b's detach
// half -- the bearer dies, the Archon DETACHES (events.Unattached naming the
// former bearer) and STAYS on the battlefield as a 4/4 white Archon creature again, never
// taking the "Aura attached to nothing" graveyard arm.
func TestCelestialArchonBearerLeavesBecomesCreatureAgain(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	archon := mustCorpusCard(t, reg, "Celestial Archon")
	e, cfg := tokenReplGame(t, 142, archon)
	archonID := moveSeededCard(t, e, 0, archon, state.ZHand)
	bear := putToken(t, e, 0, bestowBearerSrc, state.ZBattlefield)
	addMana(t, e, 0, "WWWWWWW")
	bestowCastOnto(t, e, bestowedCastOption(t, e, archonID), bear)
	if e.G.Obj(archonID).AttachedTo != bear {
		t.Fatalf("setup: archon attached to %d", e.G.Obj(archonID).AttachedTo)
	}

	e.emit(events.Sacrifice(bear))
	e.checkStateBased()

	o := e.G.Obj(archonID)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("archon zone %s, want battlefield (CR 702.114b: it becomes a creature again)", o.Zone)
	}
	if o.AttachedTo != 0 {
		t.Fatalf("archon still attached to %d, want detached", o.AttachedTo)
	}
	if !e.IsCreature(archonID) {
		t.Fatal("detached Archon must be a creature again")
	}
	if p := e.Power(archonID); p != 4 || e.Toughness(archonID) != 4 {
		t.Fatalf("detached Archon power %d tough %d, want the printed 4/4", e.Power(archonID), e.Toughness(archonID))
	}
	if !sawDetach(t, e, archonID) {
		t.Fatal("no detach Attach (no IDs) event for the bestowed card")
	}
	replayCheck(t, e, cfg)
}

// TestCelestialArchonPlainCastUnchanged: the ordinary creature cast of the
// same card still works with no target ask, enters unattached as a 4/4
// creature, and emits no bestowed provenance.
func TestCelestialArchonPlainCastUnchanged(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	archon := mustCorpusCard(t, reg, "Celestial Archon")
	e, cfg := tokenReplGame(t, 143, archon)
	archonID := moveSeededCard(t, e, 0, archon, state.ZHand)
	addMana(t, e, 0, "WWWWW") // {3}{W}{W} as five white

	opt := plainCastOption(t, e, archonID)
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("plain creature cast must not ask a target: %+v", d)
	}
	passUntilStackEmpty(t, e, 20)

	o := e.G.Obj(archonID)
	if o.Zone != state.ZBattlefield || o.AttachedTo != 0 {
		t.Fatalf("plain-cast archon zone %s attached %d, want battlefield/0", o.Zone, o.AttachedTo)
	}
	if !e.IsCreature(archonID) || e.Power(archonID) != 4 || e.Toughness(archonID) != 4 {
		t.Fatalf("plain-cast archon not a 4/4 creature (power %d tough %d)", e.Power(archonID), e.Toughness(archonID))
	}
	if sawBestowedCastInfo(t, e, archonID) {
		t.Fatal("plain cast must not carry FlagBestowed")
	}
	replayCheck(t, e, cfg)
}

// TestEidolonOfCountlessBattlesBestowTransition: the shared +X/+X static
// crosses the transition. While bestowed, the Eidolon itself is an Aura (so
// its Count$Valid Creature.YouCtrl,Aura.YouCtrl counts itself once as an
// Aura) and pumps the bearer; after the bearer leaves and the Eidolon
// detaches, the same count reads it as a creature pumping itself.
func TestEidolonOfCountlessBattlesBestowTransition(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	eidolon := mustCorpusCard(t, reg, "Eidolon of Countless Battles")
	e, cfg := tokenReplGame(t, 144, eidolon)
	eidID := moveSeededCard(t, e, 0, eidolon, state.ZHand)
	bear := putToken(t, e, 0, bestowBearerSrc, state.ZBattlefield)
	addMana(t, e, 0, "WWWW") // {2}{W}{W} as four white

	bestowCastOnto(t, e, bestowedCastOption(t, e, eidID), bear)

	// While bestowed-attached: creatures you control = the bear (1), Auras
	// you control = the bestowed Eidolon itself (1), X = 2. The bearer is
	// 1/1 + 2/+2 and the bestowed Aura itself rides Affected$ Card.Self to
	// 0/0 + 2/+2.
	if e.G.Obj(eidID).AttachedTo != bear {
		t.Fatalf("eidolon attached to %d, want bear %d", e.G.Obj(eidID).AttachedTo, bear)
	}
	if p := e.Power(bear); p != 3 || e.Toughness(bear) != 3 {
		t.Fatalf("bestowed bearer power %d tough %d, want 3/3 (X=2: bear + bestowed Eidolon)", e.Power(bear), e.Toughness(bear))
	}
	if p := e.Power(eidID); p != 2 || e.Toughness(eidID) != 2 {
		t.Fatalf("bestowed Eidolon power %d tough %d, want 2/2 (its own Card.Self arm, X=2)", e.Power(eidID), e.Toughness(eidID))
	}
	if e.IsCreature(eidID) {
		t.Fatal("bestowed-attached Eidolon must not be a creature (so it is not counted in Creature.YouCtrl)")
	}

	e.emit(events.Sacrifice(bear))
	e.checkStateBased()

	// After the unattach: creatures you control = the Eidolon (1), Auras = 0,
	// X = 1 -- the same static now pumps the unattached Eidolon itself.
	o := e.G.Obj(eidID)
	if o.Zone != state.ZBattlefield || o.AttachedTo != 0 {
		t.Fatalf("eidolon zone %s attached %d, want battlefield/0 after the bearer left", o.Zone, o.AttachedTo)
	}
	if !e.IsCreature(eidID) {
		t.Fatal("detached Eidolon must be a creature again")
	}
	if p := e.Power(eidID); p != 1 || e.Toughness(eidID) != 1 {
		t.Fatalf("detached Eidolon power %d tough %d, want 1/1 (X=1: only itself is a creature now)", e.Power(eidID), e.Toughness(eidID))
	}
	replayCheck(t, e, cfg)
}

// TestBestowExoticCostsAreOfferedAndPaid and the CR 702.114c pin live in
// rules/bestow_exotic_test.go: the three exotic bestow costs (Nyxborn Hydra's
// {X}, Detective's Phoenix's CollectEvidence<6>, Hypnotic Siren's
// colon-suffixed line) are now priced and offered rather than withheld.
