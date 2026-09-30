package searchbench

// pyRandom is CPython's random.Random: MT19937 seeded by init_by_array from
// the seed's 32-bit words, with choice() drawn through _randbelow's
// getrandbits rejection loop. The bootstrap uses it so that, given the same
// cluster order and seed, it draws exactly the resamples upstream's
// analyze.py (random.Random(seed), rng.choice(games)) draws. That makes the
// Go analysis checkable against upstream's code on the same data, and keeps
// it deterministic without ambient randomness.
type pyRandom struct {
	mt  [624]uint32
	idx int
}

func newPyRandom(seed uint64) *pyRandom {
	key := []uint32{uint32(seed)}
	if hi := uint32(seed >> 32); hi != 0 {
		key = append(key, hi)
	}
	r := &pyRandom{}
	r.initGenrand(19650218)
	i, j := 1, 0
	k := len(r.mt)
	if len(key) > k {
		k = len(key)
	}
	for ; k > 0; k-- {
		r.mt[i] = (r.mt[i] ^ ((r.mt[i-1] ^ (r.mt[i-1] >> 30)) * 1664525)) + key[j] + uint32(j)
		i++
		j++
		if i >= len(r.mt) {
			r.mt[0] = r.mt[len(r.mt)-1]
			i = 1
		}
		if j >= len(key) {
			j = 0
		}
	}
	for k = len(r.mt) - 1; k > 0; k-- {
		r.mt[i] = (r.mt[i] ^ ((r.mt[i-1] ^ (r.mt[i-1] >> 30)) * 1566083941)) - uint32(i)
		i++
		if i >= len(r.mt) {
			r.mt[0] = r.mt[len(r.mt)-1]
			i = 1
		}
	}
	r.mt[0] = 0x80000000
	r.idx = len(r.mt)
	return r
}

func (r *pyRandom) initGenrand(s uint32) {
	r.mt[0] = s
	for i := 1; i < len(r.mt); i++ {
		r.mt[i] = 1812433253*(r.mt[i-1]^(r.mt[i-1]>>30)) + uint32(i)
	}
}

func (r *pyRandom) uint32() uint32 {
	const n, m = 624, 397
	if r.idx >= n {
		for kk := 0; kk < n; kk++ {
			y := (r.mt[kk] & 0x80000000) | (r.mt[(kk+1)%n] & 0x7fffffff)
			v := r.mt[(kk+m)%n] ^ (y >> 1)
			if y&1 != 0 {
				v ^= 0x9908b0df
			}
			r.mt[kk] = v
		}
		r.idx = 0
	}
	y := r.mt[r.idx]
	r.idx++
	y ^= y >> 11
	y ^= (y << 7) & 0x9d2c5680
	y ^= (y << 15) & 0xefc60000
	y ^= y >> 18
	return y
}

// below is random._randbelow_with_getrandbits for 0 < n < 2^32.
func (r *pyRandom) below(n int) int {
	if n <= 0 {
		return 0
	}
	k := 0
	for v := n; v > 0; v >>= 1 {
		k++
	}
	for {
		v := int(r.uint32() >> (32 - k))
		if v < n {
			return v
		}
	}
}
