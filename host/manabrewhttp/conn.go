package manabrewhttp

import (
	"sync"

	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/internal/manabrew"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
)

// connKey names one seat's connection state, shared between a GET stream and
// any POST send for the same seat of the same match — they are ordinarily
// different HTTP requests (a client may send without a stream open at all,
// per §5.2's poll-only agents), so this is the only place the two meet.
type connKey struct {
	table host.TableID
	match int
	seat  state.PlayerID
}

// seatConn is one seat's cross-request state: the auto-pass policy a pass
// response installs (§6.5 — internal/manabrew.PassPolicy carries the rule,
// this struct is only where the transport keeps the live instance between
// requests), whether a concede directive is queued behind a non-priority ask
// (G-2), and the currently open stream's push channel, if any, so a
// rejected /send response can be pushed onto it (§5.2: "also pushed on the
// stream as an error message"). It is never removed once created — the
// population is bounded by the table's real seats across its real matches,
// which is small and finite, unlike a per-request allocation.
type seatConn struct {
	mu            sync.Mutex
	policy        *manabrew.PassPolicy
	concedeQueued bool
	// push is the active stream's inbound channel for out-of-band messages
	// (currently: a /send rejection). At most one stream is considered
	// "active" at a time; a newer stream's connect simply overwrites the
	// pointer, and each stream's own defer clears it only if it is still
	// the one it installed (an old, already-superseded stream's teardown
	// must never blank a newer stream's live channel).
	push chan mb.EngineMessage

	// Synthetic-prompt state (MBX-2). synSeq is the seat's event watermark:
	// the first event Seq not yet scanned for synthetic prompts, so a prompt
	// is minted and delivered exactly once per seat no matter how many times
	// the stream's refresh or a /state poll re-reads the match. synOpen maps
	// the promptIds of delivered-but-unacknowledged synthetic prompts to the
	// exact PromptMessages delivered, so a POST .../send ack (which names only
	// an id) is validated against what this seat was actually shown.
	// gameOverSent latches the terminal prompt's one-per-seat delivery.
	synSeq       uint64
	synOpen      map[int64]mb.PromptMessage
	gameOverSent bool
}

// connFor returns the seat's connection state, creating it on first use.
func (h *handler) connFor(key connKey) *seatConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.conns[key]
	if !ok {
		c = &seatConn{}
		h.conns[key] = c
	}
	return c
}

// attach installs ch as this seat's active push target and returns a detach
// function the stream's defer calls on the way out.
func (c *seatConn) attach(ch chan mb.EngineMessage) (detach func()) {
	c.mu.Lock()
	c.push = ch
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		if c.push == ch {
			c.push = nil
		}
		c.mu.Unlock()
	}
}

// pushMsg delivers msg to the seat's active stream, if one is open. The send
// never blocks: a full or absent channel just drops it, exactly as
// host.Session.push treats a slow reader — the message is a courtesy replay
// of what the HTTP reply already carries, never the only copy.
func (c *seatConn) pushMsg(msg mb.EngineMessage) {
	c.mu.Lock()
	ch := c.push
	c.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

// setPolicy installs (or clears, with nil) the seat's auto-pass policy.
func (c *seatConn) setPolicy(p *manabrew.PassPolicy) {
	c.mu.Lock()
	c.policy = p
	c.mu.Unlock()
}

// getPolicy reads the seat's current auto-pass policy.
func (c *seatConn) getPolicy() *manabrew.PassPolicy {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.policy
}

// queueConcede marks a concede directive pending the seat's next priority
// decision (G-2).
func (c *seatConn) queueConcede() {
	c.mu.Lock()
	c.concedeQueued = true
	c.mu.Unlock()
}

// takeConcedeQueued reports and clears whether a concede is queued: the
// stream loop calls it once per priority decision it is about to show, so a
// queued concession is consumed exactly once.
func (c *seatConn) takeConcedeQueued() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	q := c.concedeQueued
	c.concedeQueued = false
	return q
}

// syntheticWatermark reads the seat's event watermark for synthetic prompts
// (see drainSynthetic for the delivery protocol it drives).
func (c *seatConn) syntheticWatermark() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.synSeq
}

// deliverSynthetic advances the watermark from since to next (the first seq
// NOT covered by this scan) and records every delivered prompt in synOpen
// under the same lock, so two concurrent drains (a stream refresh and a
// /state poll, or two polls) can never double-deliver: the second caller
// re-reads the watermark under the lock and finds it already advanced past
// its own since, and delivers nothing.
func (c *seatConn) deliverSynthetic(since, next uint64, msgs []mb.EngineMessage) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.synSeq != since {
		return false // another drain covered this range already
	}
	c.synSeq = next
	for _, m := range msgs {
		pm := m.Value.(mb.PromptMessage)
		if c.synOpen == nil {
			c.synOpen = map[int64]mb.PromptMessage{}
		}
		c.synOpen[pm.PromptID] = pm
	}
	return true
}

// openSynthetic returns the exact PromptMessage this seat was shown under
// the given synthetic promptId, if one is still open (delivered, not yet
// acknowledged). This is the /send ack path's only validation input: the
// ack carries just the id, so it must be checked against what was delivered.
func (c *seatConn) openSynthetic(id int64) (mb.PromptMessage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pm, ok := c.synOpen[id]
	return pm, ok
}

// ackSynthetic closes a delivered synthetic prompt: its ack was accepted, so
// a late duplicate ack falls through to the ordinary response path (and
// there reads as stalePrompt, the same shape a late ordinary answer gets).
func (c *seatConn) ackSynthetic(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.synOpen, id)
}

// markGameOver reports whether THIS seat's terminal gameOver prompt is still
// owed, latching it so exactly one is ever delivered (MBX-2). The latch is
// seat-wide, not per-connection: a stream refresh, a /state poll and a
// reconnect all consult the same latch, so whichever observes Over first
// consumes it and every later path for that seat is state-only. A stream
// that connects after match end is therefore deliberately state-only -- see
// state.go and TestReconnectGameOverIsDeliberatelyStateOnly.
func (c *seatConn) markGameOver() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gameOverSent {
		return false
	}
	c.gameOverSent = true
	return true
}
