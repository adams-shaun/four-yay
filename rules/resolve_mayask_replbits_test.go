package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestReplEventBitCensus holds every replaceable event kind reachable by the
// every-event body scan (tapeAnyReplBodyMayAsk's forEachReplacementSourceFor
// with ^0): each kind has an event bit (DrawCards shares Draw's, its alias),
// and an object whose only R: line names the kind is visited. A kind with no
// bit sits in a zone whose summary mask is 0, so an asking body on it was a
// predicate miss.
func TestReplEventBitCensus(t *testing.T) {
	seen := map[uint32]cards.ReplEvent{}
	for k := cards.ReplEvent(1); k < cards.ReplEventCount; k++ {
		b := replEventBits[k]
		if b == 0 {
			t.Errorf("ReplEvent %s has no event bit", k)
			continue
		}
		if b&(b-1) != 0 {
			t.Errorf("ReplEvent %s bit %#x is not a single bit", k, b)
		}
		if prev, ok := seen[b]; ok && !(k == cards.ReplDrawCards && prev == cards.ReplDraw) {
			t.Errorf("ReplEvent %s shares bit %#x with %s", k, b, prev)
		}
		seen[b] = k
	}
	for k := cards.ReplEvent(1); k < cards.ReplEventCount; k++ {
		name := fmt.Sprintf("Census %s", k)
		src := fmt.Sprintf("Name:%s\nManaCost:W\nTypes:Enchantment\nR:Event$ %s | ActiveZones$ Battlefield | Description$ x\nOracle:x\n", name, k)
		e, _ := kr8Fixture(t, 2, 9900+uint64(k), src)
		id := moveByName(t, e, 0, name, state.ZBattlefield)
		found := false
		e.forEachReplacementSourceFor(^uint32(0), func(v state.ObjID) { found = found || v == id })
		if !found {
			t.Errorf("ReplEvent %s: the every-event body scan never visits a source whose only line names it", k)
		}
	}
}

// tapeGainAskerSrc is a GainLife-only replacement whose body asks (a color
// choice): the every-event body scan must find it, so an otherwise ask-free
// GainLife resolution takes a checkpoint instead of missing.
const tapeGainAskerSrc = "Name:Tape Gain Asker\nManaCost:W\nTypes:Enchantment\n" +
	"R:Event$ GainLife | ActiveZones$ Battlefield | ValidPlayer$ You | ReplaceWith$ DBChoose | Description$ x\n" +
	"SVar:DBChoose:DB$ ChooseColor | Defined$ You\nOracle:x\n"

func TestKr8GainLifeOnlyReplacementBodyAskIsCheckpointed(t *testing.T) {
	_, st := kr8Kernel(t, 2, 9204, func(t *testing.T, e *Engine) {
		moveByName(t, e, 0, "Tape Gain Asker", state.ZBattlefield)
		if !tapeAnyReplBodyMayAsk(e) {
			t.Fatal("the body scan misses a GainLife-only replacement whose body asks")
		}
		tapeCastAndResolve(t, e, "Tape Gain", "W")
	}, tapeNoAskSrc, tapeGainAskerSrc)
	if st.Misses != 0 || st.Checkpoints == 0 || st.Exempt != 0 {
		t.Fatalf("the asking replacement body was not checkpointed: %+v", st)
	}
}
