package manabrewhttp

import (
	"encoding/json"
	"net/http"

	"github.com/adams-shaun/gorge/internal/manabrew"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// state is GET .../state (§5.2): a one-shot state plus the open prompt, for
// a poll-only client that never opens a stream. It reads exactly what the
// stream's own initial frame would send and nothing more — no wake-up, no
// auto-pass, no concede queue: a poll-only client drives its own pace and is
// expected to call this endpoint again to see the effect of its own POST
// .../send, or of any other seat's play.
func (h *handler) state(w http.ResponseWriter, r *http.Request) {
	t, k, ok := matchKey(w, r)
	if !ok {
		return
	}
	claim, ok := h.claimForMatch(w, r, t)
	if !ok {
		return
	}
	v, d, err := h.pollView(t, k, claim.Seat)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	tr := manabrew.New(string(t), int64(k), h.opts.Text)
	msgs := []mb.EngineMessage{tr.State(v)}
	// Synthetic ack-only prompts for everything this seat has not been shown
	// yet (MBX-2), between the state and the open prompt, exactly as the
	// stream's refresh delivers them; the same seatConn watermark makes the
	// delivery exactly-once across BOTH paths, so a client that alternates
	// poll and stream never sees the same reveal twice.
	msgs = append(msgs, h.drainSynthetic(t, k, claim.Seat, tr, v)...)
	if d != nil {
		dv := v
		if pm, perr := tr.Prompt(d, &dv); perr == nil {
			msgs = append(msgs, mb.EngineMessage{Value: pm})
		}
		// perr != nil (an unmapped decision kind) leaves the poll response
		// as state-only, same as the stream's own degrade path — a client
		// polling for a prompt this server cannot yet translate sees no
		// prompt rather than a crash or a malformed one.
	}
	if v.Over {
		// The terminal gameOver prompt, exactly once per seat (the seatConn
		// latch), after the final state -- the scoping spec Appendix A row
		// "end of match (View.Over) -> gameOver prompt". The latch is
		// seat-wide, not per-connection: whichever path (this poll or a
		// stream) observes Over first consumes it, and every later poll or
		// reconnect for the same seat is state-only. The re-served state
		// already carries GameViewDto.gameOver, so that client is not left
		// without the information.
		if sc := h.connFor(connKey{table: t, match: k, seat: claim.Seat}); sc.markGameOver() {
			msgs = append(msgs, mb.EngineMessage{Value: tr.GameOver(&v)})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(msgs)
}
