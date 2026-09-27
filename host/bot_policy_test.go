package host

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/view"
)

func TestNormalizeBotPolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults to bot", want: "bot"},
		{name: "production bot", in: "bot", want: "bot"},
		{name: "experimental lethal pressure", in: "lethal-pressure", want: "lethal-pressure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeBotPolicy(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeBotPolicy(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	for _, name := range []string{"legacy", "random", "Bot"} {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := NormalizeBotPolicy(name)
			if err == nil {
				t.Fatalf("NormalizeBotPolicy(%q) accepted an unsupported hosted policy", name)
			}
			if !strings.Contains(err.Error(), "bot, lethal-pressure") {
				t.Fatalf("error %q does not name the deterministic hosted policy set", err)
			}
		})
	}
}

func TestNewBotPolicySeatIsDeterministic(t *testing.T) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "pass"}, {Index: 1, Kind: "cast"}}}
	for _, policy := range []string{"bot", "lethal-pressure"} {
		t.Run(policy, func(t *testing.T) {
			a, err := NewBotPolicySeat(policy, 19)
			if err != nil {
				t.Fatal(err)
			}
			b, err := NewBotPolicySeat(policy, 19)
			if err != nil {
				t.Fatal(err)
			}
			ia, err := a.Decide(context.Background(), view.View{}, d)
			if err != nil {
				t.Fatal(err)
			}
			ib, err := b.Decide(context.Background(), view.View{}, d)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ia.Choices, ib.Choices) {
				t.Fatalf("same seed chose %v and %v", ia.Choices, ib.Choices)
			}
		})
	}
}

func TestNewBotPolicySeatWithAutoPayManaSelectsOfferedWitness(t *testing.T) {
	b, err := NewBotPolicySeatWithAutoPayMana(BotPolicy, 19, true)
	if err != nil {
		t.Fatal(err)
	}
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options:        []decision.Option{{Index: 0, Kind: "activate"}, {Index: 1, Kind: "pass"}},
		PaymentActions: []decision.PaymentAction{{ID: "action", Cast: decision.PlannedCast{Object: 9, Origin: "hand"}, Plans: []decision.PaymentPlan{{ID: "plan", Version: decision.PaymentPlanV1}}}},
	}
	in, err := b.Decide(context.Background(), view.View{Phase: "main1", Viewer: 0, Active: 0,
		Players: []view.PlayerView{{ID: 0, Hand: []view.CardView{{ID: 9, Types: "Creature", Power: 3, ManaCost: "2 G"}}}}}, d)
	if err != nil {
		t.Fatal(err)
	}
	if in.Payment == nil || in.Payment.ActionID != "action" || in.Payment.Plan.ID != "plan" {
		t.Fatalf("intent = %+v, want offered auto-payment witness", in)
	}
}

func TestAutoManaFeatureGateDisablesBotPaymentPlans(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  TableConfig
		want bool
	}{
		{name: "feature disabled wins over bot flag", cfg: TableConfig{AutoMana: false, BotAutoPayMana: true}, want: false},
		{name: "feature enabled bot flag off", cfg: TableConfig{AutoMana: true, BotAutoPayMana: false}, want: false},
		{name: "both explicitly enabled", cfg: TableConfig{AutoMana: true, BotAutoPayMana: true}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.autoPayManaEnabled(); got != tc.want {
				t.Fatalf("autoPayManaEnabled() = %v, want %v for %+v", got, tc.want, tc.cfg)
			}
		})
	}
}
