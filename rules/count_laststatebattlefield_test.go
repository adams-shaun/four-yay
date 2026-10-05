// Count$LastStateBattlefieldWithFallback — the cast-time battlefield count
// (ticket cli-20261005T092134Z-fed167ca, part 1). OTJ Steer Clear and WOE
// Faerie Fencing read the battlefield "as you cast this spell" through this
// head; Volcanic Wind, Jaws of Stone, Reap, Lifestream's Blessing, Lethal
// Exploit and Flame Discharge carry it too. Forge reads
// castSA.getLastStateBattlefield() and falls back to the CURRENT battlefield
// when no cast-time snapshot exists. This engine keeps no cast-time snapshot,
// so the fallback arm is the whole read: the current battlefield filtered by
// the head's <spec> through the shared Count$Valid zone scan. The pin drives
// the real carrier bodies and moves the board to prove the count follows the
// battlefield rather than a constant.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestLastStateBattlefieldWithFallbackHeadOnSteerClear pins the head against
// Steer Clear's real SVar Y (`Permanent.Mount+YouCtrl`). Two Mounts must read
// 2, none must read 0, and removing one must read 1 — so the read follows the
// board, not a constant.
func TestLastStateBattlefieldWithFallbackHeadOnSteerClear(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	steer := onBoardCard(t, e, 0, corpusCard(t, "Steer Clear"))
	face := e.G.Obj(steer).Face()

	body, ok := face.SVars["Y"]
	if !ok || body != "Count$LastStateBattlefieldWithFallback Permanent.Mount+YouCtrl" {
		t.Fatalf("test precondition: Steer Clear SVar Y = %q (ok %v)", body, ok)
	}
	ctx := effects.NewCtxPtr(steer, 0, effects.CtxInit{SVars: face.SVars})
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("baseline Mount count = %d (ok %v), want evaluated 0", n, ok)
	}

	mountA := onBoard(t, e, 0, "Name:Test Mount A\nTypes:Creature Mount\nPT:2/2\nOracle:x\n")
	onBoard(t, e, 0, "Name:Test Mount B\nTypes:Creature Mount\nPT:2/2\nOracle:x\n")
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 2 {
		t.Fatalf("Mount count with two Mounts = %d (ok %v), want 2", n, ok)
	}

	// Move one Mount off the battlefield: the count must drop. An eventless
	// zone move is enough for the zone scan, which reads the live zone list.
	remaining := make([]state.ObjID, 0, len(e.G.Zone(state.ZBattlefield, 0)))
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if id != mountA {
			remaining = append(remaining, id)
		}
	}
	e.G.SetZone(state.ZBattlefield, 0, remaining)
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 1 {
		t.Fatalf("Mount count with one Mount = %d (ok %v), want 1", n, ok)
	}
}

// TestLastStateBattlefieldWithFallbackCarriersArePinned asserts every corpus
// card that carries the head still carries it, so the census this ticket
// measured cannot silently drift.
func TestLastStateBattlefieldWithFallbackCarriersArePinned(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	carriers := []string{
		"Steer Clear", "Faerie Fencing", "Volcanic Wind", "Lifestream's Blessing",
		"Lethal Exploit", "Reap", "Flame Discharge", "Jaws of Stone",
	}
	for _, name := range carriers {
		card, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus carrier %q is missing", name)
		}
		found := false
		for fi := range card.Faces {
			for _, b := range card.Faces[fi].SVars {
				if strings.HasPrefix(strings.TrimSpace(b), "Count$LastStateBattlefieldWithFallback ") {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("%s no longer carries Count$LastStateBattlefieldWithFallback", name)
		}
	}
}
