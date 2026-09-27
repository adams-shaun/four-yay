package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func (h *handler) tables(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.reg.LobbyTables())
}

// DeckInfo is one selectable deck loaded by the server. Format is the game
// format used by CreateGame (not free-form authoring metadata), so the client
// can filter the catalogue without reproducing the server's pool rules.
type DeckInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Format    string `json:"format"`
	Archetype string `json:"archetype"`
	Commander string `json:"commander,omitempty"`
}

func (h *handler) decks(w http.ResponseWriter, r *http.Request) {
	decks := h.opts.Decks
	if decks == nil {
		decks = []DeckInfo{}
	}
	writeJSON(w, http.StatusOK, decks)
}

func (h *handler) matches(w http.ResponseWriter, r *http.Request) {
	ms, err := h.reg.Matches(host.TableID(r.PathValue("t")))
	if err != nil {
		writeHostError(w, err)
		return
	}
	if ms == nil {
		ms = []protocol.MatchInfo{}
	}
	writeJSON(w, http.StatusOK, ms)
}

// matchKey parses {t} and {k}; a non-numeric k is a 400.
func matchKey(w http.ResponseWriter, r *http.Request) (host.TableID, int, bool) {
	k, err := strconv.Atoi(r.PathValue("k"))
	if err != nil || k < 1 {
		writeError(w, http.StatusBadRequest, "bad_request", "match index must be a positive integer")
		return "", 0, false
	}
	return host.TableID(r.PathValue("t")), k, true
}

// uintQuery parses an optional unsigned query parameter.
func uintQuery(w http.ResponseWriter, r *http.Request, name string) (uint64, bool, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, false, true
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", name+" must be a non-negative integer")
		return 0, false, false
	}
	return n, true, true
}

// claimForTable is the one authorization fence every seat-authorised handler
// uses. nil Options.Seat means spectator-only (403); an absent token is 401;
// and a claim for a different table is 403. Claims without a table are the
// pre-deploy format and intentionally stop working rather than retaining the
// old cross-table privilege.
func (h *handler) claimForTable(w http.ResponseWriter, r *http.Request, table host.TableID) (SeatClaim, bool) {
	if h.opts.Seat == nil {
		writeError(w, http.StatusForbidden, "forbidden", "this server is spectator-only: no seat claims")
		return SeatClaim{}, false
	}
	claim, ok := h.opts.Seat(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "request does not hold a seat claim")
		return SeatClaim{}, false
	}
	if claim.Table != table {
		writeError(w, http.StatusForbidden, "forbidden", "claim does not hold a seat at this table")
		return SeatClaim{}, false
	}
	return claim, true
}

// seatFromQuery resolves the optional ?seat= parameter. scoped is false (and
// ok true) when the parameter is absent: no claim is consulted and today's
// spectator behaviour runs byte-identical. A present ?seat= must name a seat
// the request actually holds — the resolver must answer, and the claim's seat
// must equal the requested one — so seat A can never read seat B's hand or
// decision through any endpoint that takes a seat parameter. ok false means
// the handler already wrote the rejection. (SubmitIntent takes no ?seat; it
// acts through claimForTable alone, and the intent body's own Player field is
// the fence.)
func (h *handler) seatFromQuery(w http.ResponseWriter, r *http.Request, table host.TableID) (seat state.PlayerID, scoped, ok bool) {
	raw := r.URL.Query().Get("seat")
	if raw == "" {
		return 0, false, true
	}
	n, err := strconv.ParseUint(raw, 10, 8)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "seat must be a non-negative integer")
		return 0, false, false
	}
	claim, granted := h.claimForTable(w, r, table)
	if !granted {
		return 0, false, false
	}
	if claim.Seat != state.PlayerID(n) {
		writeError(w, http.StatusForbidden, "forbidden",
			fmt.Sprintf("claim holds seat %d, not the requested seat %d", claim.Seat, n))
		return 0, false, false
	}
	return claim.Seat, true, true
}

func (h *handler) view(w http.ResponseWriter, r *http.Request) {
	t, k, ok := matchKey(w, r)
	if !ok {
		return
	}
	seq, given, ok := uintQuery(w, r, "seq")
	if !ok {
		return
	}
	seat, scoped, ok := h.seatFromQuery(w, r, t)
	if !ok {
		return
	}
	if !given {
		ms, err := h.reg.Matches(t)
		if err != nil {
			writeHostError(w, err)
			return
		}
		found := false
		for _, m := range ms {
			if m.Match == k && m.Events > 0 {
				seq, found = uint64(m.Events-1), true
			}
		}
		if !found {
			writeHostError(w, host.ErrNotFound)
			return
		}
	}
	var (
		v   view.View
		err error
	)
	if scoped {
		v, err = h.reg.ViewAtSeat(t, k, seq, seat)
	} else {
		v, err = h.reg.ViewAt(t, k, seq)
	}
	if err != nil {
		writeHostError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *handler) events(w http.ResponseWriter, r *http.Request) {
	t, k, ok := matchKey(w, r)
	if !ok {
		return
	}
	since, _, ok := uintQuery(w, r, "since")
	if !ok {
		return
	}
	seat, scoped, ok := h.seatFromQuery(w, r, t)
	if !ok {
		return
	}
	var (
		evs []protocol.EventBody
		err error
	)
	if scoped {
		evs, err = h.reg.EventsSeat(t, k, since, seat)
	} else {
		evs, err = h.reg.Events(t, k, since)
	}
	if err != nil {
		writeHostError(w, err)
		return
	}
	if evs == nil {
		evs = []protocol.EventBody{}
	}
	writeJSON(w, http.StatusOK, evs)
}

// pending serves the decision currently asked of one seat (M2e-2). It needs
// ?seat= — there is no spectator notion of a pending decision — and the seat
// must be the claim's own, so a seat can never read another seat's pending
// decision. Rejections (match not live, seat not human, nothing pending) are
// 409 conflicts whose body carries the registry's own reason.
func (h *handler) pending(w http.ResponseWriter, r *http.Request) {
	t, k, ok := matchKey(w, r)
	if !ok {
		return
	}
	seat, scoped, ok := h.seatFromQuery(w, r, t)
	if !ok {
		return
	}
	if !scoped {
		writeError(w, http.StatusBadRequest, "bad_request", "pending requires a seat")
		return
	}
	d, err := h.reg.Pending(t, k, seat)
	if err != nil {
		writeSeatError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// intent answers the pending decision through the claim's own seat. It takes
// no ?seat= parameter: the claim alone names the seat, and the intent body's
// own Player field is validated against the pending decision by
// decision.Decision.Validate inside the registry — the one fence — so a claim
// can never answer a decision asked of another seat. Every rejection becomes
// an HTTP status plus the registry's reason body (audit item 17); a 204 means
// the intent was accepted and the parked seat is free.
func (h *handler) intent(w http.ResponseWriter, r *http.Request) {
	t, k, ok := matchKey(w, r)
	if !ok {
		return
	}
	claim, granted := h.claimForTable(w, r, t)
	if !granted {
		return
	}
	var in decision.Intent
	if !decodeBody(w, r, &in) {
		return
	}
	if err := h.reg.SubmitIntent(t, k, claim.Seat, in); err != nil {
		writeSeatError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// undo is the POST /api/tables/{t}/matches/{k}/undo action (dispatch
// fb-20260911T201015Z, the undo slice): it asks the registry to roll the live
// match back in place to the decision the requesting seat last answered
// (host/undo.go). It takes no ?seat= and no body: the claim alone names the
// requesting seat, which the registry fences to the table's sole human seat —
// a table with two or more distinct human seats is refused (no consent flow
// exists) — exactly as Pending/SubmitIntent fence to a human seat. 204 means
// the signal was posted to the live loop; the rewind itself is observed on the
// stream (the rewind frame, then the re-derived decision at the new head) or
// through Pending/ViewAt.
func (h *handler) undo(w http.ResponseWriter, r *http.Request) {
	t, k, ok := matchKey(w, r)
	if !ok {
		return
	}
	claim, granted := h.claimForTable(w, r, t)
	if !granted {
		return
	}
	if err := h.reg.Undo(t, k, claim.Seat); err != nil {
		writeSeatError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeBody reads a small JSON body; anything malformed is a 400.
func decodeBody(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body: "+err.Error())
		return false
	}
	return true
}

// session resolves the body's session id; unknown is a 404 (it may have
// expired — the client must reconnect the stream to get a new one).
func (h *handler) session(w http.ResponseWriter, id string) (*host.Session, bool) {
	s, ok := h.reg.Session(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "unknown session "+id)
		return nil, false
	}
	return s, true
}

func (h *handler) subscribe(w http.ResponseWriter, r *http.Request) {
	var req protocol.Subscribe
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Mode != protocol.ModeOverview && req.Mode != protocol.ModeFocus {
		writeError(w, http.StatusBadRequest, "bad_request", "mode must be overview or focus")
		return
	}
	s, ok := h.session(w, req.Session)
	if !ok {
		return
	}
	if err := h.reg.Subscribe(s, host.TableID(req.Table), req.Mode); err != nil {
		if err == host.ErrNotFound {
			writeHostError(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	var req protocol.Unsubscribe
	if !decodeBody(w, r, &req) {
		return
	}
	s, ok := h.session(w, req.Session)
	if !ok {
		return
	}
	if err := h.reg.Unsubscribe(s, host.TableID(req.Table)); err != nil {
		writeHostError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateGameRequest is the POST /api/games body. Deck ids are optional: an
// omitted id leaves that seat's assignment to the server, preserving the
// original random flow. They are filename stems from GET /api/decks.
type CreateGameRequest struct {
	Format    string `json:"format"`
	HumanDeck string `json:"human_deck,omitempty"`
	BotDeck   string `json:"bot_deck,omitempty"`
	BotPolicy string `json:"bot_policy,omitempty"`
	// Mulligans is the optional London mulligan allowance for the created
	// game (finding fb-20260914T114629Z-6c81e4d6: a play-vs-bot game could
	// mulligan exactly once, with no knob at any layer). It is a pointer so
	// 0 — "0 disables the pre-game round" — is distinguishable from an
	// omitted field, which keeps the server default.
	Mulligans *int `json:"mulligans,omitempty"`
}

// CreateGameOptions is the validated request handed across Options.CreateGame.
// A struct keeps this public-ish seam extensible without another positional
// parameter each time the create flow gains an option.
type CreateGameOptions struct {
	Format    host.Format
	HumanDeck string
	BotDeck   string
	BotPolicy string
	// Mulligans is the validated mulligan allowance; nil keeps the builder's
	// server-side default.
	Mulligans *int
}

// CreateGameResponse is what a successful POST /api/games returns: the new
// table's identity plus the human seat's bearer token and the join path the
// client follows. Token is the only place the seat credential leaves the
// server (the caller owns it), so the response is the whole of the
// hand-off; a viewer replaying the match needs only the table id and seed,
// both here and on the match's own wire records.
type CreateGameResponse struct {
	Table     string `json:"table"`
	Match     int    `json:"match"`
	Seed      uint64 `json:"seed"`
	Seat      int    `json:"seat"`
	Token     string `json:"token"`
	BotPolicy string `json:"bot_policy"`
	// Join is the base-relative path the human opens to sit in the seat,
	// carrying the seat and its token: /t/<table>?seat=N&token=….
	Join string `json:"join"`
}

// games serves POST /api/games: it decodes the format, hands the request to
// the configured CreateGame builder and returns the new game's identity. The
// builder owns deck pools, seeding and the token mint; the handler only
// resolves the format name to a host.Format (400 on an unknown one) and
// turns a builder failure into a 400 so the client can show it. With no
// CreateGame configured the endpoint is not part of this server and answers
// 404 just like any other unserved path.
func (h *handler) games(w http.ResponseWriter, r *http.Request) {
	if h.opts.CreateGame == nil {
		writeError(w, http.StatusNotFound, "not_found", "play-vs-bot games are not enabled on this server")
		return
	}
	var req CreateGameRequest
	if !decodeBody(w, r, &req) {
		return
	}
	format := host.FormatConstructed
	if req.Format != "" {
		f, err := host.ParseFormat(req.Format)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		format = f
	}
	if req.Mulligans != nil && (*req.Mulligans < 0 || *req.Mulligans > 6) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"mulligans must be between 0 and 6")
		return
	}
	policy, err := host.NormalizeBotPolicy(req.BotPolicy)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	resp, err := h.opts.CreateGame(CreateGameOptions{
		Format: format, HumanDeck: req.HumanDeck, BotDeck: req.BotDeck, BotPolicy: policy, Mulligans: req.Mulligans,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
