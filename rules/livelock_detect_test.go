package rules

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestLivelockDetectMatchesBruteForce holds detect's candidate index (the
// same-slot chain, the inline second-signature test) to the definition: the
// shortest period p <= min(MaxPeriod, n/2) whose trailing 2p signatures are
// two identical halves. Random streams over tiny alphabets (so periods and
// slot collisions are common) are pushed past the ring's capacity, and every
// prefix is checked, filling and wrapped.
func TestLivelockDetectMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := 0; trial < 400; trial++ {
		maxPeriod := 1 + rng.IntN(12)
		w := livelockWatcher{guard: LoopGuard{MaxPeriod: maxPeriod, CycleEvents: 1 << 30}}
		alpha := uint64(1 + rng.IntN(3))
		var hist []uint64
		for step := 0; step < 6*maxPeriod+10; step++ {
			// Multiples of livelockCandSlots collide in the index on purpose.
			sig := rng.Uint64N(alpha) * livelockCandSlots
			if rng.IntN(4) == 0 {
				sig++
			}
			w.pushSig(sig)
			hist = append(hist, sig)
			win := hist
			if c := 2 * maxPeriod; len(win) > c {
				win = win[len(win)-c:]
			}
			n := len(win)
			want := 0
			for p := 1; p <= min(maxPeriod, n/2) && want == 0; p++ {
				ok := true
				for j := n - 1; j >= n-p; j-- {
					if win[j] != win[j-p] {
						ok = false
						break
					}
				}
				if ok {
					want = p
				}
			}
			if got := w.detectIndexed(); got != want {
				t.Fatalf("trial %d step %d: window %v: indexed detect %d, want %d", trial, step, win, got, want)
			}
			if got := w.detectScan(); got != want {
				t.Fatalf("trial %d step %d: window %v: scan %d, want %d", trial, step, win, got, want)
			}
		}
	}
}

// refEventSignature is the byte-slice FNV-1a formulation eventSignature
// replaced, kept as the reference its values must equal.
func refEventSignature(ev events.Event, damageSource state.ObjID, mint uint64) uint64 {
	h := uint64(14695981039346656037)
	put := func(b ...byte) {
		for _, c := range b {
			h ^= uint64(c)
			h *= 1099511628211
		}
	}
	u4 := func(v uint32) {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], v)
		put(b[:]...)
	}
	put(byte(ev.Kind), byte(ev.Player), byte(ev.From), byte(ev.To), byte(ev.Step))
	if ev.Secret {
		put(1)
	} else {
		put(0)
	}
	u4(uint32(ev.Obj))
	u4(uint32(len(ev.Counter)))
	put([]byte(ev.Counter)...)
	u4(uint32(len(ev.IDs)))
	for _, id := range ev.IDs {
		u4(uint32(id))
	}
	u4(uint32(len(ev.Pairs)))
	for _, pr := range ev.Pairs {
		u4(uint32(pr[0]))
		u4(uint32(pr[1]))
	}
	if damageSource != 0 {
		u4(uint32(damageSource))
	}
	if mint != 0 {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], mint)
		put(b[:]...)
	}
	return h
}

// newSignature is the watcher's signature with its damage-source and mint
// suffixes, exactly as observeFrom composes it.
func newSignature(ev *events.Event, damageSource state.ObjID, mint uint64) uint64 {
	sig := eventSignature(ev)
	if damageSource != 0 {
		sig = sigMix(sig, uint64(damageSource)|livelockDamageTag)
	}
	if mint != 0 {
		sig = sigMix(sigMix(sig, livelockMintTag), mint)
	}
	return sig
}

// TestEventSignatureEquivalence holds the word-packed signature to the
// byte-wise FNV-1a formulation it replaced: over a small field alphabet (so
// equal and near-equal events are common), two events share a new
// signature exactly when they shared the old one. The watcher only ever
// compares signatures for equality, so equal equality classes mean equal
// verdicts.
func TestEventSignatureEquivalence(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	type rec struct{ old, new uint64 }
	var recs []rec
	counters := []string{"", "P1P1", "TIME", "infect", "LOYALTY", "M1M1x", "abcdefgh", "abcdefghi"}
	for i := 0; i < 3000; i++ {
		ev := events.Event{Kind: events.Kind(rng.IntN(3)), Player: state.PlayerID(rng.IntN(2)),
			From: state.Zone(rng.IntN(2)), To: state.Zone(rng.IntN(2)), Step: state.Step(rng.IntN(2)),
			Secret: rng.IntN(2) == 0, Obj: state.ObjID(rng.IntN(3)), Counter: counters[rng.IntN(len(counters))],
			Amount: int32(rng.IntN(5)), Seq: uint64(i), Text: []string{"", "x"}[rng.IntN(2)]}
		for k := rng.IntN(4); k > 0; k-- {
			ev.IDs = append(ev.IDs, state.ObjID(rng.IntN(2)))
		}
		for k := rng.IntN(3); k > 0; k-- {
			ev.Pairs = append(ev.Pairs, [2]state.ObjID{state.ObjID(rng.IntN(2)), state.ObjID(rng.IntN(2))})
		}
		src := state.ObjID(0)
		if rng.IntN(2) == 0 {
			src = state.ObjID(1 + rng.IntN(2))
		}
		mint := uint64(0)
		if rng.IntN(2) == 0 {
			mint = 1 + uint64(rng.IntN(2))
		}
		recs = append(recs, rec{refEventSignature(ev, src, mint), newSignature(&ev, src, mint)})
	}
	equalOld := 0
	for i := range recs {
		for j := i + 1; j < len(recs); j++ {
			eo, en := recs[i].old == recs[j].old, recs[i].new == recs[j].new
			if eo != en {
				t.Fatalf("events %d and %d: old signatures equal=%v, new equal=%v", i, j, eo, en)
			}
			if eo {
				equalOld++
			}
		}
	}
	if equalOld == 0 {
		t.Fatal("alphabet too large: no equal signature pairs exercised")
	}
	// A long counter and a long ID list take the overflow path.
	long := events.Event{Counter: string(make([]byte, 1<<16)), IDs: make([]state.ObjID, 3)}
	short := long
	short.Counter = ""
	if eventSignature(&long) == eventSignature(&short) {
		t.Fatal("overflow length not folded")
	}
}
