package v2shadow

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/state"
)

// A follow-up plan is what the gorge policy answered, in the shadow, for
// the decisions its own root answer led to (targets, X, modes, colours,
// costs, a kicker) before priority came back. The wire poses those same
// decisions one substep at a time (spec §8); each is answered from the
// plan by semantics (kind, object, value), never by position, so a
// different decomposition or order on the wire still finds its answer.

// planRef is one picked object or player, recognisable on the wire.
type planRef struct {
	player bool
	seat   string
	v2id   string // the object's v2 id when the shadow staged it
	name   string // its name (library and hidden-hand cards: shown fresh)
	used   bool
}

// planStep is one recorded follow-up decision.
type planStep struct {
	kind decision.Kind
	d    *decision.Decision
	in   decision.Intent
	// refs are the chosen objects/players in answer order (seq decisions).
	refs []planRef
	// order is the answer's option positions in order (orders, modes).
	order []int
	pos   int // next unconsumed part
	done  bool
	// src is the name of the decision's source card ("" unknown): a wire
	// decision naming another source is not this step's.
	src string
	// names are the options' object names (by option position; the label
	// for an option naming no object).
	names []string
}

// followPlan is one root decision's recorded follow-ups.
type followPlan struct {
	steps []*planStep
	// kicker: the root cast chose an optional-cost mode (kicked,
	// buyback...): the wire's optional_cost follow-up answers pay:true.
	kicker    bool
	hasKicker bool
}

func optByIndex(d *decision.Decision, idx int) (*decision.Option, int) {
	for i := range d.Options {
		if d.Options[i].Index == idx {
			return &d.Options[i], i
		}
	}
	return nil, -1
}

// record adds one shadow follow-up (nd answered with in).
func (fp *followPlan) record(sh *Shadow, nd *decision.Decision, in decision.Intent) {
	dc := nd.CloneValue() // the engine reuses its pending decision
	nd = &dc
	in.Choices = append([]int(nil), in.Choices...)
	in.Rest = append([]int(nil), in.Rest...)
	st := &planStep{kind: nd.Kind, d: nd, in: in, src: sourceName(sh, nd.Source)}
	for i := range nd.Options {
		o := &nd.Options[i]
		n := o.Label
		if ob := sh.E.G.Obj(o.Obj); o.Obj != 0 && ob != nil && ob.Face() != nil {
			n = ob.Face().Name
		}
		st.names = append(st.names, n)
	}
	for _, c := range in.Choices {
		o, pos := optByIndex(nd, c)
		if o == nil {
			continue
		}
		st.order = append(st.order, pos)
		st.refs = append(st.refs, refOf(sh, o))
	}
	for _, c := range in.Rest {
		if o, _ := optByIndex(nd, c); o != nil {
			// Rest names pile B of an arrangement: kept for arrange.
			_ = o
		}
	}
	fp.steps = append(fp.steps, st)
}

// sourceName is the folded face names ("|"-joined) of the card behind a
// decision source object (an ability names its source's card), "" when
// unknown.
func sourceName(sh *Shadow, id state.ObjID) string {
	o := sh.E.G.Obj(id)
	for n := 0; o != nil && o.Face() == nil && o.Source != 0 && n < 4; n++ {
		o = sh.E.G.Obj(o.Source)
	}
	if o == nil || o.Face() == nil {
		return ""
	}
	if o.Card == nil {
		return fold(o.Face().Name)
	}
	var names []string
	for _, f := range o.Card.Faces {
		if f != nil {
			names = append(names, fold(f.Name))
		}
	}
	return strings.Join(names, "|")
}

// wireSource is the wire decision's source card name (context, else the
// first candidate naming one), "" when none is named.
func wireSource(d *v2agent.Decision) string {
	if d.Seat != nil && d.Seat.Context.Source != nil && d.Seat.Context.Source.CardName != nil {
		return *d.Seat.Context.Source.CardName
	}
	for i := range d.Candidates {
		if s := d.Candidates[i].Semantic.Source; s != nil && s.CardName != nil {
			return *s.CardName
		}
	}
	return ""
}

func refOf(sh *Shadow, o *decision.Option) planRef {
	if o.Obj == 0 {
		return planRef{player: true, seat: SeatName(o.Player)}
	}
	r := planRef{v2id: sh.ObjToV2[o.Obj]}
	if ob := sh.E.G.Obj(o.Obj); ob != nil && ob.Face() != nil {
		r.name = ob.Face().Name
	}
	return r
}

// matchesTarget reports whether wire target t names r.
func (r *planRef) matchesTarget(t *v2agent.TargetRef) bool {
	if t == nil {
		return false
	}
	if r.player {
		return t.Player != nil && *t.Player == r.seat
	}
	return t.Object != nil && r.matchesObject(t.Object)
}

func (r *planRef) matchesObject(o *v2agent.ObjectRef) bool {
	if o == nil || r.player {
		return false
	}
	if r.v2id != "" && o.ObjectID == r.v2id {
		return true
	}
	// A card shown from a hidden zone has a fresh id: match by name.
	if (o.Zone == "library" || o.Zone == "hand") && r.name != "" && o.CardName != nil && fold(*o.CardName) == fold(r.name) && r.v2id == "" {
		return true
	}
	return false
}

// answer finds, for wire decision d, the candidate the plan picks; ok
// false when no step of the plan speaks to d.
func (fp *followPlan) answer(d *v2agent.Decision) (int, bool) {
	if fp == nil {
		return 0, false
	}
	kinds := map[string]bool{} // lookup only
	for i := range d.Candidates {
		kinds[d.Candidates[i].Semantic.Kind] = true
	}
	if kinds["optional_cost"] && fp.hasKicker {
		for i := range d.Candidates {
			s := &d.Candidates[i].Semantic
			if s.Kind == "optional_cost" && s.Cost != "unless_payment" && s.Pay != nil && *s.Pay == fp.kicker {
				fp.hasKicker = false
				return i, true
			}
		}
	}
	want := wireSource(d)
	for _, st := range fp.steps {
		if st.done || (want != "" && st.src != "" && !strings.Contains("|"+st.src+"|", "|"+fold(want)+"|")) {
			continue
		}
		if k, ok := st.answer(d, kinds); ok {
			return k, true
		}
	}
	return 0, false
}

func (st *planStep) answer(d *v2agent.Decision, kinds map[string]bool) (int, bool) {
	switch {
	case kinds["choose_target"] || kinds["finish_target_selection"] || kinds["select_object"] ||
		kinds["choose_cost_target"] || kinds["finish_selection"]:
		return st.answerSeq(d)
	case kinds["choose_spell_mode"]:
		for st.pos < len(st.order) {
			want := st.order[st.pos]
			for i := range d.Candidates {
				s := &d.Candidates[i].Semantic
				if s.Kind == "choose_spell_mode" && int(s.ModeIndex) == want {
					st.pos++
					if st.pos >= len(st.order) {
						st.done = true
					}
					return i, true
				}
			}
			return 0, false
		}
	case kinds["choose_number"], kinds["choose_color"], kinds["choose_boolean"], kinds["choose_name"],
		kinds["optional_cost"]:
		return st.answerValue(d)
	case kinds["order_pick"] && st.kind == decision.KTriggerOrder:
		return st.answerTriggerOrder(d)
	case kinds["arrange_card"] || kinds["order_pick"] && st.kind == decision.KArrange:
		return st.answerArrange(d)
	case kinds["choose_option"]:
		return st.answerEnumerated(d)
	}
	return 0, false
}

func (st *planStep) answerSeq(d *v2agent.Decision) (int, bool) {
	switch st.kind {
	case decision.KTarget, decision.KChoose, decision.KModes:
	default:
		return 0, false
	}
	for ri := range st.refs {
		r := &st.refs[ri]
		if r.used {
			continue
		}
		for i := range d.Candidates {
			s := &d.Candidates[i].Semantic
			hit := false
			switch s.Kind {
			case "choose_target":
				hit = r.matchesTarget(s.Target)
			case "select_object":
				var t v2agent.TargetRef
				if len(s.Choice) > 0 && json.Unmarshal(s.Choice, &t) == nil {
					hit = r.matchesTarget(&t)
				}
			case "choose_cost_target":
				hit = r.matchesObject(s.Candidate)
			}
			if hit {
				r.used = true
				st.markDone()
				return i, true
			}
		}
	}
	allUsed := true
	for ri := range st.refs {
		if !st.refs[ri].used {
			allUsed = false
		}
	}
	if allUsed {
		for i := range d.Candidates {
			k := d.Candidates[i].Semantic.Kind
			if k == "finish_target_selection" || k == "finish_selection" {
				st.done = true
				return i, true
			}
		}
	}
	return 0, false
}

func (st *planStep) markDone() {
	for _, r := range st.refs {
		if !r.used {
			return
		}
	}
	// A variable selection still asks "finish": leave the step open when
	// the decision was not fixed-size.
	if st.d.Min == st.d.Max {
		st.done = true
	}
}

// answerValue answers a single-pick value decision.
func (st *planStep) answerValue(d *v2agent.Decision) (int, bool) {
	if len(st.in.Choices) != 1 {
		return 0, false
	}
	o, _ := optByIndex(st.d, st.in.Choices[0])
	if o == nil {
		return 0, false
	}
	for i := range d.Candidates {
		s := &d.Candidates[i].Semantic
		hit := false
		switch s.Kind {
		case "choose_number":
			var v int64
			if json.Unmarshal(s.Value, &v) == nil {
				want := int64(o.Amount)
				if o.Kind == "number" {
					if n, err := strconv.ParseInt(strings.TrimSpace(o.Label), 10, 32); err == nil {
						want = n
					}
				}
				hit = (o.Kind == "x" || o.Kind == "number") && v == want
			}
		case "choose_color":
			hit = s.Color != "" && s.Color == optionColor(o)
		case "choose_boolean":
			var v bool
			if json.Unmarshal(s.Value, &v) == nil {
				hit = (o.Kind == "yes" || o.Kind == "apply" || o.Kind == "no" || o.Kind == "decline") &&
					v == (o.Kind == "yes" || o.Kind == "apply")
			}
		case "choose_name":
			var v string
			if json.Unmarshal(s.Value, &v) == nil {
				hit = v == o.Label || v == snakeWord(o.Label) || strings.EqualFold(v, strings.TrimSpace(o.Label))
			}
		case "optional_cost":
			if s.Cost == "unless_payment" && s.Pay != nil {
				hit = (o.Mode == decision.ModeUnlessPay) == *s.Pay && (o.Mode == decision.ModeUnlessPay || o.Mode == decision.ModeUnlessDecline)
			}
		}
		if hit {
			st.done = true
			return i, true
		}
	}
	return 0, false
}

func (st *planStep) answerTriggerOrder(d *v2agent.Decision) (int, bool) {
	for st.pos < len(st.order) {
		o := &st.d.Options[st.order[st.pos]]
		want := refOf0(st, o)
		for i := range d.Candidates {
			s := &d.Candidates[i].Semantic
			if s.Kind != "order_pick" || s.Item == nil || s.Item.Trigger == nil {
				continue
			}
			tr := s.Item.Trigger
			srcOK := want == "" || (tr.Source != nil && tr.Source.ObjectID == want)
			lblOK := tr.Label == nil || *tr.Label == o.Label
			if srcOK && lblOK {
				st.pos++
				if st.pos >= len(st.order)-1 {
					st.done = true
				}
				return i, true
			}
		}
		return 0, false
	}
	return 0, false
}

func refOf0(st *planStep, o *decision.Option) string {
	for i, pos := range st.order {
		if &st.d.Options[pos] == o {
			return st.refs[i].v2id
		}
	}
	return ""
}

// answerArrange answers the 2n-1 arrangement group: per card its
// destination (top when in the answer's Choices, else pile B), then the
// order of pile A (Choices order) and pile B (Rest order).
func (st *planStep) answerArrange(d *v2agent.Decision) (int, bool) {
	if st.kind != decision.KArrange {
		return 0, false
	}
	top := map[string]int{}  // name -> count in pile A (lookup only)
	rest := map[string]int{} // lookup only
	var orderNames []string
	for _, c := range st.in.Choices {
		if o, _ := optByIndex(st.d, c); o != nil {
			n := objName(st, o)
			top[fold(n)]++
			orderNames = append(orderNames, fold(n))
		}
	}
	for _, c := range st.in.Rest {
		if o, _ := optByIndex(st.d, c); o != nil {
			n := objName(st, o)
			rest[fold(n)]++
			orderNames = append(orderNames, fold(n))
		}
	}
	if len(st.in.Rest) == 0 {
		// Pile B unordered by the answer: the engine keeps option order.
		inA := map[int]bool{} // lookup only
		for _, c := range st.in.Choices {
			inA[c] = true
		}
		for i := range st.d.Options {
			if o := &st.d.Options[i]; !inA[o.Index] {
				orderNames = append(orderNames, fold(objName(st, o)))
			}
		}
	}
	// Count what the wire has already placed from this group: the
	// substep index tells the position.
	sub := int(d.Seat.Group.SubstepIndex)
	for i := range d.Candidates {
		s := &d.Candidates[i].Semantic
		if s.Kind == "arrange_card" && s.Card != nil && s.Card.CardName != nil {
			n := fold(*s.Card.CardName)
			wantTop := top[n] > 0
			if (s.Destination == "top") == wantTop {
				return i, true
			}
		}
	}
	if len(d.Candidates) > 0 && d.Candidates[0].Semantic.Kind == "order_pick" {
		n := int(d.Candidates[0].Semantic.Count)
		pos := sub - n
		if pos >= 0 && pos < len(orderNames) {
			for i := range d.Candidates {
				s := &d.Candidates[i].Semantic
				if s.Item != nil && s.Item.Object != nil && s.Item.Object.CardName != nil && fold(*s.Item.Object.CardName) == orderNames[pos] {
					if pos >= n-2 {
						st.done = true
					}
					return i, true
				}
			}
		}
	}
	return 0, false
}

func objName(st *planStep, o *decision.Option) string {
	for i := range st.d.Options {
		if &st.d.Options[i] == o && i < len(st.names) {
			return st.names[i]
		}
	}
	return o.Label
}

// answerEnumerated matches an enumerated choose_option by the joined
// labels of the recorded answer (the engine's "A + B" label form).
func (st *planStep) answerEnumerated(d *v2agent.Decision) (int, bool) {
	var labels []string
	for _, c := range st.in.Choices {
		o, _ := optByIndex(st.d, c)
		if o == nil {
			return 0, false
		}
		labels = append(labels, o.Label)
	}
	want := strings.Join(labels, " + ")
	if want == "" {
		want = "none"
	}
	for i := range d.Candidates {
		s := &d.Candidates[i].Semantic
		if s.Kind == "choose_option" && s.OptionLabel != nil && *s.OptionLabel == want {
			st.done = true
			return i, true
		}
	}
	// A single pick: the option's own position.
	if len(st.order) == 1 {
		for i := range d.Candidates {
			s := &d.Candidates[i].Semantic
			if s.Kind == "choose_option" && int(s.OptionCount) == len(st.d.Options) && int(s.OptionIndex) == st.order[0] {
				st.done = true
				return i, true
			}
		}
	}
	return 0, false
}

var manaColorWords = map[string]string{"W": "white", "U": "blue", "B": "black", "R": "red", "G": "green"}

// optionColor is a colour option's spec 6.10 colour word (the engine's
// own rule, v2engine translate.go).
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

func snakeWord(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\'':
		case r == ' ' || r == '-':
			b.WriteByte('_')
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "_")
}

// describe renders the plan (traces).
func (fp *followPlan) describe() string {
	if fp == nil {
		return "plan=nil"
	}
	var parts []string
	for _, st := range fp.steps {
		var opts []string
		for _, c := range st.in.Choices {
			if o, _ := optByIndex(st.d, c); o != nil {
				opts = append(opts, fmt.Sprintf("%s/%q/%s", o.Kind, o.Label, o.ManaSymbol))
			}
		}
		parts = append(parts, fmt.Sprintf("{%s src=%s done=%v pos=%d min=%d max=%d chose=%v rest=%v nopts=%d}", st.kind, st.src, st.done, st.pos, st.d.Min, st.d.Max, opts, st.in.Rest, len(st.d.Options)))
	}
	return fmt.Sprintf("plan kick=%v/%v %s", fp.hasKicker, fp.kicker, strings.Join(parts, " "))
}
