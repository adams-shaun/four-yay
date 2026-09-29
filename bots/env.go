package bots

// RootSeed derives one honest root's deal seed from the per-seat seed and the
// decision sequence number (BP-05, spec 2026-09-28-hosted-bot-packages §5.1).
// It is the one derivation the bench and the host share: the caller passes
// the seed the seat was built from (the game seed XOR the seat index plus
// one, the SeatCtor convention) and the deciding decision's sequence number,
// and searchseat.HonestRoot redeals the live position at that seed -- so a
// root is a pure function of the seat's observation feed and the decision
// index: replayable, and unrelated between decisions.
func RootSeed(seatSeed, seq uint64) [2]uint64 {
	return [2]uint64{
		mix64(seatSeed ^ (seq + 0x243f6a8885a308d3)),
		mix64(seatSeed + (seq+1)*0x13198a2e03707344),
	}
}

// mix64 is the splitmix64 finalizer (MurmurHash3's fmix64): avalanche so
// nearby (seatSeed, seq) pairs do not produce nearby deals.
func mix64(z uint64) uint64 {
	z ^= z >> 33
	z *= 0xff51afd7ed558ccd
	z ^= z >> 33
	z *= 0xc4ceb9fe1a85ec53
	z ^= z >> 33
	return z
}
