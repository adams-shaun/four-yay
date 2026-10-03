package rules

// W3 step 2's dual-run tests for the zone movers' converted asks: the
// library search (pick, Optional$ confirmation, may-shuffle), the Defined$
// library fetch's election, the hidden-hand walk (pick and confirmation,
// one and several owners), the public-origin hidden pick (pick and
// confirmation), the player sacrifice (the pick, the strict election, the
// object election), ChangeZone's AlternativeDecider$ and Imprint$ choices,
// its object-path may-shuffle, manifest dread, and the generic mid-resolution
// target asks ("tgts" and ChangeZone's "choice"). Each runs on legacy and on
// the kernel and must stay byte-identical with every ask served from the tape.

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// tapeZoneSorcery is a synthetic one-mana sorcery with the given ability
// lines.
func tapeZoneSorcery(name, body string) string {
	return "Name:" + name + "\nManaCost:B\nTypes:Sorcery\n" + body + "\nOracle:x\n"
}

// tapeZoneETB is a synthetic creature whose ETB trigger runs body (its
// Execute SVar is TrigBody), so a ValidTgts$ sub of the body is the
// mid-resolution "tgts"/"choice" ask no placement or cast covers.
func tapeZoneETB(name, body string) string {
	return "Name:" + name + "\nManaCost:B\nTypes:Creature Rogue\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigBody | TriggerDescription$ x\n" +
		body + "\nOracle:x\n"
}

// tapeZoneServedKinds runs scenario on both paths, requires every ask served
// from the tape, and returns the stats with the resume kinds the tape engine
// posed (observed on the legacy run's posed decisions, which are identical).
func tapeZoneCase(t *testing.T, seats int, seed uint64, minServed int64, setup func(t *testing.T, e *Engine), name string, srcs ...string) {
	t.Helper()
	_, st := tapeDual(t, seats, seed, func(t *testing.T, e *Engine) {
		if setup != nil {
			setup(t, e)
		}
		tapeCastAndResolve(t, e, name, "B")
	}, srcs...)
	if st.Served < minServed || st.LegacySwitch != 0 || st.Aborts != 0 {
		t.Fatalf("%s: the converted asks were not served from the tape: %+v", name, st)
	}
}

// tapeZoneKinds records the resume kinds a legacy run of the scenario poses,
// so a case proves it reached the ask it is named for.
func tapeZoneKinds(t *testing.T, seats int, seed uint64, setup func(t *testing.T, e *Engine), name string, srcs ...string) map[string]int {
	t.Helper()
	e, _ := tapeFixture(t, seats, seed, false, srcs...)
	if setup != nil {
		setup(t, e)
	}
	kinds := map[string]int{}
	addMana(t, e, 0, "B")
	id := fixtureInHand(t, e, name)
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	for i := 0; i < 400; i++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			break
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				break
			}
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		kinds[d.ResumeKind]++
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: tapePick(d)}); err != nil {
			t.Fatalf("submit %s: %v", d.Kind, err)
		}
	}
	return kinds
}

func tapeZoneToGraveyard(n int) func(t *testing.T, e *Engine) {
	return func(t *testing.T, e *Engine) {
		for p := 0; p < len(e.G.Players); p++ {
			for i := 0; i < n; i++ {
				moveByName(t, e, state.PlayerID(p), "Mountain", state.ZGraveyard)
			}
		}
	}
}

func tapeZoneToHand(n int) func(t *testing.T, e *Engine) {
	return func(t *testing.T, e *Engine) {
		for p := 0; p < len(e.G.Players); p++ {
			for i := 0; i < n; i++ {
				moveByName(t, e, state.PlayerID(p), "Mountain", state.ZHand)
			}
		}
	}
}

func tapeZoneToBattlefield(n int) func(t *testing.T, e *Engine) {
	return func(t *testing.T, e *Engine) {
		for p := 0; p < len(e.G.Players); p++ {
			for i := 0; i < n; i++ {
				moveByName(t, e, state.PlayerID(p), "Mountain", state.ZBattlefield)
			}
		}
	}
}

func TestTapeConvertZone(t *testing.T) {
	gain := "\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2"
	cases := []struct {
		name, src string
		seats     int
		setup     func(t *testing.T, e *Engine)
		served    int64
		kinds     []string
		extra     []string
	}{
		{name: "Tape Tutor", seats: 2, served: 1, kinds: []string{"search"},
			src: tapeZoneSorcery("Tape Tutor", "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Land | ChangeNum$ 2 | SubAbility$ DBGain"+gain)},
		{name: "Tape Each Tutor", seats: 3, served: 3, kinds: []string{"search"},
			src: tapeZoneSorcery("Tape Each Tutor", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Library | Destination$ Graveyard | ChangeType$ Land | ChangeNum$ 1")},
		{name: "Tape May Shuffle", seats: 2, served: 2, kinds: []string{"search", "search_mayshuffle"},
			src: tapeZoneSorcery("Tape May Shuffle", "A:SP$ ChangeZone | Origin$ Library | Destination$ Library | LibraryPosition$ 0 | ChangeType$ Land | ChangeNum$ 1 | ShuffleNonMandatory$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Each May Shuffle", seats: 3, served: 6, kinds: []string{"search", "search_mayshuffle"},
			src: tapeZoneSorcery("Tape Each May Shuffle", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Library | Destination$ Hand | ChangeType$ Land | ChangeNum$ 1 | ShuffleNonMandatory$ True")},
		{name: "Tape Confirm Tutor", seats: 3, served: 3, kinds: []string{"search_confirm"},
			src: tapeZoneSorcery("Tape Confirm Tutor", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Library | Destination$ Hand | ChangeType$ Land | ChangeNum$ 1 | Optional$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Limited Tutor", seats: 2, served: 1, kinds: []string{"search"},
			src: tapeZoneSorcery("Tape Limited Tutor", "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Land | ChangeNum$ 1 | MaxRevealed$ 4 | Bogus$ 1")},
		{name: "Tape Top Fetch", seats: 2, served: 1, kinds: []string{"defined_library_optional"},
			src: tapeZoneSorcery("Tape Top Fetch", "A:SP$ ChangeZone | Defined$ TopOfLibrary | Origin$ Library | Destination$ Graveyard | Optional$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Put Back", seats: 2, served: 1, kinds: []string{"hand_move"}, setup: tapeZoneToHand(3),
			src: tapeZoneSorcery("Tape Put Back", "A:SP$ ChangeZone | Origin$ Hand | Destination$ Library | ChangeNum$ 2 | Mandatory$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Maybe Put Back", seats: 2, served: 1, kinds: []string{"hand_move_confirm"}, setup: tapeZoneToHand(3),
			src: tapeZoneSorcery("Tape Maybe Put Back", "A:SP$ ChangeZone | Origin$ Hand | Destination$ Library | ChangeNum$ 1 | Optional$ True")},
		{name: "Tape Each Hand", seats: 3, served: 3, kinds: []string{"hand_move"}, setup: tapeZoneToHand(3),
			src: tapeZoneSorcery("Tape Each Hand", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Hand | Destination$ Graveyard | ChangeNum$ 1 | Mandatory$ True")},
		{name: "Tape Each Maybe Hand", seats: 3, served: 3, kinds: []string{"hand_move_confirm"}, setup: tapeZoneToHand(3),
			src: tapeZoneSorcery("Tape Each Maybe Hand", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Hand | Destination$ Graveyard | ChangeNum$ 1 | Optional$ True")},
		{name: "Tape Hidden Pick", seats: 2, served: 1, kinds: []string{"hidden_pick"}, setup: tapeZoneToGraveyard(2),
			src: tapeZoneSorcery("Tape Hidden Pick", "A:SP$ ChangeZone | Origin$ Graveyard | Destination$ Hand | Hidden$ True | ChangeType$ Card | ChangeNum$ 2 | SubAbility$ DBGain"+gain)},
		{name: "Tape Each Hidden Pick", seats: 3, served: 3, kinds: []string{"hidden_pick"}, setup: tapeZoneToGraveyard(2),
			src: tapeZoneSorcery("Tape Each Hidden Pick", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Graveyard | Destination$ Exile | Hidden$ True | ChangeType$ Card | ChangeNum$ 1")},
		{name: "Tape Maybe Hidden Pick", seats: 3, served: 3, kinds: []string{"hidden_pick_confirm"}, setup: tapeZoneToGraveyard(2),
			src: tapeZoneSorcery("Tape Maybe Hidden Pick", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Graveyard | Destination$ Exile | Hidden$ True | ChangeType$ Card | ChangeNum$ 1 | Optional$ True")},
		{name: "Tape Edict", seats: 3, served: 3, kinds: []string{"sacrifice"}, setup: tapeZoneToBattlefield(3),
			src: tapeZoneSorcery("Tape Edict", "A:SP$ Sacrifice | Defined$ Player | SacValid$ Land | Amount$ 2 | ShowSacrificedCards$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Strict Edict", seats: 3, served: 3, kinds: []string{"sacrifice_optional"}, setup: tapeZoneToBattlefield(3),
			src: tapeZoneSorcery("Tape Strict Edict", "A:SP$ Sacrifice | Defined$ Player | SacValid$ Land | Amount$ 2 | Optional$ True | StrictAmount$ True | RememberSacrificed$ True")},
		{name: "Tape Self Sac", seats: 2, served: 1, kinds: []string{"sacrifice"}, setup: tapeZoneToBattlefield(1),
			src: tapeZoneSorcery("Tape Self Sac", "A:SP$ Sacrifice | ValidTgts$ Land | TgtPrompt$ x | Optional$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Imprint", seats: 2, served: 1, kinds: []string{"imprint"}, setup: tapeZoneToHand(3),
			src: tapeZoneSorcery("Tape Imprint", "A:SP$ ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Card | ChangeNum$ 1 | Imprint$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Vanish", seats: 2, served: 1, kinds: []string{"changezone_alternative"},
			setup: func(t *testing.T, e *Engine) { moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield) },
			extra: []string{ptResumeBearSrc},
			src:   tapeZoneSorcery("Tape Vanish", "A:SP$ ChangeZone | ValidTgts$ Creature | TgtPrompt$ x | AlternativeDecider$ TargetedOwner | Origin$ Battlefield | Destination$ Library | DestinationAlternative$ Library | LibraryPositionAlternative$ -1 | SubAbility$ DBGain"+gain)},
		{name: "Tape Dread", seats: 2, served: 1, kinds: []string{"manifest_dread"},
			src: tapeZoneSorcery("Tape Dread", "A:SP$ ManifestDread | SubAbility$ DBGain"+gain)},
		{name: "Tape Pinger", seats: 2, served: 1, kinds: []string{"tgts"},
			src: tapeZoneETB("Tape Pinger", "SVar:TrigBody:DB$ GainLife | LifeAmount$ 1 | SubAbility$ DBDmg\nSVar:DBDmg:DB$ DealDamage | ValidTgts$ Player | NumDmg$ 1 | SubAbility$ DBGain"+gain)},
		{name: "Tape Raiser", seats: 2, served: 1, kinds: []string{"choice"}, setup: tapeZoneToGraveyard(2),
			src: tapeZoneETB("Tape Raiser", "SVar:TrigBody:DB$ GainLife | LifeAmount$ 1 | SubAbility$ DBReturn\nSVar:DBReturn:DB$ ChangeZone | ValidTgts$ Card.YouOwn | TgtPrompt$ x | TargetMin$ 0 | TargetMax$ 2 | Origin$ Graveyard | Destination$ Hand | SubAbility$ DBGain"+gain)},
		{name: "Tape Reshuffle", seats: 2, served: 2, kinds: []string{"choice", "search_mayshuffle"}, setup: tapeZoneToGraveyard(2),
			src: tapeZoneETB("Tape Reshuffle", "SVar:TrigBody:DB$ GainLife | LifeAmount$ 1 | SubAbility$ DBShuffle\nSVar:DBShuffle:DB$ ChangeZone | ValidTgts$ Card.YouOwn | TgtPrompt$ x | TargetMin$ 1 | TargetMax$ 2 | Origin$ Graveyard | Destination$ Library | Shuffle$ True | ShuffleNonMandatory$ True | SubAbility$ DBGain"+gain)},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srcs := append([]string{tc.src}, tc.extra...)
			seed := 31000 + uint64(i)
			kinds := tapeZoneKinds(t, tc.seats, seed, tc.setup, tc.name, srcs...)
			for _, k := range tc.kinds {
				if kinds[k] == 0 {
					t.Fatalf("%s posed no %q ask: %v", tc.name, k, kinds)
				}
			}
			tapeZoneCase(t, tc.seats, seed, tc.served, tc.setup, tc.name, srcs...)
		})
	}
}

// Each yes/no election answered both ways: the election named by kind takes
// option `answer`, every other decision tapePick's.
func TestTapeConvertZoneElectionsBothWays(t *testing.T) {
	cases := []struct{ name, src, kind string }{
		{"Tape Sweep Confirm", tapeZoneSorcery("Tape Sweep Confirm", "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Land | ChangeNum$ 1 | Optional$ True"), "search_confirm"},
		{"Tape Sweep Shuffle", tapeZoneSorcery("Tape Sweep Shuffle", "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Land | ChangeNum$ 1 | ShuffleNonMandatory$ True"), "search_mayshuffle"},
		{"Tape Sweep Fetch", tapeZoneSorcery("Tape Sweep Fetch", "A:SP$ ChangeZone | Defined$ TopOfLibrary | Origin$ Library | Destination$ Graveyard | Optional$ True"), "defined_library_optional"},
		{"Tape Sweep Hand", tapeZoneSorcery("Tape Sweep Hand", "A:SP$ ChangeZone | Origin$ Hand | Destination$ Library | ChangeNum$ 1 | Optional$ True"), "hand_move_confirm"},
		{"Tape Sweep Hidden", tapeZoneSorcery("Tape Sweep Hidden", "A:SP$ ChangeZone | Origin$ Graveyard | Destination$ Exile | Hidden$ True | ChangeType$ Card | ChangeNum$ 1 | Optional$ True"), "hidden_pick_confirm"},
		{"Tape Sweep Strict", tapeZoneSorcery("Tape Sweep Strict", "A:SP$ Sacrifice | Defined$ You | SacValid$ Land | Amount$ 1 | Optional$ True | StrictAmount$ True"), "sacrifice_optional"},
	}
	for _, tc := range cases {
		for answer := 0; answer < 2; answer++ {
			t.Run(fmt.Sprintf("%s/%d", tc.name, answer), func(t *testing.T) {
				_, st := tapeDual(t, 2, 32000, func(t *testing.T, e *Engine) {
					tapeZoneToHand(3)(t, e)
					tapeZoneToGraveyard(2)(t, e)
					tapeZoneToBattlefield(2)(t, e)
					addMana(t, e, 0, "B")
					id := fixtureInHand(t, e, tc.name)
					submitChoices(t, e, castOptionFor(t, e, id).Index)
					hit := 0
					for i := 0; i < 400; i++ {
						d := e.Pending()
						if d == nil || e.G.Over {
							break
						}
						if d.Kind == decision.KPriority {
							if len(e.G.Stack) == 0 {
								break
							}
							submitChoices(t, e, tapePassIndex(d))
							continue
						}
						pick := tapePick(d)
						if d.ResumeKind == tc.kind {
							pick = []int{answer}
							hit++
						}
						if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: pick}); err != nil {
							t.Fatalf("submit %s: %v", d.Kind, err)
						}
					}
					if hit == 0 {
						t.Fatalf("no %q election was posed", tc.kind)
					}
				}, tc.src)
				if st.Served < 1 || st.LegacySwitch != 0 || st.Aborts != 0 {
					t.Fatalf("%s: not served from the tape: %+v", tc.name, st)
				}
			})
		}
	}
}

// A roll published before a mid-resolution target ask is still published
// after it (the Numbing Jellyfish shape the dual-run fuzz caught): the legacy
// resume used to rebuild its Ctx without the roll, so the target milled
// nothing on legacy and the die's result on the kernel. Both mill it now.
func TestTapeRollSurvivesTargetAsk(t *testing.T) {
	src := tapeZoneETB("Tape Jelly", "SVar:TrigBody:DB$ RollDice | ResultSVar$ Result | SubAbility$ DBMill\nSVar:DBMill:DB$ Mill | ValidTgts$ Player | NumCards$ Result")
	for _, tape := range []bool{false, true} {
		e, _ := tapeFixture(t, 2, 33001, tape, src)
		before := len(e.G.Zone(state.ZGraveyard, 0)) + len(e.G.Zone(state.ZGraveyard, 1))
		tapeCastAndResolve(t, e, "Tape Jelly", "B")
		if milled := len(e.G.Zone(state.ZGraveyard, 0)) + len(e.G.Zone(state.ZGraveyard, 1)) - before; milled == 0 {
			t.Fatalf("tape=%v: the rolled result milled nothing", tape)
		}
	}
	tapeZoneCase(t, 2, 33001, 1, nil, "Tape Jelly", src)
}

// A second ask inside a resumed RepeatEach iteration still binds the loop's
// subject (the Wave of Vitriol shape the dual-run fuzz caught): an Optional$
// search per sacrificed land, fetched by ImprintedController. The legacy
// resume of the confirmation re-entered the body loop-bound, but the search
// ask it then posed lost the subject, so the answered search moved nothing
// and the next iteration's confirmation came instead.
func TestTapeNestedAskKeepsRepeatSubject(t *testing.T) {
	src := tapeZoneSorcery("Tape Vitriol",
		"A:SP$ SacrificeAll | ValidCards$ Land | RememberSacrificed$ True | SubAbility$ DBRepeat\n"+
			"SVar:DBRepeat:DB$ RepeatEach | DefinedCards$ DirectRemembered.Land | UseImprinted$ True | RepeatSubAbility$ DBSearch | ClearRemembered$ True\n"+
			"SVar:DBSearch:DB$ ChangeZone | Origin$ Library | Destination$ Battlefield | ChangeType$ Land.Basic | Tapped$ True | DefinedPlayer$ ImprintedController | Chooser$ ImprintedController | NoShuffle$ True | Optional$ True")
	setup := tapeZoneToBattlefield(2)
	drive := func(t *testing.T, e *Engine) int {
		setup(t, e)
		addMana(t, e, 0, "B")
		submitChoices(t, e, castOptionFor(t, e, fixtureInHand(t, e, "Tape Vitriol")).Index)
		searches := 0
		for i := 0; i < 400; i++ {
			d := e.Pending()
			if d == nil || e.G.Over {
				break
			}
			if d.Kind == decision.KPriority {
				if len(e.G.Stack) == 0 {
					break
				}
				submitChoices(t, e, tapePassIndex(d))
				continue
			}
			pick := tapePick(d)
			switch d.ResumeKind {
			case "search_confirm":
				pick = []int{0}
			case "search":
				pick = []int{0}
				searches++
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: pick}); err != nil {
				t.Fatalf("submit: %v", err)
			}
		}
		return searches
	}
	for _, tape := range []bool{false, true} {
		e, _ := tapeFixture(t, 2, 33101, tape, src)
		searches := drive(t, e)
		lands := len(e.G.Zone(state.ZBattlefield, 0)) + len(e.G.Zone(state.ZBattlefield, 1))
		if searches != 4 || lands != 4 {
			t.Fatalf("tape=%v: %d searches answered, %d lands back (want 4 and 4)", tape, searches, lands)
		}
	}
	_, st := tapeDual(t, 2, 33101, func(t *testing.T, e *Engine) { drive(t, e) }, src)
	if st.Served < 8 || st.LegacySwitch != 0 || st.Aborts != 0 {
		t.Fatalf("the loop's asks were not served from the tape: %+v", st)
	}
}
