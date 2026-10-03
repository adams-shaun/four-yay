package rules

import (
	"fmt"
	"go/format"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The Clone generator (lasagna spec W1a step 4). The `clone:"..."` struct tags
// are the single source of truth for what Clone copies: this test renders
// clone_gen.go from them, so a new field needs a tag and nothing else, and
// TestCloneGenIsUpToDate fails when the checked-in file differs from the
// render. Regenerate with `go generate ./rules` (GORGE_GEN_CLONE=1).
//
// What is generated:
//   - cloneEngineFields: every `deep` Engine field not named in cloneHandFields
//     (the fields whose copy is conditional, re-keyed, pooled or otherwise
//     bespoke: clone.go's cloneWith keeps exactly those, each with its reason).
//   - (*cloneRemap).resumeFields: every `deep` resumePoint field.
//
// The copy of a deep field is derived from its type: scalars by value, slices
// and maps re-allocated (nil preserved), pointers to non-corpus structs
// re-allocated, pointers into the immutable compiled corpus (cards, deck)
// shared, structs by value with their reference-bearing fields deepened.
// Types whose copy carries identity or special handling are named in
// cloneTypeCopiers. A struct tagged with clone policies (the types of
// clonePolicyTypes) deepens only its `deep` fields; an untagged struct deepens
// every reference field.

// cloneHandFields names the Engine fields cloneWith still copies by hand.
var cloneHandFields = map[string]string{
	"G":                                     "built in the literal: arena recycling through the Spare",
	"L":                                     "built in the literal: log fork through the Spare",
	"rng":                                   "built in the literal",
	"engineLayerCaches.staticContinuous":    "static memo carried only when current; recycled Spare storage",
	"engineLayerCaches.staticEpoch":         "static memo",
	"engineLayerCaches.staticVersion":       "re-keyed",
	"engineLayerCaches.staticObjs":          "static memo",
	"engineLayerCaches.staticMemoGated":     "static memo",
	"engineLayerCaches.staticMemoStateRead": "static memo",
	"engineLayerCaches.staticGates":         "static memo gate records, recycled storage",
	"engineLayerCaches.staticGatesKnown":    "static memo",
	"engineLayerCaches.sbaQuiet":            "carried only when provably quiet",
	"engineDerivedTables.renames":           "table carried with its re-keyed (epoch, version)",
	"engineDerivedTables.renameEpoch":       "table key",
	"engineDerivedTables.renameVersion":     "re-keyed",
	"engineDerivedTables.renameObjs":        "table key",
	"engineDerivedTables.layer4Types":       "table carried with its re-keyed (epoch, version)",
	"engineDerivedTables.typesEpoch":        "table key",
	"engineDerivedTables.typesVersion":      "re-keyed",
	"engineDerivedTables.typesObjs":         "table key",
	"engineDerivedTables.typesIncrReady":    "carried only when ready",
	"engineDerivedTables.typesSelfOnly":     "carried only when ready",
	"engineDerivedTables.typesSrcs":         "carried only when ready",
	"engineDerivedTables.typesMayDiffer":    "carried only when ready",
	"engineDerivedTables.typesProbe":        "recycled Spare storage",
	"engineDerivedTables.typesProbeReady":   "carried only when ready",
	"engineDerivedTables.typesProbeTrue":    "carried only when ready",
	"engineDerivedTables.typesProbeEpoch":   "carried only when ready",
	"engineDerivedTables.typesProbeVersion": "re-keyed",
	"engineDerivedTables.typesProbeObjs":    "carried only when ready",
	"engineScratch.atkOffersEp":             "carried only when the memo is current",
	"engineScratch.atkOffersVer":            "re-keyed",
	"engineScratch.atkOffersObjs":           "carried only when the memo is current",
	"engineScratch.atkOffersActive":         "carried only when the memo is current",
	"engineScratch.lossProof":               "registry header re-checked once on the clone",
	"engineTriggerMaps.trigSub":             "forClone method",
	"engineDrain.pendingTriggers":           "Spare-recycled queue storage",
	"engineTriggerBatches.trigZones":        "Spare-recycled summaries",
	"engineTriggerBatches.trigZonesEp":      "paired with trigZones",
	"engineTriggerBatches.trigGrant":        "forClone method",
	"engineTriggerBatches.replZones":        "Spare-recycled summaries",
	"engineTriggerBatches.replZonesEp":      "paired with replZones",
	"engineTriggerBatches.replArena":        "shared mask",
	"engineTriggerBatches.staticZonesEp":    "carried with walkObjCls",
	"engineTriggerBatches.walkObjCls":       "carried only for an owning engine, Spare storage",
	"engineParked.queuedPlays":              "clone method with the remap",
	"engineResolveKernel.tape":              "Kernel.ForClone",
	"engineContinuation.cast":               "pendingCast clone also carries deferredPush",
	"engineCastWindows.deferredPush":        "carried inside the pending cast",
}

// cloneGates are the field-name prefixes of clusters copied only while their
// `<prefix>Open` bracket is open (an open trigger batch survives a suspension;
// a closed one holds nothing worth carrying).
var cloneGates = []string{"damageBatch", "zoneBatch", "discardBatch"}

// cloneTypeCopiers are the types whose copy is a named function, not the
// derived one. The value is the statement template: %[1]s dst, %[2]s src.
var cloneTypeCopiers = map[string]string{
	"*rules.resumePoint":             "%[1]s = remap.resume(%[2]s)",
	"*rules.lifeExchangeTransaction": "%[1]s = remap.lifeExchange(%[2]s)",
	"*effects.FlipMemory":            "%[1]s = remap.flipMemory(%[2]s)",
	"*effects.ExchangeMemory":        "%[1]s = remap.exchangeMemory(%[2]s)",
	"effects.Ctx":                    "%[1]s = remap.unlessCtx(%[2]s)",
	"*decision.Decision":             "if %[2]s != nil {\n%[1]s = cloneDecision(%[2]s)\n}",
	"*state.Object":                  "if %[2]s != nil {\ncp := %[2]s.CloneDeep()\n%[1]s = &cp\n}",
	"cost.Cost":                      "%[1]s = cloneCost(%[2]s)",
	"[]rules.pendingTrigger":         "%[1]s = clonePendingTriggers(%[2]s)",
	"cards.Trigger":                  "%[1]s = cloneTrigger(%[2]s)",
	"effects.TokenRest":              "%[1]s = %[2]s.Clone()",
}

// cloneForeignDeep names the non-rules struct types whose reference fields
// the copy deepens (every other foreign struct is copied by value).
var cloneForeignDeep = map[string]bool{
	"state.ContinuousEffect": true,
}

// cloneSharedLocal names the pointers to engine-local types the clone shares
// (immutable snapshots and the engine itself).
var cloneSharedLocal = map[string]bool{
	"*rules.triggerSnapshot": true,
	"*rules.Engine":          true,
}

type cloneGen struct {
	imports map[string]string // path -> alias
	out     strings.Builder
}

func (g *cloneGen) tn(t reflect.Type) string {
	if t.Name() != "" && t.PkgPath() != "" {
		if t.PkgPath() == reflect.TypeOf(Engine{}).PkgPath() {
			return t.Name()
		}
		g.imports[t.PkgPath()] = t.String()[:strings.Index(t.String(), ".")]
		return t.String()
	}
	switch t.Kind() {
	case reflect.Pointer:
		return "*" + g.tn(t.Elem())
	case reflect.Slice:
		return "[]" + g.tn(t.Elem())
	case reflect.Array:
		return fmt.Sprintf("[%d]%s", t.Len(), g.tn(t.Elem()))
	case reflect.Map:
		return "map[" + g.tn(t.Key()) + "]" + g.tn(t.Elem())
	}
	if t.PkgPath() == "" && t.Kind() != reflect.Struct && t.Kind() != reflect.Interface && t.Kind() != reflect.Func {
		return t.String()
	}
	panic("clone gen: cannot name type " + t.String())
}

// cloneSharedPtr reports whether a pointer is shared by the clone: every
// pointer to a foreign type except the ones the clone owns (cloneForeignDeep,
// events.Event), because the corpus and the engine's collaborators are
// immutable or shared by contract.
func cloneSharedPtr(t reflect.Type) bool {
	if cloneSharedLocal[t.String()] {
		return true
	}
	if cloneLocal(t.Elem()) || cloneForeignDeep[t.Elem().String()] || t.Elem().String() == "events.Event" {
		return false
	}
	return true
}

func cloneHasTags(t reflect.Type) bool {
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Tag.Get("clone") != "" {
			return true
		}
	}
	return false
}

func cloneLocal(t reflect.Type) bool { return t.PkgPath() == reflect.TypeOf(Engine{}).PkgPath() }

// needsDeep reports whether a value of type t shares storage after a plain
// assignment that the clone must not share.
func needsDeep(t reflect.Type) bool {
	if _, ok := cloneTypeCopiers[t.String()]; ok {
		return true
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Map:
		return true
	case reflect.Pointer:
		return !cloneSharedPtr(t)
	case reflect.Array:
		return needsDeep(t.Elem())
	case reflect.Struct:
		return structNeedsDeep(t)
	}
	return false
}

var cloneVisiting = map[reflect.Type]bool{}

func structNeedsDeep(t reflect.Type) bool {
	if cloneVisiting[t] {
		panic("clone gen: recursive type " + t.String())
	}
	cloneVisiting[t] = true
	defer delete(cloneVisiting, t)
	if !cloneLocal(t) && !cloneForeignDeep[t.String()] {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		if fieldDeep(t, t.Field(i)) {
			return true
		}
	}
	return false
}

func fieldDeep(parent reflect.Type, f reflect.StructField) bool {
	if cloneHasTags(parent) && f.Tag.Get("clone") != "deep" {
		return false
	}
	return needsDeep(f.Type)
}

func (g *cloneGen) line(s string) { g.out.WriteString(s + "\n") }

// assign emits dst = <deep copy of src>.
func (g *cloneGen) assign(dst, src string, t reflect.Type, d int) {
	if d > 12 {
		panic("clone gen: too deep at " + dst + " <- " + src + " " + t.String())
	}
	if tpl, ok := cloneTypeCopiers[t.String()]; ok && tpl != "" {
		g.line(fmt.Sprintf(tpl, dst, src))
		return
	}
	if !needsDeep(t) {
		g.line(dst + " = " + src)
		return
	}
	switch t.Kind() {
	case reflect.Slice:
		if !needsDeep(t.Elem()) {
			g.line(fmt.Sprintf("%s = append(%s(nil), %s...)", dst, g.tn(t), src))
			return
		}
		i := fmt.Sprintf("i%d", d)
		g.line(fmt.Sprintf("if %s != nil {", src))
		g.line(fmt.Sprintf("%s = make(%s, len(%s))", dst, g.tn(t), src))
		g.line(fmt.Sprintf("for %s := range %s {", i, src))
		g.assign(fmt.Sprintf("%s[%s]", dst, i), fmt.Sprintf("%s[%s]", src, i), t.Elem(), d+1)
		g.line("}\n}")
	case reflect.Map:
		k, v := fmt.Sprintf("k%d", d), fmt.Sprintf("v%d", d)
		g.line(fmt.Sprintf("if %s != nil {", src))
		g.line(fmt.Sprintf("%s = make(%s, len(%s))", dst, g.tn(t), src))
		g.line(fmt.Sprintf("for %s, %s := range %s {", k, v, src))
		if needsDeep(t.Elem()) {
			nv := fmt.Sprintf("nv%d", d)
			g.line(fmt.Sprintf("var %s %s", nv, g.tn(t.Elem())))
			g.assign(nv, v, t.Elem(), d+1)
			g.line(fmt.Sprintf("%s[%s] = %s", dst, k, nv))
		} else {
			g.line(fmt.Sprintf("%s[%s] = %s", dst, k, v))
		}
		g.line("}\n}")
	case reflect.Pointer:
		if t.Elem().Kind() != reflect.Struct {
			panic("clone gen: pointer to non-struct " + t.String())
		}
		p := fmt.Sprintf("p%d", d)
		g.line(fmt.Sprintf("if %s != nil {", src))
		g.line(fmt.Sprintf("%s := *%s", p, src))
		g.fix(p, src, t.Elem(), d+1)
		g.line(fmt.Sprintf("%s = &%s\n}", dst, p))
	case reflect.Array:
		panic("clone gen: array of references " + t.String())
	case reflect.Struct:
		g.line(dst + " = " + src)
		g.fix(dst, src, t, d)
	default:
		panic("clone gen: " + t.String())
	}
}

// fix deepens the reference fields of struct dst, which already holds a value
// copy of src.
func (g *cloneGen) fix(dst, src string, t reflect.Type, d int) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !fieldDeep(t, f) {
			continue
		}
		if !f.IsExported() && !cloneLocal(t) {
			panic("clone gen: unexported reference field " + t.String() + "." + f.Name)
		}
		fd, fs := dst+"."+f.Name, src+"."+f.Name
		if _, ok := cloneTypeCopiers[f.Type.String()]; !ok && f.Type.Kind() == reflect.Struct {
			g.fix(fd, fs, f.Type, d+1)
			continue
		}
		g.assign(fd, fs, f.Type, d)
	}
}

func renderClone(t *testing.T) string {
	g := &cloneGen{imports: map[string]string{}}
	type block struct{ cluster, body string }
	var blocks []block
	gated := map[string]string{}
	var unused []string
	hand := map[string]bool{}
	for _, f := range clonePolicyFieldsOf(reflect.TypeOf(Engine{})) {
		name := strings.TrimPrefix(f.path, "Engine.")
		short, cluster := name, "Engine"
		if i := strings.LastIndex(name, "."); i >= 0 {
			short, cluster = name[i+1:], name[:i]
		}
		if _, ok := cloneHandFields[name]; ok {
			hand[name] = true
			continue
		}
		if f.policy != "deep" {
			continue
		}
		g.out.Reset()
		g.assign("c."+short, "e."+short, f.typ, 0)
		for _, gate := range cloneGates {
			if strings.HasPrefix(short, gate) {
				gated[gate] += g.out.String()
				if short == gate+"Open" {
					blocks = append(blocks, block{cluster, "@" + gate})
				}
				goto next
			}
		}
		blocks = append(blocks, block{cluster, g.out.String()})
	next:
	}
	for i, b := range blocks {
		if strings.HasPrefix(b.body, "@") {
			gate := b.body[1:]
			blocks[i].body = "if e." + gate + "Open {\n" + gated[gate] + "}\n"
		}
	}
	g.out.Reset()
	for k := range cloneHandFields {
		if !hand[k] {
			unused = append(unused, k)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		t.Fatalf("cloneHandFields names unknown fields: %v", unused)
	}
	// Pack the field copies into functions of at most ~150 lines, one cluster
	// per function family, so no generated function grows into a 300-line
	// concern of its own.
	var names []string
	part := map[string]int{}
	for i := 0; i < len(blocks); {
		j, n := i, 0
		for j < len(blocks) && blocks[j].cluster == blocks[i].cluster && (n == 0 || n+strings.Count(blocks[j].body, "\n") <= 150) {
			n += strings.Count(blocks[j].body, "\n")
			j++
		}
		part[blocks[i].cluster]++
		name := "cloneFields" + strings.ToUpper(blocks[i].cluster[:1]) + blocks[i].cluster[1:]
		if part[blocks[i].cluster] > 1 {
			name += fmt.Sprint(part[blocks[i].cluster])
		}
		names = append(names, name)
		g.line(fmt.Sprintf("func %s(c, e *Engine, remap *cloneRemap) {", name))
		for _, b := range blocks[i:j] {
			g.out.WriteString(b.body)
		}
		g.line("}\n")
		i = j
	}
	body := g.out.String()
	g.out.Reset()
	g.line("// cloneEngineFields copies every `deep` Engine field not named in\n// cloneHandFields (clone_gen_test.go).")
	g.line("func cloneEngineFields(c, e *Engine, remap *cloneRemap) {")
	for _, n := range names {
		g.line(n + "(c, e, remap)")
	}
	g.line("}\n")
	g.out.WriteString(body)
	g.line("// resumeFields deepens the reference fields of the resume frame cp, a value\n// copy of rp.")
	g.line("func (remap *cloneRemap) resumeFields(cp, rp *resumePoint) {")
	g.fix("cp", "rp", reflect.TypeOf(resumePoint{}), 0)
	g.line("}")
	var imps []string
	for p, a := range g.imports {
		imps = append(imps, fmt.Sprintf("\t%s %q", a, p))
	}
	sort.Strings(imps)
	src := "// Code generated by TestCloneGenIsUpToDate (GORGE_GEN_CLONE=1 go generate ./rules); DO NOT EDIT.\n\npackage rules\n\nimport (\n" +
		strings.Join(imps, "\n") + "\n)\n\n" + g.out.String()
	b, err := format.Source([]byte(src))
	if err != nil {
		t.Fatalf("gofmt generated clone: %v\n%s", err, src)
	}
	return string(b)
}

func TestCloneGenIsUpToDate(t *testing.T) {
	want := renderClone(t)
	if os.Getenv("GORGE_GEN_CLONE") == "1" {
		if err := os.WriteFile("clone_gen.go", []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile("clone_gen.go")
	if err != nil || string(got) != want {
		t.Fatalf("rules/clone_gen.go is stale: run `GORGE_GEN_CLONE=1 go test ./rules -run TestCloneGenIsUpToDate` (go generate ./rules)")
	}
}
