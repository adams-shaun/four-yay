package manabrewhttp

import (
	"io"
	"net/http"

	"github.com/adams-shaun/gorge/internal/manabrew"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// maxSendBody bounds one ClientToServerMessage the same way host/httpapi
// bounds its own request bodies (decodeBody): small on purpose, since every
// legal message here is a handful of ids and booleans.
const maxSendBody = 64 << 10

// send is POST .../send (§5.2): the client's one message, decoded leniently
// and mapped against the seat's currently pending decision through
// internal/manabrew.Translator.TranslateResponse. A response becomes a
// decision.Intent submitted through Registry.SubmitIntent; a directive
// answers or queues a concession (G-2); restoreSnapshot asks Registry.Undo.
// A rejection is written to the HTTP reply AND pushed onto any open stream
// for the seat (§5.2), so a client polling GET .../state without a stream
// still sees exactly what a streamed client would.
func (h *handler) send(w http.ResponseWriter, r *http.Request) {
	t, k, ok := matchKey(w, r)
	if !ok {
		return
	}
	claim, ok := h.claimForMatch(w, r, t)
	if !ok {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxSendBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "reading request body: "+err.Error())
		return
	}
	if len(body) > maxSendBody {
		writeError(w, http.StatusBadRequest, "bad_request", "request body too large")
		return
	}
	var msg mb.ClientMessage
	if _, err := mb.Decode(body, &msg); err != nil {
		// A body that is not a well-formed ClientToServerMessage at all
		// (bad JSON, or neither "response" nor "directive") cannot be
		// mapped to any of the protocol's five error codes either — §6.4's
		// table starts from an already-decoded message. It is the same
		// "never became a ManaBrew message" shape as a claim failure.
		writeError(w, http.StatusBadRequest, "bad_request", "malformed ManaBrew message: "+err.Error())
		return
	}
	sc := h.connFor(connKey{table: t, match: k, seat: claim.Seat})
	tr := manabrew.New(string(t), int64(k), h.opts.Text)

	v, d, verr := h.pollView(t, k, claim.Seat)
	if verr != nil {
		pe := mb.ProtocolError{Code: mb.CodeStalePrompt, Message: "no match state to answer against: " + verr.Error()}
		writeProtocolError(w, pe)
		return
	}
	var pending *manabrew.Pending
	if d != nil {
		if pm, perr := tr.Prompt(d, &v); perr == nil {
			pending = &manabrew.Pending{Prompt: pm, Decision: d, View: v}
		}
		// perr != nil is an unmapped decision kind: pending stays nil, and
		// TranslateResponse's own "no prompt is open" branch reports
		// stalePrompt — accurate, since this server never actually offered
		// a prompt for that decision.
	}

	outcome := tr.TranslateResponse(msg, pending, claim.Seat)
	switch {
	case outcome.Err != nil:
		sc.pushMsg(mb.EngineMessage{Value: mb.ErrorMessage{Error: *outcome.Err}})
		writeProtocolError(w, *outcome.Err)
	case outcome.Queued:
		sc.queueConcede()
		w.WriteHeader(http.StatusNoContent)
	case outcome.Undo != nil:
		if err := h.reg.Undo(t, k, claim.Seat); err != nil {
			pe := mb.ProtocolError{Code: mb.CodeInvalidShape, Message: err.Error(), PromptID: promptIDOf(pending)}
			sc.pushMsg(mb.EngineMessage{Value: mb.ErrorMessage{Error: pe}})
			writeProtocolError(w, pe)
			return
		}
		sc.setPolicy(nil)
		w.WriteHeader(http.StatusNoContent)
	case outcome.Intent != nil:
		if err := h.reg.SubmitIntent(t, k, claim.Seat, *outcome.Intent); err != nil {
			// TranslateResponse validated the intent against the decision
			// this handler itself just fetched; a rejection here means the
			// live decision moved between that fetch and this submit (a
			// genuine race, not a client mistake), which is exactly what
			// stalePrompt describes.
			pe := mb.ProtocolError{Code: mb.CodeStalePrompt, Message: err.Error(), PromptID: promptIDOf(pending)}
			sc.pushMsg(mb.EngineMessage{Value: mb.ErrorMessage{Error: pe}})
			writeProtocolError(w, pe)
			return
		}
		if outcome.Policy != nil {
			sc.setPolicy(outcome.Policy)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		// TranslateResponse always sets exactly one arm; reaching here is a
		// translator bug, not a client mistake.
		writeError(w, http.StatusInternalServerError, "internal", "translator produced no outcome")
	}
}

// promptIDOf reads the open prompt's id for an error body, or nil when there
// was none to begin with.
func promptIDOf(p *manabrew.Pending) *int64 {
	if p == nil {
		return nil
	}
	id := p.Prompt.PromptID
	return &id
}
