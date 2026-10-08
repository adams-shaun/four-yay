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
	"strconv"
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

// Encoder owns a tree and the accumulating id set, mirroring the StateEncoder
// side of Features (featureVector). UseFeatureMap wires the research table
// (Task 5); it is off by default as upstream.
type Encoder struct {
	table         int64
	root          *Node
	vec           map[int32]struct{}
	UseFeatureMap bool
	fm            *FeatureMap
}

func NewEncoder(table int64) *Encoder {
	e := &Encoder{table: table, vec: map[int32]struct{}{}, fm: NewFeatureMap()}
	e.root = &Node{
		name: "root", seed: globalSeed, enc: e,
		occurrences: map[string]int{},
		subs:        map[string]*Node{},
		idMap:       map[string]string{},
	}
	return e
}

func (e *Encoder) Root() *Node             { return e.root }
func (e *Encoder) IDs() map[int32]struct{} { return e.vec }

// FeatureMap returns the research table. It is only populated when
// UseFeatureMap was set before the ops ran.
func (e *Encoder) FeatureMap() *FeatureMap { return e.fm }

func (e *Encoder) addIndex(h uint64, key string, n *Node) {
	idx := indexFor(h, e.table)
	e.vec[idx] = struct{}{}
	if e.UseFeatureMap {
		ns := int32(-1)
		if n.parent != nil {
			ns = indexFor(hash64(n.name, n.parent.seed), e.table)
		}
		e.fm.AddFeature(key, ns, idx)
	}
}

// Node mirrors Features: a feature node with its own occurrence counts and
// sub-feature children. seed is the namespace seed (hash64(name, parent.seed)).
type Node struct {
	parent       *Node
	name         string
	seed         uint64
	passToParent bool
	occurrences  map[string]int
	subs         map[string]*Node
	idMap        map[string]string
	enc          *Encoder
}

func newChild(p *Node, name string) *Node {
	return &Node{
		parent: p, name: name, seed: hash64(name, p.seed), enc: p.enc,
		occurrences: map[string]int{},
		subs:        map[string]*Node{},
		idMap:       map[string]string{},
	}
}

func (n *Node) AddFeature(name string) { n.AddFeatureCall(name, true) }

func (n *Node) AddFeatureCall(name string, callParent bool) {
	if n.parent != nil && callParent && n.passToParent {
		n.parent.AddFeatureCall(name, true)
	}
	n.occurrences[name]++
	key := name + "#" + strconv.Itoa(n.occurrences[name])
	n.enc.addIndex(hash64(key, n.seed), key, n)
}

func (n *Node) AddNumericFeature(name string, num int, callParent bool) {
	for _, b := range numericBreakpoints {
		if num < b {
			break
		}
		n.AddFeatureCall(name+"@"+strconv.Itoa(b), callParent)
	}
	for i := 0; i < num && i < 20; i++ {
		n.AddFeatureCall(name+"@"+strconv.Itoa(i), callParent)
	}
}

// SubFeatures mirrors Features.getSubFeatures: it first records `name` as a
// feature on this node (incrementing the occurrence count), then descends into
// the child keyed `name#n`. Within one state each call yields a new `#n`; after
// StateRefresh the counts reset, so the `#1` node is reused.
func (n *Node) SubFeatures(name string, passToParent bool) *Node {
	n.AddFeature(name)
	key := name + "#" + strconv.Itoa(n.occurrences[name])
	if c, ok := n.subs[key]; ok {
		return c
	}
	c := newChild(n, key)
	c.passToParent = passToParent
	n.subs[key] = c
	return c
}

func (n *Node) StateRefresh() {
	for k := range n.occurrences {
		n.occurrences[k] = 0
	}
	for _, c := range n.subs {
		c.StateRefresh()
	}
}
