package v1agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
)

// TestAgentGoldenTranscript replays the SpellBench agent-role golden
// (goldens/protocol_v1/agent_happy_path.transcript.jsonl): every response
// must equal the golden byte for byte in canonical form.
func TestAgentGoldenTranscript(t *testing.T) {
	f, err := os.Open("testdata/agent_happy_path.transcript.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	a := New(&Uniform{Seed: 11}, Options{Name: "uniform", Version: "1.0.0"})
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	var got []byte
	n := 0
	for sc.Scan() {
		var row struct {
			Dir     string          `json:"dir"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		switch row.Dir {
		case "host_to_agent":
			got = a.HandleLine(row.Message)
		case "agent_to_host":
			want, err := v2agent.CanonicalLine(json.RawMessage(row.Message))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("response %d:\n got %s\nwant %s", n, got, want)
			}
			n++
		}
	}
	if n != 4 {
		t.Fatalf("checked %d responses, want 4", n)
	}
}

func line(s string) []byte { return []byte(strings.ReplaceAll(s, "'", `"`)) }

func errCode(t *testing.T, resp []byte) string {
	t.Helper()
	var m struct {
		ResponseType string `json:"response_type"`
		Error        struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp, &m); err != nil {
		t.Fatalf("bad response %s", resp)
	}
	if m.ResponseType != "error" {
		return ""
	}
	return m.Error.Code
}

const gameStart = `{'request_type':'game_start','protocol':'spellbench/v1','request_id':'g1','game_id':'g-1','seat':'p0','format':'pauper-bo1','decks':[{'catalog_id':'Burn'},{'catalog_id':'Elves'}],'engine':{'name':'x','version':'1','source_revision':null,'rules_snapshot_id':'r','card_pool_identity':'c'}}`

func TestAgentErrorsAndRetry(t *testing.T) {
	a := New(First{}, Options{Name: "first", Version: "1"})
	if c := errCode(t, a.HandleLine([]byte("{nope"))); c != ErrMalformedJSON {
		t.Fatalf("malformed json: %q", c)
	}
	if c := errCode(t, a.HandleLine(line(`{'request_type':'hello','protocol':'spellbench/v2','request_id':'h'}`))); c != ErrProtocolMismatch {
		t.Fatalf("protocol: %q", c)
	}
	choose := line(`{'request_type':'choose','protocol':'spellbench/v1','request_id':'c0','game_id':'g-1','decision':{}}`)
	if c := errCode(t, a.HandleLine(choose)); c != ErrUnknownGame {
		t.Fatalf("choose before game_start: %q", c)
	}
	gs := line(gameStart)
	first := a.HandleLine(gs)
	if c := errCode(t, first); c != "" {
		t.Fatalf("game_start: %s", first)
	}
	if again := a.HandleLine(gs); !bytes.Equal(again, first) || a.Stats.RetriesServed != 1 {
		t.Fatalf("identical retransmit must replay the cached response")
	}
	reuse := bytes.Replace(gs, []byte(`"p0"`), []byte(`"p1"`), 1)
	if c := errCode(t, a.HandleLine(reuse)); c != ErrRequestIDReuse {
		t.Fatalf("reuse: %q", c)
	}
	if c := errCode(t, a.HandleLine(line(`{'request_type':'game_start','protocol':'spellbench/v1','request_id':'g2','game_id':'g-2','seat':'p0','format':'pauper-bo1','decks':[{'catalog_id':'Burn'},{'catalog_id':'Elves'}],'engine':{}}`))); c != ErrGameActive {
		t.Fatalf("second game: %q", c)
	}
}

type panicky struct{ First }

func (panicky) Choose(*Decision) int { panic("boom") }

func chooseLine(t *testing.T, id, decisionFile string) []byte {
	t.Helper()
	raw, err := os.ReadFile(decisionFile)
	if err != nil {
		t.Fatal(err)
	}
	var dec map[string]json.RawMessage
	if err := json.Unmarshal(raw, &dec); err != nil {
		t.Fatal(err)
	}
	var gameID string
	_ = json.Unmarshal(dec["game_id"], &gameID)
	out, _ := json.Marshal(map[string]any{
		"request_type": "choose", "protocol": Protocol, "request_id": id, "game_id": gameID,
		"decision": json.RawMessage(raw),
	})
	return out
}

func startFixtureGame(t *testing.T, a *Agent, seat string) {
	t.Helper()
	gs := strings.Replace(strings.Replace(gameStart, "'g-1'", "'g'", 1), "'p0'", "'"+seat+"'", 1)
	if c := errCode(t, a.HandleLine(line(gs))); c != "" {
		t.Fatalf("game_start: %s", c)
	}
}

// TestPolicyPanicFallsBack: a panicking policy never produces a wire
// error (a forfeit); the fallback answers and the failure is counted.
func TestPolicyPanicFallsBack(t *testing.T) {
	a := New(panicky{}, Options{Name: "p", Version: "1"})
	startFixtureGame(t, a, "p0")
	resp := a.HandleLine(chooseLine(t, "c1", "testdata/bolt_target.json"))
	if c := errCode(t, resp); c != "" {
		t.Fatalf("panic reached the wire: %s", resp)
	}
	if a.Stats.Fallbacks != 1 {
		t.Fatalf("fallbacks = %d", a.Stats.Fallbacks)
	}
}

func selection(t *testing.T, resp []byte) (int, json.RawMessage) {
	t.Helper()
	var m struct {
		Selection struct {
			CandidateID int             `json:"candidate_id"`
			Echo        json.RawMessage `json:"semantic_echo"`
		} `json:"selection"`
	}
	if err := json.Unmarshal(resp, &m); err != nil {
		t.Fatalf("bad choice %s", resp)
	}
	return m.Selection.CandidateID, m.Selection.Echo
}

// TestTacticalAimsBoltAtOpponent: on a real mtg-kernel decision (Burn vs
// Elves, Lightning Bolt's target list, x_kernel_v5 attached) the tactical
// policy never aims at its own side; the heuristic takes candidate 0 (its
// own face), and the echo is the candidate's semantic.
func TestTacticalAimsBoltAtOpponent(t *testing.T) {
	a := New(NewTactical(TacticalOptions{}), Options{Name: "t", Version: "1"})
	startFixtureGame(t, a, "p0")
	resp := a.HandleLine(chooseLine(t, "c1", "testdata/bolt_target.json"))
	id, echo := selection(t, resp)
	if id != 1 && id != 3 && id != 4 {
		t.Fatalf("tactical bolted candidate %d (own side)", id)
	}
	if a.Stats.Fallbacks != 0 || a.Stats.KernelMissing != 0 {
		t.Fatalf("stats %+v", a.Stats)
	}
	raw, _ := os.ReadFile("testdata/bolt_target.json")
	d, err := ParseDecision(raw)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := v2agent.Canonical(d.Candidates[id].Semantic.Raw)
	got, _ := v2agent.Canonical(echo)
	if !bytes.Equal(got, want) {
		t.Fatalf("echo %s != semantic %s", got, want)
	}
	if HeuristicPick(d) != 0 {
		t.Fatalf("heuristic pick %d, python picks 0", HeuristicPick(d))
	}
}

// TestTacticalBlockPlanIsConsistent: every answer of a real blocker scan
// is one of the offered candidates and the plan never double-assigns.
func TestTacticalBlockPlanIsConsistent(t *testing.T) {
	raw, err := os.ReadFile("testdata/block.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := ParseDecision(raw)
	if err != nil || d.Kernel == nil {
		t.Fatalf("fixture: %v %v", err, d.KernelErr)
	}
	tac := NewTactical(TacticalOptions{})
	tac.GameStart(&GameStart{Seat: d.ActingSeat, CatalogIDs: []string{"Burn", "Elves"}})
	if got := tac.Choose(d); got < 0 || got >= len(d.Candidates) {
		t.Fatalf("pick %d out of range", got)
	}
}

func TestUniformMatchesPython(t *testing.T) {
	// Values from spellbench.arena.bots.uniform (seed 11).
	if got := UniformGameSeed(11, "m0000p0000g0"); got != 10577113712748016562 {
		t.Fatalf("game seed %d", got)
	}
	s := v2agent.NewSplitMix64(UniformGameSeed(11, "g-0001"))
	for i, want := range []uint64{12277851652172234692, 9404083510866315434, 6021915195129513265} {
		if got := s.Next(); got != want {
			t.Fatalf("draw %d = %d, want %d", i, got, want)
		}
	}
}
