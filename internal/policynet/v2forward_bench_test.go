// The S0 forward-cost benchmark (P§5.1 S0 readout, P§3.4): a MEASUREMENT
// harness, not the model. internal/policynet has no v2 model yet, so this
// file shapes the P§3.4 encoder skeleton — per-row MLP, per-group scaled-sum
// + max pooling, state trunk — over a REAL v2 table and measures the wall
// time of one full forward at d=32 and d=64. The readout is compared against
// the S0 kill criterion "forward > 0.5 ms at d=32 → redesign before S1".
//
// The weights are drawn from a fixed-seed PRNG (deterministic; no global
// math/rand). The table is built through front end G (V2TableFromView) from
// a mid-game view fixture written inline below, so the row count the
// arithmetic runs over is the real one (~31 rows: two seats' battlefield,
// the viewer's hand, graveyards, exile, and a two-deep stack). Only the
// shapes below cost the S0 arithmetic; no training, gradient or model
// construction is measured.
//
// Benchmarks do not run under a plain `go test`, so this file adds no
// package test time. Run:
//
//	go test -bench 'BenchmarkV2ForwardD(32|64)' -benchmem -run XXX ./internal/policynet/

package policynet

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// ---------------------------------------------------------------------------
// The view fixture: one mid-game two-seat projection, hand-written to the
// same shape the parity harness's served views carry (a seat-visibility View
// with both seats' battlefield, the viewer's hand, graveyards, exile, and a
// stack holding a spell over a triggered ability).

func v2benchView() view.View {
	p0 := view.PlayerView{
		ID: 0, Name: "bench-north", Life: 18,
		LibrarySize: 41, HandSize: 7, GraveyardSize: 3,
		Pool:      map[string]int32{"W": 1, "U": 2},
		Available: map[string]int32{"W": 2, "U": 1},
		Hand:      v2benchHand(),
		Battlefield: []view.CardView{
			v2benchCard(20, "Plains", "Basic Land", "", 0, 0),
			v2benchCard(21, "Island", "Basic Land", "", 0, 0),
			v2benchCard(22, "Mountain", "Basic Land", "", 0, 0),
			v2benchCard(23, "Grizzly Bears", "Creature", "1 G", 2, 2),
			v2benchCard(24, "Runeclaw Bear", "Creature", "1 G G", 2, 2),
			v2benchCard(25, "Serra Angel", "Creature", "3 W W", 4, 4),
			v2benchCard(26, "Trusty Machete", "Artifact Equipment", "2", 0, 0),
			v2benchCard(27, "Hill Giant", "Creature", "3 R", 3, 3),
			v2benchCard(28, "Morph", "Creature", "", 0, 0),
		},
		Graveyard: []view.CardView{
			v2benchCard(30, "Lightning Bolt", "Instant", "R", 0, 0),
			v2benchCard(31, "Core Prowler", "Creature", "3", 2, 2),
			v2benchCard(32, "Forest", "Basic Land", "", 0, 0),
		},
		Exile: []view.CardView{v2benchCard(33, "Path to Exile", "Instant", "W", 0, 0)},
	}
	p1 := view.PlayerView{
		ID: 1, Name: "bench-south", Life: 12,
		LibrarySize: 47, HandSize: 6, GraveyardSize: 2,
		Pool:      map[string]int32{},
		Available: map[string]int32{},
		Battlefield: []view.CardView{
			v2benchCard(40, "Forest", "Basic Land", "", 0, 0),
			v2benchCard(41, "Swamp", "Basic Land", "", 0, 0),
			v2benchCard(42, "Steel Leaf Golem", "Creature", "3 G G", 5, 4),
			v2benchCard(43, "Wall of Roots", "Creature", "1 G", 0, 5),
			v2benchCard(44, "Ajani, Caller of the Pride", "Planeswalker", "1 W W", 0, 0),
			v2benchCard(45, "Grizzly Bears", "Creature", "1 G", 2, 2),
			v2benchCard(46, "Gloomwidow's Feast", "Instant", "3 G", 0, 0),
		},
		Graveyard: []view.CardView{
			v2benchCard(47, "Divination", "Sorcery", "2 U", 0, 0),
			v2benchCard(48, "Swamp", "Basic Land", "", 0, 0),
		},
	}
	c := &p0.Battlefield[2]
	c.Tapped = true
	c = &p0.Battlefield[3]
	c.SummonSick = true
	c = &p0.Battlefield[4]
	c.Tapped = true
	c.Counters = map[string]int32{"P1P1": 2}
	c.Keywords = []string{"Trample"}
	c = &p0.Battlefield[5]
	c.Attacking = true
	att := state.PlayerID(1)
	c.AttackingPlayer = &att
	c.BlockedBy = []state.ObjID{45}
	c.Keywords = []string{"Flying", "Vigilance"}
	c = &p0.Battlefield[6]
	c.AttachedTo = 24
	c = &p0.Battlefield[7]
	c.Tapped = true
	c.Damage = 1
	c = &p0.Battlefield[8]
	c.FaceDown = true
	c = &p1.Battlefield[2]
	c.Tapped = true
	c = &p1.Battlefield[3]
	c.Keywords = []string{"Reach"}
	c = &p1.Battlefield[4]
	c.Counters = map[string]int32{"Loyalty": 4}
	c = &p1.Battlefield[5]
	c.IsCopy = true
	c.Token = "#45"
	c = &p1.Battlefield[0]
	c.Tapped = true
	for i := range p0.Hand {
		p0.Hand[i].Controller, p0.Hand[i].Owner = 0, 0
	}
	for i := range p0.Battlefield {
		p0.Battlefield[i].Controller, p0.Battlefield[i].Owner = 0, 0
	}
	for i := range p0.Graveyard {
		p0.Graveyard[i].Controller, p0.Graveyard[i].Owner = 0, 0
	}
	p0.Exile[0].Controller, p0.Exile[0].Owner = 0, 0
	for i := range p1.Battlefield {
		p1.Battlefield[i].Controller, p1.Battlefield[i].Owner = 1, 1
	}
	for i := range p1.Graveyard {
		p1.Graveyard[i].Controller, p1.Graveyard[i].Owner = 1, 1
	}
	return view.View{
		Viewer: 0, Visibility: "seat", Turn: 8, Round: 4,
		Step: "Main1", Phase: "main", Active: 0, Priority: 0,
		Players: []view.PlayerView{p0, p1},
		Stack: []view.StackView{
			{ID: 50, Kind: "trigger", Name: "Ramunap Excavator", Controller: 1, Source: 41, Targets: []view.TargetView{}},
			{ID: 51, Kind: "spell", Name: "Lightning Bolt", Controller: 0,
				Card:    &view.CardView{ID: 51, Name: "Lightning Bolt", Types: "Instant", ManaCost: "R", Controller: 0, Owner: 0},
				Targets: []view.TargetView{{Player: 1, IsPlayer: true}}},
		},
	}
}

func v2benchCard(id state.ObjID, name, types, cost string, power, tough int32) view.CardView {
	return view.CardView{ID: id, Name: name, Types: types, ManaCost: cost,
		Power: power, Toughness: tough}
}

func v2benchHand() []view.CardView {
	return []view.CardView{
		v2benchCard(12, "Lightning Bolt", "Instant", "R", 0, 0),
		v2benchCard(13, "Counterspell", "Instant", "U U", 0, 0),
		v2benchCard(14, "Grizzly Bears", "Creature", "1 G", 2, 2),
		v2benchCard(15, "Plains", "Basic Land", "", 0, 0),
		v2benchCard(16, "Island", "Basic Land", "", 0, 0),
		v2benchCard(17, "Doom Blade", "Instant", "1 B", 0, 0),
		v2benchCard(18, "Giant Growth", "Instant", "G", 0, 0),
	}
}

// ---------------------------------------------------------------------------
// The P§3.4 encoder skeleton, measured. Weights are drawn from a fixed-seed
// PCG at construction; the forward runs the three P§3.4 stages over a real
// V2Table and returns a scalar readout.

const v2fwdGroups = 10 // (mine/other) × (battlefield/hand/graveyard/stack/other)

type v2FwdNet struct {
	d int
	// Per-row MLP: [64 dense ‖ d-dim name embedding] → d → d.
	w1, b1 []float32 // (V2CardWidth+d)×d, d
	w2, b2 []float32 // d×d, d
	// E[name]: the hashed-name-row embedding, indexed by Feature.Row.
	emb []float32 // 65536×d
	// State trunk: [20d pooled ‖ 2×V2PlayerWidth ‖ V2GlobalWidth] → 2d → 1.
	tw1, tb1 []float32
	tw2, tb2 []float32
	// Scratch, owned by the net (one forward at a time).
	rowIn, h1, h2   []float32
	gsum, gmax, out []float32
}

func newV2FwdNet(d int) *v2FwdNet {
	rng := rand.New(rand.NewPCG(0x5342F57D, uint64(d))) // deterministic, per-width
	rowIn := V2CardWidth + d
	pooledIn := v2fwdGroups*2*d + 2*V2PlayerWidth + V2GlobalWidth
	trunkH := 2 * d
	n := &v2FwdNet{
		d:  d,
		w1: v2fwdMat(rng, rowIn, d, rowIn), b1: v2fwdVec(rng, d, 0.01),
		w2: v2fwdMat(rng, d, d, d), b2: v2fwdVec(rng, d, 0.01),
		emb: v2fwdMat(rng, 1<<16, d, d),
		tw1: v2fwdMat(rng, pooledIn, trunkH, pooledIn), tb1: v2fwdVec(rng, trunkH, 0.01),
		tw2: v2fwdMat(rng, trunkH, 1, trunkH), tb2: v2fwdVec(rng, 1, 0),
	}
	n.rowIn = make([]float32, rowIn)
	n.h1, n.h2 = make([]float32, d), make([]float32, d)
	n.gsum = make([]float32, v2fwdGroups*d)
	n.gmax = make([]float32, v2fwdGroups*d)
	n.out = make([]float32, pooledIn)
	return n
}

func v2fwdMat(rng *rand.Rand, rows, cols, fanin int) []float32 {
	m := make([]float32, rows*cols)
	s := 1 / math.Sqrt(float64(fanin))
	for i := range m {
		m[i] = float32(rng.NormFloat64()) * float32(s)
	}
	return m
}

func v2fwdVec(rng *rand.Rand, n int, s float64) []float32 {
	v := make([]float32, n)
	for i := range v {
		v[i] = float32(rng.NormFloat64()) * float32(s)
	}
	return v
}

// v2fwdZoneGroup folds the table's zones into the P§3.4 pooling groups:
// battlefield / hand / graveyard / stack / other (exile and command).
func v2fwdZoneGroup(z uint8) int {
	switch z {
	case V2ZoneBattlefield:
		return 0
	case V2ZoneHand:
		return 1
	case V2ZoneGraveyard:
		return 2
	case V2ZoneStack:
		return 3
	default:
		return 4
	}
}

func (n *v2FwdNet) forward(t *V2Table) float32 {
	d := n.d
	for i := range n.gsum {
		n.gsum[i] = 0
		n.gmax[i] = 0
	}
	counts := [v2fwdGroups]int{}
	for ri := range t.Cards {
		c := &t.Cards[ri]
		// Stage 1a: the row input [raw ‖ E[name]].
		copy(n.rowIn, c.Raw)
		eIn := n.rowIn[V2CardWidth:]
		for j := range eIn {
			eIn[j] = 0
		}
		for _, f := range c.Rows {
			e := n.emb[int(f.Row)*d : int(f.Row)*d+d]
			for j, e := range e {
				eIn[j] += e * f.Value
			}
		}
		// Stage 1b: the per-row MLP relu(W2·relu(W1·x)).
		for j := range n.h1 {
			s := n.b1[j]
			col := j
			for _, x := range n.rowIn {
				s += x * n.w1[col]
				col += d
			}
			if s > 0 {
				n.h1[j] = s
			} else {
				n.h1[j] = 0
			}
		}
		for j := range n.h2 {
			s := n.b2[j]
			col := j
			for _, x := range n.h1 {
				s += x * n.w2[col]
				col += d
			}
			if s > 0 {
				n.h2[j] = s
			} else {
				n.h2[j] = 0
			}
		}
		// Stage 3: per-group scaled-sum and max pooling. Group side is the
		// controller bit the row builder already stamped.
		side := 0
		if c.Raw[v2cControllerOther] > c.Raw[v2cControllerMine] {
			side = 1
		}
		g := side*5 + v2fwdZoneGroup(c.Zone)
		counts[g]++
		base := g * d
		for j, h := range n.h2 {
			n.gsum[base+j] += h
			if h > n.gmax[base+j] {
				n.gmax[base+j] = h
			}
		}
	}
	// Stage 4: the state trunk over [pooled ‖ player rows ‖ global].
	pooled := n.out
	for g := 0; g < v2fwdGroups; g++ {
		base := g * d
		inv := float32(1)
		if counts[g] > 1 {
			inv = 1 / float32(counts[g])
		}
		for j := 0; j < d; j++ {
			pooled[base*2+j] = n.gsum[base+j] * inv
			pooled[base*2+d+j] = n.gmax[base+j]
		}
	}
	o := v2fwdGroups * 2 * d
	for _, p := range t.Players {
		copy(pooled[o:], p.Raw)
		o += len(p.Raw)
	}
	copy(pooled[o:], t.Global.Raw)
	trunkH := 2 * d
	var read float32
	for j := 0; j < trunkH; j++ {
		s := n.tb1[j]
		col := j
		for _, x := range pooled {
			s += x * n.tw1[col]
			col += trunkH
		}
		if s < 0 {
			s = 0
		}
		read += s * n.tw2[j]
	}
	return read + n.tb2[0]
}

// v2fwdSink keeps the benchmark's readout alive against dead-code
// elimination.
var v2fwdSink float32

func benchmarkV2Forward(b *testing.B, d int) {
	b.Helper()
	g := v2benchView()
	tbl := V2TableFromView(g, 0, true, nil)
	// Precondition: the fixture produced a real mid-game table — a
	// realistic row count across the zones, both player rows, named
	// (sparse) card rows, and a finite readout. A vacuous or broken
	// fixture must stop the benchmark loudly, not measure noise.
	if len(tbl.Table.Cards) < 25 || len(tbl.Table.Cards) > 60 {
		b.Fatalf("fixture table has %d card rows, want a mid-game 25..60", len(tbl.Table.Cards))
	}
	if len(tbl.Table.Players) != 2 {
		b.Fatalf("fixture table has %d player rows, want 2", len(tbl.Table.Players))
	}
	named, zones := 0, map[uint8]int{}
	for i := range tbl.Table.Cards {
		c := &tbl.Table.Cards[i]
		if len(c.Rows) > 0 {
			named++
		}
		zones[c.Zone]++
	}
	if named < 10 {
		b.Fatalf("fixture table has only %d named card rows, want >=10", named)
	}
	for _, z := range []uint8{V2ZoneBattlefield, V2ZoneHand, V2ZoneGraveyard, V2ZoneExile, V2ZoneStack} {
		if zones[z] == 0 {
			b.Fatalf("fixture table has no %d-zone rows, want >=1", z)
		}
	}
	n := newV2FwdNet(d)
	read := n.forward(&tbl.Table)
	if math.IsNaN(float64(read)) || math.IsInf(float64(read), 0) {
		b.Fatalf("forward readout is %v, want finite", read)
	}
	b.ResetTimer()
	var sink float32
	for i := 0; i < b.N; i++ {
		sink += n.forward(&tbl.Table)
	}
	v2fwdSink = sink
}

func BenchmarkV2ForwardD32(b *testing.B) { benchmarkV2Forward(b, 32) }
func BenchmarkV2ForwardD64(b *testing.B) { benchmarkV2Forward(b, 64) }
