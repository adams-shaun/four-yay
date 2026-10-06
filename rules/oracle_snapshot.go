package rules

import (
	"fmt"
	"sort"
	"strings"

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

// OracleSnapPerm is one battlefield permanent. Ref is the object's scenario
// ref (objRef): the one a scenario step would use to name it.
type OracleSnapPerm struct {
	Ref              string           `json:"ref"`
	Name             string           `json:"name"`
	Controller       int              `json:"controller"`
	Owner            int              `json:"owner"`
	Token            bool             `json:"token,omitempty"`
	Tapped           bool             `json:"tapped,omitempty"`
	FaceDown         bool             `json:"face_down,omitempty"`
	PT               string           `json:"pt,omitempty"` // creatures only
	Damage           int32            `json:"damage,omitempty"`
	Counters         map[string]int32 `json:"counters,omitempty"`
	Types            []string         `json:"types"` // sorted
	AllCreatureTypes bool             `json:"all_creature_types,omitempty"`
	Colors           string           `json:"colors"`
	Keywords         []string         `json:"keywords,omitempty"` // sorted
	AttachedTo       string           `json:"attached_to,omitempty"`
	Attacking        bool             `json:"attacking,omitempty"`
	Blocking         bool             `json:"blocking,omitempty"`
}

// OracleSnapStack is one stack object, listed top first.
type OracleSnapStack struct {
	Kind       string `json:"kind"`   // "spell" or "ability"
	Source     string `json:"source"` // ref of the spell, or of the ability's source
	Controller int    `json:"controller"`
}

// OracleDecision is one non-priority decision the scenario answered.
// Step is the scenario step that posed it (-1 during setup); PickIdx and
// PickRefs name the picks by option index and by object ref ("pN" for a
// player), so the generator can script the same answer for XMage.
type OracleDecision struct {
	Step     int      `json:"step"`
	Seat     int      `json:"seat"`
	Kind     string   `json:"kind"` // target, yesno, mode, choose_n, order, attackers, blockers
	Options  int      `json:"options"`
	Picks    []string `json:"picks"`
	PickIdx  []int    `json:"pick_idx"`
	PickRefs []string `json:"pick_refs"`
	// ObjectPicks records only selected game-object identities, in submission
	// order. Unlike a snapshot delta, these are the objects the player chose.
	ObjectPicks []string `json:"object_picks,omitempty"`
	// PickKinds is the engine option kind of each pick (parallel to Picks),
	// so the generator can tell a card pick XMage poses as a target from a
	// labelled pick XMage poses as a makeChoose choice without guessing from
	// the label text.
	PickKinds []string `json:"pick_kinds,omitempty"`
	// Resume is the decision's ResumeKind: it tells the controller-facing
	// which-opponent ask ("opp_pick", XMage's ChoicePlayer) from a player
	// target, which share the option kind "player".
	Resume string `json:"resume,omitempty"`
	Via    string `json:"via"` // how the runner answered: target, answer, or a fallback
	// GorgeKind and First let a generator script the same decision for
	// gorge's runner: the raw decision kind and option 0's label.
	GorgeKind string `json:"gorge_kind"`
	First     string `json:"first,omitempty"`
	Min       int    `json:"min"`
	Max       int    `json:"max"`
	// PerPlayer marks a target ask shaped by Forge's TargetsForEachPlayer$
	// (CR 601.2c): XMage poses one target for EACH player in seat order, so
	// the generator must answer one seat at a time. SeatCount is the number
	// of seats in the match, the number of asks XMage makes. Both are false/
	// zero for every ordinary target ask, so the generator's output is
	// unchanged for them.
	PerPlayer bool `json:"per_player,omitempty"`
	SeatCount int  `json:"seat_count,omitempty"`
	// PerOpponent marks a per-player target ask whose filter admits only
	// opponents' objects: XMage asks no target for the controller's seat.
	PerOpponent bool `json:"per_opponent,omitempty"`
	// AltPayable counts the options of an AlternateAdditionalCost either-or
	// ask (option kind "altaddcost") the cast could pay. XMage's OrCost poses
	// its chooseUse only when two or more of its costs can be paid, so the
	// generator scripts the boolean only for AltPayable >= 2.
	AltPayable int `json:"alt_payable,omitempty"`
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
	blocking := map[state.ObjID]bool{}
	for _, id := range field {
		for _, b := range g.Obj(id).BlockedBy {
			blocking[b] = true
		}
	}
	for _, id := range field {
		o := g.Obj(id)
		derived := e.Derived(id)
		types := append([]string(nil), derived.Types...)
		sort.Strings(types)
		kws := append([]string(nil), e.Keywords(id)...)
		sort.Strings(kws)
		p := OracleSnapPerm{
			Ref: r.objRef(o), Name: r.fieldName(o), Controller: int(o.Controller), Owner: int(o.Owner),
			Token: o.IsToken, Tapped: o.Tapped, FaceDown: o.FaceDown, Damage: o.Damage,
			Counters: r.snapCounters(o.Counters), Types: types, AllCreatureTypes: derived.AllCreatureTypes,
			Colors: e.Colors(id), Keywords: kws,
			Attacking: o.IsAttacking, Blocking: blocking[id],
		}
		if oracleHasFold(types, "Creature") {
			p.PT = fmt.Sprintf("%d/%d", e.Power(id), e.Toughness(id))
		}
		switch {
		case o.AttachedTo != 0:
			p.AttachedTo = r.objRef(g.Obj(o.AttachedTo))
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
		it.Source = r.objRef(src)
		s.Stack = append(s.Stack, it)
	}
	return s
}

// objRef names o by the ref resolve maps back to o right now, so a snapshot
// ref is the object's identity, never its position on the battlefield:
//
//   - a card named at setup keeps its setup ref ("p1:Grizzly Bears#2") in
//     every zone, whoever controls it;
//   - any other card is "p<owner>:<name>[#k]", k its rank in ObjID order
//     among the owner's live objects of that name (resolve's own fallback;
//     card objects never cease, so k never changes);
//   - a token is "p<controller>:token:<name>[#k]", k its rank among that
//     seat's battlefield tokens whose name contains <name> (resolve's token
//     rule). Tokens are positional, as in scenarios; the comparator matches
//     them by characteristics (spec section 7).
func (r *oracleRun) objRef(o *state.Object) string {
	if o == nil {
		return ""
	}
	bound := ""
	for ref, id := range r.refs {
		if id == o.ID && (bound == "" || ref < bound) {
			bound = ref
		}
	}
	if bound != "" {
		return bound
	}
	name := r.objName(o)
	lower := strings.ToLower(name)
	seat, k := o.Owner, 0
	if o.IsToken {
		seat = o.Controller
	}
	for i := range r.e.G.Objs {
		c := &r.e.G.Objs[i]
		if c.Zone == state.ZCeased || c.Ability != nil || c.Face() == nil {
			continue
		}
		if o.IsToken {
			if !c.IsToken || c.Controller != seat || c.Zone != state.ZBattlefield || !strings.Contains(strings.ToLower(c.Face().Name), lower) {
				continue
			}
		} else if c.Owner != seat || c.Face().Name != name {
			continue
		}
		k++
		if c.ID == o.ID {
			break
		}
	}
	base := fmt.Sprintf("p%d:%s", seat, name)
	if o.IsToken {
		base = fmt.Sprintf("p%d:token:%s", seat, name)
	}
	ref := base
	if k > 1 {
		ref = fmt.Sprintf("%s#%d", base, k)
	}
	// A setup ref of the same spelling bound to another object would shadow
	// this one in resolve; "#1" is never bound at setup and still resolves
	// positionally.
	if id, ok := r.refs[ref]; ok && id != o.ID && k == 1 {
		ref = base + "#1"
	}
	return ref
}
