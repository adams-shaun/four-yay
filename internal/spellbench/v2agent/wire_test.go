package v2agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

// testVectors is goldens/protocol_v2/test_vectors.json from the spellbench
// protocol-v2 branch (MIT, copied verbatim).
type testVectors struct {
	AgentSeed       map[string]uint64 `json:"agent_seed"`
	CanonicalSHA256 struct {
		SHA256 string         `json:"sha256"`
		Value  map[string]any `json:"value"`
	} `json:"canonical_sha256"`
	RunSecret      string `json:"run_secret"`
	ReferenceExtra struct {
		AgentSeed map[string]uint64 `json:"agent_seed"`
	} `json:"reference_extra"`
}

func loadVectors(t *testing.T) testVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/test_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v testVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestCanonicalMatchesSpecVector: spec 16's canonical-JSON vector (a raw
// UTF-8 u-circumflex, unescaped apostrophes, a tab written \t).
func TestCanonicalMatchesSpecVector(t *testing.T) {
	v := loadVectors(t)
	out, err := Canonical(v.CanonicalSHA256.Value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(out)
	if got := hex.EncodeToString(sum[:]); got != v.CanonicalSHA256.SHA256 {
		t.Fatalf("canonical %s hashes to %s, want %s", out, got, v.CanonicalSHA256.SHA256)
	}
}

func TestCanonicalForm(t *testing.T) {
	raw := json.RawMessage(`{ "b" : [1, -0, 20], "a": "x\u0001 <>&\"\\", "c": {"z": null, "y": true} }`)
	got, err := Canonical(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":\"x\\u0001 <>&\\\"\\\\\",\"b\":[1,0,20],\"c\":{\"y\":true,\"z\":null}}"
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// TestAgentSeedMatchesSpecVectors: spec 16 and the reference's extra
// vectors for agent_seed(i, s).
func TestAgentSeedMatchesSpecVectors(t *testing.T) {
	v := loadVectors(t)
	secret, err := hex.DecodeString(v.RunSecret)
	if err != nil || len(secret) != 32 {
		t.Fatalf("run_secret %q: %v", v.RunSecret, err)
	}
	all := map[string]uint64{}
	for k, s := range v.AgentSeed {
		all[k] = s
	}
	for k, s := range v.ReferenceExtra.AgentSeed {
		all[k] = s
	}
	if len(all) < 8 {
		t.Fatalf("only %d agent_seed vectors", len(all))
	}
	for key, want := range all {
		index, seat, _ := strings.Cut(key, ":")
		i, err := strconv.ParseUint(index, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if got := AgentSeed(secret, i, seat); got != want {
			t.Errorf("agent_seed(%s) = %d, want %d", key, got, want)
		}
	}
}

func TestLineReaderFraming(t *testing.T) {
	long := strings.Repeat("x", MaxLineBytes+1)
	exact := strings.Repeat("y", MaxLineBytes)
	input := "a\r\n" + long + "\n" + "b\n" + exact + "\n" + "c"
	lr := NewLineReader(strings.NewReader(input))
	expect := func(want string, wantErr error) {
		t.Helper()
		line, err := lr.ReadLine()
		if !errors.Is(err, wantErr) {
			t.Fatalf("err %v, want %v", err, wantErr)
		}
		if wantErr == nil && string(line) != want {
			t.Fatalf("line %.20q (len %d), want %.20q (len %d)", line, len(line), want, len(want))
		}
	}
	expect("a", nil)
	expect("", ErrLineTooLong) // the over-long line is discarded whole
	expect("b", nil)           // and the next line is read normally
	expect(exact, nil)         // exactly 8 MiB of content is accepted
	expect("", ErrUnterminated)
	expect("", io.EOF)
}

// TestServeAnswersEveryLine: one response per request line, an over-long
// line answered malformed_json with request_id "" (spec 2, 4.1), and a clean
// EOF ends the loop.
func TestServeAnswersEveryLine(t *testing.T) {
	a, err := New(First{}, Options{Name: "t", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	hello := `{"request_type":"hello","protocol":"spellbench/v2","request_id":"r-0","protocol_minor":0}`
	input := hello + "\n" + strings.Repeat(" ", MaxLineBytes+5) + "\n" + strings.Replace(hello, "r-0", "r-1", 1) + "\r\n"
	var out bytes.Buffer
	if err := a.Serve(strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("%d response lines: %q", len(lines), out.String())
	}
	for i, want := range []string{`"request_id":"r-0","requires"`, `"code":"malformed_json"`, `"request_id":"r-1","requires"`} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d %s lacks %s", i, lines[i], want)
		}
	}
	if !strings.Contains(lines[1], `"request_id":""`) {
		t.Errorf("over-long line answered with a request_id: %s", lines[1])
	}
}
