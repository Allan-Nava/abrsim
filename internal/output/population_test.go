package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Allan-Nava/abrsim/internal/finding"
	"github.com/Allan-Nava/abrsim/internal/population"
)

func popReport() PopulationReport {
	return PopulationReport{
		Source: "https://cdn.example/master.m3u8",
		Population: population.Report{
			Viewers: 30, Trace: "steps-down", Algorithm: "bola", Segments: 40,
			Startup:    population.Stat{Min: 0.4, Median: 1.2, Max: 4.8},
			Frozen:     population.Stat{Min: 0, Median: 0, Max: 21.4},
			Stalls:     population.Stat{Min: 0, Median: 0, Max: 6},
			SwitchRate: population.Stat{Min: 0.4, Median: 2.1, Max: 9.8},
			Delivered:  population.Stat{Min: 900e3, Median: 2.4e6, Max: 3.6e6},
			Checks: []population.CheckSpread{
				{Check: "rebuffer", Worst: finding.BAD, Loud: 12, OK: 18, Bad: 12,
					WorstTarget: "steps-down", WorstMessage: "6 stalls, 21s frozen in 160s of playback (13.4%)",
					WorstHint: "the picture is stopped for this long", WorstViewer: 23},
				{Check: "startup", Worst: finding.WARN, Loud: 8, OK: 22, Warn: 8,
					WorstMessage: "4.8s to the first frame, starting on 1000k", WorstViewer: 7},
				{Check: "sizes", Worst: finding.OK, OK: 30, WorstMessage: "every segment size was measured"},
			},
			Runs: []population.Viewer{{Index: 0, Startup: 1.2}, {Index: 23, Startup: 4.8, Frozen: 21.4, Stalls: 6}},
		},
		Options: map[string]any{"viewers": 30},
	}
}

func TestTextPopulation_LeadsWithTheWorstAndNamesTheViewer(t *testing.T) {
	var b bytes.Buffer
	if err := TextPopulation(&b, popReport(), false); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	out := b.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")

	if !strings.Contains(lines[0], "30 viewers") || !strings.Contains(lines[0], "steps-down") {
		t.Errorf("first line is %q — it has to say how many viewers over which network", lines[0])
	}
	first := ""
	for _, l := range lines {
		if strings.Contains(l, "BAD") {
			first = l
			break
		}
	}
	if !strings.Contains(first, "rebuffer") || !strings.Contains(first, "12 of 30") {
		t.Errorf("the worst line is %q — worst first, with the share of the audience it happened to", first)
	}
	if !strings.Contains(out, "viewer 23") {
		t.Error("the report never names the viewer the worst finding came from, so nobody can reproduce it with --viewers")
	}
	// A quiet check still prints one sentence, and that sentence is one
	// viewer's — saying it without saying whose implies it describes all thirty.
	quiet := ""
	for _, l := range lines {
		if strings.Contains(l, "sizes") {
			quiet = l
		}
	}
	if !strings.Contains(quiet, "viewer 0") {
		t.Errorf("the quiet line is %q — a single viewer's sentence has to say which viewer, or it reads as a statement about the whole audience", quiet)
	}
	for _, want := range []string{"startup", "frozen", "switches/min", "delivered", "median", "min", "max"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in the distribution table:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "21.4s") && !strings.Contains(out, "21s") {
		t.Errorf("the worst viewer's frozen seconds are missing from the table:\n%s", out)
	}
}

func TestTextPopulation_SaysWhenViewersWatchedDifferentAmounts(t *testing.T) {
	r := popReport()
	r.Population.Runs = []population.Viewer{{Index: 0, Media: 160}, {Index: 1, Media: 96}}
	var b bytes.Buffer
	if err := TextPopulation(&b, r, false); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	if !strings.Contains(b.String(), "96s") || !strings.Contains(b.String(), "160s") {
		t.Errorf("viewers watched between 96s and 160s and the summary claims one figure:\n%s", b.String())
	}

	r.Population.Runs = []population.Viewer{{Index: 0, Media: 160}, {Index: 1, Media: 160}}
	b.Reset()
	if err := TextPopulation(&b, r, false); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	if !strings.Contains(b.String(), "160s of media each") {
		t.Errorf("everybody watched the same 160s and the summary hedges:\n%s", b.String())
	}
}

func TestTextPopulation_SaysWhenTheMedianHidesTheTail(t *testing.T) {
	var b bytes.Buffer
	if err := TextPopulation(&b, popReport(), false); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	// A median of zero frozen seconds beside a maximum of 21 is the one thing a
	// single-viewer run would have got wrong, so the report has to point at it.
	if !strings.Contains(b.String(), "median") {
		t.Error("no median in the output")
	}
	if strings.Contains(b.String(), "\x1b[") {
		t.Error("colour without being asked for it")
	}
}

func TestTextPopulation_ColourWhenAsked(t *testing.T) {
	var b bytes.Buffer
	if err := TextPopulation(&b, popReport(), true); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	if !strings.Contains(b.String(), "\x1b[31m") {
		t.Error("no red for the BAD line")
	}
}

func TestJSONPopulation_ParsesAndKeepsEveryViewer(t *testing.T) {
	var b bytes.Buffer
	if err := JSONPopulation(&b, popReport()); err != nil {
		t.Fatalf("JSONPopulation: %v", err)
	}
	var got struct {
		Source     string `json:"source"`
		Population struct {
			Viewers int `json:"viewers"`
			Checks  []struct {
				Check  string `json:"check"`
				Loud   int    `json:"above_ok"`
				Worst  string `json:"worst"`
				Viewer int    `json:"worst_viewer"`
			} `json:"checks"`
			Runs    []struct{ Index int } `json:"runs"`
			Startup struct {
				Min, Median, Max float64
			} `json:"startup"`
		} `json:"population"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("the document does not parse: %v\n%s", err, b.String())
	}
	if got.Population.Viewers != 30 || len(got.Population.Checks) != 3 || len(got.Population.Runs) != 2 {
		t.Errorf("got %+v", got.Population)
	}
	if got.Population.Checks[0].Check != "rebuffer" || got.Population.Checks[0].Loud != 12 {
		t.Errorf("checks[0] = %+v", got.Population.Checks[0])
	}
	if got.Population.Startup.Max != 4.8 {
		t.Errorf("startup.max = %v, want 4.8 — the tail is the number a machine consumer is here for", got.Population.Startup.Max)
	}
	if strings.Contains(b.String(), "\"requests\"") {
		t.Error("the population document carries per-request timelines: two hundred of those is not a document anybody reads")
	}
}
