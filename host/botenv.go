package host

import (
	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// matchFeeds owns one searchseat.Feed per non-human bots.EnvSeat of a match.
// It is created once at the top of play, after the seats are final, and lives
// on the match struct; the match goroutine is its only writer and every
// access is under m.mu (Observe is called from projectNext, Record from the
// Submit critical section). A nil *matchFeeds — the case of a match with no
// EnvSeat, including every pure-bot table today — is the allocation-free fast
// path: observe and record return immediately without building anything.
//
// This is the Create/Observe/Record half of spec
// 2026-09-28-hosted-bot-packages §5.2. The honest root and the undo rebuild
// are separate tickets (BP-08, BP-09); this file only starts the feed,
// captures a frame at every decision of every player, and records the
// accepted intent, so that a live match's feed already equals what
// RebuildFeed derives from its log.
type matchFeeds struct {
	// bySlot is parallel to the match's []seat.Seat: one feed for each slot
	// whose seat implements bots.EnvSeat, nil for every other slot. Feeds are
	// per-seat, never per-decision: the actor is fixed for the match.
	bySlot []*searchseat.Feed
	// stopped marks a feed the host has retired early because its answer
	// translation failed (RecordAnswer error). A capture failure already flips
	// the feed's own Live() false; a record failure does not, and
	// searchseat.Feed exposes no Stop, so the host keeps its own bit. A
	// stopped feed is never observed or recorded again: its history would
	// silently miss an answer and corrupt every later read, so the seat plays
	// its fallback from then on (the bench crashes instead; a live table
	// plays on, §5.2 Record).
	stopped []bool
}

// newMatchFeeds builds the per-seat feeds for seats. It returns nil when no
// non-human seat implements bots.EnvSeat, so the fast path allocates nothing
// at all (not even the backing slice).
func newMatchFeeds(seats []seat.Seat) *matchFeeds {
	var f *matchFeeds
	for i, s := range seats {
		if _, ok := s.(*HumanSeat); ok {
			continue
		}
		if _, ok := s.(bots.EnvSeat); !ok {
			continue
		}
		if f == nil {
			f = &matchFeeds{
				bySlot:  make([]*searchseat.Feed, len(seats)),
				stopped: make([]bool, len(seats)),
			}
		}
		f.bySlot[i] = searchseat.NewFeed(state.PlayerID(i))
	}
	return f
}

// observe captures one frame on every live feed at the engine's current
// decision. It must run at every decision of every player — the human's
// included — because the sampler's epoch constraints need every player's
// bursts (searchseat.Feed's doc). A nil receiver is the no-EnvSeat fast path
// and touches nothing. A capture failure stops that one feed; it never
// crashes the match, and the seat falls back from then on (§5.2 Observe).
func (f *matchFeeds) observe(e *rules.Engine) {
	if f == nil {
		return
	}
	for i, fd := range f.bySlot {
		if fd == nil || f.stopped[i] || !fd.Live() {
			continue
		}
		if _, ok := fd.Observe(e); !ok {
			f.stopped[i] = true
		}
	}
}

// record files the accepted intent on the feed of the decision's actor, if
// there is one and it is still live. d is the pending decision the accepted
// intent answered (never the seat's first, possibly-refused, answer); in is
// the intent the engine actually took, which may be a refusal-ladder rung
// (§5.5). It runs inside the Submit critical section, after a successful
// m.e.Submit, under m.mu. A nil receiver or a non-EnvSeat actor is a no-op; a
// translation error stops that feed for the rest of the game.
func (f *matchFeeds) record(d *decision.Decision, in decision.Intent) {
	if f == nil || d == nil {
		return
	}
	i := int(d.Player)
	if i < 0 || i >= len(f.bySlot) {
		return
	}
	fd := f.bySlot[i]
	if fd == nil || f.stopped[i] || !fd.Live() {
		return
	}
	if err := fd.RecordAnswer(d, in); err != nil {
		f.stopped[i] = true
	}
}

// envData builds the parkedData of one bots.EnvSeat decision — BP-08's Env
// dispatch (spec 2026-09-28-hosted-bot-packages §5.1). It runs inside
// projectNext's exclusive m.mu section and is the ONLY place a match builds
// a bots.Env:
//
//   - View: exactly what a plain Seat gets from projectNext (the same
//     ProjectForControlled projection, Round filled), so an adapter that
//     falls back to a View answer answers like a non-Env seat.
//   - Board: the deciding seat's botpolicy.Board from the shared brd buffer,
//     built the same way a BoardSeat's is — the fallback the adapters play
//     when the root is refused (or WantsEnv was false, which parks on the
//     BoardSeat branch instead of here).
//   - Search: the actor's own feed (captured through THIS decision by the
//     observe above), and the honest root built by searchseat.HonestRoot
//     from that feed — the only engine an EnvSeat ever touches, its hidden
//     cards redealt from the pool the seat's observation derives. The root
//     seed is bots.RootSeed over this seat's per-seat seed (m.seed XOR the
//     slot plus one, the same derivation seats use for their own RNG) and
//     the decision's seq, so a root is a pure function of the feed, the seat
//     and the decision index — never of the real hidden cards.
//   - A refused or failed deal returns a nil engine; its fail-closed reason
//     becomes env.RootRefused, m.rootRefusals counts one, and the seat
//     answers from env.Board (or env.View) — never from the live engine
//     (§5.3: a seat that cannot see an honest world does not get the real
//     one). Delivering the Env even on a refusal keeps the adapters' fallback
//     honest: sb-tactical wants to see planner-nil explicitly, and search or
//     az-redeal play DecideBoard(env.Board), so the decision is still ASKED.
//
// The returned parkedData carries the whole Env; parkSeat hands it to
// DecideEnv outside the lock and nothing reads it again afterwards.
func (m *match) envData(p state.PlayerID, dc *decision.Decision, brd *botpolicy.Board) *parkedData {
	slot := int(p)
	seed := bots.RootSeed(m.seed^uint64(slot+1), dc.Seq)
	setup := searchprobe.PublicGame{
		Names: m.cfg.Names, Decks: m.cfg.Decks, Tokens: m.cfg.Tokens, StartingLife: m.cfg.StartingLife,
	}
	var feed *searchseat.Feed
	if m.feeds != nil && slot >= 0 && slot < len(m.feeds.bySlot) {
		feed = m.feeds.bySlot[slot]
		if m.feeds.stopped[slot] {
			// §5.2: a feed that failed its capture or its Record stops for the
			// rest of the game and "the seat plays its fallback from then on".
			// Handing HonestRoot nil instead of the stopped feed routes that
			// refusal through its one refusal home ("no live observation
			// feed") rather than building roots from a History that will miss
			// every future recorded answer — a root like that would still be
			// leak-free but would no longer keep what the seat knows.
			feed = nil
		}
	}
	root, reason := searchseat.HonestRoot(setup, feed, m.e, seed)
	if root == nil {
		m.rootRefusals++
	}
	v := view.ProjectForControlled(m.e.G, m.e, p, view.Seat, controlledSeats(m.e.G, p), dc)
	v.Round = view.RoundOf(m.e.G, m.e.L.Events)
	b := botpolicy.BoardFromGameInto(m.e.G, m.e, p, brd)
	return &parkedData{
		p:   p,
		v:   v,
		dc:  *dc,
		brd: b,
		env: &bots.Env{
			View:        v,
			Board:       b,
			Search:      searchseat.Env{Setup: setup, Engine: root, Board: b, Feed: feed},
			RootRefused: reason,
		},
	}
}
