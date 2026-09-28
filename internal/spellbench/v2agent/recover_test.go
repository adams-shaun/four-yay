package v2agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// faulty is a policy that fails the way a real one can.
type faulty struct {
	mode  string
	calls int
}

func (f *faulty) GameStart(*GameStart) {}
func (f *faulty) GameOver(*GameOver)   {}
func (f *faulty) Choose(d *Decision) (int, error) {
	f.calls++
	switch f.mode {
	case "error":
		return 0, errors.New("search blew up")
	case "panic":
		panic("nil map")
	case "range":
		return len(d.Candidates) + 3, nil
	}
	return 0, nil
}

const (
	rcStart  = `{"request_type":"game_start","protocol":"spellbench/v2","request_id":"r-1","game_id":"g-1","seat":"p0","agent_seed":3}`
	rcChoose = `{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-2","game_id":"g-1","decision":{"acting_seat":"p0","seat_step":4,"candidates":[{"candidate_id":0,"semantic":{"kind":"pass"}},{"candidate_id":1,"semantic":{"kind":"play_land","face":0}}]}}`
)

func selection(t *testing.T, out []byte) (id int64, errCode string) {
	t.Helper()
	var r struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
		Selection *struct {
			CandidateID int64 `json:"candidate_id"`
		} `json:"selection"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		return -1, r.Error.Code
	}
	if r.Selection == nil {
		t.Fatalf("no selection: %s", out)
	}
	return r.Selection.CandidateID, ""
}

// TestPolicyFailuresNeverForfeit: a policy error, panic or out-of-range
// index is answered with the fallback policy's legal choice (the heuristic
// plays the land), counted, never an internal_error the host forfeits.
func TestPolicyFailuresNeverForfeit(t *testing.T) {
	for _, mode := range []string{"error", "panic", "range"} {
		f := &faulty{mode: mode}
		a, err := New(f, Options{Name: "t", Version: "1"})
		if err != nil {
			t.Fatal(err)
		}
		a.HandleLine([]byte(rcStart))
		id, code := selection(t, a.HandleLine([]byte(rcChoose)))
		if code != "" || id != 1 {
			t.Fatalf("%s: answered %d %q, want the fallback's play_land (1)", mode, id, code)
		}
		if a.Stats.PolicyFallbacks != 1 || a.Stats.Choices != 1 || len(a.Stats.Errors) != 0 {
			t.Fatalf("%s: stats %+v", mode, a.Stats)
		}
	}
}

// TestResyncRecoveries: a choose with no game_start is answered after an
// implicit start; a second game_start replaces a stale game; candidates
// without ids are answered by position.
func TestResyncRecoveries(t *testing.T) {
	a, err := New(&Heuristic{}, Options{Name: "t", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if id, code := selection(t, a.HandleLine([]byte(rcChoose))); code != "" || id != 1 {
		t.Fatalf("choose without game_start: %d %q", id, code)
	}
	if a.Stats.GamesAdopted != 1 || a.Game == nil || a.Game.Seat != "p0" {
		t.Fatalf("adoption: stats %+v game %+v", a.Stats, a.Game)
	}
	next := strings.ReplaceAll(rcStart, `"g-1"`, `"g-2"`)
	if out := a.HandleLine([]byte(next)); !strings.Contains(string(out), `"response_type":"ack"`) {
		t.Fatalf("game_start over a stale game: %s", out)
	}
	if a.Stats.GamesReplaced != 1 {
		t.Fatalf("stats %+v", a.Stats)
	}
	noIDs := `{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-3","game_id":"g-2","decision":{"candidates":[{"semantic":{"kind":"pass"}},{"semantic":{"kind":"cast_spell"}}]}}`
	if id, code := selection(t, a.HandleLine([]byte(noIDs))); code != "" || id != 1 {
		t.Fatalf("dense ids: %d %q", id, code)
	}
	if a.Stats.DenseIDs != 2 {
		t.Fatalf("stats %+v", a.Stats)
	}
	// Nothing to answer is still an error (no legal action exists).
	empty := `{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-4","game_id":"g-2","decision":{"candidates":[]}}`
	if _, code := selection(t, a.HandleLine([]byte(empty))); code != ErrMalformedRequest {
		t.Fatalf("empty candidates: %q", code)
	}
}

// TestEchoGuard: an echo whose canonical re-encoding would change its value
// (invalid UTF-8, repaired by the encoder) is omitted, not sent to fail the
// host's canonical comparison.
func TestEchoGuard(t *testing.T) {
	if !echoPreservesValue(json.RawMessage(`{"b":1,"a":[1.5,"x",null,true]}`)) {
		t.Fatal("a plain semantic must be echoed")
	}
	if echoPreservesValue(json.RawMessage("{\"a\":\"\xff\"}")) {
		t.Fatal("an invalid UTF-8 semantic must not be echoed")
	}
}
