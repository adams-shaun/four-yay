package mzbridge

import "math/bits"

// Port of Features.java (mage/player/ai/encoder/Features.java:23-24,169-197).

const (
	// GlobalSeed is the root namespace seed (Features.GLOBAL_SEED).
	GlobalSeed uint64 = 0x9E3779B185EBCA87
	// TableSize is the number of hash bins (Features.TABLE_SIZE,
	// Integer.MAX_VALUE); MageZero's model.GLOBAL_MAX is the same number.
	TableSize = 1<<31 - 1

	hashMul uint64 = 0x9E3779B185EBCA87
	hashAdd uint64 = 0x165667B19E3779F9
)

func mix64(z uint64) uint64 {
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// Hash64 is Features.hash64. Java hashes the string's UTF-8 bytes
// (s.getBytes(UTF_8)), which is a Go string's own byte sequence, in 8-byte
// little-endian words. The tail word is mixed in even when it is empty.
func Hash64(s string, seed uint64) uint64 {
	h := mix64(seed ^ (uint64(len(s)) * hashMul))
	i := 0
	for ; len(s)-i >= 8; i += 8 {
		k := uint64(s[i]) | uint64(s[i+1])<<8 | uint64(s[i+2])<<16 | uint64(s[i+3])<<24 |
			uint64(s[i+4])<<32 | uint64(s[i+5])<<40 | uint64(s[i+6])<<48 | uint64(s[i+7])<<56
		h ^= mix64(k)
		h = bits.RotateLeft64(h, 27)*hashMul + hashAdd
	}
	var k uint64
	for j := 0; i < len(s); i, j = i+1, j+1 {
		k ^= uint64(s[i]) << (8 * j)
	}
	h ^= mix64(k)
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h
}

// IndexFor is Features.indexFor: |h| % (2^31-1) in Java's signed arithmetic.
// Java negates a negative h, and -Long.MIN_VALUE is Long.MIN_VALUE, so that
// one input yields -2; Go's % truncates toward zero exactly as Java's does.
func IndexFor(h uint64) int32 {
	v := int64(h)
	if v < 0 {
		v = -v
	}
	return int32(v % TableSize)
}

// FeatureID is the id Features.addIndex stores for key under a namespace seed.
func FeatureID(key string, seed uint64) int32 { return IndexFor(Hash64(key, seed)) }

// JavaHashCode is java.lang.String.hashCode: s[0]*31^(n-1) + ... over UTF-16
// code units in wrapping int32 arithmetic. A rune outside the BMP contributes
// its two surrogates. Go strings cannot hold a lone surrogate, so the (Java
// only) strings that do are out of reach; invalid UTF-8 hashes as U+FFFD.
func JavaHashCode(s string) int32 {
	var h int32
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			h = 31*h + int32(0xD800+(r>>10))
			h = 31*h + int32(0xDC00+(r&0x3FF))
			continue
		}
		h = 31*h + int32(r)
	}
	return h
}

// floorMod is Math.floorMod(int, int) for a positive modulus.
func floorMod(x int32, m int) int {
	r := int(x) % m
	if r < 0 {
		r += m
	}
	return r
}
