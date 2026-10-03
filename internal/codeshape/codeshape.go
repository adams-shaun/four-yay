// Package codeshape measures the shape of the rules engine's code: how long
// its functions are, how wide its interfaces and context structs are, and how
// much of its behaviour is still keyed on string literals. It is the metric
// behind the W0 shrink-only ratchets of the rules-engine refactor spec
// (docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 4) and behind the steward axis of scripts/reward_collect.py, which replaced
// the old file-size count: a long FUNCTION is a concern without a seam, while
// a long FILE says nothing about boundaries and rewarded size-only splits.
//
// It is stdlib only (go/ast, go/parser) and deterministic: every list it
// returns is sorted, and no map order reaches the output.
package codeshape

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// LongFuncLines is the threshold above which a function counts as long: a
// FuncDecl spanning MORE than this many source lines (inclusive of its
// signature and closing brace).
const LongFuncLines = 300

// ScannedDirs are the repo-relative trees measured. Each is walked
// recursively (testdata and dot/underscore directories excluded), so a later
// move of code into a rules/* subpackage stays inside the census.
var ScannedDirs = []string{"rules", "effects"}

// CtxConstructorFiles are the designated context-constructor files of the W1c
// ratchet (rules-engine refactor spec section 5): the only non-test files in
// which an effects.Ctx / effects.SpecContext / effects.TriggerContext
// composite literal is not counted. Repo-relative, slash-separated.
var CtxConstructorFiles = []string{"effects/ctx_new.go", "rules/ctx_new.go"}

// ChangeZoneCompilerFile is api:ChangeZone's parameter compiler (W4 step 3 of
// the rules-engine refactor spec, section 8): the one file in rules/ and
// effects/ allowed to read a ChangeZone ability's parameters.
const ChangeZoneCompilerFile = "effects/changezone_params.go"

// ChangeZoneFiles are ChangeZone's own resolution files. Every parameter read
// there goes through the compiled ChangeZoneParams, so the files carry no
// parameter read of any key.
var ChangeZoneFiles = []string{
	"effects/zone_change.go",
	"effects/zone_hand.go",
	"effects/zone_hidden.go",
	"effects/zone_search.go",
	"effects/zone_library.go",
}

// ChangeZoneOnlyKeys are the parameter keys only ChangeZone's compiler reads:
// a read of one of them anywhere else in rules/ or effects/ is a sibling path
// interpreting a ChangeZone parameter on its own.
var ChangeZoneOnlyKeys = []string{
	"AlternativeDecider", "AttachedToPlayer", "ChooseFromDefined", "DestAltSVar",
	"DestAltSVarCompare", "DestinationAlternative", "DifferentNames", "Exactly",
	"ExileFaceDown", "Foretold", "Hidden", "ImprintLast", "LibraryPositionAlternative",
	"MaxRevealed", "OptionalPrompt", "OriginAlternative", "RememberSearched", "Reorder",
	"SelectPrompt", "ShareLandType", "ShuffleNonMandatory", "Transformed", "Unearth",
	"Unimprint", "WithMayLook", "WithTotalCardTypes",
}

// ChangeZoneAllCompilerFile is api:ChangeZoneAll's parameter compiler (W4
// step 3): the one file allowed to read a ChangeZoneAll ability's parameters.
const ChangeZoneAllCompilerFile = "effects/changezoneall_params.go"

// ChangeZoneAllFiles are ChangeZoneAll's own resolution files (the sweep):
// they carry no parameter read of any key.
var ChangeZoneAllFiles = []string{"effects/zone_changeall.go"}

// ChangeZoneAllOnlyKeys are the parameter keys only ChangeZoneAll's compiler
// reads.
var ChangeZoneAllOnlyKeys = []string{"RandomOrder", "UseAllOriginZones"}

// AttachCompilerFile is api:Attach's parameter compiler (W4 step 3): the one
// file allowed to read an Attach ability's parameters.
const AttachCompilerFile = "effects/attach_params.go"

// AttachFiles are Attach's own resolution files: they carry no parameter read
// of any key.
var AttachFiles = []string{"effects/attach.go"}

// AttachOnlyKeys are the parameter keys only Attach's compiler reads (the
// Reconfigure offer gate reads Unattach$ through it too).
var AttachOnlyKeys = []string{"Object", "RememberAttached", "Unattach"}

// CharmCompilerFile is the modal family's parameter compiler (W4 step 3):
// the one file allowed to read a Charm/GenericChoice ability's own
// parameters (the Choices$ mode list for every modal carrier).
const CharmCompilerFile = "effects/charm_params.go"

// CharmFiles are the modal family's own resolution files: they carry no
// parameter read of any key (the mode BODIES' reads are in
// effects/charm_modes.go, api:Vote's in effects/vote_effect.go).
var CharmFiles = []string{"effects/charm.go"}

// CharmOnlyKeys are the parameter keys only the Charm compiler reads.
var CharmOnlyKeys = []string{"CanRepeatModes", "CharmNum", "ChoiceRestriction", "FallbackAbility",
	"MinCharmNum", "RandomCompare", "RandomCompareSVar", "TempRemember"}

// PumpCompilerFile is api:Pump's parameter compiler (W4 step 3), including
// the KW$/Duration$/LeaveBattlefield$ grant PumpAll shares.
const PumpCompilerFile = "effects/pump_params.go"

// PumpFiles are Pump's own resolution files: no parameter read of any key.
var PumpFiles = []string{"effects/pump.go"}

// PumpOnlyKeys are the parameter keys only Pump's compiler reads.
var PumpOnlyKeys = []string{"ClearNotedCardsFor", "ForgetImprinted", "KWChoice", "NoteCards",
	"NoteCardsFor", "NoteNumber"}

// DrawCompilerFile is api:Draw's parameter compiler (W4 step 3).
const DrawCompilerFile = "effects/draw_params.go"

// DrawFiles are Draw's own resolution files: no parameter read of any key.
var DrawFiles = []string{"effects/draw.go"}

// DrawOnlyKeys are the parameter keys only Draw's compiler reads.
var DrawOnlyKeys = []string{"RememberDrawn", "Upto"}

// ReplaceEffectCompilerFile is api:ReplaceEffect's parameter compiler (W4
// step 3).
const ReplaceEffectCompilerFile = "effects/replaceeffect_params.go"

// ReplaceEffectFiles are ReplaceEffect's own resolution files: no parameter
// read of any key.
var ReplaceEffectFiles = []string{"effects/replacement.go"}

// ReplaceEffectOnlyKeys are the parameter keys only ReplaceEffect's compiler
// reads (rules' Scry and count-operator readers of a ReplaceWith$ body read
// them through it).
var ReplaceEffectOnlyKeys = []string{"VarName", "VarValue"}

// ManaCompilerFile is api:Mana's production-parameter compiler (W4 step 3).
const ManaCompilerFile = "effects/mana_params.go"

// ManaFiles are Mana's own resolution files: no parameter read of any key.
var ManaFiles = []string{"effects/mana_effect.go"}

// ManaOnlyKeys are the parameter keys only Mana's compiler reads (the
// production riders; Produced$/Amount$/RestrictValid$ are read through it by
// every mana path too, but ManaReflected's compiler and the TapsForMana
// trigger matcher read their own keys of the same names).
var ManaOnlyKeys = []string{"AddsCounters", "AddsNoCounter", "PersistentMana",
	"PersistentUntilEndOfCombat", "TriggersWhenSpent"}

// ManaReflectedCompilerFile is api:ManaReflected's parameter compiler (W4
// step 3).
const ManaReflectedCompilerFile = "effects/manareflected_params.go"

// ManaReflectedFiles are ManaReflected's own resolution files: no parameter
// read of any key.
var ManaReflectedFiles = []string{"effects/mana_reflected.go"}

// ManaReflectedOnlyKeys are the parameter keys only ManaReflected's compiler
// reads.
var ManaReflectedOnlyKeys = []string{"ColorOrType", "ReflectProperty"}

// DealDamageCompilerFile is api:DealDamage's parameter compiler (W4 step 3):
// the one file allowed to read a DealDamage ability's parameters.
const DealDamageCompilerFile = "effects/dealdamage_params.go"

// DealDamageFiles are DealDamage's own resolution files: they carry no
// parameter read of any key.
var DealDamageFiles = []string{"effects/damage_deal.go"}

// DealDamageOnlyKeys are the parameter keys only DealDamage's compiler reads
// (Fight's unread-parameter Note names ExcessSVar$ through a variable key,
// not a read).
var DealDamageOnlyKeys = []string{"ExcessSVar", "ExcessSVarCondition", "RelativeTarget"}

// PutCounterCompilerFile is api:PutCounter's parameter compiler (W4 step 3):
// the one file allowed to read a PutCounter ability's parameters.
const PutCounterCompilerFile = "effects/putcounter_params.go"

// PutCounterFiles are PutCounter's own resolution files: they carry no
// parameter read of any key.
var PutCounterFiles = []string{"effects/counters_put.go"}

// PutCounterOnlyKeys are the parameter keys only PutCounter's compiler reads
// (the entry-counter fold and the Adapt$ offer gate read them through it).
var PutCounterOnlyKeys = []string{"Adapt", "Bolster", "ChooseDifferent", "CounterNumPerDefined",
	"CounterTypePerDefined", "Divided", "EachFromSource", "MinChoiceAmount", "PerDefined",
	"RandomType", "RememberCards", "Renown", "Support"}

// EffectCompilerFile is api:Effect's parameter compiler (W4 step 3): the one
// file allowed to read an Effect ability's parameters (and its Triggers$
// bodies' own, readEffectTriggerLine).
const EffectCompilerFile = "effects/effect_params.go"

// EffectFiles are Effect's own resolution files: they carry no parameter read
// of any key.
var EffectFiles = []string{"effects/effect.go"}

// EffectOnlyKeys are the parameter keys only Effect's compiler reads (the
// opening-hand Effect reads EffectOwner$ through it too).
var EffectOnlyKeys = []string{"EffectOwner", "ExileOnMoved", "ForgetCounter", "ForgetOnCast",
	"ForgetOnMoved", "ForgetOnPhasedIn", "ImprintOnHost", "ReplacementEffects", "SetChosenNumber",
	"Stackable"}

// DelayedTriggerCompilerFile is api:DelayedTrigger's parameter compiler (W4 step 3): the
// one file allowed to read a DelayedTrigger ability's parameters.
const DelayedTriggerCompilerFile = "effects/delayedtrigger_params.go"

// DelayedTriggerFiles are DelayedTrigger's own resolution files: they carry no parameter
// read of any key.
var DelayedTriggerFiles = []string{"effects/delayed_trigger.go"}

// DelayedTriggerOnlyKeys are the parameter keys only DelayedTrigger's compiler reads.
var DelayedTriggerOnlyKeys = []string{"NextTurn", "RememberChain"}

// CopyPermanentCompilerFile is api:CopyPermanent's parameter compiler (W4 step 3): the
// one file allowed to read a CopyPermanent ability's parameters.
const CopyPermanentCompilerFile = "effects/copypermanent_params.go"

// CopyPermanentFiles are CopyPermanent's own resolution files: they carry no parameter
// read of any key.
var CopyPermanentFiles = []string{"effects/copypermanent.go"}

// CopyPermanentOnlyKeys are the parameter keys only CopyPermanent's compiler reads.
var CopyPermanentOnlyKeys = []string{"AtEOTTrig", "DefinedName", "NumCopies", "Pawprint", "Populate", "RandomCopied", "RandomNum", "ValidSupportedCopy", "WithDifferentNames"}

// TypedParamCompiler names one API's parameter compiler for the leak census
// (Metrics.ChangeZoneParamLeaks and its siblings): the compiler file, the
// API's own resolution files (no parameter read of any key there) and the
// keys only that compiler may read anywhere in rules/ or effects/.
type TypedParamCompiler struct {
	CompilerFile string
	Files        []string
	OnlyKeys     []string
}

// effectsImportPath is the import path whose Ctx, SpecContext and
// TriggerContext types the context-literal census counts.
const effectsImportPath = "github.com/adams-shaun/gorge/effects"

// Func is one function declaration over LongFuncLines.
type Func struct {
	Name  string `json:"name"`  // receiver-qualified: "(*Engine).payCast" or "effEffect"
	File  string `json:"file"`  // repo-relative, slash-separated
	Line  int    `json:"line"`  // line of the func keyword
	Lines int    `json:"lines"` // end line - start line + 1
}

// Metrics is one measurement. The JSON names are the contract
// scripts/reward_collect.py reads; extend, never rename.
type Metrics struct {
	// FuncsOver300 counts non-test FuncDecls in rules/ and effects/ spanning
	// more than LongFuncLines lines.
	FuncsOver300 int `json:"funcs_over_300"`
	// EngineMethods counts non-test methods whose receiver base type is
	// Engine, under rules/ (recursive).
	EngineMethods int `json:"engine_methods"`
	// HostMethods counts the methods in effects.Host's method set: the ones it
	// declares directly plus every method of the role interfaces it embeds
	// (resolved among package effects' own interface types, recursively; a
	// method two roles share counts once). HostEmbeds counts the interfaces it
	// embeds directly. HostDirectMethods counts the methods Host itself
	// declares rather than takes from a role (W1d: a new method joins a
	// role), and HostRoleMaxMethods is the method-set size of its largest
	// embedded role.
	HostMethods        int `json:"host_methods"`
	HostEmbeds         int `json:"host_embeds"`
	HostDirectMethods  int `json:"host_direct_methods"`
	HostRoleMaxMethods int `json:"host_role_max_methods"`
	// HostOptionalAssertions counts, in package effects' non-test files, the
	// type assertions to an interface other than Host and its roles -- an
	// inline `h.(interface{ ... })` or a named optional interface such as
	// `h.(layerTablesHost)`. Each is a Host method by another name that the
	// role split cannot see; W1d collapsed five per-table ones into one.
	HostOptionalAssertions int `json:"host_optional_assertions"`
	// CtxFields counts the named fields of effects.Ctx (each name on a
	// multi-name line counts); CtxEmbeds its embedded fields.
	CtxFields int `json:"ctx_fields"`
	CtxEmbeds int `json:"ctx_embeds"`
	// ResumePointFields counts the fields of rules' resumePoint struct (named
	// fields per name, plus embeds).
	ResumePointFields int `json:"resume_point_fields"`
	// StringParamReads counts index expressions <x>Params["<literal>"] in
	// rules/ and effects/ non-test files (reads and writes alike; the
	// literal key is the debt). StringParamKeys is the distinct-key count.
	StringParamReads int `json:"string_param_reads"`
	StringParamKeys  int `json:"string_param_keys"`
	// StringCaseLiterals counts, in rules/ and effects/ non-test files, the
	// string literals appearing directly in a case clause's expression list:
	// each such literal counts once (`case "A", "B":` is 2). A literal
	// nested deeper inside a case expression (`case f("A"):`) or in the
	// clause body is NOT counted -- unlike the spec's grep figures, which
	// counted string literals on case lines.
	StringCaseLiterals int `json:"string_case_literals"`
	// CtxLiterals, SpecContextLiterals and TriggerContextLiterals count, in
	// rules/ and effects/ non-test files other than CtxConstructorFiles, the
	// composite literals of effects.Ctx, effects.SpecContext and
	// effects.TriggerContext (`effects.Ctx{...}` / `&effects.Ctx{...}`, the
	// bare `Ctx{...}` inside package effects, and a type-elided element of a
	// []Ctx / map[K]Ctx literal). A literal that copies fields from another
	// context is the RC2 hand-synced copy that drops a later field; build
	// through effects.NewCtx / NewSpecContext and the (*Ctx) derivations.
	CtxLiterals            int `json:"ctx_literals"`
	SpecContextLiterals    int `json:"spec_context_literals"`
	TriggerContextLiterals int `json:"trigger_context_literals"`
	// ChangeZoneParamLeaks counts ChangeZone parameter reads outside its
	// compiler (ChangeZoneCompilerFile): every parameter read in
	// ChangeZoneFiles, plus every read of a ChangeZoneOnlyKeys key in any other
	// rules/ or effects/ non-test file. A read is an index expression
	// <x>Params["k"] that is not an assignment target, a Param/ParamStr/
	// HasParam(cards.PK<k>) call, or a Num/NumResolved/NumResolvedStrict/
	// NumForObject call with a literal key. ChangeZoneLeaks lists them
	// ("file:line key"), sorted.
	ChangeZoneParamLeaks int      `json:"change_zone_param_leaks"`
	ChangeZoneLeaks      []string `json:"change_zone_leaks"`
	// ChangeZoneAllParamLeaks is the same census for api:ChangeZoneAll
	// (ChangeZoneAllCompilerFile, ChangeZoneAllFiles, ChangeZoneAllOnlyKeys).
	ChangeZoneAllParamLeaks int      `json:"change_zone_all_param_leaks"`
	ChangeZoneAllLeaks      []string `json:"change_zone_all_leaks"`
	// AttachParamLeaks is the same census for api:Attach (AttachCompilerFile,
	// AttachFiles, AttachOnlyKeys).
	AttachParamLeaks int      `json:"attach_param_leaks"`
	AttachLeaks      []string `json:"attach_leaks"`
	// CharmParamLeaks, PumpParamLeaks and DrawParamLeaks are the same census
	// for the modal family, api:Pump and api:Draw (Charm*, Pump*, Draw*
	// CompilerFile/Files/OnlyKeys).
	CharmParamLeaks int      `json:"charm_param_leaks"`
	CharmLeaks      []string `json:"charm_leaks"`
	PumpParamLeaks  int      `json:"pump_param_leaks"`
	PumpLeaks       []string `json:"pump_leaks"`
	DrawParamLeaks  int      `json:"draw_param_leaks"`
	DrawLeaks       []string `json:"draw_leaks"`
	// ReplaceEffectParamLeaks is the same census for api:ReplaceEffect
	// (ReplaceEffect* CompilerFile/Files/OnlyKeys).
	ReplaceEffectParamLeaks int      `json:"replace_effect_param_leaks"`
	ReplaceEffectLeaks      []string `json:"replace_effect_leaks"`
	// ManaParamLeaks is the same census for api:Mana (Mana* CompilerFile/
	// Files/OnlyKeys).
	ManaParamLeaks int      `json:"mana_param_leaks"`
	ManaLeaks      []string `json:"mana_leaks"`
	// ManaReflectedParamLeaks is the same census for api:ManaReflected
	// (ManaReflected* CompilerFile/Files/OnlyKeys).
	ManaReflectedParamLeaks int      `json:"mana_reflected_param_leaks"`
	ManaReflectedLeaks      []string `json:"mana_reflected_leaks"`
	// DealDamageParamLeaks is the same census for api:DealDamage
	// (DealDamageCompilerFile, DealDamageFiles, DealDamageOnlyKeys).
	DealDamageParamLeaks int      `json:"deal_damage_param_leaks"`
	DealDamageLeaks      []string `json:"deal_damage_leaks"`
	// PutCounterParamLeaks is the same census for api:PutCounter
	// (PutCounterCompilerFile, PutCounterFiles, PutCounterOnlyKeys).
	PutCounterParamLeaks int      `json:"put_counter_param_leaks"`
	PutCounterLeaks      []string `json:"put_counter_leaks"`
	// EffectParamLeaks is the same census for api:Effect (EffectCompilerFile,
	// EffectFiles, EffectOnlyKeys).
	EffectParamLeaks int      `json:"effect_param_leaks"`
	EffectLeaks      []string `json:"effect_leaks"`
	// DelayedTriggerParamLeaks is the same census for api:DelayedTrigger (DelayedTriggerCompilerFile,
	// DelayedTriggerFiles, DelayedTriggerOnlyKeys).
	DelayedTriggerParamLeaks int      `json:"delayed_trigger_param_leaks"`
	DelayedTriggerLeaks      []string `json:"delayed_trigger_leaks"`
	// CopyPermanentParamLeaks is the same census for api:CopyPermanent (CopyPermanentCompilerFile,
	// CopyPermanentFiles, CopyPermanentOnlyKeys).
	CopyPermanentParamLeaks int      `json:"copy_permanent_param_leaks"`
	CopyPermanentLeaks      []string `json:"copy_permanent_leaks"`
	// TrigmatchBoardMethods counts the methods trigmatch.Board declares
	// (rules/trigmatch/board.go): the read-only view the trigger matchers
	// reach the engine through (W5 E3). Zero when the package is absent.
	TrigmatchBoardMethods int `json:"trigmatch_board_methods"`
	// Files is how many non-test .go files were parsed.
	Files int `json:"files"`
	// LongFuncs lists every function counted by FuncsOver300, longest first
	// (ties by file then line).
	LongFuncs []Func `json:"long_funcs"`
}

// Measure parses rules/ and effects/ under root (the module root) and returns
// the metrics. It fails if a measured type (effects.Host, effects.Ctx,
// rules' resumePoint) cannot be found, so a rename cannot silently zero a
// ratchet.
func Measure(root string) (Metrics, error) {
	var m Metrics
	keys := map[string]bool{}
	hostFound, ctxFound, rpFound := false, false, false
	// ifaces is every top-level interface type of package effects, so Host's
	// embedded roles can be resolved once the whole package is parsed.
	ifaces := map[string]*ast.InterfaceType{}
	// assertedNames are the named types effects asserts to; the optional
	// interfaces among them are counted once ifaces is complete.
	var assertedNames []string
	for _, dir := range ScannedDirs {
		files, err := goFiles(root, dir)
		if err != nil {
			return m, err
		}
		for _, rel := range files {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
			if err != nil {
				return m, fmt.Errorf("codeshape: parse %s: %w", rel, err)
			}
			m.Files++
			inEffectsTop := dir == "effects" && strings.Count(rel, "/") == 1
			if !isCtxConstructorFile(rel) {
				countCtxLiterals(f, inEffectsTop, &m)
			}
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					start := fset.Position(d.Pos()).Line
					end := fset.Position(d.End()).Line
					if n := end - start + 1; n > LongFuncLines {
						m.LongFuncs = append(m.LongFuncs, Func{Name: QualName(d), File: rel, Line: start, Lines: n})
					}
					if dir == "rules" && receiverBase(d) == "Engine" {
						m.EngineMethods++
					}
				case *ast.GenDecl:
					if d.Tok != token.TYPE {
						continue
					}
					for _, spec := range d.Specs {
						ts := spec.(*ast.TypeSpec)
						if it, ok := ts.Type.(*ast.InterfaceType); ok && inEffectsTop {
							ifaces[ts.Name.Name] = it
						}
						switch {
						case inEffectsTop && ts.Name.Name == "Host":
							if _, ok := ts.Type.(*ast.InterfaceType); !ok {
								return m, fmt.Errorf("codeshape: %s: effects.Host is not an interface", rel)
							}
							hostFound = true
						case inEffectsTop && ts.Name.Name == "Ctx":
							named, embeds, err := structFields(ts, rel)
							if err != nil {
								return m, err
							}
							ctxFound = true
							m.CtxFields, m.CtxEmbeds = named, embeds
						case path.Dir(rel) == "rules/trigmatch" && ts.Name.Name == "Board":
							it, ok := ts.Type.(*ast.InterfaceType)
							if !ok {
								return m, fmt.Errorf("codeshape: %s: trigmatch.Board is not an interface", rel)
							}
							for _, fld := range it.Methods.List {
								if len(fld.Names) == 0 {
									return m, fmt.Errorf("codeshape: %s: trigmatch.Board embeds %s; list its methods instead so the ratchet sees them", rel, types.ExprString(fld.Type))
								}
								m.TrigmatchBoardMethods += len(fld.Names)
							}
						case dir == "rules" && ts.Name.Name == "resumePoint":
							named, embeds, err := structFields(ts, rel)
							if err != nil {
								return m, err
							}
							if rpFound {
								return m, fmt.Errorf("codeshape: %s: a second resumePoint struct", rel)
							}
							rpFound = true
							m.ResumePointFields = named + embeds
						}
					}
				}
			}
			inEffects := dir == "effects"
			m.ChangeZoneLeaks = append(m.ChangeZoneLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{ChangeZoneCompilerFile, ChangeZoneFiles, ChangeZoneOnlyKeys})...)
			m.ChangeZoneAllLeaks = append(m.ChangeZoneAllLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{ChangeZoneAllCompilerFile, ChangeZoneAllFiles, ChangeZoneAllOnlyKeys})...)
			m.AttachLeaks = append(m.AttachLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{AttachCompilerFile, AttachFiles, AttachOnlyKeys})...)
			m.CharmLeaks = append(m.CharmLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{CharmCompilerFile, CharmFiles, CharmOnlyKeys})...)
			m.PumpLeaks = append(m.PumpLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{PumpCompilerFile, PumpFiles, PumpOnlyKeys})...)
			m.DrawLeaks = append(m.DrawLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{DrawCompilerFile, DrawFiles, DrawOnlyKeys})...)
			m.ReplaceEffectLeaks = append(m.ReplaceEffectLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{ReplaceEffectCompilerFile, ReplaceEffectFiles, ReplaceEffectOnlyKeys})...)
			m.ManaLeaks = append(m.ManaLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{ManaCompilerFile, ManaFiles, ManaOnlyKeys})...)
			m.ManaReflectedLeaks = append(m.ManaReflectedLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{ManaReflectedCompilerFile, ManaReflectedFiles, ManaReflectedOnlyKeys})...)
			m.DealDamageLeaks = append(m.DealDamageLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{DealDamageCompilerFile, DealDamageFiles, DealDamageOnlyKeys})...)
			m.PutCounterLeaks = append(m.PutCounterLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{PutCounterCompilerFile, PutCounterFiles, PutCounterOnlyKeys})...)
			m.EffectLeaks = append(m.EffectLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{EffectCompilerFile, EffectFiles, EffectOnlyKeys})...)
			m.DelayedTriggerLeaks = append(m.DelayedTriggerLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{DelayedTriggerCompilerFile, DelayedTriggerFiles, DelayedTriggerOnlyKeys})...)
			m.CopyPermanentLeaks = append(m.CopyPermanentLeaks, paramLeaks(fset, f, rel,
				TypedParamCompiler{CopyPermanentCompilerFile, CopyPermanentFiles, CopyPermanentOnlyKeys})...)
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.TypeAssertExpr:
					if inEffects {
						switch t := x.Type.(type) {
						case *ast.InterfaceType:
							m.HostOptionalAssertions++
						case *ast.Ident:
							assertedNames = append(assertedNames, t.Name)
						}
					}
				case *ast.IndexExpr:
					if lit, ok := x.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING && isParams(x.X) {
						m.StringParamReads++
						if k, err := strconv.Unquote(lit.Value); err == nil {
							keys[k] = true
						}
					}
				case *ast.CaseClause:
					for _, e := range x.List {
						if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
							m.StringCaseLiterals++
						}
					}
				}
				return true
			})
		}
	}
	switch {
	case !hostFound:
		return m, fmt.Errorf("codeshape: no `type Host interface` in effects/; update codeshape if it moved")
	case !ctxFound:
		return m, fmt.Errorf("codeshape: no `type Ctx struct` in effects/; update codeshape if it moved")
	case !rpFound:
		return m, fmt.Errorf("codeshape: no `type resumePoint struct` under rules/; update codeshape if it moved or was retired")
	}
	if err := measureHost(ifaces, &m); err != nil {
		return m, err
	}
	roles := map[string]bool{"Host": true}
	for _, fld := range ifaces["Host"].Methods.List {
		if id, ok := fld.Type.(*ast.Ident); ok {
			roles[id.Name] = true
		}
	}
	for _, name := range assertedNames {
		if ifaces[name] != nil && !roles[name] {
			m.HostOptionalAssertions++
		}
	}
	m.StringParamKeys = len(keys)
	m.ChangeZoneParamLeaks, m.ChangeZoneLeaks = finishLeaks(m.ChangeZoneLeaks)
	m.ChangeZoneAllParamLeaks, m.ChangeZoneAllLeaks = finishLeaks(m.ChangeZoneAllLeaks)
	m.AttachParamLeaks, m.AttachLeaks = finishLeaks(m.AttachLeaks)
	m.CharmParamLeaks, m.CharmLeaks = finishLeaks(m.CharmLeaks)
	m.PumpParamLeaks, m.PumpLeaks = finishLeaks(m.PumpLeaks)
	m.DrawParamLeaks, m.DrawLeaks = finishLeaks(m.DrawLeaks)
	m.ReplaceEffectParamLeaks, m.ReplaceEffectLeaks = finishLeaks(m.ReplaceEffectLeaks)
	m.ManaParamLeaks, m.ManaLeaks = finishLeaks(m.ManaLeaks)
	m.ManaReflectedParamLeaks, m.ManaReflectedLeaks = finishLeaks(m.ManaReflectedLeaks)
	m.DealDamageParamLeaks, m.DealDamageLeaks = finishLeaks(m.DealDamageLeaks)
	m.PutCounterParamLeaks, m.PutCounterLeaks = finishLeaks(m.PutCounterLeaks)
	m.EffectParamLeaks, m.EffectLeaks = finishLeaks(m.EffectLeaks)
	m.DelayedTriggerParamLeaks, m.DelayedTriggerLeaks = finishLeaks(m.DelayedTriggerLeaks)
	m.CopyPermanentParamLeaks, m.CopyPermanentLeaks = finishLeaks(m.CopyPermanentLeaks)
	m.FuncsOver300 = len(m.LongFuncs)
	sort.Slice(m.LongFuncs, func(i, j int) bool {
		a, b := m.LongFuncs[i], m.LongFuncs[j]
		if a.Lines != b.Lines {
			return a.Lines > b.Lines
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	if m.LongFuncs == nil {
		m.LongFuncs = []Func{}
	}
	return m, nil
}

// measureHost fills the Host metrics from package effects' interface types.
// An embedded interface that is not one of them (a qualified or unknown name)
// is an error, so a role moved out of the package cannot silently shrink the
// count.
func measureHost(ifaces map[string]*ast.InterfaceType, m *Metrics) error {
	host := ifaces["Host"]
	for _, fld := range host.Methods.List {
		if _, isFunc := fld.Type.(*ast.FuncType); isFunc {
			m.HostDirectMethods += len(fld.Names)
			continue
		}
		m.HostEmbeds++
		name, ok := fld.Type.(*ast.Ident)
		if !ok || ifaces[name.Name] == nil {
			return fmt.Errorf("codeshape: effects.Host embeds %s, which is not an interface declared in package effects", types.ExprString(fld.Type))
		}
		set := map[string]bool{}
		if err := methodSet(ifaces, name.Name, set, 0); err != nil {
			return err
		}
		m.HostRoleMaxMethods = max(m.HostRoleMaxMethods, len(set))
	}
	set := map[string]bool{}
	if err := methodSet(ifaces, "Host", set, 0); err != nil {
		return err
	}
	m.HostMethods = len(set)
	return nil
}

// methodSet adds the method names of interface name (and of everything it
// embeds) to set.
func methodSet(ifaces map[string]*ast.InterfaceType, name string, set map[string]bool, depth int) error {
	it := ifaces[name]
	if it == nil || depth > 16 {
		return fmt.Errorf("codeshape: cannot resolve interface %s in package effects", name)
	}
	for _, fld := range it.Methods.List {
		if _, isFunc := fld.Type.(*ast.FuncType); isFunc {
			for _, n := range fld.Names {
				set[n.Name] = true
			}
			continue
		}
		emb, ok := fld.Type.(*ast.Ident)
		if !ok {
			return fmt.Errorf("codeshape: interface %s embeds %s, which is not an interface declared in package effects", name, types.ExprString(fld.Type))
		}
		if err := methodSet(ifaces, emb.Name, set, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// isCtxConstructorFile reports whether rel is one of CtxConstructorFiles.
func isCtxConstructorFile(rel string) bool {
	for _, c := range CtxConstructorFiles {
		if rel == c {
			return true
		}
	}
	return false
}

// countCtxLiterals adds f's context composite literals to m. inEffects is
// true for a file of package effects itself, where the types are named bare.
func countCtxLiterals(f *ast.File, inEffects bool, m *Metrics) {
	// The file's local names for the effects package (an import may be
	// aliased, or repeated under two names).
	var locals []string
	for _, imp := range f.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err == nil && path == effectsImportPath {
			name := "effects"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			locals = append(locals, name)
		}
	}
	isEffects := func(name string) bool {
		for _, l := range locals {
			if l == name && l != "_" && l != "." {
				return true
			}
		}
		return false
	}
	// ctxType names the context type expr denotes ("" for any other type),
	// looking through one pointer for an elided &T element.
	ctxType := func(expr ast.Expr) string {
		if s, ok := expr.(*ast.StarExpr); ok {
			expr = s.X
		}
		switch x := expr.(type) {
		case *ast.Ident:
			if inEffects {
				return ctxTypeName(x.Name)
			}
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && isEffects(id.Name) {
				return ctxTypeName(x.Sel.Name)
			}
		}
		return ""
	}
	tally := func(name string) {
		switch name {
		case "Ctx":
			m.CtxLiterals++
		case "SpecContext":
			m.SpecContextLiterals++
		case "TriggerContext":
			m.TriggerContextLiterals++
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok || cl.Type == nil {
			return true
		}
		tally(ctxType(cl.Type))
		// A type-elided element of a slice, array or map literal of a
		// context type is a literal of that type too.
		var elem ast.Expr
		switch t := cl.Type.(type) {
		case *ast.ArrayType:
			elem = t.Elt
		case *ast.MapType:
			elem = t.Value
		}
		if name := ctxType(elem); elem != nil && name != "" {
			for _, el := range cl.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					el = kv.Value
				}
				if u, ok := el.(*ast.UnaryExpr); ok && u.Op == token.AND {
					el = u.X
				}
				if inner, ok := el.(*ast.CompositeLit); ok && inner.Type == nil {
					tally(name)
				}
			}
		}
		return true
	})
}

func ctxTypeName(name string) string {
	switch name {
	case "Ctx", "SpecContext", "TriggerContext":
		return name
	}
	return ""
}

// goFiles returns the non-test .go files under root/dir, repo-relative and
// slash-separated, sorted.
func goFiles(root, dir string) ([]string, error) {
	var out []string
	base := filepath.Join(root, dir)
	if _, err := os.Stat(base); err != nil {
		return nil, fmt.Errorf("codeshape: %w (is %q the module root?)", err, root)
	}
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != base && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

func structFields(ts *ast.TypeSpec, rel string) (named, embeds int, err error) {
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return 0, 0, fmt.Errorf("codeshape: %s: %s is not a struct", rel, ts.Name.Name)
	}
	for _, fld := range st.Fields.List {
		if len(fld.Names) == 0 {
			embeds++
		} else {
			named += len(fld.Names)
		}
	}
	return named, embeds, nil
}

// finishLeaks sorts a leak list and returns its count, never a nil list.
func finishLeaks(l []string) (int, []string) {
	sort.Strings(l)
	if l == nil {
		l = []string{}
	}
	return len(l), l
}

// paramLeaks returns f's parameter reads that leak past tc's compiler (see
// Metrics.ChangeZoneParamLeaks): any read in one of tc's own files, and a read
// of one of tc's only-keys anywhere else.
func paramLeaks(fset *token.FileSet, f *ast.File, rel string, tc TypedParamCompiler) []string {
	if rel == tc.CompilerFile {
		return nil
	}
	var out []string
	own := false
	for _, cz := range tc.Files {
		if rel == cz {
			own = true
		}
	}
	only := map[string]bool{}
	for _, k := range tc.OnlyKeys {
		only[k] = true
	}
	writes := map[ast.Node]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok {
			for _, l := range as.Lhs {
				writes[l] = true
			}
		}
		return true
	})
	leak := func(pos token.Pos, key string) {
		if own || only[key] {
			out = append(out, fmt.Sprintf("%s:%d %s", rel, fset.Position(pos).Line, key))
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.IndexExpr:
			if lit, ok := x.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING && isParams(x.X) && !writes[x] {
				if k, err := strconv.Unquote(lit.Value); err == nil {
					leak(x.Pos(), k)
				}
			}
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			name := ""
			if ok {
				name = sel.Sel.Name
			} else if id, isID := x.Fun.(*ast.Ident); isID {
				name = id.Name
			}
			switch name {
			case "Param", "ParamStr", "HasParam":
				if len(x.Args) == 1 {
					if ks, isSel := x.Args[0].(*ast.SelectorExpr); isSel && strings.HasPrefix(ks.Sel.Name, "PK") {
						leak(x.Pos(), strings.TrimPrefix(ks.Sel.Name, "PK"))
					}
				}
			case "Num", "NumResolved", "NumResolvedStrict", "NumForObject":
				if len(x.Args) >= 4 {
					if lit, isLit := x.Args[3].(*ast.BasicLit); isLit && lit.Kind == token.STRING {
						if k, err := strconv.Unquote(lit.Value); err == nil {
							leak(x.Pos(), k)
						}
					}
				}
			}
		}
		return true
	})
	return out
}

// isParams reports whether expr names a string-keyed parameter map: an
// identifier or selector whose name ends in Params (sa.Params,
// ce.RestrictParams, ce.ReplacementParams, ...).
func isParams(expr ast.Expr) bool {
	switch x := expr.(type) {
	case *ast.Ident:
		return strings.HasSuffix(x.Name, "Params")
	case *ast.SelectorExpr:
		return strings.HasSuffix(x.Sel.Name, "Params")
	}
	return false
}

// receiverBase is the receiver's base type name ("Engine" for both
// `(e *Engine)` and `(e Engine)`), or "" for a free function.
func receiverBase(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	t := fd.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	switch x := t.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr: // generic receiver T[P]
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// QualName renders a declaration the way a review names it: "(*Engine).Ask"
// for a pointer method, "(T).M" for a value method, bare for a function.
func QualName(fd *ast.FuncDecl) string {
	base := receiverBase(fd)
	if base == "" {
		return fd.Name.Name
	}
	if _, ptr := fd.Recv.List[0].Type.(*ast.StarExpr); ptr {
		return "(*" + base + ")." + fd.Name.Name
	}
	return "(" + base + ")." + fd.Name.Name
}
