package effects

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The source's remembered list is event-backed, unlike the resolution-local
// list. Assert both and the actual clearing event for every entry path.
func TestForgetOtherRememberedPrimitives(t *testing.T) {
	cases := []struct {
		name, line, zone string
		token            bool
		wantNew          bool
	}{
		{"Dig", "DB$ Dig | Defined$ You | DigNum$ 1 | ChangeNum$ 1 | ChangeValid$ Card.IsRemembered | RememberChanged$ True", "library", false, true},
		// The reported corpus shape (Primal Surge's DBDig, Search the City's
		// SetupSearch): ChangeNum$ All with an Exile primary destination and
		// RememberChanged$ True. The first case is the line as written (the
		// ChangeValid$ default "Card" never reads memory); the second adds
		// ChangeValid$ Card.IsRemembered so the eligibility match exercises the
		// PRE-CLEAR snapshot (selection := *c) -- with the clear gone or ordered
		// after the match the window match fails and the card is never taken.
		{"DigAllExile", "DB$ Dig | Defined$ You | DigNum$ 1 | ChangeNum$ All | DestinationZone$ Exile | RememberChanged$ True", "library", false, true},
		{"DigAllExileIsRemembered", "DB$ Dig | Defined$ You | DigNum$ 1 | ChangeNum$ All | ChangeValid$ Card.IsRemembered | DestinationZone$ Exile | RememberChanged$ True", "library", false, true},
		{"DigUntil", "DB$ DigUntil | Valid$ Card.IsRemembered | FoundDestination$ Hand | RememberRevealed$ True", "library", false, true},
		{"TokenDB", "DB$ Token | TokenScript$ test_token | RememberTokens$ True", "battlefield", true, true},
		{"TokenAB", "AB$ Token | TokenScript$ test_token | RememberTokens$ True", "battlefield", true, true},
		{"Effect", "DB$ Effect | RememberObjects$ Remembered", "battlefield", false, false},
		{"ChooseCard", "DB$ ChooseCard | ChoiceZone$ Battlefield | Choices$ Card.IsRemembered | Mandatory$ True | RememberChosen$ True", "battlefield", false, true},
		{"Hand", "DB$ ChangeZone | Origin$ Hand | Destination$ Graveyard | ChangeType$ Card.IsRemembered | Mandatory$ True | RememberChanged$ True", "hand", false, true},
		{"HandOwners", "DB$ ChangeZone | Origin$ Hand | Destination$ Graveyard | DefinedPlayer$ You | ChangeType$ Card.IsRemembered | Mandatory$ True | RememberChanged$ True", "hand", false, true},
		{"ChangeZoneObject", "DB$ ChangeZone | Origin$ Battlefield | Destination$ Graveyard | Defined$ Remembered | RememberChanged$ True", "battlefield", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var absent []events.Event
			for _, flag := range []string{"", " | ForgetOtherRemembered$ True", " | ForgetOtherRemembered$ False"} {
				t.Run(strings.TrimSpace(flag), func(t *testing.T) {
					h := newHost(t, 2)
					src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Artifact\nOracle:x\n"), 0)
					old := h.g.AddObject(mkCard(t, "Name:Old\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
					fresh := h.g.AddObject(mkCard(t, "Name:Fresh\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
					h.Emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZBattlefield})
					h.Emit(events.Event{Kind: events.MoveZone, Obj: old.ID, From: state.ZLibrary, To: state.ZGraveyard})
					zone := state.ZBattlefield
					switch tc.zone {
					case "library":
						zone = state.ZLibrary
					case "hand":
						zone = state.ZHand
					}
					h.Emit(events.Event{Kind: events.MoveZone, Obj: fresh.ID, From: state.ZLibrary, To: zone})
					h.Emit(events.Event{Kind: events.Choose, Obj: src.ID, Counter: "remembered", IDs: []state.ObjID{old.ID, fresh.ID}})
					ctx := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: old.ID}, {Obj: fresh.ID}}}
					if tc.token {
						h.g.Tokens = make(map[string]*cards.Card)
						h.g.Tokens["test_token"] = mkCard(t, "Name:Test Token\nTypes:Creature\nPT:1/1\nOracle:x\n")
					}
					if len(h.g.Obj(src.ID).Remembered) != 2 || old.ID == fresh.ID || h.g.Obj(fresh.ID).Zone != zone {
						t.Fatalf("invalid fixture: memory %v, fresh zone %v", h.g.Obj(src.ID).Remembered, h.g.Obj(fresh.ID).Zone)
					}
					if (tc.name == "Dig" || tc.name == "DigUntil" || tc.name == "ChooseCard" || strings.HasPrefix(tc.name, "Hand") || tc.name == "DigAllExileIsRemembered") && !MatchesSpecCtx(h.g, "Card.IsRemembered", fresh.ID, ctx.SpecContext(0)) {
						t.Fatal("selector cannot see remembered fixture")
					}
					before := len(h.log)
					Resolve(h, ctx, sa(t, tc.line+flag))
					clear := 0
					for _, e := range h.log[before:] {
						if e.Kind == events.Choose && e.Obj == src.ID && e.Counter == "clear-remembered" {
							clear++
						}
					}
					enabled := strings.Contains(flag, "$ True")
					if flag == "" {
						absent = append([]events.Event(nil), h.log[before:]...)
					} else if !enabled && !reflect.DeepEqual(absent, h.log[before:]) {
						t.Fatalf("explicit False changed absent event bytes: %v versus %v", absent, h.log[before:])
					}
					if enabled && clear != 1 || !enabled && clear != 0 {
						t.Fatalf("clear events = %d, enabled %v; log %v", clear, enabled, h.log[before:])
					}
					mem := h.g.Obj(src.ID).Remembered
					if enabled && len(mem) > 0 && mem[0].Obj == old.ID {
						t.Fatalf("old source memory survived: %v", mem)
					}
					if !enabled && (len(mem) == 0 || mem[0].Obj != old.ID) {
						t.Fatalf("absent/false changed source memory: %v", mem)
					}
					if enabled {
						for _, v := range ctx.Remembered {
							if v.Obj == old.ID {
								t.Fatalf("old ctx memory survived: %v", ctx.Remembered)
							}
						}
					}
					if tc.wantNew && enabled {
						want := fresh.ID
						if tc.token {
							want = h.g.NextID - 1
						}
						if len(ctx.Remembered) != 1 || ctx.Remembered[0].Obj != want {
							t.Fatalf("new memory = %v, want only %d; log %v", ctx.Remembered, want, h.log[before:])
						}
						if tc.token && (len(mem) != 1 || mem[0].Obj != want) {
							t.Fatalf("new token not replay-backed on source: %v", mem)
						}
					}
					if tc.name == "Dig" || tc.name == "DigUntil" || strings.HasPrefix(tc.name, "DigAllExile") {
						want := state.ZHand
						if strings.Contains(tc.line, "DestinationZone$ Exile") {
							want = state.ZExile
						}
						if h.g.Obj(fresh.ID).Zone != want {
							t.Fatalf("selector did not take remembered library card: %v, want %v", h.g.Obj(fresh.ID).Zone, want)
						}
					}
					if strings.HasPrefix(tc.name, "Hand") {
						if h.g.Obj(fresh.ID).Zone != state.ZGraveyard {
							t.Fatalf("selector did not take remembered hand card: %v; log %v", h.g.Obj(fresh.ID).Zone, h.log[before:])
						}
					}
				})
			}
		})
	}
}
