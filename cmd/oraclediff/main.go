// Command oraclediff runs the XMage compliance pipeline's gorge side.
//
//	oraclediff gen  [-cards .cards] -manifest compliance/manifests/FRA.json -out scenarios.jsonl
//	oraclediff diff [-cards .cards] -scenarios scenarios.jsonl -xmage xmage.jsonl -out verdicts.jsonl
//	oraclediff show [-cards .cards] -scenarios scenarios.jsonl -card NAME
//
// gen writes one level-A scenario per manifest card gorge fully supports
// (skips go to <out>.skips.jsonl). The XMage driver (scripts/xmage-oracle-
// run.sh) replays the same file; diff runs gorge on each scenario, compares
// the two engines' snapshots and writes one verdict per scenario.
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
	"github.com/adams-shaun/gorge/compliance/gate"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
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
		fs.Parse(os.Args[2:])
		err = runGen(*dir, *manifest, *out)
	case "diff":
		fs := flag.NewFlagSet("diff", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus dir")
		scen := fs.String("scenarios", "", "scenario JSONL")
		xm := fs.String("xmage", "", "XMage driver output JSONL")
		out := fs.String("out", "", "verdict JSONL")
		write := fs.String("write", "", "merge verdict rows into this directory (compliance/verdicts)")
		ref := fs.String("xmage-ref", "", "XMAGE_REF the XMage results came from (required with -write)")
		fs.Parse(os.Args[2:])
		err = runDiff(*dir, *scen, *xm, *out, *write, *ref)
	case "status":
		fs := flag.NewFlagSet("status", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus dir")
		set := fs.String("set", "", "set code")
		level := fs.String("level", "A", "level")
		fs.Parse(os.Args[2:])
		err = runStatus(*dir, *set, *level)
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
	fmt.Fprintln(os.Stderr, "usage: oraclediff gen|diff ...")
	os.Exit(2)
}

func loadReg(dir string) (*cards.Registry, error) {
	if r, err := cards.LoadRegistry(cards.CachePath(dir)); err == nil {
		return r, nil
	}
	return cards.OpenCorpus(dir)
}

func runGen(dir, manifest, out string) error {
	if manifest == "" || out == "" {
		return fmt.Errorf("gen needs -manifest and -out")
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
		name, ok := compliance.CorpusName(has, mc.Name)
		var it oraclegen.Item
		var skip *oraclegen.Skip
		switch {
		case !ok:
			skip = &oraclegen.Skip{Card: mc.Name, Reason: "not in corpus"}
		default:
			c, _ := reg.Lookup(name)
			if u := reg.Unsupported(c, sup); len(u) > 0 {
				skip = &oraclegen.Skip{Card: name, Reason: fmt.Sprintf("unsupported %v", u)}
			} else {
				it, skip = oraclegen.Generate(reg, name)
			}
		}
		if skip != nil {
			b, _ := json.Marshal(skip)
			fmt.Fprintln(sw, string(b))
			skipped++
			continue
		}
		b, _ := json.Marshal(it)
		fmt.Fprintln(ow, string(b))
		n++
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

func runDiff(dir, scen, xm, out, write, ref string) error {
	if scen == "" || xm == "" || out == "" {
		return fmt.Errorf("diff needs -scenarios, -xmage and -out")
	}
	if write != "" && ref == "" {
		return fmt.Errorf("-write needs -xmage-ref")
	}
	var rows []compliance.VerdictRow
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	xres := map[string]oraclediff.XResult{}
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
		x, ok := xres[it.ID]
		if !ok {
			row.Verdict = oraclediff.Verdict{Status: oraclediff.Harness, Engine: "xmage", Msg: "no XMage result"}
		} else {
			g, gerr := rules.RunOracleScenarioJSON(reg, it.Raw())
			row.Verdict = oraclediff.Compare(g, gerr, x)
			row.XMageMS = x.MS
			vr := compliance.VerdictRow{Card: it.Card, Template: it.Template, ID: it.ID,
				ScenarioSHA: gate.ItemSHA(it), XMageRef: ref}
			switch row.Verdict.Status {
			case oraclediff.Agree:
				vr.Status = compliance.StatusAgree
				vr.CanonSHA = gate.Hash([]byte(oraclediff.Canonical(g.Snapshots)))
			case oraclediff.Diverge:
				vr.Status = compliance.StatusDiverge
				vr.Detail = fmt.Sprintf("%s %s: gorge %q, xmage %q", row.Verdict.Checkpoint, row.Verdict.Field, row.Verdict.Gorge, row.Verdict.XMage)
			default:
				vr.Status = compliance.StatusHarness
				vr.Detail = row.Verdict.Engine + ": " + firstLine(row.Verdict.Msg)
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
	return nil
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
		if n := len(g.Snapshots); n > 0 {
			sb, _ := json.MarshalIndent(g.Snapshots[n-1], "  ", " ")
			fmt.Printf("  last snapshot: %s\n", sb)
		}
		return nil
	})
}
