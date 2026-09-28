package rules

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The "whenever CARDNAME becomes unattached from a permanent" trigger class
// (Mode$ Unattached, CR 701.3b), pinned end to end on the real corpus card
// Grafted Exoskeleton. The trigger fires off the events.Unattached event
// rules/attach.go's attachmentSBAs emits when the bearer leaves the
// battlefield, and the body's Defined$ TriggeredObjectLKICopy referent must
// resolve to the FORMER BEARER -- never to the Equipment itself or any other
// permanent.

const unattachedBystanderSrc = "Name:Bystander Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// equipOn equips the Equipment onto the named battlefield creature for seat 0
// through the real Equip activation. Every caller asserts the resulting
// attachment itself, so a setup that silently failed to equip cannot reach an
// assertion.
func equipOn(t *testing.T, e *Engine, equip, bear state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "CC")
	submitChoices(t, e, abilityOption(t, e, equip, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("equip target decision: %+v", d)
	}
	idx := indexOfObjOption(d, bear)
	if idx < 0 {
		t.Fatalf("equip ask does not offer the bear: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
}

// TestGraftedExoskeletonFiresWhenTheBearerLeavesAndSkipsTheGoneLKI is the
// filing card, end to end: Grafted Exoskeleton equips a creature, the
// creature leaves the battlefield, attachmentSBAs emits events.Unattached,
// the trigger fires, and its SacrificeAll body resolves
// Defined$ TriggeredObjectLKICopy to the FORMER BEARER -- which is already in
// the graveyard, so the sacrifice skips it LOUDLY (one Note naming the bear)
// instead of sacrificing the Equipment or any other permanent.
func TestGraftedExoskeletonFiresWhenTheBearerLeavesAndSkipsTheGoneLKI(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	exo := mustCorpusCard(t, reg, "Grafted Exoskeleton")
	bearCard := card(t, attachedBearSrc)
	e, cfg := tokenReplGame(t, 111, exo, bearCard)
	exoID := moveSeededCard(t, e, 0, exo, state.ZBattlefield)
	bear := moveSeededCard(t, e, 0, bearCard, state.ZBattlefield)
	passUntilStackEmpty(t, e, 20)
	equipOn(t, e, exoID, bear)

	// Precondition: the Equipment really is attached to the bear before the
	// detach is provoked; a setup that never attached would make every
	// assertion below vacuous.
	if got := e.G.Obj(exoID).AttachedTo; got != bear {
		t.Fatalf("precondition failed: Grafted Exoskeleton attached to %d, want bear %d", got, bear)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.Face().Name != "Bear" {
		t.Fatalf("precondition failed: bearer %+v, want the real Bear on the battlefield", o)
	}

	// The bearer leaves the battlefield (the real MoveZone every removal
	// path emits). attachmentSBAs then detaches the Equipment without the
	// Equipment leaving the battlefield.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear,
		From: state.ZBattlefield, To: state.ZGraveyard})
	e.checkStateBased()
	e.pending = nil
	e.Advance()
	passUntilStackEmpty(t, e, 30)

	// The Equipment stayed on the battlefield, detached.
	if o := e.G.Obj(exoID); o == nil || o.Zone != state.ZBattlefield || o.AttachedTo != 0 {
		t.Fatalf("Equipment %+v, want battlefield and detached", o)
	}
	// The bearer really left (the "gone LKI" premise).
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("bear zone %v, want graveyard (the sacrifice must be a gone LKI copy)", o.Zone)
	}

	// The detach published a real events.Unattached naming the former bearer.
	var detach *events.Event
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.Unattached && ev.Obj == exoID {
			detach = ev
		}
	}
	if detach == nil {
		t.Fatal("Grafted Exoskeleton left the battlefield detached but no events.Unattached was emitted")
	}
	if len(detach.IDs) == 0 || detach.IDs[0] != bear {
		t.Fatalf("events.Unattached former bearer = %v, want [%d]", detach.IDs, bear)
	}

	// The trigger's body ran: SacrificeAll's Defined$ TriggeredObjectLKICopy
	// resolved to the former bear, which is off the battlefield, so it was
	// skipped with one loud Note naming that bear.
	foundReferent := false
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind != events.Note || !strings.Contains(ev.Text, "SacrificeAll target") {
			continue
		}
		if !strings.Contains(ev.Text, "not on the battlefield") {
			t.Fatalf("SacrificeAll Note did not name an off-battlefield skip: %q", ev.Text)
		}
		if !strings.Contains(ev.Text, "target "+idString(bear)+" ") {
			t.Fatalf("SacrificeAll skip Note %q does not name the former bear %d -- the referent resolved to a wrong permanent", ev.Text, bear)
		}
		foundReferent = true
	}
	if !foundReferent {
		t.Fatal("the Unattached trigger's SacrificeAll body never ran (no skip Note): the trigger did not fire or the body resolved to nothing")
	}

	// Nothing was actually sacrificed: no Sacrifice marker names the
	// Equipment or any other permanent.
	for _, ev := range e.L.Events {
		if events.IsSacrifice(ev) && ev.Obj != bear {
			t.Fatalf("a wrong permanent %d was sacrificed by the Unattached trigger", ev.Obj)
		}
	}
	if o := e.G.Obj(exoID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("Grafted Exoskeleton was sacrificed by its own trigger")
	}
	replayCheck(t, e, cfg)
}

// TestUnattachedLiveBearerIsSacrificed pins the other half of the referent:
// when the bearer is STILL on the battlefield at detach time, the trigger's
// Defined$ TriggeredObjectLKICopy sacrifices it for real. The detach event is
// the same shape attachmentSBAs emits (Obj = the Equipment, IDs[0] = the
// former bearer); driving it directly keeps this test about the referent and
// the live sacrifice, not about which SBA arm produced it (the real SBA arm
// is pinned by the bearer-left test above).
func TestUnattachedLiveBearerIsSacrificed(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	exo := mustCorpusCard(t, reg, "Grafted Exoskeleton")
	bearCard := card(t, attachedBearSrc)
	bystanderCard := card(t, unattachedBystanderSrc)
	e, cfg := tokenReplGame(t, 112, exo, bearCard, bystanderCard)
	exoID := moveSeededCard(t, e, 0, exo, state.ZBattlefield)
	bear := moveSeededCard(t, e, 0, bearCard, state.ZBattlefield)
	other := moveSeededCard(t, e, 0, bystanderCard, state.ZBattlefield)
	passUntilStackEmpty(t, e, 20)
	equipOn(t, e, exoID, bear)

	if got := e.G.Obj(exoID).AttachedTo; got != bear {
		t.Fatalf("precondition failed: attached to %d, want bear %d", got, bear)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: bear zone %v, want battlefield (the sacrifice must have a live target)", o.Zone)
	}

	// The detach with the bearer still alive on the battlefield -- the shape
	// the "bearer is no longer a creature" / bestowed SBA arms emit.
	e.emit(events.Event{Kind: events.Unattached, Obj: exoID, IDs: []state.ObjID{bear},
		Text: "test detach with a live bearer"})
	e.pending = nil
	e.Advance()
	passUntilStackEmpty(t, e, 30)

	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("live former bearer zone %v, want graveyard (the trigger must sacrifice it)", o.Zone)
	}
	sacrificed := false
	for _, ev := range e.L.Events {
		if events.IsSacrifice(ev) && ev.Obj == bear {
			sacrificed = true
		}
	}
	if !sacrificed {
		t.Fatal("no Sacrifice marker for the live former bearer -- the referent did not resolve to it")
	}
	if o := e.G.Obj(other); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the bystander was sacrificed too: zone %v, want battlefield", o.Zone)
	}
	if o := e.G.Obj(exoID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the Equipment was sacrificed instead of the bearer: zone %v", o.Zone)
	}
	replayCheck(t, e, cfg)
}

// idString renders an object id the way the effects Note does (%d).
func idString(id state.ObjID) string { return strconv.Itoa(int(id)) }

const unattachedPlainEquipSrc = "Name:Plain Sword\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:1\nOracle:x\n"

// TestUnrelatedEquipmentDetachDoesNotFireTheExoskeleton is the review-round-1
// regression for the ValidAttachment$ gate: the parameter names the
// attachment the EVENT is about (ev.Obj), never the trigger's own source.
// With two Equipments on the battlefield, an Unattached event naming the
// PLAIN one must not fire Grafted Exoskeleton's trigger at all -- before the
// fix the matcher evaluated `Card.Self` against the Exoskeleton (its own
// source), so it always matched and the trigger sacrificed the unrelated
// event's former bearer.
func TestUnrelatedEquipmentDetachDoesNotFireTheExoskeleton(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	exo := mustCorpusCard(t, reg, "Grafted Exoskeleton")
	bearCard := card(t, attachedBearSrc)
	plain := card(t, unattachedPlainEquipSrc)
	e, cfg := tokenReplGame(t, 113, exo, bearCard, plain)
	exoID := moveSeededCard(t, e, 0, exo, state.ZBattlefield)
	bear := moveSeededCard(t, e, 0, bearCard, state.ZBattlefield)
	sword := moveSeededCard(t, e, 0, plain, state.ZBattlefield)
	passUntilStackEmpty(t, e, 20)

	// Preconditions: both Equipments and the bear really are on the
	// battlefield, and the Exoskeleton is NOT attached to the bear (it never
	// was equipped), so a sacrifice of the bear could only come from the bug.
	for _, id := range []state.ObjID{exoID, bear, sword} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition failed: object %d not on the battlefield (%+v)", id, o)
		}
	}
	if got := e.G.Obj(exoID).AttachedTo; got != 0 {
		t.Fatalf("precondition failed: the Exoskeleton is attached to %d, want unattached", got)
	}

	// The PLAIN Equipment detaches from the bear. Its Obj is the sword, not
	// the Exoskeleton; the Exoskeleton's Card.Self gate must reject it.
	e.emit(events.Event{Kind: events.Unattached, Obj: sword, IDs: []state.ObjID{bear},
		Text: "test detach of an unrelated Equipment"})
	e.pending = nil
	e.Advance()
	passUntilStackEmpty(t, e, 30)

	// The bear survives: no unrelated Equipment's former bearer was
	// sacrificed on the Exoskeleton's behalf.
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the bear was sacrificed by the unrelated detach: zone %v", o.Zone)
	}
	for _, ev := range e.L.Events {
		if events.IsSacrifice(ev) && ev.Obj == bear {
			t.Fatal("a Sacrifice marker names the bear: the Exoskeleton's trigger fired on an unrelated detach")
		}
	}
	// The trigger's body must not have run at all (no SacrificeAll skip Note).
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "SacrificeAll") {
			t.Fatalf("the Exoskeleton's SacrificeAll body ran on an unrelated detach: %q", ev.Text)
		}
	}
	replayCheck(t, e, cfg)
}

// TestReequipFromOneLivingCreatureToAnotherFiresUnattached is the
// review-round-1 regression for the re-attach emit path: moving an Equipment
// from one live creature to another is CR 701.3b's "becomes unattached" for
// the former bearer, so events.Unattached must be emitted before the
// replacement Attach. Before the fix effects/attach.go emitted only Attach
// and overwrote AttachedTo, leaving the former bearer alive and the
// Exoskeleton's trigger silent.
func TestReequipFromOneLivingCreatureToAnotherFiresUnattached(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	exo := mustCorpusCard(t, reg, "Grafted Exoskeleton")
	bearA := card(t, attachedBearSrc)
	bearB := card(t, unattachedBystanderSrc)
	e, cfg := tokenReplGame(t, 114, exo, bearA, bearB)
	exoID := moveSeededCard(t, e, 0, exo, state.ZBattlefield)
	a := moveSeededCard(t, e, 0, bearA, state.ZBattlefield)
	b := moveSeededCard(t, e, 0, bearB, state.ZBattlefield)
	passUntilStackEmpty(t, e, 20)
	equipOn(t, e, exoID, a)

	// Precondition: the Exoskeleton is attached to A before the re-equip.
	if got := e.G.Obj(exoID).AttachedTo; got != a {
		t.Fatalf("precondition failed: attached to %d, want bear A %d", got, a)
	}
	if o := e.G.Obj(a); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: bear A zone %v, want battlefield", o.Zone)
	}

	// Re-equip onto B. The former bearer A must be sacrificed by the trigger
	// (Defined$ TriggeredObjectLKICopy = A), so a bug that drops the
	// Unattached leaves A alive.
	equipOn(t, e, exoID, b)

	// The re-attach published a real Unattached naming A before the new Attach.
	var detach *events.Event
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.Unattached && ev.Obj == exoID {
			detach = ev
		}
	}
	if detach == nil {
		t.Fatal("re-equipping emitted no events.Unattached: CR 701.3b's detach was dropped")
	}
	if len(detach.IDs) == 0 || detach.IDs[0] != a {
		t.Fatalf("events.Unattached former bearer = %v, want bear A [%d]", detach.IDs, a)
	}
	if got := e.G.Obj(exoID).AttachedTo; got != b {
		t.Fatalf("the Equipment ended attached to %d, want bear B %d", got, b)
	}
	// The live former bearer A was sacrificed for real.
	if o := e.G.Obj(a); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("former bearer A zone %v, want graveyard (the trigger must sacrifice it)", o.Zone)
	}
	sacrificed := false
	for _, ev := range e.L.Events {
		if events.IsSacrifice(ev) && ev.Obj == a {
			sacrificed = true
		}
	}
	if !sacrificed {
		t.Fatal("no Sacrifice marker for former bearer A -- the re-equip's Unattached did not fire the trigger")
	}
	// B (the new bearer) and the Equipment survive.
	if o := e.G.Obj(b); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("new bearer B zone %v, want battlefield", o.Zone)
	}
	if o := e.G.Obj(exoID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the Equipment zone %v, want battlefield", o.Zone)
	}
	replayCheck(t, e, cfg)
}
