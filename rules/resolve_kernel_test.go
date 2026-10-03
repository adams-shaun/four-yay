package rules

// The resolution kernel's dual-run tests (W3 steps 0-1; lasagna spec §7.7):
// every scenario is driven twice by the same deterministic driver, once on
// the legacy resume machinery and once on the tape kernel (rules/resolve),
// and the two logs must be identical event for event and intent for intent,
// with the same head and RNG draw count. The kernel counters prove the tape
// path ran.

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// tapePick is the deterministic answer policy: every non-priority decision
// takes options chosen from its sequence number, so the two runs answer
// identically exactly when they are posed identically.
func tapePick(d *decision.Decision) []int {
	if len(d.Options) == 0 {
		return nil
	}
	n := d.Max
	if n < 1 {
		n = 1
	}
	if n > len(d.Options) {
		n = len(d.Options)
	}
	if d.Min > n {
		n = d.Min
	}
	start := int(d.Seq) % len(d.Options)
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, (start+i)%len(d.Options))
	}
	return out
}

func tapePassIndex(d *decision.Decision) int {
	for _, o := range d.Options {
		if o.Kind == "pass" {
			return o.Index
		}
	}
	return -1
}

// tapeResolveAll passes priority and answers every other decision with
// tapePick until the stack is empty at a priority decision.
func tapeResolveAll(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 400; i++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			return
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return
			}
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: tapePick(d)}); err != nil {
			t.Fatalf("submit %s %v: %v", d.Kind, tapePick(d), err)
		}
	}
	t.Fatal("stack never drained")
}

// tapeCastAndResolve funds and casts the named fixture, answers its cast
// asks with tapePick, then resolves everything.
func tapeCastAndResolve(t *testing.T, e *Engine, name, mana string) {
	t.Helper()
	addMana(t, e, 0, mana)
	id := fixtureInHand(t, e, name)
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: tapePick(d)}); err != nil {
			t.Fatalf("cast-time %s: %v", d.Kind, err)
		}
	}
	tapeResolveAll(t, e)
}

func tapeRequireSameLog(t *testing.T, a, b *Engine) {
	t.Helper()
	n := min(len(a.L.Events), len(b.L.Events))
	var x, y []byte
	for i := 0; i < n; i++ {
		x = a.L.Events[i].Append(x[:0])
		y = b.L.Events[i].Append(y[:0])
		if string(x) != string(y) {
			t.Fatalf("event %d differs:\n legacy %s\n tape   %s", i, tapeEventString(a.L.Events[i]), tapeEventString(b.L.Events[i]))
		}
	}
	if len(a.L.Events) != len(b.L.Events) {
		t.Fatalf("event counts differ: legacy %d, tape %d", len(a.L.Events), len(b.L.Events))
	}
	if len(a.L.Intents) != len(b.L.Intents) {
		t.Fatalf("intent counts differ: legacy %d, tape %d", len(a.L.Intents), len(b.L.Intents))
	}
	if a.L.Head() != b.L.Head() || a.RNGDraws() != b.RNGDraws() {
		t.Fatalf("heads differ: legacy %s/%d, tape %s/%d", a.L.Head(), a.RNGDraws(), b.L.Head(), b.RNGDraws())
	}
}

func tapeEventString(ev events.Event) string {
	return fmt.Sprintf("{%v obj=%d p=%d amt=%d text=%q ctr=%q}", ev.Kind, ev.Obj, ev.Player, ev.Amount, ev.Text, ev.Counter)
}

const tapeChooseColorSrc = "Name:Tape Wash\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ ChooseColor | Defined$ You | SubAbility$ DBGain\n" +
	"SVar:DBGain:DB$ GainLife | LifeAmount$ 2\nOracle:x\n"

const tapeNoAskSrc = "Name:Tape Gain\nManaCost:W\nTypes:Sorcery\nA:SP$ GainLife | LifeAmount$ 2\nOracle:x\n"

func tapeVoteSrc() string {
	return "Name:Tape Council\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Vote | Defined$ Player | Choices$ DBGainA,DBGainB | VoteTiedAbility$ DBGainB\n" +
		"SVar:DBGainA:DB$ GainLife | LifeAmount$ 1 | Defined$ You\n" +
		"SVar:DBGainB:DB$ GainLife | LifeAmount$ 3 | Defined$ You\nOracle:x\n"
}

const tapeCardVoteSrc = "Name:Tape Judgment\nManaCost:W\nTypes:Sorcery\n" +
	"A:SP$ Vote | Defined$ Player | VoteSubAbility$ DBExile | VoteCard$ Permanent.nonLand\n" +
	"SVar:DBExile:DB$ ChangeZone | Defined$ Remembered | Origin$ Battlefield | Destination$ Exile\nOracle:x\n"

const tapePlayerVoteSrc = "Name:Tape Verdict\nManaCost:R\nTypes:Sorcery\n" +
	"A:SP$ Vote | Defined$ Player | VotePlayer$ Other | StoreVoteNum$ True | SubAbility$ DBRepeat\n" +
	"SVar:DBRepeat:DB$ RepeatEach | RepeatPlayers$ Player | RepeatSubAbility$ DBDraw | AmountFromVotes$ True\n" +
	"SVar:DBDraw:DB$ Draw | Defined$ Remembered | NumCards$ Votes\nOracle:x\n"

// A loop with per-iteration asks: every player discards a card of their
// choice (TgtChoose over Defined$ Player).
const tapeEachDiscardSrc = "Name:Tape Each Discard\nManaCost:B\nTypes:Sorcery\n" +
	"A:SP$ Discard | Defined$ Player | Mode$ TgtChoose | NumCards$ 1 | SubAbility$ DBGain\n" +
	"SVar:DBGain:DB$ GainLife | LifeAmount$ 2\nOracle:x\n"

// A converted ask then a legacy one (Scry's KArrange, held legacy): the re-run aborts and
// the legacy path replays the tape (§7.7 run-time fallback).
const tapeMixedSrc = "Name:Tape Mixed\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 1 | SubAbility$ DBScry\n" +
	"SVar:DBScry:DB$ Scry | ScryNum$ 2\nOracle:x\n"

// A legacy ask first: the kernel switches to legacy in place.
const tapeLegacyFirstSrc = "Name:Tape Legacy First\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ Scry | ScryNum$ 2 | SubAbility$ DBColor\n" +
	"SVar:DBColor:DB$ ChooseColor | Defined$ You\nOracle:x\n"

// tapeTestRedeal re-deals player p's hand and library in a hypothetical
// world exactly the way searchprobe's redealPlayer does: Secret MoveZone and
// LibraryOrder events on the world's own log (events.Emit), here with every
// hidden card unknown.
func tapeTestRedeal(w *Engine, p state.PlayerID, seed uint64) {
	hand := append([]state.ObjID(nil), w.G.Zone(state.ZHand, p)...)
	lib := append([]state.ObjID(nil), w.G.Zone(state.ZLibrary, p)...)
	pool := append(append([]state.ObjID(nil), hand...), lib...)
	r := newRNG(seed)
	r.Shuffle(pool)
	for _, id := range hand {
		events.Emit(w.G, w.L, events.Event{Kind: events.MoveZone, Player: p, Obj: id, From: state.ZHand, To: state.ZLibrary, Secret: true})
	}
	for _, id := range pool[:len(hand)] {
		events.Emit(w.G, w.L, events.Event{Kind: events.MoveZone, Player: p, Obj: id, From: state.ZLibrary, To: state.ZHand, Secret: true})
	}
	events.Emit(w.G, w.L, events.Event{Kind: events.LibraryOrder, Player: p, IDs: pool[len(hand):], Secret: true})
}

const tapeDrawVoteSrc = "Name:Tape Draw Vote\nManaCost:B\nTypes:Sorcery\n" +
	"A:SP$ Draw | NumCards$ 2 | SubAbility$ DBVote\n" +
	"SVar:DBVote:DB$ Vote | Defined$ Player | Choices$ DBGainA,DBGainB | VoteTiedAbility$ DBGainB | SubAbility$ DBDraw\n" +
	"SVar:DBGainA:DB$ GainLife | LifeAmount$ 1 | Defined$ You\n" +
	"SVar:DBGainB:DB$ GainLife | LifeAmount$ 3 | Defined$ You\n" +
	"SVar:DBDraw:DB$ Draw | NumCards$ 1 | SubAbility$ DBShuffle\n" +
	"SVar:DBShuffle:DB$ Shuffle | Defined$ You\nOracle:x\n"
