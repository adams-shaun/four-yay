package main

// The Forge oracle's gorge side (docs: .ds4/forge-oracle/DESIGN.md sections
// 5.2, 7.1, 7.2).
//
//	oraclediff forge-export [-cards .cards] -scenarios scen.jsonl -out forge-req.jsonl
//	oraclediff forge-diff   [-cards .cards] -req forge-req.jsonl [-forge forge.jsonl]
//	                        (-cache DIR | -oracle-ref REF -driver SHA) -out forge-diff.jsonl
//
// forge-export replays each gen Item in gorge once and writes the sidecar
// request the Forge driver reads: the Item exactly as gen wrote it, the
// decisions gorge's runner made (the answer source) and, per ability_index
// step, the raw script line of the ability that index names. forge-diff
// compares gorge with the Forge driver's rows through the unchanged
// oraclediff.CompareOpts and caches those rows by request sha. Neither
// command touches gen's output or compliance/verdicts, and both refuse an
// output path inside this repository: the request carries script text and
// Forge's rows are Forge output, neither of which may be committed.

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/gate"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// defaultForgeOracleDir is the host's off-repo Forge state (DESIGN section
// 4.2), used when FORGE_ORACLE_DIR is unset.
const defaultForgeOracleDir = "/mnt/sata/gorge-training/forgeoracle"

// forgeRequest is one line of forge-req.jsonl. The Item is marshalled
// exactly as gen wrote it, so its bytes (and gate.ItemSHA) are unchanged.
//
// The contract the Forge driver reads (DESIGN section 5.2 records the reasons):
//
//   - A divided allocation (Twin Bolt, Forked Bolt, distribute counters) is
//     not a field: gorge poses it as its own decision AFTER the target ask,
//     a "choose_n" with resume "damage_split", whose options are the chosen
//     targets in order (pick_refs) and whose pick_idx repeats a target's
//     index once per point it receives. min == max == the total. The target
//     ask's own "divided" is the literal total only for a triggered ability
//     and is 0 for a spell cast from hand, so the driver sums the split.
//   - A card ref's ordinal ("p0:Wastes#27") counts gorge's object order,
//     which the deal shuffles. Name and owner are the identity; the driver
//     resolves within the candidates Forge offers, which are one zone.
//   - Decisions are exported verbatim. pay_<C>, pay_generic and pay_life are
//     pick kinds of gorge's hybrid/Phyrexian pip ask, and a mana-ability
//     "activate" pick is a payment-window choice; Forge asks neither, so the
//     driver does not count them as leftover.
//   - arrange: the picks are the cards kept on top in pick order; the
//     pick_kinds name where the unpicked go (bottom, graveyard, exile, hand),
//     in offered order. hideaway_bottom and dig_bottom leave none unpicked:
//     every offered card goes to the bottom and the picks give its order.
//   - trigger_cost_pay and trigger_cost_decline answer the payment of a
//     triggered ability's cost; gift_decline declines a gift (gift_promise
//     names the opponent in its label).
//   - A cost is paid only if a decision records it: gen's Step carries no
//     kicked or cast_mode, so a cast is the plain one (or the face the card
//     ref names) unless a logged optional-cost decision says otherwise.
//   - abilities is keyed by the decimal step index.
//   - library_top is not compared when a seat's setup names library cards
//     (see forgeIgnore).
//   - A Forge row may echo request_sha, the sha256 of this line's bytes
//     without the newline; forge-diff drops a row whose echo differs.
type forgeRequest struct {
	ID          string         `json:"id"`
	ScenarioSHA string         `json:"scenario_sha"`
	Item        oraclegen.Item `json:"item"`
	// GorgeDecisions is the answer source: what gorge's deterministic
	// runner answered, so both engines answer alike.
	GorgeDecisions []rules.OracleDecision `json:"gorge_decisions"`
	// Abilities maps a step index to the raw script line of the ability
	// that step's ability_index names, the key the driver matches on.
	Abilities map[string]string `json:"abilities"`
}

// ForgeRow is one line of forge-diff's output.
type ForgeRow struct {
	ID          string             `json:"id"`
	Card        string             `json:"card"`
	Template    string             `json:"template"`
	Verdict     oraclediff.Verdict `json:"verdict"`
	ForgeMS     int                `json:"forge_ms"`
	RequestSHA  string             `json:"request_sha"`
	ScenarioSHA string             `json:"scenario_sha"`
}

func runForgeExport(args []string) error {
	fs := flag.NewFlagSet("forge-export", flag.ExitOnError)
	dir := fs.String("cards", ".cards", "corpus dir")
	scen := fs.String("scenarios", "", "scenario JSONL (from gen)")
	out := fs.String("out", "", "sidecar request JSONL (off-repo)")
	fs.Parse(args)
	if *scen == "" || *out == "" {
		return fmt.Errorf("forge-export needs -scenarios and -out")
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	n, err := forgeExport(reg, *scen, *out)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %d requests to %s\n", n, *out)
	return nil
}

// forgeExport writes one request line per scenario Item, in input order.
func forgeExport(reg *cards.Registry, scen, out string) (int, error) {
	if err := refuseRepoPath(out); err != nil {
		return 0, err
	}
	of, err := os.Create(out)
	if err != nil {
		return 0, err
	}
	defer of.Close()
	w := bufio.NewWriter(of)
	n := 0
	if err := readLines(scen, func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		g, gerr := rules.RunOracleScenarioJSON(reg, it.Raw())
		decisions := g.Decisions
		if gerr != nil || decisions == nil {
			decisions = []rules.OracleDecision{}
		}
		line, err := json.Marshal(forgeRequest{
			ID: it.ID, ScenarioSHA: gate.ItemSHA(it), Item: it,
			GorgeDecisions: decisions, Abilities: forgeAbilityLines(reg, it),
		})
		if err != nil {
			return err
		}
		n++
		w.Write(line)
		return w.WriteByte('\n')
	}); err != nil {
		return n, err
	}
	if err := w.Flush(); err != nil {
		return n, err
	}
	return n, of.Close()
}

// forgeAbilityLines maps each step that names an ability by ability_index to
// the script line (cards.SA.Line) of Face.Abilities[index] on the step's
// card, read from the card's first face. A step whose card does not resolve
// to a corpus card, or whose index is out of range, is left out: the driver
// then falls back to its own matching rather than trusting a wrong line.
func forgeAbilityLines(reg *cards.Registry, it oraclegen.Item) map[string]string {
	lines := map[string]string{}
	for i, st := range it.Steps {
		if st.AbilityIndex == nil || st.Card == "" {
			continue
		}
		c, ok := reg.Lookup(refCardName(st.Card))
		if !ok || len(c.Faces) == 0 {
			continue
		}
		f := c.Faces[0]
		if k := *st.AbilityIndex; k >= 0 && k < len(f.Abilities) && f.Abilities[k] != nil {
			lines[strconv.Itoa(i)] = f.Abilities[k].Line
		}
	}
	return lines
}

// refCardName is the printed name inside a card ref ("p0:Shock#2" is
// "Shock"). A bare name is returned as is.
func refCardName(ref string) string {
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		ref = ref[i+1:]
	}
	if j := strings.LastIndexByte(ref, '#'); j >= 0 {
		if _, err := strconv.Atoi(ref[j+1:]); err == nil {
			ref = ref[:j]
		}
	}
	return ref
}

func runForgeDiff(args []string) error {
	fs := flag.NewFlagSet("forge-diff", flag.ExitOnError)
	dir := fs.String("cards", ".cards", "corpus dir")
	req := fs.String("req", "", "sidecar request JSONL (from forge-export)")
	forge := fs.String("forge", "", "Forge driver output JSONL; rows missing from it are read from the cache")
	cache := fs.String("cache", "", "Forge result cache directory (default: $FORGE_ORACLE_DIR/cache/<oracle-ref12>-<driver12>)")
	ref := fs.String("oracle-ref", "", "FORGE_ORACLE_REF the rows came from (names the cache directory)")
	driver := fs.String("driver", "", "driver source sha (names the cache directory)")
	out := fs.String("out", "", "forge-diff JSONL (off-repo)")
	fs.Parse(args)
	if *req == "" || *out == "" {
		return fmt.Errorf("forge-diff needs -req and -out")
	}
	cacheDir, err := forgeCacheDir(*cache, *ref, *driver)
	if err != nil {
		return err
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	counts, err := forgeDiff(reg, *req, *forge, cacheDir, *out)
	if err != nil {
		return err
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

// forgeCacheDir is the Forge result cache: an explicit -cache, else
// $FORGE_ORACLE_DIR/cache/<oracle ref[:12]>-<driver sha[:12]>. The name
// carries both pins because Forge's side of a request is a function of the
// request, FORGE_ORACLE_REF and the driver source.
func forgeCacheDir(explicit, ref, driver string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if ref == "" || driver == "" {
		return "", fmt.Errorf("forge-diff needs -cache, or -oracle-ref and -driver")
	}
	base := os.Getenv("FORGE_ORACLE_DIR")
	if base == "" {
		base = defaultForgeOracleDir
	}
	return filepath.Join(base, "cache", short12(ref)+"-"+short12(driver)), nil
}

func short12(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// forgeDiff compares gorge with Forge for every request line and writes one
// ForgeRow per request. Forge rows come from forgePath (stored in the cache
// under the request sha) or, when absent there, from the cache. The result
// is the status census, keyed like diff's.
func forgeDiff(reg *cards.Registry, reqPath, forgePath, cacheDir, out string) (map[string]int, error) {
	if err := refuseRepoPath(out); err != nil {
		return nil, err
	}
	if forgePath == "" && cacheDir == "" {
		return nil, fmt.Errorf("forge-diff needs -forge or a cache")
	}
	cache := oraclediff.Cache{Dir: cacheDir}
	fres := map[string]oraclediff.XResult{}
	echoed := map[string]string{}
	if forgePath != "" {
		if err := readLines(forgePath, func(b []byte) error {
			var r struct {
				oraclediff.XResult
				Engine     string `json:"engine"`
				RequestSHA string `json:"request_sha"`
			}
			if err := json.Unmarshal(b, &r); err != nil {
				return err
			}
			// An XMage row fed in by mistake would compare as Forge's.
			if r.Engine != "forge" {
				return fmt.Errorf("row %q is not a Forge row (engine %q)", r.ID, r.Engine)
			}
			fres[r.ID] = r.XResult
			echoed[r.ID] = r.RequestSHA
			return nil
		}); err != nil {
			return nil, err
		}
	}
	of, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	defer of.Close()
	w := bufio.NewWriter(of)
	counts := map[string]int{}
	if err := readLines(reqPath, func(b []byte) error {
		var rq forgeRequest
		if err := json.Unmarshal(b, &rq); err != nil {
			return err
		}
		it := rq.Item
		sha := gate.Hash(b)
		row := ForgeRow{ID: it.ID, Card: it.Card, Template: it.Template, RequestSHA: sha, ScenarioSHA: rq.ScenarioSHA}
		x, ok := fres[it.ID]
		missMsg := "no Forge result"
		if echo := echoed[it.ID]; ok && echo != "" && echo != sha {
			// The row answers another version of this request (an older
			// export): caching it under this sha would poison the cache.
			ok = false
			missMsg = fmt.Sprintf("stale Forge row: it echoes request_sha %s, this request is %s", short12(echo), short12(sha))
		}
		if ok && cacheDir != "" {
			if err := cache.Put(sha, x); err != nil {
				return err
			}
		} else if !ok {
			x, ok = cache.Get(sha)
		}
		if !ok {
			row.Verdict = oraclediff.Verdict{Status: oraclediff.Harness, Engine: "forge", Msg: missMsg}
		} else {
			g, gerr := rules.RunOracleScenarioJSON(reg, it.Raw())
			row.Verdict = oraclediff.CompareOpts(g, gerr, x, it.Compare, forgeIgnore(it)...)
			if row.Verdict.Engine == "xmage" {
				row.Verdict.Engine = "forge"
			}
			row.ForgeMS = x.MS
		}
		key := string(row.Verdict.Status)
		if row.Verdict.Status == oraclediff.Diverge {
			key += ":" + row.Verdict.Field
		}
		counts[key]++
		rb, err := json.Marshal(row)
		if err != nil {
			return err
		}
		w.Write(rb)
		return w.WriteByte('\n')
	}); err != nil {
		return nil, err
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	return counts, of.Close()
}

// forgeIgnore is the item's own ignore list plus library_top when a seat's
// setup names library cards. Gorge's deal shuffles the library the named
// cards were mixed into (measured: Library ["Forest","Plains","Shock"] leaves
// Plains fifth from the top and Forest outside the window), while the Forge
// driver puts them under the filler, so the order cannot agree. A seat that
// names only library_top is a deterministic top (named cards, then filler),
// so it stays compared.
func forgeIgnore(it oraclegen.Item) []string {
	out := append([]string(nil), it.Ignore...)
	for _, seat := range it.Setup {
		if len(seat.Library) > 0 {
			return append(out, "library_top")
		}
	}
	return out
}

// refuseRepoPath rejects an output path inside the repository (the nearest
// ancestor of the working directory holding go.mod). Forge output and the
// request's script text are never committed, and a path under
// compliance/verdicts would be the one place a Forge pass must not write.
func refuseRepoPath(path string) error {
	root := repoRoot()
	if root == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	abs = resolveExisting(abs)
	root = resolveExisting(root)
	if rel, err := filepath.Rel(root, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to write %s: it is inside the repository %s (Forge output and request files stay off-repo)", path, root)
	}
	return nil
}

func repoRoot() string {
	d, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		p := filepath.Dir(d)
		if p == d {
			return ""
		}
		d = p
	}
}

// resolveExisting resolves symlinks in the longest existing prefix of p, so
// a not-yet-created output file is judged by where its directory really is.
func resolveExisting(p string) string {
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}
