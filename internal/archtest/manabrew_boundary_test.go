package archtest

import (
	"strings"
	"testing"
)

// TestManaBrewBoundaryFromRowsUseDirectImports pins the ManaBrew boundary
// helper's use of two DIFFERENT sets for its two row families:
//
//   - the from-manabrew rows judge a DIRECT import edge (p.imports), so an
//     adapter that legitimately reaches an engine package only through an
//     allowed intermediary is not reported;
//   - the reverse rows judge a TRANSITIVE dependency (p.deps), so the engine
//     side is forbidden even an indirect reach into the adapter.
//
// A future edit that "simplifies" both loops onto the same set silently
// reintroduces either an unsatisfiable row (from-rows on p.deps: the
// translator must import view, and view imports events) or a vacuous one
// (reverse rows on p.imports). This test fails in either direction.
func TestManaBrewBoundaryFromRowsUseDirectImports(t *testing.T) {
	const (
		manabrew = module + "/internal/manabrew"
		rules    = module + "/rules"
		events   = module + "/events"
	)
	// fromMessage/reverseMessage are the helper's verbatim message shapes.
	fromMessage := func(from, to string) string {
		return from + " imports " + to + "; the ManaBrew dependency boundary forbids it"
	}
	reverseMessage := func(from, to string) string {
		return from + " depends on " + to + " (transitively); the ManaBrew dependency boundary forbids it"
	}

	contains := func(msgs []string, want string) bool {
		for _, m := range msgs {
			if m == want {
				return true
			}
		}
		return false
	}

	// Case 1: a DIRECT edge from the adapter to an engine package is a
	// violation. The synthetic package must genuinely carry events in
	// imports (the precondition the case turns on).
	direct := pkg{path: manabrew, imports: set(events), deps: set(events)}
	if !direct.imports[events] || !direct.deps[events] {
		t.Fatal("precondition: direct fixture must hold events in both imports and deps")
	}
	msgs := manabrewBoundaryViolations(map[string]pkg{manabrew: direct})
	if !contains(msgs, fromMessage(manabrew, events)) {
		t.Errorf("direct edge not reported; got %v", msgs)
	}

	// Case 2: a TRANSITIVE-only edge is NOT a from-row violation. This is
	// the exact shape of the real package (manabrew -> view -> events), and
	// it is the case the pre-fix code got wrong. Assert the fixture really
	// differs between imports and deps before trusting the outcome.
	transitiveOnly := pkg{path: manabrew, imports: set(""), deps: set(events)}
	if transitiveOnly.imports[events] || !transitiveOnly.deps[events] {
		t.Fatal("precondition: transitive-only fixture must have events in deps but NOT in imports")
	}
	msgs = manabrewBoundaryViolations(map[string]pkg{manabrew: transitiveOnly})
	if contains(msgs, fromMessage(manabrew, events)) {
		t.Errorf("transitive-only edge wrongly reported as a direct from-row violation: %v", msgs)
	}
	if len(msgs) != 0 {
		t.Errorf("transitive-only fixture must yield no violations at all; got %v", msgs)
	}

	// Case 3a: the reverse rows stay transitive. An engine package that
	// depends on the adapter (deps) is a violation.
	reverseDep := pkg{path: rules, imports: set(""), deps: set(manabrew)}
	if !reverseDep.deps[manabrew] {
		t.Fatal("precondition: reverse fixture must hold manabrew in deps")
	}
	msgs = manabrewBoundaryViolations(map[string]pkg{rules: reverseDep})
	if !contains(msgs, reverseMessage(rules, manabrew)) {
		t.Errorf("transitive reverse edge not reported; got %v", msgs)
	}

	// Case 3b: a DIRECT-only reverse edge is NOT reported, pinning the
	// reverse family to deps rather than imports. Assert the fixture
	// differs.
	reverseImport := pkg{path: rules, imports: set(manabrew), deps: set("")}
	if !reverseImport.imports[manabrew] || reverseImport.deps[manabrew] {
		t.Fatal("precondition: direct-only reverse fixture must have manabrew in imports but NOT in deps")
	}
	msgs = manabrewBoundaryViolations(map[string]pkg{rules: reverseImport})
	if contains(msgs, reverseMessage(rules, manabrew)) {
		t.Errorf("direct-only reverse edge wrongly reported: %v", msgs)
	}
	if len(msgs) != 0 {
		t.Errorf("direct-only reverse fixture must yield no violations at all; got %v", msgs)
	}

	// Case 4: a row whose from package is absent is skipped, not reported.
	// This keeps the vacuity contract explicit and stops the helper from
	// becoming a hard-coded package census.
	msgs = manabrewBoundaryViolations(map[string]pkg{})
	if len(msgs) != 0 {
		t.Errorf("missing from-packages must be skipped; got %v", msgs)
	}
	// A present but unrelated package must also not trip any row.
	unrelated := pkg{path: module + "/deck", imports: set(events), deps: set(manabrew)}
	msgs = manabrewBoundaryViolations(map[string]pkg{unrelated.path: unrelated})
	for _, m := range msgs {
		if strings.Contains(m, "deck") {
			t.Errorf("unrelated package %s wrongly reported: %s", unrelated.path, m)
		}
	}
}
