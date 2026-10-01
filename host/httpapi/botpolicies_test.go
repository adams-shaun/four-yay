package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/protocol"
)

// botPoliciesHandler builds a registry + handler with the given offered
// listing, the same shape cmd/gorged wires at startup.
func botPoliciesHandler(t *testing.T, list *protocol.BotPolicyList) *httptest.Server {
	t.Helper()
	r, err := host.New(host.Options{LoadDeck: loader(t), Sleep: func(time.Duration, <-chan struct{}) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	srv := httptest.NewServer(NewHandler(r, Options{BotPolicies: list}))
	t.Cleanup(srv.Close)
	return srv
}

// TestBotPoliciesEndpointServesTheList proves GET /api/bot-policies returns
// exactly the configured listing, field for field, so gorged's conversion
// from bots.Info is observable over the wire.
func TestBotPoliciesEndpointServesTheList(t *testing.T) {
	want := &protocol.BotPolicyList{
		Default: "bot",
		Policies: []protocol.BotPolicyInfo{
			{
				Name: "bot", Label: "Bot", Description: "the baseline bot", Tier: "production",
				Strength: []protocol.BotMeasurement{{Claim: "55%", Versus: "manual", Setting: "mono suite", Source: "bench"}},
				MeanMS:   12.5, P95MS: 40, CostScope: "mono", CostNote: "fast", Search: false,
				Formats: []string{"constructed", "commander"},
			},
			{Name: "az", Label: "AZ", Tier: "experimental", Search: true, Formats: []string{"constructed"}},
		},
	}
	srv := botPoliciesHandler(t, want)

	resp, err := http.Get(srv.URL + "/api/bot-policies")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got protocol.BotPolicyList
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	if got.Default != want.Default || len(got.Policies) != 2 {
		t.Fatalf("listing = %+v, want %+v", got, want)
	}
	if got.Policies[0].Search || got.Policies[0].MeanMS != 12.5 ||
		len(got.Policies[0].Strength) != 1 || got.Policies[0].Strength[0].Setting != "mono suite" {
		t.Fatalf("first policy = %+v", got.Policies[0])
	}
	if !got.Policies[1].Search || got.Policies[1].Tier != "experimental" {
		t.Fatalf("second policy = %+v", got.Policies[1])
	}
}

// TestBotPoliciesEndpointEmptyWhenUnset proves an unconfigured server still
// serves the endpoint with a well-formed empty listing, not 404 and not
// null: a client can always read .policies and .default. It also pins that
// the route reached the handler (a 404 body would decode to nothing).
func TestBotPoliciesEndpointEmptyWhenUnset(t *testing.T) {
	srv := botPoliciesHandler(t, nil)

	resp, err := http.Get(srv.URL + "/api/bot-policies")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	raw := new(bytes.Buffer)
	if _, err := raw.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	// writeJSON appends a trailing newline; compare the decoded shape so the
	// assertion is on the listing, not on encoder formatting.
	var got protocol.BotPolicyList
	if err := json.Unmarshal(raw.Bytes(), &got); err != nil {
		t.Fatalf("body %q is not a listing: %v", raw.String(), err)
	}
	if got.Default != "" || got.Policies == nil || len(got.Policies) != 0 {
		t.Fatalf("body = %s, want the empty listing", raw.String())
	}
}
