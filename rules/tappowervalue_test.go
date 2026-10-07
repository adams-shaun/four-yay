package rules

// tappowervalue_test.go — the stat:TapPowerValue mechanism (CR 702.150a's
// Station amount) and its corpus census.
//
// Tapestry Warden replaces the power a toughness>power creature stations
// with for its controller:
//
//	S:Mode$ TapPowerValue | ValidCard$ Creature.powerLTtoughness+YouCtrl | ValidSA$ Activated.Station | Value$ Toughness
//
// The engine's Station flow (rules/station.go) now reads the amount through
// the ONE shared helper Engine.tapPowerValue, which honours a TapPowerValue
// static scoped to the activated action's kind (ValidSA$) and matching the
// tapped creature (ValidCard$). Until this ticket the static was read
// nowhere, so a 0/4 wall charged ZERO charge counters instead of 4
// (TestSetAudit_eoe_TapestryWarden_StationUsesToughness, now unguarded).
//
// The census at the bottom names all 10 corpus carriers, so a corpus-pin
// bump that adds or drops one fails by name instead of silently drifting.

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// tpvRunStation stations Hearthhull, the Worldseed with the given payer,
// optionally with one TapPowerValue carrier on the battlefield, and returns
// the CHARGE counters Hearthhull accrued. A carrierName of "" is the
// no-static control.
func tpvRunStation(t *testing.T, reg *cards.Registry, carrierName string, payer *cards.Card, wantPower, wantTough int32) int32 {
	t.Helper()
	extras := []*cards.Card{lookup(t, reg, "Hearthhull, the Worldseed"), payer}
	if carrierName != "" {
		extras = append(extras, lookup(t, reg, carrierName))
	}
	e := corpusEngine(t, reg, extras, nil)
	hearth := moveByName(t, e, 0, "Hearthhull, the Worldseed", state.ZBattlefield)
	if carrierName != "" {
		moveByName(t, e, 0, carrierName, state.ZBattlefield)
	}
	payerID := moveByName(t, e, 0, payer.Faces[0].Name, state.ZBattlefield)

	// Precondition: the payer's printed-then-layer-derived P/T is what the
	// test expects, and the two values differ where the case depends on the
	// read choosing between them. A vacuous setup fails here, not silently.
	if p, tough := e.Power(payerID), e.Toughness(payerID); p != wantPower || tough != wantTough {
		t.Fatalf("precondition: payer is %d/%d, want %d/%d", p, tough, wantPower, wantTough)
	}

	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision (got %+v)", d)
	}
	idx := findStationOption(d, hearth)
	if idx < 0 {
		t.Fatalf("no Station option for Hearthhull: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit station: %v", err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no tap pick after Station (got %+v)", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == payerID {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("the payer was not offered as a Station payer: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
		t.Fatalf("submit tap pick: %v", err)
	}
	// The handler ran: the payer is tapped and the CHARGE event is recorded
	// through the normal fold, so a "zero counters" reading cannot come from
	// Station never having executed.
	if !e.G.Obj(payerID).Tapped {
		t.Fatal("precondition: Station did not tap the payer (handler did not run)")
	}
	return e.G.Obj(hearth).Counter("CHARGE")
}

// TestTapPowerValueStationMechanism is the focused mechanism test, independent
// of any one card: it pins the three readings the shared helper must produce
// -- plain power (no static), TOUGHNESS replacement, and the additive
// numeric form -- and that a static scoped to a DIFFERENT activated action
// (Crew) does not leak into Station.
func TestTapPowerValueStationMechanism(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	wall := card(t, "Name:Big Wall\nManaCost:2\nTypes:Creature Wall\nPT:0/4\nOracle:x\n")
	// A 3/1: power and toughness differ, and the numeric +2 reading (5) and
	// the toughness reading (1) both differ from plain power (3), so each
	// case is discriminated.
	bruiser := card(t, "Name:Big Bruiser\nManaCost:2\nTypes:Creature Beast\nPT:3/1\nOracle:x\n")

	cases := []struct {
		name     string
		carrier  string
		payer    *cards.Card
		pow, tgh int32
		want     int32
		wantWhy  string
	}{
		{"no static reads power", "", wall, 0, 4, 0, "CR 702.150a baseline"},
		{"Tapestry Warden substitutes toughness", "Tapestry Warden", wall, 0, 4, 4, "Value$ Toughness"},
		{"Stoic Star-Captain adds 2", "Stoic Star-Captain", bruiser, 3, 1, 5, "Value$ 2 (power+2)"},
		{"Giant Ox is Crew-scoped, not Station", "Giant Ox", bruiser, 3, 1, 3, "ValidSA$ Activated.Crew+Vehicle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.want == tc.pow && tc.want == tc.tgh {
				t.Fatalf("test cannot fail: want %d equals both power %d and toughness %d", tc.want, tc.pow, tc.tgh)
			}
			if got := tpvRunStation(t, reg, tc.carrier, tc.payer, tc.pow, tc.tgh); got != tc.want {
				t.Fatalf("Station charged %d CHARGE counters, want %d (%s)", got, tc.want, tc.wantWhy)
			}
		})
	}
}

// TestTapPowerValueSAScope: the ValidSA$ parser admits only the activated
// action kinds the static names. A Crew/Saddle-scoped static must not reach
// Station; a bare or malformed scope fails closed.
func TestTapPowerValueSAScope(t *testing.T) {
	cases := []struct {
		validSA string
		saKind  string
		want    bool
	}{
		{"Activated.Station", "Station", true},
		{"Activated.Crew+Vehicle", "Crew", true},
		{"Activated.Crew+Vehicle", "Station", false},
		{"Activated.Saddle+Mount,Activated.Crew+Vehicle", "Saddle", true},
		{"Activated.Saddle+Mount,Activated.Crew+Vehicle", "Crew", true},
		{"Activated.Crew+Vehicle,Activated.Station", "Station", true},
		{"", "Station", true},
		{"Spell.Station", "Station", false},
		{"Activated.Unknown", "Station", false},
	}
	for _, tc := range cases {
		if got := tapPowerSAScopeMatches(tc.validSA, tc.saKind); got != tc.want {
			t.Errorf("tapPowerSAScopeMatches(%q, %q) = %v, want %v", tc.validSA, tc.saKind, got, tc.want)
		}
	}
}

// tapPowerCarrier is one S:Mode$ TapPowerValue corpus line: the card, the
// activated-action kinds its ValidSA$ scopes, and its Value$ class.
type tapPowerCarrier struct {
	card    string
	station bool // ValidSA$ includes the Station action
	value   string
}

// tapPowerValueCarriers is every corpus card that carries an
// `S:Mode$ TapPowerValue` line, measured on the Makefile's FORGE_REF pin:
//
//	/usr/bin/grep -rl 'TapPowerValue' .cards/cardsfolder | wc -l  ==  10
//
// Nine Pilot-family cards replace only Crew/Saddle readings; Tapestry Warden
// and Stoic Star-Captain also scope the Station action this ticket wired.
var tapPowerValueCarriers = []tapPowerCarrier{
	{"Cloudspire Captain", false, "2"},
	{"Deathless Pilot", false, "2"},
	{"Dragonfly Pilot", false, "2"},
	{"Dynamite Diver", false, "2"},
	{"Experimental Pilot", false, "2"},
	{"Giant Ox", false, "Toughness"},
	{"Hotshot Mechanic", false, "2"},
	{"Interface Ace", false, "Toughness"},
	{"Stoic Star-Captain", true, "2"},
	{"Tapestry Warden", true, "Toughness"},
}

// TestTapPowerValueCensus pins the class in both directions: every expected
// carrier still carries the static with the expected Value$ and Station
// scope, and no unexpected card joined the set. A corpus-pin bump that adds
// a carrier fails by name rather than silently joining the untested class.
func TestTapPowerValueCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	// card name -> its TapPowerValue static.
	found := map[string]cards.Static{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, st := range f.Statics {
				if st.Mode == "TapPowerValue" {
					found[f.Name] = st
				}
			}
		}
	}

	expected := map[string]tapPowerCarrier{}
	for _, tc := range tapPowerValueCarriers {
		expected[tc.card] = tc
	}

	var names []string
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) != len(tapPowerValueCarriers) {
		t.Fatalf("TapPowerValue census: %d carriers %v, want %d", len(names), names, len(tapPowerValueCarriers))
	}
	for _, tc := range tapPowerValueCarriers {
		st, ok := found[tc.card]
		if !ok {
			t.Errorf("%s no longer carries a TapPowerValue static (expected Station scope %v, Value %q)", tc.card, tc.station, tc.value)
			continue
		}
		if got := strings.TrimSpace(st.Params["Value"]); got != tc.value {
			t.Errorf("%s TapPowerValue Value$ = %q, want %q", tc.card, got, tc.value)
		}
		if got := tapPowerSAScopeMatches(st.Params["ValidSA"], "Station"); got != tc.station {
			t.Errorf("%s TapPowerValue Station scope = %v, want %v (ValidSA %q)", tc.card, got, tc.station, st.Params["ValidSA"])
		}
	}
}
