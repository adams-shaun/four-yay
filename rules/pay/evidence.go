package pay

import (
	"sort"
	"strconv"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// This file owns the CollectEvidence payment's engine-free mechanics (CR
// 701.30b): the cast/activation flow (rules/cast_targets.go's evidenceAsk),
// the trigger-body Cost$ window (rules/evidence_cost.go) and the off-stack
// mana-ability path (ManaCostChoiceStages' evidence stage, Cryptex) all read
// the candidate gather, the deterministic order (mana value DESCENDING, ties
// by object id), the greedy minimum card count and the total from here ONCE.

// EvidenceGraveCandidates returns the payer's own graveyard cards in zone
// order, excluding any id in reserved (a shared reservation set so one card
// cannot pay two cost parts), skipping objects with no printed face.
func EvidenceGraveCandidates(g *state.Game, player state.PlayerID, reserved map[state.ObjID]bool) []state.ObjID {
	var out []state.ObjID
	for _, id := range g.Zone(state.ZGraveyard, player) {
		if reserved[id] {
			continue
		}
		if o := g.Obj(id); o != nil && o.Face() != nil {
			out = append(out, id)
		}
	}
	return out
}

// EvidenceOrder copies ids and orders them by mana value DESCENDING, ties by
// object id ascending. The order is the option order the ask poses: the FIRST
// minimum-count options then always reach the threshold, so the deterministic
// bot's generic KChoose arm and Clamp's top-up both settle a legal evidence
// payment on the first answer.
func EvidenceOrder(g *state.Game, ids []state.ObjID) []state.ObjID {
	ordered := append([]state.ObjID(nil), ids...)
	sort.Slice(ordered, func(i, j int) bool {
		mi, mj := g.Obj(ordered[i]).Face().Cmc(), g.Obj(ordered[j]).Face().Cmc()
		if mi != mj {
			return mi > mj
		}
		return ordered[i] < ordered[j]
	})
	return ordered
}

// EvidenceManaValue sums the mana values of the named cards the payer owns.
func EvidenceManaValue(g *state.Game, p state.PlayerID, ids []state.ObjID) int32 {
	var n int32
	for _, id := range ids {
		if o := g.Obj(id); o != nil && o.Face() != nil && o.Owner == p {
			n = costvocab.AddClampedGeneric(n, int64(o.Face().Cmc()))
		}
	}
	return n
}

// EvidenceGreedyMin returns the smallest prefix length of the ORDERED ids
// whose total reaches need -- the ask's Min, and therefore the count the
// deterministic bot answers with. need <= 0 yields 0; a list that never
// reaches need yields len(ids) (callers check reachability first).
func EvidenceGreedyMin(g *state.Game, ids []state.ObjID, need int32) int {
	if need <= 0 {
		return 0
	}
	remaining := need
	for i, id := range ids {
		if remaining <= 0 {
			return i
		}
		remaining -= g.Obj(id).Face().Cmc()
	}
	return len(ids)
}

// ManaEvidenceNeed is the collect-evidence threshold a mana ability's cost
// owes: the sum of its literal CollectEvidence<N> parts. A part whose amount
// is a name (CollectEvidence<X>) or an announced X is resolved only by the
// cast flow (after the target stages) and has no reading here, so it answers
// -1 and the mana path refuses the ability rather than pay it free.
func ManaEvidenceNeed(cost costvocab.Cost) int32 {
	need := int32(0)
	for _, part := range cost.Evidence {
		if part.Dyn != "" || part.Announced {
			return -1
		}
		need = costvocab.AddClampedGeneric(need, int64(part.N))
	}
	return need
}

// manaEvidencePayable reports whether the payer's graveyard, less the cards
// the same cost's exile parts reserve, still reaches the evidence threshold.
func manaEvidencePayable(e Engine, p state.PlayerID, cost costvocab.Cost, reserved []state.ObjID) bool {
	if len(cost.Evidence) == 0 {
		return true
	}
	need := ManaEvidenceNeed(cost)
	if need < 0 {
		return false
	}
	if need == 0 {
		return true
	}
	held := make(map[state.ObjID]bool, len(reserved))
	for _, id := range reserved {
		held[id] = true
	}
	g := e.Game()
	return EvidenceManaValue(g, p, EvidenceGraveCandidates(g, p, held)) >= need
}

// manaCostEvidenceStage elects the cards a mana ability's CollectEvidence<N>
// part exiles (CR 701.30b), after the exile parts so the two cannot claim one
// card. A non-interactive caller (and an election with no choice) records the
// greedy mana-value-descending prefix; otherwise the seat is asked for any
// combination whose total reaches N, and a short or stale answer is rejected
// and re-posed (the cast flow's evidenceAsk settle-validate shape).
func manaCostEvidenceStage(e Engine, md *ManaCostActivation) ManaCostStep {
	if len(md.Cost.Evidence) == 0 || md.EvidenceDone {
		return ManaCostNext
	}
	need := ManaEvidenceNeed(md.Cost)
	if need < 0 {
		e.Session().ManaCost = nil
		return ManaCostDropped
	}
	if need == 0 {
		md.EvidenceDone = true
		return ManaCostNext
	}
	g := e.Game()
	reserved := make(map[state.ObjID]bool, len(md.Exiles))
	for _, id := range md.Exiles {
		reserved[id] = true
	}
	eligible := EvidenceGraveCandidates(g, md.Player, reserved)
	if EvidenceManaValue(g, md.Player, eligible) < need {
		e.Session().ManaCost = nil
		return ManaCostDropped
	}
	if md.EvidenceAsked {
		md.EvidenceAsked = false
		allowed := make(map[state.ObjID]bool, len(eligible))
		for _, id := range eligible {
			allowed[id] = true
		}
		valid := len(md.Evidence) > 0
		seen := make(map[state.ObjID]bool, len(md.Evidence))
		for _, id := range md.Evidence {
			if !allowed[id] || seen[id] {
				valid = false
				break
			}
			seen[id] = true
		}
		if valid && EvidenceManaValue(g, md.Player, md.Evidence) >= need {
			md.EvidenceDone = true
			return ManaCostNext
		}
		md.Evidence = nil
		e.Emit(events.Event{Kind: events.Note, Player: md.Player,
			Text: "evidence selection's total mana value is too low; choose again"})
	}
	ids := EvidenceOrder(g, eligible)
	min := EvidenceGreedyMin(g, ids, need)
	if !md.Interactive || min == len(ids) {
		md.Evidence = append([]state.ObjID(nil), ids[:min]...)
		md.EvidenceDone = true
		return ManaCostNext
	}
	d := &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: min, Max: len(ids),
		Prompt: "Exile evidence with total mana value " + strconv.Itoa(int(need)) +
			" to pay the mana ability cost", Source: md.Source}
	for _, id := range ids {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "evidence",
			Obj: id, Label: TargetName(g, id), Value: int(g.Obj(id).Face().Cmc())})
	}
	md.EvidenceAsked = true
	e.Ask(AskManaEvidence, d)
	return ManaCostAsked
}

// SettleManaEvidence exiles the elected evidence cards from the payer's
// graveyard and records the collect-evidence action (the cast path's
// CollectEvidence settle, rules/cast_commit.go).
func SettleManaEvidence(e Engine, md *ManaCostActivation) {
	for _, id := range md.Evidence {
		if e.Game().Obj(id) != nil {
			e.Emit(events.EvidenceCost(id))
		}
	}
	if len(md.Evidence) > 0 {
		e.Emit(events.Event{Kind: events.CollectEvidenceAction, Player: md.Player})
	}
}
