// Command oraclediff runs the XMage compliance pipeline's gorge side.
//
//	oraclediff gen  [-cards .cards] [-level A|B] -manifest compliance/manifests/FRA.json -out scenarios.jsonl
//	oraclediff plan [-cards .cards] -scenarios scenarios.jsonl -xmage-ref REF -cache DIR -replay todo.jsonl -rediff rediff.jsonl
//	oraclediff diff [-cards .cards] -scenarios scenarios.jsonl [-xmage xmage.jsonl] [-cache DIR] -out verdicts.jsonl
//	oraclediff show [-cards .cards] -scenarios scenarios.jsonl -card NAME
//	oraclediff triage [-cards .cards] [-apply]
//	oraclediff refreeze [-cards .cards] [-apply]
//	oraclediff impact [-cards .cards] [-top N] [-json]
//	oraclediff status [-cards .cards] -set S | -all [-out status.md] [-write-ratchet] [-json]
//	oraclediff tickets [-cards .cards] [-out DIR] [-min-cards 10] [-any-in FORMAT] [-json]
//
// gen writes one level-A scenario per manifest card gorge fully supports
// (skips go to <out>.skips.jsonl); -level B writes the level-A scenario and
// then every generable level-B one, with a level-B skip (keyed by its
// requirement) for each the generator cannot make yet. plan splits them into the stale ones
// (no passing verdict for this exact scenario and XMAGE_REF) and, of those,
// the ones XMage has not replayed yet. The XMage driver (scripts/xmage-
// oracle-run.sh) replays the latter; diff runs gorge on each scenario,
// compares the two engines' snapshots and writes one verdict per scenario.
// scripts/compliance-pass.sh (make compliance-pass) runs the whole pass.
//
// Disagreements are triaged by shape (compliance/shape): diff classifies a
// new disagreement that a compliance/rulings/<id>.json ruling matches, and
// triage re-applies the rulings to every committed row and writes what no
// ruling covers as one cluster per shape under compliance/triage/.
//
// A passing row freezes only the fields its scenario changed (Frozen), not
// a hash of the whole snapshot; refreeze converts legacy canon_sha rows.
//
// impact is the primitive impact table (compliance/adopt): every
// unsupported primitive, the tournament cards it blocks in each target
// format of compliance/formats.json and the sets it would unlock. tickets
// turns the findings into one would-be agentctl ticket per class -- a
// primitive (with the primitives only its cards carry), a gorge_wrong
// ruling or shape, an untriaged shape cluster -- each with its cards, sets
// and a class-census ratchet skeleton; it writes them, it never files them.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/adopt"
	"github.com/adams-shaun/gorge/compliance/gate"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/compliance/shape"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "gen":
		fs := flag.NewFlagSet("gen", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus dir")
		manifest := fs.String("manifest", "", "set manifest")
		out := fs.String("out", "", "scenario JSONL")
		level := fs.String("level", "A", "A (one level-A scenario per card) or B (the level-A scenario plus every generable level-B one)")
		fs.Parse(os.Args[2:])
		err = runGen(*dir, *manifest, *out, *level)
	case "plan":
		fs := flag.NewFlagSet("plan", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus dir")
		scen := fs.String("scenarios", "", "scenario JSONL (from gen)")
		verdicts := fs.String("verdicts", compliance.VerdictDir, "committed verdict directory")
		ref := fs.String("xmage-ref", "", "XMAGE_REF of this pass")
		cache := fs.String("cache", "", "XMage result cache directory for this XMAGE_REF and driver")
		replay := fs.String("replay", "", "write the scenarios XMage must replay here")
		rediff := fs.String("rediff", "", "write every stale scenario (replayed or cached) here")
		fs.Parse(os.Args[2:])
		err = runPlan(*dir, *scen, *verdicts, *ref, *cache, *replay, *rediff)
	case "diff":
		fs := flag.NewFlagSet("diff", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus dir")
		scen := fs.String("scenarios", "", "scenario JSONL")
		xm := fs.String("xmage", "", "XMage driver output JSONL")
		cache := fs.String("cache", "", "XMage result cache: results missing from -xmage are read from it, and -xmage's are stored in it")
		out := fs.String("out", "", "verdict JSONL")
		write := fs.String("write", "", "merge verdict rows into this directory (compliance/verdicts)")
		ref := fs.String("xmage-ref", "", "XMAGE_REF the XMage results came from (required with -write)")
		rulings := fs.String("rulings", shape.RulingDir, "shape rulings that classify a disagreement automatically")
		fs.Parse(os.Args[2:])
		err = runDiff(*dir, *scen, *xm, *cache, *out, *write, *ref, *rulings)
	case "triage":
		fs := flag.NewFlagSet("triage", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus dir")
		verdicts := fs.String("verdicts", compliance.VerdictDir, "verdict directory")
		rulings := fs.String("rulings", shape.RulingDir, "shape rulings")
		out := fs.String("out", shape.TriageDir, "cluster directory (rewritten with -apply)")
		apply := fs.Bool("apply", false, "write the classifications and the clusters")
		fs.Parse(os.Args[2:])
		err = runTriage(*dir, *verdicts, *rulings, *out, *apply)
	case "status":
		fs := flag.NewFlagSet("status", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus dir")
		set := fs.String("set", "", "set code")
		level := fs.String("level", "A", "level")
		all := fs.Bool("all", false, "every committed set: format roll-ups and reason buckets")
		out := fs.String("out", "", "with -all: write the generated dashboard here (never committed)")
		ratchet := fs.Bool("write-ratchet", false, "with -all: record "+adopt.RatchetFile+" as the sets stand")
		asJSON := fs.Bool("json", false, "with -all: one JSON line per set")
		sets := fs.String("sets", "", "with -all: only these comma-separated set codes")
		memprof := fs.String("memprofile", "", "with -all: write a heap profile here")
		procs := fs.Int("procs", 2, "with -all: gate child processes at a time (at most 4)")
		child := fs.Bool("child", false, "with -all -sets: run the gate in this process (a status child)")
		fs.Parse(os.Args[2:])
		if *all {
			var only []string
			if *sets != "" {
				only = strings.Split(*sets, ",")
			}
			err = runStatusAll(*dir, *level, *out, *ratchet, *asJSON, only, *memprof, *procs, *child)
			break
		}
		err = runStatus(*dir, *set, *level)
	case "rule":
		fs := flag.NewFlagSet("rule", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus dir")
		card := fs.String("card", "", "card")
		status := fs.String("status", "", "gorge_wrong or xmage_wrong (harness too with -shape-id)")
		ruling := fs.String("ruling", "", "who is wrong and why, citing the CR")
		shapeID := fs.String("shape-id", "", "write the ruling as compliance/rulings/<id>.json matching this card's disagreement shape (then run triage -apply)")
		template := fs.String("template", "", "verdict row's template (default: the card's level-A template; a level-B requirement key selects that row)")
		confirm := fs.Bool("confirm", false, "confirm the card's automatic classification sampled for review")
		fs.Parse(os.Args[2:])
		err = runRule(*dir, *card, *status, *ruling, *shapeID, *template, *confirm)
	case "refreeze":
		fs := flag.NewFlagSet("refreeze", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus dir")
		apply := fs.Bool("apply", false, "write the converted rows")
		fs.Parse(os.Args[2:])
		err = runRefreeze(*dir, *apply)
	case "impact":
		fs := flag.NewFlagSet("impact", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus dir")
		top := fs.Int("top", 0, "tournament rows to print (0 = all)")
		asJSON := fs.Bool("json", false, "one JSON row per primitive")
		fs.Parse(os.Args[2:])
		err = runImpact(*dir, *top, *asJSON)
	case "tickets":
		fs := flag.NewFlagSet("tickets", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus dir")
		out := fs.String("out", "", "write <id>.md briefs, index.jsonl and file.sh here (nothing is filed)")
		minCards := fs.Int("min-cards", 10, "a primitive needs this many blocked tournament cards for a ticket...")
		anyIn := fs.String("any-in", "", "...or any blocked card in this format (default: the first target in compliance/formats.json)")
		asJSON := fs.Bool("json", false, "one JSON row per ticket")
		fs.Parse(os.Args[2:])
		err = runTickets(*dir, *out, *minCards, *anyIn, *asJSON)
	case "show":
		fs := flag.NewFlagSet("show", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus dir")
		scen := fs.String("scenarios", "", "scenario JSONL")
		card := fs.String("card", "", "card whose scenarios to replay in gorge")
		fs.Parse(os.Args[2:])
		err = runShow(*dir, *scen, *card)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "oraclediff:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: oraclediff gen|plan|diff|status|rule|triage|refreeze|impact|tickets|show ...")
	os.Exit(2)
}

func loadReg(dir string) (*cards.Registry, error) {
	if r, err := cards.LoadRegistry(cards.CachePath(dir)); err == nil {
		return r, nil
	}
	return cards.OpenCorpus(dir)
}

func runGen(dir, manifest, out, level string) error {
	if manifest == "" || out == "" {
		return fmt.Errorf("gen needs -manifest and -out")
	}
	if level != "A" && level != "B" {
		return fmt.Errorf("gen -level wants A or B, got %q", level)
	}
	raw, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	var m compliance.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	sup := effects.Supported()
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)
	unfinished := map[string]bool{}
	for _, u := range m.Unfinished {
		unfinished[compliance.FoldName(u)] = true
	}
	of, err := os.Create(out)
	if err != nil {
		return err
	}
	defer of.Close()
	sf, err := os.Create(out + ".skips.jsonl")
	if err != nil {
		return err
	}
	defer sf.Close()
	ow, sw := bufio.NewWriter(of), bufio.NewWriter(sf)
	defer ow.Flush()
	defer sw.Flush()
	n, skipped := 0, 0
	for _, mc := range m.Cards {
		if unfinished[compliance.FoldName(mc.Name)] {
			b, _ := json.Marshal(&oraclegen.Skip{Card: mc.Name, Reason: "XMage does not implement it (the set marks it unfinished)"})
			fmt.Fprintln(sw, string(b))
			skipped++
			continue
		}
		name, ok := compliance.CorpusNameFold(has, folded, mc.Name)
		// Registry lookup accepts punctuation aliases as well as canonical
		// names. Scenarios must use the face's corpus spelling: otherwise
		// the fixture itself can fail to find/cast the card (e.g. XMage's
		// "With Great Power..." alias for Forge's "With Great Power . . .").
		if ok {
			if c, found := reg.Lookup(name); found && len(c.Faces) > 0 {
				name = c.Faces[0].Name
			}
		}
		var it oraclegen.Item
		var skip *oraclegen.Skip
		c, _ := reg.Lookup(name)
		switch {
		case !ok:
			skip = &oraclegen.Skip{Card: mc.Name, Reason: "not in corpus"}
		default:
			if u := reg.Unsupported(c, sup); len(u) > 0 {
				skip = &oraclegen.Skip{Card: name, Reason: fmt.Sprintf("unsupported %v", u)}
			} else {
				it, skip = templates.Generate(reg, name)
			}
		}
		if skip != nil {
			b, _ := json.Marshal(skip)
			fmt.Fprintln(sw, string(b))
			skipped++
			continue
		}
		if name != mc.Name {
			it.XMageName = mc.Name
		}
		b, _ := json.Marshal(it)
		fmt.Fprintln(ow, string(b))
		n++
		if level != "B" {
			continue
		}
		// Level B is a superset of A: a card that got a level-A item also
		// gets one scenario per level-B requirement. A requirement the
		// level-A scenario already settles adds nothing (the gate skips it
		// too), so gen writes no scenario for it. A requirement no template
		// serves yet is a B skip that names its key.
		for _, req := range levelb.Requirements(c) {
			if req.CoveredByA {
				continue
			}
			bit, bskip := templates.GenerateB(reg, name, req)
			if bskip != nil {
				sb, _ := json.Marshal(&oraclegen.Skip{Card: name, Reason: req.Key + ": " + bskip.Reason})
				fmt.Fprintln(sw, string(sb))
				skipped++
				continue
			}
			bb, _ := json.Marshal(bit)
			fmt.Fprintln(ow, string(bb))
			n++
		}
	}
	fmt.Printf("gen %s: %d scenarios, %d skipped (%s.skips.jsonl)\n", m.Code, n, skipped, out)
	return nil
}

// Row is one verdict line.
type Row struct {
	ID       string             `json:"id"`
	Card     string             `json:"card"`
	Template string             `json:"template"`
	Verdict  oraclediff.Verdict `json:"verdict"`
	XMageMS  int                `json:"xmage_ms"`
}

func readLines(path string, each func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		if err := each(append([]byte(nil), sc.Bytes()...)); err != nil {
			return err
		}
	}
	return sc.Err()
}

func runDiff(dir, scen, xm, cacheDir, out, write, ref, rulingDir string) error {
	if scen == "" || (xm == "" && cacheDir == "") || out == "" {
		return fmt.Errorf("diff needs -scenarios, -xmage or -cache, and -out")
	}
	cache := oraclediff.Cache{Dir: cacheDir}
	if write != "" && ref == "" {
		return fmt.Errorf("-write needs -xmage-ref")
	}
	var rows []compliance.VerdictRow
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	rs, err := shape.LoadRulings(rulingDir)
	if err != nil {
		return err
	}
	auto := map[string]int{}
	xres := map[string]oraclediff.XResult{}
	if xm != "" {
		if err := readLines(xm, func(b []byte) error {
			var x oraclediff.XResult
			if err := json.Unmarshal(b, &x); err != nil {
				return err
			}
			xres[x.ID] = x
			return nil
		}); err != nil {
			return err
		}
	}
	of, err := os.Create(out)
	if err != nil {
		return err
	}
	defer of.Close()
	w := bufio.NewWriter(of)
	defer w.Flush()
	counts := map[string]int{}
	if err := readLines(scen, func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		row := Row{ID: it.ID, Card: it.Card, Template: it.Template}
		sha := gate.ItemSHA(it)
		x, ok := xres[it.ID]
		if ok && cacheDir != "" {
			if err := cache.Put(sha, x); err != nil {
				return err
			}
		} else if !ok {
			x, ok = cache.Get(sha)
		}
		if !ok {
			row.Verdict = oraclediff.Verdict{Status: oraclediff.Harness, Engine: "xmage", Msg: "no XMage result"}
		} else {
			g, gerr := rules.RunOracleScenarioJSON(reg, it.Raw())
			row.Verdict = oraclediff.Compare(g, gerr, x, it.Ignore...)
			row.XMageMS = x.MS
			vr := compliance.VerdictRow{Card: it.Card, Template: it.Template, ID: it.ID,
				ScenarioSHA: sha, XMageRef: ref}
			switch row.Verdict.Status {
			case oraclediff.Agree:
				vr.Status = compliance.StatusAgree
				vr.Frozen = oraclediff.Freeze(g, it.Ignore...)
				if x.StrictMiss != "" {
					vr.Detail = "xmage chose unscripted (gorge posed no such decision): " + firstLine(x.StrictMiss)
				} else if x.Leftover != "" {
					vr.Detail = "xmage did not pose a decision gorge did: " + firstLine(x.Leftover)
				}
			case oraclediff.Diverge:
				vr.Status = compliance.StatusDiverge
				vr.Detail = fmt.Sprintf("%s %s: gorge %q, xmage %q", row.Verdict.Checkpoint, row.Verdict.Field, row.Verdict.Gorge, row.Verdict.XMage)
			default:
				if row.Verdict.Engine == "xmage" && oraclediff.XMageLacksCard(row.Verdict.Msg, it.Card) {
					// XMage's card database does not hold the card (an
					// unfinished set): not a driver gap, and not a card the
					// gate should require a verdict for.
					row.Verdict.Status = oraclediff.XMageLacks
					vr.Status = compliance.StatusXMageLacks
					vr.Detail = "xmage: " + firstLine(row.Verdict.Msg)
				} else {
					vr.Status = compliance.StatusHarness
					vr.Detail = row.Verdict.Engine + ": " + firstLine(row.Verdict.Msg)
				}
			}
			if r, _, _ := shape.Classify(&vr, rs, cardAPI(reg, it.Card)); r != nil {
				auto[r.ID]++
				if vr.Status == compliance.StatusXMageWrong {
					vr.Frozen = oraclediff.Freeze(g, it.Ignore...)
				}
			}
			rows = append(rows, vr)
		}
		key := string(row.Verdict.Status)
		if row.Verdict.Status == oraclediff.Diverge {
			key += ":" + row.Verdict.Field
		}
		counts[key]++
		rb, _ := json.Marshal(row)
		fmt.Fprintln(w, string(rb))
		return nil
	}); err != nil {
		return err
	}
	if write != "" {
		if err := compliance.MergeVerdicts(write, rows); err != nil {
			return err
		}
		fmt.Printf("merged %d verdict rows into %s\n", len(rows), write)
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%-28s %d\n", k, counts[k])
	}
	ids := make([]string, 0, len(auto))
	for id := range auto {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Printf("auto-classified by %s: %d\n", id, auto[id])
	}
	return nil
}

// runPlan reads gen's scenarios and writes the stale ones (section 11.3
// C1): -rediff gets every scenario whose verdict must be recomputed,
// -replay the subset XMage has no cached result for.
func runPlan(dir, scen, verdictDir, ref, cacheDir, replay, rediff string) error {
	if scen == "" || ref == "" || replay == "" || rediff == "" {
		return fmt.Errorf("plan needs -scenarios, -xmage-ref, -replay and -rediff")
	}
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	all, err := compliance.LoadVerdicts(verdictDir)
	if err != nil {
		return err
	}
	cache := oraclediff.Cache{Dir: cacheDir}
	var outs [2]*bufio.Writer
	for i, p := range []string{replay, rediff} {
		f, err := os.Create(p)
		if err != nil {
			return err
		}
		defer f.Close()
		outs[i] = bufio.NewWriter(f)
		defer outs[i].Flush()
	}
	counts := map[gate.Need]int{}
	reasons := map[string]int{}
	if err := readLines(scen, func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		row, have := all[it.Card][it.Template]
		need, why := gate.PlanItem(reg, it, row, have, ref, cache)
		counts[need]++
		if need == gate.Fresh {
			return nil
		}
		reasons[why]++
		fmt.Fprintln(outs[1], string(b))
		if need == gate.Replay {
			fmt.Fprintln(outs[0], string(b))
		}
		return nil
	}); err != nil {
		return err
	}
	fmt.Printf("plan: %d fresh, %d rediff from cache, %d to replay in XMage\n", counts[gate.Fresh], counts[gate.Rediff], counts[gate.Replay])
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  stale: %-40s %d\n", k, reasons[k])
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// runStatus prints what keeps a set from a level (the CI gate's check).
func runStatus(dir, set, level string) error {
	if set == "" {
		return fmt.Errorf("status needs -set")
	}
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	probs, err := gate.Check(reg, ".", set, level)
	if err != nil {
		return err
	}
	total := 0
	if pr, err := compliance.LoadPrinted("compliance/printed", set); err == nil {
		total = len(pr.Cards)
	} else {
		m, _ := compliance.LoadManifest("compliance/manifests", set)
		total = len(m.Cards)
	}
	for _, p := range probs {
		fmt.Printf("%-40s %s\n", p.Card, p.Reason)
	}
	fmt.Printf("%s:%s -- %d of %d printed cards outstanding\n", set, level, len(probs), total)
	fmt.Println(gate.LevelMeaning(level))
	return nil
}

// runRule records a triage ruling on a card's generated-scenario verdict.
// xmage_wrong freezes gorge's current result as the expectation, so the gate holds gorge to the ruled behaviour.
//
// -confirm instead marks the card's automatic classification, sampled for
// review, as checked; -shape-id writes the ruling as a reusable shape
// ruling instead of on this one row.
func runRule(dir, card, status, ruling, shapeID, template string, confirm bool) error {
	if card == "" {
		return fmt.Errorf("rule needs -card")
	}
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	all, err := compliance.LoadVerdicts(compliance.VerdictDir)
	if err != nil {
		return err
	}
	if confirm {
		var rows []compliance.VerdictRow
		for _, r := range all[card] {
			if r.Review == shape.ReviewPending {
				r.Review = shape.ReviewConfirmed
				rows = append(rows, r)
			}
		}
		if len(rows) == 0 {
			return fmt.Errorf("%s: no automatic classification is pending review", card)
		}
		return compliance.ReplaceVerdicts(compliance.VerdictDir, rows)
	}
	if shapeID != "" {
		if ruling == "" || (status != compliance.StatusGorgeWrong && status != compliance.StatusXMageWrong && status != compliance.StatusHarness) {
			return fmt.Errorf("rule -shape-id needs -ruling and -status gorge_wrong|xmage_wrong|harness")
		}
		var rows []compliance.VerdictRow
		for _, r := range all[card] {
			if _, ok := shape.Of(r); ok && (r.Status == compliance.StatusDiverge || r.Status == compliance.StatusHarness) {
				rows = append(rows, r)
			}
		}
		if len(rows) != 1 {
			return fmt.Errorf("%s: %d untriaged disagreements to take a shape from, want 1", card, len(rows))
		}
		p, err := newShapeRuling(shape.RulingDir, shapeID, status, ruling, rows[0])
		if err != nil {
			return err
		}
		fmt.Printf("wrote %s; run oraclediff triage -apply to classify every row it matches\n", p)
		return nil
	}
	if ruling == "" || (status != compliance.StatusGorgeWrong && status != compliance.StatusXMageWrong) {
		return fmt.Errorf("rule needs -ruling and -status gorge_wrong|xmage_wrong")
	}
	if template == "" {
		gen, gskip := templates.Generate(reg, card)
		if gskip != nil {
			return fmt.Errorf("%s: %s", card, gskip.Reason)
		}
		template = gen.Template
	}
	it, skip := templates.ItemFor(reg, card, template)
	if skip != nil {
		return fmt.Errorf("%s: %s", card, skip.Reason)
	}
	r, ok := all[card][it.Template]
	if !ok {
		return fmt.Errorf("%s: no %s verdict to rule on", card, it.Template)
	}
	if r.ScenarioSHA != gate.ItemSHA(it) {
		return fmt.Errorf("%s: verdict is for an older scenario; re-run the XMage pass first", card)
	}
	r.Status, r.Ruling, r.CanonSHA, r.Frozen, r.RulingID, r.Review = status, ruling, "", nil, "", ""
	if status == compliance.StatusXMageWrong {
		if r.Frozen, err = gate.Freeze(reg, it); err != nil {
			return err
		}
	}
	return compliance.MergeVerdicts(compliance.VerdictDir, []compliance.VerdictRow{r})
}

// runShow replays one card's scenarios in gorge and prints the scenario,
// gorge's transcript and its last snapshot, for triage.
func runShow(dir, scen, card string) error {
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	return readLines(scen, func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		if it.Card != card {
			return nil
		}
		fmt.Printf("== %s\n%s\n", it.ID, it.Raw())
		g, gerr := rules.RunOracleScenarioJSON(reg, it.Raw())
		if gerr != nil {
			return gerr
		}
		for _, l := range g.Transcript {
			fmt.Println("  ", l)
		}
		for _, f := range g.Fails {
			fmt.Println("  FAIL", f)
		}
		for _, d := range g.Decisions {
			db, _ := json.Marshal(d)
			fmt.Println("  DECISION", string(db))
		}
		if n := len(g.Snapshots); n > 0 {
			sb, _ := json.MarshalIndent(g.Snapshots[n-1], "  ", " ")
			fmt.Printf("  last snapshot: %s\n", sb)
		}
		return nil
	})
}
