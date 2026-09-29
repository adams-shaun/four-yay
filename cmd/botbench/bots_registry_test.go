package main

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/bots/azredeal"
	"github.com/adams-shaun/gorge/bots/sbsearch"
	"github.com/adams-shaun/gorge/bots/sbtactical"
	"github.com/adams-shaun/gorge/bots/search"
)

func TestBotbenchDefaultsAreTheHostedConfigs(t *testing.T) {
	if got, want := searchOverlay(search.Hosted()), search.Hosted(); !reflect.DeepEqual(got, want) {
		t.Errorf("search overlay = %#v, want Hosted() %#v", got, want)
	}
	if got, want := azRedealOverlay(), azredeal.Hosted(); !reflect.DeepEqual(got, want) {
		t.Errorf("az-redeal overlay = %#v, want Hosted() %#v", got, want)
	}
	if got, want := tacticalOverlay(sbtactical.Hosted()), sbtactical.Hosted(); !reflect.DeepEqual(got, want) {
		t.Errorf("sb-tactical overlay = %#v, want Hosted() %#v", got, want)
	}
	if got, want := sbSearchOverlay(sbsearch.LiteAtk()), sbsearch.Hosted(); !reflect.DeepEqual(got, want) {
		t.Errorf("sb-search overlay = %#v, want Hosted() %#v", got, want)
	}
}

func TestEveryHostedNameIsABenchPolicy(t *testing.T) {
	for _, name := range bots.Names() {
		if _, ok := policies[name]; !ok {
			t.Errorf("registered bot %q has no botbench policy", name)
		}
	}
}
