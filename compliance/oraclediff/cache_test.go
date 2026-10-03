package oraclediff

import "testing"

func TestCacheRoundTrip(t *testing.T) {
	c := Cache{Dir: t.TempDir()}
	if _, ok := c.Get("abc"); ok {
		t.Fatal("empty cache hit")
	}
	x := XResult{ID: "Shock/cast-resolve/v1", MS: 16, Harness: "h"}
	if err := c.Put("abc", x); err != nil {
		t.Fatal(err)
	}
	got, ok := c.Get("abc")
	if !ok || got.ID != x.ID || got.MS != 16 || got.Harness != "h" {
		t.Fatalf("got %+v %v", got, ok)
	}
	if _, ok := (Cache{}).Get("abc"); ok {
		t.Fatal("no-dir cache hit")
	}
}
