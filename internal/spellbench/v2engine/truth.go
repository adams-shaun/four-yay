package v2engine

import (
	"encoding/json"
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// The test-mode truth side channel (Options.Truth): one JSON line per posed
// wire decision, keyed by (game_id, seat, seat_step) -- the same key the
// agent's belief log carries -- so a shadow-state check can join the two.
//
// The public part is read through gorge's OWN seat projection
// (view.Project, visibility "seat"), not through this package's
// observation builder, so a comparison checks the builder and the agent's
// decoding against independent engine code. The hidden part is exact
// engine truth the seat must never see: its own library contents and the
// other seat's hand and library contents. It is written to a local file for
// offline comparison only and never reaches an agent (spec 6.8).

// TruthRecord is one line of the side channel.
type TruthRecord struct {
	GameID     string `json:"game_id"`
	Seat       string `json:"seat"`
	SeatStep   int64  `json:"seat_step"`
	Step       int64  `json:"step"`
	GorgeKind  string `json:"gorge_kind"`
	Translator string `json:"translator"`
	Turn       int32  `json:"turn"`
	PhaseStep  string `json:"phase_step"`
	Active     string `json:"active_seat"`

	Players []TruthPlayer `json:"players"`
	// Objects are every public object gorge's seat view shows (both
	// battlefields, graveyards, exiles, the viewer's hand) plus the stack,
	// under the v2 object id this seat's observation uses.
	Objects []TruthObject `json:"objects"`

	OwnLibrary  map[string]int `json:"own_library"`
	OppHand     map[string]int `json:"opp_hand"`
	OppLibrary  map[string]int `json:"opp_library"`
	OwnDecklist map[string]int `json:"own_decklist"`
}

// TruthPlayer is one player's public scalars from gorge's view.
type TruthPlayer struct {
	Seat          string         `json:"seat"`
	Life          int32          `json:"life"`
	HandSize      int            `json:"hand_size"`
	LibrarySize   int            `json:"library_size"`
	GraveyardSize int            `json:"graveyard_size"`
	Pool          map[string]int `json:"pool"`
}

// TruthObject is one object from gorge's view.
type TruthObject struct {
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

func (g *Game) writeTruth(p *posedDecision, d *decision.Decision) {
	seat := p.seat
	v := view.Project(g.e.G, g.e, seat, d)
	r := TruthRecord{GameID: g.id, Seat: seatOf(seat), SeatStep: g.seatStep[seat], Step: g.step,
		GorgeKind: string(d.Kind), Translator: p.translator, Turn: v.Turn, PhaseStep: phaseStep(g.e.G.Step),
		Active: seatOf(v.Active), OwnDecklist: g.decks[seat]}
	for _, pv := range v.Players {
		tp := TruthPlayer{Seat: seatOf(pv.ID), Life: pv.Life, HandSize: pv.HandSize, LibrarySize: pv.LibrarySize,
			GraveyardSize: pv.GraveyardSize, Pool: map[string]int{}}
		for k, n := range pv.Pool {
			if n != 0 {
				tp.Pool[k] = int(n)
			}
		}
		r.Players = append(r.Players, tp)
		add := func(zone string, cvs []view.CardView) {
			for _, cv := range cvs {
				o := g.e.G.Obj(cv.ID)
				to := TruthObject{ObjectID: g.objectID(seat, cv.ID, ""), Zone: zone, Name: cv.Name,
					Controller: seatOf(cv.Controller), Owner: seatOf(cv.Owner), FaceDown: cv.FaceDown}
				if zone == "battlefield" {
					to.Tapped, to.Damage, to.SummonSick, to.Attacking = cv.Tapped, cv.Damage, cv.SummonSick, cv.Attacking
					if o != nil && g.e.IsCreature(cv.ID) {
						pw, tg := cv.Power, cv.Toughness
						to.Power, to.Toughness = &pw, &tg
					}
					if len(cv.Counters) > 0 {
						to.Counters = map[string]int32{}
						for k, n := range cv.Counters {
							to.Counters[counterName(k)] += n
						}
					}
					for _, k := range cv.Keywords {
						if n := keywordName(k); n != "" {
							to.Keywords = append(to.Keywords, n)
						}
					}
					sort.Strings(to.Keywords)
				}
				if o != nil {
					to.Token = o.IsToken
				}
				r.Objects = append(r.Objects, to)
			}
		}
		add("battlefield", pv.Battlefield)
		add("graveyard", pv.Graveyard)
		add("exile", pv.Exile)
		if pv.ID == seat {
			add("hand", pv.Hand)
		}
	}
	for _, sv := range v.Stack {
		r.Objects = append(r.Objects, TruthObject{ObjectID: g.objectID(seat, sv.ID, ""), Zone: "stack", Name: sv.Name,
			Controller: seatOf(sv.Controller), Owner: seatOf(sv.Controller)})
	}
	count := func(z state.Zone, p state.PlayerID) map[string]int {
		m := map[string]int{}
		for _, o := range g.cards(z, p) {
			m[fullName(o.Card)]++
		}
		return m
	}
	r.OwnLibrary = count(state.ZLibrary, seat)
	r.OppHand = count(state.ZHand, 1-seat)
	r.OppLibrary = count(state.ZLibrary, 1-seat)
	line, err := json.Marshal(r)
	if err != nil {
		return
	}
	g.srv.opts.Truth.Write(append(line, '\n'))
}
