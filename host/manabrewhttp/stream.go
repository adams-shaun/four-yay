package manabrewhttp

import (
	"fmt"
	"net/http"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/manabrew"
	"github.com/adams-shaun/gorge/protocol"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// stream is GET .../stream (§5.2): one SSE connection for the claim's own
// seat. On connect it sends the current state and the open prompt if one
// exists; from then on a host.Session subscribed in FOCUS mode is used
// purely as a wake-up — its own frames are native-wire and are never
// forwarded (invariant 5.3: "the host Session subscription is used only as
// a wake-up") — so every board change anywhere at the table re-polls this
// seat's own ViewAtSeat/Pending and re-translates. Auto-pass (§6.5) and a
// queued concede (G-2) are serviced here, silently, before anything reaches
// the client: they are answered as ordinary logged intents and the next
// wake-up (their own burst) drives the next refresh.
func (h *handler) stream(w http.ResponseWriter, r *http.Request) {
	t, k, ok := matchKey(w, r)
	if !ok {
		return
	}
	claim, ok := h.claimForMatch(w, r, t)
	if !ok {
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}

	sc := h.connFor(connKey{table: t, match: k, seat: claim.Seat})
	push := make(chan mb.EngineMessage, 8)
	detach := sc.attach(push)
	defer detach()

	sess := h.reg.OpenSession()
	defer h.reg.CloseSession(sess.ID)
	if err := h.reg.Subscribe(sess, t, protocol.ModeFocus); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	write := func(msg mb.EngineMessage) error {
		if h.opts.WriteTimeout > 0 {
			_ = rc.SetWriteDeadline(time.Now().Add(h.opts.WriteTimeout))
		}
		raw, err := mb.Encode(msg)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "data: %s\n\n", raw)
		return err
	}

	tr := manabrew.New(string(t), int64(k), h.opts.Text)

	var (
		havePrompt   bool
		lastPromptID int64
	)

	// refresh is the one place this connection turns live engine state into
	// what the client sees. It returns a non-nil error only for a write
	// failure (the caller must stop the connection); every other outcome —
	// including "nothing changed for this seat" and "an auto-pass/concede
	// was serviced silently" — returns nil and produces no frames, relying
	// on the next wake-up (that action's own burst) to drive the follow-up.
	refresh := func() error {
		v, d, err := h.pollView(t, k, claim.Seat)
		if err != nil {
			return nil // a transient lookup failure; try again on the next wake-up
		}

		if d != nil && d.Kind == decision.KPriority {
			if sc.takeConcedeQueued() {
				if in, cerr := manabrew.ConcedeIntent(d); cerr == nil {
					_ = h.reg.SubmitIntent(t, k, claim.Seat, in)
					return nil
				}
			}
			if pol := sc.getPolicy(); pol != nil {
				if in, doPass := tr.AutoPass(pol, d, v); doPass {
					_ = h.reg.SubmitIntent(t, k, claim.Seat, in)
					return nil
				}
				// ShouldPass is false: either the policy was never active
				// (a plain pass) or a stop condition was reached (§6.5).
				// Either way there is nothing left for it to do.
				sc.setPolicy(nil)
			}
		} else if d != nil {
			// A non-priority prompt clears any running policy — its own
			// "cleared by the first non-priority prompt" rule.
			sc.setPolicy(nil)
		}

		if err := write(tr.State(v)); err != nil {
			return err
		}

		if d == nil {
			havePrompt = false
			return nil
		}
		if havePrompt && lastPromptID == int64(d.Seq) {
			return nil // this seat's own open prompt has not changed
		}
		dv := v
		pm, perr := tr.Prompt(d, &dv)
		if perr != nil {
			// A decision kind whose MB translator ticket has not landed
			// yet: loud, not silent, and never a crash (see the mapping
			// program's collision map — this transport must work against
			// whatever internal/manabrew currently covers).
			_ = write(mb.EngineMessage{Value: mb.ErrorMessage{Error: mb.ProtocolError{
				Code: mb.CodeInvalidShape, Message: "prompt kind " + string(d.Kind) + " has no ManaBrew translation yet"}}})
			return nil
		}
		if err := write(mb.EngineMessage{Value: pm}); err != nil {
			return err
		}
		havePrompt, lastPromptID = true, pm.PromptID
		return nil
	}

	if err := refresh(); err != nil {
		return
	}
	fl.Flush()

	keep := time.NewTicker(h.opts.KeepAlive)
	defer keep.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case _, open := <-sess.Out():
			if !open {
				return
			}
		drain:
			for {
				select {
				case _, open2 := <-sess.Out():
					if !open2 {
						return
					}
				default:
					break drain
				}
			}
			if err := refresh(); err != nil {
				return
			}
			fl.Flush()
		case msg := <-push:
			if err := write(msg); err != nil {
				return
			}
			fl.Flush()
		case <-keep.C:
			if h.opts.WriteTimeout > 0 {
				_ = rc.SetWriteDeadline(time.Now().Add(h.opts.WriteTimeout))
			}
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}
