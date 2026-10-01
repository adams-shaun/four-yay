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

// TestEventSignatureMatchesByteFormulation pins the register-local FNV fold
// to the byte-slice one, including the damage-source and mint suffixes.
func TestEventSignatureMatchesByteFormulation(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 5000; i++ {
		ev := events.Event{Kind: events.Kind(rng.IntN(int(events.NumKinds))), Player: state.PlayerID(rng.IntN(8)),
			From: state.Zone(rng.IntN(10)), To: state.Zone(rng.IntN(10)), Step: state.Step(rng.IntN(14)),
			Secret: rng.IntN(2) == 0, Obj: state.ObjID(rng.Uint32()), Counter: []string{"", "P1P1", "TIME", "infect"}[rng.IntN(4)]}
		for k := rng.IntN(4); k > 0; k-- {
			ev.IDs = append(ev.IDs, state.ObjID(rng.Uint32()))
		}
		for k := rng.IntN(3); k > 0; k-- {
			ev.Pairs = append(ev.Pairs, [2]state.ObjID{state.ObjID(rng.Uint32()), state.ObjID(rng.Uint32())})
		}
		src := state.ObjID(0)
		if rng.IntN(2) == 0 {
			src = state.ObjID(1 + rng.Uint32N(1000))
		}
		mint := uint64(0)
		if rng.IntN(2) == 0 {
			mint = 1 + rng.Uint64()>>1
		}
		got := eventSignature(&ev)
		if src != 0 {
			got = fnvU32(got, uint32(src))
		}
		if mint != 0 {
			got = fnvU64(got, mint)
		}
		if want := refEventSignature(ev, src, mint); got != want {
			t.Fatalf("event %+v (src %d, mint %d): signature %x, byte formulation %x", ev, src, mint, got, want)
		}
	}
}
