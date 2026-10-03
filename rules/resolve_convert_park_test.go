package rules

// W3 step 5 (the park-and-continue paths, lasagna spec §7.2): a park ask is
// answered from the tape at the point of the parked event, so the kernel's
// event order deliberately differs from the legacy park's. These tests run
// each scenario on the kernel, require its asks served with no legacy ask
// ending the run, a log-only replay that matches, and the parked event
// applied in place: before the resolving object leaves the stack.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/state"
)

// tapeEventIndex is the index of the first event of kind k (with obj, when
// non-zero) at or after from, or -1.
func tapeEventIndex(e *Engine, from int, k events.Kind, obj state.ObjID) int {
	for i := from; i < len(e.L.Events); i++ {
		if ev := e.L.Events[i]; ev.Kind == k && (obj == 0 || ev.Obj == obj) {
			return i
		}
	}
	return -1
}

const tapeBladeSrc = "Name:Tape Blade\nManaCost:1\nTypes:Artifact Equipment\n" +
	"R:Event$ Attached | ValidCard$ Card.Self | ValidTarget$ Creature | ReplaceWith$ ChooseColor | ActiveZones$ Battlefield | Description$ x\n" +
	"SVar:ChooseColor:DB$ ChooseColor | Defined$ You\nK:Equip:1\nOracle:x\n"

const tapePaperSrc = "Name:Tape Paper\nManaCost:1\nTypes:Artifact Equipment\n" +
	"R:Event$ Attached | ValidCard$ Card.Self | ValidTarget$ Creature | ReplaceWith$ ChooseName | ActiveZones$ Battlefield | Description$ x\n" +
	"SVar:ChooseName:DB$ NameCard | Defined$ You | ValidCards$ Creature | ValidDescription$ creature card\nK:Equip:1\nOracle:x\n"

const tapePrismBearSrc = "Name:Tape Prism Bear\nManaCost:B\nTypes:Creature Bear\nPT:2/2\nK:ETBReplacement:Other:ChooseColor\nSVar:ChooseColor:DB$ ChooseColor\nOracle:x\n"

const tapeTotemSrc = "Name:Tape Totem\nManaCost:B\nTypes:Artifact\nK:ETBReplacement:Other:ChooseCT\nSVar:ChooseCT:DB$ ChooseType | Type$ Creature\n" +
	"K:ETBReplacement:Other:DBChoose\nSVar:DBChoose:DB$ ChooseCard | Defined$ You | Choices$ Land.YouCtrl | ChoiceZone$ Battlefield | Mandatory$ True\nOracle:x\n"

// A chosen-copy CreateToken election (Mirrormind Crown's shape) posed while
// a DB$ Token resolves is answered in place: the copies mint at the point of
// the creation, before the rest of the chain.
func TestTapeParkTokenElection(t *testing.T) {
	copier := "Name:Tape Copier\nTypes:Enchantment\n" +
		"R:Event$ CreateToken | ActiveZones$ Battlefield | ValidPlayer$ You | Layer$ Copy | ReplaceWith$ DBCopy | Description$ x\n" +
		"SVar:DBCopy:DB$ ReplaceToken | Type$ ReplaceToken | ValidChoices$ Creature.YouCtrl | TokenScript$ Chosen\nOracle:x\n"
	name := "Tape Mint"
	src := "Name:" + name + "\nManaCost:U\nTypes:Sorcery\nA:SP$ Token | TokenScript$ tape_spirit | SubAbility$ DBGain\n" +
		tapeGainSVar + "\nOracle:x\n"
	tokens := map[string]*cards.Card{"tape_spirit": card(t, "Name:Spirit Token\nTypes:Creature Spirit\nPT:1/1\nOracle:\n")}
	before := resolve.ReadStats()
	var fixtures []*cards.Card
	for _, s := range []string{src, copier, ptResumeBearSrc, ptResumeAngelSrc} {
		fixtures = append(fixtures, card(t, s))
	}
	decks := [][]*cards.Card{append(fixtures, mountainDeck(t, 40-len(fixtures))...), mountainDeck(t, 40)}
	cfg := seatZeroStart(Config{Seed: 14100, Names: []string{"p0", "p1"}, Decks: decks, Tokens: tokens})
	e := New(cfg)
	e.Advance()
	moveByName(t, e, 0, "Tape Copier", state.ZBattlefield)
	moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
	moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
	moveByName(t, e, 0, name, state.ZHand)
	tapeCastAndResolve(t, e, name, "U")
	st := resolve.ReadStats().Sub(before)
	replayCheck(t, e, cfg)
	if st.Served < 1 || st.LegacySwitch != 0 || st.Aborts != 0 {
		t.Fatalf("the token election was not served from the tape: %+v", st)
	}
	life, mint := -1, -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.CopyToken && mint < 0 {
			mint = i
		}
		if ev.Kind == events.LifeChange && life < 0 && i > mint && mint >= 0 {
			life = i
		}
	}
	if mint < 0 || life < 0 {
		t.Fatalf("the copy minted at %d, the rider's life gain at %d: not answered in place", mint, life)
	}
}
