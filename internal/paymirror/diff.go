package paymirror

import (
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Diff is one field-level difference between the two engines a mirror check
// compares. Path is the full reflective path from the engine root
// ("G.Objs[41].Tapped"); A and B are bounded renderings of the two values.
type Diff struct {
	Path string `json:"path"`
	A    string `json:"a"`
	B    string `json:"b"`
}

// maxDiffs bounds one comparison. A divergence that reaches the cap is still
// reported (the signature is computed from what was collected), but a runaway
// structural difference cannot turn one check into a multi-megabyte report.
const maxDiffs = 64

// excludedField names one struct field the equivalence projection leaves out,
// with the reason recorded in the package documentation (see doc.go). The
// key is "<package>.<Type>.<field>" so a same-named field on another type is
// never excluded by accident.
type excludedField struct{ owner, field string }

// excluded lists the rules.Engine fields that are NOT game state: the event
// log (compared separately and semantically, see events.go), per-decision
// memo caches, scratch buffers reused across calls, and counters that tally
// the intent stream rather than the game. Every entry is justified in
// doc.go's exclusion table; keep the two in step.
var excluded = map[excludedField]bool{
	// The log is compared semantically (compareEvents): Seq, DecisionAsk,
	// DecisionMade and priority-reset events legitimately differ because
	// the manual route answers more decisions.
	{"rules.Engine", "L"}: true,
	// The pending decision carries a Seq and Seq-bound payment IDs; it is
	// compared by comparePending with those masked.
	{"rules.Engine", "pending"}: true,
	// Derived-read memo caches and their generation counters: pure caches
	// of reads of G, keyed by an epoch that advances with every ask.
	{"rules.Engine", "derivedMemo"}:          true,
	{"rules.Engine", "derivedMemoStack"}:     true,
	{"rules.Engine", "derivedMemoDepth"}:     true,
	{"rules.Engine", "derivedMemoGen"}:       true,
	{"rules.Engine", "derivedMemoTail"}:      true,
	{"rules.Engine", "derivedMemoAliasFrom"}: true,
	{"rules.Engine", "derivedMemoAliasTo"}:   true,
	{"rules.Engine", "derivedKW"}:            true,
	{"rules.Engine", "derivedTypes"}:         true,
	{"rules.Engine", "derivedDepth"}:         true,
	{"rules.Engine", "derivedPTFrames"}:      true,
	{"rules.Engine", "boardStaticsCache"}:    true,
	{"rules.Engine", "activeStaticsCache"}:   true,
	{"rules.Engine", "mayPlaysCache"}:        true,
	// The posed decision's shared potential walk (potential_walk_cache.go):
	// keyed by an ask serial and the log; Clone copies none.
	{"rules.Engine", "potentialWalk"}:       true,
	{"rules.Engine", "potentialAskSerial"}:  true,
	{"rules.Engine", "potentialWalkDepth"}:  true,
	{"rules.Engine", "potentialFullDemand"}: true,
	{"rules.Engine", "crossWalkRetires"}:    true,
	{"rules.Engine", "priorityWalk"}:        true,
	// Layer/static rebuild caches keyed by epoch/version counters.
	{"rules.Engine", "staticContinuous"}: true,
	{"rules.Engine", "staticEpoch"}:      true,
	{"rules.Engine", "staticVersion"}:    true,
	{"rules.Engine", "staticObjs"}:       true,
	{"rules.Engine", "staticBuildSeq"}:   true,
	{"rules.Engine", "staticQueueBuf"}:   true,
	// The static memo's gate records (static_gatememo.go): carried only
	// with a copied memo, otherwise empty in a clone.
	{"rules.Engine", "staticGates"}:      true,
	{"rules.Engine", "staticGatesKnown"}: true,
	{"rules.Engine", "activeBuf"}:        true,
	{"rules.Engine", "activeEpoch"}:      true,
	{"rules.Engine", "activeVersion"}:    true,
	{"rules.Engine", "activeDepth"}:      true,
	{"rules.Engine", "activeObjs"}:       true,
	{"rules.Engine", "activeBuildSeq"}:   true,
	{"rules.Engine", "activeStaticSeq"}:  true,
	{"rules.Engine", "activeKWHeads"}:    true,
	{"rules.Engine", "renames"}:          true,
	{"rules.Engine", "renameEpoch"}:      true,
	{"rules.Engine", "renameVersion"}:    true,
	{"rules.Engine", "renameBuilding"}:   true,
	{"rules.Engine", "layer4Types"}:      true,
	{"rules.Engine", "typesEpoch"}:       true,
	{"rules.Engine", "typesVersion"}:     true,
	{"rules.Engine", "typesObjs"}:        true,
	{"rules.Engine", "typesBuilding"}:    true,
	// The layer-4 table's incremental state and statics-probe cache
	// (layer4types.go), keyed by log length / continuousVersion / object
	// count; Clone copies none, so a cloned route rebuilds them from its own
	// fork point. typesVisited/typesIncrBuilds count rebuild work, not the
	// game.
	{"rules.Engine", "typesIncrReady"}:    true,
	{"rules.Engine", "typesSelfOnly"}:     true,
	{"rules.Engine", "typesSrcs"}:         true,
	{"rules.Engine", "typesMayDiffer"}:    true,
	{"rules.Engine", "typesTouch"}:        true,
	{"rules.Engine", "typesAct"}:          true,
	{"rules.Engine", "typesVisited"}:      true,
	{"rules.Engine", "typesProbe"}:        true,
	{"rules.Engine", "typesProbeReady"}:   true,
	{"rules.Engine", "typesProbeTrue"}:    true,
	{"rules.Engine", "typesProbeEpoch"}:   true,
	{"rules.Engine", "typesProbeVersion"}: true,
	{"rules.Engine", "typesProbeObjs"}:    true,
	{"rules.Engine", "typesIncrBuilds"}:   true,
	{"rules.Engine", "sbaQuiet"}:          true,
	{"rules.Engine", "sbaUnquiet"}:        true,
	// Incremental scans and census caches keyed by the log length or a
	// continuous-registry version (the registry itself, e.continuous, is
	// compared; the version only invalidates caches built over it).
	{"rules.Engine", "ascend"}:            true,
	{"rules.Engine", "storied"}:           true,
	{"rules.Engine", "turnsTaken"}:        true,
	{"rules.Engine", "turnsTakenEpoch"}:   true,
	{"rules.Engine", "turnStartTurns"}:    true,
	{"rules.Engine", "turnStartEpoch"}:    true,
	{"rules.Engine", "continuousVersion"}: true,
	// Trigger-scan lookup caches keyed by compiled face pointer.
	{"rules.Engine", "triggerEventMasks"}:  true,
	{"rules.Engine", "triggerObjectMasks"}: true,
	{"rules.Engine", "trigZones"}:          true,
	{"rules.Engine", "trigZonesEp"}:        true,
	{"rules.Engine", "trigFaceZones"}:      true,
	// The exact-signature cache, summary generation, whole-board plan and
	// last walk's signature union (trigger_kinds.go, trigger_plan.go), and
	// the granted-trigger proof (trigger_grantfree.go, which Clone copies
	// with its registry header reset): scratch validated on every use.
	{"rules.Engine", "trigFaceKinds"}:   true,
	{"rules.Engine", "trigZoneGen"}:     true,
	{"rules.Engine", "trigPlan"}:        true,
	{"rules.Engine", "trigWalkUnion"}:   true,
	{"rules.Engine", "trigWalkUnionOK"}: true,
	{"rules.Engine", "trigGrant"}:       true,
	{"rules.Engine", "phaseSpecs"}:      true,
	// The per-face text-scan memo (face_scan_memo.go) and active()'s
	// per-build digest (active_summary.go): pure caches, Clone copies none.
	{"rules.Engine", "faceScans"}: true,
	{"rules.Engine", "activeSum"}: true,
	// Characteristics' printed fast-path digest (derived_printed.go), the
	// activeSum shape.
	{"rules.Engine", "charsSum"}: true,
	// The replacement-source walk's zone summaries (repl_zoneskip.go), the
	// trigZones shape: scratch validated on every use; Clone copies none.
	{"rules.Engine", "replZones"}:   true,
	{"rules.Engine", "replZonesEp"}: true,
	// The whole-arena replacement event mask (repl_arena_mask.go): a
	// superset cache with its catch-up watermarks, rebuilt on first use.
	{"rules.Engine", "replArena"}: true,
	// The off-battlefield static-source walks' zone summaries
	// (static_zoneskip.go), the same shape.
	{"rules.Engine", "staticZones"}:   true,
	{"rules.Engine", "staticZonesEp"}: true,
	// The payment-plan interference carrier memo (payment_plan_interference.go),
	// keyed by object-arena size and log length; Clone copies none.
	{"rules.Engine", "paymentPlanCarriers"}:       true,
	{"rules.Engine", "paymentPlanCarriersObjs"}:   true,
	{"rules.Engine", "paymentPlanCarriersEvents"}: true,
	{"rules.Engine", "paymentPlanCarriersValid"}:  true,
	// The payment planner's kept and recycled query scopes and the
	// incremental zone-entry index (rules/payment_plan_search.go,
	// rules/payment_zone_entry.go): pure caches of reads of G and the log,
	// validated on every use; Clone copies none.
	{"rules.Engine", "paymentPlanQueryKept"}:      true,
	{"rules.Engine", "paymentPlanQueryKeptStamp"}: true,
	{"rules.Engine", "paymentPlanQueryFree"}:      true,
	{"rules.Engine", "zoneEntry"}:                 true,
	// The priority walk's pool-independent block record for the potential
	// walk (rules/walk_block_reuse.go) and its served-block counter: a walk
	// cache keyed like priorityWalk, and a diagnostic; Clone copies none.
	{"rules.Engine", "walkRec"}:           true,
	{"rules.Engine", "walkReuse"}:         true,
	{"rules.Engine", "walkBlocksServed"}:  true,
	{"rules.Engine", "walkMembersServed"}: true,
	{"rules.Engine", "potentialManaRec"}:  true,
	{"rules.Engine", "walkRecDemand"}:     true,
	// Scratch buffers reused across calls (contents after use are garbage).
	{"rules.Engine", "legalOptBuf"}:  true,
	{"rules.Engine", "manaAbBuf"}:    true,
	{"rules.Engine", "manaLabels"}:   true,
	{"rules.Engine", "intentBuf"}:    true,
	{"rules.Engine", "sbaIDBuf"}:     true,
	{"rules.Engine", "graveCandBuf"}: true,
	{"rules.Engine", "foreachBuf"}:   true,
	// The hypothetical-clone and read-scratch pool (rules/hypclone.go):
	// recycled Spares and per-call scratch (PotentialMana's among them),
	// owner-guarded; Clone copies none.
	{"rules.Engine", "hypSpares"}: true,
	// targetSpecContext's reusable Resolve records (trigger_referents.go),
	// a stack that is free at every intent boundary; Clone starts a fresh
	// one, so the recycled records a live engine keeps are not game state.
	{"rules.Engine", "specEnvs"}:     true,
	{"rules.Engine", "specEnvDepth"}: true,
	// The DiscardedAll matcher's FirstTime$ scratch (rules/engine.go): written
	// by discardedAllMatches on every match and read by the trigger dispatcher
	// immediately after, never before a write; Clone copies it as false, so a
	// live engine still holding the last match's value read "true vs false"
	// against its control clone (round-8 paymirror random4, seed 8529).
	{"rules.Engine", "discardAllFirstTime"}: true,
	// Intent-stream watchdogs and counters: the manual route answers more
	// decisions by construction, so these count the route, not the game.
	{"rules.Engine", "loop"}:     true,
	{"rules.Engine", "askCount"}: true,
	// legalActionWalks is legalActionsPriced's diagnostic call counter: the
	// float route re-prices every offer against a hypothetical floating pool
	// the control route never opens, so it counts the route's work, not the
	// game (ticket cli-20260927T103840Z-0fb8a354: the field was read by this
	// differ in round 1 and every route pair mismatched on it).
	{"rules.Engine", "legalActionWalks"}: true,
	// Harness-only observers (rules/clone.go: Clone deliberately copies
	// neither). cmd/cardfuzz installs ManaAbilityHook on its LIVE engine, so
	// CheckLive's control -- the live run A against a clone replaying it --
	// read "ManaAbilityHook <func> vs nil" on every planned cast (round-5
	// cardfuzz mirror diags); botbench's stats sink is the same shape. Both
	// emit nothing and mutate nothing, so they are not game state.
	{"rules.Engine", "ManaAbilityHook"}: true,
	{"rules.Engine", "paymentStats"}:    true,
	// The Derived memo's cross-walk key and active()'s double buffer
	// (derived_transparent.go): derivedSeq moves with activeBuildSeq, and
	// derivedPrev*/derivedTouched/activeBufAlt are the previous build's key
	// and list. Never cloned, so a control clone restarts them at zero while
	// the live run A has counted its own rebuilds (round-7 cardfuzz seed
	// 16178228601564090929: derivedSeq 9 vs 1).
	{"rules.Engine", "derivedSeq"}:         true,
	{"rules.Engine", "activeBufAlt"}:       true,
	{"rules.Engine", "derivedPrevEpoch"}:   true,
	{"rules.Engine", "derivedPrevVersion"}: true,
	{"rules.Engine", "derivedPrevObjs"}:    true,
	{"rules.Engine", "derivedTouched"}:     true,
	// The battlefield-membership half of that key and the zone-ledger
	// length it was taken at, and what active()'s list was assembled from
	// (active_same.go); never cloned either.
	{"rules.Engine", "derivedBFSeq"}:       true,
	{"rules.Engine", "derivedPrevEntered"}: true,
	{"rules.Engine", "activeList"}:         true,
	// The rename table's arena-size key and its derivedSeq stamp
	// (setname.go), the renames/* cache keys.
	{"rules.Engine", "renameObjs"}:  true,
	{"rules.Engine", "renameDSeq"}:  true,
	{"rules.Engine", "renameBFSeq"}: true,
	// checkFaceTriggers' zero-interest no-op memo key (log length, arena
	// size, registry version); Clone leaves it zero.
	{"rules.Engine", "trigZeroNoopEp"}:    true,
	{"rules.Engine", "trigZeroNoopObjs"}:  true,
	{"rules.Engine", "trigZeroNoopVer"}:   true,
	{"rules.Engine", "trigZeroNoopKinds"}: true,
	// attackOffers' layer-inert reuse (attack_cost.go): the last offer list
	// and its key; Clone leaves them zero.
	{"rules.Engine", "atkOffers"}:       true,
	{"rules.Engine", "atkOffersEp"}:     true,
	{"rules.Engine", "atkOffersVer"}:    true,
	{"rules.Engine", "atkOffersObjs"}:   true,
	{"rules.Engine", "atkOffersActive"}: true,
	// Recycled storage owned by one engine: the trigger-window snapshot
	// pool (trigger_snapshot_pool.go), the reusable look-back observer
	// Engine and its owner/busy guard (checkTriggers), and the posed-decision
	// arena (decision_arena.go). They hold capacity, never game state; Clone
	// leaves them nil or adopts a spent engine's cleared ones from a Spare.
	{"rules.Engine", "snapPool"}:      true,
	{"rules.Engine", "lookBack"}:      true,
	{"rules.Engine", "lookBackOwner"}: true,
	{"rules.Engine", "lookBackBusy"}:  true,
	{"rules.Engine", "preview"}:       true,
	{"rules.Engine", "previewOwner"}:  true,
	{"rules.Engine", "previewBusy"}:   true,
	{"rules.Engine", "decArena"}:      true,
	// The offer walk's incremental log-derived indexes (legal_walk_scratch.go):
	// each is a pure function of the log prefix its watermark names, and the
	// log itself is compared semantically. The manual route answers more
	// decisions, so its watermark names a longer prefix of an equivalent log
	// (round-9 cardfuzz seed 8175: airbendFolded).
	{"rules.Engine", "legalScratch"}: true,
}

// The engine_struct embedding refactor (2026-09-30) moved most of the Engine
// struct's fields onto anonymous embedded cluster structs (engineScratch,
// engineResolution, ...). The differ's walk visits those clusters under their
// own "rules.<Type>" keys, and a promoted field's reflective path grew a
// cluster prefix -- so the table above, keyed {"rules.Engine", field} exactly
// as doc.go's exclusion table documents it, stopped matching and every
// excluded cache started being compared again (the module-gate failure this
// resolution closed: all ten failing route pairs diverged on
// engineScratch.derivedMemo / engineDerivedTables.types* /
// engineLayerCaches.activeBuildSeq). The two tables below, built once by
// reflect over rules.Engine, keep the documented spellings working however
// the fields are clustered:
//
//   - engineExcludedByType maps each struct type reachable from Engine by
//     anonymous embedding to its fields that carry an exclusion entry,
//     valued by the table key the hit is credited to;
//   - engineClusterPrefixes holds each cluster's "<name>." path prefix, so
//     engineFieldPath can restore the promotion-flat spelling the float
//     route's path matchers read.
var (
	engineExcludedByType, engineClusterPrefixes = buildEngineFieldMaps()
)

func buildEngineFieldMaps() (map[string]map[string]excludedField, []string) {
	excl := map[string]map[string]excludedField{}
	var prefixes []string
	var walkType func(t reflect.Type)
	walkType = func(t reflect.Type) {
		owner := typeKey(t)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				prefixes = append(prefixes, f.Name+".")
				walkType(f.Type)
				continue
			}
			key := excludedField{"rules.Engine", f.Name}
			if excluded[key] {
				if excl[owner] == nil {
					excl[owner] = map[string]excludedField{}
				}
				excl[owner][f.Name] = key
			}
		}
	}
	walkType(reflect.TypeOf(rules.Engine{}))
	return excl, prefixes
}

// excludedAt reports the exclusion table entry that leaves field f of the
// struct type t the walk is visiting out of the comparison, with the table
// key the hit is credited to (so TestExclusionTableNamesEngineFields keeps
// proving every entry fires). The direct key covers Engine's own fields;
// engineExcludedByType resolves the table through the anonymous embedded
// clusters, whose fields the walk sees under the cluster type's own key.
func excludedAt(t reflect.Type, f reflect.StructField) (excludedField, bool) {
	key := excludedField{typeKey(t), f.Name}
	if excluded[key] {
		return key, true
	}
	if table, ok := engineExcludedByType[key.owner]; ok {
		if flat, ok := table[f.Name]; ok {
			return flat, true
		}
	}
	return excludedField{}, false
}

// engineFieldPath strips the anonymous cluster prefix a promoted field's
// reflective path grew when the Engine struct's clusters were embedded, so
// the path matchers below keep spelling the field names as they read.
func engineFieldPath(path string) string {
	for _, p := range engineClusterPrefixes {
		if rest, ok := strings.CutPrefix(path, p); ok {
			return rest
		}
	}
	return path
}

// differ walks two values of the same type in lockstep and records every
// primitive-level difference. It reads unexported fields through reflect
// (read-only), so the whole rules.Engine can be compared from outside the
// rules package without an accessor in the engine.
type differ struct {
	diffs   []Diff
	visited map[[2]uintptr]bool
	// excludedHits counts the excluded fields actually encountered, so a test
	// can prove the exclusion table is not stale (a renamed field would
	// silently stop being excluded and start being compared).
	excludedHits map[excludedField]int
	// relax, when non-nil, is the float route's cost-move reorder allowance
	// (floatReorder); nil compares every field exactly.
	relax *floatReorder
	// skipPath, when non-nil, leaves out every path it accepts (and all
	// beneath it); floatTriggerOnly uses it after proving those paths hold
	// the same content under another order or other object identities.
	skipPath func(path string) bool
}

func newDiffer() *differ {
	return &differ{visited: make(map[[2]uintptr]bool), excludedHits: make(map[excludedField]int)}
}

// floatReorder is the float route's allowance for a planned activation whose
// COST moves its source (Lotus Petal's and a Treasure's "{T}, Sacrifice",
// an Eldrazi Spawn's "Sacrifice"). Run A pays inside the cast's CR 601.2g
// window, after the spell moved to the stack; the float route activates at
// priority, before the cast begins -- the same reason the event ORDER is
// reported but not required (doc.go, Equivalence 3.). Three state fields
// record that order rather than a game fact, and nothing else:
//
//   - G.Entered, this turn's zone-entry list: the same entries in a
//     different order (the sacrifice before vs after the spell's own entry);
//   - the planned spell's PreStackEnteredLen, the CR 733.1 reverse boundary
//     into that list, which counts the entries made before the spell moved --
//     on the spell itself and on any snapshot of it (a trigger's LKI copy of
//     the permanent it became);
//   - damageSourceLKI[spell][source]: a departure snapshot every waiting
//     stack object takes of a departing object, so a source sacrificed while
//     the spell waits on the stack is snapshotted for it and one sacrificed
//     before the cast is not. The same holds for every object created since
//     the fork (the spell's cast triggers, queued while run A's window was
//     open, and the stack objects and permanents they and the spell become):
//     damageSourceLKI[new][source] records the same timing and nothing else.
//
// The allowance holds only when the two entry lists are the same multiset
// (enteredReordered); any other difference in them is still reported, and the
// spell's boundary and snapshots are masked only then.
type floatReorder struct {
	enteredReordered bool
	cast             state.ObjID     // the planned spell
	castKey          string          // the spell's rendered damageSourceLKI key
	sourceKeys       map[string]bool // the planned sources' rendered keys
	// forkObjs is the object-arena size at the fork: an ObjID above it is an
	// object created since (ObjIDs are arena positions, never reused).
	forkObjs int
}

// newFloatReorder builds the allowance for comparing run A (a) with the float
// route (b) on the planned cast of cast paid by sources.
func newFloatReorder(a, b *rules.Engine, cast state.ObjID, sources []state.ObjID, forkObjs int) *floatReorder {
	r := &floatReorder{
		cast:       cast,
		castKey:    render(reflect.ValueOf(cast), 4),
		sourceKeys: make(map[string]bool, len(sources)),
		forkObjs:   forkObjs,
	}
	for _, s := range sources {
		r.sourceKeys[render(reflect.ValueOf(s), 4)] = true
	}
	// Only a genuine reorder earns the allowance: identical lists leave the
	// spell's boundary and snapshots compared exactly.
	r.enteredReordered = !slices.Equal(a.G.Entered, b.G.Entered) && sameEntryMultiset(a.G.Entered, b.G.Entered)
	return r
}

// sameEntryMultiset reports whether two zone-entry lists hold the same
// entries, in any order.
func sameEntryMultiset(a, b []state.ZoneEntry) bool {
	if len(a) != len(b) {
		return false
	}
	n := make(map[state.ZoneEntry]int, len(a))
	for _, z := range a {
		n[z]++
	}
	for _, z := range b {
		if n[z] == 0 {
			return false
		}
		n[z]--
	}
	return true
}

// skip reports whether field f of struct value a (at path) is one the
// allowance masks: the entry list itself, or the PreStackEnteredLen of any
// state.Object value that is the planned spell or a snapshot of it.
func (r *floatReorder) skip(path string, a reflect.Value, f reflect.StructField) bool {
	if r == nil || !r.enteredReordered {
		return false
	}
	if path == "G.Entered" {
		return true
	}
	return f.Name == "PreStackEnteredLen" && a.Type() == reflect.TypeOf(state.Object{}) &&
		state.ObjID(a.FieldByName("ID").Uint()) == r.cast
}

// masksLKI reports whether damageSourceLKI is compared through walkDamageLKI.
// The field lives on the engineResolution cluster (the engine_struct
// embedding), so its path carries that prefix; engineFieldPath strips it.
func (r *floatReorder) masksLKI(path string) bool {
	return r != nil && r.enteredReordered && engineFieldPath(path) == "damageSourceLKI"
}

// walkDamageLKI is walkMap over damageSourceLKI with the planned spell's --
// and every since-the-fork object's -- snapshots of the planned sources left
// out (floatReorder); every older object's snapshots, and any object's
// snapshots of anything but a planned source, are compared exactly.
func (d *differ) walkDamageLKI(path string, a, b reflect.Value) {
	outer := func(m reflect.Value) map[string]reflect.Value {
		if m.IsNil() {
			return map[string]reflect.Value{}
		}
		return mapByKey(m)
	}
	ka, kb := outer(a), outer(b)
	keys := make([]string, 0, len(ka)+len(kb))
	for k := range ka {
		keys = append(keys, k)
	}
	for k := range kb {
		if _, ok := ka[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		va, oka := ka[k]
		vb, okb := kb[k]
		sub := path + "{" + k + "}"
		if k == d.relax.castKey || d.relax.newObjectKey(k) {
			d.walkInnerSkipping(sub, va, oka, vb, okb, d.relax.sourceKeys)
			continue
		}
		switch {
		case oka && okb:
			d.walk(sub, va, vb)
		case oka:
			d.add(sub, va, reflect.Value{})
		default:
			d.add(sub, reflect.Value{}, vb)
		}
		if d.full() {
			return
		}
	}
}

// newObjectKey reports whether a rendered damageSourceLKI key names an object
// created after the fork.
func (r *floatReorder) newObjectKey(k string) bool {
	id, err := strconv.ParseUint(k, 10, 32)
	return err == nil && r.forkObjs > 0 && id > uint64(r.forkObjs)
}

// walkInnerSkipping compares two (possibly absent) maps entry by entry,
// leaving out the keys in skip; an absent map reads as an empty one.
func (d *differ) walkInnerSkipping(path string, a reflect.Value, oka bool, b reflect.Value, okb bool, skip map[string]bool) {
	entries := func(m reflect.Value, ok bool) map[string]reflect.Value {
		out := map[string]reflect.Value{}
		if !ok || m.IsNil() {
			return out
		}
		for k, v := range mapByKey(m) {
			if !skip[k] {
				out[k] = v
			}
		}
		return out
	}
	ka, kb := entries(a, oka), entries(b, okb)
	keys := make([]string, 0, len(ka)+len(kb))
	for k := range ka {
		keys = append(keys, k)
	}
	for k := range kb {
		if _, ok := ka[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		va, inA := ka[k]
		vb, inB := kb[k]
		sub := path + "{" + k + "}"
		switch {
		case inA && inB:
			d.walk(sub, va, vb)
		case inA:
			d.add(sub, va, reflect.Value{})
		default:
			d.add(sub, reflect.Value{}, vb)
		}
		if d.full() {
			return
		}
	}
}

func (d *differ) add(path string, a, b reflect.Value) {
	if len(d.diffs) >= maxDiffs {
		return
	}
	if d.skipPath != nil && d.skipPath(path) {
		return // a one-sided map entry under a skipped path
	}
	d.diffs = append(d.diffs, Diff{Path: path, A: render(a, 3), B: render(b, 3)})
}

func (d *differ) full() bool { return len(d.diffs) >= maxDiffs }

// typeKey is the "<pkg>.<Type>" spelling excluded uses.
func typeKey(t reflect.Type) string {
	pkg := t.PkgPath()
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		pkg = pkg[i+1:]
	}
	return pkg + "." + t.Name()
}

func (d *differ) walk(path string, a, b reflect.Value) {
	if d.full() {
		return
	}
	if d.skipPath != nil && d.skipPath(path) {
		return
	}
	if !a.IsValid() || !b.IsValid() {
		if a.IsValid() != b.IsValid() {
			d.add(path, a, b)
		}
		return
	}
	if a.Type() != b.Type() {
		d.add(path+"<type>", a, b)
		return
	}
	switch a.Kind() {
	case reflect.Bool:
		if a.Bool() != b.Bool() {
			d.add(path, a, b)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if a.Int() != b.Int() {
			d.add(path, a, b)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if a.Uint() != b.Uint() {
			d.add(path, a, b)
		}
	case reflect.Float32, reflect.Float64:
		if a.Float() != b.Float() {
			d.add(path, a, b)
		}
	case reflect.Complex64, reflect.Complex128:
		if a.Complex() != b.Complex() {
			d.add(path, a, b)
		}
	case reflect.String:
		if a.String() != b.String() {
			d.add(path, a, b)
		}
	case reflect.Array:
		for i := 0; i < a.Len(); i++ {
			d.walk(path+"["+strconv.Itoa(i)+"]", a.Index(i), b.Index(i))
		}
	case reflect.Slice:
		// nil and empty are the same game fact (an absent Remembered list is
		// an empty one); only length and element content are compared.
		if a.Len() != b.Len() {
			d.add(path+".len", a, b)
			return
		}
		if a.Len() > 0 && a.Pointer() == b.Pointer() {
			return // the same backing array: shared immutable data
		}
		for i := 0; i < a.Len(); i++ {
			d.walk(path+"["+strconv.Itoa(i)+"]", a.Index(i), b.Index(i))
		}
	case reflect.Map:
		d.walkMap(path, a, b)
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				d.add(path, a, b)
			}
			return
		}
		if a.Pointer() == b.Pointer() {
			return // shared: compiled corpus data both clones reference
		}
		key := [2]uintptr{a.Pointer(), b.Pointer()}
		if d.visited[key] {
			return
		}
		d.visited[key] = true
		d.walk(path, a.Elem(), b.Elem())
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				d.add(path, a, b)
			}
			return
		}
		d.walk(path, a.Elem(), b.Elem())
	case reflect.Struct:
		t := a.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if key, ok := excludedAt(t, f); ok {
				d.excludedHits[key]++
				continue
			}
			sub := f.Name
			if path != "" {
				sub = path + "." + f.Name
			}
			if d.relax.skip(sub, a, f) {
				continue
			}
			if d.relax.masksLKI(sub) {
				d.walkDamageLKI(engineFieldPath(sub), a.Field(i), b.Field(i))
				continue
			}
			d.walk(sub, a.Field(i), b.Field(i))
		}
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if a.IsNil() != b.IsNil() {
			d.add(path, a, b)
		}
	}
}

// walkMap pairs the two maps' entries by a canonical rendering of the key,
// never by Go's map iteration order, so the report is deterministic.
func (d *differ) walkMap(path string, a, b reflect.Value) {
	if a.Len() == 0 && b.Len() == 0 {
		return
	}
	if !a.IsNil() && a.Pointer() == b.Pointer() {
		return // one shared map object (Game.Tokens): immutable corpus data
	}
	ka := mapByKey(a)
	kb := mapByKey(b)
	keys := make([]string, 0, len(ka)+len(kb))
	for k := range ka {
		keys = append(keys, k)
	}
	for k := range kb {
		if _, ok := ka[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		va, oka := ka[k]
		vb, okb := kb[k]
		sub := path + "{" + k + "}"
		switch {
		case oka && okb:
			d.walk(sub, va, vb)
		case oka:
			d.add(sub, va, reflect.Value{})
		default:
			d.add(sub, reflect.Value{}, vb)
		}
		if d.full() {
			return
		}
	}
}

func mapByKey(m reflect.Value) map[string]reflect.Value {
	out := make(map[string]reflect.Value, m.Len())
	it := m.MapRange()
	for it.Next() {
		out[render(it.Key(), 4)] = it.Value()
	}
	return out
}

// render formats a value (exported or not) without reflect.Value.Interface,
// which panics on values read through unexported fields. depth bounds nested
// composites; a pointer renders as its pointee so two clones' equal values
// read the same.
func render(v reflect.Value, depth int) string {
	var sb strings.Builder
	renderTo(&sb, v, depth)
	s := sb.String()
	if len(s) > 240 {
		s = s[:240] + "..."
	}
	return s
}

func renderTo(sb *strings.Builder, v reflect.Value, depth int) {
	if !v.IsValid() {
		sb.WriteString("<absent>")
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		sb.WriteString(strconv.FormatBool(v.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		sb.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		sb.WriteString(strconv.FormatUint(v.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		sb.WriteString(strconv.FormatFloat(v.Float(), 'g', -1, 64))
	case reflect.String:
		sb.WriteString(strconv.Quote(v.String()))
	case reflect.Pointer:
		if v.IsNil() {
			sb.WriteString("nil")
			return
		}
		if depth <= 0 {
			fmt.Fprintf(sb, "&%s", v.Type().Elem().Name())
			return
		}
		sb.WriteString("&")
		renderTo(sb, v.Elem(), depth-1)
	case reflect.Interface:
		if v.IsNil() {
			sb.WriteString("nil")
			return
		}
		renderTo(sb, v.Elem(), depth)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			sb.WriteString("[]")
			return
		}
		sb.WriteString("[")
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				sb.WriteString(" ")
			}
			if i >= 16 || depth <= 0 {
				fmt.Fprintf(sb, "...(%d)", v.Len())
				break
			}
			renderTo(sb, v.Index(i), depth-1)
		}
		sb.WriteString("]")
	case reflect.Map:
		keys := make([]string, 0, v.Len())
		vals := make(map[string]string, v.Len())
		it := v.MapRange()
		for it.Next() {
			k := render(it.Key(), depth-1)
			keys = append(keys, k)
			vals[k] = render(it.Value(), depth-1)
		}
		sort.Strings(keys)
		sb.WriteString("map[")
		for i, k := range keys {
			if i > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(k + ":" + vals[k])
		}
		sb.WriteString("]")
	case reflect.Struct:
		t := v.Type()
		// Compiled corpus types render by name only: their content is shared
		// and immutable, and a full rendering is kilobytes.
		switch typeKey(t) {
		case "cards.Card", "cards.Face":
			if f := v.FieldByName("Name"); f.IsValid() && f.Kind() == reflect.String {
				sb.WriteString(t.Name() + "(" + strconv.Quote(f.String()) + ")")
				return
			}
		case "cards.SA":
			if f := v.FieldByName("Line"); f.IsValid() && f.Kind() == reflect.String {
				sb.WriteString("SA(" + strconv.Quote(f.String()) + ")")
				return
			}
		}
		if depth <= 0 {
			sb.WriteString(t.Name() + "{...}")
			return
		}
		sb.WriteString(t.Name() + "{")
		first := true
		for i := 0; i < t.NumField(); i++ {
			f := v.Field(i)
			if f.IsZero() {
				continue
			}
			if !first {
				sb.WriteString(" ")
			}
			first = false
			sb.WriteString(t.Field(i).Name + ":")
			renderTo(sb, f, depth-1)
		}
		sb.WriteString("}")
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if v.IsNil() {
			sb.WriteString("nil")
		} else {
			sb.WriteString("<" + v.Kind().String() + ">")
		}
	default:
		sb.WriteString("<" + v.Kind().String() + ">")
	}
}

var (
	indexRE = regexp.MustCompile(`\[\d+\]`)
	keyRE   = regexp.MustCompile(`\{[^{}]*\}`)
)

// normalizePath folds per-object indices and map keys so the same divergence
// on different objects shares one signature ("G.Objs[*].Tapped").
func normalizePath(p string) string {
	p = indexRE.ReplaceAllString(p, "[*]")
	return keyRE.ReplaceAllString(p, "{*}")
}

// floatTriggerOnly reports whether every difference between run A (a) and the
// float route (b) is the one floating's own triggered abilities make by
// reaching the stack before the cast instead of after it (CR 603.3, 117.5; see
// floatTriggerPrecedesCast), and nothing else:
//
//   - the same objects exist (the arena sizes are equal) and the objects
//     created since the fork are the same multiset of abilities -- source,
//     controller, owner, zone and ability line -- under permuted ObjIDs;
//   - the stacks agree once the since-fork objects are removed (the spell and
//     every older object keep their order), and hold the same since-fork
//     objects in any order;
//   - this turn's zone-entry lists are the same multiset once since-fork
//     ObjIDs are masked, and only then is the spell's PreStackEnteredLen
//     boundary into them left uncompared;
//   - the event multisets are equal once since-fork ObjIDs are masked (a
//     cast trigger removed for want of a target names its own permuted
//     ObjID, round-9 commander4 seed 11056);
//   - a full differ walk (with the cost-move floatReorder allowance) finds
//     nothing outside those fields and the per-object bookkeeping maps keyed
//     by a since-fork object (trigger contexts and LKI snapshots, whose
//     content followed the objects' identities).
//
// It returns "" when all of that holds, or the first failed check.
func floatTriggerOnly(a, b *rules.Engine, fork int, rep *Report) string {
	if rep == nil || rep.forkObjs <= 0 || len(a.G.Objs) != len(b.G.Objs) {
		return "object_count"
	}
	isNew := func(id state.ObjID) bool { return int(id) > rep.forkObjs }
	old := func(stack []state.ObjID) []state.ObjID {
		var out []state.ObjID
		for _, id := range stack {
			if !isNew(id) {
				out = append(out, id)
			}
		}
		return out
	}
	if !slices.Equal(old(a.G.Stack), old(b.G.Stack)) || len(a.G.Stack) != len(b.G.Stack) {
		return "stack"
	}
	newKeys := func(e *rules.Engine) map[string]int {
		out := map[string]int{}
		for i := rep.forkObjs; i < len(e.G.Objs); i++ {
			o := &e.G.Objs[i]
			line, name := "", ""
			if o.Ability != nil {
				line = o.Ability.Line
			}
			if o.Face() != nil {
				name = o.Face().Name
			}
			onStack := slices.Contains(e.G.Stack, o.ID)
			out[fmt.Sprintf("%d|%d|%d|%d|%v|%q|%q", o.Source, o.Controller, o.Owner, o.Zone, onStack, line, name)]++
		}
		return out
	}
	if !maps.Equal(newKeys(a), newKeys(b)) {
		return "new_objects"
	}
	masked := func(es []state.ZoneEntry) map[state.ZoneEntry]int {
		out := map[state.ZoneEntry]int{}
		for _, z := range es {
			if isNew(z.Obj) {
				z.Obj = 0
			}
			out[z]++
		}
		return out
	}
	if !maps.Equal(masked(a.G.Entered), masked(b.G.Entered)) {
		return "entered"
	}
	if ev := compareEvents(maskNewObjects(a.L.Events[fork:], rep.forkObjs, len(a.G.Objs)), maskNewObjects(b.L.Events[fork:], rep.forkObjs, len(b.G.Objs))); len(ev.OnlyA) > 0 || len(ev.OnlyB) > 0 {
		return "events"
	}
	keyed := []string{"triggerContexts{", "triggerLKI{", "damageSourceLKI{", "sourceLifelinkLKI{", "sourceControllerLKI{"}
	df := newDiffer()
	sources := make([]state.ObjID, 0, len(rep.Plan.Activations))
	for _, act := range rep.Plan.Activations {
		sources = append(sources, act.Source)
	}
	df.relax = newFloatReorder(a, b, rep.Object, sources, rep.forkObjs)
	// The entry lists were just proven the same multiset up to the
	// since-fork objects' identities, which is the cost-move allowance's own
	// premise (a planned source's sacrifice before vs after the spell moved).
	df.relax.enteredReordered = true
	df.skipPath = func(path string) bool {
		path = engineFieldPath(path) // a cluster-prefixed engine field path reads flat
		switch path {
		case "G.Stack", "G.Entered":
			return true
		}
		if rest, ok := strings.CutPrefix(path, "G.Objs["); ok {
			i := strings.IndexByte(rest, ']')
			if i < 0 {
				return false
			}
			k, err := strconv.Atoi(rest[:i])
			if err != nil {
				return false
			}
			if isNew(state.ObjID(k + 1)) {
				return true
			}
			return state.ObjID(k+1) == rep.Object && rest[i+1:] == ".PreStackEnteredLen"
		}
		for _, p := range keyed {
			if rest, ok := strings.CutPrefix(path, p); ok {
				i := strings.IndexByte(rest, '}')
				if i < 0 {
					return false
				}
				id, err := strconv.ParseUint(rest[:i], 10, 32)
				return err == nil && isNew(state.ObjID(id))
			}
		}
		return false
	}
	df.walk("", reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem())
	comparePending(df, a.Pending(), b.Pending())
	if len(df.diffs) > 0 {
		return "state:" + df.diffs[0].Path
	}
	return ""
}
