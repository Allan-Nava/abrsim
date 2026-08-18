package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Allan-Nava/abrsim/internal/finding"
	"github.com/Allan-Nava/abrsim/internal/sim"
)

func report() Report {
	return Report{
		Source: "https://cdn.example/master.m3u8",
		Run: sim.Result{
			Algorithm: "bola", Trace: "mobile-4g",
			Bitrates: []int64{800_000, 5_000_000},
			Names:    []string{"360p", "1080p"},
			Requests: []sim.Request{{Index: 0, Rung: 0, Bytes: 400_000, Elapsed: 1.2, Duration: 4}},
			Started:  true, Startup: 1.2, Media: 240, Wall: 250, Bytes: 24_500_000,
			Stalls: 3, StallTime: 8.4, Switches: 6, Estimated: true,
		},
		Findings: []finding.Finding{
			{Check: "rebuffer", Target: "mobile-4g", Status: finding.BAD,
				Message: "3 stalls, 8.4s frozen", Value: finding.Num(8.4), Unit: "s",
				Hint: "the picture is stopped for this long"},
			{Check: "startup", Target: "session", Status: finding.WARN, Message: "1.2s to the first frame"},
			{Check: "switches", Target: "session", Status: finding.OK, Message: "6 rung changes"},
		},
	}
}

func TestText_WorstFindingIsTheFirstLine(t *testing.T) {
	// The first line is the thing the operator has to look at. Everything else
	// in the renderer is decoration.
	var b bytes.Buffer
	if err := Text(&b, report(), false); err != nil {
		t.Fatalf("Text: %v", err)
	}
	first := strings.SplitN(b.String(), "\n", 2)[0]
	if !strings.Contains(first, "BAD") || !strings.Contains(first, "rebuffer") {
		t.Errorf("first line is %q, want the BAD rebuffer finding", first)
	}
}

func TestText_CarriesTheHintAndTheSummary(t *testing.T) {
	var b bytes.Buffer
	if err := Text(&b, report(), false); err != nil {
		t.Fatalf("Text: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		"the picture is stopped for this long", // the hint
		"mobile-4g",                            // which network this was
		"bola",                                 // and which algorithm
		"1 BAD",                                // the counts
		"declared",                             // the sizes caveat, unmissable
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q:\n%s", want, out)
		}
	}
}

func TestText_NoColourWhenNotAsked(t *testing.T) {
	var b bytes.Buffer
	if err := Text(&b, report(), false); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if strings.Contains(b.String(), "\x1b[") {
		t.Error("plain output carries ANSI escapes: it gets piped into incident documents")
	}
}

func TestText_ColourWhenAsked(t *testing.T) {
	var b bytes.Buffer
	if err := Text(&b, report(), true); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if !strings.Contains(b.String(), "\x1b[") {
		t.Error("no ANSI escapes in coloured output")
	}
}

func TestJSON_IsCompleteAndParses(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, report()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("the JSON we emit does not parse: %v", err)
	}
	for _, key := range []string{"source", "run", "findings"} {
		if _, ok := got[key]; !ok {
			t.Errorf("no %q key: %v", key, keys(got))
		}
	}
	// The per-request timeline is the reason to take the JSON at all: without
	// it a consumer can only re-derive totals it was already given.
	run := got["run"].(map[string]any)
	if _, ok := run["requests"]; !ok {
		t.Error("the JSON carries no per-request timeline")
	}
	if run["estimated"] != true {
		t.Error("the JSON does not carry the estimated-sizes flag, so a consumer cannot tell a simulation from a measurement")
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestUseColour(t *testing.T) {
	// NO_COLOR is honoured whatever the terminal says.
	if UseColour(true, "1") {
		t.Error("NO_COLOR was set and colour was used anyway")
	}
	if !UseColour(true, "") {
		t.Error("a TTY with no NO_COLOR should be coloured")
	}
	if UseColour(false, "") {
		t.Error("a pipe should never be coloured")
	}
}
