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
