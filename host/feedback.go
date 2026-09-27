package host

import (
	"fmt"
	"os"
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// This file is the server-side half of the feedback capture (task
// fbrepro1): when a player files a bug report naming a table, the report
// must carry enough to reproduce the match they were in — because the
// event log a deploy wipes is the one thing a later retelling loses. The
// shapes here are what Registry.SnapshotForFeedback hands the caller
// (cmd/gorged's feedback store), which marshals them beside the report.

// FeedbackMatch is match.json's content: the sidecar of the table's current
// match — the same fields host/persist.go persists, in the same shape —
// plus DeckCards, the deck CONTENTS (card name per card, in deck order,
// one list per seat), and Sideboards, the sideboard CONTENTS the same way
// (card name per card, in sideboard order, one list per seat; a seat with
// no sideboard carries nil). Sideboards are genesis configuration exactly
// like Decks — the engine mints their objects before the first event — so a
// replay rebuilt from match.json alone needs them recorded here, or the
// genesis object IDs shift and the replayed match diverges at the first
// shuffle. Like DeckCards, the list is what a replay is actually built
// from, so the snapshot never depends on the deck files still existing.
// The fields mirror sidecar's on purpose: a test pins that the
// two marshal the same keys, so the snapshot cannot drift from the
// persisted shape without failing.
type FeedbackMatch struct {
	Table        string              `json:"table"`
	Match        int                 `json:"match"`
	Seed         uint64              `json:"seed"`
	Seats        []protocol.SeatInfo `json:"seats"`
	Names        []string            `json:"names"`
	PlayerNames  []string            `json:"player_names,omitempty"`
	Decks        []string            `json:"decks"`
	DeckCards    [][]string          `json:"deck_cards"`
	Sideboards   [][]string          `json:"sideboards,omitempty"`
	Spectator    string              `json:"spectator"`
	State        string              `json:"state"`
	Result       string              `json:"result,omitempty"`
	Winner       *uint8              `json:"winner"`
	Head         string              `json:"head,omitempty"`
	Events       int                 `json:"events"`
	Turns        int32               `json:"turns"`
	Reason       string              `json:"reason,omitempty"`
	Mulligans    int                 `json:"mulligans,omitempty"`
	Format       Format              `json:"format,omitempty"`
	StartingLife int32               `json:"starting_life,omitempty"`
	Commanders   [][]int             `json:"commanders,omitempty"`
	BotPolicy    string              `json:"bot_policy"`

	// NameUniverse and NameUniverseNames mirror the sidecar's fields of the
	// same names, and for the same reason the deck contents and the token
	// scripts are here: a name-card universe is a match MODE, not data the
	// log can recover. A match played with one poses NameCard asks the
	// legacy no-universe path never poses, so a replay rebuilt from
	// match.json alone with the mode missing finds no decision pending at
	// the recorded intent and refuses. NameUniverseNames is the immutable
	// sorted label list that match actually offered, so a corpus addition
	// or rename after the report was filed cannot renumber a recorded
	// numeric name choice. A capture from a pre-feature match carries
	// neither field, which reads back as the legacy path (R-8.4).
	NameUniverse bool `json:"name_universe,omitempty"`
	// NameUniverseNames is stripped from a COMMITTED fixture (it is ~24k
	// entries, half a megabyte per fixture): the committed shape keeps only
	// the mode bit and the replay re-derives the list from the live corpus,
	// diverging on a corpus pin move exactly the way a token-stripped
	// fixture does, and saying so.
	NameUniverseNames []string `json:"name_universe_names,omitempty"`

	// Tokens carries the raw token scripts — keyed by file stem, the exact
	// spelling a card's TokenScript$ parameter uses — behind the match's
	// rules.Config.Tokens. rules.Config.Tokens is replay input (rules.New
	// copies it onto Game.Tokens, and events.Apply's TokenCreate case mints
	// from it), so a replay rebuilt from match.json alone needs it exactly
	// as it needed the deck contents: without it, the replayed engine's
	// effToken emits an "unknown token script" Note where the log records a
	// TokenCreate, and the replay diverges at that event.
	//
	// The scripts are shipped as text, not as compiled cards, because a
	// compiled *cards.Card cannot survive a JSON round trip: its derived
	// P/T, CMC and colour-identity fields are unexported (cards/ir.go), so
	// a JSON decode would leave them zero — a 1/1 Goblin replayed as a 0/0.
	// A token Card IS fully determined by its script text, and the repro
	// tool recompiles each entry with cards.ParseBytes + Card.Link +
	// ApplyIntrinsics — the exact pipeline compileScripts runs the corpus
	// through — so the rebuilt cards are the ones the match played with.
	//
	// The whole cfg.Tokens is captured, not only the tokens some deck seems
	// to reference: the engine never enumerates which tokens a deck can
	// create (TokenScript$ reaches effToken through spell abilities,
	// triggers, replacements, keyword expansions and SVar bodies alike), so
	// any filtered capture could miss an indirect creator and reproduce the
	// very unknown-token divergence this field exists to prevent. The whole
	// pinned corpus's token text is ~150 KB, noise beside the snapshot cap.
	Tokens map[string]string `json:"tokens,omitempty"`

	// TokensUnread lists the tokens whose script text could not be read
	// back, as "<stem>: <reason>". The live match's cfg.Tokens holds the
	// compiled cards; only each card's own source path needs reading back,
	// and a miss (a corpus file moved or vanished since the match started,
	// or a synthetic token compiled from bytes with no file behind it) is
	// recorded here rather than dropped silently: a replay that later
	// reaches one of these stems will diverge the same way a missing
	// Tokens table would, and match.json says why.
	TokensUnread []string `json:"tokens_unread,omitempty"`
}

// FeedbackLog is log.json's content: the match's events.Log as of the
// snapshot — Seed, Events and Intents, the exact replay input — plus where
// that prefix stood: Head (the chain hash at that prefix, what a replay
// must reproduce), IntentCount, and the game's Turn, Step and
// Priority/Active players read off the same locked instant the log was
// copied. The Log is embedded, so its own keys (seed, events, intents)
// marshal at the top level and a caller unmarshals straight back into an
// events.Log.
type FeedbackLog struct {
	events.Log
	Head        string         `json:"head"`
	IntentCount int            `json:"intent_count"`
	Turn        int32          `json:"turn"`
	Step        string         `json:"step"`
	Priority    state.PlayerID `json:"priority"`
	Active      state.PlayerID `json:"active"`
}

// FeedbackSnapshot is everything a feedback report captures about the
// table's match: the replayable configuration (Match), the event log as of
// the submit (Log), and — when the report named a seat — that seat's own
// redacted view of the board (View). View is nil when the report named no
// seat, or when the projection failed (ViewErr says why): a failed view
// never discards the match and log, which are the part a reproduction
// needs, and it can never carry another seat's hidden zones — View is
// projected with seat (viewer-scoped) visibility, the same redaction
// ViewAtSeat serves the player's own client.
type FeedbackSnapshot struct {
	Match   FeedbackMatch
	Log     FeedbackLog
	View    *view.View
	ViewErr string
}

// SnapshotForFeedback captures the state of table id's CURRENT match — the
// live one, or, when the table is between matches, the one it just
// finished, so a report filed moments after game over still snapshots — as
// of one consistent instant. The match's read lock is held exactly long
// enough to copy what the snapshot needs (the log via Log.Clone, the
// sidecar, the deck contents, the game facts, the pending decision), and
// the seat's view is projected OUTSIDE the lock from those copies, on the
// replayed engine, exactly as ViewAtSeat does — a snapshot therefore never
// blocks a concurrent Submit for longer than the copies take, and never
// drives the live engine.
//
// seat is the reporting seat's own seat, or nil when the report named
// none. A named seat out of range is an error, not a nil view: a feedback
// caller should learn it asked for a seat the table does not have.
//
// The returned snapshot is a stable copy: later bursts append to the live
// match but never to it, so marshalling it needs no lock.
func (r *Registry) SnapshotForFeedback(id TableID, seat *state.PlayerID) (FeedbackSnapshot, error) {
	r.mu.RLock()
	t, ok := r.tables[id]
	r.mu.RUnlock()
	if !ok {
		return FeedbackSnapshot{}, ErrNotFound
	}
	t.mu.RLock()
	m := t.cur
	if m == nil && len(t.history) > 0 {
		m = t.history[len(t.history)-1]
	}
	// Persistence mode retains no finished match in memory (see
	// table.history): between matches the one that just finished is the
	// last archived sidecar, rebuilt from disk through lookup's single-slot
	// cache. t.archived is ascending by match index.
	last := 0
	if m == nil && len(t.archived) > 0 {
		last = t.archived[len(t.archived)-1].Match
	}
	t.mu.RUnlock()
	if m == nil && last > 0 {
		_, lm, err := r.lookup(id, last)
		if err != nil {
			return FeedbackSnapshot{}, fmt.Errorf("host: table %s match %d: %w", id, last, err)
		}
		m = lm
	}
	if m == nil {
		return FeedbackSnapshot{}, fmt.Errorf("host: table %s has no match", id)
	}

	m.mu.RLock()
	l := m.e.L.Clone()
	// reconcileLog's trim is for crash-cut FILES, and this clone is not one:
	// under the read lock no Submit is in flight (the match loop holds the
	// write lock across Submit and its bookkeeping), so every event in the
	// cloned log — including the tail past the last DecisionAsk a burst's
	// own post-ask continuation emitted (fb-20260915T094418Z) — belongs to
	// a completed burst. Trimming it here cut REAL events, desynced
	// log.json's head (taken over the full chain) from its event list, and
	// lost the seat view (headSeq named a seq the trimmed copy does not
	// carry — the "view unavailable: seq beyond head" reports). Only a
	// crashed match's log can carry an orphan tail and poison intent (D15),
	// and only that shape still gets the trim.
	if m.state == protocol.MatchCrashed {
		reconcileLog(l)
	}
	sc := m.sidecar()
	deckCards := make([][]string, len(m.cfg.Decks))
	for i, d := range m.cfg.Decks {
		names := make([]string, len(d))
		for j, c := range d {
			names[j] = c.Faces[0].Name
		}
		deckCards[i] = names
	}
	var sideboardCards [][]string
	if len(m.cfg.Sideboards) > 0 {
		sideboardCards = make([][]string, len(m.cfg.Sideboards))
		for i, sb := range m.cfg.Sideboards {
			names := make([]string, len(sb))
			for j, c := range sb {
				names[j] = c.Faces[0].Name
			}
			sideboardCards[i] = names
		}
	}
	// Game facts as of the same instant the log was copied: the log's tail
	// burst is complete and the game state is the state those events left,
	// so Turn/Step/Priority/Active describe exactly the prefix recorded
	// below.
	turn, step, prio, active := m.e.G.Turn, m.e.G.Step.String(), m.e.G.Priority, m.e.G.Active
	if seat != nil && int(*seat) >= len(sc.Seats) {
		n := len(sc.Seats)
		m.mu.RUnlock()
		return FeedbackSnapshot{}, fmt.Errorf("host: table %s: seat %d out of range (%d seats)", id, *seat, n)
	}
	// The pending decision, copied the way ViewAtSeat copies it: the view
	// carries the decision only when the projection lands exactly at the
	// live head and the engine is parked on one — a historical prefix never
	// misrepresents what was pending.
	var d *decision.Decision
	if p := m.e.Pending(); p != nil {
		cp := *p
		cp.Options = append([]decision.Option(nil), p.Options...)
		d = &cp
	}
	snaps := append([]snapshot(nil), m.snaps...)
	cfg := m.cfg
	// headSeq comes from the copy the view below is projected from, not
	// from the live log: the two agree for every non-crashed match (the
	// clone is not trimmed), but a crashed match's reconciled copy is
	// shorter than the live log, and a head past the copy's own end would
	// fail the projection with a beyond-head error before the replay even
	// ran (the defect fb-20260915T094418Z measured on the captured view).
	headSeq := head(m)
	if n := uint64(len(l.Events)); n > 0 && headSeq >= n {
		headSeq = n - 1
	}
	m.mu.RUnlock()

	// The token scripts are read back outside the lock: cfg.Tokens is the
	// registry-owned, never-mutated map the match was built with (the same
	// discipline viewAt's replay already relies on for cfg), and the file
	// reads are disk I/O no concurrent Submit should wait behind.
	tokens, tokensUnread := tokenScripts(cfg)

	snap := FeedbackSnapshot{
		Match: feedbackMatch(sc, deckCards, sideboardCards, tokens, tokensUnread),
		Log: FeedbackLog{
			Log:         *l,
			Head:        l.Head(),
			IntentCount: len(l.Intents),
			Turn:        turn,
			Step:        step,
			Priority:    prio,
			Active:      active,
		},
	}
	if seat != nil {
		v, err := viewAt(cfg, l, snaps, headSeq, *seat, view.Seat, d)
		if err != nil {
			snap.ViewErr = err.Error()
		} else {
			snap.View = &v
		}
	}
	return snap, nil
}

// feedbackMatch copies the sidecar's fields into the exported FeedbackMatch
// and attaches the deck contents, the sideboard contents and the token
// scripts.
func feedbackMatch(sc sidecar, deckCards [][]string, sideboards [][]string,
	tokens map[string]string, tokensUnread []string) FeedbackMatch {
	return FeedbackMatch{
		Table: sc.Table, Match: sc.Match, Seed: sc.Seed, Seats: sc.Seats, Names: sc.Names,
		PlayerNames: sc.PlayerNames, Decks: sc.Decks, DeckCards: deckCards, Sideboards: sideboards,
		Spectator: sc.Spectator,
		State:     sc.State, Result: sc.Result, Winner: sc.Winner, Head: sc.Head, Events: sc.Events,
		Turns: sc.Turns, Reason: sc.Reason, Mulligans: sc.Mulligans, Format: sc.Format,
		StartingLife: sc.StartingLife, Commanders: sc.Commanders, BotPolicy: sc.BotPolicy,
		NameUniverse:      sc.NameUniverse,
		NameUniverseNames: append([]string(nil), sc.NameUniverseNames...),
		Tokens:            tokens, TokensUnread: tokensUnread,
	}
}

// tokenScripts reads the raw script text behind cfg.Tokens back off each
// token card's own source path — the file that path names is what the
// compiled card was parsed from, so reading it back is exact. See
// FeedbackMatch.Tokens for why text and not the compiled cards, and why the
// whole map rather than a deck-filtered subset. Stems are walked in sorted
// order so TokensUnread (the only order-sensitive output) is deterministic;
// the map itself marshals with sorted keys either way.
func tokenScripts(cfg rules.Config) (map[string]string, []string) {
	if len(cfg.Tokens) == 0 {
		return nil, nil
	}
	stems := make([]string, 0, len(cfg.Tokens))
	for s := range cfg.Tokens {
		stems = append(stems, s)
	}
	sort.Strings(stems)
	out := make(map[string]string, len(stems))
	var unread []string
	for _, s := range stems {
		c := cfg.Tokens[s]
		if c == nil || c.Path == "" {
			unread = append(unread, s+": token card carries no source path")
			continue
		}
		src, err := os.ReadFile(c.Path)
		if err != nil {
			unread = append(unread, s+": "+err.Error())
			continue
		}
		out[s] = string(src)
	}
	return out, unread
}
