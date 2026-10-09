package oraclegen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules"
)

// TestXMageOptionalTargetSkipsRoundTrip: the skip plan lives on the Item, not
// the Step. It must survive the Item's JSON round trip, stay out of the Raw
// scenario gorge's runner decodes (which rejects unknown step fields), and
// leave an item without a plan byte-identical.
func TestXMageOptionalTargetSkipsRoundTrip(t *testing.T) {
	sc := Scenario{
		Setup: map[string]Seat{"p0": {Hand: []string{"Shock"}}},
		Steps: []Step{{Op: "cast", Seat: 0, Card: "p0:Shock", Targets: []string{"p1"}}, {Op: "resolve"}},
	}
	plain := NewItem(&cards.Face{Name: "Shock"}, "Shock", "cast-resolve", 1, sc)
	plainJSON := itemJSON(t, plain)
	if strings.Contains(plainJSON, "xmage_target_skips") {
		t.Fatalf("an item with no plan serialises the field: %s", plainJSON)
	}

	withPlan := plain
	withPlan.XTargetSkips = [][]XTargetSkip{{{At: 0, Slot: 0}, {At: 1, Slot: 2}}, nil}
	planJSON := itemJSON(t, withPlan)
	if !strings.Contains(planJSON, `"xmage_target_skips":[[{"at":0,"slot":0},{"at":1,"slot":2}],null]`) {
		t.Fatalf("plan not serialised as expected: %s", planJSON)
	}
	if planJSON == plainJSON {
		t.Fatal("precondition: the plan must change the item's bytes")
	}
	var back Item
	if err := json.Unmarshal([]byte(planJSON), &back); err != nil {
		t.Fatal(err)
	}
	if got := itemJSON(t, back); got != planJSON {
		t.Errorf("round trip changed the item:\n got %s\nwant %s", got, planJSON)
	}
	if len(back.XTargetSkips) != 2 || back.XTargetSkips[0][1] != (XTargetSkip{At: 1, Slot: 2}) {
		t.Errorf("decoded plan = %v", back.XTargetSkips)
	}

	// Raw is the scenario alone: identical with and without the plan, and
	// decodable by a strict decoder like the runner's.
	if string(withPlan.Raw()) != string(plain.Raw()) {
		t.Errorf("the plan changed Raw():\n%s\n%s", withPlan.Raw(), plain.Raw())
	}
	if raw := string(withPlan.Raw()); strings.Contains(raw, "target_skip") {
		t.Errorf("Raw leaks the XMage-only skip: %s", raw)
	}
	dec := json.NewDecoder(strings.NewReader(string(withPlan.Raw())))
	dec.DisallowUnknownFields()
	var strict Scenario
	if err := dec.Decode(&strict); err != nil {
		t.Errorf("Raw is not a clean scenario: %v", err)
	}
}

func optSlot(filter string) Slot {
	return Slot{Filter: filter, Optional: true, ZeroOrOne: true, Min: 0, Max: 1}
}

func castPlan(n int) (Scenario, []string) {
	targets := make([]string, n)
	for i := range targets {
		targets[i] = "p0:Card" + string(rune('A'+i))
	}
	return Scenario{Steps: []Step{{Op: "cast", Seat: 0, Card: "p0:X", Targets: targets}, {Op: "resolve"}}}, targets
}

func targetDecisions(step int, refs []string) []rules.OracleDecision {
	var ds []rules.OracleDecision
	for _, r := range refs {
		ds = append(ds, rules.OracleDecision{Step: step, Kind: "target", Via: "target", Min: 0, Max: 1, PickRefs: []string{r}})
	}
	return ds
}

// TestXMageOptionalTargetSkipBoundaries: skip offsets for leading, middle
// (Rise's Mount), trailing and consecutive empty objects, and the rejections
// that keep a speculative skip from being queued.
func TestXMageOptionalTargetSkipBoundaries(t *testing.T) {
	four := []Slot{optSlot("A"), optSlot("B"), optSlot("C"), optSlot("D")}
	cases := []struct {
		name    string
		omitted []int
		want    []XTargetSkip
	}{
		{"middle", []int{1}, []XTargetSkip{{At: 1, Slot: 1}}},
		{"leading", []int{0}, []XTargetSkip{{At: 0, Slot: 0}}},
		{"trailing", []int{3}, []XTargetSkip{{At: 3, Slot: 3}}},
		{"consecutive", []int{1, 2}, []XTargetSkip{{At: 1, Slot: 1}, {At: 1, Slot: 2}}},
		{"leading and trailing", []int{0, 3}, []XTargetSkip{{At: 0, Slot: 0}, {At: 2, Slot: 3}}},
		{"all but one", []int{0, 1, 3}, []XTargetSkip{{At: 0, Slot: 0}, {At: 0, Slot: 1}, {At: 1, Slot: 3}}},
	}
	for _, c := range cases {
		sc, targets := castPlan(len(four) - len(c.omitted))
		got := targetSkips(four, c.omitted, sc, targetDecisions(0, targets), map[int]bool{0: true})
		if got == nil {
			t.Errorf("%s: no plan for omitted %v", c.name, c.omitted)
			continue
		}
		if len(got) != len(sc.Steps) || len(got[1]) != 0 {
			t.Errorf("%s: plan not parallel to steps / resolve step carries skips: %v", c.name, got)
		}
		if len(got[0]) != len(c.want) {
			t.Errorf("%s: skips %v, want %v", c.name, got[0], c.want)
			continue
		}
		for i := range c.want {
			if got[0][i] != c.want[i] {
				t.Errorf("%s: skip %d = %v, want %v", c.name, i, got[0][i], c.want[i])
			}
		}
		if err := CheckTargetSkips(four, sc.Steps, got); err != nil {
			t.Errorf("%s: own plan rejected: %v", c.name, err)
		}
		// The queue the driver builds is every target in order with one skip
		// before each offset; the result must interleave exactly that way.
		queue := interleave(targets, got[0])
		if n := strings.Count(strings.Join(queue, ","), "[target_skip]"); n != len(c.omitted) {
			t.Errorf("%s: queue %v has %d skips, want %d", c.name, queue, n, len(c.omitted))
		}
	}

	// Rise: Creature, (Mount), Vehicle, NoAbilities -> A, skip, B, C.
	sc, targets := castPlan(3)
	rise := targetSkips(four, []int{1}, sc, targetDecisions(0, targets), map[int]bool{0: true})
	if q := strings.Join(interleave(targets, rise[0]), ","); q != "p0:CardA,[target_skip],p0:CardB,p0:CardC" {
		t.Errorf("Rise queue = %s", q)
	}

	// Unsupported shapes derive no plan (the driver keeps its legacy path).
	sc3, t3 := castPlan(3)
	good := targetDecisions(0, t3)
	none := func(name string, slots []Slot, omitted []int, sc Scenario, ds []rules.OracleDecision, cs map[int]bool) {
		t.Helper()
		if got := targetSkips(slots, omitted, sc, ds, cs); got != nil {
			t.Errorf("%s: derived plan %v, want none", name, got)
		}
	}
	// Precondition: the baseline the variants perturb does derive a plan.
	if targetSkips(four, []int{1}, sc3, good, map[int]bool{0: true}) == nil {
		t.Fatal("baseline derives no plan")
	}
	none("nothing omitted", four[:3], nil, sc3, good, map[int]bool{0: true})
	short := append([]rules.OracleDecision(nil), good[:2]...)
	none("a filled slot posed no decision", four, []int{1}, sc3, short, map[int]bool{0: true})
	declined := append([]rules.OracleDecision(nil), good...)
	declined[0].PickRefs = nil
	none("a declined slot", four, []int{1}, sc3, declined, map[int]bool{0: true})
	moved := append([]rules.OracleDecision(nil), good...)
	moved[2].PickRefs = []string{"p1:Other"}
	none("a pick that is not the cast's target", four, []int{1}, sc3, moved, map[int]bool{0: true})
	none("two target-carrying steps", four, []int{1}, sc3, good, map[int]bool{0: true, 1: true})
	none("a target-less cast", four, []int{1}, sc3, good, map[int]bool{})
	// A slot whose bounds cannot be read off the text (Min/Max 0) is never
	// closed by a derived skip, filled or omitted.
	unreadable := []Slot{optSlot("A"), {Filter: "B", Optional: true}, optSlot("C"), optSlot("D")}
	none("an unreadable bound omitted", unreadable, []int{1}, sc3, good, map[int]bool{0: true})
	none("an unreadable bound filled", unreadable, []int{0}, sc3, good, map[int]bool{0: true})
	none("every slot omitted", four[:1], []int{0}, Scenario{Steps: []Step{{Op: "cast"}}}, nil, map[int]bool{0: true})
	act := sc3
	act.Steps = []Step{{Op: "activate", Targets: t3}, {Op: "resolve"}}
	none("an activate step", four, []int{1}, act, good, map[int]bool{0: true})

	// A multi-pick slot (Min..Max, Max>1) filled with fewer than Max picks is
	// closed by a skip after its last pick -- the widening Terrific Team-Up
	// and Allies at Last need -- but only while candidates remain: an object
	// whose legal set is exhausted completes by itself (TargetImpl
	// isChoiceCompleted's moreSelectCount == 0), and a queued skip would be
	// left for the next ask.
	multi := []Slot{{Filter: "A", Min: 1, Max: 2}, {Filter: "B", Min: 1, Max: 1}}
	scM, tM := castPlan(2)
	mds := []rules.OracleDecision{
		{Step: 0, Kind: "target", Via: "target", Min: 1, Max: 2, PickRefs: []string{tM[0]},
			OptionRefs: []string{tM[0], "p0:CardB2"}},
		{Step: 0, Kind: "target", Via: "target", Min: 1, Max: 1, PickRefs: []string{tM[1]},
			OptionRefs: []string{tM[1]}},
	}
	gotM := targetSkips(multi, nil, scM, mds, map[int]bool{0: true})
	if gotM == nil || len(gotM[0]) != 1 || gotM[0][0] != (XTargetSkip{At: 1, Slot: 0}) {
		t.Fatalf("multi-pick plan = %v, want [{at:1,slot:0}]", gotM)
	}
	if q := strings.Join(interleave(tM, gotM[0]), ","); q != "p0:CardA,[target_skip],p0:CardB" {
		t.Errorf("multi-pick queue = %s", q)
	}
	// The exhausted shape (one candidate, one pick, Max 2) derives no plan:
	// XMage completes the object itself (the Cease // Desist class).
	exhausted := []rules.OracleDecision{
		{Step: 0, Kind: "target", Via: "target", Min: 1, Max: 2, PickRefs: []string{tM[0]},
			OptionRefs: []string{tM[0]}},
		{Step: 0, Kind: "target", Via: "target", Min: 1, Max: 1, PickRefs: []string{tM[1]},
			OptionRefs: []string{tM[1]}},
	}
	if got := targetSkips(multi, nil, scM, exhausted, map[int]bool{0: true}); got != nil {
		t.Errorf("an exhausted multi-pick slot derived a plan: %v", got)
	}
	// A multi-pick slot filled to its maximum needs no skip (XMage closes it
	// itself, so a skip would be left unused).
	scFull, tFull := castPlan(3)
	fullM := []rules.OracleDecision{
		{Step: 0, Kind: "target", Via: "target", Min: 1, Max: 2, PickRefs: []string{tFull[0], tFull[1]}},
		{Step: 0, Kind: "target", Via: "target", Min: 1, Max: 1, PickRefs: []string{tFull[2]}},
	}
	if got := targetSkips(multi, nil, scFull, fullM, map[int]bool{0: true}); got != nil {
		t.Errorf("a slot filled to its maximum derived a plan: %v", got)
	}
	// An omitted 0..2 object (min 0) is closable too: one skip closes it.
	upToTwo := []Slot{optSlot("A"), {Filter: "B", Optional: true, Min: 0, Max: 2}, optSlot("C"), optSlot("D")}
	scU, tU := castPlan(3)
	gotU := targetSkips(upToTwo, []int{1}, scU, targetDecisions(0, tU), map[int]bool{0: true})
	if gotU == nil || len(gotU[0]) != 1 || gotU[0][0] != (XTargetSkip{At: 1, Slot: 1}) {
		t.Errorf("omitted 0..2 plan = %v, want [{at:1,slot:1}]", gotU)
	}

	// CheckTargetSkips rejects malformed plans.
	step := func(n int) []Step { s, _ := castPlan(n); return s.Steps }
	required := []Slot{optSlot("A"), {Filter: "B", Min: 1, Max: 1}, optSlot("C")}
	bad := []struct {
		name  string
		slots []Slot
		steps []Step
		plan  [][]XTargetSkip
	}{
		{"length mismatch", four, step(3), [][]XTargetSkip{{{At: 1, Slot: 1}}}},
		{"required slot skipped", required, step(2), [][]XTargetSkip{{{At: 1, Slot: 1}}, nil}},
		{"nonliteral slot skipped", unreadable, step(3), [][]XTargetSkip{{{At: 0, Slot: 0}, {At: 1, Slot: 1}}, nil}},
		{"slot out of range", four, step(3), [][]XTargetSkip{{{At: 1, Slot: 9}}, nil}},
		{"negative slot", four, step(3), [][]XTargetSkip{{{At: 0, Slot: -1}}, nil}},
		{"wrong offset", four, step(3), [][]XTargetSkip{{{At: 2, Slot: 1}}, nil}},
		{"repeated slot", four, step(2), [][]XTargetSkip{{{At: 1, Slot: 1}, {At: 0, Slot: 1}}, nil}},
		{"descending slots", four, step(2), [][]XTargetSkip{{{At: 2, Slot: 3}, {At: 1, Slot: 1}}, nil}},
		{"unaccounted slot", four, step(2), [][]XTargetSkip{{{At: 1, Slot: 1}}, nil}},
		{"skip on a resolve step", four, step(3), [][]XTargetSkip{nil, {{At: 0, Slot: 0}}}},
	}
	for _, b := range bad {
		if err := CheckTargetSkips(b.slots, b.steps, b.plan); err == nil {
			t.Errorf("%s: malformed plan accepted", b.name)
		}
	}
	if err := CheckTargetSkips(four, step(3), [][]XTargetSkip{{{At: 1, Slot: 1}}, nil}); err != nil {
		t.Errorf("precondition: the well-formed plan is rejected: %v", err)
	}
	// A required slot may be FILLED next to a skip on an optional object: the
	// queue is [skip, required pick, pick], and the accounting admits it.
	if err := CheckTargetSkips(required, step(2), [][]XTargetSkip{{{At: 0, Slot: 0}}, nil}); err != nil {
		t.Errorf("a plan skipping an optional object before a filled required one is rejected: %v", err)
	}
	// The multi-pick plan validates and its queue matches the driver's.
	if err := CheckTargetSkips(multi, scM.Steps, gotM); err != nil {
		t.Errorf("multi-pick plan rejected: %v", err)
	}
	if err := CheckTargetSkips(upToTwo, scU.Steps, gotU); err != nil {
		t.Errorf("omitted 0..2 plan rejected: %v", err)
	}
}

// interleave mirrors the driver's queue: one skip before target At.
func interleave(targets []string, skips []XTargetSkip) []string {
	var q []string
	next := 0
	for k := 0; k <= len(targets); k++ {
		for next < len(skips) && skips[next].At == k {
			q = append(q, "[target_skip]")
			next++
		}
		if k < len(targets) {
			q = append(q, targets[k])
		}
	}
	return q
}

// TestXMageOptionalTargetSkipSlotShape: only an independent literal 0..1
// object is a ZeroOrOne slot; a repeated, ranged, divided or cross-target
// object never is, so it can never be marked as an omitted object.
func TestXMageOptionalTargetSkipSlotShape(t *testing.T) {
	base := map[string]string{"TargetMin": "0", "TargetMax": "1"}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for a, b := range base {
			m[a] = b
		}
		m[k] = v
		return m
	}
	if !zeroOrOneTarget(base) {
		t.Fatal("precondition: the bare 0..1 shape is not ZeroOrOne")
	}
	for name, params := range map[string]map[string]string{
		"up to two":         with("TargetMax", "2"),
		"up to X":           with("TargetMax", "X"),
		"required":          with("TargetMin", "1"),
		"no max":            {"TargetMin": "0"},
		"divided":           with("DividedAsYouChoose", "3"),
		"different players": with("TargetsWithDifferentControllers", "True"),
		"per player":        with("TargetsForEachPlayer", "True"),
		"unique":            with("TargetUnique", "True"),
	} {
		if zeroOrOneTarget(params) {
			t.Errorf("%s counted as an independent 0..1 object", name)
		}
	}
}
