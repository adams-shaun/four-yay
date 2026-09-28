package kshadow

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/v1agent"
	"github.com/adams-shaun/gorge/state"
)

// Decision classes (Classify).
const (
	ClassPriority = "priority"
	ClassAttack   = "attack"
	ClassBlock    = "block"
	ClassTarget   = "target"
	ClassOther    = "other"
)

// Classify names the kernel decision's class from its candidate kinds.
func Classify(d *v1agent.Decision) string {
	has := map[string]bool{}
	for i := range d.Candidates {
		has[d.Candidates[i].Kind()] = true
	}
	switch {
	case has["choose_attacker_inclusion"]:
		return ClassAttack
	case has["choose_blocker_inclusion"]:
		return ClassBlock
	case has["pass"]:
		return ClassPriority
	case has["choose_target"]:
		return ClassTarget
	}
	return ClassOther
}

// isManaKind reports a kernel candidate the gorge policies never choose
// (mana is auto-paid on both sides).
func isManaKind(k string) bool { return k == "activate_mana_ability" }

// Mapping is the kernel candidates' gorge counterparts at a priority
// decision: Gorge[i] is the gorge option index kernel candidate i maps to,
// -1 when it has none (mana abilities are not mapped: Mana[i] is set).
type Mapping struct {
	Class  string
	Staged bool
	Kind   decision.Kind
	Gorge  []int
	Mana   []bool
	Unused int // gorge non-mana options with no kernel counterpart
	// UnusedKinds and UnmappedKinds name what failed to pair (diagnostics).
	UnusedKinds   []string
	UnmappedKinds []string
	Options       int // gorge non-mana options
}

func (m *Mapping) String() string {
	return fmt.Sprintf("{%s staged=%v kind=%s gorge=%v unused=%d/%d}", m.Class, m.Staged, m.Kind, m.Gorge, m.Unused, m.Options)
}

// isManaOption reports a gorge priority option that activates a mana
// ability (autopay hides them from the policies).
func isManaOption(o *decision.Option) bool { return o.Kind == "activate" || o.ManaSymbol != "" }

// isConcede reports gorge's concede option (never mapped, never chosen).
func isConcede(o *decision.Option) bool { return o.Kind == "concede" }

// arenaOf is the kernel arena id of a gorge object (ok false when the
// object was not staged from a kernel object).
func (sh *Shadow) arenaOf(id state.ObjID) (uint32, bool) {
	a, ok := sh.ObjToArena[id]
	return a, ok
}

// kernelRef returns candidate i's kernel action (nil without x_kernel_v5).
func kernelRef(d *v1agent.Decision, i int) *v1agent.KAction {
	if d.Kernel == nil || i >= len(d.Kernel.Actions) {
		return nil
	}
	return &d.Kernel.Actions[i]
}

// MapDecision maps a priority decision's kernel candidates onto the staged
// engine's pending options (coverage measurement; the policy path maps the
// other way, KernelIndex).
func MapDecision(sh *Shadow, d *v1agent.Decision) *Mapping {
	m := &Mapping{Class: Classify(d), Staged: sh.Fatal == ""}
	if !m.Staged {
		return m
	}
	pd := sh.E.Pending()
	if pd.Kind == decision.KPriority {
		sh.E.EnsurePaymentActions()
		pd = sh.E.Pending()
	}
	m.Kind = pd.Kind
	m.Gorge = make([]int, len(d.Candidates))
	m.Mana = make([]bool, len(d.Candidates))
	used := map[int]bool{}
	var potential []decision.PotentialAction
	if pd.Kind == decision.KPriority {
		potential = sh.E.PotentialActions(sh.Me)
	}
	for i := range d.Candidates {
		m.Gorge[i] = -1
		k := d.Candidates[i].Kind()
		if isManaKind(k) {
			m.Mana[i] = true
			continue
		}
		ka := kernelRef(d, i)
		if pd.Kind == decision.KAttackers && ka != nil && ka.Attacker != nil {
			for j := range pd.Options {
				if a, ok := sh.arenaOf(pd.Options[j].Obj); ok && a == ka.Attacker.ArenaID {
					m.Gorge[i] = pd.Options[j].Index
					break
				}
			}
			continue
		}
		if pd.Kind == decision.KBlockers && ka != nil && ka.Attacker != nil && ka.Blocker != nil {
			for j := range pd.Options {
				o := &pd.Options[j]
				b, ok1 := sh.arenaOf(o.Obj)
				a, ok2 := sh.arenaOf(o.Attacker)
				if ok1 && ok2 && b == ka.Blocker.ArenaID && a == ka.Attacker.ArenaID {
					m.Gorge[i] = o.Index
					break
				}
			}
			continue
		}
		if pd.Kind != decision.KPriority {
			continue
		}
		for j := range pd.Options {
			o := &pd.Options[j]
			if isManaOption(o) || isConcede(o) || used[o.Index] {
				continue
			}
			if priorityMatch(sh, k, ka, o) {
				m.Gorge[i] = o.Index
				used[o.Index] = true
				break
			}
		}
		if m.Gorge[i] < 0 && (k == "cast_spell" || k == "activate_ability") && ka != nil && ka.Source != nil {
			for j := range pd.PaymentActions {
				a := &pd.PaymentActions[j]
				if oa, ok := sh.arenaOf(a.Cast.Object); ok && oa == ka.Source.ArenaID && paymentKindMatches(pd, a, k) {
					m.Gorge[i] = PaymentBase + j
					if a.BaseOptionIndex != nil {
						used[*a.BaseOptionIndex] = true
					}
					break
				}
			}
		}
		if m.Gorge[i] < 0 && (k == "cast_spell" || k == "activate_ability") && ka != nil && ka.Source != nil {
			for j, pa := range potential {
				if oa, ok := sh.arenaOf(pa.Obj); ok && oa == ka.Source.ArenaID &&
					(k == "cast_spell") == (pa.Kind == "cast") {
					m.Gorge[i] = PotentialBase + j
					break
				}
			}
		}
	}
	if pd.Kind == decision.KPriority {
		for j := range pd.Options {
			o := &pd.Options[j]
			if isManaOption(o) || isConcede(o) {
				continue
			}
			m.Options++
			if !used[o.Index] {
				m.Unused++
				m.UnusedKinds = append(m.UnusedKinds, o.Kind+" "+o.Label)
			}
		}
	}
	for i, g := range m.Gorge {
		if g < 0 && !m.Mana[i] {
			k := d.Candidates[i].Kind()
			if src := d.Candidates[i].Semantic.Source(); src != nil {
				k += " " + src.Name()
			}
			m.UnmappedKinds = append(m.UnmappedKinds, k)
		}
	}
	return m
}

// PaymentBase offsets a payment-action index in Mapping.Gorge, and
// PotentialBase a potential-play index (a play gorge offers only once mana
// is floating: the policy reaches it through mana activations, which the
// kernel's auto-pay makes one step).
const (
	PaymentBase   = 1000
	PotentialBase = 2000
)

// paymentKindMatches: a payment action pays a cast (kernel cast_spell) or
// an activation (activate_ability), told apart by its base option's kind
// or, without one, by the object's zone (a spell is cast from a hidden or
// graveyard zone, an ability comes from a permanent).
func paymentKindMatches(pd *decision.Decision, a *decision.PaymentAction, k string) bool {
	if a.BaseOptionIndex != nil {
		if o := optionByIndex(pd, *a.BaseOptionIndex); o != nil {
			if k == "cast_spell" {
				return o.Kind == "cast"
			}
			return o.Kind == "ability" || o.Kind == "granted"
		}
	}
	isAbility := strings.HasPrefix(a.Label, "Activate") || strings.Contains(a.Label, ": ")
	if k == "cast_spell" {
		return !isAbility
	}
	return isAbility
}

// priorityMatch reports whether kernel candidate kind k (action ka) is
// gorge option o.
func priorityMatch(sh *Shadow, k string, ka *v1agent.KAction, o *decision.Option) bool {
	src := func() (uint32, bool) {
		if ka == nil || ka.Source == nil {
			return 0, false
		}
		return ka.Source.ArenaID, true
	}
	switch k {
	case "pass":
		return o.Kind == "pass"
	case "play_land":
		a, ok := src()
		oa, ok2 := sh.arenaOf(o.Obj)
		return ok && ok2 && o.Kind == "play_land" && a == oa
	case "cast_spell":
		a, ok := src()
		oa, ok2 := sh.arenaOf(o.Obj)
		return ok && ok2 && o.Kind == "cast" && a == oa
	case "activate_ability":
		a, ok := src()
		oa, ok2 := sh.arenaOf(o.Obj)
		return ok && ok2 && (o.Kind == "ability" || o.Kind == "granted") && a == oa
	}
	return false
}

// Coverage accumulates MapDecision results per class.
type Coverage struct {
	byClass map[string]*classCov
}

type classCov struct {
	decisions, staged, full int
	cands, mapped, mana     int
	opts, unused            int
	kinds                   map[string]int // kernel candidate kinds left unmapped
}

// NewCoverage returns an empty accumulator.
func NewCoverage() *Coverage { return &Coverage{byClass: map[string]*classCov{}} }

// Add folds one decision.
func (c *Coverage) Add(class string, m *Mapping) {
	cc := c.byClass[class]
	if cc == nil {
		cc = &classCov{kinds: map[string]int{}}
		c.byClass[class] = cc
	}
	cc.decisions++
	if !m.Staged {
		return
	}
	cc.staged++
	full := true
	for i, g := range m.Gorge {
		if m.Mana[i] {
			cc.mana++
			continue
		}
		cc.cands++
		if g >= 0 {
			cc.mapped++
		} else {
			full = false
		}
	}
	if full {
		cc.full++
	}
	cc.opts += m.Options
	cc.unused += m.Unused
}

// Print writes the per-class table.
func (c *Coverage) Print(w io.Writer) {
	var ks []string
	for k := range c.byClass {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	fmt.Fprintf(w, "  %-9s %8s %8s %8s %10s %10s %12s\n", "class", "decs", "staged", "full-map", "cands", "mapped", "gorge-unused")
	for _, k := range ks {
		cc := c.byClass[k]
		fmt.Fprintf(w, "  %-9s %8d %7.1f%% %7.1f%% %10d %9.1f%% %6d/%-6d\n", k, cc.decisions,
			pct(cc.staged, cc.decisions), pct(cc.full, cc.staged), cc.cands, pct(cc.mapped, cc.cands), cc.unused, cc.opts)
	}
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return 100 * float64(a) / float64(b)
}
