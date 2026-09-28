package main

import (
	"bytes"
	"strings"
	"testing"
)

const (
	hello     = `{"request_type":"hello","protocol":"spellbench/v2","request_id":"r-0","protocol_minor":0}`
	gameStart = `{"request_type":"game_start","protocol":"spellbench/v2","request_id":"r-1","game_id":"g-f67d7fe78c792984","seat":"p0","agent_seed":8103969398531465}`
	choose    = `{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-2","game_id":"g-f67d7fe78c792984","decision":{"acting_seat":"p0","seat_step":6,"candidates":[{"candidate_id":0,"semantic":{"kind":"pass"},"display_text":"Pass priority"},{"candidate_id":1,"semantic":{"kind":"play_land","source":{"object_id":"o-2b8e4dafc6031912","card_name":"Mountain","owner_seat":"p0","controller_seat":"p0","zone":"hand"},"face":0},"display_text":"Play Mountain"}]},"clock":{"remaining_ms":540000,"max_decision_ms":60000}}`
	gameOver  = `{"request_type":"game_over","protocol":"spellbench/v2","request_id":"r-3","game_id":"g-f67d7fe78c792984","terminal":{"outcome":"p0_win","classification":"natural","winner":"p0","reason":"score","seat_step_count":1}}`
)

func session(t *testing.T, args ...string) (int, []string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	in := strings.Join([]string{hello, gameStart, choose, gameOver}, "\n") + "\n"
	code := run(args, strings.NewReader(in), &out, &errs)
	return code, strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n"), errs.String()
}

// TestSessionPerPolicy plays spec 10's four messages through run().
func TestSessionPerPolicy(t *testing.T) {
	for _, c := range []struct {
		policy, choice string
	}{
		{"first", `"selection":{"candidate_id":0,"seat_step":6,"semantic_echo":{"kind":"pass"}}`},
		{"heuristic", `"selection":{"candidate_id":1,"seat_step":6,"semantic_echo":{"face":0,"kind":"play_land","source":{"card_name":"Mountain","controller_seat":"p0","object_id":"o-2b8e4dafc6031912","owner_seat":"p0","zone":"hand"}}}`},
	} {
		code, lines, errs := session(t, "-policy", c.policy)
		if code != 0 || len(lines) != 4 {
			t.Fatalf("%s: exit %d, %d lines, stderr %q", c.policy, code, len(lines), errs)
		}
		if !strings.Contains(lines[0], `"bot":{"name":"sbagent-`+c.policy+`","version":"`+Version+`"}`) {
			t.Errorf("%s: hello_ok %s", c.policy, lines[0])
		}
		if !strings.Contains(lines[1], `"response_type":"ack"`) || !strings.Contains(lines[3], `"response_type":"ack"`) {
			t.Errorf("%s: acks %s / %s", c.policy, lines[1], lines[3])
		}
		if !strings.Contains(lines[2], c.choice) {
			t.Errorf("%s: choice %s, want %s", c.policy, lines[2], c.choice)
		}
	}
}

// TestRandomIsDeterministic: the random policy answers the same way for the
// same agent_seed, whatever the run.
func TestRandomIsDeterministic(t *testing.T) {
	_, a, _ := session(t, "-policy", "random")
	_, b, _ := session(t, "-policy", "random")
	if a[2] != b[2] {
		t.Fatalf("random differs between runs: %s vs %s", a[2], b[2])
	}
}

func TestFlags(t *testing.T) {
	code, lines, _ := session(t, "-policy", "first", "-name", "custom", "-version", "9", "-no-echo")
	if code != 0 || !strings.Contains(lines[0], `"bot":{"name":"custom","version":"9"}`) {
		t.Fatalf("exit %d, hello %s", code, lines[0])
	}
	if strings.Contains(lines[2], "semantic_echo") || strings.Contains(lines[2], "seat_step") {
		t.Fatalf("-no-echo still echoes: %s", lines[2])
	}
	for _, bad := range [][]string{{"-policy", "mcts"}, {"-bogus"}, {"extra"}} {
		if code := run(bad, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
			t.Errorf("%v: exit %d, want 2", bad, code)
		}
	}
}
