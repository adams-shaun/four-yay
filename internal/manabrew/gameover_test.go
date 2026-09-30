package manabrew

import (
	"math"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// TestGameOverPromptShape pins the terminal prompt's wire shape: the fixed
// id, the seat it is addressed to, and the no-response gameOver input.
func TestGameOverPromptShape(t *testing.T) {
	tr := New("t1", 1, nil)
	v := view.View{Viewer: 2}
	pm := tr.GameOver(&v)
	if pm.PromptID != GameOverPromptID {
		t.Fatalf("promptId = %d, want the fixed GameOverPromptID %d", pm.PromptID, GameOverPromptID)
	}
	if pm.DecidingPlayerID != "player-2" {
		t.Fatalf("decidingPlayerId = %q, want player-2 (the seat the view belongs to)", pm.DecidingPlayerID)
	}
	if _, ok := pm.Input.Value.(mb.GameOverInput); !ok {
		t.Fatalf("input type = %s, want gameOver (no response)", pm.Input.Value.PromptType())
	}
	// The response side must keep refusing every answer to it: the mapping's
	// "gameOver carries no response" branch (errors.go) drives from the
	// prompt type, not the transport.
	ack := mb.ClientMessage{Value: mb.ClientResponse{Kind: "response", PromptID: pm.PromptID,
		Action: mb.PromptOutput{Type: "gameOver", Output: mb.PromptOutputData{Value: mb.RevealCardsAcknowledged{}}}}}
	outcome := tr.TranslateResponse(ack, &Pending{Prompt: pm, Decision: &decision.Decision{Player: 2}, View: view.View{Viewer: 2}}, 2)
	if outcome.Err == nil || outcome.Err.Code != mb.CodeWrongPromptType {
		t.Fatalf("TranslateResponse(ack to gameOver) = %+v, want CodeWrongPromptType", outcome)
	}
}

// TestPromptIDSpacesAreDisjoint pins MBX-2's namespace fix over the whole
// reachable id range: an ordinary promptId (a bare decision.Seq, >= 0) can
// never equal a synthetic promptId (always < 0 for any event seq the shift
// cannot overflow) or the terminal GameOverPromptID, and the three-way split
// is exhaustive for every id the transport mints.
func TestPromptIDSpacesAreDisjoint(t *testing.T) {
	// Ordinary decision ids: non-negative by construction.
	if id := promptID(&decision.Decision{Seq: 1 << 40}); id < 0 {
		t.Fatalf("ordinary promptId = %d, want >= 0", id)
	}
	// Synthetic ids: negative for every event seq the shift cannot overflow
	// (the doc names 2^55 as the overflow boundary; the reachable magnitude
	// is far below it), and distinct per (seq, n).
	seen := map[int64]bool{}
	for seq := uint64(0); seq < 1<<20; seq += 7919 {
		for n := 0; n < 8; n++ {
			id := syntheticPromptID(seq, n)
			if id >= 0 {
				t.Fatalf("syntheticPromptID(%d, %d) = %d, want < 0", seq, n, id)
			}
			if seen[id] {
				t.Fatalf("syntheticPromptID(%d, %d) = %d repeats an earlier (seq,n) pair", seq, n, id)
			}
			seen[id] = true
		}
	}
	// The boundary the doc names: the shift must not overflow below 2^55.
	if id := syntheticPromptID(1<<55-1, 0); id >= 0 {
		t.Fatalf("syntheticPromptID at the top of the non-overflowing range = %d, want < 0", id)
	}
	// The terminal id is neither a reachable ordinary id nor a reachable
	// synthetic id: it is outside [0, 2^53) (the spec's own safety margin for
	// ordinary ids) and outside the synthetic range (every synthetic id for a
	// non-overflowing seq is > -2^55, i.e. bounded far away from MaxInt64).
	if GameOverPromptID != math.MaxInt64 {
		t.Fatalf("GameOverPromptID = %d, want math.MaxInt64 (the fixed terminal slot)", GameOverPromptID)
	}
	if GameOverPromptID < 1<<53 {
		t.Fatalf("GameOverPromptID = %d collides with the ordinary id range's reachable top", GameOverPromptID)
	}
	if id := syntheticPromptID(1<<55-1, 7); id >= GameOverPromptID {
		t.Fatalf("synthetic id %d can reach the terminal id", id)
	}
}
