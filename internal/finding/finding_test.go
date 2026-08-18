package finding

import "testing"

func TestSortWorstFirst(t *testing.T) {
	// ERROR sorts above BAD, and only for this reason: an operator needs to
	// know the coverage has a hole before they read a clean-looking result.
	fs := []Finding{
		{Check: "efficiency", Target: "session", Status: OK},
		{Check: "rebuffer", Target: "train", Status: BAD},
		{Check: "startup", Target: "session", Status: ERROR},
		{Check: "switches", Target: "session", Status: WARN},
		{Check: "ladder-gap", Target: "800k..5000k", Status: BAD},
	}
	SortWorstFirst(fs)

	want := []Status{ERROR, BAD, BAD, WARN, OK}
	for i, w := range want {
		if fs[i].Status != w {
			t.Fatalf("position %d = %s, want %s", i, fs[i].Status, w)
		}
	}
	// Ties break on check name so the output is stable between runs.
	if fs[1].Check != "ladder-gap" || fs[2].Check != "rebuffer" {
		t.Errorf("equal severities did not break the tie on check name: %q then %q", fs[1].Check, fs[2].Check)
	}
}

func TestWorstAndSummarize(t *testing.T) {
	if got := Worst(nil); got != OK {
		t.Errorf("Worst(nil) = %s, want OK", got)
	}
	fs := []Finding{{Status: OK}, {Status: WARN}, {Status: OK}}
	if got := Worst(fs); got != WARN {
		t.Errorf("Worst = %s, want WARN", got)
	}
	c := Summarize(fs)
	if c[OK] != 2 || c[WARN] != 1 || c[BAD] != 0 || c[ERROR] != 0 {
		t.Errorf("Summarize = %v", c)
	}
}

func TestAtLeast(t *testing.T) {
	if !AtLeast(BAD, WARN) || AtLeast(WARN, BAD) {
		t.Error("AtLeast does not follow the severity order")
	}
	if !AtLeast(OK, "") {
		t.Error("an empty threshold must be satisfied by anything")
	}
}
