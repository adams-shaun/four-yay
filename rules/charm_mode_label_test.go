package rules

// Charm mode labels follow the printed SpellDescription$ on the trigger
// placement path (task agent-20260928T074145Z-c832b15a). Varchild's
// War-Riders is one of the corpus's three sub-chain-only carriers and the
// only one whose Charm is a TRIGGER body: at the beginning of its
// controller's upkeep its cumulative-upkeep trigger poses the placement
// KModes ask (askTriggerModes, CR 603.3c), whose two modes
// (TrigAgeSurvivor, TrigAgeSacrifice) carry no SpellDescription$ of their
// own — theirs ride the SurvivorDistribution and Sacrifice subs, one hop
// down. Before the fix both options labelled by their raw SVar names.
//
// The script is varchilds_war_riders.txt verbatim except the deck-related
// rider (AI:RemoveDeck) and the Oracle line (display text), inlined so the
// corpus carrier's exact parameter spellings are what the engine defends
// without committing GPL data.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const varchildsWarRidersScript = "Name:Varchild's War-Riders\nManaCost:1 R\nTypes:Creature Human Warrior\nPT:3/4\n" +
	"K:Trample\nK:Rampage:1\n" +
	"T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigCumUpkeep | TriggerDescription$ Cumulative upkeep—Have an opponent create a 1/1 red Survivor creature token.\n" +
	"SVar:TrigCumUpkeep:DB$ Charm | Choices$ TrigAgeSurvivor,TrigAgeSacrifice\n" +
	"SVar:TrigAgeSurvivor:DB$ PutCounter | Defined$ Self | CounterType$ AGE | CounterNum$ 1 | SubAbility$ SurvivorDistribution\n" +
	"SVar:SurvivorDistribution:DB$ Repeat | RepeatSubAbility$ SurvivorRecipient | MaxRepeat$ X | SpellDescription$ Have an opponent create a 1/1 red Survivor creature token for each Age Counter on CARDNAME.\n" +
	"SVar:SurvivorRecipient:DB$ ChoosePlayer | Defined$ You | Choices$ Player.Opponent | ChoiceTitle$ Choose an opponent to create a 1/1 red Survivor creature token. | SubAbility$ Survivor\n" +
	"SVar:Survivor:DB$ Token | TokenScript$ r_1_1_survivor | TokenOwner$ ChosenPlayer\n" +
	"SVar:X:Count$CardCounters.AGE\n" +
	"SVar:TrigAgeSacrifice:DB$ PutCounter | Defined$ Self | CounterType$ AGE | CounterNum$ 1 | SubAbility$ Sacrifice\n" +
	"SVar:Sacrifice:DB$ Sacrifice | Defined$ Self | SpellDescription$ Sacrifice CARDNAME.\n" +
	"Oracle:x\n"

// TestVarchildsWarRidersPlacementLabelsFollowThePrintedDescriptions drives a
// battlefield Varchild's War-Riders to its upkeep trigger's placement
// KModes ask and asserts the options carry the subs' printed descriptions,
// never the raw SVar names.
func TestVarchildsWarRidersPlacementLabelsFollowThePrintedDescriptions(t *testing.T) {
	e := combatEngine(t)
	riders := onBoardCard(t, e, 0, card(t, varchildsWarRidersScript))
	if o := e.G.Obj(riders); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Varchild's War-Riders is not on the battlefield")
	}

	e.pending = nil
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("precondition: queued triggers = %d, want the War-Riders' upkeep trigger",
			len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the placement KModes ask", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("precondition: options = %+v, want the two Choices$ modes", d.Options)
	}

	for _, want := range []string{
		"create a 1/1 red Survivor creature token for each Age Counter",
		"Sacrifice CARDNAME.",
	} {
		found := false
		for _, o := range d.Options {
			if strings.Contains(o.Label, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("printed description %q not offered: %+v", want, d.Options)
		}
	}
	for _, o := range d.Options {
		if o.Label == "TrigAgeSurvivor" || o.Label == "TrigAgeSacrifice" {
			t.Fatalf("mode option still labelled by its raw SVar name: %+v", d.Options)
		}
	}
}
