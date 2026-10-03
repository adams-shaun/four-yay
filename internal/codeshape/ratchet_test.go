package codeshape

import (
	"path/filepath"
	"strings"
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
	// W4 step 3's ChangeZoneAll compiler shrank effChangeZoneAll: 54 -> 53.
	maxFuncLinesOver300 = 53
	// engineMethodCount is the number of non-test methods on rules.Engine.
	// W5 E5 moved combat legality onto rules/combat's Board (2159 -> 2126)
	// and W5 E3 the trigger matchers onto rules/trigmatch's: -> 2022. W5 E4
	// moved the layer walk onto rules/chars: -> 2019.
	engineMethodCount = 2019
	// hostMethodCount is the number of methods in the effects.Host interface
	// (its whole method set, roles included). W1d replaced ObjectText,
	// ObjectKeywords and BasePower with the one Chars query: 96 -> 94.
	hostMethodCount = 94
	// hostDirectMethodCount is the number of methods effects.Host declares
	// itself rather than takes from a role interface (W1d split it into
	// roles; host_roles.go).
	hostDirectMethodCount = 0
	// hostRoleMaxMethods is the method-set size of effects.Host's largest
	// role interface (HostTurnLedger at the W1d split).
	hostRoleMaxMethods = 18
	// hostOptionalAssertions is the number of type assertions in package
	// effects to an interface other than Host and its roles (inline
	// `interface{...}` or a named optional interface). W1d replaced five
	// per-table optional interfaces with layerTablesHost: 54 -> 45.
	hostOptionalAssertions = 45
	// ctxFieldCount is the number of named fields in effects.Ctx; embeds are
	// ratcheted separately by ctxEmbedCount.
	// W1d folded EffectiveNames, EffectiveTypes, StaticGoads and the embedded
	// LayerTables into the one named Layers field: 293/2 -> 291/1.
	ctxFieldCount = 291
	ctxEmbedCount = 1
	// resumePointFieldCount is the number of fields in rules' resumePoint.
	resumePointFieldCount = 90
	// stringParamReads is the number of <x>Params["literal"] index
	// expressions in rules/ and effects/ non-test files. W4 slice 3 (194
	// ParamKeys, the 256-key mask) moved 488 reads onto the typed accessors:
	// 1302 -> 814. The same rewrite over rules/trigmatch's moved matchers
	// (W5 E3, ValidSA/Static): 805 -> 798. W4 slice 4 added 56 keys (250 of
	// the 254 a uint8 ParamKey can name) and migrated rules/chars too:
	// 798 -> 630. W4 step 3's ChangeZone compiler (one read per key,
	// effects/changezone_params.go): 630 -> 628. loop-bugs: effReveal reads
	// RevealDefined$ once: 628 -> 627. W4 step 3's Attach compiler
	// (RememberAttached$ read once): 627 -> 626. W4 step 3's DealDamage
	// compiler (DamageSource$ read once): 626 -> 625.
	stringParamReads = 625
	// stringCaseLiterals is the number of string literals in switch case
	// lists in rules/ and effects/ non-test files.
	// W4 step 3: Attach's Object$ switch compiled to a kind: 2886 -> 2883.
	stringCaseLiterals = 2883
	// ctxLiterals, specContextLiterals and triggerContextLiterals are the
	// effects.Ctx / SpecContext / TriggerContext composite literals in rules/
	// and effects/ non-test files outside codeshape.CtxConstructorFiles (W1c,
	// spec section 5). Measured 108 / 22 / 19 on main at c49d04e14 before the
	// constructors landed.
	ctxLiterals            = 0
	specContextLiterals    = 0
	triggerContextLiterals = 14
	// trigmatchBoardMethods is the method count of trigmatch.Board, the
	// read-only view rules/trigmatch's matchers read the engine through (W5
	// E3). It replaced 21 distinct *Engine methods, 15 Engine fields and
	// pendingCast that the matcher closure reached directly. The move folded
	// SpecCtx/MatchesSpec/MatchesSpecFrom into MatchesSpec/MatchesObject with
	// SpecOpts (a SpecContext's Resolve closure must not cross the
	// interface): 29 -> 28. W5 cleanup moved IsManaAbilityAPI into cards
	// (a pure function of the API word): 28 -> 27. It then swapped the Host()
	// escape hatch (the whole engine as an effects.Host) for the one narrow
	// EvalCount the matchers needed: 27 -> 27.
	trigmatchBoardMethods = 27
	// changeZoneParamLeaks is the number of ChangeZone parameter reads
	// outside its compiler, effects/changezone_params.go (W4 step 3, spec
	// section 8): any read in ChangeZone's own resolution files, plus any
	// read of a ChangeZone-only key elsewhere (codeshape.ChangeZoneFiles,
	// codeshape.ChangeZoneOnlyKeys). It landed at zero.
	changeZoneParamLeaks = 0
	// changeZoneAllParamLeaks is the same census for api:ChangeZoneAll's
	// compiler, effects/changezoneall_params.go (codeshape.ChangeZoneAllFiles,
	// codeshape.ChangeZoneAllOnlyKeys). It landed at zero.
	changeZoneAllParamLeaks = 0
	// attachParamLeaks is the same census for api:Attach's compiler,
	// effects/attach_params.go (codeshape.AttachFiles, codeshape.AttachOnlyKeys).
	// It landed at zero.
	attachParamLeaks = 0
	// dealDamageParamLeaks is the same census for api:DealDamage's compiler,
	// effects/dealdamage_params.go (codeshape.DealDamageFiles,
	// codeshape.DealDamageOnlyKeys). It landed at zero.
	dealDamageParamLeaks = 0
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
	if m.TrigmatchBoardMethods == 0 {
		t.Error("trigmatch.Board measured no methods: rules/trigmatch/board.go's `type Board interface` " +
			"moved or was renamed; update codeshape.Measure so the ratchet cannot read zero")
	}
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
			"effects.Host is the union of narrow role interfaces; derive what you " +
				"need from an existing method (Chars carries every characteristic), or replace one, instead of adding another."},
		{"hostDirectMethodCount", m.HostDirectMethods, hostDirectMethodCount,
			"effects.Host is the union of the role interfaces in effects/host_roles.go; " +
				"add a new method to the one role it belongs to, not to Host itself."},
		{"hostRoleMaxMethods", m.HostRoleMaxMethods, hostRoleMaxMethods,
			"A Host role is to stay narrow (<= 20 methods); split a role that grows by " +
				"concern rather than widening it, or replace an existing method."},
		{"hostOptionalAssertions", m.HostOptionalAssertions, hostOptionalAssertions,
			"An optional-interface assertion on Host is a Host method the role split " +
				"cannot see. Put the method on the role it belongs to (a test double then " +
				"implements it), or extend an existing optional interface, instead of " +
				"asserting a new one."},
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
		{"trigmatchBoardMethods", m.TrigmatchBoardMethods, trigmatchBoardMethods,
			"trigmatch.Board is the trigger matchers' whole view of the engine; derive a " +
				"new fact from an existing method (Chars carries every characteristic, Facts " +
				"every per-emit trigger context value) or pass it precomputed, instead of " +
				"adding a method."},
		{"changeZoneParamLeaks", m.ChangeZoneParamLeaks, changeZoneParamLeaks,
			"Read the parameter through effects.ChangeZoneOf's compiled ChangeZoneParams " +
				"(add a field to compileChangeZone in effects/changezone_params.go) instead " +
				"of reading the ability's Params in a ChangeZone file or a ChangeZone-only " +
				"key elsewhere. Leaks: " + strings.Join(m.ChangeZoneLeaks, ", ")},
		{"changeZoneAllParamLeaks", m.ChangeZoneAllParamLeaks, changeZoneAllParamLeaks,
			"Read the parameter through effects.ChangeZoneAllOf's compiled ChangeZoneAllParams " +
				"(add a field to compileChangeZoneAll in effects/changezoneall_params.go) instead " +
				"of reading the ability's Params in effects/zone_changeall.go or a " +
				"ChangeZoneAll-only key elsewhere. Leaks: " + strings.Join(m.ChangeZoneAllLeaks, ", ")},
		{"attachParamLeaks", m.AttachParamLeaks, attachParamLeaks,
			"Read the parameter through effects.AttachOf's compiled AttachParams (add a " +
				"field to compileAttach in effects/attach_params.go) instead of reading the " +
				"ability's Params in effects/attach.go or an Attach-only key elsewhere. " +
				"Leaks: " + strings.Join(m.AttachLeaks, ", ")},
		{"dealDamageParamLeaks", m.DealDamageParamLeaks, dealDamageParamLeaks,
			"Read the parameter through effects.DealDamageOf's compiled DealDamageParams (add " +
				"a field to compileDealDamage in effects/dealdamage_params.go) instead of reading " +
				"the ability's Params in effects/damage_deal.go or a DealDamage-only key elsewhere. " +
				"Leaks: " + strings.Join(m.DealDamageLeaks, ", ")},
		{"triggerContextLiterals", m.TriggerContextLiterals, triggerContextLiterals,
			"Derive trigger referents from the firing event (rules' triggerReferents) or " +
				"copy an existing TriggerContext and set the fields that differ; a " +
				"synthesized trigger's record can be declared `var tc effects.TriggerContext` " +
				"and filled field by field."},
	})
}
