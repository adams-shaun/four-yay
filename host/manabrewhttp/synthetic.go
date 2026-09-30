package manabrewhttp

import (
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/internal/manabrew"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// drainSynthetic is the MBX-2 delivery step both the stream's refresh and
// the /state poll run after writing their state frame: it turns the match
// events this seat has not yet been shown into synthetic ack-only prompts
// (a public reveal, a dice roll -- internal/manabrew.Translator.Synthetic)
// and returns them in event order. Delivery is exactly once per seat, driven
// by the seatConn's event watermark: the scan reads EventsSeat from the
// watermark (inclusive, the same redaction the native /events route serves),
// the translator mints prompts, and deliverSynthetic advances the watermark
// and records the delivered prompts for the /send ack path under one lock --
// so a concurrent second drain finds the watermark already advanced and
// delivers nothing, rather than re-showing the same reveal. A transient
// lookup failure (the match is mid-write, or k names a match that has not
// started) delivers nothing and leaves the watermark untouched: the next
// refresh or poll retries the same range.
//
// The returned prompts are ADDITIVE to the ordinary prompt flow, never a
// substitute: they never block the next real prompt (an unanswered synthetic
// is simply left open in synOpen until acked or superseded by match end),
// they never reach the engine (AcknowledgeSynthetic produces no intent), and
// their negative promptIds live in a namespace disjoint from decision Seqs
// (internal/manabrew/synthetic.go).
func (h *handler) drainSynthetic(t host.TableID, k int, seat state.PlayerID, tr *manabrew.Translator, v view.View) []mb.EngineMessage {
	sc := h.connFor(connKey{table: t, match: k, seat: seat})
	since := sc.syntheticWatermark()
	evs, err := h.reg.EventsSeat(t, k, since, seat)
	if err != nil || len(evs) == 0 {
		return nil
	}
	// EventsSeat is since..head inclusive; next is the first seq a later
	// scan must start at. (protocol.Seq starts at 1, so the last body's Seq
	// is the head.)
	next := evs[len(evs)-1].Event.Seq + 1
	dv := v
	msgs := tr.Synthetic(evs, &dv)
	if sc.deliverSynthetic(since, next, msgs) {
		return msgs
	}
	// Another drain (a concurrent poll, or the stream's own wake-up loop)
	// already covered since..head: deliver nothing rather than duplicate.
	return nil
}
