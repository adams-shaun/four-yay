package searchprobe

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Action is a comparable semantic identity. Labels identify scalar choices,
// including modes that highlight their source. Object selections instead use
// stable observer-local identities, without cosmetic target labels.
type Action struct {
	Decision                      decision.Kind
	Source                        uint32
	Kind                          string
	Obj, Attacker                 uint32
	Player                        state.PlayerID
	Ability, AltCostIndex, Amount int
	Mode, SVar, Value             string
}

type ObservedOption struct {
	Action   Action
	Group    int
	Required bool
}
type ObservedDecision struct {
	Player      state.PlayerID
	Kind        decision.Kind
	Min, Max    int
	Source      uint32
	Options     []ObservedOption
	EffectAPI   string
	DamageKnown bool
	Damage      int
}

func (c *Collector) Actions(d *decision.Decision, in decision.Intent) ([]Action, error) {
	choices, _, err := c.IntentActions(d, in)
	return choices, err
}

// IntentActions translates both ordered parts of an intent into stable
// observer-local actions. Most callers need only Choices and use Actions;
// hindsight branching also preserves KArrange's independently ordered Rest
// pile, which cannot be represented by Choices alone.
func (c *Collector) IntentActions(d *decision.Decision, in decision.Intent) (choices, rest []Action, err error) {
	if d == nil || d.Player != c.actor {
		return nil, nil, fmt.Errorf("may only capture the acting seat's own answer")
	}
	if err := d.Validate(in); err != nil {
		return nil, nil, err
	}
	translate := func(indices []int) ([]Action, error) {
		out := make([]Action, 0, len(indices))
		for _, i := range indices {
			a, err := c.action(d, d.Options[i])
			if err != nil {
				return nil, err
			}
			out = append(out, a)
		}
		return out, nil
	}
	choices, err = translate(in.Choices)
	if err != nil {
		return nil, nil, err
	}
	rest, err = translate(in.Rest)
	return choices, rest, err
}

// Match must use the target world's collector after Capture has validated its
// observed prefix. Never reuse the source world's raw-ID dictionary.
func (c *Collector) Match(d *decision.Decision, actions []Action) (decision.Intent, error) {
	return c.MatchIntent(d, actions, nil)
}

// MatchIntent is Match with KArrange's ordered complement. Keeping Rest as a
// second semantic list lets an undo branch replay both piles without changing
// Action (and therefore without changing existing sampler history digests).
func (c *Collector) MatchIntent(d *decision.Decision, choices, rest []Action) (decision.Intent, error) {
	if d == nil || d.Player != c.actor {
		return decision.Intent{}, fmt.Errorf("may only match the acting seat's own answer")
	}
	match := func(actions []Action) ([]int, error) {
		indices := make([]int, 0, len(actions))
		for _, want := range actions {
			found := -1
			for i, o := range d.Options {
				got, err := c.action(d, o)
				if err != nil {
					return nil, err
				}
				if got == want {
					if found >= 0 {
						return nil, fmt.Errorf("ambiguous semantic action %+v", want)
					}
					found = i
				}
			}
			if found < 0 {
				return nil, fmt.Errorf("missing semantic action %+v", want)
			}
			indices = append(indices, found)
		}
		return indices, nil
	}
	chosen, err := match(choices)
	if err != nil {
		return decision.Intent{}, err
	}
	remainder, err := match(rest)
	if err != nil {
		return decision.Intent{}, err
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: chosen, Rest: remainder}
	if err := d.Validate(in); err != nil {
		return decision.Intent{}, err
	}
	return in, nil
}

func (c *Collector) action(d *decision.Decision, o decision.Option) (Action, error) {
	a := Action{Decision: d.Kind, Source: c.ref(d.Source), Kind: o.Kind, Obj: c.ref(o.Obj), Attacker: c.ref(o.Attacker), Player: o.Player, Ability: o.Ability, AltCostIndex: o.AltCostIndex, Amount: o.Amount, Mode: o.Mode, SVar: o.SVar}
	if d.Source != 0 && a.Source == 0 || o.Obj != 0 && a.Obj == 0 || o.Attacker != 0 && a.Attacker == 0 {
		return Action{}, fmt.Errorf("action references an unobserved object")
	}
	// A mode's Obj highlights its source; it is not the selected value.
	// Pay/decline and Charm modes share that Obj and differ only in their
	// wire-visible label. Object-target labels remain cosmetic and omitted.
	if o.Obj == 0 || o.Kind == "mode" {
		a.Value = o.Label
	}
	return a, nil
}

func (c *Collector) observeDecision(d *decision.Decision) (*ObservedDecision, error) {
	if d == nil {
		return nil, nil
	}
	return c.observeDecisionInto(new(ObservedDecision), nil, d)
}

// observeDecisionInto builds d's observed form in out, appending its options
// to buf[:0] (a nil buf allocates exactly len(d.Options)). Options stays nil
// for a decision with no options, as an appended-from-nil slice would, so an
// owned and a scratch observation of one decision are reflect.DeepEqual.
// Groups are numbered 1, 2, ... in first-appearance order.
func (c *Collector) observeDecisionInto(out *ObservedDecision, buf []ObservedOption, d *decision.Decision) (*ObservedDecision, error) {
	if d.Player != c.actor {
		return nil, fmt.Errorf("opponent private decision entered observation")
	}
	if buf == nil && len(d.Options) > 0 {
		buf = make([]ObservedOption, 0, len(d.Options))
	}
	opts := buf[:0]
	*out = ObservedDecision{Player: d.Player, Kind: d.Kind, Min: d.Min, Max: d.Max, Source: c.ref(d.Source)}
	// groups[i] is group i+1's name. A decision has a handful of groups, so
	// a linear scan over a stack array beats a map allocated per call.
	var groupBuf [16]string
	groups := groupBuf[:0]
	for _, o := range d.Options {
		a, err := c.action(d, o)
		if err != nil {
			return nil, err
		}
		group := 0
		if o.Group != "" {
			for i, name := range groups {
				if name == o.Group {
					group = i + 1
					break
				}
			}
			if group == 0 {
				groups = append(groups, o.Group)
				group = len(groups)
			}
		}
		opts = append(opts, ObservedOption{Action: a, Group: group, Required: o.Required})
	}
	if len(opts) > 0 {
		out.Options = opts
	}
	if d.TargetEffect != nil {
		out.EffectAPI = d.TargetEffect.API
		if d.TargetEffect.Damage != nil && d.TargetEffect.Damage.Amount != nil {
			out.DamageKnown = true
			out.Damage = *d.TargetEffect.Damage.Amount
		}
	}
	return out, nil
}
