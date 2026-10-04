package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TestZndrspltFriendCopy pins the sole measured Choices$/Chooser$ shape under
// the resolution kernel: the remembered friend, not the spell controller,
// receives the KChoose, and selecting a non-first eligible creature copies
// THAT creature under the friend.
func TestZndrspltFriendCopy(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Zndrsplt's Judgment")},
		[]*cards.Card{lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Hill Giant")})
	z := moveByName(t, e, 0, "Zndrsplt's Judgment", state.ZHand)
	friendBear := moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	friendGiant := moveByName(t, e, 1, "Hill Giant", state.ZBattlefield)
	if e.G.Obj(friendBear).Zone != state.ZBattlefield || e.G.Obj(friendGiant).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: friend's creatures not on battlefield: bear=%s giant=%s",
			e.G.Obj(friendBear).Zone, e.G.Obj(friendGiant).Zone)
	}
	sa := resolveSourceFaceSA(t, e, z, "DBClone")
	e.pending = nil // the resolution runs from a quiet engine, as from a pass
	e.probe(func() {
		effects.Resolve(e, &effects.Ctx{Source: z, Controller: 0,
			Remembered: []state.Target{{Player: 1, IsPlayer: true}}}, sa)
	})

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the friend's KChoose, got %+v", d)
	}
	if d.Player != 1 {
		t.Fatalf("the KChoose went to player %d, want the remembered friend 1", d.Player)
	}
	var objs []state.ObjID
	for _, o := range d.Options {
		if o.Obj != 0 {
			objs = append(objs, o.Obj)
		}
	}
	if len(objs) != 2 {
		t.Fatalf("the friend's option pool = %v, want their two creatures", objs)
	}
	chosen := friendGiant
	if objs[0] == friendGiant {
		chosen = friendBear
	}
	pickIdx := -1
	for _, o := range d.Options {
		if o.Obj == chosen {
			pickIdx = o.Index
		}
	}
	if pickIdx < 0 {
		t.Fatalf("chosen creature %d absent from pool %v", chosen, objs)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pickIdx}}); err != nil {
		t.Fatalf("submit friend's choice: %v", err)
	}

	copyID := findTokenCopyOf(t, e, e.G.Obj(chosen).Card, chosen)
	if e.G.Obj(copyID).Zone != state.ZBattlefield {
		t.Fatalf("the copy is not on the battlefield (zone %s)", e.G.Obj(copyID).Zone)
	}
	if !strings.Contains(strings.ToLower(e.G.Obj(copyID).Face().Name), strings.ToLower(e.G.Obj(chosen).Face().Name)) {
		t.Fatalf("the copy %q is not a copy of the chosen %q", e.G.Obj(copyID).Face().Name, e.G.Obj(chosen).Face().Name)
	}
	if got := e.G.Obj(copyID).Controller; got != 1 {
		t.Fatalf("the copy is controlled by player %d, want the friend 1", got)
	}
	noCopyPermanentModNote(t, e, "Choices", "Chooser")
	replayCheck(t, e, cfg)
}
