package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// TestCostStaticCastProvenanceProbes covers the cast-provenance cost-static
// rows that needed a probe cast from a zone other than the hand, or a
// permanent that has an Adventure:
//
//   - Bilbo (Card.!wasCastFromYourHand), Doc Aurlock
//     (Card.wasCastFromYourGraveyard,Card.wasCastFromExile) and Norman
//     Osborn's Green Goblin face (Card.wasCastFromYourGraveyard) are served
//     by a FLASHBACK cast from the probe's own graveyard, the one non-hand
//     cast the shape can script. The origin-zone rows (Doc, Norman) pay the
//     honest reduced price: the engine's pre-push OFFER window answers their
//     provenance from the object's current zone. The hand-family row (Bilbo)
//     keeps that window's fail-closed deny, so its probe is OFFERED at the
//     whole flashback price and the discount surfaces as the mana the
//     reduced payment leaves floating.
//   - Beluna (Permanent.AdventureCard) is served by the front face of a
//     two-faced Adventure card cast from the hand at the reduced price.
//
// Each row's probe must play through gorge, and the same probe must lose the
// discount once the static's own face is muted, so a probe paying the
// printed flashback price (or an engine that ignores the reduction) cannot
// pass.
func TestCostStaticCastProvenanceProbes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key string
		face      int // the requirement's face: the reduction's home
		flashback bool
		fullPrice bool // the OFFER-deny shape: the probe pays the whole flashback price
	}{
		{"Bilbo, Thief in the Night", "static#0.0", 0, true, true},
		{"Doc Aurlock, Grizzled Genius", "static#0.0", 0, true, false},
		{"Norman Osborn", "static#1.0", 1, true, false},
		{"Beluna Grandsquall", "static#0.0", 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := costRequirement(t, reg, tc.name, tc.key)
			if req.Face != tc.face {
				t.Fatalf("precondition: %s %s on face %d, table says %d", tc.name, tc.key, req.Face, tc.face)
			}
			src, ok := reg.Lookup(tc.name)
			if !ok || len(src.Faces) <= tc.face {
				t.Fatalf("precondition: %s face %d absent", tc.name, tc.face)
			}
			st := src.Faces[tc.face].Statics[atoi(t, req.Slot)]
			if st.ModeKind() != cards.StaticReduceCost {
				t.Fatalf("precondition: %s %s is not a ReduceCost static", tc.name, tc.key)
			}
			reduction := atoi(t, st.Params["Amount"])
			if reduction < 1 {
				t.Fatalf("precondition: %s %s reduces %d", tc.name, tc.key, reduction)
			}
			it, skip := GenerateB(reg, tc.name, req)
			if skip != nil {
				t.Fatalf("GenerateB: %s", skip.Reason)
			}
			probe := probeStep(t, it)
			if probe.Op != "cast" {
				t.Fatalf("probe op = %s, want cast", probe.Op)
			}
			if probe.Card == "p0:"+tc.name {
				t.Fatalf("probe is the reducer itself")
			}
			probeName := strings.TrimPrefix(probe.Card, "p0:")
			c, ok := reg.Lookup(probeName)
			if !ok {
				t.Fatalf("precondition: probe %s absent", probeName)
			}
			p0 := it.Scenario.Setup["p0"]
			found := false
			for _, permanent := range p0.Battlefield {
				found = found || permanent == tc.name
			}
			if !found {
				t.Fatalf("precondition: reduction source %q absent from p0's battlefield", tc.name)
			}
			if tc.face > 0 && !hasName(p0.BackFace, tc.name) {
				t.Fatalf("precondition: alternate-face source %q not placed on its back face", tc.name)
			}
			res := runSteps(t, reg, it.Scenario, it.Steps)
			if len(res.Fails) != 0 {
				t.Fatalf("probe fails with the static present: %v", res.Fails)
			}
			last := res.Snapshots[len(res.Snapshots)-1]
			if tc.flashback {
				flashbackCost, ok := c.Faces[0].KeywordCostParam("Flashback")
				if !ok {
					t.Fatalf("precondition: probe %s has no Flashback", probeName)
				}
				pool, why := oraclegen.PoolFor(flashbackCost)
				if why != "" {
					t.Fatalf("precondition: %s flashback %q: %s", probeName, flashbackCost, why)
				}
				if probe.CastMode != "flashback" {
					t.Fatalf("probe cast_mode = %q, want flashback", probe.CastMode)
				}
				if !hasName(p0.Graveyard, probeName) {
					t.Fatalf("precondition: probe %s not seeded in p0's graveyard", probeName)
				}
				if hasName(p0.Hand, probeName) {
					t.Fatalf("precondition: probe %s also in p0's hand -- the cast ref is ambiguous", probeName)
				}
				if strings.Count(pool, "C") < reduction {
					t.Fatalf("precondition: %s flashback %q (%s) carries %d generic, want >= %d", probeName, flashbackCost, pool, strings.Count(pool, "C"), reduction)
				}
				if tc.fullPrice {
					if probe.Mana != pool {
						t.Fatalf("probe pays %q, want the whole flashback price %q: the OFFER-deny shape is offered at full price and discounted at payment", probe.Mana, pool)
					}
					// The discount surfaces as the mana the reduced payment
					// leaves floating: empty only when the static is gone.
					if last.Players[0].Pool == "" {
						t.Fatalf("precondition: discounted payment left no floating mana, so %s's reduction is not visible", tc.name)
					}
					res2 := runSteps(t, withoutFaceStatics(reg, tc.name, tc.face), it.Scenario, it.Steps)
					if len(res2.Fails) != 0 {
						t.Fatalf("full-price probe fails without the static: %v", res2.Fails)
					}
					last2 := res2.Snapshots[len(res2.Snapshots)-1]
					if last2.Players[0].Pool != "" {
						t.Fatalf("probe is not sensitive to %s's cost reduction: pool %q is also empty with the static muted", tc.name, last2.Players[0].Pool)
					}
					return
				}
				if got := len(pool) - len(probe.Mana); got != reduction {
					t.Fatalf("precondition: probe pays %q, flashback price %q differ by %d, want the static's reduction %d", probe.Mana, pool, got, reduction)
				}
			} else {
				// The AdventureCard probe casts the front face of a two-faced
				// Adventure card from the hand at the printed price minus the
				// reduction.
				if c.AlternateMode != "Adventure" || len(c.Faces) != 2 || !adventureCardMatches(c) {
					t.Fatalf("precondition: probe %s is not an Adventure card", probeName)
				}
				if probe.CastMode != "" {
					t.Fatalf("probe cast_mode = %q, want the ordinary cast", probe.CastMode)
				}
				printed, why := oraclegen.PoolFor(c.Faces[0].ManaCost)
				if why != "" {
					t.Fatalf("precondition: %s prints %q: %s", probeName, c.Faces[0].ManaCost, why)
				}
				if got := len(printed) - len(probe.Mana); got != reduction {
					t.Fatalf("precondition: probe pays %q, printed price %q differ by %d, want the static's reduction %d", probe.Mana, printed, got, reduction)
				}
				// The discounted creature cast paid its exact reduced price
				// and the creature entered: the muted run's cheapest cast is
				// the Adventure face, which never puts the creature on the
				// battlefield, so the entry is what pins the reduction.
				if last.Players[0].Pool != "" {
					t.Fatalf("discounted cast left pool %q: not an exact reduced price", last.Players[0].Pool)
				}
				if !permNamed(last.Permanents, probeName) {
					t.Fatalf("precondition: the discounted cast did not put %s on the battlefield", probeName)
				}
			}
			res2 := runSteps(t, withoutFaceStatics(reg, tc.name, tc.face), it.Scenario, it.Steps)
			if tc.flashback {
				if len(res2.Fails) == 0 {
					t.Fatalf("probe is not sensitive to %s's cost reduction", tc.name)
				}
				return
			}
			last2 := res2.Snapshots[len(res2.Snapshots)-1]
			if permNamed(last2.Permanents, probeName) {
				t.Fatalf("probe is not sensitive to %s's cost reduction: %s still entered", tc.name, probeName)
			}
		})
	}
}

// permNamed reports whether one snapshot permanent names want.
func permNamed(perms []rules.OracleSnapPerm, want string) bool {
	for _, p := range perms {
		if p.Name == want {
			return true
		}
	}
	return false
}

// withoutFaceStatics is an independent registry in which the face-th face of
// name has no statics, so the reduction that face carries is absent and
// nothing else changed (cost_test.go's withoutStatics mutes face 0 only,
// which misses an alternate-face reduction like Norman Osborn's).
func withoutFaceStatics(reg *cards.Registry, name string, face int) *cards.Registry {
	muted := cards.NewRegistry()
	muted.Tokens = reg.Tokens
	for _, c := range reg.AllCards() {
		if c.Faces[0].Name == name {
			cc := *c
			cc.Faces = append([]*cards.Face(nil), c.Faces...)
			ff := *c.Faces[face]
			ff.Statics = nil
			cc.Faces[face] = &ff
			c = &cc
		}
		muted.Add(c)
	}
	return muted
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("precondition: %q is not a number", s)
	}
	return n
}
