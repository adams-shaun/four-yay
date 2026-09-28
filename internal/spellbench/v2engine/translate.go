package v2engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/state"
)

// cand is one wire candidate: its semantic, display text, the translator
// action it stands for, and (for the V5 order) the hidden card it names.
type cand struct {
	sem     map[string]any
	display string
	act     int
	hidden  string // "<card_name>\x00<object_id>" when it references a hidden-zone card
}

// translator decomposes one gorge Decision into wire decisions (doc.go).
type translator interface {
	name() string
	// looks are the hidden-zone cards the decision shows (fixed per decision).
	looks() []look
	// progress is the next substep and its group's size; more is false once
	// every substep is answered.
	progress() (substep, count int, more bool)
	candidates(b *obsBuild) ([]cand, error)
	answer(act int) error
	intent() (decision.Intent, error)
	contextKind() string
	contextSource(b *obsBuild) *v2agent.ObjectRef
	contextPurpose() string
}

// orderHidden puts the candidates that reference hidden-zone cards in
// (card_name, object_id) order among themselves (spec 7.1, V5).
func orderHidden(cands []cand, b *obsBuild) {
	var idx []int
	for i := range cands {
		if cands[i].hidden != "" {
			idx = append(idx, i)
		}
	}
	if len(idx) < 2 {
		return
	}
	sub := make([]cand, len(idx))
	for k, i := range idx {
		sub[k] = cands[i]
	}
	sort.SliceStable(sub, func(a, c int) bool { return sub[a].hidden < sub[c].hidden })
	for k, i := range idx {
		cands[i] = sub[k]
	}
}

// base carries what every translator shares.
type base struct {
	g    *Game
	d    *decision.Decision
	kind string
}

func (t *base) name() string           { return t.kind }
func (t *base) looks() []look          { return nil }
func (t *base) contextKind() string    { return "choice" }
func (t *base) contextPurpose() string { return "" }
func (t *base) contextSource(b *obsBuild) *v2agent.ObjectRef {
	return nil
}

// sourceID is the object the decision belongs to: gorge's Source, else the
// seat's last priority action (an ability mid-activation has Source 0).
func (t *base) sourceID() state.ObjID {
	if t.d.Source != 0 {
		return t.d.Source
	}
	return t.g.lastAction[t.d.Player]
}

// sourceRef is the reference of the decision's source (spec 7.3): the
// stack entry of the spell or ability when one exists -- sourceID itself on
// the stack, else the topmost ability on the stack whose source is sourceID,
// unless the seat is still announcing a new action from that object -- and
// otherwise the object's current visible incarnation; nil when the
// observation does not hold it.
func (t *base) sourceRef(b *obsBuild) *v2agent.ObjectRef {
	id := t.sourceID()
	if id == 0 || b.hidden[id] {
		return nil
	}
	gs := t.g.e.G
	if o := gs.Obj(id); o != nil && o.Zone != state.ZStack && t.g.lastAction[t.d.Player] != id {
		for i := len(gs.Stack) - 1; i >= 0; i-- {
			if so := gs.Obj(gs.Stack[i]); so != nil && so.Card == nil && so.Source == id {
				if r := b.refPtr(so.ID); r != nil {
					return r
				}
			}
		}
	}
	return b.refPtr(id)
}

func refOrNil(r *v2agent.ObjectRef) any {
	if r == nil {
		return nil
	}
	return *r
}

// targetOf is an option's target reference (a player or an object).
func targetOf(b *obsBuild, o *decision.Option, playerKind bool) (map[string]any, bool) {
	if playerKind || (o.Obj == 0 && o.Kind == "player") {
		return map[string]any{"player": seatOf(o.Player)}, true
	}
	if o.Obj == 0 {
		return nil, false
	}
	r, ok := b.ref(o.Obj)
	if !ok {
		return nil, false
	}
	return map[string]any{"object": r}, true
}

// safeLabel is an option's display text, or "" when the option names an
// object whose identity the viewer may not see (a face-down permanent or
// spell another seat controls, or a hidden-zone card another seat owns):
// gorge's labels are written for an omniscient reader (spec 6.8).
func safeLabel(b *obsBuild, o *decision.Option) string {
	for _, id := range []state.ObjID{o.Obj, o.Attacker, o.Battle} {
		if id == 0 {
			continue
		}
		ob := b.g.e.G.Obj(id)
		if ob == nil {
			continue
		}
		if ob.FaceDown && ob.Controller != b.viewer {
			return ""
		}
		if ob.Zone.Hidden() && ob.Owner != b.viewer {
			if _, shown := b.refs[id]; !shown {
				return ""
			}
		}
	}
	return o.Label
}

func hiddenKey(b *obsBuild, id state.ObjID) string {
	if !b.hidden[id] {
		return ""
	}
	r, _ := b.ref(id)
	name := ""
	if r.CardName != nil {
		name = *r.CardName
	}
	return name + "\x00" + r.ObjectID
}

// newTranslator picks the translator for d (doc.go's table).
func (g *Game) newTranslator(d *decision.Decision) translator {
	b := base{g: g, d: d}
	switch d.Kind {
	case decision.KPriority:
		b.kind = "priority"
		return &priorityTr{base: b}
	case decision.KAttackers:
		b.kind = "attackers"
		return newCombatTr(b, true)
	case decision.KBlockers:
		b.kind = "blockers"
		return newCombatTr(b, false)
	case decision.KTarget:
		if distinctTargets(d) {
			b.kind = "target"
			return newSeqTr(b, seqTarget, "")
		}
	case decision.KTriggerOrder:
		b.kind = "trigger_order"
		return newOrderTr(b)
	case decision.KTriggerOptional:
		b.kind = "trigger_optional"
		return newValueTr(b, "optional_trigger")
	case decision.KArrange:
		b.kind = "arrange"
		if t := newArrangeTr(b); t != nil {
			return t
		}
	case decision.KModes:
		if allModes(d) {
			b.kind = "modes"
			if d.Min == 1 && d.Max == 1 || d.Max > 1 {
				return newSeqTr(b, seqModes, "modes")
			}
		}
		if unlessPay(d) {
			b.kind = "unless_pay"
			return newValueTr(b, "unless")
		}
		if allHaveObj(d) && distinctTargets(d) {
			b.kind = "modes_objects"
			return newSeqTr(b, seqSelect, selectPurpose(d))
		}
	case decision.KChoose:
		if t := g.chooseTranslator(b); t != nil {
			return t
		}
	case decision.KReplacement:
		b.kind = "replacement"
		if kindsAll(d, "mana") && colorOptions(d) {
			return newValueTr(b, "color_mana")
		}
		if kindsAll(d, "apply", "decline") {
			return newValueTr(b, "optional_replacement")
		}
		if kindsAll(d, "replacement") && d.Min == 1 && d.Max == 1 {
			// replacement_order "engine_order" (hello_ok.engine_defaults):
			// gorge's scan order, the first applicable effect.
			b.kind = "engine_answer:replacement_order"
			return &engineTr{base: b}
		}
	case decision.KMulligan, decision.KStartingPlayer, decision.KCommanderZone:
		// Never posed under mulligan none / host_assigned; answered by the
		// engine's declared rule if gorge asks anyway.
		b.kind = "engine_answer:" + string(d.Kind)
		return &engineTr{base: b}
	}
	b.kind = "enumerated:" + string(d.Kind)
	return newEnumTr(b)
}

func (g *Game) chooseTranslator(b base) translator {
	d := b.d
	switch {
	case kindsAll(d, "x"):
		b.kind = "choose_x"
		return newValueTr(b, "x")
	case kindsAll(d, "number"):
		b.kind = "choose_number"
		return newValueTr(b, "number")
	case (kindsAll(d, "mana") || kindsAll(d, "color")) && colorOptions(d):
		b.kind = "choose_color"
		return newValueTr(b, "color")
	case kindsAll(d, "yes", "no"):
		b.kind = "choose_boolean"
		return newValueTr(b, "boolean")
	case kindsAll(d, "type"):
		b.kind = "choose_type"
		return newValueTr(b, "type")
	case kindsAll(d, "name"):
		b.kind = "choose_name"
		return newValueTr(b, "name")
	}
	if allHaveObj(d) && distinctTargets(d) {
		k := d.Options[0].Kind
		switch k {
		case "sacrifice", "returncost", "tapcost", "revealcost", "subcounter", "movetogravecost", "puttolibcost":
			b.kind = "cost_" + k
			return newSeqTr(b, seqCost, k)
		}
		b.kind = "select_" + k
		return newSeqTr(b, seqSelect, selectPurpose(d))
	}
	return nil
}

func kindsAll(d *decision.Decision, kinds ...string) bool {
	if len(d.Options) == 0 {
		return false
	}
	for _, o := range d.Options {
		ok := false
		for _, k := range kinds {
			if o.Kind == k {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func allHaveObj(d *decision.Decision) bool {
	if len(d.Options) == 0 {
		return false
	}
	k := d.Options[0].Kind
	for _, o := range d.Options {
		if o.Obj == 0 || o.Kind != k {
			return false
		}
	}
	return true
}

func allModes(d *decision.Decision) bool { return kindsAll(d, "mode") }

// distinctTargets reports whether no two options name the same object or
// player (so per-option semantics are pairwise distinct).
func distinctTargets(d *decision.Decision) bool {
	type k struct {
		obj    state.ObjID
		player state.PlayerID
	}
	seen := map[k]bool{} // lookup only
	for _, o := range d.Options {
		key := k{obj: o.Obj}
		if o.Obj == 0 {
			key.player = o.Player
		}
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func unlessPay(d *decision.Decision) bool {
	if len(d.Options) == 0 {
		return false
	}
	for _, o := range d.Options {
		if o.Mode != decision.ModeUnlessPay && o.Mode != decision.ModeUnlessDecline {
			return false
		}
	}
	return true
}

var manaColorWords = map[string]string{"W": "white", "U": "blue", "B": "black", "R": "red", "G": "green"}

// optionColor is a colour option's spec 6.10 colour word.
func optionColor(o *decision.Option) string {
	if w, ok := manaColorWords[o.ManaSymbol]; ok {
		return w
	}
	l := strings.ToLower(o.Label)
	for _, w := range []string{"white", "blue", "black", "red", "green"} {
		if strings.Contains(l, w) {
			return w
		}
	}
	for _, sym := range []string{"W", "U", "B", "R", "G"} {
		if strings.Contains(o.Label, "{"+sym+"}") || strings.HasSuffix(o.Label, " "+sym) {
			return manaColorWords[sym]
		}
	}
	return ""
}

func colorOptions(d *decision.Decision) bool {
	seen := map[string]bool{} // lookup only
	for i := range d.Options {
		c := optionColor(&d.Options[i])
		if c == "" || seen[c] {
			return false
		}
		seen[c] = true
	}
	return true
}

func selectPurpose(d *decision.Decision) string {
	switch d.Options[0].Kind {
	case "search":
		if d.Min == 0 {
			return "search"
		}
	case "discard", "mana_discard", "ward_discard":
		return "discard"
	case "exile":
		return "delve"
	case "untap":
		return "untap"
	case "keep":
		return "legend_rule"
	}
	return "other"
}

// repairedMinimal is the clamped minimal answer (pass at priority).
func repairedMinimal(d *decision.Decision) decision.Intent {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	if d.Kind == decision.KPriority {
		for _, o := range d.Options {
			if o.Kind == "pass" {
				in.Choices = []int{o.Index}
				return in
			}
		}
	}
	return botpolicy.Clamp(d, in)
}

// ---------------------------------------------------------------------------
// engineTr: a decision the engine answers under a declared rule.

type engineTr struct{ base }

func (t *engineTr) progress() (int, int, bool)             { return 0, 1, false }
func (t *engineTr) candidates(b *obsBuild) ([]cand, error) { return nil, fmt.Errorf("engine_answer") }
func (t *engineTr) answer(int) error                       { return nil }
func (t *engineTr) intent() (decision.Intent, error) {
	d := t.d
	if d.Kind == decision.KReplacement && len(d.Options) > 0 {
		return decision.Intent{Choices: []int{d.Options[0].Index}}, nil
	}
	if d.Kind == decision.KMulligan {
		for _, o := range d.Options {
			if o.Kind == "keep" {
				return decision.Intent{Choices: []int{o.Index}}, nil
			}
		}
	}
	return repairedMinimal(d), nil
}

// ---------------------------------------------------------------------------
// priorityTr: KPriority -> one priority decision (plus the kicker follow-up).

type prioCand struct {
	opt     int // option index, -1 for a planner cast
	plan    *decision.PaymentAction
	kicker  int // option index of the kicked twin, -1 none
	special bool
}

type priorityTr struct {
	base
	list     []prioCand
	chosen   int // index into list, -1 before the answer
	kickAsk  bool
	kickPaid bool
	done     bool
}

func (t *priorityTr) contextKind() string {
	if t.kickAsk {
		return "choice"
	}
	return "priority"
}

func (t *priorityTr) progress() (int, int, bool) { return 0, 1, !t.done }

var castMethods = map[string]string{
	"": "normal", "flashback": "flashback", "madness": "madness", "miracle": "miracle", "evoke": "evoke",
	"escape": "escape", "overload": "overload", "adventure": "adventure", "disturb": "disturb",
	"foretell": "foretell", "disguise": "disguise", "morph": "morph", "prototype": "prototype",
	"suspend": "suspend", "plot": "plot", "cascade": "cascade", "discover": "discover",
	"rebound": "rebound", "free": "free", "mdfc": "mdfc_back", "back": "mdfc_back",
}

func (t *priorityTr) candidates(b *obsBuild) ([]cand, error) {
	if t.kickAsk {
		c := t.list[t.chosen]
		src := b.refPtr(t.d.Options[c.opt].Obj)
		if src == nil {
			return nil, fmt.Errorf("kicker_source")
		}
		return []cand{
			{sem: map[string]any{"kind": "optional_cost", "source": *src, "cost": "kicker", "pay": false}, act: 0, display: "Don't pay kicker"},
			{sem: map[string]any{"kind": "optional_cost", "source": *src, "cost": "kicker", "pay": true}, act: 1, display: "Pay kicker"},
		}, nil
	}
	d := t.d
	manual := t.g.srv.opts.Mana != "autopay"
	t.list = t.list[:0]
	var out []cand
	add := func(pc prioCand, sem map[string]any, disp string) {
		out = append(out, cand{sem: sem, display: disp, act: len(t.list)})
		t.list = append(t.list, pc)
	}
	for i := range d.Options {
		if d.Options[i].Kind == "pass" {
			add(prioCand{opt: i, kicker: -1}, map[string]any{"kind": "pass"}, "Pass priority")
			break
		}
	}
	// kicked twins: a "kicked" cast of a card that also has a plain cast.
	plain := map[state.ObjID]int{} // lookup only
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind == "cast" && o.Mode == "" && o.AltCostIndex == 0 {
			plain[o.Obj] = i
		}
	}
	kickedOf := map[int]int{} // plain option -> kicked option
	skip := map[int]bool{}    // lookup only
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind == "cast" && o.Mode == "kicked" {
			if p, ok := plain[o.Obj]; ok {
				if _, dup := kickedOf[p]; !dup {
					kickedOf[p] = i
					skip[i] = true
				}
			}
		}
	}
	type key struct {
		kind, src, extra string
		idx              int
	}
	used := map[key]bool{} // lookup only
	abilityOrdinal := map[state.ObjID]int{}
	manaOrdinal := map[state.ObjID]int{}
	castPlanned := map[state.ObjID]bool{} // lookup only
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind == "pass" || o.Kind == "concede" || skip[i] {
			continue
		}
		if o.Kind == "activate" && !manual {
			continue
		}
		src, ok := b.ref(o.Obj)
		if !ok || b.hidden[o.Obj] {
			t.g.srv.stats.add("dropped_option:"+o.Kind, 1)
			t.g.srv.logf("priority option %q (%s) has no visible source; not offered", o.Label, o.Kind)
			continue
		}
		pc := prioCand{opt: i, kicker: -1}
		var sem map[string]any
		switch {
		case o.Kind == "play_land":
			face := 0
			if o.Mode == "back" || o.Mode == "mdfc" {
				face = 1
			}
			sem = map[string]any{"kind": "play_land", "source": src, "face": face}
		case o.Kind == "cast" && o.Mode == "plot":
			sem = map[string]any{"kind": "special_action", "source": src, "action": "plot"}
		case o.Kind == "cast":
			m, known := castMethods[o.Mode]
			if !known {
				m = "other"
			}
			if o.Mode == "" && o.AltCostIndex > 0 {
				m = "alternative"
			}
			for _, alt := range []string{m, "other", "alternative", "free"} {
				if !used[key{"cast", src.ObjectID, alt, 0}] {
					m = alt
					break
				}
			}
			used[key{"cast", src.ObjectID, m, 0}] = true
			if k, ok := kickedOf[i]; ok {
				pc.kicker = k
			}
			if o.Mode == "" && o.AltCostIndex == 0 {
				castPlanned[o.Obj] = true
			}
			sem = map[string]any{"kind": "cast_spell", "source": src, "method": m}
		case o.Kind == "activate":
			idx := manaOrdinal[o.Obj]
			manaOrdinal[o.Obj]++
			if f := t.g.e.G.Obj(o.Obj).Face(); f != nil && o.Ability >= 0 && o.Ability < len(f.Abilities) && f.Abilities[o.Ability].API == "Mana" {
				n := 0
				for k := 0; k < o.Ability; k++ {
					if f.Abilities[k].API == "Mana" {
						n++
					}
				}
				idx = n
			}
			var choice any
			if len(o.ManaSymbol) == 1 && strings.Contains("WUBRGC", o.ManaSymbol) {
				choice = o.ManaSymbol
			}
			for used[key{"mana", src.ObjectID, fmt.Sprint(choice), idx}] {
				idx++
			}
			used[key{"mana", src.ObjectID, fmt.Sprint(choice), idx}] = true
			sem = map[string]any{"kind": "activate_mana_ability", "source": src, "ability_index": idx, "mana_choice": choice, "cost_target": nil}
		case o.Kind == "ability" || o.Kind == "granted" || o.Kind == "station":
			idx := abilityOrdinal[o.Obj]
			abilityOrdinal[o.Obj]++
			if f := t.g.e.G.Obj(o.Obj).Face(); f != nil && o.Kind == "ability" && o.SVar == "" && o.Keyword == "" && o.Ability >= 0 && o.Ability < len(f.Abilities) {
				n := 0
				for k := 0; k < o.Ability; k++ {
					if f.Abilities[k].Kind == "AB" && f.Abilities[k].API != "Mana" {
						n++
					}
				}
				idx = n
			}
			for used[key{"ability", src.ObjectID, "", idx}] {
				idx++
			}
			used[key{"ability", src.ObjectID, "", idx}] = true
			sem = map[string]any{"kind": "activate_ability", "source": src, "ability_index": idx}
		default:
			action := "other"
			switch o.Kind {
			case "unlock":
				action = "unlock_door"
			case "turn_face_up":
				action = "turn_face_up"
			}
			if used[key{"special", src.ObjectID, action, 0}] {
				action = "other"
			}
			if used[key{"special", src.ObjectID, action, 0}] {
				// two unclassified special actions on one object collide
				t.g.srv.stats.add("dropped_option:"+o.Kind, 1)
				t.g.srv.logf("special action %q collides on %s; not offered", o.Label, src.ObjectID)
				continue
			}
			used[key{"special", src.ObjectID, action, 0}] = true
			pc.special = true
			sem = map[string]any{"kind": "special_action", "source": src, "action": action}
		}
		add(pc, sem, safeLabel(b, o))
	}
	if !manual {
		t.g.e.EnsurePaymentActions()
		for i := range d.PaymentActions {
			a := &d.PaymentActions[i]
			if len(a.Plans) == 0 || a.BaseOptionIndex != nil || castPlanned[a.Cast.Object] {
				continue
			}
			src, ok := b.ref(a.Cast.Object)
			if !ok || b.hidden[a.Cast.Object] {
				continue
			}
			castPlanned[a.Cast.Object] = true
			m := "normal"
			if used[key{"cast", src.ObjectID, m, 0}] {
				continue
			}
			used[key{"cast", src.ObjectID, m, 0}] = true
			add(prioCand{opt: -1, plan: a, kicker: -1}, map[string]any{"kind": "cast_spell", "source": src, "method": m}, a.Label)
		}
	}
	return out, nil
}

func (t *priorityTr) contextSource(b *obsBuild) *v2agent.ObjectRef {
	if t.kickAsk {
		return b.refPtr(t.d.Options[t.list[t.chosen].opt].Obj)
	}
	return nil
}

func (t *priorityTr) answer(act int) error {
	if t.kickAsk {
		t.kickPaid = act == 1
		t.done = true
		return nil
	}
	if act < 0 || act >= len(t.list) {
		return fmt.Errorf("priority_answer")
	}
	t.chosen = act
	if t.list[act].kicker >= 0 {
		t.kickAsk = true
		return nil
	}
	t.done = true
	return nil
}

func (t *priorityTr) intent() (decision.Intent, error) {
	c := t.list[t.chosen]
	if c.plan != nil {
		return decision.Intent{Payment: &decision.PaymentSelection{ActionID: c.plan.ID, Plan: decision.ClonePaymentPlan(c.plan.Plans[0])}}, nil
	}
	opt := c.opt
	if t.kickPaid && c.kicker >= 0 {
		opt = c.kicker
	}
	return decision.Intent{Choices: []int{t.d.Options[opt].Index}}, nil
}

// ---------------------------------------------------------------------------
// combatTr: KAttackers / KBlockers -> declare_attack / declare_block groups.

type combatGroup struct {
	obj      state.ObjID
	opts     []int // indices into d.Options
	required bool
}

type combatTr struct {
	base
	attack bool
	groups []combatGroup
	picks  []int // per answered group: option position in d.Options, -1 for null
}

func newCombatTr(b base, attack bool) *combatTr {
	t := &combatTr{base: b, attack: attack}
	at := map[state.ObjID]int{} // lookup only
	for i := range b.d.Options {
		o := &b.d.Options[i]
		k, ok := at[o.Obj]
		if !ok {
			k = len(t.groups)
			at[o.Obj] = k
			t.groups = append(t.groups, combatGroup{obj: o.Obj})
		}
		t.groups[k].opts = append(t.groups[k].opts, i)
		if o.Required {
			t.groups[k].required = true
		}
	}
	return t
}

func (t *combatTr) progress() (int, int, bool) {
	return len(t.picks), len(t.groups), len(t.picks) < len(t.groups)
}

func (t *combatTr) choices(picks []int) []int {
	var out []int
	for _, p := range picks {
		if p >= 0 {
			out = append(out, t.d.Options[p].Index)
		}
	}
	return out
}

// completable reports whether picks (a prefix) extends to an answer gorge
// accepts: the rest take null, a required creature its first option.
func (t *combatTr) completable(picks []int) bool {
	full := append([]int(nil), picks...)
	for k := len(picks); k < len(t.groups); k++ {
		if t.groups[k].required {
			full = append(full, t.groups[k].opts[0])
		} else {
			full = append(full, -1)
		}
	}
	return t.d.Validate(decision.Intent{Seq: t.d.Seq, Player: t.d.Player, Choices: t.choices(full)}) == nil
}

func (t *combatTr) candidates(b *obsBuild) ([]cand, error) {
	k := len(t.picks)
	grp := t.groups[k]
	me, ok := b.ref(grp.obj)
	if !ok {
		return nil, fmt.Errorf("combat_creature_not_visible")
	}
	var all []cand
	mk := func(p int) (cand, bool) {
		if t.attack {
			sem := map[string]any{"kind": "declare_attack", "attacker": me, "defender": nil}
			disp := "No attack"
			if p >= 0 {
				o := &t.d.Options[p]
				if o.Battle != 0 {
					r, ok := b.ref(o.Battle)
					if !ok {
						return cand{}, false
					}
					sem["defender"] = map[string]any{"object": r}
				} else {
					sem["defender"] = map[string]any{"player": seatOf(o.Player)}
				}
				disp = safeLabel(b, o)
			}
			return cand{sem: sem, display: disp, act: p}, true
		}
		sem := map[string]any{"kind": "declare_block", "blocker": me, "attacker": nil}
		disp := "No block"
		if p >= 0 {
			o := &t.d.Options[p]
			r, ok := b.ref(o.Attacker)
			if !ok {
				return cand{}, false
			}
			sem["attacker"] = r
			disp = safeLabel(b, o)
		}
		return cand{sem: sem, display: disp, act: p}, true
	}
	var choices []int
	if !grp.required {
		choices = append(choices, -1)
	}
	choices = append(choices, grp.opts...)
	var out []cand
	for _, p := range choices {
		c, ok := mk(p)
		if !ok {
			t.g.srv.stats.add("dropped_option:combat", 1)
			continue
		}
		all = append(all, c)
		if t.completable(append(append([]int(nil), t.picks...), p)) {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		t.g.srv.stats.add("combat_unfiltered", 1)
		return all, nil
	}
	return out, nil
}

func (t *combatTr) answer(act int) error {
	t.picks = append(t.picks, act)
	return nil
}

func (t *combatTr) intent() (decision.Intent, error) {
	return decision.Intent{Choices: t.choices(t.picks)}, nil
}

// ---------------------------------------------------------------------------
// seqTr: a Min..Max pick over object/player options, one pick per decision.

type seqMode int

const (
	seqTarget seqMode = iota
	seqSelect
	seqCost
	seqModes
)

type seqTr struct {
	base
	mode     seqMode
	purpose  string // select purpose, or cost option kind for seqCost
	chosen   []int  // option positions
	finished bool
	lk       []look
}

func newSeqTr(b base, mode seqMode, purpose string) *seqTr {
	t := &seqTr{base: b, mode: mode, purpose: purpose}
	gs := b.g.e.G
	for i := range b.d.Options {
		o := &b.d.Options[i]
		ob := gs.Obj(o.Obj)
		if ob == nil {
			continue
		}
		switch {
		case ob.Zone == state.ZLibrary:
			how := "looked_at"
			if purpose == "search" || o.Kind == "search" {
				how = "searching"
			}
			t.lk = append(t.lk, look{obj: o.Obj, how: how})
		case ob.Zone == state.ZHand && ob.Owner != b.d.Player:
			t.lk = append(t.lk, look{obj: o.Obj, how: "revealed"})
		}
	}
	return t
}

func (t *seqTr) looks() []look { return t.lk }

func (t *seqTr) fixed() bool { return t.d.Min == t.d.Max }

func (t *seqTr) progress() (int, int, bool) {
	more := !t.finished && len(t.chosen) < t.d.Max
	if t.fixed() {
		return len(t.chosen), t.d.Max, more
	}
	return 0, 1, more
}

func (t *seqTr) contextPurpose() string {
	switch t.mode {
	case seqSelect:
		return t.purpose
	case seqModes:
		return "modes"
	}
	return ""
}

func (t *seqTr) contextSource(b *obsBuild) *v2agent.ObjectRef {
	return t.sourceRef(b)
}

func (t *seqTr) indices(pos []int) []int {
	out := make([]int, len(pos))
	for i, p := range pos {
		out[i] = t.d.Options[p].Index
	}
	return out
}

func (t *seqTr) valid(pos []int) bool {
	return t.d.Validate(decision.Intent{Seq: t.d.Seq, Player: t.d.Player, Choices: t.indices(pos)}) == nil
}

func (t *seqTr) admissible(pos []int, p int) bool {
	d := t.d
	for _, c := range pos {
		if c == p && !d.Repeatable {
			return false
		}
	}
	chosen := t.indices(pos)
	o := &d.Options[p]
	if o.Group != "" {
		n := 0
		for _, c := range pos {
			if d.Options[c].Group == o.Group {
				n++
			}
		}
		if n >= d.GroupCapFor(o.Group) {
			return false
		}
	}
	if d.HasBudget() {
		sum := o.Value
		for _, c := range pos {
			sum += d.Options[c].Value
		}
		if sum > d.MaxSum {
			return false
		}
	}
	if d.SetPropMode != "" && !decision.SetPropAdmits(d.SetPropMode, d.SetPropsOf(chosen), o.SetProps) {
		return false
	}
	if d.TargetsWithSameController && len(pos) > 0 && d.Options[pos[0]].Controller != o.Controller {
		return false
	}
	return true
}

// completable: pos extends (greedily, in option order) to a valid answer.
func (t *seqTr) completable(pos []int) bool {
	cur := append([]int(nil), pos...)
	for {
		if len(cur) >= t.d.Min && t.valid(cur) {
			return true
		}
		if len(cur) >= t.d.Max {
			return false
		}
		grew := false
		for p := range t.d.Options {
			if t.admissible(cur, p) {
				cur = append(cur, p)
				grew = true
				break
			}
		}
		if !grew {
			return false
		}
	}
}

func (t *seqTr) candidates(b *obsBuild) ([]cand, error) {
	d := t.d
	src := t.sourceRef(b)
	n := len(t.chosen)
	var out []cand
	needSource := t.mode == seqTarget || t.mode == seqCost || t.mode == seqModes
	degrade := needSource && src == nil
	if degrade {
		t.g.srv.stats.add("degraded_sourceless:"+t.kind, 1)
	}
	purpose := t.purpose
	if t.mode != seqSelect {
		purpose = "other"
	}
	if t.mode == seqModes {
		purpose = "modes"
	}
	if !t.fixed() && n >= d.Min && t.valid(t.chosen) {
		var sem map[string]any
		switch {
		case t.mode == seqTarget && !degrade:
			sem = map[string]any{"kind": "finish_target_selection", "source": *src, "slot": 0, "selected_count": n}
		default:
			sem = map[string]any{"kind": "finish_selection", "source": refOrNil(src), "purpose": purpose, "selected_count": n}
		}
		out = append(out, cand{sem: sem, display: "Done", act: -1})
	}
	for p := range d.Options {
		if !t.admissible(t.chosen, p) || !t.completable(append(append([]int(nil), t.chosen...), p)) {
			continue
		}
		o := &d.Options[p]
		var sem map[string]any
		switch {
		case t.mode == seqModes && !degrade:
			sem = map[string]any{"kind": "choose_spell_mode", "source": *src, "mode_index": p, "mode_count": len(d.Options),
				"selected_count": n, "minimum": d.Min, "maximum": d.Max}
		case t.mode == seqModes:
			sem = map[string]any{"kind": "choose_option", "source": nil, "purpose": "other", "option_index": p,
				"option_count": len(d.Options), "option_label": nullIfEmpty(safeLabel(b, o))}
		default:
			tgt, ok := targetOf(b, o, false)
			if !ok {
				t.g.srv.stats.add("dropped_option:"+t.kind, 1)
				t.g.srv.logf("%s option %q has no visible object; not offered", t.kind, o.Label)
				continue
			}
			switch {
			case t.mode == seqTarget && !degrade:
				sem = map[string]any{"kind": "choose_target", "source": *src, "slot": 0, "target": tgt,
					"selected_count": n, "minimum": d.Min, "maximum": d.Max}
			case t.mode == seqCost && !degrade && o.Obj != 0:
				sem = map[string]any{"kind": "choose_cost_target", "source": *src, "cost_kind": costKind(t.purpose),
					"candidate": tgt["object"], "selected_count": n, "minimum": d.Min, "maximum": d.Max}
			default:
				pur := purpose
				if t.mode == seqCost {
					pur = costSelectPurpose(t.purpose)
				}
				sem = map[string]any{"kind": "select_object", "source": refOrNil(src), "purpose": pur, "choice": tgt,
					"selected_count": n, "minimum": d.Min, "maximum": d.Max}
			}
		}
		out = append(out, cand{sem: sem, display: safeLabel(b, o), act: p, hidden: hiddenKey(b, o.Obj)})
	}
	return out, nil
}

func costKind(k string) string {
	switch k {
	case "sacrifice":
		return "sacrifice"
	case "returncost":
		return "return_to_hand"
	case "tapcost":
		return "tap"
	case "revealcost":
		return "reveal"
	case "subcounter":
		return "remove_counter"
	}
	return "other"
}

func costSelectPurpose(k string) string {
	switch k {
	case "sacrifice":
		return "sacrifice"
	case "returncost":
		return "return_to_hand"
	case "tapcost":
		return "tap"
	case "revealcost":
		return "reveal"
	}
	return "other"
}

func (t *seqTr) answer(act int) error {
	if act < 0 {
		t.finished = true
		return nil
	}
	t.chosen = append(t.chosen, act)
	return nil
}

func (t *seqTr) intent() (decision.Intent, error) {
	return decision.Intent{Choices: t.indices(t.chosen)}, nil
}

// ---------------------------------------------------------------------------
// valueTr: one pick of a value (number, colour, boolean, name) or an
// unless-payment answer.

type valueTr struct {
	base
	vk     string
	pick   int
	done   bool
	fallbk bool
}

func newValueTr(b base, vk string) translator {
	t := &valueTr{base: b, vk: vk, pick: -1}
	if b.d.Min != 1 || b.d.Max != 1 {
		b.kind = "enumerated:" + string(b.d.Kind)
		return newEnumTr(b)
	}
	return t
}

func (t *valueTr) progress() (int, int, bool) { return 0, 1, !t.done }

func (t *valueTr) contextSource(b *obsBuild) *v2agent.ObjectRef { return t.sourceRef(b) }

func (t *valueTr) candidates(b *obsBuild) ([]cand, error) {
	d := t.d
	src := t.sourceRef(b)
	var out []cand
	minX, maxX := int64(1<<31-1), int64(-1<<31)
	values := make([]int64, len(d.Options))
	if t.vk == "x" || t.vk == "number" {
		for i := range d.Options {
			v := int64(d.Options[i].Amount)
			if t.vk == "number" {
				if n, err := strconv.ParseInt(strings.TrimSpace(d.Options[i].Label), 10, 32); err == nil {
					v = n
				} else {
					v = int64(i)
				}
			}
			values[i] = v
			minX, maxX = min(minX, v), max(maxX, v)
		}
	}
	for i := range d.Options {
		o := &d.Options[i]
		var sem map[string]any
		switch t.vk {
		case "x", "number":
			pur := "x_value"
			if t.vk == "number" {
				pur = "amount"
			}
			sem = map[string]any{"kind": "choose_number", "source": refOrNil(src), "purpose": pur, "value": values[i], "minimum": minX, "maximum": maxX}
		case "color", "color_mana":
			pur := "effect"
			if o.Kind == "mana" || t.vk == "color_mana" {
				pur = "mana"
			}
			sem = map[string]any{"kind": "choose_color", "source": refOrNil(src), "purpose": pur, "color": optionColor(o)}
		case "boolean", "optional_trigger", "optional_replacement":
			pur := "may_ability"
			if t.vk != "boolean" {
				pur = t.vk
			}
			val := o.Kind == "yes" || o.Kind == "apply"
			sem = map[string]any{"kind": "choose_boolean", "source": refOrNil(src), "purpose": pur, "value": val}
		case "type":
			l := strings.ToLower(strings.TrimSpace(o.Label))
			if _, ok := cardTypeWords[strings.TrimSpace(o.Label)]; ok {
				sem = map[string]any{"kind": "choose_name", "source": refOrNil(src), "purpose": "card_type", "value": cardTypeWords[strings.TrimSpace(o.Label)]}
			} else if s := snake(l); s != "" {
				sem = map[string]any{"kind": "choose_name", "source": refOrNil(src), "purpose": "creature_type", "value": s}
			}
		case "name":
			if t.g.inDomain(o.Label) {
				sem = map[string]any{"kind": "choose_name", "source": refOrNil(src), "purpose": "card_name", "value": o.Label}
			}
		case "unless":
			if src != nil {
				sem = map[string]any{"kind": "optional_cost", "source": *src, "cost": "unless_payment", "pay": o.Mode == decision.ModeUnlessPay}
			}
		}
		if sem == nil {
			t.g.srv.stats.add("value_fallback:"+t.vk, 1)
			return (&enumTr{base: t.base}).singles(b)
		}
		out = append(out, cand{sem: sem, display: safeLabel(b, o), act: i})
	}
	// distinct values, else the generic single pick
	seen := map[string]bool{} // lookup only
	for _, c := range out {
		k := fmt.Sprint(c.sem)
		if seen[k] {
			t.g.srv.stats.add("value_fallback_dup:"+t.vk, 1)
			return (&enumTr{base: t.base}).singles(b)
		}
		seen[k] = true
	}
	return out, nil
}

func (t *valueTr) answer(act int) error { t.pick, t.done = act, true; return nil }

func (t *valueTr) intent() (decision.Intent, error) {
	return decision.Intent{Choices: []int{t.d.Options[t.pick].Index}}, nil
}

// ---------------------------------------------------------------------------
// orderTr: KTriggerOrder -> order_pick triggers, n-1 posed picks.

type orderTr struct {
	base
	order []int
}

func newOrderTr(b base) translator {
	if b.d.Min != len(b.d.Options) || b.d.Max != len(b.d.Options) || len(b.d.Options) < 2 {
		b.kind = "enumerated:" + string(b.d.Kind)
		return newEnumTr(b)
	}
	return &orderTr{base: b}
}

func (t *orderTr) progress() (int, int, bool) {
	n := len(t.d.Options)
	return len(t.order), n - 1, len(t.order) < n-1
}

func (t *orderTr) contextPurpose() string { return "triggers" }

func (t *orderTr) candidates(b *obsBuild) ([]cand, error) {
	d := t.d
	n := len(d.Options)
	placed := map[int]bool{} // lookup only
	for _, p := range t.order {
		placed[p] = true
	}
	instance := map[string]int{}
	inst := make([]int, n)
	for i := range d.Options {
		k := fmt.Sprintf("%d\x00%s", d.Options[i].Obj, d.Options[i].Label)
		inst[i] = instance[k]
		instance[k]++
	}
	var out []cand
	for i := range d.Options {
		if placed[i] {
			continue
		}
		o := &d.Options[i]
		srcRef := b.refPtr(o.Obj)
		if b.hidden[o.Obj] {
			srcRef = nil
		}
		var srcName any
		if srcRef != nil && srcRef.CardName != nil {
			srcName = *srcRef.CardName
		}
		label := nullIfEmpty(safeLabel(b, o))
		if srcRef == nil {
			label = nil
		}
		item := map[string]any{"trigger": map[string]any{"source": refOrNil(srcRef), "source_name": srcName,
			"ability_index": nil, "event_objects": []any{}, "instance": inst[i], "label": label}}
		out = append(out, cand{sem: map[string]any{"kind": "order_pick", "source": nil, "purpose": "triggers",
			"item": item, "position": len(t.order), "count": n}, display: safeLabel(b, o), act: i})
	}
	return out, nil
}

func (t *orderTr) answer(act int) error { t.order = append(t.order, act); return nil }

func (t *orderTr) intent() (decision.Intent, error) {
	d := t.d
	placed := map[int]bool{} // lookup only
	var ch []int
	for _, p := range t.order {
		placed[p] = true
		ch = append(ch, d.Options[p].Index)
	}
	for i := range d.Options {
		if !placed[i] {
			ch = append(ch, d.Options[i].Index)
		}
	}
	return decision.Intent{Choices: ch}, nil
}

// ---------------------------------------------------------------------------
// arrangeTr: KArrange -> spec 7.5's 2n-1 arrangement group.

var arrangeOrder = []string{"top", "bottom", "graveyard", "exile", "hand", "battlefield"}

type arrangeTr struct {
	base
	dest    []string // per option: its chosen destination
	bDest   string   // pile B's destination
	allB    bool     // every card goes to pile A, whose destination is bDest (dig_bottom, hideaway_bottom)
	order   []int    // order picks (option positions)
	lk      []look
	purpose string
	// pendingDests are the destinations offered by the posed arrange_card.
	pendingDests []string
}

func newArrangeTr(b base) translator {
	d := b.d
	if len(d.Options) == 0 {
		return nil
	}
	k := d.Options[0].Kind
	for _, o := range d.Options {
		if o.Kind != k || o.Obj == 0 {
			return nil
		}
	}
	t := &arrangeTr{base: b}
	switch k {
	case "bottom":
		t.bDest, t.purpose = "bottom", "scry"
	case "graveyard":
		t.bDest, t.purpose = "graveyard", "surveil"
	case "exile":
		t.bDest, t.purpose = "exile", "other"
	case "hand":
		t.bDest, t.purpose = "hand", "dig"
	case "dig_bottom", "hideaway_bottom":
		t.bDest, t.purpose, t.allB = "bottom", "dig", true
		if d.Min != len(d.Options) {
			return nil
		}
	default:
		return nil
	}
	for _, o := range d.Options {
		if ob := b.g.e.G.Obj(o.Obj); ob != nil && ob.Zone == state.ZLibrary {
			t.lk = append(t.lk, look{obj: o.Obj, how: "looked_at"})
		}
	}
	return t
}

func (t *arrangeTr) looks() []look          { return t.lk }
func (t *arrangeTr) contextPurpose() string { return "" }
func (t *arrangeTr) contextSource(b *obsBuild) *v2agent.ObjectRef {
	return t.sourceRef(b)
}

func (t *arrangeTr) progress() (int, int, bool) {
	n := len(t.d.Options)
	done := len(t.dest) + len(t.order)
	return done, 2*n - 1, done < 2*n-1
}

func (t *arrangeTr) tops() int {
	n := 0
	for _, dst := range t.dest {
		if dst == "top" {
			n++
		}
	}
	return n
}

func (t *arrangeTr) candidates(b *obsBuild) ([]cand, error) {
	d := t.d
	n := len(d.Options)
	src := refOrNil(t.sourceRef(b))
	if len(t.dest) < n {
		k := len(t.dest)
		card, ok := b.ref(d.Options[k].Obj)
		if !ok {
			return nil, fmt.Errorf("arrange_card_not_visible")
		}
		var dests []string
		if t.allB {
			dests = []string{t.bDest}
		} else {
			tops := t.tops()
			if tops < d.Max {
				dests = append(dests, "top")
			}
			if tops+(n-k-1) >= d.Min {
				dests = append(dests, t.bDest)
			}
		}
		var out []cand
		for i, dst := range dests {
			out = append(out, cand{sem: map[string]any{"kind": "arrange_card", "source": src, "purpose": t.purpose, "card": card,
				"card_index": k, "card_count": n, "destination": dst}, display: safeLabel(b, &d.Options[k]) + " -> " + dst, act: i,
				hidden: hiddenKey(b, d.Options[k].Obj)})
		}
		t.pendingDests = dests
		return out, nil
	}
	// ordering picks: the current destination's unplaced cards
	placed := map[int]bool{} // lookup only
	for _, p := range t.order {
		placed[p] = true
	}
	var group []int
	for _, dst := range arrangeOrder {
		for i := range d.Options {
			if !placed[i] && t.dest[i] == dst {
				group = append(group, i)
			}
		}
		if len(group) > 0 {
			break
		}
	}
	var out []cand
	for _, i := range group {
		card, _ := b.ref(d.Options[i].Obj)
		out = append(out, cand{sem: map[string]any{"kind": "order_pick", "source": src, "purpose": "arrangement",
			"item": map[string]any{"object": card}, "position": len(t.order), "count": n}, display: safeLabel(b, &d.Options[i]), act: i,
			hidden: hiddenKey(b, d.Options[i].Obj)})
	}
	return out, nil
}

func (t *arrangeTr) answer(act int) error {
	if len(t.dest) < len(t.d.Options) {
		if act < 0 || act >= len(t.pendingDests) {
			return fmt.Errorf("arrange_answer")
		}
		t.dest = append(t.dest, t.pendingDests[act])
		return nil
	}
	t.order = append(t.order, act)
	return nil
}

func (t *arrangeTr) intent() (decision.Intent, error) {
	d := t.d
	placed := map[int]bool{} // lookup only
	full := append([]int(nil), t.order...)
	for _, p := range t.order {
		placed[p] = true
	}
	for _, dst := range arrangeOrder {
		for i := range d.Options {
			if !placed[i] && t.dest[i] == dst {
				full = append(full, i)
				placed[i] = true
			}
		}
	}
	var pileA, pileB []int
	for _, i := range full {
		if t.allB || t.dest[i] == "top" {
			pileA = append(pileA, d.Options[i].Index)
		} else {
			pileB = append(pileB, d.Options[i].Index)
		}
	}
	in := decision.Intent{Choices: pileA}
	if d.Restable && len(pileB) > 0 {
		in.Rest = pileB
	}
	return in, nil
}

// ---------------------------------------------------------------------------
// enumTr: one choose_option over every complete answer gorge accepts.

type enumTr struct {
	base
	answers [][]int
	pick    int
	done    bool
	built   bool
}

func newEnumTr(b base) translator {
	b.g.srv.stats.add("enumerated:"+string(b.d.Kind), 1)
	return &enumTr{base: b, pick: -1}
}

func (t *enumTr) progress() (int, int, bool) { return 0, 1, !t.done }

// singles is the Min == Max == 1 case: one choose_option per option.
func (t *enumTr) singles(b *obsBuild) ([]cand, error) {
	d := t.d
	src := refOrNil(t.sourceRef(b))
	var out []cand
	for i := range d.Options {
		l := safeLabel(b, &d.Options[i])
		out = append(out, cand{sem: map[string]any{"kind": "choose_option", "source": src, "purpose": "other",
			"option_index": i, "option_count": len(d.Options), "option_label": nullIfEmpty(l)}, display: l, act: i})
	}
	return out, nil
}

func (t *enumTr) build() error {
	if t.built {
		return nil
	}
	t.built = true
	d := t.d
	n := len(d.Options)
	valid := func(c []int) bool {
		ch := make([]int, len(c))
		for i, p := range c {
			ch[i] = d.Options[p].Index
		}
		return d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}) == nil
	}
	nodes := 0
	var rec func(start int, cur []int) error
	rec = func(start int, cur []int) error {
		if nodes++; nodes > 200000 {
			return fmt.Errorf("enumeration_limit")
		}
		if len(cur) >= d.Min && len(cur) <= d.Max && valid(cur) {
			t.answers = append(t.answers, append([]int(nil), cur...))
			if len(t.answers) > 4096 {
				return fmt.Errorf("candidate_limit")
			}
		}
		if len(cur) == d.Max {
			return nil
		}
		for p := start; p < n; p++ {
			if err := rec(p+1, append(cur, p)); err != nil {
				return err
			}
		}
		return nil
	}
	return rec(0, nil)
}

func (t *enumTr) candidates(b *obsBuild) ([]cand, error) {
	if err := t.build(); err != nil {
		return nil, err
	}
	d := t.d
	src := refOrNil(t.sourceRef(b))
	var out []cand
	for k, a := range t.answers {
		var labels []string
		for _, p := range a {
			l := safeLabel(b, &d.Options[p])
			if l == "" {
				l = "option " + strconv.Itoa(p)
			}
			labels = append(labels, l)
		}
		label := strings.Join(labels, " + ")
		if label == "" {
			label = "none"
		}
		out = append(out, cand{sem: map[string]any{"kind": "choose_option", "source": src, "purpose": "other",
			"option_index": k, "option_count": len(t.answers), "option_label": label}, display: label, act: k})
	}
	return out, nil
}

func (t *enumTr) answer(act int) error { t.pick, t.done = act, true; return nil }

func (t *enumTr) intent() (decision.Intent, error) {
	if t.pick < 0 || t.pick >= len(t.answers) {
		return decision.Intent{}, fmt.Errorf("enum_answer")
	}
	var ch []int
	for _, p := range t.answers[t.pick] {
		ch = append(ch, t.d.Options[p].Index)
	}
	return decision.Intent{Choices: ch}, nil
}
