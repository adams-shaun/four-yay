package main

import (
	"reflect"
	"testing"
)

// TestWeightFieldsCoverCastWeights holds weightFields to CastWeights: every
// int32 field, in struct order, and each accessor reaching its own field.
func TestWeightFieldsCoverCastWeights(t *testing.T) {
	rt := reflect.TypeOf(Weights{})
	var want []string
	for i := 0; i < rt.NumField(); i++ {
		if rt.Field(i).Type.Kind() == reflect.Int32 {
			want = append(want, rt.Field(i).Name)
		}
	}
	if got := weightFieldNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("weightFields = %v\nCastWeights int32 fields = %v", got, want)
	}
	for i, name := range want {
		var w Weights
		setWeight(&w, name, int32(i+1))
		if v := reflect.ValueOf(w).FieldByName(name).Int(); v != int64(i+1) {
			t.Errorf("setWeight(%s) wrote elsewhere: field = %d", name, v)
		}
		if g := getWeight(w, name); g != int32(i+1) {
			t.Errorf("getWeight(%s) = %d", name, g)
		}
	}
}
