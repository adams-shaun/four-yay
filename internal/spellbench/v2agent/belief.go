package v2agent

import (
	"encoding/json"
	"io"
	"sort"
	"strings"
)

// Belief is the seat's reconstruction of the game from its own message
// stream: the decoded observation reduced to comparable facts, plus the
// hidden-zone contents it can infer from the decklists (spec 10.2 always
// sends the seat its own list; the opponent's list when the rules make it
// visible). It is the input a shadow gorge state and a world sampler build
// on (design spec D§4, D§5), and what the reverse-adapter shadow check
// compares against engine truth (cmd/sbv2shadow).
//
// Inference is pure multiset arithmetic, deliberately simple so a mismatch
// points at the observation or the rule, not at the estimator:
//
//	own library  = own decklist - own cards seen in hand, on any battlefield,
//	               in the graveyard, in exile, in the command zone and as
//	               spells on the stack (tokens and copies excluded)
//	opp hidden   = opponent decklist - the opponent's cards seen the same
//	               way (its hand is hidden, so this is hand + library)
//
// A card whose name is hidden (face down) cannot be subtracted and is
// counted in Unnamed instead.
type Belief struct {
	seat    string
	ownDeck map[string]int
	oppDeck map[string]int // nil when the opponent's list is hidden
	// faces maps each face of a multi-face decklist entry ("A // B") to
	// the entry: an object names the face currently up (spec 4.4).
	faces map[string]string
}

// NewBelief starts a belief from game_start.
func NewBelief(g *GameStart) *Belief {
	b := &Belief{seat: g.Seat, ownDeck: deckCounts(g.OwnDeck), faces: map[string]string{}}
	if g.OpponentDeck != nil {
		b.oppDeck = deckCounts(g.OpponentDeck)
	}
	for _, d := range []map[string]int{b.ownDeck, b.oppDeck} {
		for name := range d {
			if parts := strings.Split(name, " // "); len(parts) > 1 {
				for _, p := range parts {
					b.faces[p] = name
				}
			}
		}
	}
	return b
}

// deckKey is the decklist entry a visible object counts against: its full
// name when the engine sends one, else the entry its face name belongs to.
func (b *Belief) deckKey(name string, full *string) string {
	if full != nil && *full != "" {
		return *full
	}
	if k, ok := b.faces[name]; ok {
		return k
	}
	return name
}

func deckCounts(d *Deck) map[string]int {
	if d == nil {
		return nil
	}
	m := map[string]int{}
	for _, r := range d.Decklist {
		m[r.Name] += int(r.Count)
	}
	return m
}

// BeliefRecord is one decision's reconstruction, keyed like the engine's
// truth record (game_id, seat, seat_step).
type BeliefRecord struct {
	GameID    string `json:"game_id"`
	Seat      string `json:"seat"`
	SeatStep  int64  `json:"seat_step"`
	Turn      int64  `json:"turn"`
	PhaseStep string `json:"phase_step"`
	Active    string `json:"active_seat"`

	Players []BeliefPlayer `json:"players"`
	Objects []BeliefObject `json:"objects"`

	OwnLibrary      map[string]int `json:"own_library"`
	OwnUnnamed      int            `json:"own_unnamed"`
	OppHidden       map[string]int `json:"opp_hidden"`
	OppUnnamed      int            `json:"opp_unnamed"`
	OppHiddenCount  int            `json:"opp_hidden_count"`
	OwnLibraryCount int            `json:"own_library_count"`
}

// BeliefPlayer is one player's public scalars as decoded.
type BeliefPlayer struct {
	Seat          string         `json:"seat"`
	Life          int32          `json:"life"`
	HandCount     int            `json:"hand_size"`
	LibraryCount  int            `json:"library_size"`
	GraveyardSize int            `json:"graveyard_size"`
	Pool          map[string]int `json:"pool"`
}

// BeliefObject is one decoded object record (the shape of the engine's
// truth objects, so the two join by object_id).
type BeliefObject struct {
	ObjectID   string           `json:"object_id"`
	Zone       string           `json:"zone"`
	Name       string           `json:"name"`
	Controller string           `json:"controller"`
	Owner      string           `json:"owner"`
	Tapped     bool             `json:"tapped,omitempty"`
	Power      *int32           `json:"power,omitempty"`
	Toughness  *int32           `json:"toughness,omitempty"`
	Damage     int32            `json:"damage,omitempty"`
	Counters   map[string]int32 `json:"counters,omitempty"`
	SummonSick bool             `json:"summon_sick,omitempty"`
	Attacking  bool             `json:"attacking,omitempty"`
	Token      bool             `json:"token,omitempty"`
	FaceDown   bool             `json:"face_down,omitempty"`
	Keywords   []string         `json:"keywords,omitempty"`
}

// Record reduces decision d to a BeliefRecord.
func (b *Belief) Record(gameID string, d *Decision) BeliefRecord {
	o := d.Observation()
	r := BeliefRecord{GameID: gameID, Seat: b.seat, Turn: o.Turn, PhaseStep: o.PhaseStep}
	if d.SeatStep != nil {
		r.SeatStep = *d.SeatStep
	}
	if o.ActiveSeat != nil {
		r.Active = *o.ActiveSeat
	}
	own, opp := copyCounts(b.ownDeck), copyCounts(b.oppDeck)
	subtract := func(ref *ObjectRef, full *string, token, isCopy bool) {
		if token || isCopy {
			return
		}
		target, unnamed := own, &r.OwnUnnamed
		if ref.OwnerSeat != b.seat {
			target, unnamed = opp, &r.OppUnnamed
		}
		if ref.CardName == nil {
			*unnamed++
			return
		}
		if target != nil {
			target[b.deckKey(*ref.CardName, full)]--
		}
	}
	for i := range o.Players {
		p := &o.Players[i]
		pool := map[string]int{}
		for _, kv := range []struct {
			k string
			v uint32
		}{{"W", p.ManaPool.W}, {"U", p.ManaPool.U}, {"B", p.ManaPool.B}, {"R", p.ManaPool.R}, {"G", p.ManaPool.G}, {"C", p.ManaPool.C}} {
			if kv.v != 0 {
				pool[kv.k] = int(kv.v)
			}
		}
		r.Players = append(r.Players, BeliefPlayer{Seat: p.Seat, Life: p.Life, HandCount: int(p.HandCount),
			LibraryCount: int(p.LibraryCount), GraveyardSize: len(p.Graveyard), Pool: pool})
		if p.Seat == b.seat {
			r.OwnLibraryCount = int(p.LibraryCount)
		} else {
			r.OppHiddenCount = int(p.HandCount) + int(p.LibraryCount)
		}
		for _, zone := range []struct {
			name string
			recs []ObjectRecord
		}{{"battlefield", p.Battlefield}, {"graveyard", p.Graveyard}, {"exile", p.Exile}, {"hand", p.Hand}, {"command", p.Command}} {
			for j := range zone.recs {
				rec := &zone.recs[j]
				subtract(&rec.ObjectRef, rec.FullName, rec.Token, rec.Copy)
				r.Objects = append(r.Objects, beliefObject(zone.name, rec))
			}
		}
	}
	for i := range o.Stack {
		se := &o.Stack[i]
		name := ""
		if se.CardName != nil {
			name = *se.CardName
		}
		r.Objects = append(r.Objects, BeliefObject{ObjectID: se.ObjectID, Zone: "stack", Name: name,
			Controller: se.ControllerSeat, Owner: se.OwnerSeat})
		if se.StackKind == "spell" {
			subtract(&se.ObjectRef, nil, false, se.Copy)
		}
	}
	r.OwnLibrary, r.OppHidden = positive(own), positive(opp)
	return r
}

func beliefObject(zone string, rec *ObjectRecord) BeliefObject {
	bo := BeliefObject{ObjectID: rec.ObjectID, Zone: zone, Controller: rec.ControllerSeat, Owner: rec.OwnerSeat,
		Token: rec.Token, FaceDown: rec.FaceDown}
	if rec.CardName != nil {
		bo.Name = *rec.CardName
	}
	if p := rec.Permanent; p != nil {
		bo.Tapped, bo.Damage, bo.SummonSick, bo.Attacking = p.Tapped, int32(p.Damage), p.SummoningSick, p.Attacking
		if len(p.Counters) > 0 {
			bo.Counters = map[string]int32{}
			for k, v := range p.Counters {
				bo.Counters[k] = int32(v)
			}
		}
		if c := rec.Characteristics; c != nil {
			bo.Power, bo.Toughness = c.Power, c.Toughness
			bo.Keywords = append([]string(nil), c.Keywords...)
			sort.Strings(bo.Keywords)
		}
	}
	return bo
}

func copyCounts(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// positive drops zero entries (a negative one, an impossible count, is
// kept: it is exactly what the shadow check must see).
func positive(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := map[string]int{}
	for k, v := range m {
		if v != 0 {
			out[k] = v
		}
	}
	return out
}

// beliefWriter writes one JSON line per record.
type beliefWriter struct{ w io.Writer }

func (bw beliefWriter) write(r BeliefRecord) {
	raw, err := json.Marshal(r)
	if err != nil {
		return
	}
	bw.w.Write(append(raw, '\n'))
}
