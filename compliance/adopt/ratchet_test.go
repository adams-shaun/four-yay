package adopt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// statusChildEnv makes TestStatusChild run the gate for these sets (a
// comma list) and print their lines: TestCertificationRatchet's child
// process (see ChildMaxCards for why the gate runs in children).
const statusChildEnv = "GORGE_ADOPT_STATUS_SETS"

// ratchetChildSem bounds the two ratchet tests' child processes TOGETHER to
// the four StatusChunked allows one test. The tests run in parallel (they
// share only the read-only census and their children are separate
// processes), so without a shared bound a grown level-B claim could put
// 4+4 children at ~0.6 GiB each on one binary.
var ratchetChildSem = make(chan struct{}, 4)

func TestStatusChild(t *testing.T) {
	sets := os.Getenv(statusChildEnv)
	if sets == "" {
		return // only meaningful as TestCertificationRatchet's child
	}
	reg := testutil.CorpusRegistry(t)
	cs, err := Build(reg, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cs.Status(reg, root, "A", strings.Split(sets, ",")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		b, _ := json.Marshal(s)
		os.Stdout.WriteString("STATUS " + string(b) + "\n")
	}
}

func runStatusChild(batch []string) ([]SetStatus, error) {
	ratchetChildSem <- struct{}{}
	defer func() { <-ratchetChildSem }()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStatusChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), statusChildEnv+"="+strings.Join(batch, ","))
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

type childError struct {
	err error
	out string
}

func (e *childError) Error() string { return e.err.Error() + ": " + e.out }

// TestCertificationRatchet is section 11.3 C8's ratchet, modelled on
// knownUnsupported: compliance/ratchet.json records every committed set's
// declared level (a floor: a declared set or level only grows) and its
// outstanding count at level A (a ceiling: it only shrinks). Every set
// must be in the file and every declaration recorded in it. The gate is
// re-run here for the first target format's sets and every declared set
// (where the verdicts live); `make compliance-status` re-runs it for all
// of them. A set that improved passes and is logged: record it with
// `go run ./cmd/oraclediff status -all -sets <SETS> -write-ratchet`.
func TestCertificationRatchet(t *testing.T) {
	t.Parallel()
	if os.Getenv(statusChildEnv) != "" {
		return
	}
	cs := census(t)
	ratchet, err := LoadRatchet(root)
	if err != nil {
		t.Fatal(err)
	}
	declared, err := compliance.LoadDeclared(filepath.Join(root, "compliance", "declared.json"))
	if err != nil {
		t.Fatal(err)
	}
	scope := cs.FormatSets(cs.Config.Formats[0].Name)
	for s := range declared {
		if !contains(scope, s) {
			scope = append(scope, s)
		}
	}
	// 4 concurrent children (the cap StatusChunked allows): the 20-set
	// Standard scope is 3 batches of ChildMaxCards, and at 2 they ran in two
	// rounds, each child re-paying the corpus load. Measured 2026-10-09
	// (4 vCPU budget): 16.4s -> 9.1s; four children peak ~2.4 GiB RSS
	// (~0.6 GiB each, status.go) plus this process, inside the doubled
	// 2026-10-09 per-test budget.
	measured, err := cs.StatusChunked(scope, 4, runStatusChild)
	if err != nil {
		t.Fatal(err)
	}
	if len(measured) != len(scope) {
		t.Fatalf("measured %d of %d sets", len(measured), len(scope))
	}
	fails, slack := CheckRatchet(cs.SetCodes(), measured, declared, ratchet)
	for _, f := range fails {
		t.Error(f)
	}
	if len(slack) > 0 {
		t.Logf("improved past the ratchet (record with -write-ratchet): %s", strings.Join(slack, "; "))
	}
	for _, r := range cs.Rollups(measured) {
		if r.Format == cs.Config.Formats[0].Name {
			t.Logf("ratchet: %d sets measured; %s", len(measured), r.Label)
		}
	}
}
