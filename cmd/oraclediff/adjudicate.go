package main

// The Forge oracle's three-way adjudication (DESIGN sections 8-9, plan
// P3-2 and P3-3).
//
//	oraclediff adjudicate -scenarios scen.jsonl -forge-diff forge-diff.jsonl
//	    -xmage-cache DIR -oracle-ref REF [-driver SHA | -forge-cache DIR]
//	    [-verdicts compliance/verdicts] [-d1 C|B] [-ledger DIR]
//	    [-report PATH] [-mark-review]
//
// adjudicate joins three things, by the scenario sha (gate.ItemSHA) and the
// Forge request sha:
//
//   - the committed verdict rows, read from -verdicts, are the V(G,X) leg.
//     Their status is never changed: Forge never writes a verdict
//     (DESIGN section 8.2, operator decision D4);
//   - the run's forge-diff rows (-forge-diff) are the V(G,F) leg, already
//     computed by forge-diff, and carry the request sha that keys the
//     Forge cache;
//   - the two caches give Forge's and XMage's snapshots, from which the
//     V(F,X) leg is computed with the unchanged comparator (CompareOpts
//     over ForgeOracleResult, DESIGN section 7.1).
//
// compliance/oraclediff.ThreeWay then classifies each scenario with one of
// the DESIGN section 8.1 patterns. Outputs:
//
//   - the compact ledger (operator decision D1): one row per classified
//     scenario, {card, template, scenario_sha, forge_ref, pattern,
//     gf_field, fx_field}, sharded <a-z>.jsonl. Under D1=C (the default)
//     it is written under compliance/adjudication/; under D1=B, in the
//     run directory. Field names are gorge's comparator vocabulary; no
//     Forge-produced value, message or script text is ever included.
//   - the per-row report: pattern, the first-difference field of each pair,
//     and the suggested "oraclediff rule ..." command with its
//     "forge <pattern> @<ref12>" corroboration suffix (section 8.2). It
//     goes next to forge-diff.jsonl in the run directory, never inside the
//     repository: the run's Forge values stay off-repo under both options.
//   - with -mark-review (and D1=C): the DESIGN section 8.2 contradiction
//     rows get Review: pending through compliance.ReplaceVerdicts -- an
//     xmage_wrong row whose pattern is FX|G (Forge sides with XMage
//     against the ruling), and a gorge_wrong row whose pattern is GF|X
//     (possibly mis-ruled, or a shared script bug). Only the Review field
//     is written; a status, ruling or frozen expectation is never touched,
//     hand or automatic. The agree + GX|F contradiction is recorded only
//     (section 8.2), and under D1=B every contradiction is reported only.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/gate"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/shape"
)

// adjudicationDir is the D1=C ledger directory (DESIGN section 9.1, option
// C), relative to the repo root. The Forge licence boundary test allows
// exactly this path for the ledger shards and nothing else.
const adjudicationDir = "compliance/adjudication"

// ledgerRow is one line of the D1=C compact ledger (DESIGN section 9.1,
// option C). ForgeRef is FORGE_ORACLE_REF[:12], the same pin the cache
// directory name and the @<ref12> ruling suffix carry.
type ledgerRow struct {
	Card        string `json:"card"`
	Template    string `json:"template"`
	ScenarioSHA string `json:"scenario_sha"`
	ForgeRef    string `json:"forge_ref"`
	Pattern     string `json:"pattern"`
	// GFField and FXField are the first-difference field of the G/F and F/X
	// pairs, in gorge's comparator vocabulary; empty when that pair agreed
	// (or was not compared).
	GFField string `json:"gf_field,omitempty"`
	FXField string `json:"fx_field,omitempty"`
}

// reportRow is one line of the per-row report. It names fields, patterns
// and commands only -- never an engine value -- so it is safe wherever the
// run directory is.
type reportRow struct {
	Card        string `json:"card"`
	Template    string `json:"template"`
	ID          string `json:"id"`
	ScenarioSHA string `json:"scenario_sha"`
	// Status is the committed verdict status, the thing a contradiction is
	// judged against.
	Status string `json:"status,omitempty"`
	// Pattern is the section 8.1 pattern; empty when the row could not be
	// classified (Note says why).
	Pattern string `json:"pattern,omitempty"`
	// GXField, GFField and FXField are the first-difference field of each
	// pair; empty when that pair agreed or was not compared.
	GXField string `json:"gx_field,omitempty"`
	GFField string `json:"gf_field,omitempty"`
	FXField string `json:"fx_field,omitempty"`
	// Evidence is the pattern's section 8.1 "evidence that closes it" text.
	Evidence string `json:"evidence,omitempty"`
	// Rule is the suggested oraclediff rule command, when the pattern's
	// section 8.1 action is a row ruling this flow can still take.
	Rule string `json:"rule,omitempty"`
	// Contradiction names a section 8.2 contradiction, if any.
	Contradiction string `json:"contradiction,omitempty"`
	// Note is a skip reason or remark.
	Note string `json:"note,omitempty"`
}

func runAdjudicate(args []string) error {
	fs := flag.NewFlagSet("adjudicate", flag.ExitOnError)
	scen := fs.String("scenarios", "", "scenario JSONL (from gen)")
	verdicts := fs.String("verdicts", compliance.VerdictDir, "committed verdict directory (the V(G,X) leg; never written without -mark-review)")
	fdPath := fs.String("forge-diff", "", "forge-diff JSONL (the V(G,F) leg; also names the run directory)")
	xcache := fs.String("xmage-cache", "", "XMage result cache directory for this XMAGE_REF and driver (keyed by scenario sha)")
	fcache := fs.String("forge-cache", "", "Forge result cache directory (default: $FORGE_ORACLE_DIR/cache/<oracle-ref12>-<driver12>)")
	ref := fs.String("oracle-ref", "", "FORGE_ORACLE_REF of the Forge pass (required: names the Forge cache, the ledger rows and the @ref12 suffix)")
	driver := fs.String("driver", "", "driver source sha (names the Forge cache directory)")
	d1 := fs.String("d1", "C", "operator decision D1: C (ledger under "+adjudicationDir+") or B (ledger in the run directory)")
	ledger := fs.String("ledger", adjudicationDir, "D1=C ledger directory")
	report := fs.String("report", "", "per-row report JSONL (default: adjudicate-report.jsonl next to -forge-diff, in the run directory)")
	mark := fs.Bool("mark-review", false, "D1=C: set Review: pending on the section 8.2 contradiction rows through compliance.ReplaceVerdicts")
	fs.Parse(args)
	if *scen == "" || *fdPath == "" || *xcache == "" || *ref == "" {
		return fmt.Errorf("adjudicate needs -scenarios, -forge-diff, -xmage-cache and -oracle-ref")
	}
	if *d1 != "C" && *d1 != "B" {
		return fmt.Errorf("adjudicate -d1 wants C or B, got %q", *d1)
	}
	if *fcache == "" && *driver == "" {
		return fmt.Errorf("adjudicate needs -forge-cache, or -driver with -oracle-ref")
	}
	forgeCache, err := forgeCacheDir(*fcache, *ref, *driver)
	if err != nil {
		return err
	}
	runDir := filepath.Dir(*fdPath)
	reportPath := *report
	if reportPath == "" {
		reportPath = filepath.Join(runDir, "adjudicate-report.jsonl")
	}
	ledgerDir := *ledger
	if *d1 == "B" {
		ledgerDir = runDir
	}
	frows, dup, err := readForgeRows(*fdPath)
	if err != nil {
		return err
	}
	if dup > 0 {
		fmt.Printf("forge-diff: %d duplicate scenario rows (first kept)\n", dup)
	}
	res, err := adjudicateRun(*scen, *verdicts, frows,
		oraclediff.Cache{Dir: *xcache}, oraclediff.Cache{Dir: forgeCache},
		short12(*ref))
	if err != nil {
		return err
	}
	if err := writeLedger(ledgerDir, res.ledger); err != nil {
		return err
	}
	if err := writeReport(reportPath, res.report); err != nil {
		return err
	}
	printCounts("patterns", res.patterns)
	printCounts("skip", res.skips)
	if len(res.contradictions) > 0 {
		printCounts("contradiction", res.contradictions)
	}
	if *mark && len(res.review) > 0 {
		if *d1 == "C" {
			if err := compliance.ReplaceVerdicts(*verdicts, res.review); err != nil {
				return err
			}
			fmt.Printf("marked %d contradiction rows Review: pending in %s\n", len(res.review), *verdicts)
		} else {
			fmt.Printf("D1=B: %d contradiction rows reported only; verdicts untouched\n", len(res.review))
		}
	}
	fmt.Printf("ledger: %d rows in %s (D1=%s)\n", len(res.ledger), ledgerDir, *d1)
	fmt.Printf("report: %s\n", reportPath)
	return nil
}

// adjudicateResult is everything one adjudicate pass produced.
type adjudicateResult struct {
	ledger         []ledgerRow
	report         []reportRow
	patterns       map[string]int
	skips          map[string]int
	contradictions map[string]int
	// review holds the verdict rows -mark-review would rewrite.
	review []compliance.VerdictRow
}

// adjudicateRun classifies every scenario line. frows are the run's
// forge-diff rows keyed by scenario sha; the caches answer the V(F,X) leg.
func adjudicateRun(scenPath, verdictDir string, frows map[string]ForgeRow,
	xcache, forgeCache oraclediff.Cache, ref12 string) (adjudicateResult, error) {
	all, err := compliance.LoadVerdicts(verdictDir)
	if err != nil {
		return adjudicateResult{}, err
	}
	res := adjudicateResult{
		patterns:       map[string]int{},
		skips:          map[string]int{},
		contradictions: map[string]int{},
	}
	err = readLines(scenPath, func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		sha := gate.ItemSHA(it)
		rep := reportRow{Card: it.Card, Template: it.Template, ID: it.ID, ScenarioSHA: sha}
		skip := func(note string) {
			rep.Note = note
			res.report = append(res.report, rep)
			res.skips[noteBucket(note)]++
		}
		vr, ok := all[it.Card][it.Template]
		if !ok {
			skip("no committed verdict row")
			return nil
		}
		rep.Status = vr.Status
		if vr.ScenarioSHA != sha {
			skip("verdict is for an older scenario (scenario_sha " + short12(vr.ScenarioSHA) + ")")
			return nil
		}
		fr, ok := frows[sha]
		if !ok {
			skip("no forge-diff row for this scenario")
			return nil
		}
		gf := fr.Verdict
		gx := committedGX(vr)
		forgeHarness := gf.Status == oraclediff.Harness && gf.Engine != "gorge"
		// The V(F,X) leg needs both engines' snapshots, which only exist
		// when the G/X leg was compared and the Forge row carried a result
		// (DESIGN section 8.1: a harness or lacks row on either side gets
		// its own pattern without it).
		fx := oraclediff.Verdict{}
		fxMissing := ""
		if !forgeHarness && gf.Status != oraclediff.Harness &&
			(gx.Status == oraclediff.Agree || gx.Status == oraclediff.Diverge) {
			fsnap, ok := forgeCache.Get(fr.RequestSHA)
			if !ok {
				fxMissing = "no cached Forge result for the F/X leg (request_sha " + short12(fr.RequestSHA) + ")"
			} else if xsnap, ok := xcache.Get(sha); !ok {
				fxMissing = "no cached XMage result for the F/X leg"
			} else {
				fx = oraclediff.CompareOpts(oraclediff.ForgeOracleResult(fsnap), nil, xsnap, it.Compare, it.Ignore...)
			}
		}
		tw := oraclediff.ThreeWay(gx, gf, fx, forgeHarness, false)
		if fxMissing != "" {
			skip(fxMissing)
			return nil
		}
		if tw.Pattern == oraclediff.Inconsistent {
			skip("the three pairwise verdicts match no section 8.1 row (gorge's own leg: " +
				string(gx.Status) + " " + gx.Engine + ")")
			return nil
		}
		rep.Pattern = string(tw.Pattern)
		rep.Evidence = tw.Evidence
		if gx.Status == oraclediff.Diverge {
			rep.GXField = gx.Field
		}
		if gf.Status == oraclediff.Diverge {
			rep.GFField = gf.Field
		}
		if fx.Status == oraclediff.Diverge {
			rep.FXField = fx.Field
		}
		contradict, mark, corroborated := adjudicateRead(&rep, vr, tw.Pattern, ref12)
		if corroborated {
			rep.Note = "Forge corroborates the ruling"
		}
		res.patterns[rep.Pattern]++
		if contradict != "" {
			res.contradictions[contradict]++
			rep.Contradiction = contradict
			if mark {
				res.review = append(res.review, pendingReview(vr))
			}
		}
		res.report = append(res.report, rep)
		res.ledger = append(res.ledger, ledgerRow{
			Card: it.Card, Template: it.Template, ScenarioSHA: sha,
			ForgeRef: ref12, Pattern: rep.Pattern,
			GFField: rep.GFField, FXField: rep.FXField,
		})
		return nil
	})
	return res, err
}

// adjudicateRead fills the report's suggested rule command and returns the
// row's section 8.2 contradiction, if any, and whether -mark-review
// reopens it. The suggested command is the section 8.1 action only where it
// would still change something: a GF|X on an already-ruled xmage_wrong row
// is corroboration, and an FX|G on an already-ruled gorge_wrong row
// likewise. The agree + GX|F contradiction is listed but never marked
// (recorded only, low priority).
func adjudicateRead(rep *reportRow, vr compliance.VerdictRow, p oraclediff.Pattern, ref12 string) (contradict string, mark, corroborated bool) {
	switch p {
	case oraclediff.GorgeForgeX:
		switch vr.Status {
		case compliance.StatusDiverge:
			rep.Rule = ruleCmd(p, rep.Card, rep.Template, compliance.StatusXMageWrong, ref12)
		case compliance.StatusGorgeWrong:
			contradict, mark = "gorge_wrong contradicted by GF|X", true
		case compliance.StatusXMageWrong:
			corroborated = true
		}
	case oraclediff.ForgeXMageG:
		switch vr.Status {
		case compliance.StatusDiverge:
			rep.Rule = ruleCmd(p, rep.Card, rep.Template, compliance.StatusGorgeWrong, ref12)
		case compliance.StatusXMageWrong:
			contradict, mark = "xmage_wrong contradicted by FX|G", true
		case compliance.StatusGorgeWrong:
			corroborated = true
		}
	case oraclediff.GorgeXMageF:
		if vr.Status == compliance.StatusAgree {
			// Recorded only, low priority (DESIGN section 8.2): listed,
			// never marked for review.
			contradict = "agree contradicted by GX|F (recorded only)"
		}
	}
	return contradict, mark, corroborated
}

// pendingReview copies a contradiction row with only its Review field set:
// the status, ruling, frozen expectation and every other field are carried
// through untouched (DESIGN section 8.2: Forge never changes a verdict
// status, and -mark-review reopens a ruling for a human check, it does not
// overturn one).
func pendingReview(vr compliance.VerdictRow) compliance.VerdictRow {
	vr.Review = shape.ReviewPending
	return vr
}

// ruleCmd is the suggested oraclediff rule command for a GF|X or FX|G row
// (DESIGN section 8.1's "Action in the existing flow"), carrying the
// "forge <pattern> @<ref12>" corroboration suffix (section 8.2). A level-B
// template (a requirement key, carrying "#") is passed on; a level-A row
// needs no -template.
func ruleCmd(p oraclediff.Pattern, card, template, status, ref12 string) string {
	tmpl := ""
	if strings.Contains(template, "#") {
		tmpl = " -template " + template
	}
	cite := "<Oracle/CR cite>"
	if p == oraclediff.ForgeXMageG {
		cite = "<Oracle cite>"
	}
	return fmt.Sprintf("oraclediff rule -card %q -status %s%s -ruling %q",
		card, status, tmpl, cite+"; forge "+string(p)+" @"+ref12)
}

// committedGX maps a committed verdict row to the V(G,X) Verdict the
// three-way core takes. A gorge_wrong or xmage_wrong row is a classified
// divergence: its Detail still names the checkpoint and field the
// classification was made on.
func committedGX(vr compliance.VerdictRow) oraclediff.Verdict {
	switch vr.Status {
	case compliance.StatusAgree:
		return oraclediff.Verdict{Status: oraclediff.Agree}
	case compliance.StatusDiverge, compliance.StatusGorgeWrong, compliance.StatusXMageWrong:
		cp, field := parseDivergenceDetail(vr.Detail)
		return oraclediff.Verdict{Status: oraclediff.Diverge, Checkpoint: cp, Field: field}
	case compliance.StatusHarness:
		// runDiff writes "<engine>: <first line of the message>".
		eng, msg, _ := strings.Cut(vr.Detail, ": ")
		return oraclediff.Verdict{Status: oraclediff.Harness, Engine: eng, Msg: msg}
	case compliance.StatusXMageLacks:
		return oraclediff.Verdict{Status: oraclediff.XMageLacks, Msg: vr.Detail}
	}
	// An unknown status matches no section 8.1 row; the report says so.
	return oraclediff.Verdict{Status: oraclediff.Harness, Engine: "gorge",
		Msg: "unknown committed status " + vr.Status}
}

// parseDivergenceDetail reads the checkpoint and field out of a diverge
// row's Detail ("step 1 (resolve) p0.life: gorge \"20\", xmage \"21\"",
// written by runDiff). The checkpoint may contain spaces; the field, the
// comparator's vocabulary, does not.
func parseDivergenceDetail(detail string) (cp, field string) {
	i := strings.Index(detail, ": gorge ")
	if i < 0 {
		return "", ""
	}
	left := strings.TrimSpace(detail[:i])
	if j := strings.LastIndexByte(left, ' '); j >= 0 {
		return left[:j], left[j+1:]
	}
	return "", left
}

// readForgeRows reads a forge-diff JSONL, keyed by scenario sha. On a
// duplicate the first row is kept, so the join is independent of read
// order.
func readForgeRows(path string) (map[string]ForgeRow, int, error) {
	out := map[string]ForgeRow{}
	dup := 0
	err := readLines(path, func(b []byte) error {
		var r ForgeRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		if _, ok := out[r.ScenarioSHA]; ok {
			dup++
			return nil
		}
		out[r.ScenarioSHA] = r
		return nil
	})
	return out, dup, err
}

func writeReport(path string, rows []reportRow) error {
	return writeJSONL(path, len(rows), func(i int) (any, error) { return rows[i], nil })
}

func writeLedger(dir string, rows []ledgerRow) error {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Card != rows[j].Card {
			return rows[i].Card < rows[j].Card
		}
		return rows[i].Template < rows[j].Template
	})
	shards := map[string][]ledgerRow{}
	for _, r := range rows {
		s := compliance.Shard(r.Card)
		shards[s] = append(shards[s], r)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(shards))
	for k := range shards {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, s := range keys {
		rs := shards[s]
		p := filepath.Join(dir, s+".jsonl")
		if err := writeJSONL(p, len(rs), func(i int) (any, error) { return rs[i], nil }); err != nil {
			return err
		}
	}
	return nil
}

// writeJSONL writes n lines through tmp+rename, so a reader never sees a
// half-written file.
func writeJSONL(path string, n int, line func(int) (any, error)) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for i := 0; i < n; i++ {
		v, err := line(i)
		if err != nil {
			return err
		}
		if err := enc.Encode(v); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadLedger reads back a ledger directory: only the one-letter shard
// files, never the report or anything else sharing the directory.
func loadLedger(dir string) ([]ledgerRow, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "[a-z0-9].jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []ledgerRow
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			if len(bytes.TrimSpace(sc.Bytes())) == 0 {
				continue
			}
			var r ledgerRow
			if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
				f.Close()
				return nil, fmt.Errorf("%s: %v", p, err)
			}
			out = append(out, r)
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// noteBucket folds a skip note to its reason bucket for the census: the
// sentence up to its first parenthesised detail.
func noteBucket(note string) string {
	if i := strings.IndexByte(note, '('); i > 0 {
		return strings.TrimSpace(note[:i])
	}
	return note
}

func printCounts(label string, counts map[string]int) {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%-14s %6d  %s\n", label, counts[k], k)
	}
}
