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
// cmd/testtime is exempt because it is a build-time developer tool, not part
// of the engine or the server: it stamps each TEST_HISTORY.md row with the
// UTC time the measurement was taken. Nothing it produces reaches a game, an
// event, a view or a replay, so D16's determinism argument does not apply.
// cmd/botbench is exempt for the same class of reason: its grind mode reads
// the wall clock only to bound its own run window (deadline) and report per-
// deck elapsed time — how long the BENCH chose to run, not anything a game,
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
// keep-alive and write deadlines. internal/mzbridge/mzclient (the HTTP client
// for MageZero's inference server) uses time only for its micro-batch flush
// timer and request timeout; the bridge's encoders in internal/mzbridge
// import no time. No game, event, view or
// replay reads the clock, and intents reach the engine only through
// SubmitIntent.
func TestTimeIsImportedOnlyByTheHost(t *testing.T) {
	allowed := map[string]bool{
		module + "/host":              true,
		module + "/host/httpapi":      true,
		module + "/cmd/gorged":        true,
		module + "/cmd/testtime":      true,
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
		module + "/host/manabrewhttp": true,

		module + "/internal/mzbridge/mzclient": true,
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

// resumeFieldWriters walks the non-test rules/ sources and returns every
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
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("no rules sources found under %s (cwd %s)", dir, mustCwd())
	}
	out := map[string][]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
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
		rel := filepath.Join("rules", filepath.Base(file))
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

// TestResumeStateOwnedOnlyByTheResolutionMachinery is ruling T21-e's static,
// enforced half for the mid-resolution ask. An asking primitive suspends by
// calling effects.Host.Ask and returning true; only the resolution machinery
// (rules/resolution.go: Ask, handleModes, resumeResolution) and the CR 800.4f
// departed-player release hook (rules/sba.go: releasePendingDecisionOfDepartedPlayer)
// may set or clear e.resume. A write from anywhere else bypasses the event
// emit and is invisible to a log-only replay — exactly the class of bug that
// used to be caught only by a human reading a diff.
//
// The allow-list is the exact census measured at the time the rule was
// rewritten (fx40). Both directions are pinned: a writer not on the list
// fails, and a listed function that no longer writes also fails, so adding a
// new writer is a deliberate, reviewed edit to the allow-list rather than a
// silent pass, and pruning a dead entry is equally deliberate.
//
// A named limit, for a reader who would otherwise take the rule to be
// airtight: the census is a selector-chain rule, not an escaping-alias
// analysis. It counts a write whenever any selector in the assignment
// target's chain is named `resume`, so `e.resume.outer = ...` is caught, but
// an alias that hides the field — `rp := e.resume; rp.outer = ...` — leaves
// no `.resume` name in the LHS and defeats it. Measured alias-free at this
// commit: the only non-test `rules/` locals bound from `e.resume` are the
// `rp` snapshots in `handleModes` (rules/resolution.go:133) and
// `releasePendingDecisionOfDepartedPlayer` (rules/sba.go:661), and neither is
// ever assigned through (each holds the pointer only until `e.resume = nil`
// and then passes it to `resumeResolution`, which writes `e.resume.outer`
// directly rather than through the alias). The rule is therefore a syntactic
// approximation: it is exact for the writers that exist today, but it would
// not notice a future alias write.
func TestResumeStateOwnedOnlyByTheResolutionMachinery(t *testing.T) {
	allowed := map[string]string{
		"(*Engine).Ask":                                    "the asking primitive: records the resume point for the answer machine to re-enter (rules/resolution.go)",
		"(*Engine).handleModes":                            "the KModes answer handler: clears the resume point then re-enters via resumeResolution (rules/resolution.go)",
		"(*Engine).handleChoose":                           "the KChoose answer handler: clears the resume point then re-enters via resumeResolution (rules/turn.go)",
		"(*Engine).handleTriggerOptional":                  "the trigger_optional answer handler: clears the resume point (resolution or madness shape) and re-enters, or answers the Miracle placement ask (rules/trigger_queue.go)",
		"(*Engine).handleArrange":                          "the KArrange answer handler: clears the resume point then applies the answered piles (rules/arrange.go)",
		"(*Engine).handleReplacement":                      "the replacement answer handler: re-links a parked frame's outer continuation when a nested ask replaced it, and clears then re-enters the parked event's chain on completion (rules/replacement.go)",
		"(*Engine).askOptionalAtResolution":                "the CR 603.5 optional-resolution ask: installs the fresh resume point the yes/no answer re-enters, never stacked over an existing one (rules/trigger_queue.go)",
		"(*Engine).askMadnessCast":                         "the madness cast offer: installs the fresh madness resume point the offer's answer re-enters (rules/altcast.go)",
		"(*Engine).beginWardPayment":                       "the Ward CR 702.21a gate: after its payment ask, preserves the enclosing continuation on the Ask-installed resume point (rules/ward.go)",
		"(*Engine).askWardObjects":                         "a Ward payment-window ask: preserves the enclosing continuation on the Ask-installed resume point (rules/ward.go)",
		"(*Engine).askWardMana":                            "the Ward mana payment window: carries the prior Ward frame's continuation and replacement snapshot onto the fresh resume point so the window survives nested mana asks (rules/ward.go)",
		"(*Engine).resolveTop":                             "resolution's first pass: when effects.Resolve suspends on a nested mid-resolution ask, preserves the enclosing SubAbility continuation chain on the fresh resume point (rules/stack.go)",
		"(*Engine).SuspendRepeat":                          "the RepeatEach suspension hook (effects.Host): binds the pending ask and continuation frames to the iteration's Remembered and appends the loop's own cursor frame (rules/resolution.go)",
		"(*Engine).bindLoopFrames":                         "binds the pending ask and every unbound continuation frame this pass to the loop's Remembered (rules/resolution.go)",
		"(*Engine).cloneWith":                              "a snapshot clone (Clone, CloneInto) copies the resume point onto the freshly-cloned engine, not the live one (rules/clone.go)",
		"(*Engine).resumeResolution":                       "the re-entry point: links the new pending point's outer continuation up to the frame it is re-entering (rules/resolution.go)",
		"(*Engine).releasePendingDecisionOfDepartedPlayer": "CR 800.4f: a departed player's outstanding ask is released with an empty answer (rules/sba.go)",
		"(*Engine).settleReplacementQueue":                 "handleReplacement's queue tail, factored out: hands the parked resolution frame to queued in-resolution competitions or chains it behind a nested ask's frame, and resumes it once the queue drains (rules/replacement.go)",
		"(*Engine).resolveReplacementBody":                 "a ReplaceWith$ body resolved outside any resolution pass (combat damage): links the body's reported continuations after the ask it posed, or queues the whole body at the tail of an already-pending chain instead of overwriting that ask (rules/replacement.go)",
		"(*Engine).lifeReplacementDraw":                    "the GainLife→Draw replacement's suspension-aware draw loop: parks the remaining card count on the Ask-installed Dredge resume point so the answered dredge re-drives the rest instead of posing a second ask over the outstanding one (rules/replacement.go)",
		"(*Engine).settleTokenElection":                    "the CreateToken replacement election's answer tail: resumes the resolution the election suspended once the plan settles, or chains it behind a nested ask the settle posed (rules/replacement.go)",
	}
	writers := resumeFieldWriters(t)

	// Enforcement: a direct write from a function that does not own the
	// engine's resume state is a replay-visibility bug. Fail naming the
	// offending symbol and the ruling.
	for fn, sites := range writers {
		if _, ok := allowed[fn]; ok {
			continue
		}
		for _, site := range sites {
			t.Errorf("%s: %s writes the engine resume state directly; "+
				"only the resolution machinery or the CR 800.4f departed-player hook may "+
				"set or clear it (ruling T21-e: state changes route through an event emit)",
				site, fn)
		}
	}

	// Census exactness: keep the allow-list in lockstep with reality so a
	// write that moves between functions is a deliberate review, and a dead
	// entry is pruned rather than silently trusted.
	for fn := range allowed {
		if _, ok := writers[fn]; !ok {
			t.Errorf("allow-list entry %s is stale: it writes no resume field; prune it or the write moved (ruling T21-e)", fn)
		}
	}

	// The census is worth recording even when every writer is legitimate:
	// the rule only means something if the list of observed writers is the
	// finite set we audited. Sort for a deterministic failure message.
	named := make([]string, 0, len(writers))
	for fn := range writers {
		named = append(named, fn)
	}
	sort.Strings(named)
	for _, fn := range named {
		t.Logf("resume writer %s at %s", fn, strings.Join(writers[fn], ", "))
	}
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
// The package list is enumerated first and cmd/repro's transient scratch
// packages (zzrepro-*, created and deleted at the repo root by its emit-test
// probes, which run concurrently under `go test ./...`) are dropped: building
// `./...` directly raced them, failing with "cannot find package" or a
// vanished source file whenever a probe cleaned up mid-build.
func TestEngineCompilesFor32Bit(t *testing.T) {
	env := append(os.Environ(), "GOOS=linux", "GOARCH=386", "CGO_ENABLED=0")
	list := exec.Command("go", "list", "-e", module+"/...")
	list.Env = env
	raw, err := list.Output()
	if err != nil {
		t.Fatalf("go list %s/...: %v", module, err)
	}
	args := []string{"build"}
	for _, p := range strings.Fields(string(raw)) {
		if !strings.Contains(p, "/zzrepro-") {
			args = append(args, p)
		}
	}
	cmd := exec.Command("go", args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("the module does not build for a 32-bit word (GOARCH=386): %v\n%s\n"+
			"an int is 32 bits there; keep two counters in two fields rather than "+
			"packing them into one int, and size any wide constant explicitly", err, out)
	}
}
