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
	// W1a generated Engine.cloneWith's field copies from the clone tags
	// (rules/clone_gen.go): 53 -> 52. Re-measured at W1a pool/rekey/if tags: 48. W3 dead: 45.
	maxFuncLinesOver300 = 45
	// engineMethodCount is the number of non-test methods on rules.Engine.
	// W5 E5 moved combat legality onto rules/combat's Board (2159 -> 2126)
	// and W5 E3 the trigger matchers onto rules/trigmatch's: -> 2022. W5 E4
	// moved the layer walk onto rules/chars: -> 2019. W3 clean deleted the
	// Suspend* no-ops: -> 1965. W5 E7 moved mana payment onto rules/pay:
	// -> 1944. Slice 4 made the payer grants pay.Engine adapter methods: -> 1940. W3 dead deleted the resume-scratch setters: -> 1931.
	// E7 slice 9 moved the payment board reads to rules/pay funcs: -> 1917.
	engineMethodCount = 1917
	// hostMethodCount is the number of methods in the effects.Host interface
	// (its whole method set, roles included). W1d replaced ObjectText,
	// ObjectKeywords and BasePower with the one Chars query: 96 -> 94. W3 clean
	// deleted the eight Suspend* no-ops: 94 -> 86.
	hostMethodCount = 85
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
	hostOptionalAssertions = 38
	// ctxFieldCount is the number of named fields in effects.Ctx; embeds are
	// ratcheted separately by ctxEmbedCount.
	// W1d folded EffectiveNames, EffectiveTypes, StaticGoads and the embedded
	// LayerTables into the one named Layers field: 293/2 -> 291/1. W3 clean
	// grouped the LKI snapshot set, the replacement context, clone-as-enters,
	// mana, kicker and the per-primitive cursors (effects/ctx_groups.go): 121 -> 73.
	// W3 dead removed write-only/never-set fields: 73 -> 68.
	ctxFieldCount = 68
	ctxEmbedCount = 1
	// resumePointFieldCount is the number of fields in rules' resumePoint.
	resumePointFieldCount = 5
	// stringParamReads is the number of <x>Params["literal"] index
	// expressions in rules/ and effects/ non-test files. W4 slice 3 (194
	// ParamKeys, the 256-key mask) moved 488 reads onto the typed accessors:
	// 1302 -> 814. The same rewrite over rules/trigmatch's moved matchers
	// (W5 E3, ValidSA/Static): 805 -> 798. W4 slice 4 added 56 keys (250 of
	// the 254 a uint8 ParamKey can name) and migrated rules/chars too:
	// 798 -> 630. W4 step 3's ChangeZone compiler (one read per key,
	// effects/changezone_params.go): 630 -> 628. loop-bugs: effReveal reads
	// RevealDefined$ once: 628 -> 627. W4 step 3's Attach compiler
	// (RememberAttached$ read once): 627 -> 626. The Charm and Pump
	// compilers (charm_params.go, pump_params.go): 626 -> 624. W4 step 3's
	// DealDamage compiler (DamageSource$ read once): 624 -> 623. W4 step 3's
	// PutCounter compiler (one read per key; the entry fold's presence gate
	// and the rules-side CounterTypePerDefined$ read moved into it): 623 -> 621.
	// W4 step 3's Effect compiler (ImprintOnHost$ read once, the opening
	// hand's EffectOwner$ read moved into it): 621 -> 619. The Mana compiler
	// (mana_params.go: each rider read once, the plain-shape key spelling a
	// table scan): 619 -> 610. The ManaReflected compiler (ReflectProperty$
	// read once): 610 -> 609. Measured slack on main (caa66ee75): 609 ->
	// 608. The CopyPermanent compiler (SetCreatureTypes$, RemoveSubTypes$
	// and NumCopies$ read once through rawParamText): 608 -> 605. W4 step 4's
	// Defined-reference tier (the compound Defined$ branch and RevealDefined$
	// no longer rewrite a copied Params map's Defined$ entry; each selector
	// is a compiled Ref): 605 -> 603. The Clone
	// compiler (clone.go's CloneTarget$/ExcludeChosen$/CloneZone$/NewName$/
	// GainThisAbility$/KeepFacedown$/CopyFromChosenName$/SetCreatureTypes$/
	// RemoveSubTypes$ reads compiled once): 603 -> 593.
	// The Dig compiler (effDig moved to dig.go; its twelve literal reads
	// compiled once): 593 -> 581.
	// The DigUntil compiler (effDigUntil moved to diguntil.go; its literal
	// reads and withheld-rider list compiled once): 581 -> 568.
	// The RemoveCounter compiler (effRemoveCounter moved to removecounter.go;
	// CounterNumShared$/ChoiceNum$/RememberRemoved$ read once): 568 -> 564.
	// TargetsAtRandom$ compiled into TargetParams (TgtAtRandom) through
	// PKTargetsAtRandom: 564 -> 563.
	// The Token compiler (effToken's literal reads compiled once; the
	// TokenRemembered$ helper takes each compiler's value): 563 -> 562.
	// The Vote compiler (the three ballots' literal reads compiled once):
	// 562 -> 561.
	// The RepeatEach compiler (effRepeatEach moved to repeateach.go): reads
	// compiled once, RepeatCards$ Zone$ switch a ZoneMask table.
	// W4 tail: every remaining literal read in effects/ and rules/ went through
	// the ParamKey accessors (state.ContinuousEffect.RestrictParam and kin for
	// the continuous-effect maps); what is left is writes and rules/play_tape.go
	// (left to its live branch): 549 -> 20. W4 cases: play_tape.go and
	// resolution_modes.go reads moved to ParamStr (7 new keys): 20 -> 12 (what
	// is left are writes). W4 cases: the twelve writes go through the typed
	// SA.SetParam / Repl.SetParam setters: 12 -> 0.
	stringParamReads = 0
	// stringCaseLiterals is the number of string literals in switch case
	// lists in rules/ and effects/ non-test files.
	// W4 step 3: Attach: 2886 -> 2883. RepeatEach: 2883 -> 2876. W4 cases:
	// constant-returning switches and param whitelists became cards.StrTable /
	// cards.NameSet (sorted dense slices, built once at init): 2876 -> 2046. Every
	// other literal-case switch dispatches on a cards.StrCodes code (one lookup,
	// integer switch, vocabulary in one table): 2046 -> 181. What is left is the
	// case-whitelists the param census reads over a Params range. W4 cases:
	// those whitelists became NameSets (the census reads a range's
	// `set.Has(k)` / `switch codes.Code(k)` keys from the file's table), the
	// mixed and init-form switches code families, and the single-literal
	// switches plain comparisons: 181 -> 0.
	stringCaseLiterals = 0
	// ctxLiterals, specContextLiterals and triggerContextLiterals are the
	// effects.Ctx / SpecContext / TriggerContext composite literals in rules/
	// and effects/ non-test files outside codeshape.CtxConstructorFiles (W1c,
	// spec section 5). Measured 108 / 22 / 19 on main at c49d04e14 before the
	// constructors landed.
	ctxLiterals            = 0
	specContextLiterals    = 0
	triggerContextLiterals = 13
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
	// payEngineMethods is the method count of pay.Engine, the payment
	// layer's whole view of the engine (W5 E7; the spec's target is under
	// 20). Slice 2 moved mana payment behind it with 10.
	payEngineMethods = 10
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
	// charmParamLeaks, pumpParamLeaks and drawParamLeaks are the same census
	// for the modal family's compiler (effects/charm_params.go), api:Pump's
	// (effects/pump_params.go) and api:Draw's (effects/draw_params.go). Each
	// landed at zero.
	charmParamLeaks = 0
	pumpParamLeaks  = 0
	drawParamLeaks  = 0
	// replaceEffectParamLeaks is the same census for api:ReplaceEffect's
	// compiler (effects/replaceeffect_params.go). It landed at zero.
	replaceEffectParamLeaks = 0
	// manaParamLeaks is the same census for api:Mana's production compiler
	// (effects/mana_params.go). It landed at zero.
	manaParamLeaks = 0
	// manaReflectedParamLeaks is the same census for api:ManaReflected's
	// compiler (effects/manareflected_params.go). It landed at zero.
	manaReflectedParamLeaks = 0
	// dealDamageParamLeaks is the same census for api:DealDamage's compiler,
	// effects/dealdamage_params.go (codeshape.DealDamageFiles,
	// codeshape.DealDamageOnlyKeys). It landed at zero.
	dealDamageParamLeaks = 0
	// putCounterParamLeaks is the same census for api:PutCounter's compiler,
	// effects/putcounter_params.go (codeshape.PutCounterFiles,
	// codeshape.PutCounterOnlyKeys). It landed at zero.
	putCounterParamLeaks = 0
	// effectParamLeaks is the same census for api:Effect's compiler,
	// effects/effect_params.go (codeshape.EffectFiles,
	// codeshape.EffectOnlyKeys). It landed at zero.
	effectParamLeaks = 0
	// targetParamLeaks is the same census for the generic targeting tier's
	// compiler, effects/targets_params.go (codeshape.TargetOnlyKeys): no
	// targeting key is read anywhere in rules/ or effects/ outside it. It
	// landed at zero.
	targetParamLeaks = 0
	// definedParamLeaks is the same census for the generic Defined-reference
	// tier's compiler, effects/defined_params.go (codeshape.DefinedOnlyKeys):
	// no Defined$/DefinedCards$/DefinedPlayer$/DefinedTarget$ read anywhere
	// in rules/ or effects/ outside it. It landed at zero.
	definedParamLeaks = 0
	// delayedTriggerParamLeaks is the same census for api:DelayedTrigger's compiler,
	// effects/delayedtrigger_params.go (codeshape.DelayedTriggerFiles, codeshape.DelayedTriggerOnlyKeys). It landed
	// at zero.
	delayedTriggerParamLeaks = 0
	// copyPermanentParamLeaks is the same census for api:CopyPermanent's compiler,
	// effects/copypermanent_params.go (codeshape.CopyPermanentFiles, codeshape.CopyPermanentOnlyKeys). It landed
	// at zero.
	copyPermanentParamLeaks = 0
	// cloneParamLeaks is the same census for api:Clone's compiler,
	// effects/clone_params.go (codeshape.CloneFiles, codeshape.CloneOnlyKeys).
	// It landed at zero.
	cloneParamLeaks = 0
	// digParamLeaks is the same census for api:Dig's compiler,
	// effects/dig_params.go (codeshape.DigFiles, codeshape.DigOnlyKeys). It landed at
	// zero.
	digParamLeaks = 0
	// digUntilParamLeaks is the same census for api:DigUntil's compiler,
	// effects/diguntil_params.go (codeshape.DigUntilFiles, codeshape.DigUntilOnlyKeys). It landed at
	// zero.
	digUntilParamLeaks = 0
	// removeCounterParamLeaks is the same census for api:RemoveCounter's compiler,
	// effects/removecounter_params.go (codeshape.RemoveCounterFiles, codeshape.RemoveCounterOnlyKeys). It landed at
	// zero.
	removeCounterParamLeaks = 0
	// tokenParamLeaks is the same census for api:Token's compiler,
	// effects/token_params.go (codeshape.TokenFiles, codeshape.TokenOnlyKeys). It landed at
	// zero.
	tokenParamLeaks = 0
	// voteParamLeaks is the same census for api:Vote's compiler,
	// effects/vote_params.go (codeshape.VoteFiles, codeshape.VoteOnlyKeys). It landed at
	// zero.
	voteParamLeaks = 0
	// repeatEachParamLeaks is the same census for api:RepeatEach's compiler,
	// effects/repeateach_params.go (codeshape.RepeatEachFiles, codeshape.RepeatEachOnlyKeys). It landed at
	// zero.
	repeatEachParamLeaks = 0
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
		{"payEngineMethods", m.PayEngineMethods, payEngineMethods,
			"pay.Engine is the payment layer's whole view of the engine (target under " +
				"20); derive a new fact from an existing method or pass it precomputed, " +
				"instead of adding a method."},
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
		{"charmParamLeaks", m.CharmParamLeaks, charmParamLeaks,
			"Read the parameter through effects.CharmOf's compiled CharmParams (add a " +
				"field to compileCharm in effects/charm_params.go) instead of reading the " +
				"ability's Params in effects/charm.go or a Charm-only key elsewhere. " +
				"Leaks: " + strings.Join(m.CharmLeaks, ", ")},
		{"pumpParamLeaks", m.PumpParamLeaks, pumpParamLeaks,
			"Read the parameter through effects.PumpOf's compiled PumpParams (add a " +
				"field to compilePump in effects/pump_params.go) instead of reading the " +
				"ability's Params in effects/pump.go or a Pump-only key elsewhere. " +
				"Leaks: " + strings.Join(m.PumpLeaks, ", ")},
		{"drawParamLeaks", m.DrawParamLeaks, drawParamLeaks,
			"Read the parameter through effects.DrawOf's compiled DrawParams (add a " +
				"field to compileDraw in effects/draw_params.go) instead of reading the " +
				"ability's Params in effects/draw.go or a Draw-only key elsewhere. " +
				"Leaks: " + strings.Join(m.DrawLeaks, ", ")},
		{"replaceEffectParamLeaks", m.ReplaceEffectParamLeaks, replaceEffectParamLeaks,
			"Read the parameter through effects.ReplaceEffectOf's compiled " +
				"ReplaceEffectParams (add a field to compileReplaceEffect in " +
				"effects/replaceeffect_params.go) instead of reading the ability's Params " +
				"in effects/replacement.go or a ReplaceEffect-only key elsewhere. " +
				"Leaks: " + strings.Join(m.ReplaceEffectLeaks, ", ")},
		{"manaParamLeaks", m.ManaParamLeaks, manaParamLeaks,
			"Read the parameter through effects.ManaOf's compiled ManaParams (add a " +
				"field to compileMana in effects/mana_params.go) instead of reading the " +
				"ability's Params in effects/mana_effect.go or a Mana-only rider key " +
				"elsewhere. Leaks: " + strings.Join(m.ManaLeaks, ", ")},
		{"manaReflectedParamLeaks", m.ManaReflectedParamLeaks, manaReflectedParamLeaks,
			"Read the parameter through effects.ManaReflectedOf's compiled " +
				"ManaReflectedParams (add a field to compileManaReflected in " +
				"effects/manareflected_params.go) instead of reading the ability's Params " +
				"in effects/mana_reflected.go or a ManaReflected-only key elsewhere. " +
				"Leaks: " + strings.Join(m.ManaReflectedLeaks, ", ")},
		{"dealDamageParamLeaks", m.DealDamageParamLeaks, dealDamageParamLeaks,
			"Read the parameter through effects.DealDamageOf's compiled DealDamageParams (add " +
				"a field to compileDealDamage in effects/dealdamage_params.go) instead of reading " +
				"the ability's Params in effects/damage_deal.go or a DealDamage-only key elsewhere. " +
				"Leaks: " + strings.Join(m.DealDamageLeaks, ", ")},
		{"putCounterParamLeaks", m.PutCounterParamLeaks, putCounterParamLeaks,
			"Read the parameter through effects.PutCounterOf's compiled PutCounterParams (add " +
				"a field to compilePutCounter in effects/putcounter_params.go) instead of reading " +
				"the ability's Params in effects/counters_put.go or a PutCounter-only key elsewhere. " +
				"Leaks: " + strings.Join(m.PutCounterLeaks, ", ")},
		{"effectParamLeaks", m.EffectParamLeaks, effectParamLeaks,
			"Read the parameter through effects.EffectOf's compiled EffectParams (add a " +
				"field to compileEffect in effects/effect_params.go) instead of reading the " +
				"ability's Params in effects/effect.go or an Effect-only key elsewhere. " +
				"Leaks: " + strings.Join(m.EffectLeaks, ", ")},
		{"targetParamLeaks", m.TargetParamLeaks, targetParamLeaks,
			"Read the targeting parameter through effects.TargetsOf's compiled TargetParams " +
				"(add a field to compileTargets in effects/targets_params.go) instead of reading " +
				"a targeting key from the ability's Params. Leaks: " + strings.Join(m.TargetLeaks, ", ")},
		{"definedParamLeaks", m.DefinedParamLeaks, definedParamLeaks,
			"Read the selector through effects.DefinedOf's compiled Ref (add a field to " +
				"compileDefined in effects/defined_params.go, or resolve selector text with " +
				"effects.RefOf/DefinedSpec) instead of reading a Defined key from the ability's " +
				"Params. Leaks: " + strings.Join(m.DefinedLeaks, ", ")},
		{"delayedTriggerParamLeaks", m.DelayedTriggerParamLeaks, delayedTriggerParamLeaks,
			"Read the parameter through effects.DelayedTriggerOf's compiled DelayedTriggerParams (add a " +
				"field to compileDelayedTrigger in effects/delayedtrigger_params.go) instead of reading the ability's Params in " +
				"effects/delayed_trigger.go or a DelayedTrigger-only key elsewhere. " +
				"Leaks: " + strings.Join(m.DelayedTriggerLeaks, ", ")},
		{"copyPermanentParamLeaks", m.CopyPermanentParamLeaks, copyPermanentParamLeaks,
			"Read the parameter through effects.CopyPermanentOf's compiled CopyPermanentParams (add a " +
				"field to compileCopyPermanent in effects/copypermanent_params.go) instead of reading the ability's Params in " +
				"effects/copypermanent.go or a CopyPermanent-only key elsewhere. " +
				"Leaks: " + strings.Join(m.CopyPermanentLeaks, ", ")},
		{"cloneParamLeaks", m.CloneParamLeaks, cloneParamLeaks,
			"Read the parameter through effects.CloneOf's compiled CloneParams (add a " +
				"field to compileClone in effects/clone_params.go) instead of reading the ability's Params in " +
				"effects/clone.go or a Clone-only key elsewhere. " +
				"Leaks: " + strings.Join(m.CloneLeaks, ", ")},
		{"digParamLeaks", m.DigParamLeaks, digParamLeaks,
			"Read the parameter through effects.DigOf's compiled DigParams (add a " +
				"field to compileDig in effects/dig_params.go) instead of reading the ability's Params in " +
				"effects/dig.go or a Dig-only key elsewhere. " +
				"Leaks: " + strings.Join(m.DigLeaks, ", ")},
		{"digUntilParamLeaks", m.DigUntilParamLeaks, digUntilParamLeaks,
			"Read the parameter through effects.DigUntilOf's compiled DigUntilParams (add a " +
				"field to compileDigUntil in effects/diguntil_params.go) instead of reading the ability's Params in " +
				"effects/diguntil.go or a DigUntil-only key elsewhere. " +
				"Leaks: " + strings.Join(m.DigUntilLeaks, ", ")},
		{"removeCounterParamLeaks", m.RemoveCounterParamLeaks, removeCounterParamLeaks,
			"Read the parameter through effects.RemoveCounterOf's compiled RemoveCounterParams (add a " +
				"field to compileRemoveCounter in effects/removecounter_params.go) instead of reading the ability's Params in " +
				"effects/removecounter.go or a RemoveCounter-only key elsewhere. " +
				"Leaks: " + strings.Join(m.RemoveCounterLeaks, ", ")},
		{"tokenParamLeaks", m.TokenParamLeaks, tokenParamLeaks,
			"Read the parameter through effects.TokenOf's compiled TokenParams (add a " +
				"field to compileToken in effects/token_params.go) instead of reading the ability's Params in " +
				"effects/token.go or a Token-only key elsewhere. " +
				"Leaks: " + strings.Join(m.TokenLeaks, ", ")},
		{"voteParamLeaks", m.VoteParamLeaks, voteParamLeaks,
			"Read the parameter through effects.VoteOf's compiled VoteParams (add a " +
				"field to compileVote in effects/vote_params.go) instead of reading the ability's Params in " +
				"effects/vote.go or effects/vote_effect.go or a Vote-only key elsewhere. " +
				"Leaks: " + strings.Join(m.VoteLeaks, ", ")},
		{"repeatEachParamLeaks", m.RepeatEachParamLeaks, repeatEachParamLeaks,
			"Read the parameter through effects.RepeatEachOf's compiled RepeatEachParams (add a " +
				"field to compileRepeatEach in effects/repeateach_params.go) instead of reading the ability's Params in " +
				"effects/repeateach.go or a RepeatEach-only key elsewhere. " +
				"Leaks: " + strings.Join(m.RepeatEachLeaks, ", ")},
		{"triggerContextLiterals", m.TriggerContextLiterals, triggerContextLiterals,
			"Derive trigger referents from the firing event (rules' triggerReferents) or " +
				"copy an existing TriggerContext and set the fields that differ; a " +
				"synthesized trigger's record can be declared `var tc effects.TriggerContext` " +
				"and filled field by field."},
	})
}
