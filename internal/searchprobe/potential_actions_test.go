package searchprobe

import (
	"bytes"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestStripPotentialActionsMatchesASkippedCapture is the equality the sampler's
// replay rests on: the observed board with its potential_actions member
// textually removed is byte for byte the board the same capture produces when
// the projection is skipped outright. It is checked on every frame of a real
// game, and the run is required to contain frames that actually carry the
// field, so the equality is not vacuous.
func TestStripPotentialActionsMatchesASkippedCapture(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	names := []string{"mono-red-prowess", "mono-blue-tempo"}
	decks := make([][]*cards.Card, len(names))
	for i, n := range names {
		var err error
		if decks[i], err = testutil.LoadRepoDeck(reg, n); err != nil {
			t.Fatal(err)
		}
	}
	e := rules.New(rules.Config{Seed: 30_000_000, Names: names, Decks: decks, Tokens: reg.Tokens})
	e.Advance()
	// Two collectors in lockstep on the one engine: identical observation
	// refs, so the only difference their boards may carry is the field.
	full, skipped := NewCollector(0), NewCollector(0)
	full.retainJSON, skipped.retainJSON = true, true
	rngs := BotRandoms(7, len(names))
	board := botpolicy.NewBoard(len(names))
	pos, carried, frames := 0, 0, 0
	for i := 0; i < 400; i++ {
		burst := e.L.Events[pos:]
		want, err := full.Capture(e, burst)
		if err != nil {
			t.Fatal(err)
		}
		got, err := skipped.captureScratch(e, burst, false)
		if err != nil {
			t.Fatal(err)
		}
		frames++
		if bytes.Contains(want.Board.raw, []byte(`"potential_actions":`)) {
			carried++
		}
		if stripped := stripPotentialActions(want.Board.raw); !bytes.Equal(stripped, got.Board.raw) {
			t.Fatalf("frame %d: stripped observed board differs from the skipped capture\n stripped: %s\n  skipped: %s", i, stripped, got.Board.raw)
		}
		// The digests the replay compares say the same thing as the bytes.
		if want.Board.Stripped != got.Board.Sum || got.Board.Stripped != got.Board.Sum {
			t.Fatalf("frame %d: board digests disagree with the stripped bytes", i)
		}
		d := e.Pending()
		if d == nil {
			break
		}
		pos = len(e.L.Events)
		if err := e.Submit(botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])); err != nil {
			t.Fatal(err)
		}
	}
	if frames < 50 {
		t.Fatalf("fixture too short to be evidence: %d frames", frames)
	}
	if carried == 0 {
		t.Fatalf("no frame of %d carried potential_actions: the equality is vacuous", frames)
	}
	t.Logf("%d frames, %d carrying potential_actions", frames, carried)
}

// TestSkippingPotentialActionsAcceptsTheSameWorlds is the soundness
// measurement for the skip, in the shape the land exclusion's carries: sample
// the real-deck root both ways and require the accepted worlds to be
// identical. The measured run also reports how many rejections the
// potential_actions projection decided on its own -- the only way the skip
// could move a world -- and that count must be zero.
func TestSkippingPotentialActionsAcceptsTheSameWorlds(t *testing.T) {
	f := benchRoot(t)
	opts := benchSampleOptions()
	opts.ComparePotentialActions = true
	measured, err := Sample(f.setup, f.h, opts)
	if err != nil {
		t.Fatal(err)
	}
	if measured.BoardPotentialActionsOnly != 0 {
		t.Fatalf("%d rejections were decided by potential_actions alone: the skip is not sound here", measured.BoardPotentialActionsOnly)
	}
	skipped, err := Sample(f.setup, f.h, benchSampleOptions())
	if err != nil {
		t.Fatal(err)
	}
	if measured.Accepted != skipped.Accepted || measured.Attempts != skipped.Attempts {
		t.Fatalf("acceptance moved: measured %d/%d, skipped %d/%d", measured.Accepted, measured.Attempts, skipped.Accepted, skipped.Attempts)
	}
	if a, b := worldsDigest(t, measured), worldsDigest(t, skipped); a != b {
		t.Fatalf("accepted worlds differ:\n measured %s\n  skipped %s", a, b)
	}
	if measured.PrefixRejected != skipped.PrefixRejected {
		t.Fatalf("rejection count moved: %d vs %d", measured.PrefixRejected, skipped.PrefixRejected)
	}
}

// TestStripPotentialActionsIgnoresTheKeyInsideAString pins the scan's string
// awareness: a card name (or any other string value) spelling the member is
// data, not a member, and must survive.
func TestStripPotentialActionsIgnoresTheKeyInsideAString(t *testing.T) {
	const board = `{"a":1,"name":",\"potential_actions\":[1]","potential_actions":[{"kind":"cast"}],"b":[2,3]}`
	const want = `{"a":1,"name":",\"potential_actions\":[1]","b":[2,3]}`
	if got := string(stripPotentialActions([]byte(board))); got != want {
		t.Fatalf("strip:\n got %s\nwant %s", got, want)
	}
}

// TestStripPotentialActionsLeavesABoardWithoutTheMemberAlone keeps the fast
// path honest: nothing to remove means the input is returned untouched.
func TestStripPotentialActionsLeavesABoardWithoutTheMemberAlone(t *testing.T) {
	const board = `{"a":1,"players":[{"seat":0,"pool":{}}]}`
	got := stripPotentialActions([]byte(board))
	if string(got) != board {
		t.Fatalf("strip changed a board carrying no member: %s", got)
	}
	if strings.Contains(board, "potential_actions") {
		t.Fatal("fixture should not carry the member")
	}
}
