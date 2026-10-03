// Package gate is the compliance claim's check (spec section 8): for a
// declared set, every manifest card must be fully supported and covered by
// a passing verdict whose frozen expectation gorge still meets at this head
// -- or by a hand-authored oracle scenario. The CI test
// (TestDeclaredSetsCompliant) and `oraclediff status` share it.
package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules"
)

// Problem is one card that keeps a set from its declared level.
type Problem struct {
	Card   string `json:"card"`
	Reason string `json:"reason"`
}

// Hash is the sha256 hex of b.
func Hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ItemSHA is the hash a verdict row records for a generated scenario.
func ItemSHA(it oraclegen.Item) string {
	b, _ := json.Marshal(it)
	return Hash(b)
}

// GorgeCanon replays a generated scenario in gorge and returns the hash of
// its canonical snapshots.
func GorgeCanon(reg *cards.Registry, it oraclegen.Item) (string, error) {
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		return "", err
	}
	return Hash([]byte(oraclediff.Canonical(res.Snapshots, it.Ignore...))), nil
}

// HandScenarios lists the cards that have a hand-authored oracle scenario
// under oracleDir (rules/testdata/oracle) and no known-divergent row. The
// audit (rules.TestOracleAudit) holds every such scenario green.
func HandScenarios(oracleDir string) (map[string]bool, error) {
	out := map[string]bool{}
	paths, err := filepath.Glob(filepath.Join(oracleDir, "*", "*.json"))
	if err != nil {
		return nil, err
	}
	divergent := map[string]bool{}
	agg, _ := filepath.Glob(filepath.Join(oracleDir, "*", "known-divergent.json"))
	for _, p := range agg {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var rows map[string]string
		if json.Unmarshal(raw, &rows) == nil {
			for k := range rows {
				divergent[strings.SplitN(k, "/", 2)[0]] = true
			}
		}
	}
	per, _ := filepath.Glob(filepath.Join(oracleDir, "*", "known-divergent", "*.json"))
	for _, p := range per {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var pc struct {
			Card string `json:"card"`
		}
		if json.Unmarshal(raw, &pc) == nil && pc.Card != "" {
			divergent[pc.Card] = true
		}
	}
	for _, p := range paths {
		if filepath.Base(p) == "known-divergent.json" {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var f struct {
			Card string `json:"card"`
		}
		if json.Unmarshal(raw, &f) == nil && f.Card != "" && !divergent[f.Card] {
			out[f.Card] = true
		}
	}
	return out, nil
}

// Check returns every card of set that keeps it from level, sorted. root is
// the repo root (manifests, verdicts, oracle scenarios are read under it).
func Check(reg *cards.Registry, root, set, level string) ([]Problem, error) {
	m, err := compliance.LoadManifest(filepath.Join(root, "compliance", "manifests"), set)
	if err != nil {
		return nil, err
	}
	verdicts, err := compliance.LoadVerdicts(filepath.Join(root, compliance.VerdictDir))
	if err != nil {
		return nil, err
	}
	hand, err := HandScenarios(filepath.Join(root, "rules", "testdata", "oracle"))
	if err != nil {
		return nil, err
	}
	if level != "A" {
		return []Problem{{Card: "*", Reason: "level " + level + " has no templates yet"}}, nil
	}
	sup := effects.Supported()
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	var out []Problem
	bad := func(card, format string, a ...any) {
		out = append(out, Problem{card, fmt.Sprintf(format, a...)})
	}
	// The claim covers every printed card. XMage's manifest says which of
	// them the oracle can check; the rest need a hand-authored scenario.
	inXMage := map[string]bool{}
	names := make([]string, 0, len(m.Cards))
	for _, mc := range m.Cards {
		inXMage[mc.Name] = true
		names = append(names, mc.Name)
	}
	if pr, err := compliance.LoadPrinted(filepath.Join(root, "compliance", "printed"), set); err == nil {
		names = pr.Cards
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for _, printed := range names {
		name, ok := compliance.CorpusName(has, printed)
		if !ok {
			bad(printed, "not in the corpus")
			continue
		}
		c, _ := reg.Lookup(name)
		if isBasicLand(c) {
			continue
		}
		if u := reg.Unsupported(c, sup); len(u) > 0 {
			bad(name, "unsupported %v", u)
			continue
		}
		rows := verdicts[name]
		wrong := false
		for _, r := range rows {
			if r.Status == compliance.StatusGorgeWrong {
				bad(name, "gorge_wrong verdict (%s): %s", r.Template, r.Ruling)
				wrong = true
			}
		}
		if wrong {
			continue
		}
		if hand[name] {
			continue
		}
		if !inXMage[printed] {
			bad(name, "XMage does not implement it; needs a hand-authored oracle scenario")
			continue
		}
		it, skip := oraclegen.Generate(reg, name)
		if skip != nil {
			bad(name, "no generated scenario (%s) and no hand oracle scenario", skip.Reason)
			continue
		}
		r, ok := rows[it.Template]
		switch {
		case !ok:
			bad(name, "no verdict for %s", it.ID)
			continue
		case r.Status != compliance.StatusAgree && r.Status != compliance.StatusXMageWrong:
			bad(name, "verdict %s (%s): %s", r.Status, it.Template, r.Detail)
			continue
		case r.ScenarioSHA != ItemSHA(it):
			bad(name, "verdict is for an older scenario (%s); re-run the XMage pass", it.ID)
			continue
		}
		if ok, why := StillMeets(reg, it, r); !ok {
			bad(name, "%s (%s)", why, it.ID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Card < out[j].Card })
	return out, nil
}

func isBasicLand(c *cards.Card) bool {
	if len(c.Faces) == 0 {
		return false
	}
	basic, land := false, false
	for _, t := range c.Faces[0].Types {
		basic = basic || strings.EqualFold(t, "Basic")
		land = land || strings.EqualFold(t, "Land")
	}
	return basic && land
}
