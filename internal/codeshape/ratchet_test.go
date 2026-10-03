package codeshape

import (
	"path/filepath"
	"testing"
)

// The W0 shrink-only ratchets of the rules-engine refactor spec
// (docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 4), on the pattern of internal/testutil/agentsdoc_test.go's
// knownApproximationRows: each constant is the measured value on main at
// 49bd9af8d, the test FAILS when the measurement grows past it and LOGS when
// it falls below it. A change that shrinks a metric lowers its constant to
// the new measurement in the same commit; NEVER raise one.
//
// Measure with `go run ./cmd/codeshape -table`.
const (
	// maxFuncLinesOver300 is the number of non-test functions in rules/ and
	// effects/ spanning more than 300 lines.
	maxFuncLinesOver300 = 54
	// engineMethodCount is the number of non-test methods on rules.Engine.
	engineMethodCount = 2162
	// hostMethodCount is the number of methods in the effects.Host interface.
	hostMethodCount = 96
	// ctxFieldCount is the number of named fields in effects.Ctx; embeds are
	// ratcheted separately by ctxEmbedCount.
	ctxFieldCount = 293
	ctxEmbedCount = 2
	// resumePointFieldCount is the number of fields in rules' resumePoint.
	resumePointFieldCount = 90
	// stringParamReads is the number of <x>Params["literal"] index
	// expressions in rules/ and effects/ non-test files.
	stringParamReads = 2142
	// stringCaseLiterals is the number of string literals in switch case
	// lists in rules/ and effects/ non-test files.
	stringCaseLiterals = 2886
	// ctxLiterals, specContextLiterals and triggerContextLiterals are the
	// effects.Ctx / SpecContext / TriggerContext composite literals in rules/
	// and effects/ non-test files outside codeshape.CtxConstructorFiles (W1c,
	// spec section 5). Measured 108 / 22 / 19 on main at c49d04e14 before the
	// constructors landed.
	ctxLiterals            = 0
	specContextLiterals    = 0
	triggerContextLiterals = 14
)

func measureRepo(t *testing.T) Metrics {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	m, err := Measure(root)
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	return m
}

type ratchet struct {
	name   string // the constant's name
	got    int
	limit  int
	advice string // what to do instead of growing it
}

func checkRatchets(t *testing.T, rs []ratchet) {
	t.Helper()
	for _, r := range rs {
		switch {
		case r.got > r.limit:
			t.Errorf("%s: measured %d, above the frozen ceiling of %d.\n"+
				"This is a SHRINK-ONLY ratchet (rules-engine refactor spec, W0): it may "+
				"fall, never rise. %s\n"+
				"Do not raise the constant to make this pass; a reviewer treats a raised "+
				"ratchet as a MAJOR finding. Measure with `go run ./cmd/codeshape -table`.",
				r.name, r.got, r.limit, r.advice)
		case r.got < r.limit:
			t.Logf("%s: measured %d, below the ceiling of %d. Lower %s in "+
				"internal/codeshape/ratchet_test.go to %d in the same commit so the gain "+
				"is locked in.", r.name, r.got, r.limit, r.name, r.got)
		default:
			t.Logf("%s: %d (at ceiling)", r.name, r.got)
		}
	}
}

func TestCodeShapeOnlyShrinks(t *testing.T) {
	m := measureRepo(t)
	checkRatchets(t, []ratchet{
		{"maxFuncLinesOver300", m.FuncsOver300, maxFuncLinesOver300,
			"A function over 300 lines is a concern without a seam: extract the concern " +
				"behind a named helper or type rather than growing the function. " +
				"`go run ./cmd/codeshape -table` lists them longest first."},
		{"engineMethodCount", m.EngineMethods, engineMethodCount,
			"Every *Engine method can call every other; put new logic on a narrower " +
				"receiver (a feature struct holding what it needs) or a free function, or " +
				"remove an Engine method in the same change."},
		{"hostMethodCount", m.HostMethods, hostMethodCount,
			"effects.Host is to become role interfaces of <= 20 methods; derive what you " +
				"need from an existing method, or replace one, instead of adding another."},
		{"ctxFieldCount", m.CtxFields, ctxFieldCount,
			"effects.Ctx fields are mostly per-primitive ask/resume cursors; keep a " +
				"primitive's cursor in its own resume record instead of widening Ctx."},
		{"ctxEmbedCount", m.CtxEmbeds, ctxEmbedCount,
			"An embed promotes every field of the embedded type into effects.Ctx; add a " +
				"named field instead (and count it against ctxFieldCount)."},
		{"resumePointFieldCount", m.ResumePointFields, resumePointFieldCount,
			"resumePoint is the hand-defunctionalised continuation W3 replaces; carry a " +
				"new suspension's state in the primitive's own resume record."},
		{"stringParamReads", m.StringParamReads, stringParamReads,
			"Read a parameter through a typed accessor (cards.ParamKey vocabulary) " +
				"instead of a new Params[\"Literal\"] index."},
		{"stringCaseLiterals", m.StringCaseLiterals, stringCaseLiterals,
			"Dispatch on a compiled enum, a bitmask or a registry entry rather than a " +
				"new `case \"Literal\":` arm."},
		{"ctxLiterals", m.CtxLiterals, ctxLiterals,
			"Build an effects.Ctx with effects.NewCtx / NewCtxPtr (seeded by a CtxInit, " +
				"other fields assigned on the result), derive one from a resolving context " +
				"with (*Ctx).Child or (*Ctx).ForTrigger, or add a constructor to " +
				"effects/ctx_new.go (rules/ctx_new.go for one that binds engine-owned " +
				"tables). A hand-written literal that copies another context's fields drops " +
				"the next field added to Ctx."},
		{"specContextLiterals", m.SpecContextLiterals, specContextLiterals,
			"Build a SpecContext with effects.NewSpecContext, derive it from a resolving " +
				"Ctx with (*Ctx).SpecContext / (*Ctx).TableSpecContext, or bind rules' layer " +
				"tables through Engine.withNames / specCtxSVars."},
		{"triggerContextLiterals", m.TriggerContextLiterals, triggerContextLiterals,
			"Derive trigger referents from the firing event (rules' triggerReferents) or " +
				"copy an existing TriggerContext and set the fields that differ; a " +
				"synthesized trigger's record can be declared `var tc effects.TriggerContext` " +
				"and filled field by field."},
	})
}
