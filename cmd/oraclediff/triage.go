package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/gate"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/compliance/shape"
)

// cardAPI is a card's IR API signature for ruling matches.
func cardAPI(reg *cards.Registry, name string) string {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return "-"
	}
	return shape.API(c.Faces[0])
}

// printedSets maps each card named on a committed printed list to the
// sets that print it.
func printedSets(root string) map[string][]string {
	out := map[string][]string{}
	paths, _ := filepath.Glob(filepath.Join(root, "compliance", "printed", "*.json"))
	for _, p := range paths {
		code := strings.TrimSuffix(filepath.Base(p), ".json")
		pr, err := compliance.LoadPrinted(filepath.Dir(p), code)
		if err != nil {
			continue
		}
		for _, c := range pr.Cards {
			out[c] = append(out[c], pr.Code)
		}
	}
	for c := range out {
		sort.Strings(out[c])
	}
	return out
}

// freezeRuled sets a row newly classified xmage_wrong to gorge's current
// result for its scenario (the ruled expectation). ok is false when the
// row's scenario is no longer what the generator makes: it needs a pass
// before a ruling can freeze anything.
func freezeRuled(reg *cards.Registry, r *compliance.VerdictRow) bool {
	it, skip := templates.ItemFor(reg, r.Card, r.Template)
	if skip != nil || gate.ItemSHA(it) != r.ScenarioSHA {
		return false
	}
	fz, err := gate.Freeze(reg, it)
	if err != nil {
		return false
	}
	r.Frozen, r.CanonSHA = fz, ""
	return true
}

// runTriage applies the shape rulings to every committed disagreement and
// clusters what no ruling covers (section 11.3 C2). Without -apply it only
// reports.
func runTriage(dir, verdictDir, rulingDir, outDir string, apply bool) error {
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	all, err := compliance.LoadVerdicts(verdictDir)
	if err != nil {
		return err
	}
	rs, err := shape.LoadRulings(rulingDir)
	if err != nil {
		return err
	}
	sets := printedSets(".")
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	clusters := map[string]*shape.Cluster{}
	var changed, review []compliance.VerdictRow
	byRuling := map[string]int{}
	var overlaps, stale []string
	open := 0
	for _, card := range names {
		tmpls := make([]string, 0, len(all[card]))
		for t := range all[card] {
			tmpls = append(tmpls, t)
		}
		sort.Strings(tmpls)
		for _, t := range tmpls {
			r := all[card][t]
			api := cardAPI(reg, card)
			work := r
			applied, ov, ch := shape.Classify(&work, rs, api)
			if len(ov) > 0 {
				overlaps = append(overlaps, fmt.Sprintf("%s (%s): %s also matches %v", card, t, applied.ID, ov))
			}
			if ch && work.Status == compliance.StatusXMageWrong && !work.HasExpectation() {
				if !freezeRuled(reg, &work) {
					stale = append(stale, fmt.Sprintf("%s (%s): ruling %s, but the scenario changed since the verdict; run a pass", card, t, applied.ID))
					work = r
					ch = false
				}
			}
			if ch {
				changed = append(changed, work)
			}
			if work.RulingID != "" {
				byRuling[work.RulingID]++
			}
			if work.Review == shape.ReviewPending {
				review = append(review, work)
			}
			untriaged := work.Status == compliance.StatusDiverge ||
				(work.Status == compliance.StatusHarness && work.Ruling == "")
			if !untriaged {
				continue
			}
			s, ok := shape.Of(work)
			if !ok {
				continue
			}
			open++
			c := clusters[s.Key()]
			if c == nil {
				c = &shape.Cluster{Shape: s}
				clusters[s.Key()] = c
			}
			c.Members = append(c.Members, shape.Member{Card: card, Template: t, Status: work.Status, API: api, Sets: sets[card], Detail: work.Detail})
		}
	}
	keys := make([]string, 0, len(clusters))
	for k := range clusters {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := clusters[keys[i]], clusters[keys[j]]
		if len(a.Members) != len(b.Members) {
			return len(a.Members) > len(b.Members)
		}
		return keys[i] < keys[j]
	})
	if apply {
		if len(changed) > 0 {
			if err := compliance.ReplaceVerdicts(verdictDir, changed); err != nil {
				return err
			}
		}
		if err := writeClusters(outDir, keys, clusters, review); err != nil {
			return err
		}
	}
	verb := "would change"
	if apply {
		verb = "changed"
	}
	fmt.Printf("triage: %d rulings; %s %d verdict rows\n", len(rs), verb, len(changed))
	ids := make([]string, 0, len(byRuling))
	for id := range byRuling {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Printf("  ruling %-40s %d rows\n", id, byRuling[id])
	}
	fmt.Printf("open: %d untriaged rows in %d shape clusters (triage items)\n", open, len(clusters))
	for _, k := range keys {
		fmt.Printf("  %4d  %s\n", len(clusters[k].Members), k)
	}
	if len(review) > 0 {
		fmt.Printf("sampled for review (confirm with oraclediff rule -card NAME -confirm): %d\n", len(review))
		for _, r := range review {
			fmt.Printf("  %s (%s): %s\n", r.Card, r.Template, r.RulingID)
		}
	}
	for _, o := range overlaps {
		fmt.Println("overlap:", o)
	}
	for _, s := range stale {
		fmt.Println("stale:", s)
	}
	return nil
}

// writeClusters replaces outDir's cluster files with the current ones and
// the pending-review list.
func writeClusters(outDir string, keys []string, clusters map[string]*shape.Cluster, review []compliance.VerdictRow) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	old, _ := filepath.Glob(filepath.Join(outDir, "*.jsonl"))
	for _, p := range old {
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	for _, k := range keys {
		c := clusters[k]
		if err := os.WriteFile(filepath.Join(outDir, c.Shape.Slug()+".jsonl"), c.MarshalLines(), 0o644); err != nil {
			return err
		}
	}
	if len(review) == 0 {
		return nil
	}
	f, err := os.Create(filepath.Join(outDir, shape.ReviewFile))
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, r := range review {
		b := shape.Marshal(struct {
			Card     string `json:"card"`
			Template string `json:"template"`
			RulingID string `json:"ruling_id"`
			Status   string `json:"status"`
			Detail   string `json:"detail"`
		}{r.Card, r.Template, r.RulingID, r.Status, r.Detail})
		fmt.Fprintln(w, string(b))
	}
	return w.Flush()
}

// newShapeRuling writes compliance/rulings/<id>.json matching exactly the
// shape of one card's current disagreement.
func newShapeRuling(rulingDir, id, status, text string, r compliance.VerdictRow) (string, error) {
	s, ok := shape.Of(r)
	if !ok {
		return "", fmt.Errorf("%s (%s): no disagreement to take a shape from", r.Card, r.Template)
	}
	ru := shape.Ruling{ID: id, Status: status, Ruling: text,
		Match:  shape.Match{Template: s.Template, Op: s.Op, Field: s.Field, Diff: s.Diff},
		Source: r.Card + " (" + r.Template + ")"}
	var ind bytes.Buffer
	if err := json.Indent(&ind, shape.Marshal(ru), "", "  "); err != nil {
		return "", err
	}
	b := ind.Bytes()
	p := filepath.Join(rulingDir, id+".json")
	if _, err := os.Stat(p); err == nil {
		return "", fmt.Errorf("%s exists", p)
	}
	if err := os.MkdirAll(rulingDir, 0o755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, append(b, '\n'), 0o644)
}
