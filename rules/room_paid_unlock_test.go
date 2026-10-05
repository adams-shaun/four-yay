package rules

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func paidRoomManaTotal(c Cost) int32 { return c.Generic + c.Colored.Total() }

func paidUnlockRoom(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	room := card(t, "Name:Paid Room\nManaCost:0\nTypes:Enchantment Room\nOracle:x\n"+
		"ALTERNATE\nName:Alternate Vault\nManaCost:2 U\nTypes:Enchantment Room\nOracle:y\nAlternateMode:Split\n")
	room.Faces[0].ManaCost = "W"
	e := corpusEngine(t, reg, []*cards.Card{room}, nil)
	id := moveByName(t, e, 0, "Paid Room", state.ZBattlefield)
	designateRoomCastFace(e, id)
	for _, symbol := range "WUU" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(symbol), Amount: 1})
	}
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || !o.CastDoor || !o.DoorUnlocked(int(o.FaceIdx)) {
		t.Fatalf("precondition: Room is not on battlefield with cast face designated: %+v", o)
	}
	if paidRoomManaTotal(e.faceCost(o.Card.Faces[0])) == paidRoomManaTotal(e.faceCost(o.Card.Faces[1])) {
		t.Fatal("precondition: face costs must differ")
	}
	e.emit(events.Event{Kind: events.DoorLock, Obj: id, Amount: int32(o.FaceIdx) + 1})
	o = e.G.Obj(id)
	if o.Zone != state.ZBattlefield || o.DoorUnlocked(int(o.FaceIdx)) || o.DoorUnlocked(1-int(o.FaceIdx)) {
		t.Fatalf("precondition: expected both doors locked on battlefield: cast=%v alternate=%v", o.DoorUnlocked(int(o.FaceIdx)), o.DoorUnlocked(1-int(o.FaceIdx)))
	}
	e.priorityRound()
	return e, id
}

func paidUnlockOptions(d *decision.Decision, id state.ObjID) map[int]decision.Option {
	out := make(map[int]decision.Option)
	for _, opt := range d.Options {
		if opt.Kind == "unlock" && opt.Obj == id {
			fi, err := strconv.Atoi(opt.Mode)
			if err == nil {
				out[fi] = opt
			}
		}
	}
	return out
}

func TestPaidRoomUnlockOffersBothLockedDoors(t *testing.T) {
	e, id := paidUnlockRoom(t)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.DoorUnlocked(0) || o.DoorUnlocked(1) {
		t.Fatalf("precondition: expected battlefield Room with both doors locked: %+v", o)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected priority decision, got %+v", d)
	}
	options := paidUnlockOptions(d, id)
	if len(options) != 2 || options[0].Mode != "0" || options[1].Mode != "1" {
		t.Fatalf("wanted face-identified options for both doors, got %+v", options)
	}
	if paidRoomManaTotal(e.faceCost(o.Card.Faces[0])) == paidRoomManaTotal(e.faceCost(o.Card.Faces[1])) {
		t.Fatal("precondition: selected doors have identical costs")
	}
}

func TestPaidRoomUnlockPaysAndUnlocksSelectedDoor(t *testing.T) {
	for _, fi := range []int{0, 1} {
		t.Run(strconv.Itoa(fi), func(t *testing.T) {
			e, id := paidUnlockRoom(t)
			o := e.G.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield || o.DoorUnlocked(0) || o.DoorUnlocked(1) {
				t.Fatalf("precondition: both doors are not locked on battlefield: %+v", o)
			}
			before := e.G.Players[0].Pool.Total()
			d := e.Pending()
			opt, ok := paidUnlockOptions(d, id)[fi]
			if !ok {
				t.Fatalf("no option for selected face %d: %+v", fi, d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
				t.Fatalf("submit selected face unlock: %v", err)
			}
			after := e.G.Players[0].Pool.Total()
			wantSpent := paidRoomManaTotal(e.faceCost(o.Card.Faces[fi]))
			if before-after != wantSpent {
				t.Fatalf("paid %d mana for face %d, want that face's cost %d", before-after, fi, wantSpent)
			}
			o = e.G.Obj(id)
			if !o.DoorUnlocked(fi) || o.DoorUnlocked(1-fi) {
				t.Fatalf("selected face %d did not alone unlock: face0=%v face1=%v", fi, o.DoorUnlocked(0), o.DoorUnlocked(1))
			}
			wantAmount := int32(0)
			if fi == int(o.FaceIdx) {
				wantAmount = int32(fi) + 1
			}
			found := false
			for _, ev := range e.L.Events {
				if ev.Kind == events.DoorUnlock && ev.Obj == id {
					found = ev.Amount == wantAmount
				}
			}
			if !found {
				t.Fatalf("DoorUnlock event did not encode selected face %d with amount %d", fi, wantAmount)
			}
		})
	}
}

func TestPaidRoomUnlockRejectsStaleFaceOption(t *testing.T) {
	e, id := paidUnlockRoom(t)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.DoorUnlocked(0) || o.DoorUnlocked(1) {
		t.Fatalf("precondition: both doors are not locked on battlefield: %+v", o)
	}
	d := e.Pending()
	opt, ok := paidUnlockOptions(d, id)[0]
	if !ok {
		t.Fatalf("no face-0 unlock option in %+v", d)
	}
	e.emit(events.Event{Kind: events.DoorUnlock, Obj: id, Amount: int32(o.FaceIdx) + 1})
	if !e.G.Obj(id).DoorUnlocked(0) {
		t.Fatal("precondition: selected option's face was not unlocked before stale submission")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err == nil {
		t.Fatal("stale face-specific option was accepted")
	}
	if e.G.Obj(id).DoorUnlocked(1) {
		t.Fatal("stale face option unlocked a different door")
	}
}
