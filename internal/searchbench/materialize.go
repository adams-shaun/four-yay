package searchbench

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/searchbench/statespec"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Refusal is a reason a spec, a position or an item is not used, with a
// stable Code (the rejection-ledger key, mirroring upstream items.py's
// Rejected reasons where one exists) and a free-text Detail.
type Refusal struct {
	Code, Detail string
}

func (r *Refusal) Error() string {
	if r.Detail == "" {
		return r.Code
	}
	return r.Code + ": " + r.Detail
}

func refuse(code, format string, a ...any) *Refusal {
	return &Refusal{Code: code, Detail: fmt.Sprintf(format, a...)}
}

// SeatID maps a StateSpec seat onto a gorge player: A is seat 0, B seat 1.
func SeatID(seat string) state.PlayerID {
	if seat == "B" {
		return 1
	}
	return 0
}

// SeatName is SeatID's inverse.
func SeatName(p state.PlayerID) string {
	if p == 1 {
		return "B"
	}
	return "A"
}

// Materialized is a StateSpec built into a hypothetical gorge engine.
type Materialized struct {
	Engine *rules.Engine
	Spec   *statespec.Spec
	// Perms[seat][i] lists the objects of battlefield entry i of seat's
	// battlefield, one per copy (count > 1 expands in copy order).
	Perms map[string][][]state.ObjID
	// Alias maps every spec alias ("A:x", "A:x#2") to its object.
	Alias map[string]state.ObjID
	// Tokens records each token entry's gorge script, keyed like Alias
	// ("<seat>:<index>" for an entry with no id).
	Tokens map[string]string
}

// AliasOf is the spec alias naming id ("" if none does). A bare "<seat>:x"
// is preferred to "<seat>:x#1".
func (m *Materialized) AliasOf(id state.ObjID) string {
	best := ""
	for a, o := range m.Alias {
		if o != id {
			continue
		}
		if best == "" || len(a) < len(best) || (len(a) == len(best) && a < best) {
			best = a
		}
	}
	return best
}

// LookupCard resolves a StateSpec card name against reg: the exact name,
// else a DFC/split "A // B" by its front face (17lands names those by the
// whole name, Forge's registry by face).
func LookupCard(reg *cards.Registry, name string) (*cards.Card, bool) {
	if c, ok := reg.Lookup(name); ok {
		return c, true
	}
	if i := strings.Index(name, " // "); i > 0 {
		if c, ok := reg.Lookup(name[:i]); ok {
			return c, true
		}
	}
	return nil, false
}

var stepOf = map[string]state.Step{
	"UPKEEP": state.StepUpkeep, "DRAW": state.StepDraw, "PRECOMBAT_MAIN": state.StepMain1,
	"BEGIN_COMBAT": state.StepBeginCombat, "DECLARE_ATTACKERS": state.StepDeclareAttackers,
	"DECLARE_BLOCKERS": state.StepDeclareBlockers, "END_COMBAT": state.StepEndCombat,
	"POSTCOMBAT_MAIN": state.StepMain2, "END_TURN": state.StepEnd, "CLEANUP": state.StepCleanup,
}

// StepOf maps a StateSpec step onto gorge's (false for UNTAP and the two
// combat damage steps, which cannot be entered).
func StepOf(step string) (state.Step, bool) {
	s, ok := stepOf[step]
	return s, ok
}

// StepName is StepOf's inverse (XMage names).
func StepName(s state.Step) string {
	for k, v := range stepOf {
		if v == s {
			return k
		}
	}
	switch s {
	case state.StepUntap:
		return "UNTAP"
	case state.StepCombatDamage:
		return "COMBAT_DAMAGE"
	}
	return s.String()
}

// counterKinds are the XMage CounterType names a spec may SET, mapped onto
// gorge (Forge) counter names. They coincide for every kind listed.
var counterKinds = map[string]string{
	"P1P1": "P1P1", "M1M1": "M1M1", "LOYALTY": "LOYALTY", "LORE": "LORE", "STUN": "STUN",
	"SHIELD": "SHIELD", "FINALITY": "FINALITY", "OIL": "OIL", "CHARGE": "CHARGE", "TIME": "TIME",
	"DEFENSE": "DEFENSE", "FLYING": "FLYING", "DEATHTOUCH": "DEATHTOUCH", "LIFELINK": "LIFELINK",
	"TRAMPLE": "TRAMPLE", "VIGILANCE": "VIGILANCE", "MENACE": "MENACE", "REACH": "REACH", "HEXPROOF": "HEXPROOF",
	"INDESTRUCTIBLE": "INDESTRUCTIBLE", "FIRST_STRIKE": "FIRST_STRIKE", "DOUBLE_STRIKE": "DOUBLE_STRIKE",
}

// Materialize builds spec into a hypothetical engine.
//
//   - Libraries are each decklist minus every card named in the seat's
//     other zones (and its owned permanents on either battlefield), with
//     libraryTop on top and the rest in an order drawn from seed;
//     librarySize trims the ordered library to that many cards.
//   - handUnknown > 0 is FILLED from that seat's shuffled remainder (below
//     libraryTop) by the same seeded order, as upstream's bridge does for a
//     partial spec. The benchmark's specs are determinized (0 unknowns).
//   - Tokens resolve through ResolveToken; card names through LookupCard.
//   - The engine's later shuffles are seeded by seed as well (rules.NewStaged).
//
// A spec this build cannot represent is refused (*Refusal).
func Materialize(reg *cards.Registry, spec *statespec.Spec, seed uint64) (*Materialized, error) {
	if errs := spec.Validate(); len(errs) > 0 {
		return nil, refuse("spec invalid", "%s", strings.Join(errs, "; "))
	}
	if len(spec.Stack) > 0 {
		return nil, refuse("stack", "%d stack items", len(spec.Stack))
	}
	if len(spec.Blockers) > 0 {
		return nil, refuse("blockers", "%d declared blockers", len(spec.Blockers))
	}
	step, ok := StepOf(spec.Step)
	if !ok {
		return nil, refuse("step", "%s cannot be entered", spec.Step)
	}
	lookup := func(name string) (*cards.Card, error) {
		c, ok := LookupCard(reg, name)
		if !ok {
			return nil, refuse("unknown card", "%s", name)
		}
		return c, nil
	}
	cardsOf := func(names []string) ([]*cards.Card, error) {
		out := make([]*cards.Card, 0, len(names))
		for _, n := range names {
			c, err := lookup(n)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
		return out, nil
	}
	owned := spec.Owned()
	st := rules.Stage{
		Turn: int32(spec.Turn), Active: SeatID(spec.ActivePlayer), Starting: SeatID(spec.StartingSeat()),
		Step: step, Players: make([]rules.StagedPlayer, 2),
	}
	switch spec.EnterMode {
	case "PRIORITY_FRESH":
		st.Enter = rules.StagePriorityFresh
	case "BEGIN_STEP":
		st.Enter = rules.StageBeginStep
	case "PRIORITY_HELD":
		st.Enter = rules.StagePriorityHeld
		st.PriorityPlayer = SeatID(*spec.PriorityPlayer)
		st.Passed = len(spec.PassedPlayers)
	}
	for _, seat := range statespec.Seats {
		p := spec.Players[seat]
		sp := &st.Players[SeatID(seat)]
		sp.Life = int32(p.Life)
		sp.LandsPlayed = p.LandsPlayed
		if p.ManaPool != nil {
			sp.ManaPool = *p.ManaPool
		}
		var err error
		if sp.Graveyard, err = cardsOf(p.Graveyard); err != nil {
			return nil, err
		}
		if sp.Exile, err = cardsOf(p.Exile); err != nil {
			return nil, err
		}
		if sp.Hand, err = cardsOf(p.Hand); err != nil {
			return nil, err
		}
		top, err := cardsOf(p.LibraryTop)
		if err != nil {
			return nil, err
		}
		// The remainder: decklist minus owned, as a multiset, in decklist
		// order before the seeded shuffle (so the shuffle is the only
		// source of order).
		left := map[string]int{} // lookup only
		for _, n := range owned[seat] {
			left[n]++
		}
		var rest []string
		for _, n := range p.Decklist {
			if left[n] > 0 {
				left[n]--
				continue
			}
			rest = append(rest, n)
		}
		r := rand.New(rand.NewPCG(seed, 0x5eed0000+uint64(SeatID(seat))))
		r.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
		if p.HandUnknown > len(rest) {
			return nil, refuse("spec invalid", "%s.handUnknown %d > %d library cards", seat, p.HandUnknown, len(rest))
		}
		filled, err := cardsOf(rest[:p.HandUnknown])
		if err != nil {
			return nil, err
		}
		sp.Hand = append(sp.Hand, filled...)
		rest = rest[p.HandUnknown:]
		lib, err := cardsOf(rest)
		if err != nil {
			return nil, err
		}
		sp.Library = append(top, lib...)
		if p.LibrarySize != nil && *p.LibrarySize < len(sp.Library) {
			sp.Library = sp.Library[:*p.LibrarySize]
		}
	}
	// Battlefield entries, A then B, copies expanded.
	type ref struct {
		seat  string
		entry int
		copy  int
	}
	var refs []ref
	m := &Materialized{Spec: spec, Perms: map[string][][]state.ObjID{}, Alias: map[string]state.ObjID{}, Tokens: map[string]string{}}
	aliases := spec.Aliases()
	indexOf := map[ref]int{} // lookup only
	for _, seat := range statespec.Seats {
		for i, perm := range spec.Players[seat].Battlefield {
			if perm.FaceDown {
				return nil, refuse("face down", "%s %s", seat, perm.What())
			}
			for k := 1; k <= perm.Count; k++ {
				indexOf[ref{seat, i, k}] = len(refs)
				refs = append(refs, ref{seat, i, k})
			}
		}
	}
	st.Permanents = make([]rules.StagedPermanent, len(refs))
	for n, rf := range refs {
		perm := spec.Players[rf.seat].Battlefield[rf.entry]
		sp := rules.StagedPermanent{Controller: SeatID(rf.seat), Owner: SeatID(rf.seat), Tapped: perm.Tapped, Sick: perm.Sick, Damage: int32(perm.Damage)}
		if perm.Owner != "" {
			sp.Owner = SeatID(perm.Owner)
		}
		if perm.IsToken() {
			script, err := ResolveToken(reg, perm.TokenClass, perm.Token)
			if err != nil {
				return nil, err
			}
			sp.Token = script
			key := rf.seat + ":" + perm.ID
			if perm.ID == "" {
				key = fmt.Sprintf("%s:%d", rf.seat, rf.entry)
			}
			m.Tokens[key] = script
		} else {
			c, err := lookup(perm.Name)
			if err != nil {
				return nil, err
			}
			sp.Card = c
		}
		kinds := make([]string, 0, len(perm.Counters))
		for k := range perm.Counters {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, k := range kinds {
			g, ok := counterKinds[k]
			if !ok {
				return nil, refuse("counter kind", "%s on %s", k, perm.What())
			}
			sp.Counters = append(sp.Counters, rules.StagedCounter{Kind: g, N: int32(*perm.Counters[k])})
		}
		if perm.AttachTo != "" {
			a := aliases[perm.AttachTo]
			sp.AttachTo = indexOf[ref{a.Seat, a.Index, a.Copy}] + 1
		}
		st.Permanents[n] = sp
	}
	for _, at := range spec.Attackers {
		if !strings.HasPrefix(at.Defender, "player:") {
			return nil, refuse("attack on permanent", "%s attacks %s", at.Attacker, at.Defender)
		}
		a := aliases[at.Attacker]
		st.Attackers = append(st.Attackers, rules.StagedAttack{Attacker: indexOf[ref{a.Seat, a.Index, a.Copy}], Defender: SeatID(strings.TrimPrefix(at.Defender, "player:"))})
	}
	cfg := rules.Config{Seed: seed, Names: []string{"A", "B"}, Tokens: reg.Tokens}
	e, objs, err := rules.NewStaged(cfg, st)
	if err != nil {
		return nil, refuse("stage", "%v", err)
	}
	m.Engine = e
	for _, seat := range statespec.Seats {
		m.Perms[seat] = make([][]state.ObjID, len(spec.Players[seat].Battlefield))
	}
	for n, rf := range refs {
		m.Perms[rf.seat][rf.entry] = append(m.Perms[rf.seat][rf.entry], objs.Permanents[n])
	}
	for a, al := range aliases {
		m.Alias[a] = m.Perms[al.Seat][al.Index][al.Copy-1]
	}
	return m, nil
}
