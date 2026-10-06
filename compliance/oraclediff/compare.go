// Package oraclediff compares gorge's oracle-scenario snapshots with the
// XMage driver's (tools/xmageoracle) and reports the first difference
// (spec 2026-10-02-xmage-compliance-oracle-design section 7).
package oraclediff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules"
)

// XResult is one line of the XMage driver's output.
type XResult struct {
	Name       string                 `json:"name"`
	ID         string                 `json:"id,omitempty"`
	Harness    string                 `json:"harness,omitempty"`
	Strict     bool                   `json:"strict"`
	StrictMiss string                 `json:"strict_miss,omitempty"` // XMage asked what the script did not answer
	Leftover   string                 `json:"leftover,omitempty"`    // scripted answers XMage never asked for
	MS         int                    `json:"ms"`
	Snapshots  []rules.OracleSnapshot `json:"snapshots"`
}

// Status is a comparison outcome.
type Status string

const (
	Agree   Status = "AGREE"
	Diverge Status = "DIVERGE"
	Harness Status = "HARNESS"
	// XMageLacks is not a disagreement: XMage's card database does not hold
	// the card (a set marks it unfinished, so its SetCardInfo entry is
	// removed from the set), and the driver's "Couldn't find a card" is not
	// a driver gap. The card belongs in the gate's no-XMage bucket.
	XMageLacks Status = "XMAGE_LACKS"
)

// Verdict is the first difference between the two engines, or Agree.
type Verdict struct {
	Status     Status `json:"status"`
	Checkpoint string `json:"checkpoint,omitempty"`
	Field      string `json:"field,omitempty"`
	Gorge      string `json:"gorge,omitempty"`
	XMage      string `json:"xmage,omitempty"`
	Engine     string `json:"engine,omitempty"` // for Harness
	Msg        string `json:"msg,omitempty"`    // for Harness
}

// XMageLacksCard reports an XMage database miss only when the named missing
// card is the scenario card. Setup cards and targets can trigger the same
// driver exception; those failures remain HARNESS rather than exempting the
// card under test.
func XMageLacksCard(msg, scenarioCard string) bool {
	const marker = "Couldn't find a card:"
	_, missing, ok := strings.Cut(msg, marker)
	if !ok {
		return false
	}
	missing = strings.TrimSpace(strings.TrimRight(missing, "]"))
	return missing != "" && compliance.FoldName(missing) == compliance.FoldName(scenarioCard)
}

// Compare walks both engines' checkpoints in order. A harness failure on
// either side is never a verdict on the card.
//
// ignore names fields left out of the comparison (by suffix, e.g.
// "library_top" after a shuffle, whose order is random in both engines).
func Compare(g rules.OracleResult, gerr error, x XResult, ignore ...string) Verdict {
	return CompareOpts(g, gerr, x, nil, ignore...)
}

// CompareOpts is Compare with the item's opt-in comparison fields
// (oraclegen.Item.Compare, e.g. ["keywords"] or ["no_library_order"]). A nil
// list is exactly
// Compare, so every level-A comparison is unchanged.
func CompareOpts(g rules.OracleResult, gerr error, x XResult, compare []string, ignore ...string) Verdict {
	if gerr != nil {
		return Verdict{Status: Harness, Engine: "gorge", Msg: gerr.Error()}
	}
	if x.Harness != "" {
		return Verdict{Status: Harness, Engine: "xmage", Msg: x.Harness}
	}
	// The runner marks a failure to PERFORM a scenario "harness:"; any other
	// "step N (op): ..." failure is gorge refusing or failing an action XMage
	// performed, and a leftover answer is a decision-shape divergence.
	for _, f := range g.Fails {
		if strings.Contains(f, "harness:") {
			return Verdict{Status: Harness, Engine: "gorge", Msg: f}
		}
	}
	for _, f := range g.Fails {
		switch {
		case strings.Contains(f, "unconsumed answer(s)"):
			return Verdict{Status: Diverge, Field: "decisions", Gorge: f, XMage: "consumed"}
		case strings.HasPrefix(f, "step "):
			return Verdict{Status: Diverge, Field: "action", Gorge: f, XMage: "performed"}
		}
	}
	if len(g.Snapshots) != len(x.Snapshots) {
		return Verdict{Status: Harness, Engine: "both", Msg: fmt.Sprintf("%d gorge checkpoints, %d xmage", len(g.Snapshots), len(x.Snapshots))}
	}
	for i := range g.Snapshots {
		if v, ok := compareSnap(g.Snapshots[i], x.Snapshots[i], compare, ignore); !ok {
			return v
		}
	}
	return Verdict{Status: Agree}
}

// field is one compared value of a checkpoint, already normalized.
type field struct{ name, value string }

// fields renders a snapshot as the ordered, normalized field list the
// comparator compares; xmage selects XMage's step vocabulary. The list
// doubles as the frozen expectation (Canonical); offered labels additionally
// admit normalized advisory prefixes through fieldEqual.
func fields(s rules.OracleSnapshot, xmage bool, compare []string) []field {
	step := s.Step
	if xmage {
		step = XMageStep(step)
	}
	out := []field{
		{"turn", fmt.Sprint(s.Turn)}, {"step", step},
		{"active", fmt.Sprint(s.Active)}, {"over", fmt.Sprint(s.Over)},
		{"players", fmt.Sprint(len(s.Players))},
	}
	noLibraryOrder := wantsCompare(compare, CompareNoLibraryOrder)
	for i, p := range s.Players {
		pf := func(f string) string { return fmt.Sprintf("p%d.%s", i, f) }
		out = append(out,
			field{pf("life"), fmt.Sprint(p.Life)},
			field{pf("counters"), counters(p.Counters)},
			field{pf("hand"), list(p.Hand, true)},
			field{pf("graveyard"), list(p.Graveyard, false)},
			field{pf("exile"), list(p.Exile, true)},
			field{pf("library_count"), fmt.Sprint(p.LibraryCount)},
		)
		if !noLibraryOrder {
			out = append(out, field{pf("library_top"), list(p.LibraryTop, false)})
		}
		out = append(out,
			field{pf("pool"), sortMana(p.Pool)},
		)
	}
	out = append(out,
		field{"permanents", strings.Join(permKeysOpts(s.Permanents, compare), "\n")},
		field{"stack", stackKeys(s.Stack)},
	)
	if wantsCompare(compare, CompareOffered) {
		out = append(out, field{"offered", offeredKeys(s.Offered)})
	}
	return out
}

func ignored(name string, ignore []string) bool {
	for _, s := range ignore {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

func compareSnap(g, x rules.OracleSnapshot, compare []string, ignore []string) (Verdict, bool) {
	if g.Checkpoint != x.Checkpoint {
		return Verdict{Status: Harness, Engine: "both", Msg: fmt.Sprintf("checkpoint %q vs %q", g.Checkpoint, x.Checkpoint)}, false
	}
	gf, xf := fields(g, false, compare), fields(x, true, compare)
	for k := range gf {
		if ignored(gf[k].name, ignore) {
			continue
		}
		if k >= len(xf) || !fieldEqual(gf[k], xf[k]) {
			xv := ""
			if k < len(xf) {
				xv = xf[k].value
			}
			gv := gf[k].value
			if gf[k].name == "permanents" {
				onlyG, onlyX := symDiff(strings.Split(gv, "\n"), strings.Split(xv, "\n"))
				gv, xv = strings.Join(onlyG, "; "), strings.Join(onlyX, "; ")
			}
			return Verdict{Status: Diverge, Checkpoint: g.Checkpoint, Field: gf[k].name, Gorge: gv, XMage: xv}, false
		}
	}
	return Verdict{}, true
}

// Canonical is gorge's side of a scenario in the comparator's normalized
// form. Disambiguating offered labels retain their full normalized text;
// Compare and Meets also accept advisory prefixes of that text. A verdict row
// freezes its hash, so the CI gate can re-check gorge without Java.
func Canonical(snaps []rules.OracleSnapshot, ignore ...string) string {
	return CanonicalOpts(snaps, nil, ignore...)
}

// CanonicalOpts is Canonical with the item's opt-in comparison fields
// (oraclegen.Item.Compare). A nil list is exactly Canonical.
func CanonicalOpts(snaps []rules.OracleSnapshot, compare []string, ignore ...string) string {
	var b strings.Builder
	for _, s := range snaps {
		fmt.Fprintf(&b, "== %s\n", s.Checkpoint)
		for _, f := range fields(s, false, compare) {
			if ignored(f.name, ignore) {
				continue
			}
			fmt.Fprintf(&b, "%s: %s\n", f.name, f.value)
		}
	}
	return b.String()
}

// XMageStep maps an XMage PhaseStep name onto gorge's step name.
func XMageStep(s string) string {
	switch s {
	case "UNTAP":
		return "untap"
	case "UPKEEP":
		return "upkeep"
	case "DRAW":
		return "draw"
	case "PRECOMBAT_MAIN":
		return "main1"
	case "BEGIN_COMBAT":
		return "begin-combat"
	case "DECLARE_ATTACKERS":
		return "declare-attackers"
	case "DECLARE_BLOCKERS":
		return "declare-blockers"
	case "FIRST_COMBAT_DAMAGE", "COMBAT_DAMAGE":
		return "combat-damage"
	case "END_COMBAT":
		return "end-combat"
	case "POSTCOMBAT_MAIN":
		return "main2"
	case "END_TURN":
		return "end"
	case "CLEANUP":
		return "cleanup"
	}
	return s
}

// RefName strips a scenario ref ("p1:token:Name#2") to the object name.
func RefName(ref string) string {
	n := ref
	if i := strings.IndexByte(n, ':'); i >= 0 && strings.HasPrefix(n, "p") {
		n = n[i+1:]
	}
	n = strings.TrimPrefix(n, "token:")
	if j := strings.LastIndexByte(n, '#'); j >= 0 && j+1 < len(n) && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n
}

func normCounter(k string) string {
	switch strings.ReplaceAll(strings.ToLower(k), " ", "") {
	case "+1/+1", "p1p1":
		return "P1P1"
	case "-1/-1", "m1m1":
		return "M1M1"
	}
	return strings.ToUpper(strings.ReplaceAll(k, " ", ""))
}

func counters(m map[string]int32) string {
	keys := make([]string, 0, len(m))
	agg := map[string]int32{}
	for k, v := range m {
		if v == 0 {
			continue
		}
		nk := normCounter(k)
		if _, ok := agg[nk]; !ok {
			keys = append(keys, nk)
		}
		agg[nk] += v
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, agg[k]))
	}
	return strings.Join(parts, ",")
}

func list(s []string, sorted bool) string {
	c := append([]string(nil), s...)
	if sorted {
		sort.Strings(c)
	}
	return "[" + strings.Join(c, ", ") + "]"
}

// sortMana renders any WUBRGC letter string in WUBRGC order.
func sortMana(s string) string {
	var b strings.Builder
	for _, c := range "WUBRGC" {
		b.WriteString(strings.Repeat(string(c), strings.Count(strings.ToUpper(s), string(c))))
	}
	return b.String()
}

// colors keeps only WUBRG letters, in WUBRG order: XMage's ObjectColor and
// gorge's colour string spell colourless differently.
func colors(s string) string {
	var b strings.Builder
	for _, c := range "WUBRG" {
		if strings.ContainsRune(strings.ToUpper(s), c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func types(ts []string, allCreatureTypes bool) string {
	c := make([]string, 0, len(ts)+1)
	subtypeWords := make(map[string]bool, len(effects.CreatureTypeWordList()))
	for _, subtype := range effects.CreatureTypeWordList() {
		subtypeWords[strings.ToLower(strings.ReplaceAll(subtype, " ", ""))] = true
	}
	for _, t := range ts {
		normalized := strings.ToLower(strings.ReplaceAll(t, " ", ""))
		if allCreatureTypes && subtypeWords[normalized] {
			continue
		}
		c = append(c, normalized)
	}
	if allCreatureTypes {
		c = append(c, "allcreaturetypes")
	}
	sort.Strings(c)
	return strings.Join(c, " ")
}

// permKeys renders each permanent as one canonical line and sorts them:
// cards match by controller and name (the multiset), tokens by
// characteristics, since the engines name tokens differently. It is
// permKeysOpts with no opt-in comparison fields.
func permKeys(ps []rules.OracleSnapPerm) []string {
	return permKeysOpts(ps, nil)
}

// permKeysOpts is permKeys with the item's opt-in field list: when it names
// "keywords" the line gains the permanent's folded evergreen keywords
// (spec 2026-10-05-compliance-level-b section 2.3).
func permKeysOpts(ps []rules.OracleSnapPerm, compare []string) []string {
	wantKeywords := wantsCompare(compare, CompareKeywords)
	// attached_to names its host the way XMage's driver does: the host
	// permanent's CURRENT name (its layer-3 SetName$ name, or "" while face
	// down, CR 708.2a), never the printed name the ref encodes. An empty
	// host name is omitted, matching the driver's absent field.
	hostName := map[string]string{}
	for _, p := range ps {
		hostName[p.Ref] = p.Name
	}
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		name := p.Name
		if p.Token {
			name = "token"
		}
		k := fmt.Sprintf("c%d o%d %s [%s] {%s}", p.Controller, p.Owner, name, types(p.Types, p.AllCreatureTypes), colors(p.Colors))
		if p.PT != "" {
			k += " " + p.PT
		}
		if p.Tapped {
			k += " tapped"
		}
		if p.FaceDown {
			k += " facedown"
		}
		if p.Damage != 0 {
			k += fmt.Sprintf(" dmg=%d", p.Damage)
		}
		if c := counters(p.Counters); c != "" {
			k += " counters=" + c
		}
		if wantKeywords {
			if kw := evergreenList(p.Keywords); kw != "" {
				k += " kw=" + kw
			}
		}
		if p.AttachedTo != "" {
			if host, ok := hostName[p.AttachedTo]; !ok {
				k += " on=" + RefName(p.AttachedTo)
			} else if host != "" {
				k += " on=" + host
			}
		}
		if p.Attacking {
			k += " attacking"
		}
		if p.Blocking {
			k += " blocking"
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// wantsCompare reports whether the item's opt-in field list names field.
func wantsCompare(compare []string, field string) bool {
	for _, c := range compare {
		if c == field {
			return true
		}
	}
	return false
}

func symDiff(a, b []string) (onlyA, onlyB []string) {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case j >= len(b) || i < len(a) && a[i] < b[j]:
			onlyA = append(onlyA, a[i])
			i++
		case i >= len(a) || b[j] < a[i]:
			onlyB = append(onlyB, b[j])
			j++
		default:
			i++
			j++
		}
	}
	return onlyA, onlyB
}

func stackKeys(st []rules.OracleSnapStack) string {
	parts := make([]string, 0, len(st))
	for _, s := range st {
		parts = append(parts, fmt.Sprintf("%s:%s@p%d", s.Kind, RefName(s.Source), s.Controller))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
