package rules

import (
	"fmt"
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// OracleSnapshot is the engine-neutral observable state of an oracle
// scenario at one checkpoint (spec 2026-10-02-xmage-compliance-oracle-design
// §7). The XMage driver emits the same shape, so the comparator diffs the
// two field by field. Building one never writes game state.
type OracleSnapshot struct {
	Checkpoint string             `json:"checkpoint"`
	Turn       int32              `json:"turn"`
	Step       string             `json:"step"`
	Active     int                `json:"active"`
	Priority   int                `json:"priority"`
	Over       bool               `json:"over"`
	Winner     *int               `json:"winner,omitempty"`
	Players    []OracleSnapPlayer `json:"players"`
	Permanents []OracleSnapPerm   `json:"permanents"`
	Stack      []OracleSnapStack  `json:"stack"`
}

// OracleSnapPlayer is one seat's public and (scenarios are omniscient)
// hidden state.
type OracleSnapPlayer struct {
	Seat         int              `json:"seat"`
	Life         int32            `json:"life"`
	Counters     map[string]int32 `json:"counters,omitempty"`
	Hand         []string         `json:"hand"`      // sorted
	Graveyard    []string         `json:"graveyard"` // zone order
	Exile        []string         `json:"exile"`     // sorted
	Command      []string         `json:"command"`   // sorted
	LibraryCount int              `json:"library_count"`
	LibraryTop   []string         `json:"library_top"` // top first, at most oracleLibraryTopN
	Pool         string           `json:"pool"`        // WUBRGC letters
}

// OracleSnapPerm is one battlefield permanent. Ref follows the scenario ref
// rule: "p<controller>:<name>", "#k" for the k-th (k>1) of that name the
// seat controls in arrival (ObjID) order; tokens are "p<c>:token:<name>".
type OracleSnapPerm struct {
	Ref        string           `json:"ref"`
	Name       string           `json:"name"`
	Controller int              `json:"controller"`
	Owner      int              `json:"owner"`
	Token      bool             `json:"token,omitempty"`
	Tapped     bool             `json:"tapped,omitempty"`
	FaceDown   bool             `json:"face_down,omitempty"`
	PT         string           `json:"pt,omitempty"` // creatures only
	Damage     int32            `json:"damage,omitempty"`
	Counters   map[string]int32 `json:"counters,omitempty"`
	Types      []string         `json:"types"` // sorted
	Colors     string           `json:"colors"`
	Keywords   []string         `json:"keywords,omitempty"` // sorted
	AttachedTo string           `json:"attached_to,omitempty"`
	Attacking  bool             `json:"attacking,omitempty"`
	Blocking   bool             `json:"blocking,omitempty"`
}

// OracleSnapStack is one stack object, listed top first.
type OracleSnapStack struct {
	Kind       string `json:"kind"`   // "spell" or "ability"
	Source     string `json:"source"` // ref of the spell, or of the ability's source
	Controller int    `json:"controller"`
}

// OracleDecision is one non-priority decision the scenario answered.
type OracleDecision struct {
	Seat    int      `json:"seat"`
	Kind    string   `json:"kind"` // target, yesno, mode, choose_n, order, attackers, blockers
	Options int      `json:"options"`
	Picks   []string `json:"picks"`
}

const oracleLibraryTopN = 5

// oracleDecisionKind normalizes a decision kind to the cross-engine
// vocabulary; ok is false for kinds that are not compared.
func oracleDecisionKind(k decision.Kind) (string, bool) {
	switch k {
	case decision.KPriority, decision.KMulligan, decision.KStartingPlayer:
		return "", false
	case decision.KTriggerOptional, decision.KCommanderZone:
		return "yesno", true
	case decision.KModes:
		return "mode", true
	case decision.KChoose:
		return "choose_n", true
	case decision.KTriggerOrder, decision.KArrange, decision.KReplacement:
		return "order", true
	}
	return string(k), true
}

func (r *oracleRun) snapCounters(cs []state.Counter) map[string]int32 {
	if len(cs) == 0 {
		return nil
	}
	m := map[string]int32{}
	for _, c := range cs {
		m[normCounter(c.Kind)] += c.N
	}
	return m
}

func (r *oracleRun) zoneNames(z state.Zone, p state.PlayerID, sorted bool) []string {
	ids := r.e.G.Zone(z, p)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.objName(r.e.G.Obj(id)))
	}
	if sorted {
		sort.Strings(out)
	}
	return out
}

// snapshot builds the checkpoint. Read-only: it calls only accessors.
func (r *oracleRun) snapshot(checkpoint string) OracleSnapshot {
	e, g := r.e, r.e.G
	s := OracleSnapshot{
		Checkpoint: checkpoint, Turn: g.Turn, Step: g.Step.String(),
		Active: int(g.Active), Priority: int(g.Priority), Over: g.Over,
		Players: []OracleSnapPlayer{}, Permanents: []OracleSnapPerm{}, Stack: []OracleSnapStack{},
	}
	if g.Over && !g.Draw {
		w := int(g.Winner)
		s.Winner = &w
	}
	for i := range g.Players {
		p := state.PlayerID(i)
		lib := g.Zone(state.ZLibrary, p)
		top := make([]string, 0, oracleLibraryTopN)
		for j := 0; j < len(lib) && j < oracleLibraryTopN; j++ {
			top = append(top, r.objName(g.Obj(lib[j])))
		}
		s.Players = append(s.Players, OracleSnapPlayer{
			Seat: i, Life: g.Players[i].Life, Counters: r.snapCounters(g.Players[i].Counters),
			Hand: r.zoneNames(state.ZHand, p, true), Graveyard: r.zoneNames(state.ZGraveyard, p, false),
			Exile: r.zoneNames(state.ZExile, p, true), Command: r.zoneNames(state.ZCommand, p, true),
			LibraryCount: len(lib), LibraryTop: top, Pool: poolString(g.Players[i].Pool),
		})
	}
	var field []state.ObjID
	for i := range g.Players {
		field = append(field, g.Zone(state.ZBattlefield, state.PlayerID(i))...)
	}
	sort.Slice(field, func(a, b int) bool { return field[a] < field[b] })
	refs := r.snapRefs(field)
	blocking := map[state.ObjID]bool{}
	for _, id := range field {
		for _, b := range g.Obj(id).BlockedBy {
			blocking[b] = true
		}
	}
	for _, id := range field {
		o := g.Obj(id)
		types := append([]string(nil), e.Derived(id).Types...)
		sort.Strings(types)
		kws := append([]string(nil), e.Keywords(id)...)
		sort.Strings(kws)
		p := OracleSnapPerm{
			Ref: refs[id], Name: r.objName(o), Controller: int(o.Controller), Owner: int(o.Owner),
			Token: o.IsToken, Tapped: o.Tapped, FaceDown: o.FaceDown, Damage: o.Damage,
			Counters: r.snapCounters(o.Counters), Types: types, Colors: e.Colors(id), Keywords: kws,
			Attacking: o.IsAttacking, Blocking: blocking[id],
		}
		if oracleHasFold(types, "Creature") {
			p.PT = fmt.Sprintf("%d/%d", e.Power(id), e.Toughness(id))
		}
		switch {
		case o.AttachedTo != 0:
			if ref, ok := refs[o.AttachedTo]; ok {
				p.AttachedTo = ref
			} else {
				p.AttachedTo = r.objName(g.Obj(o.AttachedTo))
			}
		case o.HasAttachedPlayer:
			p.AttachedTo = fmt.Sprintf("p%d", o.AttachedPlayer)
		}
		s.Permanents = append(s.Permanents, p)
	}
	for i := len(g.Stack) - 1; i >= 0; i-- {
		o := g.Obj(g.Stack[i])
		if o == nil {
			continue
		}
		it := OracleSnapStack{Kind: "spell", Controller: int(o.Controller)}
		src := o
		if o.Ability != nil {
			it.Kind = "ability"
			src = g.Obj(o.Source)
		}
		it.Source = r.stackRef(src)
		s.Stack = append(s.Stack, it)
	}
	return s
}

// snapRefs assigns scenario-style refs to ids, which must be in ObjID order.
func (r *oracleRun) snapRefs(ids []state.ObjID) map[state.ObjID]string {
	refs := make(map[state.ObjID]string, len(ids))
	seen := map[string]int{}
	for _, id := range ids {
		o := r.e.G.Obj(id)
		base := fmt.Sprintf("p%d:%s", o.Controller, r.objName(o))
		if o.IsToken {
			base = fmt.Sprintf("p%d:token:%s", o.Controller, r.objName(o))
		}
		seen[base]++
		if seen[base] > 1 {
			base = fmt.Sprintf("%s#%d", base, seen[base])
		}
		refs[id] = base
	}
	return refs
}

// stackRef names a spell or ability source by the scenario ref bound at
// setup when there is one (a card keeps its ref across zones), else by
// "p<owner>:<name>".
func (r *oracleRun) stackRef(o *state.Object) string {
	if o == nil {
		return ""
	}
	best := ""
	for ref, id := range r.refs {
		if id == o.ID && (best == "" || ref < best) {
			best = ref
		}
	}
	if best != "" {
		return best
	}
	return fmt.Sprintf("p%d:%s", o.Owner, r.objName(o))
}
