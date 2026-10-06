package oraclegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// EndTurn may leave later-step actions queued, but it must not mask a target
// or choice that a completed step never consumed. Exercise the Java queue
// matcher without starting XMage (its H2 database cannot start in the jail).
func TestScenarioReplayEndTurnRejectsCompletedStepAnswers(t *testing.T) {
	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	start := strings.Index(java, "    private static <T> boolean remainingIsSkipped(")
	end := strings.Index(java, "    private static int unusedActionCount(")
	if start < 0 || end <= start {
		t.Fatal("missing Java answer-queue matcher")
	}
	// The matcher must be used at the fallback, on BOTH players' choice and
	// target queues. Capture begins before scripted answers, not after them.
	for _, required := range []string{
		"skippedAnswersMatch(completedSteps, choicesA, choicesB, targetsA, targetsB)",
		"remainingIsSkipped(actualA.getChoices(), choicesA, first)",
		"remainingIsSkipped(actualB.getChoices(), choicesB, first)",
		"remainingIsSkipped(actualA.getTargets(), targetsA, first)",
		"remainingIsSkipped(actualB.getTargets(), targetsB, first)",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("missing EndTurn answer guard: %s", required)
		}
	}
	before := strings.Index(java, "int beforeChoicesA = playerA.getChoices().size();")
	script := strings.Index(java, "scripted(xans.get(i).getAsJsonArray());")
	capture := strings.Index(java, "choicesA.add(new ArrayList<>(playerA.getChoices().subList(beforeChoicesA")
	if before < 0 || script < before || capture < script {
		t.Fatal("step answer capture must include scripted answers")
	}

	javac, err := exec.LookPath("javac")
	if err != nil {
		javac = "/mnt/sata/gorge-training/xmageoracle/jdk/bin/javac"
		if _, err := os.Stat(javac); err != nil {
			t.Skip("JDK unavailable; source wiring checked")
		}
	}
	javaCmd := filepath.Join(filepath.Dir(javac), "java")
	dir := t.TempDir()
	probe := `import java.util.*;
public class EndTurnQueueProbe {
` + java[start:end] + `
    public static void main(String[] args) {
        List<List<String>> steps = Arrays.asList(Arrays.asList("completed"), Arrays.asList("skipped"));
        // This is the bug: a completed cast's surplus target survives even
        // though the later step's actions were correctly skipped.
        if (remainingIsSkipped(Arrays.asList("completed", "skipped"), steps, 1)) throw new AssertionError("surplus target accepted");
        if (remainingIsSkipped(Arrays.asList("completed"), steps, 1)) throw new AssertionError("surplus choice accepted");
        if (!remainingIsSkipped(Arrays.asList("skipped"), steps, 1)) throw new AssertionError("skipped answer rejected");
        if (remainingIsSkipped(Collections.emptyList(), steps, 1)) throw new AssertionError("lost skipped answer accepted");
        if (!remainingIsSkipped(Collections.emptyList(), Arrays.asList(Collections.emptyList(), Collections.emptyList()), 1)) throw new AssertionError("empty queues rejected");
        System.out.println("EndTurn answer queues: completed leftovers rejected; skipped queues accepted");
    }
}`
	path := filepath.Join(dir, "EndTurnQueueProbe.java")
	if err := os.WriteFile(path, []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(javac, "-d", dir, path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, output)
	}
	cmd = exec.Command(javaCmd, "-cp", dir, "EndTurnQueueProbe")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Java queue regression: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
