package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The Q3b part-bound rows (design
// docs/superpowers/specs/2026-10-08-quiet-seat-walk-skip-design.md, §6 Q3b):
// one satisfiable and one unsatisfiable row per bounded part kind. A
// satisfiable row asserts the ability's group exists on the face and the
// proof blocks with qbBattlefieldAbility; its unsatisfiable sibling differs
// ONLY in the ingredient the part condition reads, and asserts the proof is
// quiet (qbNone). The pair makes the bound itself the thing under test: a
// bound that regressed to nonMana (blocked outright) fails the unsatisfiable
// row, and a bound that mis-prices the part fails one of the two via
// quietContract's walk cross-check.
//
// Where the seat's mana can actually pay the ability (floor 0, or the base
// fixture's single untapped Plains at {1}), the satisfiable row also asserts
// the walk offers the activate and the unsatisfiable row asserts it does
// not, so the rows prove the walk agrees about payability in both
// directions.

func quietGroupWithPart(t *testing.T, e *Engine, o *state.Object, kind quietPartKind) *quietBoundedAbility {
	t.Helper()
	if o == nil || o.Face() == nil {
		t.Fatal("precondition: object has no face")
	}
	ff := e.walkFaceFactsOf(o.Face())
	if ff == nil {
		t.Fatal("precondition: no face facts for the object")
	}
	aq := ff.quiet.abQuiet[quietZoneIndex(state.ZBattlefield)]
	if !aq.any {
		t.Fatal("precondition: the battlefield ability summary is empty")
	}
	if aq.nonMana {
		t.Fatal("precondition: the ability summary is nonMana; the Q3b bound did not price the cost")
	}
	for i := range aq.groups {
		g := &aq.groups[i]
		for j := uint8(0); j < g.nParts; j++ {
			if g.parts[j].kind == kind {
				return g
			}
		}
	}
	t.Fatalf("precondition: no %v part group on the face; the Q3b bound did not store the part", kind)
	return nil
}

// quietActivateFor returns the walk's non-mana ability offer for id, or nil
// (the walk spells those options Kind "ability"; mana taps are Kind
// "activate").
func quietActivateFor(opts []decision.Option, id state.ObjID) *decision.Option {
	for i := range opts {
		if opts[i].Obj == id && (opts[i].Kind == "ability" || opts[i].Kind == "activate") {
			return &opts[i]
		}
	}
	return nil
}

func TestQuietPartBounds(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	type tc struct {
		name string
		// card under test, seated on seat 0's battlefield by build.
		build func(t *testing.T) (*Engine, state.ObjID)
		// want is the expected blocker; qbNone rows must come out quiet.
		want quietBlockerID
		// checkPart names the part kind whose group must be on the face.
		checkPart quietPartKind
		// offerManaFeasible marks a satisfiable row whose ability the walk
		// must actually offer (floor 0 or payable at the base ceiling) and
		// an unsatisfiable row whose ability the walk must NOT offer.
		offerManaFeasible bool
	}
	rows := []tc{
		{
			// pqSac, satisfiable: Witch's Cauldron's {1}{B}{T},
			// Sac<1/Creature> cost (the artifact cannot sacrifice itself,
			// so the part reads the battlefield's creatures) against one
			// creature, with the pool funding the {1}{B} so the mana floor
			// is not the reason anything blocks. The walk offers the
			// ability; the part is what decides.
			name: "Sac part satisfiable: sac a creature blocks",
			build: func(t *testing.T) (*Engine, state.ObjID) {
				cauldron := lookup(t, reg, "Witch's Cauldron")
				bears := lookup(t, reg, "Grizzly Bears")
				e := quietBaseLandWith(t, reg, "Swamp", []*cards.Card{cauldron, bears})
				e.G.Players[0].Pool[state.MB] = 1
				e.G.Players[0].Pool[state.MC] = 1
				id := addZone(t, e, 0, bears, state.ZBattlefield)
				if o := e.G.Obj(id); o == nil || !o.Face().IsCreature() {
					t.Fatalf("precondition: the sac candidate is not a creature: %+v", o)
				}
				return e, addZone(t, e, 0, cauldron, state.ZBattlefield)
			},
			want:              qbBattlefieldAbility,
			checkPart:         pqSac,
			offerManaFeasible: true,
		},
		{
			// pqSac, unsatisfiable: the same funded board with NO creature
			// -- the part cannot be paid, the walk refuses it, and the
			// proof is quiet.
			name: "Sac part unsatisfiable: no creature is quiet",
			build: func(t *testing.T) (*Engine, state.ObjID) {
				cauldron := lookup(t, reg, "Witch's Cauldron")
				e := quietBaseLandWith(t, reg, "Swamp", []*cards.Card{cauldron})
				e.G.Players[0].Pool[state.MB] = 1
				e.G.Players[0].Pool[state.MC] = 1
				for _, id := range append([]state.ObjID{}, e.G.Zone(state.ZBattlefield, 0)...) {
					if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().IsCreature() {
						t.Fatalf("precondition: %s is a creature; the row needs an empty battlefield of them", o.Face().Name)
					}
				}
				return e, addZone(t, e, 0, cauldron, state.ZBattlefield)
			},
			want:              qbNone,
			checkPart:         pqSac,
			offerManaFeasible: true,
		},
		{
			// pqDiscard, satisfiable: Key to the City's {T}, Discard<1/Card>
			// -- floor 0, so the walk offers the ability when the hand holds
			// a card.
			name: "Discard part satisfiable: a hand card blocks",
			build: func(t *testing.T) (*Engine, state.ObjID) {
				key := lookup(t, reg, "Key to the City")
				bears := lookup(t, reg, "Grizzly Bears")
				e := quietBaseWith(t, reg, []*cards.Card{key, bears})
				addHand(t, e, 0, bears)
				return e, addZone(t, e, 0, key, state.ZBattlefield)
			},
			want:              qbBattlefieldAbility,
			checkPart:         pqDiscard,
			offerManaFeasible: true,
		},
		{
			// pqDiscard, unsatisfiable: the base fixture empties seat 0's
			// hand, so the part cannot be paid and the proof is quiet.
			name: "Discard part unsatisfiable: empty hand is quiet",
			build: func(t *testing.T) (*Engine, state.ObjID) {
				key := lookup(t, reg, "Key to the City")
				e := quietBaseWith(t, reg, []*cards.Card{key})
				if len(e.G.Zone(state.ZHand, 0)) != 0 {
					t.Fatalf("precondition: seat 0's hand is not empty: %d cards", len(e.G.Zone(state.ZHand, 0)))
				}
				return e, addZone(t, e, 0, key, state.ZBattlefield)
			},
			want:              qbNone,
			checkPart:         pqDiscard,
			offerManaFeasible: true,
		},
		{
			// pqSubCounter, satisfiable: Barkhide Troll's {1}, remove a
			// +1/+1 counter -- it enters with one (K:etbCounter), so the
			// part reads true, and the funded pool pays the {1}.
			name: "SubCounter part satisfiable: the counter is present",
			build: func(t *testing.T) (*Engine, state.ObjID) {
				troll := lookup(t, reg, "Barkhide Troll")
				e := quietBaseWith(t, reg, []*cards.Card{troll})
				e.G.Players[0].Pool[state.MC] = 1
				id := addZone(t, e, 0, troll, state.ZBattlefield)
				if n := e.G.Obj(id).Counter("P1P1"); n < 1 {
					t.Fatalf("precondition: Barkhide Troll entered with %d +1/+1 counters", n)
				}
				return e, id
			},
			want:              qbBattlefieldAbility,
			checkPart:         pqSubCounter,
			offerManaFeasible: true,
		},
		{
			// pqSubCounter, unsatisfiable: the same troll with its counter
			// removed -- the part reads false and the proof is quiet.
			name: "SubCounter part unsatisfiable: no counter is quiet",
			build: func(t *testing.T) (*Engine, state.ObjID) {
				troll := lookup(t, reg, "Barkhide Troll")
				e := quietBaseWith(t, reg, []*cards.Card{troll})
				e.G.Players[0].Pool[state.MC] = 1
				id := addZone(t, e, 0, troll, state.ZBattlefield)
				e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: -1})
				if n := e.G.Obj(id).Counter("P1P1"); n != 0 {
					t.Fatalf("precondition: the counter removal left %d counters", n)
				}
				return e, id
			},
			want:              qbNone,
			checkPart:         pqSubCounter,
			offerManaFeasible: true,
		},
		{
			// pqLife, satisfiable: Vona's {T}, PayLife<7> against 20 life.
			// The walk offers it only at a window where the {T} is legal
			// (Vona ETB'd this turn and is summoning sick), so this row
			// proves the bound through the part and the blocker, not
			// through a walk offer.
			name: "Life part satisfiable: enough life blocks",
			build: func(t *testing.T) (*Engine, state.ObjID) {
				vona := lookup(t, reg, "Vona, Butcher of Magan")
				e := quietBaseWith(t, reg, []*cards.Card{vona})
				if life := e.G.Players[0].Life; life < 7 {
					t.Fatalf("precondition: seat 0 has %d life; the row needs at least 7", life)
				}
				return e, addZone(t, e, 0, vona, state.ZBattlefield)
			},
			want:      qbBattlefieldAbility,
			checkPart: pqLife,
		},
		{
			// pqLife, unsatisfiable: the same Vona at 6 life -- below the
			// part's 7, so ResolveManaWith's own life gate would refuse and
			// the proof is quiet.
			name: "Life part unsatisfiable: too little life is quiet",
			build: func(t *testing.T) (*Engine, state.ObjID) {
				vona := lookup(t, reg, "Vona, Butcher of Magan")
				e := quietBaseWith(t, reg, []*cards.Card{vona})
				id := addZone(t, e, 0, vona, state.ZBattlefield)
				for i := 0; i < 14; i++ {
					e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -1})
				}
				if life := e.G.Players[0].Life; life >= 7 {
					t.Fatalf("precondition: seat 0 has %d life; the row needs fewer than 7", life)
				}
				return e, id
			},
			want:      qbNone,
			checkPart: pqLife,
		},
		{
			// pqExileGrave, satisfiable: Bearscape's {1}{G}, exile two cards
			// from the graveyard (an enchantment, so no summoning-sickness
			// gate), with the pool funding the {1}{G} over a Forest.
			name: "Exile-graveyard part satisfiable: cards in the graveyard block",
			build: func(t *testing.T) (*Engine, state.ObjID) {
				escape := lookup(t, reg, "Bearscape")
				bears := lookup(t, reg, "Grizzly Bears")
				e := quietBaseLandWith(t, reg, "Forest", []*cards.Card{escape, bears, bears})
				e.G.Players[0].Pool[state.MG] = 1
				e.G.Players[0].Pool[state.MC] = 1
				addZone(t, e, 0, bears, state.ZGraveyard)
				addZone(t, e, 0, bears, state.ZGraveyard)
				if n := len(e.G.Zone(state.ZGraveyard, 0)); n < 2 {
					t.Fatalf("precondition: seat 0's graveyard holds %d cards; the row needs two", n)
				}
				return e, addZone(t, e, 0, escape, state.ZBattlefield)
			},
			want:              qbBattlefieldAbility,
			checkPart:         pqExileGrave,
			offerManaFeasible: true,
		},
		{
			// pqExileGrave, unsatisfiable: the funded board with an empty
			// graveyard, so the part reads false and the proof is quiet.
			name: "Exile-graveyard part unsatisfiable: empty graveyard is quiet",
			build: func(t *testing.T) (*Engine, state.ObjID) {
				escape := lookup(t, reg, "Bearscape")
				e := quietBaseLandWith(t, reg, "Forest", []*cards.Card{escape})
				e.G.Players[0].Pool[state.MG] = 1
				e.G.Players[0].Pool[state.MC] = 1
				if n := len(e.G.Zone(state.ZGraveyard, 0)); n != 0 {
					t.Fatalf("precondition: seat 0's graveyard holds %d cards; the row needs it empty", n)
				}
				return e, addZone(t, e, 0, escape, state.ZBattlefield)
			},
			want:              qbNone,
			checkPart:         pqExileGrave,
			offerManaFeasible: true,
		},
		{
			// pqTapPermanent, satisfiable: Bramblesnap's tapXType<1/Creature>
			// -- it may tap ITSELF (the spec has no .Other), so the untapped
			// troll satisfies the part.
			name: "TapPermanent part satisfiable: an untapped creature blocks",
			build: func(t *testing.T) (*Engine, state.ObjID) {
				snap := lookup(t, reg, "Bramblesnap")
				e := quietBaseWith(t, reg, []*cards.Card{snap})
				id := addZone(t, e, 0, snap, state.ZBattlefield)
				if o := e.G.Obj(id); o == nil || o.Tapped || !o.Face().IsCreature() {
					t.Fatalf("precondition: Bramblesnap is not an untapped creature: %+v", o)
				}
				return e, id
			},
			want:              qbBattlefieldAbility,
			checkPart:         pqTapPermanent,
			offerManaFeasible: true,
		},
		{
			// pqTapPermanent, unsatisfiable: the same troll tapped -- no
			// untapped creature remains, so the part reads false and the
			// proof is quiet.
			name: "TapPermanent part unsatisfiable: no untapped creature is quiet",
			build: func(t *testing.T) (*Engine, state.ObjID) {
				snap := lookup(t, reg, "Bramblesnap")
				e := quietBaseWith(t, reg, []*cards.Card{snap})
				id := addZone(t, e, 0, snap, state.ZBattlefield)
				e.emit(events.Event{Kind: events.Tap, Obj: id})
				if o := e.G.Obj(id); o == nil || !o.Tapped {
					t.Fatalf("precondition: the tap event left Bramblesnap untapped: %+v", o)
				}
				for _, oid := range e.G.Zone(state.ZBattlefield, 0) {
					if o := e.G.Obj(oid); o != nil && !o.Tapped && o.Face() != nil && o.Face().IsCreature() {
						t.Fatalf("precondition: %s is an untapped creature; the row needs none", o.Face().Name)
					}
				}
				return e, id
			},
			want:              qbNone,
			checkPart:         pqTapPermanent,
			offerManaFeasible: true,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			e, id := row.build(t)
			// Precondition: the Q3b bound stored the part as a group. A
			// regression to nonMana would make both rows of the pair block
			// (the satisfiable one vacuously); this fails loudly instead.
			quietGroupWithPart(t, e, e.G.Obj(id), row.checkPart)
			opts, quiet, _ := quietContract(t, e, 0)
			if got := e.quietBlocker(0); got != row.want {
				t.Fatalf("quietBlocker = %s, want %s\nturn %d step %v options: %v",
					quietBlockerNames[got], quietBlockerNames[row.want], e.G.Turn, e.G.Step, optKinds(opts))
			}
			if row.want == qbNone && !quiet {
				t.Fatalf("row is meant to be quiet but the proof blocked")
			}
			if row.want != qbNone && quiet {
				t.Fatalf("row is meant to block with %s but the proof was quiet", quietBlockerNames[row.want])
			}
			if !row.offerManaFeasible {
				return
			}
			// Where the seat's mana can pay the ability, the walk must agree
			// with the part bound in BOTH directions: offered exactly when
			// the part could be paid.
			offered := quietActivateFor(opts, id) != nil
			if row.want != qbNone && !offered {
				t.Fatalf("the walk did not offer the ability the part bound prices as payable: %v", optKinds(opts))
			}
			if row.want == qbNone && offered {
				t.Fatalf("the walk offered the ability the part bound prices as unpayable: %v", optKinds(opts))
			}
		})
	}
}
