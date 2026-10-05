package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestPlayerCountOtherKayaCompiledAbility reads the actual compiled Kaya
// ability's OneEach SVar and confirms its TargetMax$ consumer has the value
// for the live opponents in this multiplayer fixture. Corpus census: Kaya,
// Spirits' Justice uses PlayerCountOther$Amount; Advice from the Fae and Peer
// Pressure use PlayerCountOther$HighestValid Creature.YouCtrl.
func TestPlayerCountOtherKayaCompiledAbility(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	kaya, ok := reg.Lookup("Kaya, Spirits' Justice")
	if !ok || kaya == nil {
		t.Fatal("missing corpus Kaya, Spirits' Justice")
	}
	var ability *cards.SA
	for _, candidate := range kaya.Faces[0].Abilities {
		if candidate.API == "ChangeZone" && candidate.Params["Cost"] == "SubCounter<2/LOYALTY>" {
			ability = candidate
			break
		}
	}
	if ability == nil {
		t.Fatal("Kaya compiled -2 ChangeZone ability not found")
	}
	foreach := ability.Sub
	if foreach == nil || foreach.Params["TargetMax"] != "OneEach" {
		t.Fatalf("precondition: Kaya compiled SubAbility = %+v, want TargetMax OneEach", foreach)
	}
	body := kaya.Faces[0].SVars["OneEach"]
	if body == "" {
		t.Fatal("precondition: Kaya compiled OneEach SVar is absent")
	}

	h := newHost(t, 3)
	src := h.g.AddObject(kaya, 0)
	src.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID})
	for _, player := range []state.PlayerID{1, 1, 2} {
		o := h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature\nPT:1/1\nOracle:x\n"), player)
		o.Zone = state.ZBattlefield
		h.g.SetZone(state.ZBattlefield, player, append(h.g.Zone(state.ZBattlefield, player), o.ID))
	}
	ctx := &Ctx{Source: src.ID, Controller: 0, SVars: kaya.Faces[0].SVars}
	if len(opponentGroup(h.g, ctx)) != 2 {
		t.Fatal("precondition: Kaya must have two living other players")
	}
	if got, ok := EvalCountOK(h, ctx, body); !ok || got != 2 {
		t.Fatalf("Kaya compiled OneEach SVar %q = (%d,%v), want (2,true)", body, got, ok)
	}
}

// TestPlayerCountDefinedRememberedOwnerDeadlyCoverUpCompiledAbility evaluates
// the exact compiled count SVars consumed by Deadly Cover-Up's ChangeZone
// chain against remembered-card owners and their distinct zones. Corpus
// census: its NumInYard, NumInHand, and NumInLib variables are the three
// PlayerCountDefinedRememberedOwner carriers.
func TestPlayerCountDefinedRememberedOwnerDeadlyCoverUpCompiledAbility(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Deadly Cover-Up")
	if !ok || card == nil {
		t.Fatal("missing corpus Deadly Cover-Up")
	}
	face := card.Faces[0]
	body := cards.ResolveSVar(face.SVars, "ExileYard")
	if body == nil || body.API != "ChangeZone" {
		t.Fatalf("precondition: compiled ExileYard = %+v", body)
	}
	for _, name := range []string{"NumInYard", "NumInHand", "NumInLib"} {
		if face.SVars[name] == "" {
			t.Fatalf("precondition: missing compiled SVar %s", name)
		}
	}
	mountain, ok := reg.Lookup("Mountain")
	if !ok || mountain == nil {
		t.Fatal("missing corpus Mountain")
	}
	h := newHost(t, 3)
	remembered := h.g.AddObject(mountain, 1)
	remembered.Zone = state.ZExile
	h.g.SetZone(state.ZExile, 1, []state.ObjID{remembered.ID})
	ctx := &Ctx{Source: h.g.AddObject(card, 0).ID, Controller: 0, SVars: face.SVars,
		Remembered: []state.Target{{Obj: remembered.ID}}}
	add := func(player state.PlayerID, zone state.Zone) state.ObjID {
		o := h.g.AddObject(mountain, player)
		o.Zone = zone
		h.g.SetZone(zone, player, append(h.g.Zone(zone, player), o.ID))
		return o.ID
	}
	grave := add(1, state.ZGraveyard)
	hand := add(1, state.ZHand)
	library := add(1, state.ZLibrary)
	distractor := add(2, state.ZGraveyard)
	if len(h.g.Zone(state.ZGraveyard, 1)) != 1 || len(h.g.Zone(state.ZHand, 1)) != 1 || len(h.g.Zone(state.ZLibrary, 1)) != 1 {
		t.Fatal("precondition: remembered owner must have exactly one same-name card in each searched zone")
	}

	if got, ok := EvalCountOK(h, ctx, face.SVars["NumInYard"]); !ok || got != 1 {
		t.Errorf("Deadly Cover-Up compiled NumInYard %q = (%d,%v), want (1,true)", face.SVars["NumInYard"], got, ok)
	}
	if got, ok := EvalCountOK(h, ctx, face.SVars["NumInHand"]); !ok || got != 1 {
		t.Errorf("Deadly Cover-Up compiled NumInHand %q = (%d,%v), want (1,true)", face.SVars["NumInHand"], got, ok)
	}
	if got, ok := EvalCountOK(h, ctx, face.SVars["NumInLib"]); !ok || got != 1 {
		t.Errorf("Deadly Cover-Up compiled NumInLib %q = (%d,%v), want (1,true)", face.SVars["NumInLib"], got, ok)
	}
	if h.g.Obj(grave).Zone != state.ZGraveyard || h.g.Obj(hand).Zone != state.ZHand || h.g.Obj(library).Zone != state.ZLibrary || h.g.Obj(distractor).Zone != state.ZGraveyard {
		t.Fatal("precondition: evaluating counts must not mutate the owner or distractor zones")
	}
}

// TestPlayerCountHasCardsInHandAclazotzCompiledAbility follows the actual
// compiled Temple of the Dead activation's CheckSVar gate. With two cards in
// every hand it must not transform; reducing one player's hand to one makes
// the same ability transform the actual double-faced corpus permanent.
// Corpus census: Aclazotz, Deepest Betrayal and Naktamun Lorespinner both use
// PlayerCount$HasPropertyHasCardsInHand_Card_LE1.
func TestPlayerCountHasCardsInHandAclazotzCompiledAbility(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Aclazotz, Deepest Betrayal")
	if !ok || card == nil || len(card.Faces) != 2 {
		t.Fatalf("missing/two-face corpus Aclazotz: %+v", card)
	}
	back := card.Faces[1]
	var ability *cards.SA
	for _, candidate := range back.Abilities {
		if candidate.API == "SetState" && candidate.Params["Mode"] == "Transform" {
			ability = candidate
			break
		}
	}
	if ability == nil {
		t.Fatal("compiled Temple of the Dead Transform activation not found")
	}
	h := newHost(t, 2)
	src := h.g.AddObject(card, 0)
	src.Zone = state.ZBattlefield
	src.SetFaceIdx(1)
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID})
	addHand := func(player state.PlayerID, count int) {
		for i := 0; i < count; i++ {
			o := h.g.AddObject(mkCard(t, "Name:Hand card\nTypes:Sorcery\nOracle:x\n"), player)
			o.Zone = state.ZHand
			h.g.SetZone(state.ZHand, player, append(h.g.Zone(state.ZHand, player), o.ID))
		}
	}
	addHand(0, 2)
	addHand(1, 2)
	if h.g.Obj(src.ID).FaceIdx != 1 || len(h.g.Zone(state.ZHand, 0)) != 2 || len(h.g.Zone(state.ZHand, 1)) != 2 {
		t.Fatal("precondition: Temple face and two-card hands are required")
	}
	ctx := &Ctx{Source: src.ID, Controller: 0, SVars: back.SVars}
	holds, evaluated := CheckSVarHolds(h, ctx, ability.Params["CheckSVar"], ability.Params["SVarCompare"])
	if !evaluated || holds {
		t.Fatalf("Aclazotz compiled ability gate = (%v,%v), want evaluated false", holds, evaluated)
	}
	// One qualifying player is the exact threshold: <=1 rather than <1.
	first := h.g.Zone(state.ZHand, 1)[0]
	h.Emit(events.Event{Kind: events.MoveZone, Obj: first, From: state.ZHand, To: state.ZGraveyard})
	if len(h.g.Zone(state.ZHand, 1)) != 1 {
		t.Fatalf("precondition: opponent hand = %d, want exactly one", len(h.g.Zone(state.ZHand, 1)))
	}
	holds, evaluated = CheckSVarHolds(h, ctx, ability.Params["CheckSVar"], ability.Params["SVarCompare"])
	if !evaluated || !holds {
		t.Fatalf("Aclazotz compiled ability gate at one-card threshold = (%v,%v), want true,true", holds, evaluated)
	}
	Resolve(h, ctx, ability)
	if h.g.Obj(src.ID).FaceIdx != 0 {
		t.Fatalf("Aclazotz compiled ability did not transform with one qualifying player: face=%d", h.g.Obj(src.ID).FaceIdx)
	}
	lorespinner, ok := reg.Lookup("Naktamun Lorespinner")
	if !ok || lorespinner == nil {
		t.Fatal("missing corpus Naktamun Lorespinner")
	}
	if lorespinner.Faces[0].SVars["X"] != back.SVars["X"] {
		t.Fatalf("corpus Count$ carrier changed: Aclazotz=%q Naktamun=%q", back.SVars["X"], lorespinner.Faces[0].SVars["X"])
	}
	triggerCtx := &Ctx{Controller: 0, SVars: lorespinner.Faces[0].SVars}
	for _, tr := range lorespinner.Faces[0].Triggers {
		if tr.Params["CheckSVar"] != "X" {
			continue
		}
		holds, evaluated := CheckSVarHolds(h, triggerCtx, tr.Params["CheckSVar"], tr.Params["SVarCompare"])
		if !evaluated || !holds {
			t.Fatalf("Naktamun Lorespinner compiled trigger gate = (%v,%v), want true,true", holds, evaluated)
		}
		return
	}
	t.Fatal("Naktamun Lorespinner compiled X trigger not found")
}
