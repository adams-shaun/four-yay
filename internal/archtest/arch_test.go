package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const module = "github.com/adams-shaun/gorge"

type pkg struct {
	path    string
	imports map[string]bool // direct, non-test
	deps    map[string]bool // transitive, non-test
}

// packages lists every package in the module with its direct and transitive
// non-test imports. Test files are excluded on purpose: tests may import
// anything (view's tests import rules; cards' tests import time).
func packages(t *testing.T) map[string]pkg {
	t.Helper()
	out, err := exec.Command("go", "list", "-f", `{{.ImportPath}}|{{join .Imports " "}}|{{join .Deps " "}}`, module+"/...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	pkgs := map[string]pkg{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			t.Fatalf("unexpected go list line %q", line)
		}
		p := pkg{path: parts[0], imports: set(parts[1]), deps: set(parts[2])}
		pkgs[p.path] = p
	}
	if len(pkgs) < 10 {
		t.Fatalf("go list found only %d packages", len(pkgs))
	}
	return pkgs
}

func set(s string) map[string]bool {
	m := map[string]bool{}
	for _, f := range strings.Fields(s) {
		m[f] = true
	}
	return m
}

// TestTimeIsImportedOnlyByTheHost is spec D16: the host's injected sleep,
// the SSE writer's ticker/keep-alive and gorged's shutdown timeout are the
// only clocks in the system. Every other package must be a pure function
// of its inputs.
//
// cmd/botbench is exempt because it is a build-time developer tool, not part
// of the engine or the server: its grind mode reads the wall clock only to
// bound its own run window (deadline) and report per-deck elapsed time — how long the BENCH chose to run, not anything a game,
// event, view or replay depends on. cmd/ledger is exempt likewise: it stamps
// the ledger document's Generated: field for the dashboard, a docs tool
// output, never engine state.
// cmd/searchprobe is another diagnostic tool: it measures corpus load, total
// elapsed time and per-root sampling/search cost. It injects a clock into the
// experimental harness only for returned metrics; fixed work counts, explicit
// seeds and ordinary engine execution govern every proposal and action.
// cmd/searchteacher (the 2026-09-19 search-teacher spike) is exempt on the
// same terms: it reads the clock only to report per-decision sampling and
// search milliseconds; no proposal, rollout, label or game reads it.
// cmd/cardfuzz reads it only for its per-game hang watchdog and its
// games/s progress line; every deck and game is a pure function of its seed.
// cmd/exitloop (the L10 expert-iteration loop) reads it only to report each
// stage's wall seconds in timing.tsv; the stages are separate processes and no
// label, checkpoint, game or summary.tsv byte reads it.
// cmd/traindash (the live read-only training dashboard) reads it only to
// stamp Generated:/last-modified fields and to decide a run's live/stale
// status from file mtimes; it scans a training output tree and never drives
// the engine, so no game, event, view or replay depends on its clock.
// cmd/hindsight (the pn20 branch-mining tool) reads it only to report run and
// per-decision wall seconds; every sample, rollout and record is a pure
// function of its seeds. The paymirror A/B harness reads the clock only in
// its cmd/paymirror CLI: the per-game wall-time budget is injected to the
// library as a paymirror.DriverOptions.BudgetExceeded predicate the CLI
// builds from time.Now, and the CLI's progress line prints per-game elapsed
// seconds. The internal/paymirror library itself imports no time and is a
// pure function of its seeds and the corpus for a completed game; every
// spec, game, report and trace byte is either that pure result or a game the
// CLI's own budget truncated (Err "truncated: budget"), never something the
// library's clock changed; elapsed time cannot affect engine choices, events,
// replays or verdicts.
// cmd/sbv1agent (the SpellBench v1 agent) reads it only to report each
// shadow-policy decision's wall milliseconds in its -stats line; every
// answer is a pure function of the decision stream and its seed.
// cmd/kshadowcheck (the kernel-shadow fidelity tool) reads it only to report
// staging cost; it plays no game.
// cmd/sbagent (the SpellBench v2 agent) reads it to report each shadow
// policy decision's wall milliseconds in its -stats line and for the
// search's per-decision clock guard (counted when it fires); cmd/sbv2local
// (the in-process v2 runner) only reports latency. cmd/searchbench reads it
// only at the command boundary: a run's per-item CoreSeconds and progress
// lines, a build's stage seconds (build.json, never the manifest); every
// answer and every manifest byte is a pure function of the items, seeds and
// corpus. cmd/enginebench (the docs/015 engine-speed rows) reads it only to
// report each row's elapsed wall time beside its CPU time; every game and
// seed is fixed by its flags. host/manabrewhttp uses time only for SSE
// keep-alive and write deadlines. No game, event, view or
// replay reads the clock, and intents reach the engine only through
// SubmitIntent.
func TestTimeIsImportedOnlyByTheHost(t *testing.T) {
	allowed := map[string]bool{
		module + "/host":              true,
		module + "/host/httpapi":      true,
		module + "/cmd/gorged":        true,
		module + "/cmd/botbench":      true,
		module + "/cmd/ledger":        true,
		module + "/cmd/searchprobe":   true,
		module + "/cmd/searchteacher": true,
		module + "/cmd/cardfuzz":      true,
		module + "/cmd/exitloop":      true,
		module + "/cmd/traindash":     true,
		module + "/cmd/hindsight":     true,
		module + "/cmd/paymirror":     true,
		module + "/cmd/sbv1agent":     true,
		module + "/cmd/kshadowcheck":  true,
		module + "/cmd/sbagent":       true,
		module + "/cmd/sbv2local":     true,
		module + "/cmd/searchbench":   true,
		module + "/cmd/enginebench":   true,
		module + "/cmd/dzgorge":       true,
		module + "/internal/gamepool": true,
		module + "/internal/broker":   true,
		module + "/host/manabrewhttp": true,
	}
	for path, p := range packages(t) {
		if p.imports["time"] && !allowed[path] {
			t.Errorf("%s imports time; only host, host/httpapi and cmd/gorged may", path)
		}
	}
}

// TestDependencyOrderHolds pins the arrows that must never appear, direct or
// transitive.
func TestDependencyOrderHolds(t *testing.T) {
	pkgs := packages(t)
	forbidden := []struct{ from, to string }{
		{module + "/effects", module + "/rules"},
		{module + "/view", module + "/rules"},
		{module + "/botpolicy", module + "/view"},
		{module + "/botpolicy", module + "/rules"},
		{module + "/botpolicy", module + "/seat"},
		{module + "/protocol", module + "/rules"},
		{module + "/protocol", module + "/bots"},
		{module + "/host", module + "/internal/testutil"},
		{module + "/host/httpapi", module + "/internal/testutil"},
		{module + "/cmd/gorged", module + "/internal/testutil"},
		{module + "/cards", module + "/state"},
		{module + "/deck", module + "/rules"},
		// The az search seat's clairvoyant world clones the REAL engine,
		// hidden zones and future chance included (spec 2026-09-27 §1). It
		// lives in internal/azmcts/clairvoyant and is bench and training only
		// (botbench injects it as azmcts.SeatConfig.Source), so nothing that
		// seats a non-bench opponent may link it. The honest azmcts core
		// never pulls the clone back in, and neither do the hosted search
		// packages or any bots/ entry.
		{module + "/host", module + "/internal/azmcts/clairvoyant"},
		{module + "/host/httpapi", module + "/internal/azmcts/clairvoyant"},
		{module + "/cmd/gorged", module + "/internal/azmcts/clairvoyant"},
		{module + "/internal/azmcts", module + "/internal/azmcts/clairvoyant"},
		{module + "/internal/spellbench/sbsearch", module + "/internal/azmcts/clairvoyant"},
		{module + "/internal/searchseat", module + "/internal/azmcts/clairvoyant"},
		// The lasagna layering (spec 2026-10-03 §3): rules/cost is the L2
		// cost vocabulary leaf. It sits below effects and below every rules
		// subsystem package, so none of them -- and never rules itself -- may
		// be reachable from it. TestCostVocabularyIsALeaf pins its whole
		// direct import set; these rows name the edges the layering forbids.
		{module + "/rules/cost", module + "/rules"},
		{module + "/rules/cost", module + "/effects"},
		{module + "/rules/cost", module + "/events"},
		{module + "/rules/cost", module + "/decision"},
		{module + "/rules/cost", module + "/botpolicy"},
		{module + "/rules/cost", module + "/rules/pay"},
		{module + "/rules/cost", module + "/rules/chars"},
		{module + "/rules/cost", module + "/rules/trigmatch"},
		{module + "/rules/cost", module + "/rules/combat"},
		{module + "/rules/cost", module + "/rules/resolve"},
		// rules/combat is an L5 subsystem package (W5 step E5): attack and
		// block legality behind combat.Board. It never reaches back into
		// rules (Go forbids the direct cycle once rules imports it; these
		// rows also forbid the transitive one), never into a sibling
		// subsystem package or the resolution kernel above it, and never
		// into the bot layer. (decision is reachable through effects, so it
		// is not a transitive row; TestCombatImportsStayBelowRules pins the
		// direct import set, which keeps decision -- and with it the asks
		// and validators of the declaration flow -- out of the package.)
		{module + "/rules/combat", module + "/rules"},
		{module + "/rules/combat", module + "/rules/pay"},
		{module + "/rules/combat", module + "/rules/trigmatch"},
		{module + "/rules/combat", module + "/rules/resolve"},
		{module + "/rules/combat", module + "/botpolicy"},
		// rules/trigmatch (W5 E3) is an L5 subsystem package: the trigger
		// matchers read the engine through trigmatch.Board, never through
		// *rules.Engine, so it may not reach rules, its sibling L5 packages
		// (pay, combat) or the L6 resolution kernel. It may sit on rules/chars
		// (L4) and rules/cost (L2). TestTrigmatchImportsStayBelowL5 pins its
		// direct import set.
		{module + "/rules/trigmatch", module + "/rules"},
		{module + "/rules/trigmatch", module + "/rules/pay"},
		{module + "/rules/trigmatch", module + "/rules/combat"},
		{module + "/rules/trigmatch", module + "/rules/resolve"},
		// rules/scriptfacts is the pure card-fact leaf of the rules-split plan
		// (2026-10-06): facts a card's printed text and compiled script imply,
		// with no *rules.Engine and no game state. It sits below every rules
		// subsystem, so it may never reach rules or any package above it.
		// TestScriptFactsImportsStayBelowRules pins its whole direct import set;
		// these rows name the edges the layering forbids transitively.
		{module + "/rules/scriptfacts", module + "/rules"},
		{module + "/rules/scriptfacts", module + "/effects"},
		{module + "/rules/scriptfacts", module + "/events"},
		{module + "/rules/scriptfacts", module + "/decision"},
		{module + "/rules/scriptfacts", module + "/botpolicy"},
		{module + "/rules/scriptfacts", module + "/rules/pay"},
		{module + "/rules/scriptfacts", module + "/rules/chars"},
		{module + "/rules/scriptfacts", module + "/rules/trigmatch"},
		{module + "/rules/scriptfacts", module + "/rules/combat"},
		{module + "/rules/scriptfacts", module + "/rules/resolve"},
	}
	for path, p := range pkgs {
		if strings.HasPrefix(path, module+"/bots") {
			for _, target := range []string{module + "/host", module + "/internal/testutil", module + "/internal/azmcts/clairvoyant"} {
				if p.deps[target] {
					t.Errorf("%s depends on %s (transitively); bots packages may not", path, target)
				}
			}
		}
	}
	for _, f := range forbidden {
		p, ok := pkgs[f.from]
		if !ok {
			continue // not built yet; the constraint binds once it is
		}
		if p.deps[f.to] {
			t.Errorf("%s depends on %s (transitively); the dependency order forbids it", f.from, f.to)
		}
	}

	for _, msg := range manabrewBoundaryViolations(pkgs) {
		t.Error(msg)
	}
}

// manabrewBoundaryViolations returns one message per ManaBrew dependency
// boundary violation, for the synthetic or live package map pkgs.
//
// The from-manabrew rows check DIRECT imports only, deliberately. The
// translator is allowed to import view, decision and state (spec §5.1), and
// view imports events; the transport is allowed to import host, and host
// imports rules. A transitive check on those rows would therefore forbid the
// imports §5.1 requires, and note that host/manabrewhttp cannot avoid host.
// What the boundary actually forbids is the adapter reaching an engine
// package ITSELF — a direct edge — which is what p.imports reports. The
// reverse rows stay transitive: nothing on the engine side may depend on the
// adapter even indirectly.
//
// A row whose from package is absent from pkgs is skipped: these package
// paths are intentionally listed before those packages exist, and the checks
// bind automatically as go list begins reporting each package.
//
// The returned slice is sorted so the helper is deterministic.
func manabrewBoundaryViolations(pkgs map[string]pkg) []string {
	forbiddenManaBrewDirect := []struct{ from, to string }{
		{module + "/internal/manabrew", module + "/rules"},
		{module + "/internal/manabrew", module + "/effects"},
		{module + "/internal/manabrew", module + "/events"},
		{module + "/internal/manabrew", module + "/host"},
		{module + "/internal/manabrew", module + "/host/httpapi"},
		{module + "/internal/manabrew", module + "/internal/azmcts"},
		{module + "/internal/manabrew", module + "/internal/testutil"},
		{module + "/host/manabrewhttp", module + "/rules"},
		{module + "/host/manabrewhttp", module + "/effects"},
		{module + "/host/manabrewhttp", module + "/internal/testutil"},
		{module + "/host/manabrewhttp", module + "/internal/azmcts"},
	}
	var msgs []string
	for _, f := range forbiddenManaBrewDirect {
		p, ok := pkgs[f.from]
		if !ok {
			continue
		}
		if p.imports[f.to] {
			msgs = append(msgs, fmt.Sprintf("%s imports %s; the ManaBrew dependency boundary forbids it", f.from, f.to))
		}
	}
	var forbiddenManaBrewReverse []struct{ from, to string }
	for _, from := range []string{
		"cards", "state", "decision", "events", "effects", "botpolicy",
		"rules", "view", "seat", "replay", "protocol", "host", "host/httpapi",
	} {
		for _, to := range []string{"protocol/manabrew", "internal/manabrew", "host/manabrewhttp"} {
			forbiddenManaBrewReverse = append(forbiddenManaBrewReverse, struct{ from, to string }{module + "/" + from, module + "/" + to})
		}
	}
	for _, f := range forbiddenManaBrewReverse {
		p, ok := pkgs[f.from]
		if !ok {
			continue
		}
		if p.deps[f.to] {
			msgs = append(msgs, fmt.Sprintf("%s depends on %s (transitively); the ManaBrew dependency boundary forbids it", f.from, f.to))
		}
	}
	sort.Strings(msgs)
	return msgs
}

// TestManaBrewWireIsStdlibOnly keeps the protocol's public wire types free of
// gorge package dependencies. The row is in place before protocol/manabrew
// exists and starts enforcing the boundary as soon as it is added.
func TestManaBrewWireIsStdlibOnly(t *testing.T) {
	const wire = module + "/protocol/manabrew"
	p, ok := packages(t)[wire]
	if !ok {
		t.Skip("protocol/manabrew is not built yet")
	}
	for imp := range p.imports {
		out, err := exec.Command("go", "list", "-f", "{{.Standard}}", imp).Output()
		if err != nil {
			t.Errorf("inspect %s import %s: %v", wire, imp, err)
			continue
		}
		if strings.TrimSpace(string(out)) != "true" {
			t.Errorf("%s imports %s; ManaBrew wire types must be stdlib-only", wire, imp)
		}
	}
}

// TestNoExportLeaksAnEngineGame is D6's compile-time half: no exported
// function or method of host or host/httpapi may expose a *state.Game or a
// *rules.Engine through its signature — by return type, or by any parameter
// shape a caller could funnel back into a leaked live game. Every value that
// crosses either package is already a view.View, a protocol.* or a
// decision.* payload; the engine and its game never leave rules/ at all (a
// client layer must read state only through view).
//
// The scan is deliberately structural and text-restricted: it reads `go doc
// -all` output and considers ONLY the `func` declaration lines — the
// signatures — never the doc prose. A bare-substring sweep over the whole
// doc block (the plan's sketch) would false-positive on the Events method's
// own doc, which legitimately says "state.Game.Clone" to explain what it
// copies; matching only the declaration lines keeps that prose out of scope.
// The token is boundary-matched so an identifier that merely STARTS with one
// of these type names (were state.GameX or rules.EngineY ever to exist)
// still does not trip it.
func TestNoExportLeaksAnEngineGame(t *testing.T) {
	// These exact type names (state.Game, rules.Engine) are what a leak's
	// signature would carry; a legitimate client-facing type never has an
	// element in either package. Word boundaries stop the match from
	// prefix-colliding with a hypothetical longer identifier.
	leaks := []*regexp.Regexp{
		regexp.MustCompile(`\bstate\.Game\b`),
		regexp.MustCompile(`\brules\.Engine\b`),
	}
	for _, pkg := range []string{module + "/host", module + "/host/httpapi"} {
		out, err := exec.Command("go", "doc", "-all", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go doc -all %s: %v", pkg, err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.HasPrefix(line, "func ") {
				continue // signatures only; prose that merely names the type is not a leak
			}
			for _, re := range leaks {
				if re.MatchString(line) {
					t.Errorf("%s exposes an engine game through %q", pkg, line)
				}
			}
		}
	}
}

// TestNoLegacyMathRand: math/rand/v2 with an explicit seeded source is the
// only randomness (rules/rng.go, seat/bot.go). The v1 package's global
// functions are exactly the ambient randomness the engine spec forbids.
func TestNoLegacyMathRand(t *testing.T) {
	for path, p := range packages(t) {
		if p.imports["math/rand"] {
			t.Errorf("%s imports math/rand; use math/rand/v2 with a seeded source", path)
		}
	}
}

// resumeFieldWriters walks the non-test rules/ sources (recursively, so a
// rules/* subpackage stays in the census) and returns every
// function that assigns to the Engine.resume state, keyed by the receiver-
// qualified function name (e.g. "(*Engine).Ask") with the sites it writes.
// A write reachable through the resume field is a write to the resume state:
// e.resume, e.resume.outer and e.resume.outer.sa all count (fx40), because
// resolution.go carries a real nested write of the shape e.resume.outer = ...
// that the pre-fx40 outermost-selector-only match never saw. It is the census
// that TestResumeStateOwnedOnlyByTheResolutionMachinery both enforces against
// and keeps exact.
func resumeFieldWriters(t *testing.T) map[string][]string {
	t.Helper()
	dir := filepath.Join("..", "..", "rules")
	files := goSourcesUnder(t, dir)
	if len(files) == 0 {
		t.Fatalf("no rules sources found under %s (cwd %s)", dir, mustCwd())
	}
	out := map[string][]string{}
	for _, file := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		// The enclosing function for a position is the func span of smallest
		// extent that contains it — the innermost, whether named or a func
		// literal. Named methods are what the allow-list keys on.
		type span struct {
			name string
			pos  token.Pos
			end  token.Pos
		}
		var spans []span
		ast.Inspect(f, func(n ast.Node) bool {
			if fd, ok := n.(*ast.FuncDecl); ok {
				spans = append(spans, span{funcQualName(fd), fd.Pos(), fd.End()})
			}
			return true
		})
		enclosing := func(p token.Pos) string {
			best := ""
			bestSpan := token.Pos(1 << 30)
			for _, s := range spans {
				if s.pos <= p && p <= s.end && s.end-s.pos < bestSpan {
					best = s.name
					bestSpan = s.end - s.pos
				}
			}
			return best
		}
		rel, err := filepath.Rel(filepath.Join("..", ".."), file)
		if err != nil {
			t.Fatalf("rel %s: %v", file, err)
		}
		rel = filepath.ToSlash(rel)
		ast.Inspect(f, func(n ast.Node) bool {
			check := func(target ast.Expr) {
				resumeSel, ok := resumeSelectorInChain(target)
				if !ok {
					return
				}
				fn := enclosing(resumeSel.Pos())
				out[fn] = append(out[fn], fmt.Sprintf("%s:%d", rel, fset.Position(resumeSel.Pos()).Line))
			}
			switch node := n.(type) {
			case *ast.AssignStmt:
				for _, lhs := range node.Lhs {
					check(lhs)
				}
			case *ast.IncDecStmt:
				check(node.X)
			}
			return true
		})
	}
	return out
}

// goSourcesUnder returns every non-test .go file under dir, RECURSIVELY
// (testdata and dot/underscore directories excluded), sorted. A census that
// globbed only dir/*.go would silently drop every file a refactor moves into
// a subpackage (the rules-engine refactor spec's W5 package extraction).
func goSourcesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != dir && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(files)
	return files
}

// resumeSelectorInChain reports whether any selector in an assignment target's
// selector chain is named "resume", and if so returns that selector. It is
// what makes e.resume, e.resume.outer and e.resume.outer.sa all writes to the
// engine's resume state: the pre-fx40 census compared only the outermost
// selector's name, so a nested write through the field (the e.resume.outer =
// ... line resolution.go has carried since fx34) never registered. It is
// still a syntactic approximation — an alias bound from e.resume and then
// assigned through would defeat it (see the alias-hole limit recorded in
// TestResumeStateOwnedOnlyByTheResolutionMachinery).
func resumeSelectorInChain(expr ast.Expr) (ast.Expr, bool) {
	for {
		sel, ok := expr.(*ast.SelectorExpr)
		if !ok {
			return nil, false
		}
		if sel.Sel.Name == "resume" {
			return sel, true
		}
		expr = sel.X
	}
}

// funcQualName renders a function declaration the way a review comment would
// name it: the receiver qualified for a method, bare for a free function.
// A generic receiver (never used today) falls back to the bare name.
func funcQualName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	switch t := fd.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return "(" + t.Name + ")." + fd.Name.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return "(*" + id.Name + ")." + fd.Name.Name
		}
	}
	return fd.Name.Name
}

func mustCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "?"
	}
	return cwd
}

// TestEngineCompilesFor32Bit pins portability of the continuation machinery.
// `int` is 64 bits on this box and 32 bits on a 32-bit build, so a
// continuation that packs two counters into one int field — the shape Time
// Travel's resume point first reached for, `(round << 32) | idx` in a
// decision's ResumeTarget — is not merely unportable, it does not compile
// there: the `0xffffffff` mask that unpacks it overflows an untyped int
// constant, and even cast, the shift would discard the high half outright.
// The same class covers any `1 << 31`-and-up constant assigned to an int, a
// len() cast assumed to be 64 bits, and an unsafe.Sizeof assumption.
//
// A cross-compile is the honest test for it, because no amount of running on
// amd64 can observe the narrower word. It costs about a second warm and three
// cold, so it is kept here rather than in the engine packages' own suites.
// GOARCH=386 is the narrowest target the toolchain always ships; CGO is off
// because nothing in the module uses it and a 386 C toolchain is not assumed.
//
// cmd/repro's emit-test probes create scratch packages at the repo root while
// `go test ./...` runs; they are `_`-prefixed (cmd/repro's emitScratch), so
// the `/...` pattern never names them and this build cannot race their
// cleanup.
func TestEngineCompilesFor32Bit(t *testing.T) {
	env := append(os.Environ(), "GOOS=linux", "GOARCH=386", "CGO_ENABLED=0")
	cmd := exec.Command("go", "build", module+"/...")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("the module does not build for a 32-bit word (GOARCH=386): %v\n%s\n"+
			"an int is 32 bits there; keep two counters in two fields rather than "+
			"packing them into one int, and size any wide constant explicitly", err, out)
	}
}
