package searchbench

import (
	"fmt"
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// CanonOption is one canonical answer at an item decision. Index 0 of every
// Canon is the passive answer (Pass / don't attack with X / X doesn't block).
type CanonOption struct {
	// Label is the option's canonical text: "Pass", "Cast <name>",
	// "Activate <engine ability label>", "Play <name>", "No attack",
	// "Attack", "No block", "Block <attacker alias>".
	Label string
	// Act is false only for the passive answer.
	Act bool
	// Kind is "pass", "cast", "ability", "land", "special", "noattack",
	// "attack", "noblock" or "block".
	Kind string
	// Name is the card (cast/land) or source (ability) name.
	Name string
	// Mode is a cast's payment mode ("" the card's own cost).
	Mode string
	// Attacker names a block option's attacker.
	Attacker      state.ObjID
	AttackerAlias string

	key string
}

// Canon is an item decision's canonical options.
type Canon struct {
	Kind     DecisionType
	Decision *decision.Decision
	Options  []CanonOption
	// Focus is the attack/block item's creature X.
	Focus      state.ObjID
	FocusAlias string

	byKey    map[string]int // lookup only
	payments []decision.PaymentAction
	options  []decision.Option
}

// priorityKey is the semantic identity of a priority option, "" when the
// option is no canonical action (a mana activation, concede).
func priorityKey(e *rules.Engine, kind string, obj state.ObjID, mode, label string) (key string, opt CanonOption) {
	name := objName(e, obj)
	switch kind {
	case "pass":
		return "pass", CanonOption{Label: "Pass", Kind: "pass"}
	case "cast":
		l := "Cast " + name
		if mode != "" {
			l += " [" + mode + "]"
		}
		return "cast|" + name + "|" + mode, CanonOption{Label: l, Act: true, Kind: "cast", Name: name, Mode: mode}
	case "ability", "granted":
		// The engine label is "<source name>: <ability description>", the
		// same for every copy of the source: one semantic action.
		return "ability|" + label, CanonOption{Label: "Activate " + label, Act: true, Kind: "ability", Name: name}
	case "play_land":
		return "land|" + name, CanonOption{Label: "Play " + name, Act: true, Kind: "land", Name: name}
	case "unlock", "turn_face_up", "specialize", "station":
		return kind + "|" + name + "|" + mode, CanonOption{Label: label, Act: true, Kind: "special", Name: name, Mode: mode}
	}
	return "", CanonOption{}
}

// BuildCanon builds the canonical options of the decision Reach stopped at.
//
//   - spell/hold: Pass, then every distinct legal non-mana priority action
//     sorted by label: one "Cast <name>" per castable card name and cast
//     mode, one "Activate <source>: <ability>" per distinct ability text,
//     one "Play <name>" per land name. An action is legal when the decision
//     offers it (payable from the floating pool), when an offered payment
//     action has a plan (Engine.EnsurePaymentActions), or when the offer
//     walk priced against the seat's potential mana admits it and the exact
//     payment planner does not prove it unpayable
//     (Engine.PotentialPaymentPlans, any verdict but "insufficient"). Mana
//     activations are no canonical action.
//   - attack: X is the first potential attacker, in the spec's battlefield
//     order, that labels.attacks names; options [No attack, Attack].
//   - block: X is the first potential blocker, in the spec's battlefield
//     order, that labels.blocks names; options [No block] + one
//     "Block <alias>" per attacker X may legally block, in the spec's
//     attackers order.
func BuildCanon(m *Materialized, r *Reached) (*Canon, error) {
	e, d := m.Engine, r.Decision
	c := &Canon{Kind: r.Kind, Decision: d, byKey: map[string]int{}, options: d.Options}
	switch r.Kind {
	case DecisionSpell, DecisionHold:
		if d.Kind != decision.KPriority {
			return nil, refuse("reached", "%s for a %s item", d.Kind, r.Kind)
		}
		var acts []CanonOption
		add := func(key string, o CanonOption) {
			if key == "" || key == "pass" {
				return
			}
			if _, ok := c.byKey[key]; ok {
				return
			}
			c.byKey[key] = -1
			o.key = key
			acts = append(acts, o)
		}
		for _, o := range d.Options {
			add(priorityKey(e, o.Kind, o.Obj, o.Mode, o.Label))
		}
		c.payments = e.EnsurePaymentActions()
		for _, pa := range c.payments {
			if len(pa.Plans) > 0 {
				add(priorityKey(e, "cast", pa.Cast.Object, "", ""))
			}
		}
		for _, pp := range e.PotentialPaymentPlans(d.Player) {
			// Only a PROVEN-unpayable play is left out: "unsupported",
			// "search_limit" and "ambiguous" verdicts are plays the offer
			// walk admits against the seat's potential mana (an overbound,
			// as XMage's available-mana playability check is) that the exact
			// planner could not witness.
			if pp.Reason != "insufficient" {
				add(priorityKey(e, pp.Action.Kind, pp.Action.Obj, pp.Action.Mode, pp.Action.Label))
			}
		}
		sort.SliceStable(acts, func(i, j int) bool { return acts[i].Label < acts[j].Label })
		c.Options = append([]CanonOption{{Label: "Pass", Kind: "pass", key: "pass"}}, acts...)
		for i, o := range c.Options {
			c.byKey[o.key] = i
		}
	case DecisionAttack:
		if d.Kind != decision.KAttackers {
			return nil, refuse("reached", "%s for an attack item", d.Kind)
		}
		labels, err := m.Spec.ParseLabels()
		if err != nil {
			return nil, refuse("labels", "%v", err)
		}
		can := map[state.ObjID]bool{} // lookup only
		for _, o := range d.Options {
			can[o.Obj] = true
		}
		seat := SeatName(d.Player)
	attack:
		for i := range m.Spec.Players[seat].Battlefield {
			for _, id := range m.Perms[seat][i] {
				if !can[id] {
					continue
				}
				for _, a := range m.aliasesOf(id) {
					if _, ok := labels.Attacks[a]; ok {
						c.Focus, c.FocusAlias = id, a
						break attack
					}
				}
			}
		}
		if c.Focus == 0 {
			return nil, refuse("focus", "no potential attacker has an attack label")
		}
		c.Options = []CanonOption{{Label: "No attack", Kind: "noattack"}, {Label: "Attack", Act: true, Kind: "attack", Name: objName(e, c.Focus)}}
	case DecisionBlock:
		if d.Kind != decision.KBlockers {
			return nil, refuse("reached", "%s for a block item", d.Kind)
		}
		labels, err := m.Spec.ParseLabels()
		if err != nil {
			return nil, refuse("labels", "%v", err)
		}
		can := map[state.ObjID]bool{} // lookup only
		for _, o := range d.Options {
			can[o.Obj] = true
		}
		named := map[string]bool{} // lookup only
		for _, b := range labels.Blocks {
			named[b.Blocker] = true
		}
		seat := SeatName(d.Player)
	block:
		for i := range m.Spec.Players[seat].Battlefield {
			for _, id := range m.Perms[seat][i] {
				if !can[id] {
					continue
				}
				for _, a := range m.aliasesOf(id) {
					if named[a] {
						c.Focus, c.FocusAlias = id, a
						break block
					}
				}
			}
		}
		if c.Focus == 0 {
			return nil, refuse("focus", "no potential blocker has a block label")
		}
		c.Options = []CanonOption{{Label: "No block", Kind: "noblock"}}
		legal := map[state.ObjID]bool{} // lookup only
		for _, o := range d.Options {
			if o.Obj == c.Focus {
				legal[o.Attacker] = true
			}
		}
		for _, at := range m.Spec.Attackers {
			id := m.Alias[at.Attacker]
			if legal[id] {
				legal[id] = false
				c.Options = append(c.Options, CanonOption{Label: "Block " + at.Attacker, Act: true, Kind: "block",
					Name: objName(e, id), Attacker: id, AttackerAlias: at.Attacker})
			}
		}
		for _, o := range d.Options { // an attacker the spec does not list (none today)
			if o.Obj == c.Focus && legal[o.Attacker] {
				legal[o.Attacker] = false
				c.Options = append(c.Options, CanonOption{Label: "Block " + m.AliasOf(o.Attacker), Act: true, Kind: "block",
					Name: objName(e, o.Attacker), Attacker: o.Attacker, AttackerAlias: m.AliasOf(o.Attacker)})
			}
		}
	default:
		return nil, fmt.Errorf("searchbench: unknown item kind %q", r.Kind)
	}
	return c, nil
}

// aliasesOf lists every spec alias of id, shortest first.
func (m *Materialized) aliasesOf(id state.ObjID) []string {
	var out []string
	for a, o := range m.Alias {
		if o == id {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) < len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// Labels lists the canonical labels in index order.
func (c *Canon) Labels() []string {
	out := make([]string, len(c.Options))
	for i, o := range c.Options {
		out[i] = o.Label
	}
	return out
}

// Project maps any engine intent at the canon's decision onto (canonical
// index, act). A priority option or a payment/announce action names its
// card or ability; a KAttackers answer says whether X attacks; a KBlockers
// answer says what X blocks. A mana activation or a concession has no
// canonical option (error), and so has an action the canon does not list.
func (c *Canon) Project(e *rules.Engine, in decision.Intent) (int, bool, error) {
	d := c.Decision
	switch c.Kind {
	case DecisionSpell, DecisionHold:
		var key string
		switch {
		case in.Payment != nil || in.Announce != nil:
			id := ""
			if in.Payment != nil {
				id = in.Payment.ActionID
			} else {
				id = in.Announce.ActionID
			}
			found := false
			for _, pa := range c.payments {
				if pa.ID == id {
					key, _ = priorityKey(e, "cast", pa.Cast.Object, "", "")
					found = true
					break
				}
			}
			if !found {
				return -1, false, fmt.Errorf("searchbench: payment action %q not offered", id)
			}
		case len(in.Choices) == 1 && in.Choices[0] >= 0 && in.Choices[0] < len(c.options):
			o := c.options[in.Choices[0]]
			key, _ = priorityKey(e, o.Kind, o.Obj, o.Mode, o.Label)
			if key == "" {
				return -1, false, fmt.Errorf("searchbench: %s option %q has no canonical action", o.Kind, o.Label)
			}
		default:
			return -1, false, fmt.Errorf("searchbench: priority intent %v", in.Choices)
		}
		i, ok := c.byKey[key]
		if !ok || i < 0 {
			return -1, false, fmt.Errorf("searchbench: action %q is not canonical here", key)
		}
		return i, c.Options[i].Act, nil
	case DecisionAttack:
		for _, ch := range in.Choices {
			if ch >= 0 && ch < len(d.Options) && d.Options[ch].Obj == c.Focus {
				return 1, true, nil
			}
		}
		return 0, false, nil
	case DecisionBlock:
		for _, ch := range in.Choices {
			if ch < 0 || ch >= len(d.Options) || d.Options[ch].Obj != c.Focus {
				continue
			}
			for i, o := range c.Options {
				if o.Kind == "block" && o.Attacker == d.Options[ch].Attacker {
					return i, true, nil
				}
			}
			return -1, false, fmt.Errorf("searchbench: X blocks an attacker the canon does not list")
		}
		return 0, false, nil
	}
	return -1, false, fmt.Errorf("searchbench: unknown item kind %q", c.Kind)
}

// ProjectAt is Project for a spell/hold item's intent at a later priority
// decision d of engine e (a clone of the root engine on which some mana was
// already activated): the action is named by its semantic key on e, so it
// matches the root's canonical option for the same card, mode or ability.
func (c *Canon) ProjectAt(e *rules.Engine, d *decision.Decision, in decision.Intent) (int, bool, error) {
	if c.Kind != DecisionSpell && c.Kind != DecisionHold {
		return -1, false, fmt.Errorf("searchbench: ProjectAt is for priority items, not %s", c.Kind)
	}
	var key string
	switch {
	case in.Payment != nil || in.Announce != nil:
		id := ""
		if in.Payment != nil {
			id = in.Payment.ActionID
		} else {
			id = in.Announce.ActionID
		}
		for _, pa := range e.EnsurePaymentActions() {
			if pa.ID == id {
				key, _ = priorityKey(e, "cast", pa.Cast.Object, "", "")
				break
			}
		}
	case len(in.Choices) == 1 && in.Choices[0] >= 0 && in.Choices[0] < len(d.Options):
		o := d.Options[in.Choices[0]]
		key, _ = priorityKey(e, o.Kind, o.Obj, o.Mode, o.Label)
	}
	if key == "" {
		return -1, false, fmt.Errorf("searchbench: intent %v has no canonical action", in.Choices)
	}
	i, ok := c.byKey[key]
	if !ok || i < 0 {
		return -1, false, fmt.Errorf("searchbench: action %q is not canonical here", key)
	}
	return i, c.Options[i].Act, nil
}

// IsManaActivation reports whether in is a single-option answer choosing a
// mana ability activation at d.
func IsManaActivation(d *decision.Decision, in decision.Intent) bool {
	return d != nil && d.Kind == decision.KPriority && in.Payment == nil && in.Announce == nil &&
		len(in.Choices) == 1 && in.Choices[0] >= 0 && in.Choices[0] < len(d.Options) && d.Options[in.Choices[0]].Kind == "activate"
}
