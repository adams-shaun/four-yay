package adopt

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/shape"
)

// Ticket classes (section 11.3 C4): one ticket per class of finding, never
// one per card.
const (
	ClassPrimitive = "primitive" // an unsupported primitive (plus the primitives only its cards carry)
	ClassFix       = "fix"       // a gorge_wrong ruling or shape: an engine defect across its cards
	ClassShape     = "shape"     // an untriaged disagreement shape cluster (compliance/triage)
)

// Ticket is one would-be `agentctl issue add`: an id stable across runs (so
// re-filing is idempotent), a title, a priority and a complete brief.
type Ticket struct {
	ID         string   `json:"id"`
	Class      string   `json:"class"`
	Title      string   `json:"title"`
	Priority   int      `json:"priority"`
	Primitives []string `json:"primitives,omitempty"`
	Cards      []string `json:"cards"`
	Sets       []string `json:"sets"`
	Body       string   `json:"-"`
}

// TicketOptions scopes the primitive tickets.
type TicketOptions struct {
	// MinCards is the tournament cards (All target) a primitive must block
	// for a ticket; section 11.1's queue is the primitives blocking 10+.
	MinCards int
	// AnyIn names a format in which blocking a single card is enough (the
	// first target, so a new set's day-0 gaps are always filed).
	AnyIn string
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimSuffix(b.String(), "-")
	if len(out) > 60 {
		out = strings.TrimSuffix(out[:60], "-")
	}
	return out
}

// goIdent turns a primitive or shape into an exported identifier fragment.
func goIdent(s string) string {
	var b strings.Builder
	up := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			if up {
				r -= 'a' - 'A'
			}
			b.WriteRune(r)
			up = false
		case r >= 'A' && r <= 'Z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			up = false
		default:
			up = true
		}
	}
	out := b.String()
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

// Tickets builds every class ticket at this head: primitive groups from
// the impact table, fix tickets from gorge_wrong verdicts, and one shape
// ticket per committed triage cluster. root is the repo root. notes says
// what was left out and why (a ruling that already names its ticket).
func (cs *Census) Tickets(reg *cards.Registry, root string, opt TicketOptions) (ts []Ticket, notes []string, err error) {
	out := cs.primitiveTickets(reg, opt)
	verdicts, err := compliance.LoadVerdicts(filepath.Join(root, compliance.VerdictDir))
	if err != nil {
		return nil, nil, err
	}
	rulings, err := shape.LoadRulings(filepath.Join(root, shape.RulingDir))
	if err != nil {
		return nil, nil, err
	}
	fix, notes := cs.fixTickets(reg, verdicts, rulings)
	out = append(out, fix...)
	st, err := cs.shapeTickets(filepath.Join(root, shape.TriageDir))
	if err != nil {
		return nil, nil, err
	}
	out = append(out, st...)
	for i := range out {
		if out[i].Body, err = formatGoBlocks(out[i].Body); err != nil {
			return nil, nil, fmt.Errorf("%s: %v", out[i].ID, err)
		}
	}
	return out, notes, nil
}

// formatGoBlocks gofmts every ```go block of a brief, so the skeleton a
// seat copies is already formatted -- and refuses one that does not parse.
func formatGoBlocks(body string) (string, error) {
	const open, closing = "```go\n", "\n```"
	var b strings.Builder
	for {
		i := strings.Index(body, open)
		if i < 0 {
			b.WriteString(body)
			return b.String(), nil
		}
		j := strings.Index(body[i+len(open):], closing)
		if j < 0 {
			return "", fmt.Errorf("unterminated go block")
		}
		src := body[i+len(open) : i+len(open)+j]
		f, err := format.Source([]byte(src))
		if err != nil {
			return "", fmt.Errorf("skeleton does not parse: %v", err)
		}
		b.WriteString(body[:i+len(open)])
		b.WriteString(strings.TrimSuffix(string(f), "\n"))
		body = body[i+len(open)+j:]
	}
}

// primitiveGroup is a ticket's primitives: a leader and the primitives
// every card of which the leader also blocks (count:Teamwork rides with
// kw:Teamwork, kw:Nightbound with kw:Daybound) -- those can never unlock a
// card on their own, so a separate ticket would be a duplicate fix.
type primitiveGroup struct {
	lead    Impact
	members []string
	cards   map[string]bool
}

func (cs *Census) primitiveTickets(reg *cards.Registry, opt TicketOptions) []Ticket {
	rows := cs.Impact()
	anyIn, all := -1, len(cs.Config.Formats)-1
	for i, f := range cs.Config.Formats {
		if f.Name == opt.AnyIn {
			anyIn = i
		}
	}
	var groups []*primitiveGroup
	for _, r := range rows {
		if r.NonTournament {
			continue
		}
		var home *primitiveGroup
		for _, g := range groups {
			sub := true
			for _, c := range r.Cards {
				if !g.cards[c] {
					sub = false
					break
				}
			}
			if sub {
				home = g
				break
			}
		}
		if home != nil {
			home.members = append(home.members, r.Primitive)
			continue
		}
		g := &primitiveGroup{lead: r, members: []string{r.Primitive}, cards: map[string]bool{}}
		for _, c := range r.Cards {
			g.cards[c] = true
		}
		groups = append(groups, g)
	}
	var out []Ticket
	idx := newCarrierIndex(reg)
	for _, g := range groups {
		r := g.lead
		inAny := anyIn >= 0 && r.ByFormat[anyIn] > 0
		if r.ByFormat[all] < opt.MinCards && !inAny {
			continue
		}
		prio := 3
		if inAny {
			prio = 2
		}
		t := Ticket{
			ID: "compliance-prim-" + slug(r.Primitive), Class: ClassPrimitive, Priority: prio,
			Primitives: g.members, Cards: r.Cards, Sets: r.Sets,
		}
		t.Title = fmt.Sprintf("Implement %s: unblocks %d tournament cards (%s)", strings.Join(g.members, " + "), len(r.Cards), cs.formatCounts(r.ByFormat))
		t.Body = cs.primitiveBody(idx, t, r)
		out = append(out, t)
	}
	return out
}

func (cs *Census) formatCounts(n []int) string {
	var parts []string
	for i, f := range cs.Config.Formats {
		parts = append(parts, fmt.Sprintf("%s %d", f.Name, n[i]))
	}
	return strings.Join(parts, ", ")
}

// carrierIndex maps every primitive and value head to the corpus cards
// (front-face names) carrying it, built once per Tickets call.
type carrierIndex map[string][]string

func newCarrierIndex(reg *cards.Registry) carrierIndex {
	idx := carrierIndex{}
	for _, c := range reg.Cards {
		if len(c.Faces) == 0 || c.Faces[0] == nil || c.Faces[0].Name == "" {
			continue
		}
		seen := map[string]bool{}
		for _, p := range append(c.Primitives(), c.ValueHeads()...) {
			if !seen[p] {
				seen[p] = true
				idx[p] = append(idx[p], c.Faces[0].Name)
			}
		}
	}
	return idx
}

// carriers lists every corpus card carrying one of ps, sorted.
func (idx carrierIndex) carriers(ps []string) []string {
	set := map[string]bool{}
	for _, p := range ps {
		for _, n := range idx[p] {
			set[n] = true
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func quoteList(xs []string, indent string) string {
	var b strings.Builder
	line := indent
	for i, x := range xs {
		q := fmt.Sprintf("%q,", x)
		if len(line)+len(q)+1 > 100 && line != indent {
			b.WriteString(line + "\n")
			line = indent
		}
		if line != indent {
			line += " "
		}
		line += q
		_ = i
	}
	if line != indent {
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (cs *Census) primitiveBody(idx carrierIndex, t Ticket, r Impact) string {
	group := map[string]bool{}
	for _, p := range t.Primitives {
		group[p] = true
	}
	var unlock []string // cards whose every missing primitive is in the group
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", t.Title)
	fmt.Fprintf(&b, "Generated by `go run ./cmd/oraclediff tickets` (docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md section 11.3 C4) from the primitive impact table (`oraclediff impact`, C5). One ticket per primitive class, never per card: fix the primitive, not the card.\n\n")
	fmt.Fprintf(&b, "## Why\n\n")
	fmt.Fprintf(&b, "`%s` blocks %d tournament cards at this head: %s (of which it alone blocks %s).", r.Primitive, len(r.Cards), cs.formatCounts(r.ByFormat), cs.formatCounts(r.SoleByFormat))
	if len(t.Primitives) > 1 {
		fmt.Fprintf(&b, " It carries %s with it: every card those block, `%s` blocks too, so they cannot unlock a card on their own and land in this ticket rather than a duplicate one.", "`"+strings.Join(t.Primitives[1:], "`, `")+"`", r.Primitive)
	}
	if len(r.SetsUnlocked) > 0 {
		fmt.Fprintf(&b, " Implementing it leaves nothing unsupported in %s.", strings.Join(r.SetsUnlocked, ", "))
	}
	fmt.Fprintf(&b, "\n\n## Cards (%d) and the sets that print them\n\n", len(r.Cards))
	for _, n := range r.Cards {
		c := cs.byName[n]
		var other []string
		for _, p := range c.Missing {
			if !group[p] {
				other = append(other, p)
			}
		}
		line := fmt.Sprintf("- %s -- %s", n, clip(c.Sets, 6))
		if len(other) > 0 {
			line += " (also needs " + strings.Join(other, ", ") + ")"
		} else {
			unlock = append(unlock, n)
		}
		fmt.Fprintln(&b, line)
	}
	if len(r.NonTournamentCards) > 0 {
		fmt.Fprintf(&b, "\nNon-tournament cards carrying it (out of scope): %s.\n", clip(r.NonTournamentCards, 8))
	}
	car := idx.carriers(t.Primitives)
	name := "TestClass" + goIdent(r.Primitive) + "Census"
	fmt.Fprintf(&b, "\n## Class-census ratchet\n\nAdd `rules/class_%s_census_test.go` (the model is `rules/fra_gap2_census_test.go`). It walks the WHOLE corpus, not the cards above, so a carrier this list misses still fails it; the carrier count is pinned so a corpus pin bump that adds one is re-censused. Measured at generation: %d corpus carriers.\n\n", strings.ReplaceAll(slug(r.Primitive), "-", "_"), len(car))
	fmt.Fprintf(&b, "```go\npackage rules\n\nimport (\n\t\"sort\"\n\t\"strings\"\n\t\"testing\"\n\n\t\"github.com/adams-shaun/gorge/effects\"\n\t\"github.com/adams-shaun/gorge/internal/testutil\"\n)\n\n")
	fmt.Fprintf(&b, "// %s walks the whole corpus for every carrier of %s\n// and asserts the class is registered, no carrier is still blocked by it,\n// and every card whose only gap was the class is now fully supported.\n//\n// TODO(ticket): assert the class's parameters are read (measureParamCensus /\n// cardCensusLabels, as TestRealityFractureGap2ClassCensus does), and add one\n// behaviour test per distinct shape the census finds.\n", name, strings.Join(t.Primitives, ", "))
	fmt.Fprintf(&b, "func %s(t *testing.T) {\n\tt.Parallel()\n\tclass := map[string]bool{\n%s\t}\n\tsup := effects.Supported()\n\tfor p := range class {\n\t\tif !sup[p] {\n\t\t\tt.Errorf(\"%%s is not registered as supported\", p)\n\t\t}\n\t}\n", name, quoteKeys(t.Primitives, "\t\t"))
	fmt.Fprintf(&b, "\treg := testutil.CorpusRegistry(t)\n\tvar carriers, blocked []string\n\tfor _, c := range reg.Cards {\n\t\tif len(c.Faces) == 0 || c.Faces[0] == nil || c.Faces[0].Name == \"\" {\n\t\t\tcontinue\n\t\t}\n\t\tcarries := false\n\t\tfor _, p := range append(c.Primitives(), c.ValueHeads()...) {\n\t\t\tcarries = carries || class[p]\n\t\t}\n\t\tif !carries {\n\t\t\tcontinue\n\t\t}\n\t\tcarriers = append(carriers, c.Faces[0].Name)\n\t\tfor _, p := range reg.Unsupported(c, sup) {\n\t\t\tif class[p] {\n\t\t\t\tblocked = append(blocked, c.Faces[0].Name+\": \"+p)\n\t\t\t}\n\t\t}\n\t}\n\tsort.Strings(blocked)\n\tif len(blocked) > 0 {\n\t\tt.Errorf(\"class carriers still blocked by the class:\\n  %%s\", strings.Join(blocked, \"\\n  \"))\n\t}\n")
	fmt.Fprintf(&b, "\tif len(carriers) != %d {\n\t\tt.Errorf(\"carrier count moved (re-census the new shapes): %%d, want %d\", len(carriers))\n\t}\n", len(car), len(car))
	if len(unlock) > 0 {
		fmt.Fprintf(&b, "\t// The tournament cards whose only gap was this class.\n\tfor _, n := range []string{\n%s\t} {\n\t\tc, ok := reg.Lookup(n)\n\t\tif !ok {\n\t\t\tt.Errorf(\"%%s: not in the corpus\", n)\n\t\t\tcontinue\n\t\t}\n\t\tif u := reg.Unsupported(c, sup); len(u) > 0 {\n\t\t\tt.Errorf(\"%%s: unsupported %%v\", n, u)\n\t\t}\n\t}\n", quoteList(unlock, "\t\t"))
	}
	fmt.Fprintf(&b, "\tt.Logf(\"census: %%d carriers of the class\", len(carriers))\n}\n```\n\n")
	fmt.Fprintf(&b, "## Out of scope\n\nThe other primitives named under \"also needs\" (each has its own ticket), and level-B scenarios.\n\n")
	fmt.Fprintf(&b, "## Done means\n\n`go test ./rules -run %s` is green and the unlock list in it passes; `go run ./cmd/oraclediff impact` no longer lists %s; `go test ./rules/ -run 'TestEveryRepoDeck|TestHeads$'` and `go test ./compliance/...` are green (update the ratchet tables only for the entries this change moves); `make compliance-pass SETS=\"%s\"` is run for the Standard sets it touches, if any, so their new scenarios get verdicts.\n", name, "`"+strings.Join(t.Primitives, "`, `")+"`", strings.Join(standardOf(cs, r.Sets), " "))
	return b.String()
}

func quoteKeys(xs []string, indent string) string {
	var b strings.Builder
	for _, x := range xs {
		fmt.Fprintf(&b, "%s%q: true,\n", indent, x)
	}
	return b.String()
}

// standardOf filters sets to those in the first target format.
func standardOf(cs *Census, sets []string) []string {
	first := cs.Config.Formats[0].Name
	var out []string
	for _, s := range sets {
		if contains(cs.SetFormats[s], first) {
			out = append(out, s)
		}
	}
	return out
}

// fixTickets groups the gorge_wrong verdicts: one ticket per shape ruling
// (compliance/rulings/<id>.json with status gorge_wrong or harness), and
// one per disagreement shape for hand-ruled rows.
func (cs *Census) fixTickets(reg *cards.Registry, verdicts map[string]map[string]compliance.VerdictRow, rulings []shape.Ruling) ([]Ticket, []string) {
	byID := map[string]shape.Ruling{}
	for _, r := range rulings {
		byID[r.ID] = r
	}
	type group struct {
		key, ruling string
		rows        []compliance.VerdictRow
		ruled       *shape.Ruling
	}
	groups := map[string]*group{}
	var cardsSorted []string
	for c := range verdicts {
		cardsSorted = append(cardsSorted, c)
	}
	sort.Strings(cardsSorted)
	for _, card := range cardsSorted {
		var tmpls []string
		for t := range verdicts[card] {
			tmpls = append(tmpls, t)
		}
		sort.Strings(tmpls)
		for _, tm := range tmpls {
			r := verdicts[card][tm]
			harnessRuled := r.Status == compliance.StatusHarness && r.RulingID != ""
			if r.Status != compliance.StatusGorgeWrong && !harnessRuled {
				continue
			}
			var key string
			var ru *shape.Ruling
			switch {
			case r.RulingID != "":
				key = "ruling:" + r.RulingID
				if x, ok := byID[r.RulingID]; ok {
					ru = &x
				}
			default:
				if s, ok := shape.Of(r); ok {
					key = "shape:" + s.Key()
				} else {
					key = "card:" + card
				}
			}
			g := groups[key]
			if g == nil {
				g = &group{key: key, ruling: r.Ruling, ruled: ru}
				groups[key] = g
			}
			g.rows = append(g.rows, r)
		}
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []Ticket
	var notes []string
	for _, k := range keys {
		g := groups[k]
		if g.ruled != nil && g.ruled.Ticket != "" {
			notes = append(notes, fmt.Sprintf("fix %s (%d cards): ruling already names its ticket: %s", g.ruled.ID, len(g.rows), g.ruled.Ticket))
			continue
		}
		id := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(k, "ruling:"), "shape:"), "card:")
		if strings.HasPrefix(k, "shape:") {
			id = shapeOfRow(g.rows[0]).Slug()
		}
		t := Ticket{ID: "compliance-fix-" + slug(id), Class: ClassFix, Priority: 1}
		setSeen := map[string]bool{}
		for _, r := range g.rows {
			t.Cards = append(t.Cards, r.Card)
			if c, ok := cs.byName[r.Card]; ok {
				for _, s := range c.Sets {
					setSeen[s] = true
				}
			}
		}
		for s := range setSeen {
			t.Sets = append(t.Sets, s)
		}
		sort.Strings(t.Sets)
		what := id
		if g.ruled != nil {
			what = g.ruled.ID
		}
		t.Title = fmt.Sprintf("Fix gorge: %s (%d cards wrong per the XMage oracle)", what, len(g.rows))
		t.Body = cs.fixBody(reg, t, g.key, g.ruling, g.ruled, g.rows)
		out = append(out, t)
	}
	return out, notes
}

func shapeOfRow(r compliance.VerdictRow) shape.Shape {
	s, _ := shape.Of(r)
	return s
}

func (cs *Census) fixBody(reg *cards.Registry, t Ticket, key, ruling string, ru *shape.Ruling, rows []compliance.VerdictRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", t.Title)
	fmt.Fprintf(&b, "Generated by `go run ./cmd/oraclediff tickets` (section 11.3 C4). The XMage oracle and a triage ruling agree that gorge is wrong on every card below; they share one root cause, so this is one fix, not %d.\n\n", len(rows))
	fmt.Fprintf(&b, "## The ruling\n\n")
	if ru != nil {
		fmt.Fprintf(&b, "`compliance/rulings/%s.json` (status %s): %s", ru.ID, ru.Status, ru.Ruling)
		if len(ru.CR) > 0 {
			fmt.Fprintf(&b, " (CR %s)", strings.Join(ru.CR, ", "))
		}
		fmt.Fprintf(&b, "\n\nMatch: template `%s`, op `%s`, field `%s`, diff `%s`.\n\n", ru.Match.Template, ru.Match.Op, ru.Match.Field, ru.Match.Diff)
	} else {
		fmt.Fprintf(&b, "%s\n\nShape: `%s`.\n\n", ruling, strings.TrimPrefix(key, "shape:"))
	}
	apis := map[string]bool{}
	fmt.Fprintf(&b, "## Cards (%d)\n\n", len(rows))
	for _, r := range rows {
		api := "-"
		if c, ok := reg.Lookup(r.Card); ok && len(c.Faces) > 0 {
			api = shape.API(c.Faces[0])
		}
		apis[api] = true
		sets := ""
		if c, ok := cs.byName[r.Card]; ok {
			sets = clip(c.Sets, 6)
		}
		fmt.Fprintf(&b, "- %s (%s; api %s; %s): %s\n", r.Card, r.Template, api, sets, firstLine(r.Detail, 200))
	}
	var apiList []string
	for a := range apis {
		apiList = append(apiList, a)
	}
	sort.Strings(apiList)
	id := "rule"
	if ru != nil {
		id = ru.ID
	}
	name := "TestFix" + goIdent(id) + "Census"
	fmt.Fprintf(&b, "\n## Class-census ratchet\n\nAdd `compliance/shape/fix_%s_test.go` (modelled on `rules/fra_gap2_census_test.go`): every committed verdict row of the class must have left gorge_wrong, which happens when `make compliance-pass` re-runs it after the fix and it agrees.\n\n", strings.ReplaceAll(slug(id), "-", "_"))
	fmt.Fprintf(&b, "```go\npackage shape\n\nimport (\n\t\"path/filepath\"\n\t\"testing\"\n\n\t\"github.com/adams-shaun/gorge/compliance\"\n)\n\n")
	fmt.Fprintf(&b, "// %s: no committed verdict of the class is still\n// gorge_wrong. Measured at generation: %d rows.\n//\n// TODO(ticket): add the engine-side census for the root cause -- walk the corpus\n// for every carrier (API signatures seen: %s) as\n// TestRealityFractureGap2ClassCensus does, and pin the carrier count.\n", name, len(rows), strings.Join(apiList, ", "))
	fmt.Fprintf(&b, "func %s(t *testing.T) {\n\tall, err := compliance.LoadVerdicts(filepath.Join(\"..\", \"..\", compliance.VerdictDir))\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\tclass := map[string]bool{\n%s\t}\n\tfor card, rows := range all {\n\t\tfor _, r := range rows {\n\t\t\tif class[card] && r.Status == compliance.StatusGorgeWrong {\n\t\t\t\tt.Errorf(\"%%s (%%s): still gorge_wrong: %%s\", card, r.Template, r.Detail)\n\t\t\t}\n\t\t}\n\t}\n}\n```\n\n", name, quoteKeys(t.Cards, "\t\t"))
	fmt.Fprintf(&b, "## Out of scope\n\nAny card-specific workaround: fix the primitive the ruling names, for every card it covers.\n\n")
	fmt.Fprintf(&b, "## Done means\n\n`go test ./compliance/shape -run %s` is green after `make compliance-pass SETS=\"%s\"` re-runs the class and every row agrees; `go test ./rules/ -run 'TestOracleAudit|TestHeads$'` and `go test ./compliance/...` are green.\n", name, strings.Join(standardOf(cs, t.Sets), " "))
	return b.String()
}

// shapeTickets turns every committed triage cluster of two or more cards
// into one ticket. Single-card clusters are one triage batch per
// (template, op, field): a ticket per singleton would be the per-card
// ticket this generator exists to retire.
func (cs *Census) shapeTickets(dir string) ([]Ticket, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []Ticket
	type batch struct {
		shapes  []shape.Shape
		stems   []string
		members [][]shape.Member
	}
	batches := map[string]*batch{}
	var batchKeys []string
	for _, p := range paths {
		if filepath.Base(p) == shape.ReviewFile {
			continue
		}
		hdr, members, err := readCluster(p)
		if err != nil {
			return nil, err
		}
		stem := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		if len(members) < 2 {
			k := hdr.Shape.Template + "/" + hdr.Shape.Op + "/" + hdr.Shape.Field
			bt := batches[k]
			if bt == nil {
				bt = &batch{}
				batches[k] = bt
				batchKeys = append(batchKeys, k)
			}
			bt.shapes = append(bt.shapes, hdr.Shape)
			bt.stems = append(bt.stems, stem)
			bt.members = append(bt.members, members)
			continue
		}
		t := shapeTicket("compliance-shape-"+slug(stem), hdr.Shape.Op, [][]shape.Member{members})
		kind := "Triage"
		if hdr.Shape.Op == "harness" {
			kind = "Fix the harness for"
		}
		t.Title = fmt.Sprintf("%s disagreement shape %s/%s/%s: %d cards (%s)", kind, hdr.Shape.Template, hdr.Shape.Op, hdr.Shape.Field, len(members), firstLine(hdr.Shape.Diff, 70))
		t.Body = cs.shapeBody(t, []string{stem}, []shape.Shape{hdr.Shape}, [][]shape.Member{members})
		out = append(out, t)
	}
	sort.Strings(batchKeys)
	for _, k := range batchKeys {
		bt := batches[k]
		t := shapeTicket("compliance-shapes-"+slug(k), bt.shapes[0].Op, bt.members)
		if len(bt.shapes) == 1 {
			t.ID = "compliance-shape-" + slug(bt.stems[0])
			t.Title = fmt.Sprintf("Triage disagreement shape %s: 1 card (%s)", k, firstLine(bt.shapes[0].Diff, 70))
		} else {
			kind := "Triage"
			if bt.shapes[0].Op == "harness" {
				kind = "Fix the harness for"
			}
			t.Title = fmt.Sprintf("%s %d single-card disagreement shapes in %s", kind, len(bt.shapes), k)
		}
		t.Body = cs.shapeBody(t, bt.stems, bt.shapes, bt.members)
		out = append(out, t)
	}
	return out, nil
}

func shapeTicket(id, op string, members [][]shape.Member) Ticket {
	t := Ticket{ID: id, Class: ClassShape, Priority: 2}
	if op == "harness" {
		t.Priority = 3
	}
	setSeen := map[string]bool{}
	for _, ms := range members {
		for _, m := range ms {
			t.Cards = append(t.Cards, m.Card)
			for _, s := range m.Sets {
				setSeen[s] = true
			}
		}
	}
	for s := range setSeen {
		t.Sets = append(t.Sets, s)
	}
	sort.Strings(t.Sets)
	sort.Strings(t.Cards)
	return t
}

type clusterHeader struct {
	Shape shape.Shape `json:"shape"`
	Key   string      `json:"key"`
	Cards int         `json:"cards"`
}

func readCluster(p string) (clusterHeader, []shape.Member, error) {
	f, err := os.Open(p)
	if err != nil {
		return clusterHeader{}, nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var hdr clusterHeader
	var ms []shape.Member
	first := true
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		if first {
			if err := json.Unmarshal(sc.Bytes(), &hdr); err != nil {
				return hdr, nil, fmt.Errorf("%s: header: %v", p, err)
			}
			first = false
			continue
		}
		var m shape.Member
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			return hdr, nil, fmt.Errorf("%s: %v", p, err)
		}
		ms = append(ms, m)
	}
	return hdr, ms, sc.Err()
}

func (cs *Census) shapeBody(t Ticket, stems []string, shapes []shape.Shape, members [][]shape.Member) string {
	var b strings.Builder
	n := len(t.Cards)
	fmt.Fprintf(&b, "# %s\n\n", t.Title)
	if len(shapes) == 1 {
		fmt.Fprintf(&b, "Generated by `go run ./cmd/oraclediff tickets` (section 11.3 C4) from `compliance/triage/%s.jsonl`. Every card below disagrees with XMage in the same shape, so it is one triage item and at most one fix -- not %d.\n\n", stems[0], n)
	} else {
		fmt.Fprintf(&b, "Generated by `go run ./cmd/oraclediff tickets` (section 11.3 C4): %d single-card shape clusters under `compliance/triage/` that share a template, op and field. Each is its own triage item, but they are batched so a single card never becomes a ticket; look for a shared cause first -- two of them with one cause mean the shape normalisation (compliance/shape) is too fine.\n\n", len(shapes))
	}
	for i, s := range shapes {
		ms := members[i]
		fmt.Fprintf(&b, "## Shape `%s`\n\n- template `%s`, op `%s`, field `%s`\n- normalised diff: `%s`\n\n", stems[i], s.Template, s.Op, s.Field, s.Diff)
		for _, m := range ms {
			fmt.Fprintf(&b, "- %s (%s; api %s; %s): %s\n", m.Card, m.Template, m.API, clip(m.Sets, 6), firstLine(m.Detail, 200))
		}
		b.WriteString("\n")
	}
	example := members[0][0].Card
	fmt.Fprintf(&b, "## Goal\n\nDecide who is wrong from the Oracle text and the CR (never from the other engine). Then, once per shape:\n\n")
	if shapes[0].Op == "harness" {
		fmt.Fprintf(&b, "- a scenario-template or driver gap: fix the generator (`compliance/oraclegen/templates`) or the XMage driver so the scenario is expressible, or record a harness shape ruling (`oraclediff rule -card \"%s\" -status harness -ruling ... -shape-id <id>`);\n", example)
	} else {
		fmt.Fprintf(&b, "- xmage wrong: `oraclediff rule -card \"%s\" -status xmage_wrong -ruling \"<who and why, CR cite>\" -shape-id <id>`, then `oraclediff triage -apply`;\n- gorge wrong: the same with `-status gorge_wrong`, which turns the shape into a `compliance-fix-<id>` ticket on the next `oraclediff tickets` (or fix it here if it is small);\n", example)
	}
	fmt.Fprintf(&b, "- if a cluster is really two causes, say so and split the shape (compliance/shape normalisation) rather than ruling card by card.\n\n")
	name := "TestShape" + goIdent(t.ID) + "Closed"
	var keys []string
	for _, s := range shapes {
		keys = append(keys, s.Key())
	}
	fmt.Fprintf(&b, "## Class-census ratchet\n\nAdd `compliance/shape/%s_test.go`: no committed verdict row may still carry one of these untriaged shapes.\n\n", strings.ReplaceAll(slug(t.ID), "-", "_"))
	fmt.Fprintf(&b, "```go\npackage shape\n\nimport (\n\t\"path/filepath\"\n\t\"testing\"\n\n\t\"github.com/adams-shaun/gorge/compliance\"\n)\n\n")
	fmt.Fprintf(&b, "// %s: the shapes below have no untriaged row left.\n// Measured at generation: %d rows.\nfunc %s(t *testing.T) {\n\tclass := map[string]bool{\n%s\t}\n\tall, err := compliance.LoadVerdicts(filepath.Join(\"..\", \"..\", compliance.VerdictDir))\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\tfor card, rows := range all {\n\t\tfor _, r := range rows {\n\t\t\tuntriaged := r.Status == compliance.StatusDiverge || (r.Status == compliance.StatusHarness && r.Ruling == \"\")\n\t\t\tif s, ok := Of(r); ok && untriaged && class[s.Key()] {\n\t\t\t\tt.Errorf(\"%%s (%%s): still untriaged: %%s\", card, r.Template, s.Key())\n\t\t\t}\n\t\t}\n\t}\n}\n```\n\n", name, n, name, quoteKeys(keys, "\t\t"))
	fmt.Fprintf(&b, "## Out of scope\n\nOther shapes (each has its own ticket) and per-card rulings.\n\n")
	fmt.Fprintf(&b, "## Done means\n\n`go run ./cmd/oraclediff triage` no longer lists the shape(s), `go test ./compliance/shape -run %s` is green, and `go test ./compliance/... ./rules -run 'TestOracleAudit|TestHeads$'` is green.\n", name)
	return b.String()
}

func firstLine(s string, n int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > n {
		s = s[:n] + "..."
	}
	return s
}
