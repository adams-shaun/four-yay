package main

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/host/httpapi"
	"github.com/adams-shaun/gorge/view"
)

// TestBotPolicyFlagsValidateAtStartup pins every startup refusal of the
// §9.4 policy flags (BP-18): an unknown default, an unknown offered name, a
// default missing from the offered set, a default that cannot play a format,
// and a duplicate in the offered list. A valid pairing must pass and return
// the offered set in registry order, and a blank default must normalize to
// bots.Default so a config built without the flag still names a policy.
//
// The test drives validateBotPolicyFlags directly rather than through
// serve(): serve opens the corpus, and these are pure flag checks that must
// fail before any listener opens.
func TestBotPolicyFlagsValidateAtStartup(t *testing.T) {
	// A policy that exists but does NOT play commander. If this precondition
	// is false the "lacks a format" case below would be vacuous, so assert
	// it loudly.
	search, ok := bots.Lookup("search")
	if !ok {
		t.Fatal("bots registry has no \"search\" entry; the format case cannot be set up")
	}
	if contains(search.Formats, host.FormatCommander.String()) {
		t.Fatalf("\"search\" unexpectedly supports commander (formats %v); pick a constructed-only policy", search.Formats)
	}
	// "bot" must play BOTH formats, or the valid case and the default-
	// normalization case would not exercise the success path.
	bot, ok := bots.Lookup(bots.Default)
	if !ok {
		t.Fatalf("bots registry has no default %q", bots.Default)
	}
	for _, f := range []host.Format{host.FormatConstructed, host.FormatCommander} {
		if !contains(bot.Formats, f.String()) {
			t.Fatalf("default %q lacks %s (formats %v); the valid case is not valid", bots.Default, f, bot.Formats)
		}
	}

	for _, tc := range []struct {
		name       string
		def        string
		offeredRaw string
		wantErr    string
	}{
		{
			name: "unknown default", def: "nope", offeredRaw: "bot,search",
			wantErr: "unknown bot policy",
		},
		{
			name: "unknown offered name", def: "bot", offeredRaw: "bot,not-a-policy",
			wantErr: "unknown bot policy",
		},
		{
			name: "default not in the offered set", def: "search", offeredRaw: "bot",
			wantErr: "is not in -bot-policies",
		},
		{
			name: "default lacks a format", def: "search", offeredRaw: "bot,search",
			wantErr: "does not support the commander format",
		},
		{
			name: "duplicate offered name", def: "bot", offeredRaw: "bot,bot",
			wantErr: "duplicate bot policy",
		},
		{
			name: "empty offered entry", def: "bot", offeredRaw: "bot,,",
			wantErr: "empty entry",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := validateBotPolicyFlags(tc.def, tc.offeredRaw)
			if err == nil {
				t.Fatalf("validateBotPolicyFlags(%q, %q) = nil error, want one containing %q", tc.def, tc.offeredRaw, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}

	for _, tc := range []struct {
		name       string
		def        string
		offeredRaw string
		wantDef    string
		wantOff    []string
	}{
		{
			name: "explicit list", def: "bot", offeredRaw: "search,bot",
			wantDef: "bot", wantOff: []string{"bot", "search"}, // registry order, not typed order
		},
		{
			name: "empty offers everything", def: "bot", offeredRaw: "",
			wantDef: "bot", wantOff: bots.Names(),
		},
		{
			name: "blank default normalizes", def: "", offeredRaw: "bot",
			wantDef: bots.Default, wantOff: []string{"bot"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def, offered, err := validateBotPolicyFlags(tc.def, tc.offeredRaw)
			if err != nil {
				t.Fatalf("validateBotPolicyFlags(%q, %q): %v", tc.def, tc.offeredRaw, err)
			}
			if def != tc.wantDef {
				t.Fatalf("default = %q, want %q", def, tc.wantDef)
			}
			if strings.Join(offered, ",") != strings.Join(tc.wantOff, ",") {
				t.Fatalf("offered = %v, want %v", offered, tc.wantOff)
			}
		})
	}
}

// TestBuildBotPolicyListFiltersAndOrders proves the wire listing is exactly
// the offered set, in production-then-experimental-then-name order (the
// registry's own display order), with the default echoed back and each
// entry's fields carried: an offered set that omits a policy must drop it,
// and the fields the UI reads (strength, cost, search, formats) must survive
// the conversion.
func TestBuildBotPolicyListFiltersAndOrders(t *testing.T) {
	production := bots.Default // "bot" is the only production entry
	if e, _ := bots.Lookup(production); e.Tier != bots.Production {
		t.Fatalf("%q is not production", production)
	}
	// "search" is experimental and constructed-only; use it as the second
	// offered name so both tier ordering and field carrying are observable.
	src, ok := bots.Lookup("search")
	if !ok || src.Tier != bots.Experimental {
		t.Fatalf("\"search\" is not an experimental entry")
	}

	list := buildBotPolicyList(production, []string{"search", production})
	if list.Default != production {
		t.Fatalf("default = %q, want %q", list.Default, production)
	}
	if len(list.Policies) != 2 {
		t.Fatalf("policies = %+v, want exactly the two offered names", list.Policies)
	}
	if list.Policies[0].Name != production || list.Policies[1].Name != "search" {
		t.Fatalf("order = [%s %s], want [%s search]", list.Policies[0].Name, list.Policies[1].Name, production)
	}
	got := list.Policies[1]
	if !got.Search || got.Tier != string(bots.Experimental) ||
		len(got.Formats) != 1 || got.Formats[0] != "constructed" ||
		len(got.Strength) == 0 || got.Strength[0].Claim == "" ||
		got.CostScope == "" || got.MeanMS == 0 {
		t.Fatalf("search entry lost fields in conversion: %+v", got)
	}

	// A set that omits search must not leak it into the listing.
	only := buildBotPolicyList(production, []string{production})
	for _, p := range only.Policies {
		if p.Name == "search" {
			t.Fatalf("offered set without search still lists it: %+v", only.Policies)
		}
	}
	if len(only.Policies) != 1 {
		t.Fatalf("policies = %+v, want just %q", only.Policies, production)
	}
}

// TestCreateGameAppliesTheDefaultPolicy proves an omitted bot_policy becomes
// the server's -bot-policy, both in the response and on the registered table,
// when that default is explicitly offered. The precondition — the default is
// a distinct name from the vocabulary default — is asserted so the test
// cannot pass by normalization.
func TestCreateGameAppliesTheDefaultPolicy(t *testing.T) {
	if host.LethalPressurePolicy == bots.Default {
		t.Fatal("lethal-pressure equals the vocabulary default; the test would be vacuous")
	}
	r, gate := freshGameLock(t)
	c := config{mulligans: 0, botPolicy: host.LethalPressurePolicy, botPolicies: []string{host.BotPolicy, host.LethalPressurePolicy}}
	create := c.createGame(r, gate, []string{"a", "b"}, []string{"c", "d"}, view.Omniscient)
	resp, err := create(httpapi.CreateGameOptions{Format: host.FormatConstructed})
	if err != nil {
		t.Fatal(err)
	}
	if resp.BotPolicy != host.LethalPressurePolicy {
		t.Fatalf("response policy = %q, want the server default %q", resp.BotPolicy, host.LethalPressurePolicy)
	}
	var got string
	for _, info := range r.Tables() {
		if string(info.ID) == resp.Table {
			got = info.BotPolicy
		}
	}
	if got != host.LethalPressurePolicy {
		t.Fatalf("table %s policy = %q, want %q", resp.Table, got, host.LethalPressurePolicy)
	}
}

// TestCreateGameRefusesAnUnofferedPolicy proves -bot-policies gates an
// explicit client pick: a registered policy the server does not offer is a
// refusal naming the offered set, and no table is created.
func TestCreateGameRefusesAnUnofferedPolicy(t *testing.T) {
	r, gate := freshGameLock(t)
	c := config{mulligans: 0, botPolicy: host.BotPolicy, botPolicies: []string{host.BotPolicy}}
	create := c.createGame(r, gate, []string{"a", "b"}, []string{"c", "d"}, view.Omniscient)
	_, err := create(httpapi.CreateGameOptions{Format: host.FormatConstructed, BotPolicy: host.LethalPressurePolicy})
	if err == nil {
		t.Fatalf("unoffered policy %q was accepted; want a refusal", host.LethalPressurePolicy)
	}
	if !strings.Contains(err.Error(), "is not offered") {
		t.Fatalf("error %q does not name the offered-set refusal", err)
	}
	if n := len(r.Tables()); n != 0 {
		t.Fatalf("refused request created %d table(s)", n)
	}
}

// TestCreateGameRefusesAFormatThePolicyLacks proves the create flow refuses
// a policy that cannot play the requested format, naming both. "search" is
// constructed-only (asserted as a precondition), so a commander request must
// be refused and create no table.
func TestCreateGameRefusesAFormatThePolicyLacks(t *testing.T) {
	search, ok := bots.Lookup("search")
	if !ok {
		t.Fatal("bots registry has no \"search\" entry")
	}
	if contains(search.Formats, host.FormatCommander.String()) {
		t.Fatal("\"search\" unexpectedly supports commander; the test would be vacuous")
	}
	r, gate := freshGameLock(t)
	c := config{mulligans: 0, botPolicy: host.BotPolicy, botPolicies: []string{host.BotPolicy, "search"}}
	create := c.createGame(r, gate, []string{"a", "b"}, []string{"c", "d"}, view.Omniscient)
	_, err := create(httpapi.CreateGameOptions{Format: host.FormatCommander, BotPolicy: "search"})
	if err == nil {
		t.Fatalf("search accepted on a commander request; want a refusal")
	}
	if !strings.Contains(err.Error(), "does not support the commander format") {
		t.Fatalf("error %q does not name the format refusal", err)
	}
	if n := len(r.Tables()); n != 0 {
		t.Fatalf("refused request created %d table(s)", n)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
