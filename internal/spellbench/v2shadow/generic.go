package v2shadow

import (
	"encoding/json"
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/v1agent"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/state"
)

// Route (a): the hint-free v1 policy (sb-generic, v1agent.NewGeneric, spec
// D§3.5 "Hint-free build") ported to v2 observations. Its judgement reads a
// kernel-shaped board (ObservationV5, which the kernel bridge attaches as
// x_kernel_v5) and kernel-shaped candidates; both are synthesized here from
// the v2 observation and the shadow's own options, so the policy chooses
// among exactly the plays gorge offers (every castable spell a payment plan
// exists for: the kernel auto-pays, so the policy never taps mana itself).
// Its choice is then played through the same pipeline as sb-tactical's: a
// planner-paid cast is lowered onto the wire's taps, follow-ups (targets,
// modes, X) are answered from the shadow plan, and combat declarations come
// from the policy's own attack and block planners.

// genericState is one game's route-(a) state.
type genericState struct {
	t     *v1agent.Tactical
	arena map[string]uint32 // v2 object id -> kernel arena id (lookup only)
}

func newGenericState(g *v2agent.GameStart) *genericState {
	t := v1agent.NewGeneric(v1agent.TacticalOptions{})
	cat := []string{"", ""}
	me := seatIndex(g.Seat)
	if g.OwnDeck != nil {
		cat[me] = g.OwnDeck.Name
	}
	if g.OpponentDeck != nil {
		cat[1-me] = g.OpponentDeck.Name
	}
	t.GameStart(&v1agent.GameStart{GameID: g.GameID, Seat: g.Seat, CatalogIDs: cat})
	return &genericState{t: t, arena: map[string]uint32{}}
}

func (g *genericState) aid(v2id string) uint32 {
	if a, ok := g.arena[v2id]; ok {
		return a
	}
	a := uint32(len(g.arena) + 1)
	g.arena[v2id] = a
	return a
}

var kernelZones = map[string]string{"hand": "Hand", "battlefield": "Battlefield", "graveyard": "Graveyard",
	"exile": "Exile", "stack": "Stack", "library": "Library", "command": "Command"}

func (g *genericState) kref(r *v2agent.ObjectRef) v1agent.KRef {
	k := v1agent.KRef{ArenaID: g.aid(r.ObjectID), Owner: r.OwnerSeat, Controller: r.ControllerSeat, Zone: kernelZones[r.Zone]}
	if r.CardName != nil {
		k.CardDBID, _ = v1agent.KernelCardID(*r.CardName)
	}
	return k
}

var colourBits = map[string]uint8{"white": 1, "blue": 2, "black": 4, "red": 8, "green": 16}

func (g *genericState) kcard(r *v2agent.ObjectRecord, attachments map[string][]uint32) v1agent.KCard {
	c := v1agent.KCard{Stable: g.kref(&r.ObjectRef), IsToken: r.Token}
	if r.CardName != nil {
		c.Name = *r.CardName
	}
	if ch := r.Characteristics; ch != nil {
		ty := &c.Characteristics.Types
		ty.Land, ty.Creature, ty.Instant = ch.HasType("land"), ch.HasType("creature"), ch.HasType("instant")
		ty.Sorcery, ty.Artifact, ty.Enchantment = ch.HasType("sorcery"), ch.HasType("artifact"), ch.HasType("enchantment")
		if ch.Power != nil {
			p := int(*ch.Power)
			c.Characteristics.Power = &p
		}
		if ch.Toughness != nil {
			t := int(*ch.Toughness)
			c.Characteristics.Toughness = &t
		}
		for _, col := range ch.Colors {
			c.Characteristics.ColorMask |= colourBits[col]
		}
		k := &c.Characteristics.Keywords
		for _, kw := range ch.Keywords {
			switch kw {
			case "flying":
				k.Flying = true
			case "reach":
				k.Reach = true
			case "haste":
				k.Haste = true
			case "vigilance":
				k.Vigilance = true
			case "trample":
				k.Trample = true
			case "first_strike":
				k.FirstStrike = true
			case "double_strike":
				k.DoubleStrike = true
			case "deathtouch":
				k.Deathtouch = true
			case "menace":
				k.Menace = true
				k.MinimumBlockers = 2
			case "defender":
				k.Defender = true
			case "lifelink":
				k.Lifelink = true
			case "hexproof":
				k.Hexproof = true
			case "indestructible":
				k.Indestructible = true
			}
		}
	}
	if pm := r.Permanent; pm != nil {
		c.Tapped, c.SummoningSick, c.Damage = pm.Tapped, pm.SummoningSick, int(pm.Damage)
		c.Counters = v1agent.KCounters{P1P1: int(pm.Counters["p1p1"]), M1M1: int(pm.Counters["m1m1"]),
			M0M1: int(pm.Counters["m0m1"]), Stun: int(pm.Counters["stun"]), Lore: int(pm.Counters["lore"])}
		c.Attachments = attachments[r.ObjectID]
	}
	return c
}

var kernelPhases = map[string]string{
	"untap": "untap", "upkeep": "upkeep", "draw": "draw", "precombat_main": "main1",
	"beginning_of_combat": "begin_combat", "declare_attackers": "declare_attackers",
	"declare_blockers": "declare_blockers", "combat_damage": "combat_damage",
	"end_of_combat": "end_combat", "postcombat_main": "main2", "end_step": "end", "cleanup": "cleanup",
}

// kernelObs synthesizes the acting seat's ObservationV5 from its v2
// observation.
func (g *genericState) kernelObs(obs *v2agent.Observation, priority bool) v1agent.KObservation {
	ko := v1agent.KObservation{ActingPlayer: obs.Viewer}
	p := &ko.Projection
	p.Turn, p.Phase = int(obs.Turn), kernelPhases[obs.PhaseStep]
	if obs.ActiveSeat != nil {
		p.ActivePlayer = *obs.ActiveSeat
	}
	p.PriorityPlayer = obs.Viewer
	if obs.PrioritySeat != nil {
		p.PriorityPlayer = *obs.PrioritySeat
	}
	attachments := map[string][]uint32{} // lookup only
	for i := range obs.Players {
		for _, r := range obs.Players[i].Battlefield {
			if r.Permanent != nil && r.Permanent.AttachedTo != nil && r.Permanent.AttachedTo.Object != nil {
				t := r.Permanent.AttachedTo.Object.ObjectID
				attachments[t] = append(attachments[t], g.aid(r.ObjectID))
			}
		}
	}
	var attackers []v1agent.KRef
	blocks := map[string][]v1agent.KRef{} // attacker v2 id -> blockers (lookup only)
	var attackOrder []string
	declared := false
	for i := range obs.Players {
		pl := &obs.Players[i]
		s := seatIndex(pl.Seat)
		p.Life[s] = int(pl.Life)
		mp := pl.ManaPool
		p.ManaPools[s] = [6]int{int(mp.W), int(mp.U), int(mp.B), int(mp.R), int(mp.G), int(mp.C)}
		p.HandCounts[s], p.LibraryCounts[s] = int(pl.HandCount), int(pl.LibraryCount)
		p.Status[s].LandsPlayed = int(pl.LandsPlayedThisTurn)
		for j := range pl.Battlefield {
			r := &pl.Battlefield[j]
			p.Battlefield[s] = append(p.Battlefield[s], g.kcard(r, attachments))
			if r.Permanent == nil {
				continue
			}
			if r.Permanent.Attacking {
				declared = true
				attackers = append(attackers, g.kref(&r.ObjectRef))
				attackOrder = append(attackOrder, r.ObjectID)
			}
			if r.Permanent.Blocking {
				for _, a := range r.Permanent.BlockedAttackers {
					blocks[a.ObjectID] = append(blocks[a.ObjectID], g.kref(&r.ObjectRef))
				}
			}
		}
		for j := range pl.Graveyard {
			p.Graveyards[s] = append(p.Graveyards[s], g.kcard(&pl.Graveyard[j], nil))
		}
		for j := range pl.Exile {
			p.Exile = append(p.Exile, g.kcard(&pl.Exile[j], nil))
		}
		if pl.Seat == obs.Viewer {
			for j := range pl.Hand {
				h := &pl.Hand[j]
				name := ""
				if h.CardName != nil {
					name = *h.CardName
				}
				ko.OwnHand = append(ko.OwnHand, v1agent.KHandCard{Stable: g.kref(&h.ObjectRef), Name: name})
			}
		}
	}
	for i := range obs.Stack {
		se := &obs.Stack[i]
		it := v1agent.KStackItem{Index: i, Controller: se.ControllerSeat, Kind: se.StackKind, IsCopy: se.Copy}
		src := se.ObjectRef
		if se.StackKind != "spell" && se.Source != nil {
			src = *se.Source
		}
		it.Source = g.kref(&src)
		if se.XValue != nil {
			it.XValue = int(*se.XValue)
		}
		for _, t := range se.Targets {
			switch {
			case t == nil:
			case t.Player != nil:
				it.Targets = append(it.Targets, v1agent.KTarget{Kind: "player", Player: *t.Player})
			case t.Object != nil:
				r := g.kref(t.Object)
				it.Targets = append(it.Targets, v1agent.KTarget{Kind: "object", Object: &r})
			}
		}
		p.Stack = append(p.Stack, it)
	}
	st := p.Phase
	p.Combat.AttackersDeclared = declared || (priority && (st == "declare_attackers" || st == "declare_blockers"))
	p.Combat.Attackers = attackers
	p.Combat.BlockersDeclared = len(blocks) > 0 || (priority && st == "declare_blockers") || st == "combat_damage"
	for _, a := range attackOrder {
		ref := obs.Object(a).ObjectRef
		ar := g.kref(&ref)
		bl := blocks[a]
		if bl == nil {
			bl = []v1agent.KRef{}
		}
		raw, _ := json.Marshal([]any{ar, bl})
		p.Combat.BlockersRaw = append(p.Combat.BlockersRaw, raw)
	}
	return ko
}

// v1Decision is the kernel-shaped decision over cands (with their kernel
// actions) at d's observation.
func (g *genericState) v1Decision(d *v2agent.Decision, cands []v1agent.Candidate, acts []v1agent.KAction, priority bool) *v1agent.Decision {
	obs := d.Observation()
	sum := v1agent.StateSummary{Turn: int(obs.Turn), PhaseStep: obs.PhaseStep, ActiveSeat: deref(obs.ActiveSeat),
		PrioritySeat: deref(obs.PrioritySeat), StackCount: len(obs.Stack)}
	for i := range obs.Players {
		pl := &obs.Players[i]
		sum.Seats = append(sum.Seats, v1agent.SeatSummary{Seat: pl.Seat, Life: int(pl.Life), HandCount: int(pl.HandCount),
			LibraryCount: int(pl.LibraryCount), GraveyardCount: len(pl.Graveyard), BattlefieldCount: len(pl.Battlefield)})
	}
	step := int64(0)
	var grp v1agent.Group
	if d.Seat != nil {
		step = d.Seat.SeatStep
		grp = v1agent.Group{GroupID: d.Seat.Group.GroupID, SubstepIndex: int(d.Seat.Group.SubstepIndex), SubstepCount: int(d.Seat.Group.SubstepCount)}
	}
	ko := g.kernelObs(obs, priority)
	return &v1agent.Decision{GameID: d.GameID, Step: step, ActingSeat: obs.Viewer, Group: grp, Summary: sum,
		Candidates: cands, Kernel: &v1agent.KernelView{Obs: ko, Actions: acts}}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// v1Cand builds one kernel-shaped candidate.
func v1Cand(kind string, fields map[string]any) v1agent.Candidate {
	fields["kind"] = kind
	raw, _ := json.Marshal(fields)
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &m)
	return v1agent.Candidate{Semantic: v1agent.Semantic{Kind: kind, Fields: m, Raw: raw}}
}

// shadowRef is the v2-style reference of a shadow object (its v2 id and
// name from the observation when staged).
func shadowRef(sh *Shadow, obs *v2agent.Observation, id state.ObjID) (*v2agent.ObjectRef, bool) {
	v2, ok := sh.ObjToV2[id]
	if !ok {
		return nil, false
	}
	if r := obs.Object(v2); r != nil {
		ref := r.ObjectRef
		return &ref, true
	}
	return nil, false
}

// genericPriority is route (a)'s answer at the shadow's pending priority
// decision rd: the kernel-shaped candidates are pass, the land plays, every
// castable spell (a pool-paid cast or a planner-paid one) and every
// offered non-mana ability; the policy's pick becomes the gorge intent.
func (p *Policy) genericPriority(sh *Shadow, d *v2agent.Decision, rd *decision.Decision) (decision.Intent, error) {
	g := p.gen
	obs := d.Observation()
	var cands []v1agent.Candidate
	var acts []v1agent.KAction
	var intents []decision.Intent
	add := func(kind string, ref *v2agent.ObjectRef, extra map[string]any, in decision.Intent) {
		f := map[string]any{}
		for k, v := range extra {
			f[k] = v
		}
		var ka v1agent.KAction
		ka.Kind = kind
		if ref != nil {
			f["source"] = ref
			r := g.kref(ref)
			ka.Source = &r
		}
		cands = append(cands, v1Cand(kind, f))
		acts = append(acts, ka)
		in.Seq, in.Player = rd.Seq, rd.Player
		intents = append(intents, in)
	}
	offeredCast := map[state.ObjID]bool{} // lookup only
	for i := range rd.Options {
		o := &rd.Options[i]
		switch {
		case o.Kind == "pass":
			add("pass", nil, nil, decision.Intent{Choices: []int{o.Index}})
		case o.Kind == "play_land":
			if ref, ok := shadowRef(sh, obs, o.Obj); ok {
				add("play_land", ref, nil, decision.Intent{Choices: []int{o.Index}})
			}
		case o.Kind == "cast" && o.Mode != "plot":
			if ref, ok := shadowRef(sh, obs, o.Obj); ok && !offeredCast[o.Obj] {
				offeredCast[o.Obj] = true
				add("cast_spell", ref, nil, decision.Intent{Choices: []int{o.Index}})
			}
		case o.Kind == "ability" || o.Kind == "granted":
			if ref, ok := shadowRef(sh, obs, o.Obj); ok {
				add("activate_ability", ref, map[string]any{"ability_index": abilityIndex(sh, o, rd)}, decision.Intent{Choices: []int{o.Index}})
			}
		}
	}
	for i := range rd.PaymentActions {
		a := &rd.PaymentActions[i]
		if len(a.Plans) == 0 || a.BaseOptionIndex != nil || offeredCast[a.Cast.Object] {
			continue
		}
		ref, ok := shadowRef(sh, obs, a.Cast.Object)
		if !ok {
			continue
		}
		offeredCast[a.Cast.Object] = true
		add("cast_spell", ref, nil, decision.Intent{Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: decision.ClonePaymentPlan(a.Plans[0])}})
	}
	if len(cands) == 0 {
		return decision.Intent{}, fmt.Errorf("no generic candidates")
	}
	k := g.t.Choose(g.v1Decision(d, cands, acts, true))
	if k < 0 || k >= len(intents) {
		return decision.Intent{}, fmt.Errorf("generic chose %d of %d", k, len(intents))
	}
	return intents[k], nil
}

// genericAttack is route (a)'s attack declaration at the shadow's pending
// KAttackers decision pd: the policy's planner over our creatures that can
// attack the opponent.
func (p *Policy) genericAttack(sh *Shadow, d *v2agent.Decision, pd *decision.Decision) (decision.Intent, error) {
	g := p.gen
	var ids []uint32
	byArena := map[uint32]state.ObjID{} // lookup only
	seen := map[state.ObjID]bool{}      // lookup only
	for _, o := range pd.Options {
		if seen[o.Obj] || o.Battle != 0 || o.Player == pd.Player {
			continue
		}
		seen[o.Obj] = true
		if v2, ok := sh.ObjToV2[o.Obj]; ok {
			a := g.aid(v2)
			ids = append(ids, a)
			byArena[a] = o.Obj
		}
	}
	plan := g.t.PlanAttackFor(g.v1Decision(d, nil, nil, false), ids)
	in := decision.Intent{Seq: pd.Seq, Player: pd.Player}
	for _, o := range pd.Options {
		if o.Battle != 0 || o.Player == pd.Player || !plan[g.aid(sh.ObjToV2[o.Obj])] {
			continue
		}
		dup := false
		for _, c := range in.Choices {
			if o2, _ := optByIndex(pd, c); o2 != nil && o2.Obj == o.Obj {
				dup = true
			}
		}
		if !dup {
			in.Choices = append(in.Choices, o.Index)
		}
	}
	if err := pd.Validate(in); err != nil {
		return in, fmt.Errorf("generic attack refused: %v", err)
	}
	return in, nil
}

// genericBlock is route (a)'s block declaration at the shadow's pending
// KBlockers decision pd.
func (p *Policy) genericBlock(sh *Shadow, d *v2agent.Decision, pd *decision.Decision) (decision.Intent, error) {
	g := p.gen
	plan := g.t.PlanBlocksFor(g.v1Decision(d, nil, nil, true))
	in := decision.Intent{Seq: pd.Seq, Player: pd.Player}
	for _, o := range pd.Options {
		bv, ok1 := sh.ObjToV2[o.Obj]
		av, ok2 := sh.ObjToV2[o.Attacker]
		if !ok1 || !ok2 {
			continue
		}
		if a, planned := plan[g.aid(bv)]; planned && a == g.aid(av) {
			in.Choices = append(in.Choices, o.Index)
		}
	}
	if err := pd.Validate(in); err != nil {
		return in, fmt.Errorf("generic block refused: %v", err)
	}
	return in, nil
}
