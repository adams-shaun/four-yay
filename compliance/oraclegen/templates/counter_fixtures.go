// Counter and speed fixtures for the level-B static and activate templates
// (ticket levelb-setup-counters-speed). A setup permanent can start with
// counters and a seat with a speed (rules/oracle_setup_state.go), so:
//
//   - a static gated on its own counters (a Spacecraft's `Card.Self+
//     counters_GE8_CHARGE`, "as long as it has a +1/+1 counter") is probed with
//     the card on the battlefield holding exactly that many counters;
//   - an activated ability whose cost removes counters from its source
//     (SubCounter<n/KIND>, RemoveAnyCounter<n/Any/CARDNAME>) is activated with
//     the card holding the counters the cost removes;
//   - a static gated on Condition$ MaxSpeed is probed at speed 4.
package templates

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// maxSpeed is the speed Condition$ MaxSpeed needs (CR 702.179b).
const maxSpeed = 4

// selfCounterGate matches the `Card.Self+counters_GE<n>_<KIND>` gate a static
// carries in Affected$ (a Spacecraft's own station) or IsPresent$ ("as long as
// CARDNAME has a shield counter on it").
var selfCounterGate = regexp.MustCompile(`Card\.Self\+counters_GE([0-9]+)_([A-Za-z0-9]+)`)

// probeCounterGate matches the affected-permanent counter gate word the static
// probe fixture places counters for (counters_GE1_P1P1).
var probeCounterGate = regexp.MustCompile(`(?i)counters_GE([0-9]+)_([A-Za-z0-9]+)`)

// staticCounterGate returns the counter kind and count st's own-counter gate
// needs, or ok=false when st has none.
func staticCounterGate(st *cards.Static) (kind string, n int32, ok bool) {
	for _, k := range []cards.ParamKey{cards.PKAffected, cards.PKIsPresent} {
		m := selfCounterGate.FindStringSubmatch(st.ParamStr(k))
		if m == nil {
			continue
		}
		v, err := strconv.Atoi(m[1])
		if err != nil || v <= 0 {
			return "", 0, false
		}
		return m[2], int32(v), true
	}
	return "", 0, false
}

// staticSelfCounterGate returns the counter kind and count a self static's
// CheckSVar$ X gate needs where SVar:X:Count$CardCounters.<KIND> (Warden of
// the Inner Sky, Ezio Brash Novice, Hero of Bretagard). ok is false unless the
// static's Affected$ is the card itself and the compare is GE/GT: a LT/LE
// gate wants the counters ABSENT, so placing them would falsify it. A missing
// compare is Forge's implicit GE1.
func staticSelfCounterGate(f *cards.Face, st *cards.Static) (kind string, n int32, ok bool) {
	if !strings.EqualFold(strings.TrimSpace(st.ParamStr(cards.PKAffected)), "Card.Self") {
		return "", 0, false
	}
	check := strings.TrimSpace(st.ParamStr(cards.PKCheckSVar))
	if check == "" {
		return "", 0, false
	}
	body, has := f.SVars[check]
	if !has {
		body = check
	}
	body = strings.TrimSpace(body)
	const prefix = "Count$CardCounters."
	if len(body) < len(prefix) || !strings.EqualFold(body[:len(prefix)], prefix) {
		return "", 0, false
	}
	kind = strings.ToUpper(strings.TrimSpace(body[len(prefix):]))
	if i := strings.IndexByte(kind, '/'); i >= 0 {
		kind = kind[:i]
	}
	if kind == "" {
		return "", 0, false
	}
	if kind == "ALL" {
		kind = "P1P1"
	}
	cmp := strings.ToUpper(strings.TrimSpace(st.ParamStr(cards.PKSVarCompare)))
	switch {
	case cmp == "":
		n = 1
	case strings.HasPrefix(cmp, "GE"), strings.HasPrefix(cmp, "GT"):
		v, err := strconv.Atoi(cmp[2:])
		if err != nil || v <= 0 {
			return "", 0, false
		}
		n = int32(v)
		if strings.HasPrefix(cmp, "GT") {
			n++
		}
	default:
		return "", 0, false
	}
	return kind, n, true
}

// stationGatedSelf reports a CHARGE-gated static on a Spacecraft (the
// card's station static, the EOE ship cycle) whose cast path cannot reach
// the gate. Its counters are the fixture's, and the card's own
// enters-the-battlefield trigger is an entry shape the xmageFixture placement
// drops (setupPlacementDropsTrigger) -- the same silence XMage's addCard has
// -- so the placed card serves like any other counter-gated card instead of
// staying on the cast path, where nothing puts the gate counters on it. An
// ETB that puts counters of ANY kind keeps the cast path (Atmospheric
// Greenhouse's "put a +1/+1 counter on each creature you control" is the
// served story its row asserts): the placed path is only for ETBs that could
// never make the compared fields move.
func stationGatedSelf(f *cards.Face, st *cards.Static) bool {
	kind, _, _ := staticCounterGate(st)
	if !strings.EqualFold(kind, "CHARGE") || !oraclegen.HasType(f, "Spacecraft") {
		return false
	}
	for i := range f.Triggers {
		body, ok := f.SVars[f.Triggers[i].ParamStr(cards.PKExecute)]
		if ok && strings.Contains(body, "PutCounter") {
			return false
		}
	}
	return true
}

// staticGatedOnMaxSpeed reports whether st applies only at max speed.
func staticGatedOnMaxSpeed(st *cards.Static) bool {
	return strings.EqualFold(st.ParamStr(cards.PKCondition), "MaxSpeed")
}

// withMaxSpeed puts p0 at max speed in setup, which is what Condition$
// MaxSpeed reads.
func withMaxSpeed(setup map[string]oraclegen.Seat) {
	p0 := setup["p0"]
	p0.Speed = maxSpeed
	setup["p0"] = p0
}

// staticSelfETB reports whether f has its own enters-the-battlefield trigger
// (ValidCard$ Card.Self, Destination$ Battlefield). A setup placement fires
// such a trigger in gorge, but XMage cheats setup permanents onto the
// battlefield before the game starts and never fires it, so its effect is
// gorge-only and the two engines diverge (Atmospheric Greenhouse's +1/+1
// counter, Kefka's discard). A counter-gated static on such a card therefore
// stays on the cast path, which fires the trigger in both engines.
func staticSelfETB(f *cards.Face) bool {
	for i := range f.Triggers {
		tr := &f.Triggers[i]
		if !strings.Contains(strings.ToLower(tr.ParamStr(cards.PKValidCard)), "card.self") {
			continue
		}
		if strings.Contains(strings.ToLower(tr.ParamStr(cards.PKDestination)), "battlefield") {
			return true
		}
	}
	return false
}

// staticGrantsAbility reports whether st grants an ability, trigger, static or
// replacement effect (the shapes the probes cannot observe).
func staticGrantsAbility(st *cards.Static) bool {
	return st.HasParam(cards.PKAddAbility) || st.HasParam(cards.PKAddTrigger) ||
		st.HasParam(cards.PKAddStaticAbility) || st.Params["AddReplacementEffect"] != ""
}

// withSetupCounters returns s with n counters of kind on its battlefield card
// name, leaving s's own counters map untouched (fixtures share seats).
func withSetupCounters(s oraclegen.Seat, name, kind string, n int32) oraclegen.Seat {
	counters := map[string]map[string]int32{}
	for k, kinds := range s.Counters {
		counters[k] = map[string]int32{}
		for kk, v := range kinds {
			counters[k][kk] = v
		}
	}
	if counters[name] == nil {
		counters[name] = map[string]int32{}
	}
	counters[name][kind] += n
	s.Counters = counters
	return s
}

// sourceCounterCost parses the counter-removal cost tokens whose counters
// come off the ability's own source with a literal count: SubCounter<n/KIND>,
// SubCounter<n/KIND/CARDNAME> (or NICKNAME) and RemoveAnyCounter<n/Any/
// CARDNAME/this creature>. kind is the counter kind the fixture must place:
// the named kind, or M1M1 for "any" -- the ECL blight creatures all enter with
// -1/-1 counters, so that is the counter they have, and a lone kind leaves no
// which-kind choice for either engine (a +1/+1 counter beside a -1/-1 one
// would cancel). A planeswalker's LOYALTY cost is not a fixture
// (loyaltyCounter owns it), nor is a cost that takes counters from another
// permanent or announces X.
func sourceCounterCost(tok string) (n int32, kind string, ok bool) {
	payload, has := bracketPayload(tok)
	if !has {
		return 0, "", false
	}
	fields := strings.Split(payload, "/")
	if len(fields) < 2 {
		return 0, "", false
	}
	v, err := strconv.Atoi(fields[0])
	if err != nil || v <= 0 {
		return 0, "", false
	}
	switch strings.TrimSpace(tok[:strings.IndexByte(tok, '<')]) {
	case "SubCounter":
		kind = strings.TrimSpace(fields[1])
		if strings.EqualFold(kind, "LOYALTY") || strings.EqualFold(kind, "Any") {
			return 0, "", false
		}
		if len(fields) >= 3 && !strings.EqualFold(fields[2], "CARDNAME") && !strings.EqualFold(fields[2], "NICKNAME") {
			return 0, "", false
		}
	case "RemoveAnyCounter":
		// NICKNAME is the back face's own-name spelling (Ghost-Spider's
		// RemoveAnyCounter<2/Any/NICKNAME>), the same source anchor CARDNAME is.
		if !strings.EqualFold(fields[1], "Any") || len(fields) < 3 ||
			(!strings.EqualFold(fields[2], "CARDNAME") && !strings.EqualFold(fields[2], "NICKNAME")) {
			return 0, "", false
		}
		kind = "M1M1"
	default:
		return 0, "", false
	}
	return int32(v), kind, true
}

// otherCounterCost parses the counter-removal cost tokens whose counters come
// off a battlefield permanent the FIXTURE places, not the ability's own
// source: SubCounter<n/KIND/Creature.YouCtrl/...> (Sunstar Chaplain, Ray
// Fillet) and RemoveAnyCounter<n/KIND/Artifact> (Iron Spider). The fixture
// card is the single object matching the filter on p0's battlefield, so the
// engine's removal stage has one candidate and auto-picks it without posing a
// removal ask. A wildcard kind ("Any") is not fixture-supported: the kinds the
// bearer would carry are a choice neither engine's answer can name cheaply.
func otherCounterCost(tok string) (n int32, kind, fixture string, ok bool) {
	payload, has := bracketPayload(tok)
	if !has {
		return 0, "", "", false
	}
	head := strings.TrimSpace(tok[:strings.IndexByte(tok, '<')])
	if head != "SubCounter" && head != "RemoveAnyCounter" {
		return 0, "", "", false
	}
	fields := strings.Split(payload, "/")
	if len(fields) < 3 {
		return 0, "", "", false
	}
	v, err := strconv.Atoi(fields[0])
	if err != nil || v <= 0 {
		return 0, "", "", false
	}
	kind = strings.TrimSpace(fields[1])
	if kind == "" || strings.EqualFold(kind, "Any") || strings.EqualFold(kind, "LOYALTY") {
		return 0, "", "", false
	}
	switch strings.TrimSpace(fields[2]) {
	case "Creature.YouCtrl":
		return int32(v), kind, staticProbe, true
	case "Artifact":
		return int32(v), kind, "Sol Ring", true
	}
	return 0, "", "", false
}

// announcedSourceCounterX reports whether tok is an announced SubCounter<X/
// KIND> or RemoveAnyCounter<X/KIND> part whose counters come off the ability's
// own source (or its own loyalty) with a fixed kind: the activation announces
// X = 1 and the fixture seeds one counter of the kind on the source (The
// Astonishing Ant-Man's SubCounter<X/P1P1>, Chandra, Chill of Compliance's
// SubCounter<X/LOYALTY>). A third field naming another permanent is not
// fixture-supported.
func announcedSourceCounterX(tok string) bool {
	payload, has := bracketPayload(tok)
	if !has {
		return false
	}
	head := strings.TrimSpace(tok[:strings.IndexByte(tok, '<')])
	if head != "SubCounter" && head != "RemoveAnyCounter" {
		return false
	}
	fields := strings.Split(payload, "/")
	if len(fields) < 2 || strings.TrimSpace(fields[0]) != "X" {
		return false
	}
	if len(fields) >= 3 {
		t := strings.TrimSpace(fields[2])
		if !strings.EqualFold(t, "CARDNAME") && !strings.EqualFold(t, "NICKNAME") {
			return false
		}
	}
	kind := strings.TrimSpace(fields[1])
	return kind != "" && !strings.EqualFold(kind, "Any")
}

// announcedSourceCounterXKind is the counter kind announcedSourceCounterX's
// fixture seeds on the source.
func announcedSourceCounterXKind(tok string) string {
	payload, has := bracketPayload(tok)
	if !has {
		return ""
	}
	fields := strings.Split(payload, "/")
	if len(fields) < 2 {
		return ""
	}
	return strings.TrimSpace(fields[1])
}

// addActivationCounterFixtures gives p0's source card the counters its cost
// removes, so the activation is payable.
func addActivationCounterFixtures(p0 *oraclegen.Seat, name, cost string) {
	for _, tok := range costTokens(cost) {
		if n, kind, ok := sourceCounterCost(tok); ok {
			*p0 = withSetupCounters(*p0, name, kind, n)
			continue
		}
		if n, kind, fixture, ok := otherCounterCost(tok); ok {
			p0.Battlefield = appendFixtureUnique(p0.Battlefield, fixture)
			*p0 = withSetupCounters(*p0, fixture, kind, n)
			continue
		}
		if announcedSourceCounterX(tok) {
			*p0 = withSetupCounters(*p0, name, announcedSourceCounterXKind(tok), 1)
		}
	}
}
