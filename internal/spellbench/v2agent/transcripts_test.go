package v2agent

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// transcript is one line of testdata/agent_transcripts.jsonl.gz, written by
// cmd/sbagent/scripts/sbv2_harness.py fixtures: the exact agent-role request
// lines one seat received in a game on the spellbench v2 fake engine
// (python/tests/fake_v2_engine.py, the scoring game and the Task 26 tours),
// played through the reference game loop (spellbench.host.game.play_game),
// and each python v2 builtin's answer to every line
// (spellbench.bot.BotSession.handle_line). The agent_errors transcript is a
// hand-written run of malformed and out-of-sequence requests.
type transcript struct {
	Name     string              `json:"name"`
	Seat     *string             `json:"seat"`
	Requests []string            `json:"requests"`
	Expected map[string][]string `json:"expected"`
}

func loadTranscripts(t *testing.T) []transcript {
	t.Helper()
	f, err := os.Open("testdata/agent_transcripts.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var out []transcript
	sc := bufio.NewScanner(gz)
	sc.Buffer(make([]byte, 1<<20), MaxLineBytes)
	for sc.Scan() {
		var tr transcript
		if err := json.Unmarshal(sc.Bytes(), &tr); err != nil {
			t.Fatal(err)
		}
		out = append(out, tr)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) < 6 {
		t.Fatalf("only %d transcripts", len(out))
	}
	return out
}

// twins pairs each Go policy with the python builtin it must reproduce.
var twins = []struct{ goPolicy, python string }{
	{"first", "first"},
	{"heuristic", "heuristic"},
	{"random", "uniform"}, // same SplitMix64 derivation, python --seed 0
}

// knownDeviations are requests this agent answers differently from the
// python reference bot on purpose: a top level that is valid JSON but not an
// object. The reference answers malformed_json; this agent answers
// malformed_request, the code spec 9.8 gives a non-object top level (spec
// 10.5 defines malformed_json as "not valid JSON").
var knownDeviations = map[string]string{"[1,2]": ErrMalformedRequest, "null": ErrMalformedRequest}

// recoveries are the agent_errors requests (by request_id) this agent
// answers where the python reference agent errors -- and the host forfeits
// the seat for any error answering game_start or choose (spec 10.5, 11.5).
// Each is a recoverable error (Agent.Stats): a game_start while a game is
// active replaces the stale game; a choose for a game the agent is not
// serving starts it implicitly when there is something to answer; a
// candidate without an integer candidate_id is answered by position (ids
// are dense, spec 11.3 V1). The state they leave differs from python's, so
// r-4 and r-5 differ too: g-2 is now the active game.
var recoveries = map[string]string{
	"r-3": "ack",                          // game_start g-2 while g-1 is active
	"r-4": "error:" + ErrMalformedRequest, // g-2 is active; no candidates
	"r-5": "error:" + ErrUnknownGame,      // g-1 was replaced; nothing to answer
	"r-6": "choice:0",                     // g-1 adopted; missing candidate_id
	"r-7": "choice:0",                     // non-object candidate
	"r-8": "choice:0",                     // string candidate_id
}

type response struct {
	ResponseType string `json:"response_type"`
	Protocol     string `json:"protocol"`
	RequestID    string `json:"request_id"`
	Error        *struct {
		Code string `json:"code"`
	} `json:"error"`
	Selection *struct {
		CandidateID  *int64          `json:"candidate_id"`
		SeatStep     *int64          `json:"seat_step"`
		SemanticEcho json.RawMessage `json:"semantic_echo"`
	} `json:"selection"`
}

// TestTranscriptsMatchPythonBuiltins replays every recorded request line
// through the Go agent with each policy and requires the python twin's
// answer: the same response type, request id, error code and candidate_id.
// It also checks the echoes the python bots omit: seat_step equals the
// decision's and semantic_echo is canonically equal to the chosen
// candidate's semantic.
func TestTranscriptsMatchPythonBuiltins(t *testing.T) {
	choices := 0
	for _, tr := range loadTranscripts(t) {
		for _, tw := range twins {
			want := tr.Expected[tw.python]
			if len(want) != len(tr.Requests) {
				t.Fatalf("%s: %d expected answers for %d requests", tr.Name, len(want), len(tr.Requests))
			}
			policy, err := NewPolicy(tw.goPolicy, 0)
			if err != nil {
				t.Fatal(err)
			}
			agent, err := New(policy, Options{Name: "sbagent-" + tw.goPolicy, Version: "test"})
			if err != nil {
				t.Fatal(err)
			}
			for i, req := range tr.Requests {
				out := agent.HandleLine([]byte(req))
				if !bytes.HasSuffix(out, []byte("\n")) || bytes.Count(out, []byte("\n")) != 1 {
					t.Fatalf("%s/%s #%d: response is not one line: %q", tr.Name, tw.goPolicy, i, out)
				}
				var got, exp response
				if err := json.Unmarshal(out, &got); err != nil {
					t.Fatalf("%s/%s #%d: %v", tr.Name, tw.goPolicy, i, err)
				}
				if err := json.Unmarshal([]byte(want[i]), &exp); err != nil {
					t.Fatal(err)
				}
				canon, err := Canonical(json.RawMessage(out[:len(out)-1]))
				if err != nil || !bytes.Equal(canon, out[:len(out)-1]) {
					t.Errorf("%s/%s #%d: response is not canonical JSON: %s", tr.Name, tw.goPolicy, i, out)
				}
				if tr.Name == "agent_errors" {
					if want, ok := recoveries[got.RequestID]; ok {
						var have string
						switch {
						case got.Error != nil:
							have = "error:" + got.Error.Code
						case got.Selection != nil && got.Selection.CandidateID != nil:
							have = fmt.Sprintf("choice:%d", *got.Selection.CandidateID)
						default:
							have = got.ResponseType
						}
						if have != want {
							t.Errorf("%s/%s #%d: recovery %s, want %s (%s)", tr.Name, tw.goPolicy, i, have, want, out)
						}
						continue
					}
				}
				if code, ok := knownDeviations[req]; ok {
					if got.Error == nil || got.Error.Code != code {
						t.Errorf("%s #%d %q: got %s, want error %s", tr.Name, i, req, out, code)
					}
					continue
				}
				if got.ResponseType != exp.ResponseType || got.RequestID != exp.RequestID || got.Protocol != Protocol {
					t.Errorf("%s/%s #%d: got %s\nwant %s", tr.Name, tw.goPolicy, i, out, want[i])
					continue
				}
				if exp.Error != nil {
					if got.Error == nil || got.Error.Code != exp.Error.Code {
						t.Errorf("%s/%s #%d: got %s, want error %s", tr.Name, tw.goPolicy, i, out, exp.Error.Code)
					}
					continue
				}
				if exp.Selection == nil {
					continue
				}
				if tr.Name == "agent_errors" && tw.goPolicy == "random" && (got.RequestID == "r-9" || got.RequestID == "r-10") {
					// The recoveries above drew three answers from a stream
					// re-seeded by the adoption of g-1; python's drew none.
					continue
				}
				choices++
				if got.Selection == nil || got.Selection.CandidateID == nil || *got.Selection.CandidateID != *exp.Selection.CandidateID {
					t.Errorf("%s/%s #%d: got %s, python %s chose %d", tr.Name, tw.goPolicy, i, out, tw.python, *exp.Selection.CandidateID)
					continue
				}
				checkEchoes(t, tr.Name+"/"+tw.goPolicy, req, got.Selection.CandidateID, got.Selection.SeatStep, got.Selection.SemanticEcho)
			}
		}
	}
	if choices < 150 {
		t.Fatalf("only %d choices compared", choices)
	}
}

// checkEchoes verifies seat_step and semantic_echo against the request.
func checkEchoes(t *testing.T, label, req string, candidateID *int64, seatStep *int64, echo json.RawMessage) {
	t.Helper()
	var r struct {
		Decision struct {
			SeatStep   *int64 `json:"seat_step"`
			Candidates []struct {
				CandidateID int64           `json:"candidate_id"`
				Semantic    json.RawMessage `json:"semantic"`
			} `json:"candidates"`
		} `json:"decision"`
	}
	if err := json.Unmarshal([]byte(req), &r); err != nil {
		t.Fatal(err)
	}
	if (r.Decision.SeatStep == nil) != (seatStep == nil) || (seatStep != nil && *seatStep != *r.Decision.SeatStep) {
		t.Errorf("%s: seat_step echo %v, decision has %v", label, seatStep, r.Decision.SeatStep)
	}
	for _, c := range r.Decision.Candidates {
		if c.CandidateID != *candidateID {
			continue
		}
		want, err1 := Canonical(c.Semantic)
		got, err2 := Canonical(echo)
		if err1 != nil || err2 != nil || !bytes.Equal(want, got) {
			t.Errorf("%s: semantic_echo %s, candidate semantic %s", label, echo, c.Semantic)
		}
		return
	}
	t.Errorf("%s: chose candidate_id %d, which the decision does not offer", label, *candidateID)
}

// TestTypedDecodeRoundTrips decodes every recorded game_start, seat
// decision and candidate semantic into the typed structs with unknown fields
// disallowed, and re-encodes the game_start and seat decision: the canonical
// bytes must equal the original's, so the structs model every field of
// spec 6, 9.3 and 10.2 with the right nullability.
func TestTypedDecodeRoundTrips(t *testing.T) {
	var decisions, semantics, starts int
	kinds := map[string]bool{}
	observed := map[string]bool{}
	for _, tr := range loadTranscripts(t) {
		if tr.Seat == nil {
			continue
		}
		for _, req := range tr.Requests {
			var top map[string]json.RawMessage
			if err := json.Unmarshal([]byte(req), &top); err != nil {
				t.Fatal(err)
			}
			switch mustString(t, top["request_type"]) {
			case "game_start":
				var gs gameStartRequest
				strictDecode(t, tr.Name+" game_start", []byte(req), &gs)
				roundTrip(t, tr.Name+" game_start", []byte(req), gs)
				starts++
			case "choose":
				var sd SeatDecision
				strictDecode(t, tr.Name+" decision", top["decision"], &sd)
				roundTrip(t, tr.Name+" decision", top["decision"], sd)
				decisions++
				noteObservation(observed, &sd.Observation)
				for _, c := range sd.Candidates {
					var s Semantic
					strictDecode(t, tr.Name+" semantic", c.Semantic, &s)
					kinds[s.Kind] = true
					semantics++
				}
			}
		}
	}
	if len(kinds) < 30 {
		t.Errorf("transcripts cover %d kinds, want all 30 v2.0 kinds: %v", len(kinds), kinds)
	}
	for _, feature := range []string{"known", "stack", "pending_triggers", "permanent", "keywords", "exiled_by", "face_down", "progress", "counters", "day_night", "passed_seats"} {
		if !observed[feature] {
			t.Errorf("no recorded observation exercises %s", feature)
		}
	}
	t.Logf("%d game_starts, %d decisions, %d semantics, %d kinds", starts, decisions, semantics, len(kinds))
}

// noteObservation records which optional observation features a decision
// exercises, so the round trip is known to cover them.
func noteObservation(seen map[string]bool, o *Observation) {
	mark := func(name string, on bool) {
		if on {
			seen[name] = true
		}
	}
	mark("known", len(o.Known) > 0)
	mark("stack", len(o.Stack) > 0)
	mark("pending_triggers", len(o.PendingTriggers) > 0)
	mark("day_night", o.DayNight != nil)
	mark("passed_seats", o.PassedSeats != nil)
	for _, p := range o.Players {
		mark("progress", p.Progress != nil)
		mark("counters", p.Counters != nil)
		for _, zone := range [][]ObjectRecord{p.Hand, p.Battlefield, p.Graveyard, p.Exile, p.Command} {
			for _, r := range zone {
				mark("permanent", r.Permanent != nil)
				mark("exiled_by", r.ExiledBy != nil)
				mark("face_down", r.FaceDown)
				mark("keywords", r.Characteristics != nil && r.Characteristics.Keywords != nil)
			}
		}
	}
}

func mustString(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	s, ok := rawString(raw)
	if !ok {
		t.Fatalf("not a string: %s", raw)
	}
	return s
}

func strictDecode(t *testing.T, label string, raw []byte, target any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		t.Errorf("%s: strict decode: %v", label, err)
	}
	if unknown := UnknownFields(raw, target); len(unknown) > 0 {
		t.Errorf("%s: unknown fields %v", label, unknown)
	}
	checkUnknownOracle(t, label, raw, target)
}

func roundTrip(t *testing.T, label string, raw []byte, v any) {
	t.Helper()
	want, err := Canonical(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Canonical(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		i := 0
		for i < len(got) && i < len(want) && got[i] == want[i] {
			i++
		}
		lo := max(0, i-80)
		t.Errorf("%s: round trip differs at byte %d:\n got  ...%s\n want ...%s", label, i,
			got[lo:min(len(got), i+80)], want[lo:min(len(want), i+80)])
	}
}

// TestAgentKeepsLatestObservation: after a choose, Agent.Observation is the
// decision's typed observation.
func TestAgentKeepsLatestObservation(t *testing.T) {
	for _, tr := range loadTranscripts(t) {
		if tr.Name != "tour_board_p0" {
			continue
		}
		agent, err := New(First{}, Options{Name: "t", Version: "1"})
		if err != nil {
			t.Fatal(err)
		}
		var lastViewer string
		for _, req := range tr.Requests {
			agent.HandleLine([]byte(req))
			if strings.Contains(req, `"request_type":"choose"`) {
				if agent.Observation == nil {
					t.Fatal("no observation retained after choose")
				}
				lastViewer = agent.Observation.Viewer
				if agent.Observation.Me() == nil || agent.Observation.Me().Hand == nil {
					t.Error("viewer's own hand missing from the retained observation")
				}
				if opp := agent.Observation.Opponent(); opp == nil || opp.Hand != nil {
					t.Error("opponent hand should be present as null")
				}
			}
		}
		if lastViewer != *tr.Seat {
			t.Errorf("viewer %q, seat %q", lastViewer, *tr.Seat)
		}
		return
	}
	t.Fatal("tour_board_p0 transcript missing")
}
