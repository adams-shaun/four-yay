package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/gate"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

const forgeFixtures = "testdata/forge"

func fixtureLines(t *testing.T, name string) [][]byte {
	t.Helper()
	return readLinesBytes(t, filepath.Join(forgeFixtures, name))
}

// exportFixture exports the 3-item fixture to a fresh temp file and returns
// its bytes and path.
func exportFixture(t *testing.T, reg *cards.Registry) ([]byte, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "forge-req.jsonl")
	n, err := forgeExport(reg, filepath.Join(forgeFixtures, "items.jsonl"), out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("exported %d requests, want 3", n)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b, out
}

func decodeRequests(t *testing.T, b []byte) []forgeRequest {
	t.Helper()
	var out []forgeRequest
	for _, l := range bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n")) {
		var rq forgeRequest
		if err := json.Unmarshal(l, &rq); err != nil {
			t.Fatal(err)
		}
		out = append(out, rq)
	}
	return out
}

// TestForgeExportByteStable: two exports of the same scenarios are
// byte-identical, and each request carries its Item exactly as gen wrote it
// (so ItemSHA is the scenario_sha), gorge's decisions and the ability line.
func TestForgeExportByteStable(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	a, _ := exportFixture(t, reg)
	b, _ := exportFixture(t, reg)
	if !bytes.Equal(a, b) {
		t.Fatalf("two exports differ:\n%s\n---\n%s", a, b)
	}
	items := fixtureLines(t, "items.jsonl")
	reqs := decodeRequests(t, a)
	if len(reqs) != len(items) {
		t.Fatalf("%d requests for %d items", len(reqs), len(items))
	}
	for i, rq := range reqs {
		var it oraclegen.Item
		if err := json.Unmarshal(items[i], &it); err != nil {
			t.Fatal(err)
		}
		itemBytes, _ := json.Marshal(rq.Item)
		if !bytes.Equal(itemBytes, items[i]) {
			t.Errorf("item %d is not verbatim:\n got %s\nwant %s", i, itemBytes, items[i])
		}
		if rq.ID != it.ID || rq.ScenarioSHA != gate.ItemSHA(it) {
			t.Errorf("item %d id/sha = %q/%q, want %q/%q", i, rq.ID, rq.ScenarioSHA, it.ID, gate.ItemSHA(it))
		}
		if rq.Abilities == nil || rq.GorgeDecisions == nil {
			t.Errorf("item %d: abilities/gorge_decisions must encode as {} and [], not null", i)
		}
	}
	// Precondition for the assertions below: the cast at a creature posed a
	// target decision gorge answered, and the Cage's step 0 names an index.
	if len(reqs[0].GorgeDecisions) == 0 || reqs[0].GorgeDecisions[0].Kind != "target" {
		t.Fatalf("Shock request lacks gorge's target decision: %+v", reqs[0].GorgeDecisions)
	}
	if reqs[2].Item.Steps[0].AbilityIndex == nil {
		t.Fatal("fixture's Cage step 0 has no ability_index")
	}
	if len(reqs[0].Abilities) != 0 {
		t.Errorf("a cast with no ability_index got ability lines: %v", reqs[0].Abilities)
	}
	if l := reqs[2].Abilities["0"]; !strings.HasPrefix(l, "AB$ PutCounter") {
		t.Errorf("Cage step 0 ability line = %q, want the script's AB$ PutCounter line", l)
	}
	if len(reqs[2].Abilities) != 1 {
		t.Errorf("Cage abilities = %v, want only step 0", reqs[2].Abilities)
	}
}

// TestForgeExportRefusesRepoPath: the request carries script text, so it is
// never written inside the repository (nor under compliance/verdicts).
func TestForgeExportRefusesRepoPath(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, out := range []string{
		filepath.Join("..", "..", compliance.VerdictDir, "forge-req.jsonl"),
		filepath.Join("..", "..", "forge-req.jsonl"),
	} {
		_, err := forgeExport(reg, filepath.Join(forgeFixtures, "items.jsonl"), out)
		if err == nil || !strings.Contains(err.Error(), "inside the repository") {
			t.Errorf("export to %s: err = %v, want a repository refusal", out, err)
		}
		if _, serr := os.Stat(out); serr == nil {
			os.Remove(out)
			t.Errorf("export to %s created the file", out)
		}
	}
}

// forgeRowFor builds the Forge result row for a request: snapshots are
// gorge's own (so the rows AGREE) and mutate edits them before encoding.
func forgeRowFor(t *testing.T, reg *cards.Registry, rq forgeRequest, mutate func(*oraclediff.XResult)) []byte {
	t.Helper()
	g, err := rules.RunOracleScenarioJSON(reg, rq.Item.Raw())
	if err != nil || len(g.Fails) != 0 || len(g.Snapshots) < 2 {
		t.Fatalf("gorge run of %s: err %v fails %v snapshots %d", rq.ID, err, g.Fails, len(g.Snapshots))
	}
	x := oraclediff.XResult{Name: rq.Item.Name, ID: rq.ID, Strict: true, MS: 7}
	for _, s := range g.Snapshots {
		s.Players = append([]rules.OracleSnapPlayer(nil), s.Players...)
		x.Snapshots = append(x.Snapshots, s)
	}
	if mutate != nil {
		mutate(&x)
	}
	b, err := json.Marshal(struct {
		oraclediff.XResult
		Engine   string `json:"engine"`
		ForgeRef string `json:"forge_ref"`
	}{x, "forge", "0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeLines(t *testing.T, path string, lines ...[]byte) {
	t.Helper()
	if err := os.WriteFile(path, append(bytes.Join(lines, []byte("\n")), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readRows(t *testing.T, path string) []ForgeRow {
	t.Helper()
	var rows []ForgeRow
	for _, l := range readLinesBytes(t, path) {
		var r ForgeRow
		if err := json.Unmarshal(l, &r); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, r)
	}
	return rows
}

// TestForgeDiffVerdictsAndCache: AGREE, DIVERGE and HARNESS come out of the
// unchanged comparator; Forge's rows are cached by request sha, so a second
// run with no -forge file reproduces them, and a request whose gorge
// decisions changed (a different sha) misses the cache.
func TestForgeDiffVerdictsAndCache(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	_, reqPath := exportFixture(t, reg)
	reqLines := readLinesBytes(t, reqPath)
	reqs := decodeRequests(t, bytes.Join(reqLines, []byte("\n")))
	tmp := t.TempDir()

	agree := forgeRowFor(t, reg, reqs[0], nil)
	diverge := forgeRowFor(t, reg, reqs[1], func(x *oraclediff.XResult) {
		x.Snapshots[1].Players[0].Life++
	})
	// The third request gets no Forge row at all.
	forgePath := filepath.Join(tmp, "forge.jsonl")
	writeLines(t, forgePath, agree, diverge)
	cacheDir := filepath.Join(tmp, "cache", "ref-driver")
	out1 := filepath.Join(tmp, "diff1.jsonl")
	counts, err := forgeDiff(reg, reqPath, forgePath, cacheDir, out1)
	if err != nil {
		t.Fatal(err)
	}
	rows := readRows(t, out1)
	if len(rows) != 3 {
		t.Fatalf("%d rows, want 3", len(rows))
	}
	if rows[0].Verdict.Status != oraclediff.Agree || rows[0].ForgeMS != 7 {
		t.Errorf("row 0 = %+v, want AGREE with forge_ms 7", rows[0])
	}
	if v := rows[1].Verdict; v.Status != oraclediff.Diverge || v.Field != "p0.life" {
		t.Errorf("row 1 verdict = %+v, want DIVERGE on p0.life", v)
	}
	if v := rows[2].Verdict; v.Status != oraclediff.Harness || v.Engine != "forge" || v.Msg != "no Forge result" {
		t.Errorf("row 2 verdict = %+v, want a forge HARNESS 'no Forge result'", v)
	}
	if counts["AGREE"] != 1 || counts["DIVERGE:p0.life"] != 1 || counts["HARNESS"] != 1 {
		t.Errorf("counts = %v", counts)
	}
	for i, r := range rows {
		if r.RequestSHA != gate.Hash(reqLines[i]) || r.ScenarioSHA != reqs[i].ScenarioSHA {
			t.Errorf("row %d shas = %s/%s", i, r.RequestSHA, r.ScenarioSHA)
		}
	}
	if x, ok := (oraclediff.Cache{Dir: cacheDir}).Get(rows[0].RequestSHA); !ok || x.MS != 7 {
		t.Errorf("Forge row was not cached under the request sha: %+v %v", x, ok)
	}

	// A second pass with no Forge file replays from the cache alone.
	out2 := filepath.Join(tmp, "diff2.jsonl")
	if _, err := forgeDiff(reg, reqPath, "", cacheDir, out2); err != nil {
		t.Fatal(err)
	}
	b1, _ := os.ReadFile(out1)
	b2, _ := os.ReadFile(out2)
	if !bytes.Equal(b1, b2) {
		t.Errorf("cache-only pass differs:\n%s\n---\n%s", b1, b2)
	}

	// Change gorge's decisions on request 0: its sha moves, so the cached
	// Forge answers must not be reused.
	var rq0 forgeRequest
	if err := json.Unmarshal(reqLines[0], &rq0); err != nil {
		t.Fatal(err)
	}
	rq0.GorgeDecisions[0].PickIdx = []int{rq0.GorgeDecisions[0].PickIdx[0] + 1}
	changed, _ := json.Marshal(rq0)
	if gate.Hash(changed) == gate.Hash(reqLines[0]) {
		t.Fatal("test bug: the edited request has the same sha")
	}
	req2 := filepath.Join(tmp, "req2.jsonl")
	writeLines(t, req2, changed)
	out3 := filepath.Join(tmp, "diff3.jsonl")
	if _, err := forgeDiff(reg, req2, "", cacheDir, out3); err != nil {
		t.Fatal(err)
	}
	if v := readRows(t, out3)[0].Verdict; v.Msg != "no Forge result" {
		t.Errorf("a request with new decisions hit the old cache entry: %+v", v)
	}
}

// TestForgeDiffForgeHarnessRow: the canned Forge harness row (a card
// missing from Forge's database) is a HARNESS verdict attributed to forge,
// never a disagreement; and an XMage row (no engine marker) is refused.
func TestForgeDiffForgeHarnessRow(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	_, reqPath := exportFixture(t, reg)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "diff.jsonl")
	if _, err := forgeDiff(reg, reqPath, filepath.Join(forgeFixtures, "harness-row.jsonl"), filepath.Join(tmp, "c"), out); err != nil {
		t.Fatal(err)
	}
	v := readRows(t, out)[0].Verdict
	if v.Status != oraclediff.Harness || v.Engine != "forge" || !strings.Contains(v.Msg, "Couldn't find a card") {
		t.Errorf("verdict = %+v, want forge HARNESS carrying Forge's message", v)
	}
	_, err := forgeDiff(reg, reqPath, filepath.Join(forgeFixtures, "xmage-row.jsonl"), filepath.Join(tmp, "c2"), filepath.Join(tmp, "diff2.jsonl"))
	if err == nil || !strings.Contains(err.Error(), "not a Forge row") {
		t.Errorf("an XMage row was accepted as Forge's: err = %v", err)
	}
}

// TestForgeDiffNeverWritesRepoOrVerdicts: -out inside the repository (above
// all compliance/verdicts) is refused before anything is created.
func TestForgeDiffNeverWritesRepoOrVerdicts(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	_, reqPath := exportFixture(t, reg)
	out := filepath.Join("..", "..", compliance.VerdictDir, "forge-diff.jsonl")
	_, err := forgeDiff(reg, reqPath, "", t.TempDir(), out)
	if err == nil || !strings.Contains(err.Error(), "inside the repository") {
		t.Fatalf("err = %v, want a repository refusal", err)
	}
	if _, serr := os.Stat(out); serr == nil {
		os.Remove(out)
		t.Fatal("forge-diff created a file under compliance/verdicts")
	}
}

// TestForgeDiffCacheDir: the cache directory is named after both pins.
func TestForgeDiffCacheDir(t *testing.T) {
	t.Setenv("FORGE_ORACLE_DIR", "/off/repo")
	got, err := forgeCacheDir("", "0123456789abcdef0123", "fedcba9876543210ffff")
	if err != nil || got != "/off/repo/cache/0123456789ab-fedcba987654" {
		t.Errorf("cache dir = %q, %v", got, err)
	}
	if _, err := forgeCacheDir("", "ref", ""); err == nil {
		t.Error("a missing driver sha was accepted")
	}
	if got, _ := forgeCacheDir("/x", "", ""); got != "/x" {
		t.Errorf("explicit -cache = %q", got)
	}
}
