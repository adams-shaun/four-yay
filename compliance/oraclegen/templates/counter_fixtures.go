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
		if !strings.EqualFold(fields[1], "Any") || len(fields) < 3 || !strings.EqualFold(fields[2], "CARDNAME") {
			return 0, "", false
		}
		kind = "M1M1"
	default:
		return 0, "", false
	}
	return int32(v), kind, true
}

// addActivationCounterFixtures gives p0's source card the counters its cost
// removes, so the activation is payable. A kind the face's own etbCounter
// keyword already enters with is seeded only for the shortfall: the setup
// placement is a real battlefield entry that applies the entry counters, so
// seeding the full count on top double-counts them and kills the source (a
// Flitterwing Nuisance seeded a second -1/-1 counter dies before the probe).
func addActivationCounterFixtures(p0 *oraclegen.Seat, f *cards.Face, name, cost string) {
	for _, tok := range costTokens(cost) {
		if n, kind, ok := sourceCounterCost(tok); ok {
			covered := int32(etbCounterCount(f, kind))
			if covered >= n {
				continue
			}
			*p0 = withSetupCounters(*p0, name, kind, n-covered)
		}
	}
}

// etbCounterCount is how many counters of kind the face's etbCounter keyword
// enters with ("K:etbCounter:M1M1:2"), 0 when it enters with none.
func etbCounterCount(f *cards.Face, kind string) int {
	if f == nil {
		return 0
	}
	for _, k := range f.Keywords {
		head, rest, ok := strings.Cut(k, ":")
		if !ok || !strings.EqualFold(head, "etbCounter") {
			continue
		}
		fkind, count, _ := strings.Cut(rest, ":")
		if !strings.EqualFold(strings.TrimSpace(fkind), kind) {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(count)); err == nil {
			return n
		}
	}
	return 0
}
