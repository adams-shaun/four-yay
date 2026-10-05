package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCorpusDoorLockHidesLockedRoomAbilities covers the production card path:
// Keys to the House unlocks the alternate face, then locks the cast face of an
// inline Room whose two doors have distinct activated abilities.
func TestCorpusDoorLockHidesLockedRoomAbilities(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	keys := lookup(t, reg, "Keys to the House")
	keysAbility := -1
	if keys != nil && len(keys.Faces) > 0 {
		for i, ability := range keys.Faces[0].Abilities {
			if ability.API == "UnlockDoor" {
				keysAbility = i
			}
		}
	}
	if keysAbility < 0 {
		t.Fatal("precondition: corpus Keys to the House lacks its UnlockDoor activated ability")
	}
	room := card(t, "Name:Corpus Ability Room\nManaCost:0\nTypes:Enchantment Room\nA:AB$ Draw | Cost$ 0 | NumCards$ 1\nOracle:x\nALTERNATE\nName:Corpus Ability Chamber\nManaCost:0\nTypes:Enchantment Room\nA:AB$ Draw | Cost$ 0 | NumCards$ 2\nOracle:y\nAlternateMode:Split\n")
	e := corpusEngine(t, reg, []*cards.Card{keys, keys, keys, room}, nil)
	keysIDs := []state.ObjID{
		moveByName(t, e, 0, "Keys to the House", state.ZBattlefield),
		moveByName(t, e, 0, "Keys to the House", state.ZBattlefield),
		moveByName(t, e, 0, "Keys to the House", state.ZBattlefield),
	}
	addMana(t, e, 0, "WWWWWWWWW")
	roomID := moveByName(t, e, 0, "Corpus Ability Room", state.ZBattlefield)
	designateRoomCastFace(e, roomID)
	o := e.G.Obj(roomID)
	if o == nil || o.Zone != state.ZBattlefield || !isRoom(o) || !o.DoorUnlocked(int(o.FaceIdx)) || o.DoorUnlocked(1-int(o.FaceIdx)) {
		t.Fatalf("precondition: expected battlefield Room with only its cast face unlocked: %+v", o)
	}
	if len(o.Card.Faces[0].Abilities) == 0 || len(o.Card.Faces[1].Abilities) == 0 {
		t.Fatal("precondition: both Room faces need distinct activated abilities")
	}
	frontN := o.Card.Faces[0].Abilities[0].ParamStr(cards.PKNumCards)
	backN := o.Card.Faces[1].Abilities[0].ParamStr(cards.PKNumCards)
	if frontN == "" || backN == "" || frontN == backN {
		t.Fatalf("precondition: Room door ability values do not distinguish the faces: %q vs %q", frontN, backN)
	}
	flat, ok := o.PileAbilityIndex(0, 0)
	if !ok || flat != 0 {
		t.Fatalf("precondition: cast-face ability has unexpected flat index %d, %v", flat, ok)
	}
	originalAbility, ok := o.PileAbilityAt(flat)
	if !ok || originalAbility.SA == nil {
		t.Fatal("precondition: unlocked cast-face ability does not resolve at its flat index")
	}
	if !roomAbilityOffered(e, roomID) {
		t.Fatal("precondition: unlocked cast-face ability is not offered")
	}

	// Exercise Keys to the House's printed ability to unlock the alternate face.
	activateCorpusDoorAction(t, e, keysIDs[0], keysAbility, roomID, 0, "Corpus Ability Chamber") // unlock
	if !o.DoorUnlocked(1-int(o.FaceIdx)) || !roomAbilityOffered(e, roomID) {
		t.Fatal("unlocking the alternate door changed or hid the cast-face ability")
	}
	// Both designations are now unlocked; ask Keys to relock the cast face.
	activateCorpusDoorAction(t, e, keysIDs[1], keysAbility, roomID, 1, "Corpus Ability Room") // lock
	if o.DoorUnlocked(int(o.FaceIdx)) {
		t.Fatal("Keys' lock choice did not lock the cast/designated face")
	}
	if !o.DoorUnlocked(1 - int(o.FaceIdx)) {
		t.Fatal("locking the cast face also locked the still-unlocked alternate door")
	}
	if roomAbilityOffered(e, roomID) {
		t.Fatal("locked cast-face ability remains offered")
	}
	if _, ok := o.PileAbilityAt(flat); ok {
		t.Fatal("locked cast-face ability still resolves at its reserved flat index")
	}
	// The stale flat slot remains reserved but must not pass activation's
	// current-state revalidation.
	e.beginActivation(0, decision.Option{Kind: "ability", Obj: roomID, Ability: flat})
	if e.cast != nil {
		t.Fatal("locked cast-face stale ability index was accepted")
	}
	if got, ok := o.PileAbilityIndex(0, 0); !ok || got != flat {
		t.Fatalf("lock changed the stable flat ability index: got %d, %v; want %d", got, ok, flat)
	}
	activateCorpusDoorAction(t, e, keysIDs[2], keysAbility, roomID, 0, "Corpus Ability Room") // unlock cast face again
	if !o.DoorUnlocked(int(o.FaceIdx)) || !o.DoorUnlocked(1-int(o.FaceIdx)) || !roomAbilityOffered(e, roomID) {
		t.Fatal("unlocking the cast face did not restore its ability while preserving the alternate door")
	}
	if got, ok := o.PileAbilityIndex(0, 0); !ok || got != flat {
		t.Fatalf("unlock changed the stable flat ability index: got %d, %v; want %d", got, ok, flat)
	}
	if restored, ok := o.PileAbilityAt(flat); !ok || restored.SA != originalAbility.SA {
		t.Fatalf("unlock did not restore the original ability at flat index %d: %+v, %v", flat, restored, ok)
	}
}

func roomAbilityOffered(e *Engine, id state.ObjID) bool {
	for _, opt := range e.legalActions(0) {
		if opt.Kind == "ability" && opt.Obj == id {
			return true
		}
	}
	return false
}

func activateCorpusDoorAction(t *testing.T, e *Engine, source state.ObjID, ability int, room state.ObjID, choice int, doorName string) {
	t.Helper()
	e.beginActivation(0, decision.Option{Kind: "ability", Obj: source, Ability: ability})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Keys activation did not ask for a Room target: %+v", d)
	}
	pick := -1
	for _, option := range d.Options {
		if option.Obj == room {
			pick = option.Index
		}
	}
	if pick < 0 {
		t.Fatalf("Keys did not offer the test Room as a target: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
		t.Fatalf("submit Keys' Room target: %v", err)
	}
	for i := 0; i < 4; i++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			break
		}
		pass := -1
		for _, option := range d.Options {
			if option.Kind == "pass" {
				pass = option.Index
				break
			}
		}
		if pass < 0 {
			t.Fatalf("no pass option while resolving Keys: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
			t.Fatalf("pass priority to resolve Keys: %v", err)
		}
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("Keys did not ask the lock/unlock choice: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{choice}}); err != nil {
		t.Fatalf("submit Keys lock/unlock choice: %v", err)
	}
	d = e.Pending()
	if d != nil && d.Kind == decision.KChoose {
		pick = -1
		for _, option := range d.Options {
			if option.Label == doorName {
				pick = option.Index
			}
		}
		if pick < 0 {
			t.Fatalf("Keys did not offer door %q: %+v", doorName, d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
			t.Fatalf("submit Keys door choice: %v", err)
		}
	} else if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("unexpected decision after Keys lock/unlock choice: %+v", d)
	}
	passUntilStackEmpty(t, e, 30)
}
