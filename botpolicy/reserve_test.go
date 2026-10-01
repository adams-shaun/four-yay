package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestB2ReserveIsCheapestInstantSpeedCard pins reserve() (the B2 mana
// reserve): it is the converted cost of the cheapest castable card the seat
// holds that can be cast at instant speed -- an Instant, or a permanent with
// Flash -- and 0 when it holds none. It is evidence-driven: it reads what is
// actually in hand (Castable), what it costs (CMC) and whether it is
// instant-speed (InstantSpeed). The gate makes reserve() a fixed constant
// (or reads the expensive card) and this test fails.
func TestB2ReserveIsCheapestInstantSpeedCard(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		1: {CMC: 3, Castable: true, InstantSpeed: true}, // a 3-cost instant
		2: {CMC: 1, Castable: true, InstantSpeed: true}, // a 1-cost flash creature
		3: {CMC: 2, Castable: true},                     // a sorcery: not instant speed
		4: {CMC: 4, InstantSpeed: true},                 // a battlefield permanent: not castable
		5: {CMC: 2, Castable: true, InstantSpeed: true}, // a graveyard flashback, castable
	})}
	if got := b.reserve(); got != 1 {
		t.Fatalf("reserve() = %d, want the cheapest instant-speed castable card (1, the flash creature)", got)
	}
	// No instant-speed castable card: reserve 0, so C7 is inert.
	b = Board{Cards: TableOf(map[state.ObjID]Card{3: {CMC: 2, Castable: true}})}
	if got := b.reserve(); got != 0 {
		t.Fatalf("reserve() with no instant-speed card = %d, want 0", got)
	}
}

// TestB2ReservePrefersKeepingCast pins C7 (cast.go chooseCast): the reserve
// is a PREFERENCE for a cast that keeps the mana pool at or above the cost
// of the cheapest instant-speed card in hand over a comparable cast that
// empties it. With a 1-cost spell, a 3-cost spell, a 1-cost instant in hand
// (reserve 1) and a pool of 3, the 1-cost cast leaves the pool at 2 (>= the
// reserve) while the 3-cost cast empties it; the reserve preference makes
// the 1-cost cast win even though the 3-cost one is otherwise the bigger
// spell. The gate deletes the reserve preference and this test fails: the
// draining 3-cost cast is chosen and the pool hits zero.
func TestB2ReservePrefersKeepingCast(t *testing.T) {
	b := Board{IsMain: true,
		Pool: state.Mana{state.MC: 3},
		Cards: TableOf(map[state.ObjID]Card{
			1: {CMC: 1, Castable: true},                     // a 1-cost spell: leaves pool 2 >= reserve 1
			3: {CMC: 3, Castable: true},                     // a 3-cost spell: empties the pool
			2: {CMC: 1, Castable: true, InstantSpeed: true}, // the reserve (a 1-cost instant)
		}),
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "cast", Obj: 3},
			{Index: 1, Kind: "cast", Obj: 1},
			{Index: 2, Kind: "pass"},
		}}
	if got := b.chooseCast(d); got != 1 {
		t.Fatalf("chooseCast = option %d, want option 1 (the 1-cost cast that keeps the reserve) over option 0 (the draining 3-cost cast)", got)
	}
}

// TestB2ReserveNeverSuppressesBestPlay is C7's negative half: the preference
// must not make the bot refuse a clearly better cast to hoard mana. A
// 4-power creature (a real threat) is cast even though it empties the pool
// and even though the seat holds an instant to reserve mana for. The gate
// turns the reserve preference into a hard block and this test fails: the
// creature is refused and the bot sits on mana.
func TestB2ReserveNeverSuppressesBestPlay(t *testing.T) {
	b := Board{IsMain: true,
		Pool: state.Mana{state.MC: 4},
		Cards: TableOf(map[state.ObjID]Card{
			1: {CMC: 4, Creature: true, Power: 4, Castable: true}, // a real threat, empties the pool
			2: {CMC: 1, Castable: true, InstantSpeed: true},       // the reserve
		}),
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "cast", Obj: 1},
			{Index: 1, Kind: "pass"},
		}}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("chooseCast = option %d, want 0 — the 4-power creature is cast even though it spends the reserve", got)
	}
}

// TestB2ReserveAllowsCastWithSurplus is C7's positive half: the reserve only
// stops a cast that would dip INTO it; a cast the pool can pay WHILE still
// holding the reserve is fine. A pool of 4, a 3-cost creature and a 1-cost
// instant (reserve 1) leave pool 1 after the cast, so the creature is cast.
// The gate makes C7 block every cast while a reserve exists and this test
// fails.
func TestB2ReserveAllowsCastWithSurplus(t *testing.T) {
	b := Board{IsMain: true,
		Pool: state.Mana{state.MC: 4},
		Cards: TableOf(map[state.ObjID]Card{
			1: {CMC: 3, Creature: true, Power: 2, Castable: true},
			2: {CMC: 1, Castable: true, InstantSpeed: true},
		}),
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "cast", Obj: 1},
			{Index: 1, Kind: "pass"},
		}}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("chooseCast = option %d, want 0 — a cast that keeps the reserve (pool 4 - 3 = 1 >= reserve 1) is allowed", got)
	}
}

// TestB2ReserveInertWithoutInstant pins C7's "not a magic constant" half: a
// seat with no instant-speed card in hand reserves nothing (reserve 0), so it
// casts exactly as it did before — an otherwise-unused 3-cost creature from a
// pool of exactly 3 is cast even though that empties the pool. The gate makes
// reserve() return a constant and this test fails: a deck holding no instant
// is forced to hoard.
func TestB2ReserveInertWithoutInstant(t *testing.T) {
	b := Board{IsMain: true,
		Pool: state.Mana{state.MC: 3},
		Cards: TableOf(map[state.ObjID]Card{
			1: {CMC: 3, Creature: true, Power: 2, Castable: true},
		}),
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "cast", Obj: 1},
			{Index: 1, Kind: "pass"},
		}}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("chooseCast = option %d, want 0 — with no instant in hand the reserve is 0 and the 3-cost creature is cast", got)
	}
}

// TestB2ReserveKeepsBestCastableExemptForCommander pins C7's carve-out
// (cast.go chooseCast): the reserve never refuses the seat's commander cast
// from the command zone, whatever the reserve -- a deck built around its
// commander must cast it, and holding mana forever for an instant is not the
// point. The gate applies the reserve to the command-zone cast too and this
// test fails: the commander is suppressed and never cast.
func TestB2ReserveKeepsBestCastableExemptForCommander(t *testing.T) {
	b := Board{IsMain: true,
		Pool:       state.Mana{state.MC: 3},
		Commanders: TableOf(map[state.ObjID]Commander{9: {Casts: 0, InCommandZone: true}}),
		Cards: TableOf(map[state.ObjID]Card{
			9: {CMC: 3, Creature: true, Power: 3, Castable: true}, // the commander, from the command zone
			2: {CMC: 1, Castable: true, InstantSpeed: true},       // the reserve (a 1-cost instant in hand)
		}),
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "cast", Obj: 9},
			{Index: 1, Kind: "pass"},
		}}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("chooseCast = option %d, want 0 -- the command-zone commander is cast even though it spends the reserve", got)
	}
}
