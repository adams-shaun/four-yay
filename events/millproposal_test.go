package events

import "testing"

func TestMillProposalHasKindDescriptor(t *testing.T) {
	if int(MillProposal) >= NumKinds {
		t.Fatalf("MillProposal kind %d is outside NumKinds %d", MillProposal, NumKinds)
	}
	info, ok := Info(MillProposal)
	if !ok || info.Name != "mill_proposal" || info.Trigger != TriggerNone {
		t.Fatalf("MillProposal descriptor = %+v, %v; want registered no-trigger proposal", info, ok)
	}
}
