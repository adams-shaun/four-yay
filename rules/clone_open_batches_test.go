package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCloneCarriesOpenZoneAndDiscardBatches pins that a clone taken while a
// ChangeZoneTable$ zone batch or an api:Discard discard batch is open -- both
// stay open across a mid-resolution suspension, which is an intent boundary
// -- carries the open bracket and its entries as its own copy: closing the
// clone's batch patches the clone's queued trigger exactly as the original's
// close would, and leaves the original's batch open and untouched.
func TestCloneCarriesOpenZoneAndDiscardBatches(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 3, Names: names, Decks: decks})
	e.Advance()
	drive(t, e, newTestBot(3), 10)

	const src = state.ObjID(1)
	key := triggerKey{Source: src, Idx: 0}
	e.pendingTriggers = append(e.pendingTriggers[:0], pendingTrigger{Source: src, Idx: 0}, pendingTrigger{Source: src, Idx: 0})

	e.openZoneBatch()
	e.zoneBatchIdx = map[zoneBatchKey]int{{triggerKey: key}: 0}
	e.zoneBatchLog = []zoneBatchEntry{{key: key, idx: 0, moved: []state.Target{{Obj: 7}, {Obj: 8}}}}
	e.openDiscardBatch()
	e.discardBatchIdx = map[triggerKey]int{key: 1}
	e.discardBatchLog = []discardBatchEntry{{key: key, idx: 1, amount: 2, discarded: []state.Target{{Obj: 9}}}}

	c := e.Clone()
	if !c.zoneBatchOpen || c.zoneBatchDepth != 1 || len(c.zoneBatchLog) != 1 || len(c.zoneBatchIdx) != 1 {
		t.Fatalf("clone dropped the open zone batch: open %v depth %d log %d idx %d",
			c.zoneBatchOpen, c.zoneBatchDepth, len(c.zoneBatchLog), len(c.zoneBatchIdx))
	}
	if !c.discardBatchOpen || c.discardBatchDepth != 1 || len(c.discardBatchLog) != 1 || len(c.discardBatchIdx) != 1 {
		t.Fatalf("clone dropped the open discard batch: open %v depth %d log %d idx %d",
			c.discardBatchOpen, c.discardBatchDepth, len(c.discardBatchLog), len(c.discardBatchIdx))
	}
	// The clone owns its entries: an in-place write never reaches the original.
	c.zoneBatchLog[0].moved[0].Obj = 99
	c.discardBatchLog[0].discarded[0].Obj = 99
	c.zoneBatchIdx[zoneBatchKey{triggerKey: key}] = 5
	if e.zoneBatchLog[0].moved[0].Obj != 7 || e.discardBatchLog[0].discarded[0].Obj != 9 || e.zoneBatchIdx[zoneBatchKey{triggerKey: key}] != 0 {
		t.Fatal("clone shares an open batch's entries with the original")
	}
	c.zoneBatchLog[0].moved[0].Obj = 7
	c.discardBatchLog[0].discarded[0].Obj = 9

	c.closeZoneBatch()
	c.closeDiscardBatch()
	if got := c.pendingTriggers[0].Ctx.Remembered; len(got) != 2 || got[0].Obj != 7 || got[1].Obj != 8 {
		t.Fatalf("closing the clone's zone batch patched Remembered = %v, want the batch's moved set [7 8]", got)
	}
	if got := c.pendingTriggers[1].Ctx.TriggerContext.TriggerAmount; got != 2 {
		t.Fatalf("closing the clone's discard batch patched TriggerAmount = %d, want 2", got)
	}
	if !e.zoneBatchOpen || !e.discardBatchOpen {
		t.Fatal("closing the clone's batches closed the original's")
	}
	if len(e.pendingTriggers[0].Ctx.Remembered) != 0 || e.pendingTriggers[1].Ctx.TriggerContext.TriggerAmount != 0 {
		t.Fatal("closing the clone's batches patched the original's queued triggers")
	}
}
