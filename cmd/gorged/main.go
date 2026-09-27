package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/host/httpapi"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Command gorged runs perpetual bot tables and serves them to browsers:
// the host library behind a net/http server with the Svelte client
// embedded. It is the M2a deliverable; mtgserve embeds the same packages.
type config struct {
	addr, cards, decks, dir, spectator string
	tables, seats                      int
	pace, cooldown                     time.Duration
	seed                               uint64
	perpetual                          bool
	// mulligans is the London mulligan allowance the served tables hand the
	// engine: each player may take up to this many mulligans between the deal
	// and turn 1 (R-E5-1). Defaults to 1 — a mulligan is the first decision of
	// a real game — and 0 restores the pre-task behaviour exactly (no round).
	mulligans int
	// formatsRaw is the -format flag: a comma-separated table-format list,
	// table i taking formats[(i-1) mod n]. "constructed" (the default) and
	// "commander" are the two names — the smallest thing that lets a person
	// run both formats on one server is a two-element list, no table
	// configuration language. applyFormats parses it into formats at serve
	// time so a malformed list fails before any table is added (R-E3-1).
	formatsRaw string
	formats    []host.Format
	// humansRaw is the -humans flag: a comma-separated list of table t1's
	// slot indices that are real people. Empty stays all-bot, identical to
	// today. applyHumans parses it into humans at serve time so a malformed
	// list fails before any table is added (R-E3-1).
	humansRaw string
	humans    []int
	// vsbot is the -vsbot flag: arm the on-demand play-vs-bot flow so the
	// landing page can seat a human against a bot (POST /api/games). It is
	// OPT-IN — default off — so a server that never turned it on keeps the
	// spectator-only default exactly as it was before the feature existed
	// (Options.Seat stays nil and a seat-scoped request is refused 403,
	// pinned by TestNoHumansIsSpectatorOnly). When on, Options.Seat is armed
	// so the per-game seat token resolves, and a no-token seat request
	// declines to 401 rather than 403.
	vsbot bool
	// botAutoPayMana is the -bot-auto-mana startup switch. It is copied into
	// every startup and on-demand vs-bot table, including human caretakers.
	botAutoPayMana bool
	// autoMana enables payment-plan publication and controls for human seats.
	autoMana bool
	// maxOnDemandTables limits retained private vs-bot games in one process.
	// A bounded refusal preserves each existing game's credentials, feedback
	// capture and replay routes; it never evicts a game behind a live URL.
	maxOnDemandTables int
	// seatToken is the -seat-token flag: a fixed bearer token for the first
	// human slot instead of a random per-slot one. Tests and local use only
	// (R-E3-3) — production runs mint random tokens.
	seatToken string
	// feedback is the -feedback flag: where a player's own bug reports are
	// written (feedback.go). It is deliberately NOT under -dir. The
	// persistence directory is disposable by design — scripts/deploy-demo.sh
	// `rm -rf`s it on every deploy so a config written by an older binary
	// cannot come back with zero-valued fields — which would mean a report
	// filed against a bug was destroyed by the deploy that shipped its fix.
	// A report is the one artifact here that cannot be regenerated.
	feedback string
	// artDir is the -art-dir flag: where the card-art cache (art.go) lives.
	// Empty keeps the historical <dir>/art so every existing deployment, test
	// and local run is byte-for-byte unchanged; a durable path — deploy-demo.sh
	// passes /mnt/sata/gorge-data/art — survives the persistence dir's
	// deploy-time wipe. A stale durable cache can never come back semantically
	// wrong: every key is hashed under artKeyVersion (art.go), so a change in
	// what a fetch writes rotates every key at once and the old bytes are
	// simply never requested again. That is why the wipe's rationale does NOT
	// extend to the art cache.
	artDir string
	// prewarm arms serve's background art-cache prewarm (art.go prewarmArt).
	// The zero value is OFF — the same convention vsbot and feedback follow, so
	// a config built in a test (or any caller that does not go through
	// serveFlags) never fires a single outbound request — but the FLAG default
	// is true (serveFlags), so the real binary always prewarms: the default
	// lives in the flag registration itself, pinned by
	// TestServeFlagPrewarmDefaultsOn, not in a line main must remember to run
	// after Parse. A test (or an operator) that wants the wiring exercised
	// sets it true and points the cache's namedBaseURL at its own fixture
	// first (newServeArtCache).
	prewarm bool
	// prewarmArtOnly is the -prewarm-art-only flag: fill the card-art cache
	// from -decks into artCacheDir() and exit, before any listener opens —
	// what scripts/deploy-demo.sh runs ahead of both servers so a deploy
	// never serves a cold cache, and what `make prewarm-art` runs by hand.
	// Exit code 1 when any name FAILED (a genuine 404 is not a failure) or
	// the pass was stopped by one of the two bounds below.
	prewarmArtOnly bool
	// prewarmArtBudget is the -prewarm-art-budget flag: the wall-clock limit
	// on one -prewarm-art-only pass (0 = none). Art is cosmetic, so the
	// deploy bounds its pre-start fill with it — well under the post-merge
	// hook's 600s lock wait — and starts the servers either way; their
	// background prewarm finishes whatever the bounded pass left.
	prewarmArtBudget time.Duration
	// prewarmArtMaxConsecutiveFailures is the
	// -prewarm-art-max-consecutive-failures flag: stop a -prewarm-art-only
	// pass once this many names in a row have failed (0 = never). A dead or
	// erroring Scryfall then costs N requests, not one per deck name.
	prewarmArtMaxConsecutiveFailures int
}

func main() {
	fs, c := serveFlags()
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gorged:", err)
		os.Exit(2)
	}
	// The one-shot art fill needs no listener, no corpus and no tables — it
	// reads deck JSON and writes the cache — so it dispatches before the
	// bind. A non-zero exit means the cache is not complete; the deploy
	// reports it loudly and starts the servers anyway (art is cosmetic).
	if c.prewarmArtOnly {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		os.Exit(runPrewarmArtOnly(ctx, *c, os.Stderr))
	}
	ln, err := net.Listen("tcp", c.addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gorged:", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, *c, ln); err != nil {
		fmt.Fprintln(os.Stderr, "gorged:", err)
		os.Exit(1)
	}
}

// serveFlags registers every gorged flag on a fresh FlagSet and returns it
// with a POINTER to the config the flags fill. The pointer is load-bearing:
// flag.Value implementations retain the addresses passed to StringVar and
// friends, so returning config by value would leave Parse mutating its escaped
// original while main received an unchanged copy in which every flag still had
// its default value.
//
// Split from main so the DEFAULTS — above all
// prewarm's true, the thing a deploy depends on — are pinned by a test
// (TestServeFlagPrewarmDefaultsOn) instead of living in a line after Parse
// that a refactor could silently drop.
func serveFlags() (*flag.FlagSet, *config) {
	fs := flag.NewFlagSet("gorged", flag.ExitOnError)
	c := new(config)
	fs.StringVar(&c.addr, "addr", ":8080", "listen address")
	fs.StringVar(&c.cards, "cards", ".cards", "corpus directory (ir.gob.gz / cardsfolder)")
	fs.StringVar(&c.decks, "decks", "internal/testutil/decks", "directory of deck JSON files")
	fs.IntVar(&c.tables, "tables", 4, "number of tables")
	fs.IntVar(&c.seats, "seats", 4, "seats per table")
	fs.DurationVar(&c.pace, "pace", 250*time.Millisecond, "sleep after every decision; 0 = as fast as possible")
	fs.DurationVar(&c.cooldown, "cooldown", 5*time.Second, "pause between matches on a perpetual table")
	fs.StringVar(&c.dir, "dir", "gorged-data", "persistence directory")
	fs.StringVar(&c.feedback, "feedback", "feedback", "directory player bug reports are written to (kept OUT of -dir, which deploys wipe)")
	fs.StringVar(&c.artDir, "art-dir", "", "durable card-art cache directory (default <dir>/art, which deploy scripts wipe with the persistence dir)")
	fs.StringVar(&c.spectator, "spectator", "omniscient", "spectator visibility: public or omniscient")
	fs.Uint64Var(&c.seed, "seed", 1, "seed of table 1; table i uses seed+i-1")
	fs.BoolVar(&c.perpetual, "perpetual", true, "start a new match when one ends")
	fs.IntVar(&c.mulligans, "mulligans", 1, "London mulligans per player before turn 1; 0 disables the pre-game round")
	fs.StringVar(&c.formatsRaw, "format", "constructed", "comma-separated table formats (constructed, commander); table i uses formats[i-1 mod n], e.g. -format commander,constructed runs one of each")
	fs.StringVar(&c.humansRaw, "humans", "", "comma-separated slots of table t1 that are real people (e.g. 0,2); t2..tN stay bot tables")
	fs.StringVar(&c.seatToken, "seat-token", "", "fixed bearer token for the first human slot (tests and local use only; default mints a random token per slot)")
	fs.BoolVar(&c.vsbot, "vsbot", false, "arm the on-demand play-vs-bot flow (landing page seats a human against a bot via POST /api/games)")
	fs.BoolVar(&c.botAutoPayMana, "bot-auto-mana", true, "have hosted bots and human-seat caretakers cast through offered automatic mana payment plans")
	fs.BoolVar(&c.autoMana, "auto-mana", true, "enable automatic mana payment plans and controls for human seats (disable with -auto-mana=false for the legacy manual path)")
	fs.IntVar(&c.maxOnDemandTables, "max-on-demand-tables", 32, "maximum retained private play-vs-bot tables per process (0 = unlimited)")
	// The prewarm default is TRUE, deliberately, and lives here — the flag
	// registration — not in a statement main must run after Parse. The
	// background goroutine fills the art cache for every deck card at startup
	// so a fresh deploy never serves a missing-art window; an operator who
	// genuinely wants it off passes -prewarm=false.
	fs.BoolVar(&c.prewarm, "prewarm", true, "prewarm the card-art cache for every card in every dealt deck at startup (disable with -prewarm=false)")
	fs.BoolVar(&c.prewarmArtOnly, "prewarm-art-only", false, "fill the card-art cache from -decks (into -art-dir) and exit; no server is started. Exits non-zero if any name failed")
	fs.DurationVar(&c.prewarmArtBudget, "prewarm-art-budget", 0, "with -prewarm-art-only: stop the fill after this much wall-clock time and exit non-zero (0 = no limit)")
	fs.IntVar(&c.prewarmArtMaxConsecutiveFailures, "prewarm-art-max-consecutive-failures", 0, "with -prewarm-art-only: stop the fill once this many names in a row have failed and exit non-zero (0 = no limit)")
	return fs, c
}

// serve runs until ctx is cancelled, then aborts live matches and shuts
// the server down. Split from main so a test can drive it on a random
// port.
func serve(ctx context.Context, c config, ln net.Listener) error {
	vis, err := view.ParseVisibility(c.spectator)
	if err != nil {
		return err
	}
	if vis == view.Seat {
		return fmt.Errorf("-spectator must be public or omniscient")
	}
	reg, err := cards.OpenCorpus(c.cards)
	if err != nil {
		return fmt.Errorf("opening corpus at %s: %w (run make fetch-cards compile-cards)", c.cards, err)
	}
	// -humans applies to table t1 alone by configuration. Claims are now
	// table-bound, so this is no longer a security restriction; parse it now
	// so a malformed list or "-humans with -tables 0" fails before listening.
	if err := c.applyHumans(); err != nil {
		return err
	}
	if err := c.applyFormats(); err != nil {
		return err
	}
	// The deck directory is split into the two pools the tables deal: the
	// commander-declared files (each validated up front, below) and the
	// constructed rest. One directory serves both formats, so the numbered
	// tables can run a Commander game and a constructed game side by side
	// without any table-level deck configuration.
	cmdPool, conPool, err := splitDecks(reg, c.decks)
	if err != nil {
		return err
	}
	deckCatalogue, err := loadDeckCatalogue(c.decks, cmdPool, conPool)
	if err != nil {
		return err
	}
	if len(cmdPool)+len(conPool) == 0 {
		return fmt.Errorf("no deck files in %s", c.decks)
	}
	// A format with no deck to deal is a configuration error, not a table
	// that fills its seats with the wrong pool: name the gap so a misbuild
	// is fixed, not guessed.
	if c.wantsFormat(host.FormatCommander) && len(cmdPool) == 0 {
		return fmt.Errorf("-format includes commander but no deck in %s names a commander", c.decks)
	}
	if c.wantsFormat(host.FormatConstructed) && len(conPool) == 0 {
		return fmt.Errorf("-format includes constructed but no deck in %s is a constructed deck", c.decks)
	}

	load := deckLoader(reg, c.decks)
	r, err := host.New(c.hostOptions(reg, load))
	if err != nil {
		return err
	}
	if len(r.Tables()) == 0 {
		for _, cfg := range c.tableConfigs(cmdPool, conPool, vis) {
			if err := r.AddTable(cfg); err != nil {
				return err
			}
		}
	}
	if err := r.StartAll(); err != nil {
		return err
	}
	opts := httpapi.Options{Web: webFS(), Decks: deckCatalogue}
	var gate *seatGate
	if len(c.humans) > 0 {
		// R-E3-3: arm Options.Seat with a real token check — one opaque
		// token per human slot, minted at startup. With no humans the
		// resolver stays nil and the server is spectator-only, exactly as
		// before the flag existed.
		gate, err = newSeatGate(c.seatToken, "t1", c.humans)
		if err != nil {
			return err
		}
		opts.Seat = gate.resolve
	}
	// Task ui11: with -vsbot on and a deck pool to deal, arm the play-vs-bot
	// flow so the landing page can seat a human against a bot on demand. This
	// is additive — the startup tables above are untouched — and needs a gate
	// even when there were no -humans slots, so create one (an empty store)
	// and route the seat-scoped endpoints through it. Off (the default), the
	// server is spectator-only exactly as before, and a request naming a seat
	// is refused 403 (TestNoHumansIsSpectatorOnly).
	if c.vsbot && len(cmdPool)+len(conPool) > 0 {
		if gate == nil {
			gate = &seatGate{tokenToClaim: map[string]httpapi.SeatClaim{}, claimTokens: map[httpapi.SeatClaim]string{}}
		}
		opts.CreateGame = c.createGame(r, gate, cmdPool, conPool, vis)
		opts.Seat = gate.resolve
	}
	// Card art is cached and served from this server's own origin (art.go)
	// rather than sending every viewer's browser to Scryfall directly — see
	// art.go's doc comment. /art/ is routed ahead of httpapi's own mux (which
	// owns "/" for the SPA and every /api/ path), so this is additive: a
	// server with the art directory unavailable would only ever affect these
	// two new patterns, never anything httpapi already serves.
	ac, err := newServeArtCache(c.artCacheDir())
	if err != nil {
		return err
	}
	// The card catalog (the six printed facts, /cards/named) is served from
	// the same art-cache records art.go keeps — one Scryfall named response
	// feeds both the image and the text — and is routed here, ahead of
	// httpapi's mux, for the same reason /art/ is: additive, never shadowing
	// anything httpapi already serves.
	// A player's own bug report (feedback.go), written to its own durable
	// directory rather than the disposable persistence dir — see config's
	// `feedback` field. Routed here, ahead of httpapi's mux, for the same
	// reason /art/ is: it is additive, and a server whose feedback directory
	// is unavailable fails at startup rather than at the first submission.
	topMux := http.NewServeMux()
	topMux.HandleFunc("GET /art/named", ac.named)
	topMux.HandleFunc("GET /art/blob/{key}", ac.blob)
	topMux.HandleFunc("GET /cards/named", ac.text)
	// An empty -feedback disarms the endpoint entirely, the same opt-in shape
	// Options.Seat and CreateGame use. That is what a config built in a test
	// gets (the flag default only applies to the real binary), so a test
	// server never creates a directory it did not ask for -- and a submission
	// to a server with feedback off is a 404 rather than a silent success.
	if c.feedback != "" {
		fb, err := newFeedbackStore(c.feedback, r)
		if err != nil {
			return err
		}
		topMux.HandleFunc("POST /api/feedback", fb.submit)
	}
	topMux.Handle("/", httpapi.NewHandler(r, opts))
	srv := &http.Server{Handler: topMux}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	fmt.Fprintf(os.Stderr, "gorged: %d tables of %d on %s (dir %s)\n", len(r.Tables()), c.seats, ln.Addr(), c.dir)
	if gate != nil {
		for _, s := range c.humans {
			seat := state.PlayerID(s)
			// The path is the LIVE table route, not "/". "/" is the
			// lobby, and clicking a live match there navigates to
			// /t/t1/m/1 -- the finished-match replay route, which paints
			// a frozen snapshot and never advances. A player who joined
			// through the lobby therefore ended up watching history while
			// the real game waited for them. The join URL goes straight
			// to the seat. Humans are on t1 alone (FL-97), so the table
			// is not a guess.
			fmt.Fprintf(os.Stderr, "gorged: table t1 seat %d joins at http://%s/t/t1?seat=%d&token=%s\n",
				s, joinHost(ln.Addr()), s, gate.token("t1", seat))
		}
	}
	// Task fb-20260914T113850Z-682e875e: prewarm the art cache for every
	// distinct card name (plus each commander) across the dealt decks, in a
	// background goroutine (art.go prewarmArt): after a deploy the whole pool
	// would otherwise refill lazily, one paced Scryfall fetch per browser
	// request, and every viewer saw missing art for minutes. serve()'s own ctx
	// cancels the prewarm on shutdown; ensure/ensureText are single-flight and
	// disk-checked, so a prewarm racing a browser's first request for the same
	// name shares one Scryfall round trip and a warm name costs nothing. Off
	// (the zero value) in any config that did not come through serveFlags —
	// whose -prewarm flag defaults on — so a test config built directly never
	// fires an outbound request.
	// The rate limiter's 429 retries are worth one log line each so an
	// operator can SEE the pacing working in the demo log — for the prewarm
	// and for browser-driven fetches alike (both go through this one cache).
	ac.logf = func(f string, a ...any) {
		fmt.Fprintf(os.Stderr, "gorged: "+f+"\n", a...)
	}
	if c.prewarm {
		go prewarmArt(ctx, ac, c.decks, func(f string, a ...any) {
			fmt.Fprintf(os.Stderr, "gorged: "+f+"\n", a...)
		})
	}
	select {
	case err := <-errc:
		r.Close()
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	return r.Close()
}

// applyHumans parses the -humans flag into c.humans and rejects the
// configurations R-E3-1 forbids up front: a malformed list, or humans with
// no table to seat them on. A slot index out of range is deliberately not
// checked again here — TableConfig.validate owns that check, and the t1
// AddTable fails with the same information.
func (c *config) applyHumans() error {
	if strings.TrimSpace(c.humansRaw) == "" {
		c.humans = nil
		return nil
	}
	parts := strings.Split(c.humansRaw, ",")
	humans := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return fmt.Errorf("-humans %q: %q is not a slot index", c.humansRaw, p)
		}
		humans = append(humans, n)
	}
	if c.tables < 1 {
		return fmt.Errorf("-humans %s: needs at least one table (-tables 1..)", c.humansRaw)
	}
	c.humans = humans
	return nil
}

// applyFormats parses the -format flag into c.formats. The list is cycled
// over the tables — a single -format commander makes every table a
// Commander game, and -format commander,constructed runs one of each — and
// a name that is neither constructed nor commander fails here, before any
// table is added (R-E3-1), so a typo'd format can never half-start a
// server of wrong-format tables.
func (c *config) applyFormats() error {
	c.formats = nil
	// The zero value (no -format flag at all — tests, and a config that
	// never sets the field) means constructed, exactly what every table
	// before this flag was; applyHumans follows the same empty-means-
	// default convention.
	if strings.TrimSpace(c.formatsRaw) == "" {
		c.formats = []host.Format{host.FormatConstructed}
		return nil
	}
	for _, p := range strings.Split(c.formatsRaw, ",") {
		f, err := host.ParseFormat(strings.TrimSpace(p))
		if err != nil {
			return err
		}
		c.formats = append(c.formats, f)
	}
	return nil
}

// wantsFormat reports whether any table in the configuration plays format f.
func (c config) wantsFormat(f host.Format) bool {
	for _, g := range c.formats {
		if g == f {
			return true
		}
	}
	return false
}

// splitDecks lists the deck stems in dir (deckFiles) and splits them into
// the commander and constructed pools a table of each format deals, then
// validates every commander deck up front (deck.ValidateCommander, the m35
// CR 903.4/903.5 gate) so a deployed commander table can never half-start
// on an illegal deck — the deck is named in the error. A file that names a
// commander belongs to the commander pool and every other file to the
// constructed pool: the two pools are disjoint and the five Foundations
// decks can never be dealt as 100-card constructed piles. Both pools keep
// deckFiles' sorted order, so seat assignment stays deterministic.
func splitDecks(reg *cards.Registry, dir string) (commander, constructed []string, err error) {
	names, err := deckFiles(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, n := range names {
		f, _, lerr := deck.Load(reg, filepath.Join(dir, n+".json"))
		if lerr != nil {
			return nil, nil, lerr
		}
		if len(f.CommanderNames()) == 0 {
			constructed = append(constructed, n)
			continue
		}
		if verr := f.ValidateCommander(reg); verr != nil {
			return nil, nil, fmt.Errorf("commander deck %q: %w", n, verr)
		}
		commander = append(commander, n)
	}
	return commander, constructed, nil
}

// loadDeckCatalogue reads display metadata for the exact pools splitDecks
// validated. The format exposed to the client is the server's pool format,
// not the deck file's free-form authoring field (constructed files currently
// call that field "custom"). Pool and catalogue therefore cannot disagree.
func loadDeckCatalogue(dir string, commander, constructed []string) ([]httpapi.DeckInfo, error) {
	formats := make(map[string]host.Format, len(commander)+len(constructed))
	for _, id := range commander {
		formats[id] = host.FormatCommander
	}
	for _, id := range constructed {
		formats[id] = host.FormatConstructed
	}
	ids, err := deckFiles(dir)
	if err != nil {
		return nil, err
	}
	out := make([]httpapi.DeckInfo, 0, len(ids))
	for _, id := range ids {
		format, ok := formats[id]
		if !ok {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, id+".json"))
		if err != nil {
			return nil, err
		}
		f, err := deck.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(dir, id+".json"), err)
		}
		name := f.Name
		if name == "" {
			name = id
		}
		// The wire's DeckInfo.Commander stays a single string: a partner pair
		// is joined with " & " so the existing field needs no schema change
		// and the web client renders it as-is.
		out = append(out, httpapi.DeckInfo{ID: id, Name: name, Format: format.String(), Archetype: f.Archetype, Commander: strings.Join(f.CommanderNames(), " & ")})
	}
	return out, nil
}

// tableConfigs builds one TableConfig per table, in ID order, dealing each
// table the deck pool of its format — commander tables the commander pool,
// constructed tables the constructed pool — so one server runs both formats
// side by side and a commander deck is never dealt as a 100-card
// constructed pile. A table the -format list does not reach (fewer entries
// than tables) is constructed, the zero value. Human slots apply to table
// t1 alone; claims bind to their table, so a token for t1 cannot act on
// another table. R-E3-2: a human-seated table is single-shot by definition,
// and the -perpetual flag
// defaults to true, so a naive copy of the bot config would make AddTable
// reject it (perpetual+humans); Perpetual is forced false for t1,
// regardless of the flag, and the bot tables keep the flag. AddTable still
// validates the result (slot range, duplicates, commander decks), so serve
// fails before listening on a bad -humans list or a commander table dealt
// a deck with no commander.
func (c config) tableConfigs(cmdPool, conPool []string, vis view.Visibility) []host.TableConfig {
	cfgs := make([]host.TableConfig, 0, c.tables)
	for i := 1; i <= c.tables; i++ {
		format := host.FormatConstructed
		if len(c.formats) > 0 {
			format = c.formats[(i-1)%len(c.formats)]
		}
		pool := conPool
		if format == host.FormatCommander {
			pool = cmdPool
		}
		cfg := host.TableConfig{ID: host.TableID(fmt.Sprintf("t%d", i)), Name: fmt.Sprintf("Table %d", i), Seats: c.seats,
			Decks: pool, Seed: c.seed + uint64(i-1), Pace: c.pace, Spectator: vis, Perpetual: c.perpetual,
			Mulligans: c.mulligans, Format: format, BotAutoPayMana: c.botAutoPayMana, AutoMana: c.autoMana}
		if i == 1 && len(c.humans) > 0 {
			cfg.Humans = c.humans
			cfg.Perpetual = false
		}
		cfgs = append(cfgs, cfg)
	}
	return cfgs
}

// hostOptions builds the Registry options for a config, threading the FL-40
// token corpus (reg.Tokens) into host.Options.Tokens so every live match and
// its replay mints the same tokens. Split from serve so TestHostThreads
// CorpusTokens can assert the thread works without driving the whole server.
func (g config) hostOptions(reg *cards.Registry, load func(string) (host.Deck, error)) host.Options {
	// Sleep is left nil: host installs its default interruptible sleep, the
	// package's only sanctioned clock read, so a long pace or cooldown still
	// yields to Close.
	//
	// MaxDecisionsPerTurn arms the host's per-turn progress guard (Task HW1)
	// for every served table at the recommended value: a bot policy stall
	// that re-offers one no-op action forever — the 36-of-200 frozen
	// four-seat Commander games measured on main — previously left the table
	// TableLive with no match ever following, because ThinkTimeout only
	// covers a parked human seat. The value cannot fire on a legitimate
	// turn (measured legit ceiling 244 decisions; the stall ran ~10000 per
	// turn; 25000 is 100x the first and swept by the second within three
	// stall-turns); the count-based design keeps a match's outcome a pure
	// function of its seed on every machine and every replay, exactly like
	// the engine and the bots themselves.
	return host.Options{Dir: g.dir, LoadDeck: load, Tokens: reg.Tokens, NameUniverse: reg.Cards, Sync: true, Cooldown: g.cooldown,
		MaxDecisionsPerTurn: host.DefaultMaxDecisionsPerTurn, DefaultBotAutoPayMana: g.botAutoPayMana,
		MaxOnDemandTables: g.maxOnDemandTables}
}

// artCacheDir resolves where the card-art cache lives: -art-dir when set,
// else the historical <dir>/art. The empty default is pinned by a test so
// every existing deployment and test keeps its exact location; only a caller
// that explicitly asks for durability (deploy-demo.sh) moves the cache out
// from under the wiped persistence dir.
func (c config) artCacheDir() string {
	if c.artDir != "" {
		return c.artDir
	}
	return filepath.Join(c.dir, "art")
}

// deckCardNames collects the distinct printed card names every deck file in
// dir references, plus each commander, sorted — the set of names a browser
// viewing a dealt table can ask the art cache for. Prewarm walks these, not
// the whole corpus: the dealt decks are a bounded, sane warm set (measured
// 436 distinct names across this repo's 19 deck files), while the corpus is
// ~33k cards. Names are parsed from the deck files only (deck.Parse — no
// registry lookup), so this reads nothing but JSON.
func deckCardNames(dir string) ([]string, error) {
	stems, err := deckFiles(dir)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var names []string
	add := func(n string) {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		names = append(names, n)
	}
	for _, stem := range stems {
		raw, err := os.ReadFile(filepath.Join(dir, stem+".json"))
		if err != nil {
			return nil, err
		}
		f, err := deck.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(dir, stem+".json"), err)
		}
		for _, n := range f.CommanderNames() {
			add(n)
		}
		for _, e := range f.Cards {
			add(e.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// deckFiles lists the deck names (file stems) in dir, sorted, so seat
// assignment is the same on every machine.
func deckFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			names = append(names, deck.Stem(e.Name()))
		}
	}
	sort.Strings(names)
	return names, nil
}

// deckLoader resolves a name to dir/<name>.json once and caches it: the
// host asks for the same decks every match. The seat is named after the
// file stem (PL-14), not the deck file's own name field; the File is read
// anyway because a deck that names a commander carries its command-zone
// index on the host Deck (deck.File.CommanderIndex — the same resolution
// rules' own commander games use, and the pool split validated up front),
// so a commander table's seats get their command zone and a constructed
// table's seats get none.
func deckLoader(reg *cards.Registry, dir string) func(string) (host.Deck, error) {
	var mu sync.Mutex
	cache := map[string]host.Deck{}
	return func(name string) (host.Deck, error) {
		mu.Lock()
		defer mu.Unlock()
		if d, ok := cache[name]; ok {
			return d, nil
		}
		f, cs, err := deck.Load(reg, filepath.Join(dir, name+".json"))
		if err != nil {
			return host.Deck{}, err
		}
		sb, err := f.ResolveSideboard(reg)
		if err != nil {
			return host.Deck{}, err
		}
		d := host.Deck{Name: name, Cards: cs, Sideboard: sb}
		if names := f.CommanderNames(); len(names) > 0 {
			d.Commanders = f.CommanderIndices()
		}
		cache[name] = d
		return d, nil
	}
}
