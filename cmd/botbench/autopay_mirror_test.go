package main

import "testing"

func TestAutoPayMirrorPolicyFlagScope(t *testing.T) {
	for _, name := range []string{"bot-auto-pay", "cast-profile-auto-pay"} {
		if !isAutoPayPolicy(name) {
			t.Errorf("%q must be included in opt-in auto-pay mirroring", name)
		}
	}
	for _, name := range []string{"bot", "cast-profile", "legacy"} {
		if isAutoPayPolicy(name) {
			t.Errorf("non-auto-pay policy %q was included", name)
		}
	}
}
