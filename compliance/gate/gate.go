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
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules"
)

// LevelMeaning says what a claim at level certifies and what it does not
// (spec 2026-10-03 section 11.3 C6: level A is shallow and must say so).
// Every gate report prints it next to the claim.
func LevelMeaning(level string) string {
	switch level {
	case "A":
		return "level A: every card is fully supported and its generated cast-and-resolve, play-land or counter-spell scenario agrees with XMage (or a ruled divergence, or a hand oracle scenario passes); activated abilities, trigger modes, attacks/blocks and statics are NOT exercised (level B)"
	case "B":
		return "level B: level A plus one generated scenario per activated ability, trigger mode, static and attack/block, each compared with XMage; a requirement that no v1 template serves (an unusual activation zone, a trigger mode outside the recipe table, a cost or legality static, a face after 0) is a template gap and waits for its template"
	}
	return "level " + level + ": undefined"
}

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
// its canonical snapshots: the legacy whole-snapshot expectation
// (VerdictRow.CanonSHA), superseded by Freeze.
func GorgeCanon(reg *cards.Registry, it oraclegen.Item) (string, error) {
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		return "", err
	}
	return Hash([]byte(oraclediff.CanonicalOpts(res.Snapshots, it.Compare, it.Ignore...))), nil
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
// Level A is the cast-and-resolve claim (spec 2026-10-03 section 11.3 C6);
// level B is level A plus one scenario per activated ability, trigger mode,
// static and attack/block (spec 2026-10-05 section 4). Any other level is
// not built and reports the one stub problem it always has.
func Check(reg *cards.Registry, root, set, level string) ([]Problem, error) {
	switch level {
	case "A":
		probs, _, _, err := checkA(reg, root, set)
		return probs, err
	case "B":
		return checkB(reg, root, set)
	default:
		return []Problem{{Card: "*", Reason: "level " + level + " has no templates yet"}}, nil
	}
}

// setScan is what a level's card walk needs: the set's printed names, which
// of them XMage can check, the hand-authored scenarios, every verdict row,
// and name resolution. checkA builds it once and both levels share it.
type setScan struct {
	verdicts map[string]map[string]compliance.VerdictRow
	hand     map[string]bool
	names    []string // printed names, in the set's order
	inXMage  map[string]bool
	sup      map[string]bool
	has      func(string) bool
	folded   map[string]string
}

// loadSet reads and resolves everything a card walk needs. The returned
// problems are the set-level ones (no printed list); loadSet does not walk
// cards.
func loadSet(reg *cards.Registry, root, set string) (*setScan, []Problem, error) {
	m, err := compliance.LoadManifest(filepath.Join(root, "compliance", "manifests"), set)
	if err != nil {
		return nil, nil, err
	}
	verdicts, err := compliance.LoadVerdicts(filepath.Join(root, compliance.VerdictDir))
	if err != nil {
		return nil, nil, err
	}
	hand, err := HandScenarios(filepath.Join(root, "rules", "testdata", "oracle"))
	if err != nil {
		return nil, nil, err
	}
	// The claim covers every printed card. XMage's manifest says which of
	// them the oracle can check; the rest need a hand-authored scenario.
	// Names are matched by fold (case, diacritics, punctuation): Forge prints
	// "Dáin Ironfoot" where the XMage set class writes "Dain Ironfoot", and
	// "With Great Power . . ." where XMage writes "With Great Power...".
	// A card the set marks unfinished is in SetCardInfo but removed from
	// XMage's card database, so it is no-XMage too.
	inXMage := map[string]bool{}
	unfinished := map[string]bool{}
	for _, u := range m.Unfinished {
		unfinished[compliance.FoldName(u)] = true
	}
	names := make([]string, 0, len(m.Cards))
	for _, mc := range m.Cards {
		if unfinished[compliance.FoldName(mc.Name)] {
			continue
		}
		inXMage[compliance.FoldName(mc.Name)] = true
		names = append(names, mc.Name)
	}
	var lead []Problem
	if pr, err := compliance.LoadPrinted(filepath.Join(root, "compliance", "printed"), set); err == nil {
		names = pr.Cards
	} else if os.IsNotExist(err) {
		// The XMage manifest omits the cards XMage lacks, so a claim over it
		// would drop them silently (section 11.3 C7): the cards are still
		// reported, but the set cannot be declared until it has a list.
		lead = append(lead, Problem{"*", fmt.Sprintf("no printed list (compliance/printed/%s.json): the XMage manifest omits cards XMage lacks, so the set cannot be declared", set)})
	} else {
		return nil, nil, err
	}
	return &setScan{
		verdicts: verdicts,
		hand:     hand,
		names:    names,
		inXMage:  inXMage,
		sup:      effects.Supported(),
		has:      func(n string) bool { _, ok := reg.Lookup(n); return ok },
		folded:   compliance.FoldedNames(reg),
	}, lead, nil
}

// rowOK applies the level-A verdict-row checks to one generated scenario
// (the block at gate.go's old lines 214-242), reporting any problem through
// bad and returning false. Level A and level B share it so their wording
// cannot drift; the item's template is the requirement key at level B.
func rowOK(reg *cards.Registry, it oraclegen.Item, rows map[string]compliance.VerdictRow, bad func(string, string, ...any)) bool {
	r, ok := rows[it.Template]
	switch {
	case !ok:
		bad(it.Card, "no verdict for %s", it.ID)
		return false
	case r.Status != compliance.StatusAgree && r.Status != compliance.StatusXMageWrong:
		if r.RulingID != "" {
			bad(it.Card, "verdict %s (%s) [ruling %s]: %s", r.Status, it.Template, r.RulingID, r.Detail)
		} else {
			bad(it.Card, "verdict %s (%s): %s", r.Status, it.Template, r.Detail)
		}
		return false
	case r.RulingID != "" && r.Review == "pending":
		// One automatic classification in ten waits for a human check
		// before it counts (shape.Sampled).
		bad(it.Card, "automatic ruling %s sampled for review; check it, then oraclediff rule -card %q -confirm", r.RulingID, it.Card)
		return false
	case r.ScenarioSHA != ItemSHA(it):
		bad(it.Card, "verdict is for an older scenario (%s); re-run the XMage pass", it.ID)
		return false
	}
	if ok, why := StillMeets(reg, it, r); !ok {
		bad(it.Card, "%s (%s)", why, it.ID)
		return false
	}
	return true
}

// checkA runs the level-A checks over set and returns the problems, the
// scan both levels share, and the set of cards that had NO level-A problem
// (so level B skips them wholesale).
func checkA(reg *cards.Registry, root, set string) ([]Problem, *setScan, map[string]bool, error) {
	scan, out, err := loadSet(reg, root, set)
	if err != nil {
		return nil, nil, nil, err
	}
	okA := map[string]bool{}
	bad := func(card, format string, a ...any) {
		out = append(out, Problem{card, fmt.Sprintf(format, a...)})
	}
	for _, printed := range scan.names {
		name, ok := compliance.CorpusNameFold(scan.has, scan.folded, printed)
		if !ok {
			bad(printed, "not in the corpus")
			continue
		}
		c, _ := reg.Lookup(name)
		if isBasicLand(c) {
			continue
		}
		if u := reg.Unsupported(c, scan.sup); len(u) > 0 {
			bad(name, "unsupported %v", u)
			continue
		}
		rows := scan.verdicts[name]
		wrong := false
		lacks := false
		for _, r := range rows {
			switch r.Status {
			case compliance.StatusGorgeWrong:
				bad(name, "gorge_wrong verdict (%s): %s", r.Template, r.Ruling)
				wrong = true
			case compliance.StatusXMageLacks:
				lacks = true
			}
		}
		if wrong {
			continue
		}
		if scan.hand[name] {
			continue
		}
		if !scan.inXMage[compliance.FoldName(printed)] || lacks {
			bad(name, "XMage does not implement it; needs a hand-authored oracle scenario")
			continue
		}
		it, skip := templates.Generate(reg, name)
		if skip != nil {
			bad(name, "no generated scenario (%s) and no hand oracle scenario", skip.Reason)
			continue
		}
		if !rowOK(reg, it, rows, bad) {
			continue
		}
		okA[name] = true
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Card < out[j].Card })
	return out, scan, okA, nil
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
