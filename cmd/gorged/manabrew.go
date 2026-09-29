package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/host/manabrewhttp"
	"github.com/adams-shaun/gorge/internal/manabrew"
)

// MB-9 of the ManaBrew protocol scoping spec (§5.4, §9): the gorged-side
// toggle for host/manabrewhttp (MB-10). Everything here is additive and
// opt-in -- with -manabrew off (the default), applyManaBrew and
// mountManaBrew are no-ops and serve()'s behaviour is byte-identical to a
// binary built before this file existed.

// manabrewRoutePrefix is where the ManaBrew routes are mounted on the main
// listener's topMux when no separate -manabrew-addr is given. It sits ahead
// of httpapi.NewHandler in the pattern space the same way /art/ and
// /api/feedback do (main.go), but net/http's ServeMux picks the most
// specific registered pattern regardless of registration order anyway: with
// -manabrew off, nothing is registered under this prefix at all, so a
// request here falls through to httpapi's own "/api/" catch-all 404
// (handler.go) exactly as it does today -- "manabrew" never collides with
// the literal "tables" component httpapi's own routes require at the same
// path position. TestManaBrewOffMountsNothing pins this.
const manabrewRoutePrefix = "/api/manabrew/v0/tables"

// demoPorts are never accepted for -manabrew-addr: 8080/8081 are reserved
// for the shared demo server (AGENTS.md's port allocation table), and a
// second listener bound to one of them would either collide with, or
// silently steal traffic from, whichever process is meant to hold it.
var demoPorts = map[string]bool{"8080": true, "8081": true}

// applyManaBrew validates -manabrew-addr and lets a non-empty value imply
// -manabrew, so "-manabrew-addr :8091" alone is enough to turn the adapter
// on, the same convention -humans uses to imply a seat gate without a
// separate -humans-enabled flag. It runs before any table is added or any
// listener opens (R-E3-1 style), so a bad address or a demo port fails
// startup instead of mounting routes that would answer with a bind error.
func (c *config) applyManaBrew() error {
	if c.manabrewAddr == "" {
		return nil
	}
	_, port, err := net.SplitHostPort(c.manabrewAddr)
	if err != nil {
		return fmt.Errorf("-manabrew-addr %q: %w", c.manabrewAddr, err)
	}
	if demoPorts[port] {
		return fmt.Errorf("-manabrew-addr %q: ports 8080 and 8081 are reserved for the shared demo server", c.manabrewAddr)
	}
	c.manabrew = true
	return nil
}

// manabrewThinkTimeout is the host.Options.ThinkTimeout override
// -manabrew-think requests (scoping spec §5.4, Q5), scoped to when the
// adapter is actually on. With -manabrew off this returns 0 -- the zero
// value host.Options.ThinkTimeout already carries today, since gorged sets
// no other think timeout -- so an operator who sets -manabrew-think without
// -manabrew changes nothing, and "off" stays byte-identical.
func (c config) manabrewThinkTimeout() time.Duration {
	if !c.manabrew {
		return 0
	}
	return c.manabrewThink
}

// mountManaBrew wires host/manabrewhttp onto either topMux (the default:
// sharing the main listener, ahead of httpapi) or, when -manabrew-addr
// names one, a second listener of its own. It is a no-op when -manabrew is
// off. gate must be non-nil whenever c.manabrew is true; serve's own
// startup check (main.go, right after applyManaBrew) already refuses to
// reach here otherwise, so a nil Options.Seat -- which would 403 every
// request -- never happens by construction. It returns the second server
// and its Serve error channel only when a separate address was requested;
// both are nil when the routes share topMux or the adapter is off.
func (c config) mountManaBrew(topMux *http.ServeMux, r *host.Registry, gate *seatGate, text manabrew.CardText) (mbSrv *http.Server, mbErrc chan error, err error) {
	if !c.manabrew {
		return nil, nil, nil
	}
	handler := manabrewhttp.NewHandler(r, manabrewhttp.Options{Seat: gate.resolve, Text: text})
	if c.manabrewAddr == "" {
		topMux.Handle(manabrewRoutePrefix+"/", http.StripPrefix(manabrewRoutePrefix, handler))
		fmt.Fprintf(os.Stderr, "gorged: manabrew routes mounted at %s/\n", manabrewRoutePrefix)
		return nil, nil, nil
	}
	ln, err := net.Listen("tcp", c.manabrewAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("manabrew: listening on %s: %w", c.manabrewAddr, err)
	}
	srv := &http.Server{Handler: handler}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	fmt.Fprintf(os.Stderr, "gorged: manabrew routes served on %s\n", ln.Addr())
	return srv, errc, nil
}
