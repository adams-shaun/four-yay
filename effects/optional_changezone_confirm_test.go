package effects

// The explicit Optional$ confirm-before-pick gate for the two ChangeZone
// fetch paths Forge routes through changeHiddenOriginResolve's confirmAction:
// the hidden-hand walk (handMoveOwnersWalk, ordinary non-ForgetOther fetches
// included) and the Hidden$ True public-origin pick (effHiddenPick). The
// marker asks the decider whether to proceed BEFORE any card is picked; a
// decline poses no pick and changes nothing, while an accepted confirmation
// enters the fetch -- whose Min-0 pick may still take nothing, and whose
// empty eligible pool still confirms (Forge's gate runs before the fetch
// list is consulted). ChoiceOptional$ is the pick's own cardinality marker,
// never a confirmation, and the markerless text-may shapes stay
// confirmation-free.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const ocHandOptional = "DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | ChangeNum$ 1 | Optional$ True"

// TestOptionalChangeZoneHandConfirm is the ordinary (non-ForgetOther)
// Optional$ hand fetch's two-ask contract.
func TestOptionalChangeZoneHandConfirm(t *testing.T) {
	t.Run("confirmation gate runs before the pick", func(t *testing.T) {
		h, ids := handAskFixture(t)
		Resolve(h, &Ctx{Controller: 0}, sa(t, ocHandOptional))
		confirm := h.asked
		if confirm == nil || confirm.ResumeKind != "hand_move_confirm" || confirm.Player != 0 ||
			confirm.Min != 1 || confirm.Max != 1 ||
			len(confirm.Options) != 2 || confirm.Options[0].Kind != "yes" || confirm.Options[1].Kind != "no" {
			t.Fatalf("precondition: first ask = %+v, want the hand_move_confirm yes/no gate for the controller", confirm)
		}
		for _, id := range ids {
			if o := h.g.Obj(id); o.Zone != state.ZHand {
				t.Fatalf("the confirmation itself moved ids[%d] to %s", id, o.Zone)
			}
		}
	})

	t.Run("decline poses no pick and moves nothing", func(t *testing.T) {
		h, ids := handAskFixture(t)
		Resolve(h, &Ctx{Controller: 0}, sa(t, ocHandOptional))
		if h.asked == nil || h.asked.ResumeKind != "hand_move_confirm" {
			t.Fatalf("precondition: no confirmation gate was posed: %+v", h.asked)
		}
		h.asked = nil
		Resolve(h, &Ctx{Controller: 0, HandMoveConfirmDone: true, HandMoveConfirm: "no",
			HandMoveConfirmTarget: 0}, sa(t, ocHandOptional))
		if h.asked != nil {
			t.Fatalf("a declined confirmation posed a card pick: %+v", h.asked)
		}
		for i, id := range ids {
			if o := h.g.Obj(id); o.Zone != state.ZHand {
				t.Fatalf("decline moved ids[%d] to %s", i, o.Zone)
			}
		}
	})

	t.Run("accepted confirmation picks", func(t *testing.T) {
		h, ids := handAskFixture(t)
		Resolve(h, &Ctx{Controller: 0}, sa(t, ocHandOptional))
		if h.asked == nil || h.asked.ResumeKind != "hand_move_confirm" {
			t.Fatalf("precondition: no confirmation gate was posed: %+v", h.asked)
		}
		Resolve(h, &Ctx{Controller: 0, HandMoveConfirmDone: true, HandMoveConfirm: "yes",
			HandMoveConfirmTarget: 0}, sa(t, ocHandOptional))
		pick := h.asked
		if pick == nil || pick.ResumeKind != "hand_move" || pick.Min != 0 || pick.Max != 1 ||
			len(pick.Options) != 2 || pick.Options[0].Obj != ids[1] || pick.Options[1].Obj != ids[2] {
			t.Fatalf("accepted confirmation pick = %+v, want a Min 0 hand_move over the two Isles", pick)
		}
		// The player's answer moves exactly the picked isle.
		Resolve(h, &Ctx{Controller: 0, HandMove: []state.ObjID{ids[2]}, HandMoveDone: true,
			HandMoveTarget: 0}, sa(t, ocHandOptional))
		if o := h.g.Obj(ids[2]); o.Zone != state.ZBattlefield {
			t.Fatalf("answered isle on %s, want battlefield", o.Zone)
		}
		if o := h.g.Obj(ids[1]); o.Zone != state.ZHand {
			t.Fatalf("unchosen isle moved: on %s", o.Zone)
		}
	})

	t.Run("accepted confirmation may pick none", func(t *testing.T) {
		h, ids := handAskFixture(t)
		Resolve(h, &Ctx{Controller: 0}, sa(t, ocHandOptional))
		if h.asked == nil || h.asked.ResumeKind != "hand_move_confirm" {
			t.Fatalf("precondition: no confirmation gate was posed: %+v", h.asked)
		}
		Resolve(h, &Ctx{Controller: 0, HandMoveConfirmDone: true, HandMoveConfirm: "yes",
			HandMoveConfirmTarget: 0}, sa(t, ocHandOptional))
		if h.asked == nil || h.asked.ResumeKind != "hand_move" {
			t.Fatalf("precondition: the accepted confirmation posed no pick: %+v", h.asked)
		}
		Resolve(h, &Ctx{Controller: 0, HandMove: nil, HandMoveDone: true, HandMoveTarget: 0},
			sa(t, ocHandOptional))
		for i, id := range ids {
			if o := h.g.Obj(id); o.Zone != state.ZHand {
				t.Fatalf("accept-then-pick-none moved ids[%d] to %s", i, o.Zone)
			}
		}
	})

	t.Run("markerless may-shape stays confirmation-free", func(t *testing.T) {
		h, _ := handAskFixture(t)
		Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | ChangeNum$ 1 | SpellDescription$ You may put a land card from your hand onto the battlefield."))
		d := h.asked
		if d == nil || d.ResumeKind != "hand_move" || d.Min != 0 || d.Max != 1 {
			t.Fatalf("markerless may first ask = %+v, want the Min 0 hand_move pick with no confirmation gate", d)
		}
	})

	t.Run("no-host confirmation proceeds and takes deterministically", func(t *testing.T) {
		h, ids := handNoHostFixture(t)
		Resolve(h, &Ctx{Controller: 0}, sa(t, ocHandOptional))
		// The no-host run built BOTH asks (the confirmation gate and the
		// Min-0 pick) and answered each deterministically in the player's
		// place; lastAsk holds only the second.
		if h.askCount != 2 {
			t.Fatalf("no-host ask count = %d, want 2 (the confirmation gate and the pick)", h.askCount)
		}
		// R-9: no host to answer -- play "may" as "do": the deterministic
		// stand-in takes the first eligible card.
		if o := h.g.Obj(ids[0]); o.Zone != state.ZBattlefield {
			t.Fatalf("no-host fetch left the first land on %s, want battlefield", o.Zone)
		}
	})

	t.Run("real corpus carrier: Yuna's Decision", func(t *testing.T) {
		reg := testutil.CorpusRegistry(t)
		yuna, ok := reg.Lookup("Yuna's Decision")
		if !ok || len(yuna.Faces) == 0 {
			t.Fatal("Yuna's Decision is absent from the corpus")
		}
		fetch := cards.ResolveSVar(yuna.Faces[0].SVars, "DBChangeZone")
		if fetch == nil {
			t.Fatal("Yuna's Decision has no compiled DBChangeZone continuation")
		}
		if fetch.Params["Optional"] != "True" {
			t.Fatalf("precondition: the corpus ability lost its Optional$ marker: %+v", fetch.Params)
		}
		h := &askHost{}
		h.g = state.NewGame(names(2))
		bear := mkCard(t, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n")
		isle := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
		bearID := h.g.AddObject(bear, 0).ID
		isleID := h.g.AddObject(isle, 0).ID
		h.g.SetZone(state.ZHand, 0, []state.ObjID{bearID, isleID})
		h.g.Obj(bearID).Zone = state.ZHand
		h.g.Obj(isleID).Zone = state.ZHand
		// The SVar carries ConditionDefined$ Remembered | ConditionPresent$
		// Card.Creature: seed the remembered set so the fetch really runs.
		c := &Ctx{Controller: 0, Remembered: []state.Target{{Obj: bearID}}}
		Resolve(h, c, fetch)
		confirm := h.asked
		if confirm == nil || confirm.ResumeKind != "hand_move_confirm" || confirm.Player != 0 {
			t.Fatalf("Yuna's Decision fetch first ask = %+v, want its hand_move_confirm gate", confirm)
		}
		// Accept: the EACH Creature & Land structured pick follows.
		Resolve(h, &Ctx{Controller: 0, HandMoveConfirmDone: true, HandMoveConfirm: "yes",
			HandMoveConfirmTarget: 0, Remembered: []state.Target{{Obj: bearID}}}, fetch)
		pick := h.asked
		if pick == nil || pick.ResumeKind != "hand_move" {
			t.Fatalf("accepted Yuna confirmation pick = %+v, want the structured hand_move", pick)
		}
		var bearOffered, isleOffered bool
		for _, o := range pick.Options {
			if o.Obj == bearID {
				bearOffered = true
			}
			if o.Obj == isleID {
				isleOffered = true
			}
		}
		if !bearOffered || !isleOffered {
			t.Fatalf("structured pick options = %+v, want the EACH Creature & Land members (bear and isle)", pick.Options)
		}
	})
}

const ocHiddenOptional = "DB$ ChangeZone | Hidden$ True | Origin$ Graveyard | Destination$ Battlefield | ChangeType$ Creature | ChangeNum$ 1 | Optional$ True"

// ocHiddenFixture seats a creature and a land in seat 0's graveyard and
// returns (host, creature id, land id).
func ocHiddenFixture(t *testing.T) (*askHost, state.ObjID, state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	bear := mkCard(t, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n")
	isle := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	bearID := h.g.AddObject(bear, 0).ID
	isleID := h.g.AddObject(isle, 0).ID
	h.g.SetZone(state.ZGraveyard, 0, []state.ObjID{bearID, isleID})
	h.g.Obj(bearID).Zone = state.ZGraveyard
	h.g.Obj(isleID).Zone = state.ZGraveyard
	return h, bearID, isleID
}

// TestOptionalChangeZoneHiddenPickConfirm is the Hidden$ True public-origin
// pick's confirm-before-pick contract, including the real empty-pool case.
func TestOptionalChangeZoneHiddenPickConfirm(t *testing.T) {
	t.Run("confirmation gate runs before the pick", func(t *testing.T) {
		h, bearID, _ := ocHiddenFixture(t)
		if bearID == 0 || h.g.Obj(bearID).Zone != state.ZGraveyard {
			t.Fatal("precondition: the creature is not in the graveyard the fetch reads")
		}
		Resolve(h, &Ctx{Controller: 0}, sa(t, ocHiddenOptional))
		confirm := h.asked
		if confirm == nil || confirm.ResumeKind != "hidden_pick_confirm" || confirm.Player != 0 ||
			confirm.Min != 1 || confirm.Max != 1 ||
			len(confirm.Options) != 2 || confirm.Options[0].Kind != "yes" || confirm.Options[1].Kind != "no" {
			t.Fatalf("precondition: first ask = %+v, want the hidden_pick_confirm yes/no gate", confirm)
		}
		if h.g.Obj(bearID).Zone != state.ZGraveyard {
			t.Fatalf("the confirmation itself moved the creature to %s", h.g.Obj(bearID).Zone)
		}
	})

	t.Run("decline poses no pick and moves nothing", func(t *testing.T) {
		h, bearID, _ := ocHiddenFixture(t)
		Resolve(h, &Ctx{Controller: 0}, sa(t, ocHiddenOptional))
		if h.asked == nil || h.asked.ResumeKind != "hidden_pick_confirm" {
			t.Fatalf("precondition: no confirmation gate was posed: %+v", h.asked)
		}
		h.asked = nil
		Resolve(h, &Ctx{Controller: 0, HiddenPickConfirmDone: true, HiddenPickConfirm: "no",
			HiddenPickConfirmTarget: 0}, sa(t, ocHiddenOptional))
		if h.asked != nil {
			t.Fatalf("a declined confirmation posed a card pick: %+v", h.asked)
		}
		if h.g.Obj(bearID).Zone != state.ZGraveyard {
			t.Fatalf("decline moved the creature to %s", h.g.Obj(bearID).Zone)
		}
	})

	t.Run("accepted confirmation picks the eligible card", func(t *testing.T) {
		h, bearID, isleID := ocHiddenFixture(t)
		Resolve(h, &Ctx{Controller: 0}, sa(t, ocHiddenOptional))
		if h.asked == nil || h.asked.ResumeKind != "hidden_pick_confirm" {
			t.Fatalf("precondition: no confirmation gate was posed: %+v", h.asked)
		}
		Resolve(h, &Ctx{Controller: 0, HiddenPickConfirmDone: true, HiddenPickConfirm: "yes",
			HiddenPickConfirmTarget: 0}, sa(t, ocHiddenOptional))
		pick := h.asked
		if pick == nil || pick.ResumeKind != "hidden_pick" || pick.Min != 0 || pick.Max != 1 ||
			len(pick.Options) != 1 || pick.Options[0].Obj != bearID {
			t.Fatalf("accepted pick = %+v, want a Min 0 hidden_pick whose only option is the bear (the land must be filtered out)", pick)
		}
		Resolve(h, &Ctx{Controller: 0, HiddenPick: []state.ObjID{bearID}, HiddenPickDone: true,
			HiddenPickTarget: 0}, sa(t, ocHiddenOptional))
		if o := h.g.Obj(bearID); o.Zone != state.ZBattlefield {
			t.Fatalf("answered creature on %s, want battlefield", o.Zone)
		}
		if o := h.g.Obj(isleID); o.Zone != state.ZGraveyard {
			t.Fatalf("the land moved: on %s", o.Zone)
		}
	})

	t.Run("accepted confirmation may pick none", func(t *testing.T) {
		h, bearID, _ := ocHiddenFixture(t)
		Resolve(h, &Ctx{Controller: 0}, sa(t, ocHiddenOptional))
		if h.asked == nil || h.asked.ResumeKind != "hidden_pick_confirm" {
			t.Fatalf("precondition: no confirmation gate was posed: %+v", h.asked)
		}
		Resolve(h, &Ctx{Controller: 0, HiddenPickConfirmDone: true, HiddenPickConfirm: "yes",
			HiddenPickConfirmTarget: 0}, sa(t, ocHiddenOptional))
		if h.asked == nil || h.asked.ResumeKind != "hidden_pick" {
			t.Fatalf("precondition: the accepted confirmation posed no pick: %+v", h.asked)
		}
		Resolve(h, &Ctx{Controller: 0, HiddenPick: nil, HiddenPickDone: true, HiddenPickTarget: 0},
			sa(t, ocHiddenOptional))
		if o := h.g.Obj(bearID); o.Zone != state.ZGraveyard {
			t.Fatalf("accept-then-pick-none moved the creature to %s", o.Zone)
		}
	})

	t.Run("empty eligible pool still confirms", func(t *testing.T) {
		h := &askHost{}
		h.g = state.NewGame(names(2))
		isle := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
		isleID := h.g.AddObject(isle, 0).ID
		h.g.SetZone(state.ZGraveyard, 0, []state.ObjID{isleID})
		h.g.Obj(isleID).Zone = state.ZGraveyard
		// Precondition: the pool the ChangeType$ filter reads holds no creature.
		if h.g.Obj(isleID).Zone != state.ZGraveyard {
			t.Fatal("precondition: the land left the graveyard")
		}
		Resolve(h, &Ctx{Controller: 0}, sa(t, ocHiddenOptional))
		confirm := h.asked
		if confirm == nil || confirm.ResumeKind != "hidden_pick_confirm" {
			t.Fatalf("empty-pool first ask = %+v, want the confirmation gate (Forge asks before the fetch list is consulted)", confirm)
		}
		// Declining an empty-pool fetch moves nothing.
		h.asked = nil
		Resolve(h, &Ctx{Controller: 0, HiddenPickConfirmDone: true, HiddenPickConfirm: "no",
			HiddenPickConfirmTarget: 0}, sa(t, ocHiddenOptional))
		if h.asked != nil {
			t.Fatalf("a declined empty-pool fetch posed a pick: %+v", h.asked)
		}
		if h.g.Obj(isleID).Zone != state.ZGraveyard {
			t.Fatalf("decline moved the land to %s", h.g.Obj(isleID).Zone)
		}
		// Accepting it enters the fetch, whose empty continuation completes
		// without a pick ask.
		h.asked = nil
		Resolve(h, &Ctx{Controller: 0, HiddenPickConfirmDone: true, HiddenPickConfirm: "yes",
			HiddenPickConfirmTarget: 0}, sa(t, ocHiddenOptional))
		if h.asked != nil {
			t.Fatalf("an accepted empty-pool fetch posed a pick: %+v", h.asked)
		}
		if h.g.Obj(isleID).Zone != state.ZGraveyard {
			t.Fatalf("accepted empty fetch moved the land to %s", h.g.Obj(isleID).Zone)
		}
	})

	t.Run("ChoiceOptional$ cardinality marker does not confirm", func(t *testing.T) {
		h, _, _ := ocHiddenFixture(t)
		Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ ChangeZone | Hidden$ True | Origin$ Graveyard | Destination$ Battlefield | ChangeType$ Creature | ChangeNum$ 1 | ChoiceOptional$ True"))
		d := h.asked
		if d == nil || d.ResumeKind != "hidden_pick" || d.Min != 0 || d.Max != 1 {
			t.Fatalf("ChoiceOptional$ first ask = %+v, want the Min 0 hidden_pick with no confirmation gate", d)
		}
	})

	t.Run("mandatory pick does not confirm", func(t *testing.T) {
		h, _, _ := ocHiddenFixture(t)
		Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ ChangeZone | Hidden$ True | Origin$ Graveyard | Destination$ Battlefield | ChangeType$ Creature | ChangeNum$ 1 | Mandatory$ True"))
		d := h.asked
		if d == nil || d.ResumeKind != "hidden_pick" || d.Min != 1 || d.Max != 1 {
			t.Fatalf("mandatory first ask = %+v, want the Min 1 hidden_pick with no confirmation gate", d)
		}
	})

	t.Run("no-host confirmation proceeds deterministically", func(t *testing.T) {
		fh, bearID, _ := ocHiddenFixture(t)
		h := &fakeHost{g: fh.g}
		Resolve(h, &Ctx{Controller: 0}, sa(t, ocHiddenOptional))
		// The no-host run built BOTH asks (the confirmation gate and the
		// Min-0 pick) and answered each deterministically in the player's
		// place; lastAsk holds only the second.
		if h.askCount != 2 {
			t.Fatalf("no-host ask count = %d, want 2 (the confirmation gate and the pick)", h.askCount)
		}
		// R-9: no host to answer -- play "may" as "do": the deterministic
		// stand-in takes the first eligible card.
		if o := h.g.Obj(bearID); o.Zone != state.ZBattlefield {
			t.Fatalf("no-host fetch left the creature on %s, want battlefield", o.Zone)
		}
	})
}
