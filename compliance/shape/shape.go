// Package shape gives every XMage disagreement a reusable signature, so a
// triage ruling is made once per root cause instead of once per card
// (docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md section
// 11.3 C2; the oracle is 2026-10-02-xmage-compliance-oracle-design.md
// section 8).
//
// A Shape is (template, checkpoint op, field, normalised diff). The diff is
// normalised by the role each name plays rather than the name itself: the
// card under test is $CARD, the library filler the runner pads every
// library with is $LIB, and the generator's fixture names (Grizzly Bears,
// Shock, ...) stay literal because they are the same for every card. Counts
// are dropped from zone diffs, and a diff that moves nothing but library
// filler is one "library placement" shape whichever zone it surfaced in:
// that is a library choice the engines made differently (surveil, scry, a
// "may" search), whatever card asked it. A driver message is normalised the
// same way, minus the target list it echoes.
//
// A Ruling (compliance/rulings/<id>.json) matches shapes, optionally
// narrowed to the IR APIs it was decided for, and classifies every row it
// matches automatically. Unmatched rows are clustered by shape into
// compliance/triage/<slug>.jsonl: one triage item per cluster.
package shape

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
)

// Filler is the library filler of every oracle scenario
// (rules.oracleFiller).
const Filler = "Wastes"

// Shape is one disagreement's signature.
type Shape struct {
	// Template is the template FAMILY (the part of a row's template before
	// '#'), so every slot of one level-B family (activate#0.1, activate#0.3)
	// and every level-A template string (which has no '#') key one shape.
	Template string `json:"template"`
	Op       string `json:"op"`    // checkpoint op (cast, resolve, play), or "harness"
	Field    string `json:"field"` // comparator field, pN.library for library placement, or the harness engine
	Diff     string `json:"diff"`  // normalised difference or driver message
}

// Key is the shape as one string, the cluster identity.
func (s Shape) Key() string {
	return s.Template + " | " + s.Op + " | " + s.Field + " | " + s.Diff
}

// Slug is the shape's file name stem under compliance/triage: readable
// template and field, plus a hash of the whole key.
func (s Shape) Slug() string {
	h := sha256.Sum256([]byte(s.Key()))
	clean := func(x string) string {
		return strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
				return r
			}
			return '-'
		}, x)
	}
	return clean(s.Template) + "." + clean(s.Op) + "." + clean(s.Field) + "." + hex.EncodeToString(h[:4])
}

var (
	divergeRE = regexp.MustCompile(`^(.*?) ?([A-Za-z0-9_.]+): gorge ("(?:[^"\\]|\\.)*"), xmage ("(?:[^"\\]|\\.)*")$`)
	stepRE    = regexp.MustCompile(`^step \d+ \((.*)\)$`)
	digitsRE  = regexp.MustCompile(`\d+`)
	bracketRE = regexp.MustCompile(`\[[^\]]*\]`)
)

// Of computes the shape of a diverge or harness verdict row from its Detail
// (the comparator's first difference, or the harness message). ok is false
// for a row with no disagreement to shape.
func Of(r compliance.VerdictRow) (Shape, bool) {
	if r.Detail == "" {
		return Shape{}, false
	}
	if r.Status == compliance.StatusHarness {
		engine, msg, _ := strings.Cut(r.Detail, ": ")
		return Shape{Template: templateFamily(r.Template), Op: "harness", Field: engine, Diff: harnessMsg(msg, r.Card)}, true
	}
	m := divergeRE.FindStringSubmatch(r.Detail)
	if m == nil {
		return Shape{}, false
	}
	g, err1 := strconv.Unquote(m[3])
	x, err2 := strconv.Unquote(m[4])
	if err1 != nil || err2 != nil {
		return Shape{}, false
	}
	op := "-"
	if sm := stepRE.FindStringSubmatch(m[1]); sm != nil {
		op = sm[1]
	} else if m[1] != "" {
		op = m[1]
	}
	field, diff := normDiff(m[2], g, x, r.Card)
	return Shape{Template: templateFamily(r.Template), Op: op, Field: field, Diff: diff}, true
}

// templateFamily is a template string's family: the part before '#' for a
// level-B requirement key (activate#0.2 -> activate), the whole string for a
// level-A template, which has no '#'. Level-A strings are returned unchanged,
// so every committed shape id and triage file name stays as it was.
func templateFamily(template string) string {
	if i := strings.IndexByte(template, '#'); i >= 0 {
		return template[:i]
	}
	return template
}

// harnessMsg normalises a driver message: the card is $CARD, the target
// list a cast command echoes is dropped, bracketed lists and numbers are
// elided.
func harnessMsg(msg, card string) string {
	msg = strings.ReplaceAll(msg, card, "$CARD")
	if i := strings.Index(msg, "$CARD$"); i >= 0 {
		// "Cast X$target=Grizzly Bears^..." -- the targets are the fixture.
		msg = msg[:i+len("$CARD")]
	}
	msg = bracketRE.ReplaceAllString(msg, "[…]")
	msg = digitsRE.ReplaceAllString(msg, "N")
	if len(msg) > 160 {
		msg = msg[:160]
	}
	return msg
}

func role(name, card string) string {
	if name == card {
		return "$CARD"
	}
	for _, f := range strings.Split(card, " // ") {
		if name == f {
			return "$CARD"
		}
	}
	if name == Filler {
		return "$LIB"
	}
	return name
}

func parseList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	if s == "" {
		return nil
	}
	return strings.Split(s, ", ")
}

// multisetDiff returns a minus b and b minus a, each sorted.
func multisetDiff(a, b []string) (onlyA, onlyB []string) {
	cnt := map[string]int{}
	for _, x := range a {
		cnt[x]++
	}
	for _, x := range b {
		cnt[x]--
	}
	for x, n := range cnt {
		for ; n > 0; n-- {
			onlyA = append(onlyA, x)
		}
		for ; n < 0; n++ {
			onlyB = append(onlyB, x)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	return onlyA, onlyB
}

func uniq(xs []string) []string {
	var out []string
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}

func set(xs []string) string { return "[" + strings.Join(uniq(xs), ", ") + "]" }

func onlyFiller(xs ...[]string) bool {
	n := 0
	for _, l := range xs {
		for _, x := range l {
			if x != "$LIB" {
				return false
			}
			n++
		}
	}
	return n > 0
}

// normDiff returns the shape's field and normalised diff for one
// comparator field difference.
func normDiff(field, g, x, card string) (string, string) {
	seat, sub, hasSeat := strings.Cut(field, ".")
	if !hasSeat {
		sub = field
	}
	switch {
	case hasSeat && (sub == "hand" || sub == "graveyard" || sub == "exile" || sub == "library_top"):
		var gl, xl []string
		for _, n := range parseList(g) {
			gl = append(gl, role(n, card))
		}
		for _, n := range parseList(x) {
			xl = append(xl, role(n, card))
		}
		og, ox := multisetDiff(gl, xl)
		if onlyFiller(og, ox) {
			return seat + ".library", "placement"
		}
		return field, "gorge+" + set(og) + " xmage+" + set(ox)
	case hasSeat && sub == "library_count":
		return seat + ".library", "placement"
	case hasSeat && sub == "life":
		gi, e1 := strconv.Atoi(g)
		xi, e2 := strconv.Atoi(x)
		if e1 == nil && e2 == nil {
			return field, "gorge-xmage=" + strconv.Itoa(gi-xi)
		}
	case field == "permanents":
		return field, permDiff(g, x, card)
	case field == "decisions" || field == "action":
		return field, harnessMsg(g, card)
	}
	return field, "gorge=" + roleText(g, card) + " xmage=" + roleText(x, card)
}

func roleText(s, card string) string {
	s = strings.ReplaceAll(s, card, "$CARD")
	return s
}

type perm struct {
	ctl, own, name, types, colors string
	attrs                         []string
}

var permRE = regexp.MustCompile(`^c(\d+) o(\d+) (.*?) \[(.*?)\] \{(.*?)\}(.*)$`)

// parsePerm reads one comparator permanent line (oraclediff.permKeys).
func parsePerm(line, card string) (perm, bool) {
	m := permRE.FindStringSubmatch(line)
	if m == nil {
		return perm{}, false
	}
	p := perm{ctl: m[1], own: m[2], name: role(m[3], card), types: m[4], colors: m[5]}
	rest := strings.TrimSpace(m[6])
	for rest != "" {
		var tok string
		if strings.HasPrefix(rest, "on=") {
			// The attached-to name runs to the next flag.
			end := len(rest)
			for _, f := range []string{" attacking", " blocking"} {
				if i := strings.Index(rest, f); i >= 0 && i < end {
					end = i
				}
			}
			tok, rest = "on="+role(rest[3:end], card), strings.TrimSpace(rest[end:])
		} else {
			tok, rest, _ = strings.Cut(rest, " ")
			rest = strings.TrimSpace(rest)
		}
		p.attrs = append(p.attrs, tok)
	}
	return p, true
}

func words(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

// permDiff pairs gorge-only and XMage-only permanent lines by controller and
// name role and renders, per pair, which characteristics differ; unpaired
// lines are whole extra permanents.
func permDiff(g, x, card string) string {
	split := func(s string) []perm {
		var out []perm
		if s == "" {
			return nil
		}
		for _, l := range strings.Split(s, "; ") {
			if p, ok := parsePerm(l, card); ok {
				out = append(out, p)
			} else {
				out = append(out, perm{name: role(l, card)})
			}
		}
		return out
	}
	gs, xs := split(g), split(x)
	used := make([]bool, len(xs))
	var parts []string
	for _, a := range gs {
		j := -1
		for k, b := range xs {
			if !used[k] && b.ctl == a.ctl && b.name == a.name {
				j = k
				break
			}
		}
		if j < 0 {
			parts = append(parts, "gorge+"+a.name)
			continue
		}
		used[j] = true
		b := xs[j]
		if a.own != b.own {
			parts = append(parts, fmt.Sprintf("%s owner gorge=%s xmage=%s", a.name, a.own, b.own))
		}
		if og, ox := multisetDiff(words(a.types), words(b.types)); len(og)+len(ox) > 0 {
			parts = append(parts, fmt.Sprintf("%s types gorge+%s xmage+%s", a.name, set(og), set(ox)))
		}
		if a.colors != b.colors {
			parts = append(parts, fmt.Sprintf("%s colors gorge=%s xmage=%s", a.name, a.colors, b.colors))
		}
		if og, ox := multisetDiff(a.attrs, b.attrs); len(og)+len(ox) > 0 {
			parts = append(parts, fmt.Sprintf("%s gorge+%s xmage+%s", a.name, set(og), set(ox)))
		}
	}
	for k, b := range xs {
		if !used[k] {
			parts = append(parts, "xmage+"+b.name)
		}
	}
	sort.Strings(parts)
	return strings.Join(uniq(parts), "; ")
}

// API is the card's IR API signature for a ruling's api filter: the API
// chain of its spell ability, or for a permanent without one, each
// trigger's mode and effect API. "-" when it has neither.
func API(f *cards.Face) string {
	var parts []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			parts = append(parts, s)
		}
	}
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" {
			continue
		}
		for s := sa; s != nil; s = s.Sub {
			add(s.API)
		}
		break
	}
	if len(parts) == 0 {
		for _, t := range f.Triggers {
			m := t.Mode
			if t.Effect != nil && t.Effect.API != "" {
				m += ":" + t.Effect.API
			}
			add(m)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "+")
}

// Marshal is json.Marshal without HTML escaping, so committed files read
// as the card names they hold.
func Marshal(v any) []byte {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return []byte(strings.TrimSuffix(b.String(), "\n"))
}

// Member is one row of a triage cluster file.
type Member struct {
	Card     string   `json:"card"`
	Template string   `json:"template"`
	Status   string   `json:"status"`
	API      string   `json:"api"`
	Sets     []string `json:"sets,omitempty"`
	Detail   string   `json:"detail"`
}

// Cluster is every unmatched row sharing one shape.
type Cluster struct {
	Shape   Shape
	Members []Member
}

// MarshalLines renders a cluster as its committed JSONL: a header line with
// the shape and size, then one member per line, sorted by card.
func (c Cluster) MarshalLines() []byte {
	sort.Slice(c.Members, func(i, j int) bool {
		if c.Members[i].Card != c.Members[j].Card {
			return c.Members[i].Card < c.Members[j].Card
		}
		return c.Members[i].Template < c.Members[j].Template
	})
	var b strings.Builder
	hdr := Marshal(struct {
		Shape Shape  `json:"shape"`
		Key   string `json:"key"`
		Cards int    `json:"cards"`
	}{c.Shape, c.Shape.Key(), len(c.Members)})
	b.Write(hdr)
	b.WriteByte('\n')
	for _, m := range c.Members {
		b.Write(Marshal(m))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
