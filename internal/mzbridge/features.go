package mzbridge

import (
	"slices"
	"strconv"
	"strings"
)

// Port of the namespace tree in Features.java (77-152) plus the id set and
// cleanString of StateEncoder.java (59, 682-689).
//
// A FeatureSet is one encoder's tree and the ids emitted since the last
// Refresh. A Node is one namespace. Adding a feature name to a node bumps
// that node's occurrence count n for the name and emits
// FeatureID(name+"#"+n, node seed); unless told otherwise it first does the
// same in the parent, which is how a card's features pool up into its zone,
// its player and the root. Like upstream, the tree's nodes persist across
// Refresh; only the occurrence counts and the id set are reset.

// numericBreakpoints is Features.NUMERIC_BREAKPOINTS.
var numericBreakpoints = [...]int{32, 64, 128, 256, 512}

// numericUnaryCap is the count of name@0.. features a number can add.
const numericUnaryCap = 20

// FeatureSet owns a feature tree and the ids it has emitted.
type FeatureSet struct {
	root *Node
	ids  []int32

	debug bool
	names map[int32][]string

	// onEmit, when set, sees every emission in order (tests).
	onEmit func(id int32, n *Node, key string)
}

// Node is one namespace of the tree.
type Node struct {
	set    *FeatureSet
	parent *Node
	name   string // "root", or the "name#n" key it was created under
	path   string // debug path from the root
	seed   uint64
	// passToParent is fixed when the node is first created; a later Sub
	// call for the same key returns the node unchanged (Features.java:89-96).
	passToParent bool
	occurrences  map[string]int
	subs         map[string]*Node
}

// NewFeatureSet returns an empty tree rooted at Features' global seed.
func NewFeatureSet() *FeatureSet {
	s := &FeatureSet{}
	s.root = &Node{set: s, name: "root", path: "root", seed: GlobalSeed, passToParent: true,
		occurrences: map[string]int{}, subs: map[string]*Node{}}
	return s
}

// SetDebug turns on recording of the feature-name path behind every id.
// It costs a string per emission; leave it off for self-play.
func (s *FeatureSet) SetDebug(on bool) {
	s.debug = on
	if on && s.names == nil {
		s.names = map[int32][]string{}
	}
}

// Root is the tree's root namespace.
func (s *FeatureSet) Root() *Node { return s.root }

// Refresh is Features.stateRefresh plus featureVector.clear(): call it
// before encoding each state.
func (s *FeatureSet) Refresh() {
	s.root.refresh()
	s.ids = s.ids[:0]
	clear(s.names)
}

func (n *Node) refresh() {
	for k := range n.occurrences {
		n.occurrences[k] = 0
	}
	for _, c := range n.subs {
		c.refresh()
	}
}

// IDs returns the distinct ids emitted since the last Refresh, ascending:
// the form LabeledStateWriter stores a state in (it sorts a HashSet).
func (s *FeatureSet) IDs() []int32 {
	out := slices.Clone(s.ids)
	slices.Sort(out)
	return slices.Compact(out)
}

// Names returns, in debug mode, the name paths that emitted id since the
// last Refresh ("root/Player#1/Battlefield#1/Forest#2"), in emission order.
// More than one path means a hash collision or a deliberately pooled name.
func (s *FeatureSet) Names(id int32) []string { return s.names[id] }

// Describe returns, in debug mode, one "id<TAB>path" line per (id, path),
// ordered by id then emission order.
func (s *FeatureSet) Describe() []string {
	var out []string
	for _, id := range s.IDs() {
		for _, p := range s.names[id] {
			out = append(out, strconv.Itoa(int(id))+"\t"+p)
		}
	}
	return out
}

// Path is the node's debug path from the root.
func (n *Node) Path() string { return n.path }

// Namespace is the index upstream's FeatureMap logs for a node: the id of
// the node's own key under its parent's seed, or -1 for the root.
func (n *Node) Namespace() int32 {
	if n.parent == nil {
		return -1
	}
	return FeatureID(n.name, n.parent.seed)
}

// Add is Features.addFeature(name): add to this node and pool upward.
func (n *Node) Add(name string) { n.AddFeature(name, true) }

// AddFeature is Features.addFeature(name, callParent). The parent chain is
// followed when callParent is set and this node passes to its parent; each
// ancestor then applies its own passToParent.
func (n *Node) AddFeature(name string, callParent bool) {
	if n.parent != nil && callParent && n.passToParent {
		n.parent.AddFeature(name, true)
	}
	c := n.occurrences[name] + 1
	n.occurrences[name] = c
	key := name + "#" + strconv.Itoa(c)
	id := FeatureID(key, n.seed)
	s := n.set
	s.ids = append(s.ids, id)
	if s.debug {
		s.names[id] = append(s.names[id], n.path+"/"+key)
	}
	if s.onEmit != nil {
		s.onEmit(id, n, key)
	}
}

// Sub is Features.getSubFeatures(name, passToParent): it adds name as an
// ordinary feature (pooled upward) and returns the child namespace keyed by
// name and its occurrence number, creating it on first use.
func (n *Node) Sub(name string, passToParent bool) *Node {
	n.AddFeature(name, true)
	key := name + "#" + strconv.Itoa(n.occurrences[name])
	if c, ok := n.subs[key]; ok {
		return c
	}
	c := &Node{set: n.set, parent: n, name: key, path: n.path + "/" + key, seed: Hash64(key, n.seed),
		passToParent: passToParent, occurrences: map[string]int{}, subs: map[string]*Node{}}
	n.subs[key] = c
	return c
}

// AddNumeric is Features.addNumericFeature(name, num): name@B for every
// breakpoint B <= num, then name@0 .. name@(min(num,20)-1). A number of zero
// or less adds nothing.
func (n *Node) AddNumeric(name string, num int) { n.AddNumericFeature(name, num, true) }

// AddNumericFeature is Features.addNumericFeature(name, num, callParent).
func (n *Node) AddNumericFeature(name string, num int, callParent bool) {
	for _, b := range numericBreakpoints {
		if num < b {
			break
		}
		n.AddFeature(name+"@"+strconv.Itoa(b), callParent)
	}
	for i := 0; i < num && i < numericUnaryCap; i++ {
		n.AddFeature(name+"@"+strconv.Itoa(i), callParent)
	}
}

// CleanString is StateEncoder.cleanString: it removes every " [hex]" object
// id suffix (regex ` \[[0-9a-f]+]`), then every "<...>" markup run (regex
// `<[^>]*>`), in that order.
func CleanString(s string) string {
	if !strings.Contains(s, " [") && !strings.Contains(s, "<") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == ' ' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] >= 'a' && s[j] <= 'f') {
				j++
			}
			if j > i+2 && j < len(s) && s[j] == ']' {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	s = b.String()
	if !strings.Contains(s, "<") {
		return s
	}
	b.Reset()
	for {
		lt := strings.IndexByte(s, '<')
		if lt < 0 {
			break
		}
		gt := strings.IndexByte(s[lt:], '>')
		if gt < 0 {
			break
		}
		b.WriteString(s[:lt])
		s = s[lt+gt+1:]
	}
	b.WriteString(s)
	return b.String()
}
