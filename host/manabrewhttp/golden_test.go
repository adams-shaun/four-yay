//go:build manabrew

package manabrewhttp

// TestGoldenTranscript is MB-11's end-to-end golden (scoping spec §8 item 6):
// a fixed-seed 2-seat game driven entirely through the real HTTP transport
// (httptest, no port binding), with a first-legal mock client answering
// every prompt. The complete sequence of engine->client messages seat 0
// observes is compared byte for byte with testdata/golden/2seat.ndjson.
//
// The spec's §8 item 6 names internal/manabrew/testdata/golden/*.ndjson as
// the location; this ticket's own Files line -- and the fact that the
// fixture is produced by this package's httptest transport, not by
// internal/manabrew in isolation -- puts it at
// host/manabrewhttp/testdata/golden/ instead. Following the ticket over the
// spec citation here, per this ticket's brief's own instruction to do so.
//
// # Why the capture uses GET .../state, not the SSE stream
//
// The obvious design reads seat 0's raw SSE bytes. It was tried first and
// found genuinely non-deterministic, for a reason that is a property of the
// transport's own documented design, not a bug this ticket introduces:
// stream.go's own comment says bursts are coalesced by draining the
// session's wake-up channel before each re-read, so how many separate
// `state` frames a run of several engine-internal steps (untap, an
// upkeep with no trigger, draw, ...) produces on the wire depends on
// whether the stream's own goroutine gets scheduled between two of those
// steps' wake-ups or after both -- real wall-clock goroutine-scheduling
// timing, not anything about the game itself, which replays byte-identical
// at the EVENT level every time (confirmed independently:
// TestRepoDeckGamesReplayExactly and TestHeads pin exactly this). Two
// otherwise-identical runs of this fixture were observed to differ by
// several extra/missing bare `state` frames at exactly the steps where no
// decision was needed, which is the coalescing boundary moving, not a
// real divergence.
//
// GET .../state (state.go) shares the exact same tr.State/tr.Prompt code
// that the stream's refresh() closure calls, but as an ordinary
// request/response with no broadcast-coalescing step in between, so it is
// fully deterministic. This golden therefore polls it once per decision
// point (plus once at the very start and once at gameOver), decodes the
// JSON array it returns, and appends each element as its own NDJSON line
// -- the same one-message-per-line shape the SSE stream would have used,
// just sourced from the deterministic endpoint. SSE's own framing and
// wake-up contract are covered elsewhere (TestStreamSendsStateThenPrompt,
// TestReconnectIdempotent, TestNoForeignHandEverVisible in
// manabrewhttp_test.go); this ticket's addition is the golden byte-pin,
// which needs a deterministic source to mean anything.
//
// # What the fixture exercises
//
// Only decision kinds already translated are exercised: the fixture
// deliberately never casts a spell or plays a land, so no
// KTarget/KModes/KReplacement/KCommanderZone/KStartingPlayer decision is
// ever posed, and no creature ever exists, so the engine resolves
// KAttackers/KBlockers with the empty declaration and asks nothing at all
// (rules/combat.go's "no legal option list" no-decision path). It does
// reach one KChoose shape (the cleanup hand-size discard, once a
// never-discarding hand grows past the max). KTriggerOrder and KArrange are
// never exercised, since nothing in this fixture ever puts more than one
// trigger on the stack at once or asks for a reorder/scry/etc. ask. A
// follow-up ticket that wants those covered should extend
// firstLegalAnswer, which fails loudly (not silently) on any prompt shape
// it does not recognise -- that failure names exactly where to add the new
// case before regenerating the fixture.
//
// The transcript also never contains a "gameOver" prompt:
// internal/manabrew/dispatch.go has no case that ever builds
// mb.GameOverInput -- GameOver is not a decision.Kind the engine asks
// about, it is a state.View field the final `state` message carries
// (view.Over / GameViewDto.GameOver). This test detects termination by that
// field instead of by a prompt the current translator can never emit.
// Whether the spec intends the engine to also synthesize a terminal
// gameOver AgentPrompt with no decision.Kind behind it is an open question
// this ticket surfaces but does not resolve: MB-11 wires the transport and
// the fixture, not new decision kinds or dispatch.go cases (see this
// ticket's final report).

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/manabrew"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
)

// MBX-2's golden delta, up front: the transcript gains exactly ONE line, the
// terminal gameOver prompt at the very end (see the report and commit
// message). The file doc above still says the transcript "never contains a
// gameOver prompt" -- that was true when MB-11 landed; MBX-2 closes exactly
// that open question, and the last line of the committed fixture is now the
// prompt the spec's Appendix A "end of match" row promises. The synthetic
// ack-only prompts (reveal/dice) are wired through the same poll path but
// mint nothing in this fixture: it never casts a spell, never attacks, so no
// public-reveal or dice Note event ever exists for Synthetic to read.

// goldenPath is gzipped (5.3 MB uncompressed; ~46 KB gzipped) so the fixture
// is committable. The test always compares decompressed bytes; regenerating
// writes gzip with a fixed header (no ModTime, no Name) via gzipDeterministic
// so the compressed bytes themselves are reproducible too, not just their
// decompression.
const goldenPath = "testdata/golden/2seat.ndjson.gz"

// gzipDeterministic gzip-compresses data with every header field that would
// otherwise vary (ModTime, Name, Comment, OS) held at its zero/default
// value, so two regenerations of byte-identical content produce
// byte-identical .gz files -- gzip.Writer defaults OS but stamps ModTime
// from time.Now() unless the caller overrides gzip.Header first.
func gzipDeterministic(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	w.Header.ModTime = time.Time{}
	w.Header.Name = ""
	w.Header.Comment = ""
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pollSeat0Raw is GET .../state for seat 0, decoded only far enough to split
// the returned JSON array into its individual messages -- each element's
// bytes are kept exactly as the server wrote them, so the golden pins the
// real wire content, not a re-encoding of it.
func pollSeat0Raw(t *testing.T, ts *testServer) []json.RawMessage {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.srv.URL+"/t1/matches/1/state", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer s0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET .../state: status %d, body %s", resp.StatusCode, body)
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(body, &raws); err != nil {
		t.Fatalf("decoding state poll body: %v", err)
	}
	return raws
}

// gameOverIn reports whether any of msgs is a StateUpdate with GameOver set.
func gameOverIn(t *testing.T, msgs []json.RawMessage) bool {
	t.Helper()
	for _, raw := range msgs {
		var m mb.EngineMessage
		if _, err := mb.Decode(raw, &m); err != nil {
			t.Fatalf("decoding polled message: %v", err)
		}
		if su, ok := m.Value.(mb.StateUpdate); ok && su.GameView.GameOver {
			return true
		}
	}
	return false
}

// firstLegalAnswer builds the minimal always-legal response to pm: pass
// priority, decline every attack/block, and discard the first Min offered
// cards for a cleanup hand-size ask. Priority's pass option is always
// offered (§6.3's mapping keeps it out of the AvailableAction list, but the
// decision itself always carries one), CR 508.1a/509.1c make the empty
// attacker/blocker declaration legal unconditionally, and a discard-to-
// hand-size choice carries no ordering rule over which cards go.
func firstLegalAnswer(t *testing.T, pm mb.PromptMessage) (string, mb.PromptOutputValue) {
	t.Helper()
	switch in := pm.Input.Value.(type) {
	case mb.ChooseActionInput:
		return "chooseAction", mb.PassOutput{}
	case mb.ChooseAttackersInput:
		return "chooseAttackers", mb.DeclareAttackersDecision{}
	case mb.ChooseBlockersInput:
		return "chooseBlockers", mb.DeclareBlockersDecision{}
	case mb.ChooseCardsInput:
		n := in.Min
		if n > len(in.Cards) {
			n = len(in.Cards)
		}
		ids := make([]string, 0, n)
		for i := 0; i < n; i++ {
			ids = append(ids, in.Cards[i].ID)
		}
		return "chooseCards", mb.ChooseCardsDecision{ChosenCardIDs: ids}
	default:
		t.Fatalf("golden driver has no first-legal answer for prompt type %q; "+
			"extend firstLegalAnswer once the fixture is changed to exercise it", pm.Input.Value.PromptType())
		return "", nil
	}
}

// waitForFreshPending retries (host.Registry.Pending is a plain read, so
// this converges as soon as the match's own goroutine has actually applied
// the last submitted intent -- host/humanseat.go's submitAdmitted only hands
// an intent to that goroutine over a channel, so it does not clear a seat's
// answered slot synchronously) until some seat in order has an open decision
// whose Seq is not the one this driver already answered, or the deadline
// passes. Skipping an unchanged Seq is what makes this safe to call
// immediately after send() returns: without it, the first poll after
// answering a decision can still observe that same (not yet superseded)
// decision and try to answer it a second time, which the second time around
// is a genuine stalePrompt (this was hit and fixed while writing this
// fixture).
func waitForFreshPending(ts *testServer, seats []state.PlayerID, lastSeq map[state.PlayerID]uint64, deadline time.Time) (state.PlayerID, *decision.Decision, bool) {
	for {
		for _, s := range seats {
			d, err := ts.reg.Pending("t1", 1, s)
			if err != nil {
				continue
			}
			if ls, ok := lastSeq[s]; ok && ls == d.Seq {
				continue
			}
			return s, d, true
		}
		if time.Now().After(deadline) {
			return 0, nil, false
		}
		time.Sleep(time.Millisecond)
	}
}

// runGoldenGame drives the fixed-seed 2-seat game to gameOver and returns
// the NDJSON lines seat 0 would have observed, one per engine->client
// message, sourced deterministically (see the file doc comment).
func runGoldenGame(t *testing.T) [][]byte {
	t.Helper()
	ts := newTestServer(t, []int{0, 1}, 0, 0) // both seats human, no mulligans
	tr := manabrew.New("t1", 1, nil)
	seats := []state.PlayerID{0, 1}

	var transcript [][]byte
	lastSeq := map[state.PlayerID]uint64{}
	overallDeadline := time.Now().Add(30 * time.Second)
	for {
		var seat state.PlayerID
		var d *decision.Decision
		var ok bool
		for {
			seat, d, ok = waitForFreshPending(ts, seats, lastSeq, time.Now().Add(200*time.Millisecond))
			if !ok {
				// No seat has a fresh ask: the game may have ended with nobody
				// left to ask anything (decking out is a state-based action,
				// not a decision), so a blind wait for "some seat is pending"
				// would otherwise burn the whole overall deadline on every
				// ordinary game-over. Poll ONCE, and if it is over record THAT
				// poll: the gameOver prompt is delivered exactly once per seat
				// (MBX-2's seatConn latch), so the former second poll would
				// have re-captured the state but not the terminal prompt.
				msgs := pollSeat0Raw(t, ts)
				if gameOverIn(t, msgs) {
					for _, m := range msgs {
						transcript = append(transcript, []byte(m))
					}
					return transcript
				}
				if time.Now().After(overallDeadline) {
					t.Fatal("golden game drive timed out: neither seat has a pending decision, and the match has not ended")
				}
				continue
			}
			break
		}

		// The decision is settled (Pending just returned it): capture the
		// deterministic snapshot at exactly this point before acting on it.
		msgs := pollSeat0Raw(t, ts)
		for _, m := range msgs {
			transcript = append(transcript, []byte(m))
		}
		if gameOverIn(t, msgs) {
			return transcript
		}

		seq, err := headSeq(ts.reg, "t1", 1)
		if err != nil {
			t.Fatalf("headSeq: %v", err)
		}
		v, err := ts.reg.ViewAtSeat("t1", 1, seq, seat)
		if err != nil {
			t.Fatalf("ViewAtSeat(seat %d): %v", seat, err)
		}
		pm, perr := tr.Prompt(d, &v)
		if perr != nil {
			t.Fatalf("decision kind %s (seat %d) has no ManaBrew translation yet: %v", d.Kind, seat, perr)
		}
		typ, out := firstLegalAnswer(t, pm)
		resp := mb.ClientMessage{Value: mb.ClientResponse{
			PromptID: pm.PromptID,
			Action:   mb.PromptOutput{Type: typ, Output: mb.PromptOutputData{Value: out}},
		}}
		token := "s" + strconv.Itoa(int(seat))
		if code, pe := send(t, ts, token, resp); code != http.StatusNoContent {
			t.Fatalf("seat %d answer to %s rejected: status %d, %+v", seat, d.Kind, code, pe)
		}
		lastSeq[seat] = d.Seq
	}
}

// TestGoldenTranscript is this ticket's Done-means gate.
func TestGoldenTranscript(t *testing.T) {
	transcript := runGoldenGame(t)
	got := bytes.Join(transcript, []byte("\n"))
	got = append(got, '\n')

	if os.Getenv("MANABREW_REGEN_GOLDEN") != "" {
		gz, err := gzipDeterministic(got)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, gz, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("regenerated %s (%d frames, %d bytes gzipped)", goldenPath, len(transcript), len(gz))
		return
	}

	gz, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading %s: %v (run with MANABREW_REGEN_GOLDEN=1 to create it)", goldenPath, err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatalf("opening gzip fixture %s: %v", goldenPath, err)
	}
	want, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("decompressing gzip fixture %s: %v", goldenPath, err)
	}
	if !bytes.Equal(got, want) {
		gotLines := bytes.Split(bytes.TrimRight(got, "\n"), []byte("\n"))
		wantLines := bytes.Split(bytes.TrimRight(want, "\n"), []byte("\n"))
		if len(gotLines) != len(wantLines) {
			t.Fatalf("golden transcript has %d lines, committed fixture has %d; "+
				"rerun with MANABREW_REGEN_GOLDEN=1 if this is an intended change",
				len(gotLines), len(wantLines))
		}
		for i := range gotLines {
			if !bytes.Equal(gotLines[i], wantLines[i]) {
				t.Fatalf("golden transcript diverges at line %d:\n got:  %s\nwant: %s",
					i, gotLines[i], wantLines[i])
			}
		}
	}
}

// TestGoldenTranscriptDeterministic pins that two independent runs of the
// same fixed-seed fixture produce byte-identical transcripts -- the
// no-nondeterminism invariant (AGENTS.md), applied to this transport's
// deterministic (poll-based) capture path.
func TestGoldenTranscriptDeterministic(t *testing.T) {
	a := bytes.Join(runGoldenGame(t), []byte("\n"))
	b := bytes.Join(runGoldenGame(t), []byte("\n"))
	if !bytes.Equal(a, b) {
		t.Fatal("two runs of the same fixed-seed fixture produced different transcripts")
	}
}
