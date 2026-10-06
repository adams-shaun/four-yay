package templates

import "testing"

// Exhaustive census in four-set chunks; no individual test must grow past
// the one-minute seat budget. runSameNameAnswerCensus checks declared coverage.
func TestSameNameAnswerCensusChunk00(t *testing.T) { runSameNameAnswerCensus(t, 0, 4) }
func TestSameNameAnswerCensusChunk01(t *testing.T) { runSameNameAnswerCensus(t, 4, 8) }
func TestSameNameAnswerCensusChunk02(t *testing.T) { runSameNameAnswerCensus(t, 8, 12) }
func TestSameNameAnswerCensusChunk03(t *testing.T) { runSameNameAnswerCensus(t, 12, 16) }
func TestSameNameAnswerCensusChunk04(t *testing.T) { runSameNameAnswerCensus(t, 16, 20) }

func TestSameNameAnswerCensusIndependentCandidates(t *testing.T) {
	options := []string{"p0:Forest", "p0:Forest", "p0:Forest#2", "p1:token:Forest", "p0:Island"}
	if got := sameNameCandidates("p0:Forest#2", options); got != 3 {
		t.Fatalf("fixture must expose three DISTINCT same-named objects, got %d", got)
	}
	for _, answer := range []string{"Forest", "Forest[no copy]", "@p0:Forest"} {
		bucket, _ := classifyAnswer(answer, "p0:Forest#2", options)
		if bucket != "unresolved" {
			t.Errorf("%q selects multiple objects or the wrong sibling, got %s", answer, bucket)
		}
	}
	wrong := segmentFor([]string{"@p0:Island"}, "p0:Forest#2")
	if wrong == "" {
		t.Fatal("a wrong-name object alias must not disappear from the census")
	}
	if bucket, _ := classifyAnswer(wrong, "p0:Forest#2", options); bucket != "unresolved" {
		t.Fatalf("wrong-name alias must be unresolved, got %s", bucket)
	}
	bucket, matches := classifyAnswer("@p0:Forest#2", "p0:Forest#2", options)
	if bucket != "alias" || matches != 1 {
		t.Fatalf("exact pick must be uniquely resolved: %s, %d", bucket, matches)
	}
}
