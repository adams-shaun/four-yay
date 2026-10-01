package main

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/protocol"
)

// defaultBotPoliciesRaw is -bot-policies' default: the empty string means
// "every registered entry", resolved against the registry at startup rather
// than frozen into a literal list that would go stale as entries land.
const defaultBotPoliciesRaw = ""

// botPolicyOffered parses a -bot-policies comma list into the offered set.
// The empty string means every registered entry, in the registry's display
// order (production first, then experimental, then by name — bots.Entries).
// A non-empty list is returned in that same registry order, not the order
// the operator typed, so the listing is stable regardless of spelling; a
// name the registry does not know is an error naming it. Duplicates are
// rejected rather than silently collapsed: "bot,bot" is a typo, not a
// one-element set.
func botPolicyOffered(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return bots.Names(), nil
	}
	seen := map[string]bool{}
	requested := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			return nil, fmt.Errorf("-bot-policies %q: empty entry in comma list", raw)
		}
		if _, ok := bots.Lookup(name); !ok {
			return nil, fmt.Errorf("-bot-policies: unknown bot policy %q (known: %s)", name, strings.Join(bots.Names(), ", "))
		}
		if seen[name] {
			return nil, fmt.Errorf("-bot-policies %q: duplicate bot policy %q", raw, name)
		}
		seen[name] = true
		requested[name] = true
	}
	// Return the registry's canonical order so the listing is identical no
	// matter how the operator spelled the list.
	out := make([]string, 0, len(requested))
	for _, name := range bots.Names() {
		if requested[name] {
			out = append(out, name)
		}
	}
	return out, nil
}

// validateBotPolicyFlags validates the two policy flags at startup and
// returns the normalized default plus the offered set. It is split out from
// serve so the checks can be tested without opening the corpus. The default
// is normalized here ("" -> bots.Default) so a config built without the flag
// still names a policy on the wire listing.
//
//   - -bot-policies: every name registered (botPolicyOffered), no
//     duplicates (also there), and it must contain -bot-policy.
//   - -bot-policy: the ordinary default for a vs-bot request that omits
//     bot_policy. It must be registered, offered, support BOTH formats and
//     seat at least 2, because a default that lacks a format would let an
//     omitted-field request reach a policy that then refuses it.
func validateBotPolicyFlags(def, offeredRaw string) (string, []string, error) {
	offered, err := botPolicyOffered(offeredRaw)
	if err != nil {
		return "", nil, err
	}
	if def == "" {
		def = bots.Default
	}
	entry, ok := bots.Lookup(def)
	if !ok {
		return "", nil, fmt.Errorf("-bot-policy: unknown bot policy %q (known: %s)", def, strings.Join(bots.Names(), ", "))
	}
	if !containsString(offered, def) {
		return "", nil, fmt.Errorf("-bot-policy %q is not in -bot-policies (offered: %s)", def, strings.Join(offered, ", "))
	}
	for _, f := range []host.Format{host.FormatConstructed, host.FormatCommander} {
		if !containsString(entry.Formats, f.String()) {
			return "", nil, fmt.Errorf("-bot-policy %q does not support the %s format (formats: %s)", def, f, strings.Join(entry.Formats, ", "))
		}
	}
	if entry.MaxSeats > 0 && entry.MaxSeats < 2 {
		return "", nil, fmt.Errorf("-bot-policy %q seats at most %d, want >= 2", def, entry.MaxSeats)
	}
	return def, offered, nil
}

// buildBotPolicyList is the immutable GET /api/bot-policies listing gorged
// arms httpapi with at startup: the offered set in display order (production
// first, then experimental, then by name) and the server's default. A name
// the offered set omits is simply absent, so the listing and the create
// flow's acceptance test are the same set by construction.
func buildBotPolicyList(def string, offered []string) *protocol.BotPolicyList {
	offeredSet := map[string]bool{}
	for _, name := range offered {
		offeredSet[name] = true
	}
	list := &protocol.BotPolicyList{Default: def, Policies: []protocol.BotPolicyInfo{}}
	for _, e := range bots.Entries() { // production first, then name
		if !offeredSet[e.Name] {
			continue
		}
		list.Policies = append(list.Policies, botPolicyInfo(e))
	}
	return list
}

// botPolicyInfo converts one registry entry to its wire record.
func botPolicyInfo(e bots.Entry) protocol.BotPolicyInfo {
	info := protocol.BotPolicyInfo{
		Name:        e.Name,
		Label:       e.Label,
		Description: e.Description,
		Tier:        string(e.Tier),
		MeanMS:      e.Cost.MeanMS,
		P95MS:       e.Cost.P95MS,
		CostScope:   e.Cost.Scope,
		CostNote:    e.Cost.Note,
		Search:      e.Search,
		Formats:     append([]string(nil), e.Formats...),
	}
	for _, m := range e.Strength {
		info.Strength = append(info.Strength, protocol.BotMeasurement{
			Claim: m.Claim, Versus: m.Versus, Setting: m.Setting, Source: m.Source,
		})
	}
	if info.Formats == nil {
		info.Formats = []string{}
	}
	if info.Strength == nil {
		info.Strength = []protocol.BotMeasurement{}
	}
	return info
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
