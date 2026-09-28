package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The RepeatEach primitive's DamageMap$ read (task
// inbox-paramcensus-final-stragglers, Price of Progress entry). DamageMap$
// True makes the loop's damage ONE damage batch: Forge accumulates every
// iteration's dealDamage into a per-SA damage table and deals it once after
// the loop (RepeatEachEffect.resolve), so the loop's DamageDealtOnce
// triggers latch once per batch -- once per dealing source -- instead of
// once per iteration's own batch-of-one. This build's shape of that is the
// existing damage-batch bracket opened around the whole loop; the Damage
// events themselves are unchanged.

// popSrc is Price of Progress's real script shape (the SP$/DB$/X trio
// verbatim): 2 damage to each player per nonbasic land they control.
const popSrc = "Name:Price of Progress\nManaCost:1 R\nTypes:Instant\n" +
	"A:SP$ RepeatEach | RepeatPlayers$ Player | RepeatSubAbility$ DBDamage | DamageMap$ True | SpellDescription$ CARDNAME deals 2 damage to each player for each nonbasic land they control.\n" +
	"SVar:DBDamage:DB$ DealDamage | Defined$ Remembered | NumDmg$ X\n" +
	"SVar:X:Count$Valid Land.nonBasic+RememberedPlayerCtrl/Times.2\nOracle:x\n"

// popWatcherSrc is a synthetic batch watcher: once per damage batch dealt by
// an instant or sorcery, draw a card. With the DamageMap batch open across
// Price of Progress's whole loop, the one dealing source queues exactly one
// trigger; with per-iteration batches it would queue one per player.
const popWatcherSrc = "Name:Batch Watcher\nTypes:Creature\nPT:1/1\n" +
	"T:Mode$ DamageDealtOnce | ValidSource$ Instant,Sorcery | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ Whenever one or more players are dealt damage by an instant or sorcery spell in one batch, you draw a card.\n" +
	"SVar:TrigDraw:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n"

func popEngine(t *testing.T, watcher bool) (*Engine, int) {
	t.Helper()
	e, _, id := newFixtureDeck(t, 96, popSrc)
	if watcher {
		onBoard(t, e, 0, popWatcherSrc)
	}
	onBoard(t, e, 0, "Name:Isle\nTypes:Land Island\nOracle:x\n")         // my nonbasic
	onBoard(t, e, 1, "Name:Ile\nTypes:Land Island\nOracle:x\n")          // their nonbasic #1
	onBoard(t, e, 1, "Name:Ilx\nTypes:Land Island\nOracle:x\n")          // their nonbasic #2
	onBoard(t, e, 1, "Name:Peak\nTypes:Basic Land Mountain\nOracle:x\n") // basic: never counted
	_ = id
	return e, len(e.G.Zone(state.ZHand, 0))
}

// TestPriceOfProgressDamageAndBatch pins both halves: the per-player damage
// (2x each player's own nonbasic count) and the batch latch -- the watcher
// draws exactly ONE card for the whole loop, not one per player.
func TestPriceOfProgressDamageAndBatch(t *testing.T) {
	t.Parallel()
	e, hand := popEngine(t, true)
	myLife := e.G.Players[0].Life
	theirLife := e.G.Players[1].Life
	addMana(t, e, 0, "RR")
	castFixture(t, e, e.G.Zone(state.ZHand, 0)[firstHandIndexOf(t, e, "Price of Progress")], -1)
	passUntilStackEmpty(t, e, 20)
	drainPopPending(t, e)

	if got := myLife - e.G.Players[0].Life; got != 2 {
		t.Fatalf("seat 0 took %d damage, want 2 (one nonbasic)", got)
	}
	if got := theirLife - e.G.Players[1].Life; got != 4 {
		t.Fatalf("seat 1 took %d damage, want 4 (two nonbasics)", got)
	}
	// One batched DealtOnce fire for the whole loop: PoP leaves the hand and
	// the watcher's single draw replaces it.
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand {
		t.Fatalf("hand %d -> %d, want %d (the cast card out, one batched watcher draw in)", hand, got, hand)
	}
}

// TestPriceOfProgressDoneOncePerReferent is the contrast pin: a
// DamageDoneOnce watcher latches per DAMAGED player, not per batch, so the
// same batched loop draws it two cards -- one per player that took damage.
const popDoneWatcherSrc = "Name:Done Watcher\nTypes:Creature\nPT:1/1\n" +
	"T:Mode$ DamageDoneOnce | ValidTarget$ Player | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ Whenever one or more players are dealt damage in one batch, you draw a card.\n" +
	"SVar:TrigDraw:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n"

func TestPriceOfProgressDoneOncePerReferent(t *testing.T) {
	t.Parallel()
	e, _ := popEngine(t, false)
	onBoard(t, e, 0, popDoneWatcherSrc)
	hand := len(e.G.Zone(state.ZHand, 0))
	addMana(t, e, 0, "RR")
	castFixture(t, e, e.G.Zone(state.ZHand, 0)[firstHandIndexOf(t, e, "Price of Progress")], -1)
	passUntilStackEmpty(t, e, 20)
	drainPopPending(t, e)
	// Per-referent latch: two players damaged in one batch, two DoneOnce
	// fires. The cast card left, so the hand moved by 2-1.
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand+1 {
		t.Fatalf("done-watcher hand %d -> %d, want %d (one draw per damaged player)", hand, got, hand+1)
	}
}

// drainPopPending resolves the batch's queued triggers: the stack may empty
// before the drain runs, so priority rounds until no queued trigger remains
// (bounded), passing whatever non-priority ask the drain itself poses.
func drainPopPending(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 20 && len(e.pendingTriggers) > 0; i++ {
		e.priorityRound()
		for j := 0; j < 20 && (len(e.G.Stack) > 0 || len(e.pendingTriggers) > 0); j++ {
			d := e.Pending()
			if d == nil {
				break
			}
			var choices []int
			switch d.Kind {
			case decision.KPriority:
				passIdx := -1
				for _, o := range d.Options {
					if o.Kind == "pass" {
						passIdx = o.Index
					}
				}
				if passIdx < 0 {
					t.Fatalf("no pass option: %+v", d)
				}
				choices = []int{passIdx}
			case decision.KTriggerOrder:
				// The drain's own APNAP ordering ask: submit the offered
				// order as-is (deterministic, the drains' usual stand-in).
				for k := range d.Options {
					choices = append(choices, k)
				}
			default:
				t.Fatalf("unexpected decision while draining the triggers: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("submit drain choice %v: %v", choices, err)
			}
		}
	}
}

func firstHandIndexOf(t *testing.T, e *Engine, name string) int {
	t.Helper()
	for i, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return i
		}
	}
	t.Fatalf("%s not in seat 0's hand", name)
	return -1
}
