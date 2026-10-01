package searchbench

import (
	"reflect"
	"testing"
)

// Reference values from CPython 3.13: random.Random(s).getrandbits(32) and
// random.Random(s).choice(range(n)).
func TestPyRandomMatchesCPython(t *testing.T) {
	r := newPyRandom(0)
	for i, want := range []uint32{3626764237, 1654615998, 3255389356} {
		if got := r.uint32(); got != want {
			t.Fatalf("Random(0) word %d = %d, want %d", i, got, want)
		}
	}
	for _, tc := range []struct {
		seed uint64
		n    int
		want []int
	}{
		{0, 1000, []int{864, 394, 776, 911, 430, 41, 265, 988}},
		{12345, 7, []int{3, 5, 0, 6, 6, 6, 2, 6}},
	} {
		r := newPyRandom(tc.seed)
		var got []int
		for range tc.want {
			got = append(got, r.below(tc.n))
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("Random(%d).choice(range(%d)) = %v, want %v", tc.seed, tc.n, got, tc.want)
		}
	}
	r = newPyRandom(1<<40 + 3)
	if a, b := r.uint32(), r.uint32(); a != 943978446 || b != 261273136 {
		t.Fatalf("Random(2**40+3) = %d %d", a, b)
	}
}

func TestBootstrapIsDeterministicAndSeeded(t *testing.T) {
	m := benchManifest(200, 7)
	run := bindT(t, m, benchArm(m, "x", .5, 3))
	a, b := DefaultBootstrap.Estimates(run), DefaultBootstrap.Estimates(run)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed gave different estimates")
	}
	c := Bootstrap{Resamples: 1000, Seed: 1}.Estimates(run)
	if reflect.DeepEqual(a["a_set"].CI, c["a_set"].CI) {
		t.Fatal("a different seed gave the same CI")
	}
	if *a["a_set"].Value != *c["a_set"].Value {
		t.Fatal("the point estimate depends on the bootstrap seed")
	}
	for _, k := range []string{"a_set", "balanced", "which_spell"} {
		e := a[k]
		if e.CI == nil || e.CI[0] > *e.Value || e.CI[1] < *e.Value || e.CI[0] == e.CI[1] {
			t.Fatalf("%s CI %v does not bracket %v", k, e.CI, *e.Value)
		}
	}
	if none := (Bootstrap{}).Estimates(run); none["a_set"].CI != nil {
		t.Fatal("zero resamples still produced a CI")
	}
}

// Items of one game move together: a resample weights each item by its
// game's draw count, so when every game holds one agreeing and one
// disagreeing item the pooled agreement never moves, while an item-level
// bootstrap would spread it.
func TestBootstrapResamplesGamesNotItems(t *testing.T) {
	m := benchManifest(100, 9)
	cl, nc := clusters(m.Items)
	if nc != 100 || cl[0] != cl[1] || cl[1] == cl[2] {
		t.Fatalf("clusters %v (%d)", cl[:4], nc)
	}
	s := make([]scored, len(m.Items))
	for i := range s {
		s[i] = scored{typ: 0, cluster: cl[i], set: i%2 == 0}
	}
	DefaultBootstrap.resample(nc, func(counts []int) {
		var tl tally
		for _, x := range s {
			tl.add(x, counts[x.cluster])
		}
		if v := float64(tl.set[0]) / float64(tl.n[0]); v != .5 {
			t.Fatalf("pooled agreement %v varies under a game bootstrap", v)
		}
	})
}

func TestPairedAgainstItselfIsExactlyZero(t *testing.T) {
	m := benchManifest(150, 11)
	run := bindT(t, m, benchArm(m, "x", .4, 5))
	d, err := DefaultBootstrap.Paired(run, run)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range MetricNames {
		e := d[k]
		if e.Value == nil {
			continue
		}
		if *e.Value != 0 || e.CI == nil || e.CI[0] != 0 || e.CI[1] != 0 {
			t.Fatalf("%s self-difference = %v %v", k, *e.Value, e.CI)
		}
	}
	if s := d["same_choice"]; *s.Value != 1 || s.CI[0] != 1 || s.CI[1] != 1 {
		t.Fatalf("same_choice = %v", s)
	}
}

func TestPairedDifferenceMatchesTheTwoRuns(t *testing.T) {
	m := benchManifest(150, 13)
	a, b := bindT(t, m, benchArm(m, "a", .7, 5)), bindT(t, m, benchArm(m, "b", .2, 6))
	d, err := DefaultBootstrap.Paired(a, b)
	if err != nil {
		t.Fatal(err)
	}
	ea, eb := DefaultBootstrap.Estimates(a), DefaultBootstrap.Estimates(b)
	for _, k := range []string{"a_set", "balanced", "a_strict"} {
		if !near(*d[k].Value, *ea[k].Value-*eb[k].Value) {
			t.Fatalf("%s paired point %v != %v - %v", k, *d[k].Value, *ea[k].Value, *eb[k].Value)
		}
		if d[k].CI[0] <= 0 {
			t.Fatalf("%s: a clearly better arm's paired CI %v includes zero", k, d[k].CI)
		}
	}
	// The paired CI is narrower than the difference of unpaired intervals.
	if w, u := d["a_set"].CI[1]-d["a_set"].CI[0], (ea["a_set"].CI[1]-ea["a_set"].CI[0])+(eb["a_set"].CI[1]-eb["a_set"].CI[0]); w >= u {
		t.Fatalf("paired width %v not below unpaired %v", w, u)
	}
	other := benchManifest(151, 13)
	if _, err := DefaultBootstrap.Paired(a, bindT(t, other, benchArm(other, "c", .5, 1))); err == nil {
		t.Fatal("paired runs over different items")
	}
}
