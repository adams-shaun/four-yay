package kshadow

import (
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"strings"

	"github.com/adams-shaun/gorge/internal/spellbench/v1agent"
	"github.com/adams-shaun/gorge/state"
)

// Fidelity is one decision's field-by-field comparison of the staged
// shadow, re-read through gorge's own derivation, against the kernel
// observation it was built from. Checked counts every field compared and
// Mismatch every field that differed, both keyed by field name.
type Fidelity struct {
	Checked  map[string]int
	Mismatch map[string]int
	// Examples keeps the first mismatch detail per field.
	Examples map[string]string
}

func newFidelity() *Fidelity {
	return &Fidelity{Checked: map[string]int{}, Mismatch: map[string]int{}, Examples: map[string]string{}}
}

func (f *Fidelity) cmp(field string, equal bool, detail func() string) {
	f.Checked[field]++
	if !equal {
		f.Mismatch[field]++
		if _, ok := f.Examples[field]; !ok && detail != nil {
			f.Examples[field] = detail()
		}
	}
}

// Add accumulates o into f.
func (f *Fidelity) Add(o *Fidelity) {
	for k, v := range o.Checked {
		f.Checked[k] += v
	}
	for k, v := range o.Mismatch {
		f.Mismatch[k] += v
	}
	for k, v := range o.Examples {
		if _, ok := f.Examples[k]; !ok {
			f.Examples[k] = v
		}
	}
}

// NewFidelity returns an empty accumulator.
func NewFidelity() *Fidelity { return newFidelity() }

// Fields lists the compared fields in sorted order.
func (f *Fidelity) Fields() []string {
	var out []string
	for k := range f.Checked {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// Compare re-reads the shadow and diffs it against obs (the observation
// the shadow was staged from). The shadow's engine must not have been
// stepped past its staged decision.
func Compare(sh *Shadow, obs *v1agent.KObservation) *Fidelity {
	f := newFidelity()
	p := &obs.Projection
	e := sh.E
	g := e.G
	f.cmp("staged", sh.Fatal == "", func() string { return sh.Fatal })
	if sh.Fatal != "" {
		return f
	}
	f.cmp("turn", int(g.Turn) == p.Turn, nil)
	f.cmp("step", kernelStep[p.Phase] == g.Step || sh.StagedStep == g.Step, func() string { return p.Phase })
	f.cmp("active", g.Active == seatID(p.ActivePlayer), nil)
	if d := e.Pending(); d != nil && d.Kind == decision.KPriority {
		f.cmp("priority", d.Player == seatID(p.PriorityPlayer) || p.PriorityPlayer == "", func() string { return string(d.Kind) })
	}
	for pl := 0; pl < 2; pl++ {
		P := state.PlayerID(pl)
		f.cmp("life", int(g.Players[pl].Life) == p.Life[pl], func() string { return itoa(int(g.Players[pl].Life)) + " vs " + itoa(p.Life[pl]) })
		f.cmp("hand_count", len(g.Zone(state.ZHand, P)) == p.HandCounts[pl], func() string {
			return itoa(len(g.Zone(state.ZHand, P))) + " vs " + itoa(p.HandCounts[pl])
		})
		f.cmp("library_count", len(g.Zone(state.ZLibrary, P)) == p.LibraryCounts[pl], func() string {
			return itoa(len(g.Zone(state.ZLibrary, P))) + " vs " + itoa(p.LibraryCounts[pl])
		})
		gy := 0
		for _, c := range p.Graveyards[pl] {
			if !c.IsToken {
				gy++
			}
		}
		f.cmp("graveyard_count", len(g.Zone(state.ZGraveyard, P)) == gy, nil)
		f.cmp("lands_played", int(g.Players[pl].LandsPlayed) == p.Status[pl].LandsPlayed, nil)
		pool := g.Players[pl].Pool
		same := true
		for i := 0; i < 6 && i < len(pool); i++ {
			if int(pool[i]) != p.ManaPools[pl][i] {
				same = false
			}
		}
		f.cmp("mana_pool", same, nil)
		// Battlefield: object sets by (name) multiset, then per object.
		var kn, gn []string
		for i := range p.Battlefield[pl] {
			kn = append(kn, fold(p.Battlefield[pl][i].Name))
		}
		for _, id := range g.Zone(state.ZBattlefield, P) {
			gn = append(gn, fold(e.Name(id)))
		}
		f.cmp("battlefield_names", sameMultiset(kn, gn), func() string { return strings.Join(kn, ",") + " | " + strings.Join(gn, ",") })
		for i := range p.Battlefield[pl] {
			c := &p.Battlefield[pl][i]
			id, ok := sh.ArenaToObj[c.Stable.ArenaID]
			f.cmp("bf_object_staged", ok && g.Obj(id) != nil && g.Obj(id).Zone == state.ZBattlefield, func() string { return c.Name })
			if !ok {
				continue
			}
			o := g.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield {
				continue
			}
			f.cmp("bf_controller", o.Controller == seatID(c.Stable.Controller), nil)
			f.cmp("bf_tapped", o.Tapped == c.Tapped, func() string { return c.Name })
			f.cmp("bf_damage", int(o.Damage) == c.Damage, func() string { return c.Name })
			f.cmp("bf_counters", int(o.Counter("P1P1")) == c.Counters.P1P1 && int(o.Counter("M1M1")) == c.Counters.M1M1, func() string { return c.Name })
			isCre := e.IsCreature(id)
			f.cmp("bf_is_creature", isCre == c.IsCreature(), func() string { return c.Name })
			f.cmp("bf_is_land", e.IsLand(id) == c.IsLand(), func() string { return c.Name })
			if isCre && c.IsCreature() {
				f.cmp("bf_power", int(e.Power(id)) == c.Power(), func() string {
					return c.Name + " " + itoa(int(e.Power(id))) + " vs " + itoa(c.Power())
				})
				f.cmp("bf_toughness", int(e.Toughness(id)) == c.Toughness(), func() string {
					return c.Name + " " + itoa(int(e.Toughness(id))) + " vs " + itoa(c.Toughness())
				})
				for _, kw := range keywordTable {
					kk := kw.get(&c.Characteristics.Keywords)
					gk := e.HasKeyword(id, kw.gorge)
					f.cmp("bf_kw_"+strings.ReplaceAll(strings.ToLower(kw.gorge), " ", "_"), kk == gk, func() string { return c.Name })
				}
				if pl == int(g.Active) {
					sick := o.SummonSick && !e.HasKeyword(id, "Haste")
					ksick := c.SummoningSick && !c.Characteristics.Keywords.Haste
					f.cmp("bf_summoning_sick", sick == ksick, func() string { return c.Name })
				}
			}
			var att []uint32
			for j := range g.Objs {
				if x := &g.Objs[j]; x.Zone == state.ZBattlefield && x.AttachedTo == id {
					att = append(att, sh.ObjToArena[x.ID])
				}
			}
			f.cmp("bf_attachments", sameU32(att, c.Attachments), func() string { return c.Name })
			f.cmp("bf_token", o.IsToken == c.IsToken, func() string { return c.Name })
		}
	}
	// Exile (both owners).
	var ke, ge []string
	for i := range p.Exile {
		if !p.Exile[i].IsToken {
			ke = append(ke, fold(p.Exile[i].Name))
		}
	}
	for pl := 0; pl < 2; pl++ {
		for _, id := range g.Zone(state.ZExile, state.PlayerID(pl)) {
			if _, ok := sh.ObjToArena[id]; ok {
				ge = append(ge, fold(e.Name(id)))
			}
		}
	}
	f.cmp("exile_names", sameMultiset(ke, ge), nil)
	// Own hand.
	var kh, gh []string
	for _, h := range obs.OwnHand {
		kh = append(kh, fold(h.Name))
	}
	for _, id := range g.Zone(state.ZHand, sh.Me) {
		gh = append(gh, fold(e.Name(id)))
	}
	f.cmp("own_hand", sameMultiset(kh, gh), func() string { return strings.Join(kh, ",") + " | " + strings.Join(gh, ",") })
	// Stack.
	f.cmp("stack_count", len(g.Stack) == len(p.Stack), func() string { return itoa(len(g.Stack)) + " vs " + itoa(len(p.Stack)) })
	if len(g.Stack) == len(p.Stack) {
		items := append([]v1agent.KStackItem(nil), p.Stack...)
		sort.SliceStable(items, func(i, j int) bool { return items[i].Index < items[j].Index })
		for i, it := range items {
			o := g.Obj(g.Stack[i])
			if o == nil {
				f.cmp("stack_item", false, nil)
				continue
			}
			f.cmp("stack_controller", o.Controller == seatID(it.Controller), nil)
			f.cmp("stack_targets", len(o.Targets) == len(it.Targets), func() string { return e.Name(o.ID) })
		}
	}
	// Combat.
	if p.Combat.AttackersDeclared && g.Step >= state.StepDeclareAttackers && g.Step <= state.StepCombatDamage {
		n := 0
		for j := range g.Objs {
			if x := &g.Objs[j]; x.Zone == state.ZBattlefield && x.IsAttacking {
				n++
			}
		}
		f.cmp("attackers", n == len(p.Combat.Attackers), nil)
	}
	return f
}

func sameMultiset(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameU32(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	a = append([]uint32(nil), a...)
	b = append([]uint32(nil), b...)
	sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	sort.Slice(b, func(i, j int) bool { return b[i] < b[j] })
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SeatIndex is 0 for p0, 1 for p1.
func SeatIndex(seat string) int { return int(seatID(seat)) }

// CheckHiddenPool checks the shadow's hidden-card belief against the
// opponent's true hand (read from the opponent's own observation by the
// offline checker only): every true hand card must be in the pool the
// shadow deals the opponent's hand from (hand + library), and the pool's
// size must equal the kernel's hand + library counts.
func CheckHiddenPool(sh *Shadow, oppHand []string) *Fidelity {
	f := newFidelity()
	g := sh.E.G
	opp := 1 - sh.Me
	pool := map[string]int{}
	n := 0
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range g.Zone(z, opp) {
			pool[fold(sh.E.Name(id))]++
			n++
		}
	}
	ok := true
	missing := ""
	for _, h := range oppHand {
		k := fold(h)
		if pool[k] == 0 {
			ok = false
			missing = h
			continue
		}
		pool[k]--
	}
	f.cmp("hidden_opp_hand_in_pool", ok, func() string { return missing })
	return f
}
