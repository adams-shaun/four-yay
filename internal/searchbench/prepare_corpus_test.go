package searchbench

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestPrepareItemCorpus runs the gorge half over a prep.py batch file
// (SEARCHBENCH_PAYLOADS, a candidates JSONL.gz from `prep.py batch`; 17lands
// derived, never committed) and logs the item and refusal counts.
// SEARCHBENCH_PAYLOADS_LIMIT bounds the payloads read;
// SEARCHBENCH_PAYLOADS_DIFF=1 logs the first differing board bytes of an
// observation refusal.
func TestPrepareItemCorpus(t *testing.T) {
	path := os.Getenv("SEARCHBENCH_PAYLOADS")
	if path == "" {
		t.Skip("SEARCHBENCH_PAYLOADS unset")
	}
	limit, _ := strconv.Atoi(os.Getenv("SEARCHBENCH_PAYLOADS_LIMIT"))
	diff := os.Getenv("SEARCHBENCH_PAYLOADS_DIFF") == "1"
	reg := testutil.CorpusRegistry(t)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	s := bufio.NewScanner(zr)
	s.Buffer(make([]byte, 0, 1<<20), 64<<20)
	reasons, details := map[string]int{}, map[string]int{}
	n, ok := 0, 0
	for s.Scan() {
		var p Payload
		if err := json.Unmarshal(s.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.Reject != "" || p.Error != "" {
			continue
		}
		n++
		if limit > 0 && n > limit {
			break
		}
		_, err := PrepareItem(reg, &p, SBV1WorldSeeds())
		var ir *ItemRefusal
		switch {
		case err == nil:
			ok++
			continue
		case errors.As(err, &ir):
			reasons[p.Kind+": "+ir.Key()]++
			d := ir.Error()
			if len(d) > 110 {
				d = d[:110]
			}
			details[p.Kind+": "+d]++
		default:
			t.Fatalf("row %d turn %d %s: %v", p.Row, p.Turn, p.Kind, err)
		}
		if diff && ir != nil && ir.Stage == StageWorlds && reasons[p.Kind+": "+ir.Key()] <= 3 {
			logBoardDiff(t, reg, &p)
		}
	}
	t.Logf("payloads %d  items %d", n, ok)
	logSorted(t, "reason", reasons)
	logSorted(t, "detail", details)
}

func logBoardDiff(t *testing.T, reg any, p *Payload) {
	t.Helper()
	req, _ := parseRequest(p.Opts)
	kind := DecisionType(p.Kind)
	pos, err := positionRaw(testutil.CorpusRegistry(t), kind, req, p.Real, p.BuildSeed, p.Worlds, SBV1WorldSeeds(), WorldCount)
	if err != nil {
		t.Logf("row %d turn %d %s: %v", p.Row, p.Turn, p.Kind, err)
		return
	}
	actor := pos.Real.Reached.Decision.Player
	_, wantJSON, _ := searchprobe.NewCollector(actor).CaptureBoardJSON(pos.Real.M.Engine, nil)
	for i, w := range pos.Worlds {
		_, gotJSON, _ := searchprobe.NewCollector(actor).CaptureBoardJSON(w.M.Engine, nil)
		a, b := string(wantJSON), string(gotJSON)
		if a == b {
			continue
		}
		k := 0
		for k < len(a) && k < len(b) && a[k] == b[k] {
			k++
		}
		lo := max(0, k-300)
		t.Logf("row %d turn %d %s world %d differs at %d:\n real  ...%s\n world ...%s", p.Row, p.Turn, p.Kind, i, k, a[lo:min(len(a), k+200)], b[lo:min(len(b), k+200)])
		if dir := os.Getenv("SEARCHBENCH_PAYLOADS_DUMP"); dir != "" {
			_ = os.WriteFile(fmt.Sprintf("%s/%d-%d-%s-real.json", dir, p.Row, p.Turn, p.Kind), wantJSON, 0o644)
			_ = os.WriteFile(fmt.Sprintf("%s/%d-%d-%s-w%d.json", dir, p.Row, p.Turn, p.Kind, i), gotJSON, 0o644)
		}
		return
	}
}
