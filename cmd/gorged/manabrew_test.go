package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestManaBrewAddrRefusesDemoPorts pins the -manabrew-addr validation
// (scoping spec §5.4): 8080 and 8081 are the shared demo's ports
// (AGENTS.md's port table) and must never be handed to a second listener,
// while an ordinary address is accepted and implies -manabrew. This needs
// no corpus: applyManaBrew is pure config validation.
func TestManaBrewAddrRefusesDemoPorts(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{":8080", ":8081", "127.0.0.1:8080", "0.0.0.0:8081"} {
		c := &config{manabrewAddr: addr}
		if err := c.applyManaBrew(); err == nil {
			t.Fatalf("-manabrew-addr %q: expected refusal, got none", addr)
		}
	}
	c := &config{manabrewAddr: ":8091"}
	if err := c.applyManaBrew(); err != nil {
		t.Fatalf("-manabrew-addr :8091: unexpected error: %v", err)
	}
	if !c.manabrew {
		t.Fatal("a non-empty -manabrew-addr should imply -manabrew")
	}
	// A malformed address (no port at all) is also refused, distinctly from
	// the demo-port case.
	bad := &config{manabrewAddr: "not-an-address"}
	if err := bad.applyManaBrew(); err == nil {
		t.Fatal("a malformed -manabrew-addr should fail validation")
	}
	// Off by default: an empty -manabrew-addr never touches c.manabrew.
	empty := &config{}
	if err := empty.applyManaBrew(); err != nil {
		t.Fatalf("empty -manabrew-addr: unexpected error: %v", err)
	}
	if empty.manabrew {
		t.Fatal("an empty -manabrew-addr must not imply -manabrew")
	}
}

// TestManaBrewOnRequiresSeatGate pins the §5.4 startup validation: -manabrew
// with neither -humans nor -vsbot must fail before any table is added,
// rather than mounting routes that could only ever answer 403. The check
// runs on the flags themselves (before splitDecks/host.New), so this
// returns fast with no bot table ever started.
func TestManaBrewOnRequiresSeatGate(t *testing.T) {
	t.Parallel()
	testutil.CorpusRegistry(t) // skips when .cards/ is absent
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	cfg := config{cards: "../../.cards", decks: "../../internal/testutil/decks", tables: 1, seats: 2,
		dir: t.TempDir(), spectator: "omniscient", seed: 1, perpetual: false, manabrew: true}
	err = serve(context.Background(), cfg, ln)
	if err == nil {
		t.Fatal("expected -manabrew without -humans/-vsbot to fail startup")
	}
	if !strings.Contains(err.Error(), "manabrew") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestManaBrewOffMountsNothing pins §5.4's "off is byte-identical" claim
// (MB-9 item 1): with -manabrew off, a request under the ManaBrew route
// prefix gets exactly the same 404 JSON httpapi's own catch-all already
// serves for any unknown /api/ route -- because nothing registers a more
// specific pattern to intercept it.
func TestManaBrewOffMountsNothing(t *testing.T) {
	t.Parallel()
	testutil.CorpusRegistry(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{cards: "../../.cards", decks: "../../internal/testutil/decks", tables: 1, seats: 2, pace: 0,
		cooldown: 0, dir: t.TempDir(), spectator: "omniscient", seed: 1, perpetual: false}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, ln) }()
	url := "http://" + ln.Addr().String()
	waitForServer(t, url)

	want := mustGet(t, url+"/api/definitely-not-a-route")
	got := mustGet(t, url+"/api/manabrew/v0/tables/t1/matches/1/stream")

	if got.status != want.status {
		t.Fatalf("status: got %d, want %d", got.status, want.status)
	}
	if got.contentType != want.contentType {
		t.Fatalf("Content-Type: got %q, want %q", got.contentType, want.contentType)
	}
	if !bytes.Equal(got.body, want.body) {
		t.Fatalf("body: got %q, want %q", got.body, want.body)
	}
}

type getResult struct {
	status      int
	contentType string
	body        []byte
}

func mustGet(t *testing.T, url string) getResult {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: reading body: %v", url, err)
	}
	return getResult{status: resp.StatusCode, contentType: resp.Header.Get("Content-Type"), body: body}
}

// waitForServer polls /api/tables until the server started by serve() in a
// background goroutine is actually listening, the same readiness pattern
// TestServesTablesOverHTTP uses.
func waitForServer(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url + "/api/tables")
		if err == nil {
			resp.Body.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never came up: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
