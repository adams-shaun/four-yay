// Command ledger builds the judge-lane issue ledger the agent dashboard renders.
//
// The ledger is DERIVED, never hand-maintained, because a hand-copied list of
// open defects goes stale exactly the way AGENTS.md's own table header warns
// about: an entry is only as good as the thing it names still existing. Three
// sources, two of them measured:
//
//   - AGENTS.md's "Known approximations" table -> approved stand-ins. These are
//     dispositioned: someone looked at them and accepted them until a milestone.
//   - the opt-in conformance lane's -v output -> divergences. A FAIL leaf is an
//     UNFIXED defect with no approved row; that is the judge seat's deliverable.
//   - the orchestrator's tracked issue files (.ds4/issues/*.md) -> reports. A
//     human reported the defect and the pipeline is tracking it; the row's
//     disposition says where it stands. A report is NOT an engine measurement
//     and must never be merged with a divergence or an approximation.
//
// Usage:
//
//	scripts/ledger-lane.sh lane.txt   # cached, capped, under the heavy lock
//	go run ./cmd/ledger -lane lane.txt -out .ds4/ledger.json
//
// -lane may be repeated for lanes in more than one package. -issues defaults to
// <root>/.ds4/issues; a missing or empty dir is an empty source, not an error.
// With no -lane the ledger still carries the approximation rows, so the table
// is never blank.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Entry is one row of the ledger.
type Entry struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`   // "divergence" | "approximation" | "report"
	Status      string `json:"status"` // "open" | "closed" | "approved"
	Title       string `json:"title"`
	Where       string `json:"where"`
	Disposition string `json:"disposition"`
	// Priority orders the work: 1 is most urgent, defaultPriority when a file
	// says nothing. Only issue rows carry one; lane and approximation rows
	// leave it zero, which sorts ahead of nothing and is ignored by readers.
	Priority int `json:"priority,omitempty"`
	// DependsOn is every id this item declares a Depends-On line for.
	// BlockedBy is the subset still open (an unknown id blocks too -- a
	// dependency nobody can find is not a satisfied one). DependencyPath is
	// this item followed by the chain of still-open dependencies down to the
	// root blocker, so a reader sees WHY an item cannot start, not just that
	// it cannot. Empty on an unblocked item.
	DependsOn      []string `json:"depends_on,omitempty"`
	BlockedBy      []string `json:"blocked_by,omitempty"`
	DependencyPath []string `json:"dependency_path,omitempty"`
}

// Ledger is the whole document the dashboard reads.
type Ledger struct {
	Generated string         `json:"generated"`
	Commit    string         `json:"commit"`
	Counts    map[string]int `json:"counts"`
	Entries   []Entry        `json:"entries"`
}

func main() {
	var lanes multiFlag
	flag.Var(&lanes, "lane", "conformance lane `go test -v` output (repeatable)")
	out := flag.String("out", ".ds4/ledger.json", "where to write the ledger")
	root := flag.String("root", ".", "repo root")
	issues := flag.String("issues", "", "orchestrator tracked-issue `dir` (default <root>/.ds4/issues)")
	flag.Parse()

	docs, err := testDocs(*root)
	if err != nil {
		die(err)
	}
	if *issues == "" {
		*issues = filepath.Join(*root, ".ds4", "issues")
	}
	var entries []Entry
	seen := map[string]bool{}
	for _, f := range lanes {
		b, err := os.ReadFile(f)
		if err != nil {
			die(err)
		}
		for _, e := range laneEntries(string(b), docs) {
			if seen[e.ID] {
				continue
			}
			seen[e.ID] = true
			entries = append(entries, e)
		}
	}
	agents, err := os.ReadFile(filepath.Join(*root, "AGENTS.md"))
	if err != nil {
		die(err)
	}
	entries = append(entries, approximations(string(agents))...)
	reps, err := issueEntries(*issues, *root)
	if err != nil {
		die(err)
	}
	for _, e := range reps {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		entries = append(entries, e)
	}

	counts := map[string]int{}
	for _, e := range entries {
		counts[e.Status]++
	}
	l := Ledger{
		Generated: time.Now().UTC().Format(time.RFC3339),
		Commit:    gitHead(*root),
		Counts:    counts,
		Entries:   entries,
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		die(err)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		die(err)
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		die(err)
	}
	fmt.Printf("%s: %d entries (%d open, %d closed, %d approved)\n",
		*out, len(entries), counts["open"], counts["closed"], counts["approved"])
}

// issueEntries reads the orchestrator's tracked issue files: one Entry per
// top-level *.md of dir. It NEVER descends into the inbox/ subdir, which is
// the un-triaged drop zone (orchestrator/config.py INBOX_DIR); a ticket there
// becomes a top-level file at triage, so reading both would double-count one
// defect. A missing or empty dir is an empty source, not an error — a fresh
// worktree has no .ds4/issues at all.
//
// root is the repository root the ledger is built from: every Where must name
// its file RELATIVE to it (`.ds4/issues/<id>.md`), because the default issues
// dir is built from -root and an absolute -root or -issues must not leak an
// absolute path into the dashboard. A path outside the root, or one whose
// relativity cannot be computed (mixed absolute/relative inputs), falls back
// to the path as given.
func issueEntries(dir, root string) ([]Entry, error) {
	fis, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	// First pass: every issue's status and declared dependencies, so the second
	// pass can resolve a chain. A dependency path cannot be computed from one
	// file alone -- it needs the whole set.
	type issueFile struct {
		path string
		fm   map[string]string
		body string
	}
	var files []issueFile
	closed := map[string]bool{}
	known := map[string]bool{}
	deps := map[string][]string{}
	for _, fi := range fis {
		if fi.IsDir() || !strings.HasSuffix(fi.Name(), ".md") {
			continue
		}
		p := filepath.Join(dir, fi.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		fm := frontmatter(string(b))
		id := fm["id"]
		if id == "" {
			continue // a malformed file in a human drop zone is skipped, not fatal
		}
		files = append(files, issueFile{p, fm, string(b)})
		known[id] = true
		closed[id] = fm["status"] == "merged" || fm["status"] == "superseded"
		deps[id] = dependsOn(string(b))
	}

	// blockers returns the dependencies of id that are not closed. An id no
	// file declares counts as blocking: a dependency nobody can find is not a
	// satisfied one, and saying so is how a typo gets noticed.
	blockers := func(id string) []string {
		var out []string
		for _, d := range deps[id] {
			if !known[d] || !closed[d] {
				out = append(out, d)
			}
		}
		return out
	}

	var out []Entry
	for _, f := range files {
		p, fm := f.path, f.fm
		id := fm["id"]
		e := Entry{
			// The issue- prefix keeps a report out of the way of lane-test names
			// and approx-NN ids, so the caller's dedup map needs no change.
			ID:     "issue-" + id,
			Kind:   "report",
			Title:  strings.Join(strings.Fields(fm["title"]), " "),
			Where:  issueWhere(p, root),
			Status: "open",
		}
		e.Priority = defaultPriority
		if v := strings.TrimSpace(fm["priority"]); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				e.Priority = n
			}
		}
		e.DependsOn = deps[id]
		e.BlockedBy = blockers(id)
		if len(e.BlockedBy) > 0 {
			// Walk the first unmet blocker down to the root, stopping on a
			// repeat so a dependency cycle terminates instead of hanging.
			seen := map[string]bool{id: true}
			path := []string{id}
			for cur := e.BlockedBy[0]; ; {
				path = append(path, cur)
				if seen[cur] {
					break
				}
				seen[cur] = true
				next := blockers(cur)
				if len(next) == 0 {
					break
				}
				cur = next[0]
			}
			e.DependencyPath = path
		}
		// merged -> closed; every other value -> open. A human_needed defect is
		// still an UNFIXED defect — it needs a human, which is what the
		// disposition says — and the pipeline's vocabulary (new, briefed,
		// dispatched, review) is an active issue by orchestrator/issues.py's own
		// open_issues() rule. An unlisted status opens too, named honestly.
		switch fm["status"] {
		case "merged":
			e.Status = "closed"
			if c := fm["commits"]; c != "" {
				e.Disposition = "merged — " + c
			} else {
				e.Disposition = "merged"
			}
		case "superseded":
			// Reviewer-approved no-diff outcome: nothing of this issue's
			// own landed, so there is no commit to name.
			e.Status = "closed"
			e.Disposition = "superseded — no commits of its own"
		case "human_needed":
			e.Disposition = "needs human"
		case "new":
			e.Disposition = "reported — awaiting triage"
		case "briefed", "dispatched", "review":
			e.Disposition = fm["status"] + " — seat active"
		case "waiting":
			e.Disposition = "waiting — fix round queued for a seat slot"
		default:
			e.Disposition = "open — status " + fm["status"]
		}
		out = append(out, e)
	}
	return out, nil
}

// issueWhere names an issue file relative to the repo root.
func issueWhere(p, root string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(rel)
}

// frontmatter parses the key: value lines between a leading --- and its
// closing ---. Hand-rolled, stdlib only. An absent opening fence or an
// unterminated block is no frontmatter at all (an empty map), so such a file
// carries no id and the caller skips it.
func frontmatter(s string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return out
	}
	end := -1
	for i, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			end = i + 1
			break
		}
	}
	if end < 0 {
		return out
	}
	for _, l := range lines[1:end] {
		i := strings.Index(l, ":")
		if i < 0 {
			continue
		}
		out[strings.TrimSpace(l[:i])] = strings.TrimSpace(l[i+1:])
	}
	return out
}

// defaultPriority is what an issue file with no (or an unparseable) priority
// line gets: the middle of the 1..5 band, so an unprioritised item neither
// jumps the queue nor sinks below everything that was triaged.
const defaultPriority = 3

// dependsOnLine matches the Depends-On line the daemon already honours
// (orchestrator/daemon.py `_unmet_dependency`). It lives in the issue BODY --
// inbox tickets carry it in the report, triage copies it into the brief -- so
// it is parsed from the whole file, not from the frontmatter.
var dependsOnLine = regexp.MustCompile(`(?mi)^[ \t]*Depends-On:[ \t]*(.+)$`)

func dependsOn(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range dependsOnLine.FindAllStringSubmatch(body, -1) {
		for _, part := range strings.Split(m[1], ",") {
			id := strings.TrimSpace(part)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

var resultLine = regexp.MustCompile(`^\s*--- (PASS|FAIL|SKIP): (\S+)`)

// laneEntries turns one lane's -v output into ledger rows, counting LEAVES
// only. A `--- PASS|FAIL: name` entry is a leaf iff no other reported name has
// it as a "/" prefix: a parent test that fails only because a subtest failed is
// the same defect counted twice, and double-counting a lane is how a lane stops
// meaning anything.
func laneEntries(s string, docs map[string]doc) []Entry {
	type res struct{ status, name string }
	var all []res
	names := map[string]bool{}
	for _, line := range strings.Split(s, "\n") {
		if m := resultLine.FindStringSubmatch(line); m != nil {
			all = append(all, res{m[1], m[2]})
			names[m[2]] = true
		}
	}
	var out []Entry
	for _, r := range all {
		leaf := true
		for n := range names {
			if n != r.name && strings.HasPrefix(n, r.name+"/") {
				leaf = false
				break
			}
		}
		if !leaf || r.status == "SKIP" {
			continue
		}
		// A subtest's prose lives on the parent func; the subtest name is the
		// case, not a separate rule.
		fn := r.name
		if i := strings.Index(fn, "/"); i >= 0 {
			fn = fn[:i]
		}
		d := docs[fn]
		// The test NAME is the identity and the doc sentence is only support:
		// every subtest of one func shares that func's prose, so a table keyed
		// on the sentence shows three identical rows for three distinct cases.
		e := Entry{ID: r.name, Kind: "divergence", Title: d.title, Where: d.where}
		// Keep the disposition SHORT. The same 40-word paragraph repeated on
		// every open row is not information; the distinction between the three
		// statuses belongs in the section header, stated once.
		if r.status == "FAIL" {
			e.Status = "open"
			e.Disposition = "unfixed — no approved row"
		} else {
			e.Status = "closed"
			e.Disposition = "lane green — regression pin"
		}
		out = append(out, e)
	}
	return out
}

// approximations reads AGENTS.md's "Known approximations" table. Each row is an
// APPROVED stand-in with a named milestone, which is a different disposition
// from a lane failure and must never be merged with one on the page.
func approximations(s string) []Entry {
	lines := strings.Split(s, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "## Known approximations") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	var out []Entry
	n := 0
	for _, l := range lines[start+1:] {
		if strings.HasPrefix(l, "## ") {
			break
		}
		if !strings.HasPrefix(l, "| ") {
			continue
		}
		cells := splitRow(l)
		if len(cells) < 3 || cells[0] == "Stand-in" || strings.HasPrefix(cells[0], "---") {
			// A row with fewer than three cells is a MALFORMED table row, not a
			// row to quietly drop: it names no location and no milestone, which
			// is the one thing this table's own header forbids. Surface it.
			if strings.HasPrefix(l, "| ") && len(cells) < 3 && cells[0] != "Stand-in" &&
				!strings.HasPrefix(cells[0], "---") {
				n++
				out = append(out, Entry{
					ID:          fmt.Sprintf("approx-%02d", n),
					Kind:        "approximation",
					Status:      "open",
					Title:       cells[0],
					Where:       "AGENTS.md (malformed row)",
					Disposition: "malformed row — no Where, no milestone",
				})
			}
			continue
		}
		n++
		// Forge script fragments in the description carry their own "|"
		// separators, so the description is everything BUT the last two cells,
		// never cells[0]. Splitting naively put a script fragment in Where.
		title := strings.Join(cells[:len(cells)-2], " | ")
		out = append(out, Entry{
			ID:          fmt.Sprintf("approx-%02d", n),
			Kind:        "approximation",
			Status:      "approved",
			Title:       title,
			Where:       cells[len(cells)-2],
			Disposition: "approved — removed by " + cells[len(cells)-1],
		})
	}
	return out
}

func splitRow(l string) []string {
	l = strings.TrimSpace(l)
	l = strings.TrimPrefix(l, "|")
	l = strings.TrimSuffix(l, "|")
	parts := strings.Split(l, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

type doc struct{ title, where string }

// testDocs maps every Test func in the repo to its location and the first
// sentence of its doc comment. The doc comment is what the test's author said
// the rule IS, so it is a far better ledger title than the CamelCase name.
func testDocs(root string) (map[string]doc, error) {
	out := map[string]doc{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".worktrees", "node_modules", ".cards":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if perr != nil {
			return nil // a file that will not parse is not a reason to have no ledger
		}
		rel, _ := filepath.Rel(root, p)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			pos := fset.Position(fn.Pos())
			out[fn.Name.Name] = doc{
				title: firstSentence(fn.Doc.Text()),
				where: fmt.Sprintf("%s:%d", rel, pos.Line),
			}
		}
		return nil
	})
	return out, err
}

// firstSentence takes the lead sentence of a doc comment, dropping the Go
// convention's leading func name so the row reads as prose.
func firstSentence(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	// "TestFoo checks that ..." -> "checks that ..."
	if i := strings.Index(s, " "); i > 0 && strings.HasPrefix(s[:i], "Test") {
		s = s[i+1:]
	}
	for i, r := range s {
		if r == '.' && (i+1 == len(s) || s[i+1] == ' ') {
			return s[:i+1]
		}
	}
	if len(s) > 240 {
		return s[:240] + "…"
	}
	return s
}

func gitHead(root string) string {
	b, err := exec.Command("git", "-C", root, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func die(err error) {
	fmt.Fprintln(os.Stderr, "ledger:", err)
	os.Exit(1)
}
