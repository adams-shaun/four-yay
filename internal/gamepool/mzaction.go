//go:build gamepool

package gamepool

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// javaHash is Java's String.hashCode, the hash MageZero's ActionEncoder keys
// its action features on.
func javaHash(s string) int32 {
	var h int32
	for i := 0; i < len(s); i++ {
		h = 31*h + int32(s[i])
	}
	return h
}

// Layout of the 128-wide action vector: a one-hot decision-kind block, a
// one-hot option-kind block, then hashed content slots.
const (
	actKindSlots  = 16 // [0,16): decision kind
	actOptSlots   = 16 // [16,32): option kind
	actFlagBase   = 32 // [32,36): self/opp, has-attacker, has-battle, has-obj
	actHashedBase = 40 // [40,128): hashed name / label / attacker-name slots
)

func hashSlot(out []float32, key string) {
	h := javaHash(key)
	n := int32(len(out) - actHashedBase)
	idx := h % n
	if idx < 0 {
		idx += n
	}
	out[actHashedBase+int(idx)] = 1
}

// cardName resolves an object id to a card name from the seat's view: every
// player zone, then the stack. "" when the object is not visible.
func cardName(v *view.View, id state.ObjID) string {
	if id == 0 {
		return ""
	}
	find := func(cs []view.CardView) string {
		for i := range cs {
			if cs[i].ID == id {
				return cs[i].Name
			}
		}
		return ""
	}
	for i := range v.Players {
		p := &v.Players[i]
		for _, z := range [][]view.CardView{p.Battlefield, p.Hand, p.Graveyard, p.Exile} {
			if n := find(z); n != "" {
				return n
			}
		}
	}
	for i := range v.Stack {
		if v.Stack[i].ID == id {
			return v.Stack[i].Name
		}
	}
	return ""
}

// encodeActionMZ writes an option's content-derived features into out
// (flatActionDim wide): decision kind, option kind, who it concerns, and
// Java-hashed slots for the option label, the card it names and, for a block,
// the attacker it blocks. Pure function of (view, decision, option).
func encodeActionMZ(r *Request, o decision.Option, out []float32) {
	for i := range out {
		out[i] = 0
	}
	out[int(uint32(javaHash(string(r.Decision.Kind)))%actKindSlots)] = 1
	out[actKindSlots+int(uint32(javaHash(o.Kind))%actOptSlots)] = 1
	if o.Player == r.Decision.Player {
		out[actFlagBase] = 1
	} else {
		out[actFlagBase+1] = 1
	}
	if o.Attacker != 0 {
		out[actFlagBase+2] = 1
		if n := cardName(&r.View, o.Attacker); n != "" {
			hashSlot(out, "attacker:"+n)
		}
	}
	if o.Obj != 0 {
		out[actFlagBase+3] = 1
		if n := cardName(&r.View, o.Obj); n != "" {
			hashSlot(out, "card:"+n)
		}
	}
	hashSlot(out, "label:"+o.Label)
}
