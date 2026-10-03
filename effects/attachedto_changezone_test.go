package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// attachedToBoard builds the Mantle-of-the-Ancients-shaped board: an Aura
// source attached to a battlefield creature bearer, an enchantable Aura card
// and an Equipment card in seat 0's graveyard, and a second Aura whose
// Enchant spec the bearer does NOT satisfy. The returned map keys the object
// ids. Every zone is set through g.Obj(id) immediately after its own
// AddObject, never through the returned pointer (the aliasing rule
// attach_test.go's attachBoard documents).
func attachedToBoard(t *testing.T) (*fakeHost, *Ctx, map[string]state.ObjID) {
	t.Helper()
	h := newHost(t, 2)
	mantle := h.g.AddObject(mkCard(t, "Name:Mantle\nManaCost:3 W W\nTypes:Enchantment Aura\nK:Enchant:Creature.YouCtrl\nOracle:x\n"), 0)
	bear := h.g.AddObject(mkCard(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	favor := h.g.AddObject(mkCard(t, "Name:Favor\nManaCost:1 W\nTypes:Enchantment Aura\nK:Enchant:Creature\nOracle:x\n"), 0)
	splitter := h.g.AddObject(mkCard(t, "Name:Splitter\nManaCost:1\nTypes:Artifact Equipment\nOracle:x\n"), 0)
	sprawl := h.g.AddObject(mkCard(t, "Name:Sprawl\nManaCost:G\nTypes:Enchantment Aura\nK:Enchant:Forest\nOracle:x\n"), 0)
	h.g.Obj(mantle.ID).Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{mantle.ID, bear.ID})
	h.g.Obj(bear.ID).Zone = state.ZBattlefield
	h.g.SetZone(state.ZGraveyard, 0, []state.ObjID{favor.ID, splitter.ID, sprawl.ID})
	h.g.Obj(mantle.ID).AttachedTo = bear.ID
	c := &Ctx{Source: mantle.ID, Controller: 0}
	return h, c, map[string]state.ObjID{
		"mantle": mantle.ID, "bear": bear.ID, "favor": favor.ID,
		"splitter": splitter.ID, "sprawl": sprawl.ID,
	}
}

// attachedToLanded puts the fixture card id onto seat 0's battlefield: the
// post-move AttachedTo$ rider (changeZoneAttachedTo) reads a card whose
// battlefield entry has landed, and leaves alone one that never entered.
func attachedToLanded(h *fakeHost, id state.ObjID) {
	gy := h.g.Zone(state.ZGraveyard, 0)
	keep := make([]state.ObjID, 0, len(gy))
	for _, x := range gy {
		if x != id {
			keep = append(keep, x)
		}
	}
	h.g.SetZone(state.ZGraveyard, 0, keep)
	h.g.SetZone(state.ZBattlefield, 0, append(append([]state.ObjID(nil), h.g.Zone(state.ZBattlefield, 0)...), id))
	h.g.Obj(id).Zone = state.ZBattlefield
}

// TestAttachedToValidGraveyardDefinedResolvesTheNamedZone pins the
// zone-suffixed Valid filter family in Defined$ (definedSpec's
// ValidGraveyard/ValidHand/... branch, the twin of count.go's countZone):
// "Defined$ ValidGraveyard Aura.YouOwn" -- Retether's mass return and 16
// more raw ChangeZone lines -- resolves the named ZONE's matching cards. The
// spelling used to fall through Defined to its source fallback (the stack
// object), which the object path's Origin$ precondition then skipped, so the
// spell moved nothing silently.
func TestAttachedToValidGraveyardDefinedResolvesTheNamedZone(t *testing.T) {
	h, c, ids := attachedToBoard(t)
	ts, ok := knownDefinedTargets(h, c, "ValidGraveyard Aura.YouOwn")
	if !ok {
		t.Fatal("ValidGraveyard Aura.YouOwn classified unknown")
	}
	if len(ts) != 2 {
		t.Fatalf("targets %+v, want the two graveyard Auras (Favor, Sprawl)", ts)
	}
	got := map[state.ObjID]bool{}
	for _, t2 := range ts {
		got[t2.Obj] = true
	}
	if !got[ids["favor"]] || !got[ids["sprawl"]] {
		t.Fatalf("targets %+v missing Favor %d or Sprawl %d", ts, ids["favor"], ids["sprawl"])
	}
}

// TestAttachedToBareCardFilterAttachesTheFirstCreature pins the bare
// card-filter spelling of AttachedTo$ (Retether's `AttachedTo$ Creature`;
// One Last Job/Storm Herald/Nomad Mythmaker's `Creature.YouCtrl`): the value
// is not a Defined$ referent, so it must not ride Defined's source fallback
// (which would fasten the moved Aura to the resolving spell itself); it
// resolves as a battlefield card filter and the moved card attaches to the
// first creature the walk admits.
func TestAttachedToBareCardFilterAttachesTheFirstCreature(t *testing.T) {
	h, c, ids := attachedToBoard(t)
	attachedToLanded(h, ids["favor"])
	attachCreature := &cards.SA{Params: map[string]string{"AttachedTo": "Creature"}}
	changeZoneAttachedTo(h, c, attachCreature, ChangeZoneOf(attachCreature), ids["favor"])
	var attachs []events.Event
	for _, ev := range h.log {
		if ev.Kind == events.Attach && ev.Obj == ids["favor"] {
			attachs = append(attachs, ev)
		}
	}
	if len(attachs) != 1 || len(attachs[0].IDs) != 1 || attachs[0].IDs[0] != ids["bear"] {
		t.Fatalf("attachs %+v, want one Attach{favor -> bear}", attachs)
	}
	if h.g.Obj(ids["favor"]).AttachedTo != ids["bear"] {
		t.Fatalf("favor.AttachedTo = %d, want bear %d", h.g.Obj(ids["favor"]).AttachedTo, ids["bear"])
	}
}

// TestAttachedToUnresolvableFilterEmitsOneNoteLeavesUnattached pins the
// fail-closed direction for an AttachedTo$ card filter the grammar cannot
// evaluate: ONE loud Note, the card enters unattached, never a guessed
// attach to the resolving source.
func TestAttachedToUnresolvableFilterEmitsOneNoteLeavesUnattached(t *testing.T) {
	h, c, ids := attachedToBoard(t)
	attachedToLanded(h, ids["favor"])
	attachLand := &cards.SA{Params: map[string]string{"AttachedTo": "Land"}}
	changeZoneAttachedTo(h, c, attachLand, ChangeZoneOf(attachLand), ids["favor"])
	var attachs, notes []events.Event
	for _, ev := range h.log {
		switch ev.Kind {
		case events.Attach:
			attachs = append(attachs, ev)
		case events.Note:
			notes = append(notes, ev)
		}
	}
	if len(attachs) != 0 {
		t.Fatalf("attachs %+v, want none", attachs)
	}
	if len(notes) != 1 || h.g.Obj(ids["favor"]).AttachedTo != 0 {
		t.Fatalf("notes %+v, want exactly one loud-resolves-to-nothing Note and an unattached card", notes)
	}
}

// TestAttachedToCanEnchantEquippedByPredicate pins the CanEnchantEquippedBy
// filter predicate (Mantle of the Ancients' ValidTgts$ and Holy Avenger's
// ChangeType$ -- the two corpus carriers): the candidate card could legally
// be attached to the creature the resolving source attaches to. The referent
// is the source itself when it is a creature (Holy Avenger's equipped
// creature fires the trigger), else the source's AttachedTo bearer (Mantle's
// enchanted creature); the Face-less ability wrapper a placement ask sees as
// SpecContext.Source unwraps to its Source permanent. An Aura whose Enchant
// spec the bearer fails (Sprawl's Forest against the Bear) is ineligible;
// an Equipment is eligible against a creature bearer.
func TestAttachedToCanEnchantEquippedByPredicate(t *testing.T) {
	h, c, ids := attachedToBoard(t)
	spec := "Aura.CanEnchantEquippedBy+YouOwn,Equipment.CanEnchantEquippedBy+YouOwn"
	for _, tc := range []struct {
		name string
		id   state.ObjID
		want bool
	}{
		{"enchantable aura", ids["favor"], true},
		{"equipment", ids["splitter"], true},
		{"aura whose enchant spec fails", ids["sprawl"], false},
	} {
		if got := MatchesSpecCtx(h.g, spec, tc.id, c.SpecContext(0)); got != tc.want {
			t.Fatalf("%s: MatchesSpecCtx = %v, want %v", tc.name, got, tc.want)
		}
	}
	// Holy Avenger's shape: the trigger's source is the equipped CREATURE
	// itself, not an attachment.
	if !canEnchantEquippedBy(h.g, h.g.Obj(ids["favor"]), 0, ids["bear"]) {
		t.Fatal("creature-sourced referent (Holy Avenger shape) admitted nothing")
	}
	// The placement ask's shape: SpecContext.Source is the Face-less ability
	// wrapper a real AbilityPush mints (AddObject(nil, ...), no Card, hence
	// no Face) whose Source field names the permanent.
	w := h.g.AddObject(nil, 0)
	g := h.g.Obj(w.ID)
	g.Zone = state.ZStack
	g.Ability = &cards.SA{}
	g.Source = ids["mantle"]
	if !canEnchantEquippedBy(h.g, h.g.Obj(ids["favor"]), 0, w.ID) {
		t.Fatal("wrapper-sourced referent admitted nothing (unwrap missed)")
	}
}
