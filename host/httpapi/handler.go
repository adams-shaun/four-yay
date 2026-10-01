package httpapi

import (
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/state"
)

// SeatClaim is what Options.Seat resolves a request to when it may act as a
// seat. A claim names both the table and seat it may act for. Every
// seat-authorised endpoint verifies the request table through claimForTable
// before it reaches the Registry, so a token for seat 0 at one table cannot
// read or act as seat 0 at another. A zero Table is an old unbound claim and
// is deliberately rejected after deployment.
type SeatClaim struct {
	Table host.TableID
	Seat  state.PlayerID
}

// Options configures the handler. Every duration defaults when zero.
type Options struct {
	Authorize      func(*http.Request) error // nil allows everyone; an error is a 401
	WidgetInterval time.Duration             // SSE widget flush cadence; 0 = 250ms (Task 15)
	KeepAlive      time.Duration             // SSE comment cadence; 0 = 15s (Task 15)
	ResumeGrace    time.Duration             // how long a disconnected session survives; 0 = 30s (Task 15)
	Web            fs.FS                     // the built client (has index.html); nil = 503 for non-API paths (Task 15)
	// Seat resolves which seat a request may act for, separately from
	// Authorize: nil — the default — keeps today's spectator-only behaviour,
	// nobody may act as a seat, so any request that names one is refused
	// outright. A non-nil resolver that declines a request is refused like
	// an Authorize failure (401); a claim whose table or seat disagrees with
	// the request is refused (403); only an agreeing claim reaches the
	// seat-scoped Registry methods. See SeatClaim.
	Seat func(*http.Request) (SeatClaim, bool) // nil = spectator-only (M2e-2)
	// CreateGame, when non-nil, arms the POST /api/games endpoint that seats
	// a human against a bot on demand (Task ui11). It builds and starts a
	// fresh single-shot table for the requested format and returns its
	// identity (table, seed, the human seat and its bearer token), which the
	// client follows to the join URL. nil — the default — leaves the
	// play-vs-bot endpoint disabled, so a server that never wired the flow
	// keeps today's surface byte-identical: the endpoint answers 404 like
	// any path it does not serve. It is the boundary that knows the deck
	// pools, the loader and the token store; the HTTP layer only decodes the
	// body and calls it.
	CreateGame func(CreateGameOptions) (CreateGameResponse, error)
	// Decks is the immutable deck catalogue exposed by GET /api/decks. The
	// server builds it from the same files it loaded into CreateGame's pools,
	// so clients can enumerate choices without guessing deck ids.
	Decks []DeckInfo
	// BotPolicies is the immutable hosted-bot listing exposed by
	// GET /api/bot-policies. The server builds it once at startup from the
	// offered set, so the response is pure configuration: it carries no live
	// table or match state. nil serves the empty listing with status 200.
	BotPolicies *protocol.BotPolicyList
}

func (o Options) withDefaults() Options {
	if o.WidgetInterval == 0 {
		o.WidgetInterval = 250 * time.Millisecond
	}
	if o.KeepAlive == 0 {
		o.KeepAlive = 15 * time.Second
	}
	if o.ResumeGrace == 0 {
		o.ResumeGrace = 30 * time.Second
	}
	return o
}

type handler struct {
	reg  *host.Registry
	opts Options

	mu       sync.Mutex
	grace    map[string]*graceTimer // session id -> pending close after disconnect (Task 15)
	graceGen uint64                 // monotonic token so stale grace callbacks recognise themselves
}

// newHandler builds the handler behind NewHandler. Tests in this package use
// it to observe the grace map directly, which is the only way to assert the
// reconnect cancellation as a state transition rather than as a deadline.
func newHandler(r *host.Registry, o Options) (*handler, http.Handler) {
	h := &handler{reg: r, opts: o.withDefaults(), grace: map[string]*graceTimer{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tables", h.tables)
	mux.HandleFunc("GET /api/decks", h.decks)
	mux.HandleFunc("GET /api/bot-policies", h.botPolicies)
	mux.HandleFunc("GET /api/stats/decks", h.deckStats)
	mux.HandleFunc("GET /api/tables/{t}/matches", h.matches)
	mux.HandleFunc("GET /api/tables/{t}/matches/{k}/view", h.view)
	mux.HandleFunc("GET /api/tables/{t}/matches/{k}/events", h.events)
	mux.HandleFunc("GET /api/tables/{t}/matches/{k}/pending", h.pending)
	mux.HandleFunc("POST /api/tables/{t}/matches/{k}/intent", h.intent)
	mux.HandleFunc("POST /api/tables/{t}/matches/{k}/undo", h.undo)
	mux.HandleFunc("POST /api/subscribe", h.subscribe)
	mux.HandleFunc("POST /api/unsubscribe", h.unsubscribe)
	mux.HandleFunc("POST /api/games", h.games)
	// Method-less twins of every API pattern: the mux prefers the
	// method-specific pattern, so these only ever see the wrong method and
	// answer 405 in JSON rather than the mux's default text body.
	for _, p := range []string{"/api/tables", "/api/decks", "/api/bot-policies", "/api/stats/decks", "/api/tables/{t}/matches", "/api/tables/{t}/matches/{k}/view",
		"/api/tables/{t}/matches/{k}/events", "/api/tables/{t}/matches/{k}/pending", "/api/tables/{t}/matches/{k}/intent",
		"/api/tables/{t}/matches/{k}/undo",
		"/api/subscribe", "/api/unsubscribe", "/api/games", "/api/stream"} {
		mux.HandleFunc(p, methodNotAllowed)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	h.mountStream(mux) // Task 15
	h.mountStatic(mux) // Task 15
	return h, h.authorized(mux)
}

// NewHandler routes the API and, when Options.Web is set, the client. It is
// the package's only exported constructor; callers that need to observe the
// handler's internals (tests) should use newHandler.
func NewHandler(r *host.Registry, o Options) http.Handler {
	_, mux := newHandler(r, o)
	return mux
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", r.Method+" is not allowed here")
}

// authorized runs the hook before every request.
func (h *handler) authorized(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.opts.Authorize != nil {
			if err := h.opts.Authorize(r); err != nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
