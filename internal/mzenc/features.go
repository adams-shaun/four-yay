// Package mzenc is a byte-identical Go port of MageZero v0.2's feature encoder
// (XMage fork WillWroble/mage @ master cb7e9c6f, package mage.player.ai.encoder).
//
// This file ports the hash core: Features.mix64/hash64/indexFor. See
// docs/superpowers/specs/2026-10-07-mzenc-design.md and
// docs/superpowers/plans/2026-10-07-mzenc-hash-port.md.
package mzenc

import (
	"encoding/binary"
	"math/bits"
)

const (
	// globalSeed is the bit pattern of Java's 0x9E3779B185EBCA87L, which is a
	// negative long (2^64/phi). As uint64 it is exactly that pattern.
	globalSeed   uint64 = 0x9E3779B185EBCA87
	defaultTable int64  = 2_147_483_647 // Features.TABLE_SIZE = Integer.MAX_VALUE (v0.2)
)

var numericBreakpoints = [...]int{32, 64, 128, 256, 512}

func mix64(z uint64) uint64 {
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func hash64(s string, seed uint64) uint64 {
	data := []byte(s)
	h := mix64(seed ^ (uint64(len(data)) * 0x9E3779B185EBCA87))
	for len(data) >= 8 {
		k := binary.LittleEndian.Uint64(data)
		h ^= mix64(k)
		h = bits.RotateLeft64(h, 27)*0x9E3779B185EBCA87 + 0x165667B19E3779F9
		data = data[8:]
	}
	var k uint64
	for i, b := range data {
		k ^= uint64(b) << (8 * i)
	}
	h ^= mix64(k)
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h
}

// indexFor mirrors Features.indexFor exactly: the Java `if (h<0) h=-h` long
// overflow when h is math.MinInt64, and the sign of Java's `%` operator. Both
// Go and Java truncate toward zero and both wrap `-MinInt64` to MinInt64, so
// the same expression reproduces the same lattice, negative results included.
func indexFor(h uint64, table int64) int32 {
	sh := int64(h)
	if sh < 0 {
		sh = -sh
	}
	return int32(sh % table)
}
