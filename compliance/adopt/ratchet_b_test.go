package adopt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// levelBStatusChildEnv makes TestStatusBChild run the level-B gate for these
// sets and print their lines. It mirrors statusChildEnv (ratchet_test.go),
// which is fixed to level A.
const levelBStatusChildEnv = "GORGE_ADOPT_STATUS_B_SETS"

func TestStatusBChild(t *testing.T) {
	sets := os.Getenv(levelBStatusChildEnv)
	if sets == "" {
		return // only meaningful as TestLevelBRatchet's child
	}
	reg := testutil.CorpusRegistry(t)
	cs, err := Build(reg, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cs.Status(reg, root, "B", strings.Split(sets, ",")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		b, _ := json.Marshal(s)
		os.Stdout.WriteString("STATUS " + string(b) + "\n")
	}
}

func runStatusBChild(batch []string) ([]SetStatus, error) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestStatusBChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), levelBStatusChildEnv+"="+strings.Join(batch, ","))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, &childError{err, string(out)}
	}
	var sets []SetStatus
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "STATUS ")
		if !ok {
			continue
		}
		var s SetStatus
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, err
		}
		sets = append(sets, s)
	}
	return sets, sc.Err()
}

// TestLevelBRatchet holds the sets that carry a level-B floor in
// compliance/ratchet.json to it. It measures at level B only those sets, so
// its cost grows with the B claim, exactly as TestCertificationRatchet grows
// with the declared claim. Record an improved count with
// `go run ./cmd/oraclediff status -all -level B -sets <SETS> -write-ratchet`.
func TestLevelBRatchet(t *testing.T) {
	if os.Getenv(levelBStatusChildEnv) != "" {
		return
	}
	cs := census(t)
	ratchet, err := LoadRatchet(root)
	if err != nil {
		t.Fatal(err)
	}
	var scope []string
	for _, s := range cs.SetCodes() {
		if e, ok := ratchet[s]; ok && e.OutstandingB != nil {
			scope = append(scope, s)
		}
	}
	if len(scope) == 0 {
		t.Fatal("no set carries an outstanding_b floor; record one with `oraclediff status -all -level B -sets BIG,EOE,FDN,FRA -write-ratchet`")
	}
	measured, err := cs.StatusChunked(scope, 2, runStatusBChild)
	if err != nil {
		t.Fatal(err)
	}
	if len(measured) != len(scope) {
		t.Fatalf("measured %d of %d sets", len(measured), len(scope))
	}
	fails, slack := CheckRatchetB(measured, ratchet)
	for _, f := range fails {
		t.Error(f)
	}
	if len(slack) > 0 {
		t.Logf("improved past the level-B floor (record with -write-ratchet): %s", strings.Join(slack, "; "))
	}
	t.Logf("level-B ratchet: %d sets measured: %s", len(measured), strings.Join(scope, ","))
}

// TestRatchetAWriteKeepsOutstandingB is the carry-forward rule: a level-A
// -write-ratchet rebuilds the level-A entries from the run, so it must keep
// each entry's existing level-B floor instead of dropping it.
func TestRatchetAWriteKeepsOutstandingB(t *testing.T) {
	floor := 384
	prev := map[string]RatchetEntry{
		"FRA": {Level: "A", Outstanding: 0, OutstandingB: &floor},
	}
	sets := []SetStatus{{Set: "FRA", Declared: "A", Outstanding: 0}}
	got := RatchetOf(sets, prev)
	e, ok := got["FRA"]
	if !ok {
		t.Fatal("RatchetOf dropped FRA")
	}
	if e.OutstandingB == nil || *e.OutstandingB != floor {
		t.Fatalf("A write dropped outstanding_b: %+v, want floor %d", e.OutstandingB, floor)
	}
	if e.Outstanding != 0 || e.Level != "A" {
		t.Fatalf("A write did not record the level-A fields: %+v", e)
	}
}

// TestCheckRatchetBFloors is the B ratchet's two directions: a measured count
// above its floor fails, one below it is slack, and a set with no B floor is
// not part of the claim.
func TestCheckRatchetBFloors(t *testing.T) {
	floor := 40
	ratchet := map[string]RatchetEntry{
		"FRA": {OutstandingB: &floor},
		"FDN": {}, // never measured at B
	}
	fails, slack := CheckRatchetB([]SetStatus{{Set: "FRA", Outstanding: 41}, {Set: "FDN", Outstanding: 999}}, ratchet)
	if len(fails) != 1 || !strings.Contains(fails[0], "FRA: 41 outstanding at level B, ratchet 40") {
		t.Errorf("growth: fails = %v, want the FRA 41 > 40 failure", fails)
	}
	if len(slack) != 0 {
		t.Errorf("growth: slack = %v, want none", slack)
	}
	fails, slack = CheckRatchetB([]SetStatus{{Set: "FRA", Outstanding: 39}}, ratchet)
	if len(fails) != 0 {
		t.Errorf("improvement: fails = %v, want none", fails)
	}
	if len(slack) != 1 || !strings.Contains(slack[0], "FRA: 40 -> 39") {
		t.Errorf("improvement: slack = %v, want the FRA 40 -> 39 entry", slack)
	}
}
