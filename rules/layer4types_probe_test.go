package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/state"
)

// TestFaceStaticProbesAreConservative cross-checks the two derived per-face
// static probes that let the layer path skip work (cards.Face.
// StaticsMayChangeTypes, read by anyLayer4Active's precheck, and
// ContinuousStaticsMayFunctionOffBattlefield, read by staticEffects' off-
// battlefield skip) against rules' OWN source-zone gate, over every face of
// every corpus card and token script. For each zone staticEffects walks, a
// Continuous static that passes the real gate (stackSelfStaticOK ||
// chars.StaticZoneAdmits) off the battlefield must make the off-battlefield probe
// answer true, and one that could reach the AddType branch (directly, or
// through an AddStaticAbility$ grant) must make the type probe for that zone
// class answer true. It also pins that the corpus faces come out of the
// registry with a BOUND probe, so the fast path is actually taken; an unbound
// probe is conservative (true) but would silently cost the whole saving.
func TestFaceStaticProbesAreConservative(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := &Engine{}
	var faces []*cards.Face
	for _, c := range reg.Cards {
		faces = append(faces, c.Faces...)
	}
	stems := make([]string, 0, len(reg.Tokens))
	for k := range reg.Tokens {
		stems = append(stems, k)
	}
	sort.Strings(stems)
	for _, k := range stems {
		faces = append(faces, reg.Tokens[k].Faces...)
	}
	checked, typeFaces, offFaces := 0, 0, 0
	for _, f := range faces {
		if f == nil || len(f.Statics) == 0 {
			continue
		}
		for _, z := range staticSourceZones {
			o := &state.Object{Zone: z}
			onBF := z == state.ZBattlefield
			for _, st := range f.Statics {
				if st.Mode != "Continuous" {
					continue
				}
				if !e.stackSelfStaticOK(st, o) && !chars.StaticZoneAdmits(st.Params["ExcludeZone"], st.Params["EffectZone"], z) {
					continue
				}
				if !onBF && !f.ContinuousStaticsMayFunctionOffBattlefield() {
					t.Errorf("%s: Continuous static live in zone %v but the off-battlefield probe says no", f.Name, z)
				}
				typeChanging := hasStat(st, "AddType") || hasStat(st, "AddTypes") || hasStat(st, "AddAllCreatureTypes") || strings.TrimSpace(st.Params["RemoveType"]) != ""
				if name := st.Params["AddStaticAbility"]; name != "" {
					if inners, ok := cards.ParseStaticLines(f.SVars[name]); ok {
						for _, in := range inners {
							if in.Mode == "Continuous" && (hasStat(in, "AddType") || hasStat(in, "AddTypes") || hasStat(in, "AddAllCreatureTypes") || strings.TrimSpace(in.Params["RemoveType"]) != "") {
								typeChanging = true
							}
						}
					}
				}
				if typeChanging && !f.StaticsMayChangeTypes(onBF) {
					t.Errorf("%s: type-changing static live in zone %v but StaticsMayChangeTypes(%v) says no", f.Name, z, onBF)
				}
			}
		}
		checked++
		if f.StaticsMayChangeTypes(true) {
			typeFaces++
		}
		if f.ContinuousStaticsMayFunctionOffBattlefield() {
			offFaces++
		}
	}
	t.Logf("%d faces with statics: %d may change types on the battlefield, %d have an off-battlefield Continuous static", checked, typeFaces, offFaces)
	// A bound probe answers false for most faces; if every face came back
	// "maybe", the probe was not bound at load and the fast path is dead.
	if typeFaces*2 > checked || offFaces*2 > checked {
		t.Fatalf("probe answers true for most faces (%d/%d, %d/%d): not bound after registry load?", typeFaces, checked, offFaces, checked)
	}
}

// TestFaceStaticProbeUnboundIsConservative pins the probe's staleness rule:
// a face never derived, or whose Statics grew after derive, answers true.
func TestFaceStaticProbeUnboundIsConservative(t *testing.T) {
	lit := &cards.Face{Statics: []cards.Static{{Mode: "Continuous", Params: map[string]string{"AddPower": "1"}}}}
	if !lit.StaticsMayChangeTypes(true) || !lit.StaticsMayChangeTypes(false) || !lit.ContinuousStaticsMayFunctionOffBattlefield() {
		t.Fatal("an underived face must answer true")
	}
	if (&cards.Face{}).StaticsMayChangeTypes(true) || (*cards.Face)(nil).StaticsMayChangeTypes(true) {
		t.Fatal("a face with no statics must answer false")
	}
	c, _ := cards.ParseBytes("p.txt", []byte("Name:Pump Lord\nManaCost:1 W\nTypes:Creature Human\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.YouCtrl+Other | AddPower$ 1 | Description$x\nOracle:x\n"))
	f := c.Faces[0]
	if f.StaticsMayChangeTypes(true) || f.ContinuousStaticsMayFunctionOffBattlefield() {
		t.Fatal("a derived pump-only face must answer false")
	}
	cp := *f
	cp.Statics = append(cp.Statics[:len(cp.Statics):len(cp.Statics)], cards.Static{Mode: "Continuous", Params: map[string]string{"AddType": "Goblin", "EffectZone": "All"}})
	if !cp.StaticsMayChangeTypes(true) || !cp.StaticsMayChangeTypes(false) || !cp.ContinuousStaticsMayFunctionOffBattlefield() {
		t.Fatal("a face whose Statics grew after derive must answer true")
	}
}
