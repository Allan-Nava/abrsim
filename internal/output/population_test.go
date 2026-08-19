package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Allan-Nava/abrsim/internal/analyze"
	"github.com/Allan-Nava/abrsim/internal/finding"
	"github.com/Allan-Nava/abrsim/internal/population"
)

func popReport() PopulationReport {
	return PopulationReport{
		Source: "https://cdn.example/master.m3u8",
		Population: population.Report{
			Viewers: 30, Trace: "steps-down", Algorithm: "bola", Segments: 40,
			Startup:    stat(30, 0.4, 1.2, 3.9, 4.8),
			Frozen:     stat(30, 0, 0, 18.2, 21.4),
			Stalls:     stat(30, 0, 0, 5, 6),
			SwitchRate: stat(30, 0.4, 2.1, 8.8, 9.8),
			Delivered:  stat(30, 900e3, 2.4e6, 1.1e6, 3.6e6),
			Checks: []population.CheckSpread{
				{Check: "rebuffer", Worst: finding.BAD, Loud: 12, OK: 18, Bad: 12,
					AtP50: finding.OK, AtP95: finding.BAD, FiresFrom: 60,
					WorstTarget: "steps-down", WorstMessage: "6 stalls, 21s frozen in 160s of playback (13.4%)",
					WorstHint: "the picture is stopped for this long", WorstViewer: 23},
				{Check: "startup", Worst: finding.WARN, Loud: 8, OK: 22, Warn: 8,
					AtP50: finding.OK, AtP95: finding.WARN, FiresFrom: 73.3,
					WorstMessage: "4.8s to the first frame, starting on 1000k", WorstViewer: 7},
				{Check: "sizes", Worst: finding.OK, OK: 30, AtP50: finding.OK, AtP95: finding.OK,
					WorstMessage: "every segment size was measured"},
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
	for _, want := range []string{"startup", "frozen", "switches/min", "delivered", "p50", "p95", "min", "max"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in the distribution table:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "21.4s") && !strings.Contains(out, "21s") {
		t.Errorf("the worst viewer's frozen seconds are missing from the table:\n%s", out)
	}
}

func TestTextPopulation_LeadsWithTheSeverityAtTheP95(t *testing.T) {
	var b bytes.Buffer
	if err := TextPopulation(&b, popReport(), false); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "p95 BAD") {
		t.Errorf("no severity at the p95 on the rebuffer line:\n%s", out)
	}
	if !strings.Contains(out, "p50 OK") {
		t.Errorf("no severity at the p50 beside it — that pairing is what shows the median hiding the tail:\n%s", out)
	}
	if !strings.Contains(out, "from p60") {
		t.Errorf("the report never says the percentile the check starts firing from:\n%s", out)
	}
	// The p95 of the headline measurements belongs in the summary line, not a mean.
	if strings.Contains(out, "mean") {
		t.Error("the population report mentions a mean: a mean startup time hides the viewer who left")
	}
}

func TestTextPopulation_SaysWhenTheAudienceIsTooSmallForAPercentile(t *testing.T) {
	r := popReport()
	r.Population.Viewers = 8
	r.Population.Startup = stat(8, 0.4, 1.2, 0, 4.8) // no p95: eight viewers cannot carry one
	r.Population.Frozen = stat(8, 0, 0, 0, 21.4)
	r.Population.Stalls = stat(8, 0, 0, 0, 6)
	r.Population.SwitchRate = stat(8, 0.4, 2.1, 0, 9.8)
	r.Population.Delivered = stat(8, 900e3, 2.4e6, 0, 3.6e6)
	for i := range r.Population.Checks {
		r.Population.Checks[i].AtP95 = ""
	}
	var b bytes.Buffer
	if err := TextPopulation(&b, r, false); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "—") {
		t.Errorf("an unsupported percentile is not shown as absent:\n%s", out)
	}
	if !strings.Contains(out, "20 viewers") {
		t.Errorf("the report does not say what a p95 would need:\n%s", out)
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

func TestTextPopulation_PutsTheP50BesideTheTail(t *testing.T) {
	var b bytes.Buffer
	if err := TextPopulation(&b, popReport(), false); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	out := b.String()
	// Zero frozen seconds at the p50 beside eighteen at the p95 is the one thing
	// a single-viewer run gets wrong, so the two have to be on the same line.
	frozen := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "frozen") {
			frozen = l
		}
	}
	if frozen == "" {
		t.Fatalf("no frozen row in:\n%s", out)
	}
	if !strings.Contains(frozen, "0.0s") || !strings.Contains(frozen, "18s") || !strings.Contains(frozen, "21s") {
		t.Errorf("the frozen row is %q — it has to carry the quiet p50 and the loud tail together", frozen)
	}
	if strings.Contains(out, "\x1b[") {
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
				Min, P50, Max float64
				P95, P99      *float64
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
	if got.Population.Startup.P95 == nil || *got.Population.Startup.P95 != 3.9 {
		t.Errorf("startup.p95 = %v, want 3.9", got.Population.Startup.P95)
	}
	if got.Population.Startup.P99 != nil {
		t.Errorf("startup.p99 = %v, want null: thirty viewers cannot carry a p99", *got.Population.Startup.P99)
	}
	if !strings.Contains(b.String(), "\"p99\": null") {
		t.Error("an unsupported percentile should be an explicit null, the same protocol as (value, false) elsewhere")
	}
	if strings.Contains(b.String(), "\"requests\"") {
		t.Error("the population document carries per-request timelines: two hundred of those is not a document anybody reads")
	}
}

// stat builds a distribution for the fixtures. p95 is omitted when it is zero,
// which is how a real Stat comes back from an audience too small to carry one.
func stat(viewers int, min, p50, p95, max float64) population.Stat {
	s := population.Stat{Viewers: viewers, Min: min, P50: p50, Max: max}
	if p95 != 0 {
		v := p95
		s.P95 = &v
	}
	return s
}

func richReport() PopulationReport {
	r := popReport()
	p := &r.Population
	p.QoE = stat(30, 0.4, 1.9, 0.7, 2.4)
	p.QoEWeights = analyze.QoEWeights{Rebuffer: 4.3, Switch: 1.0}
	p.Egress = stat(30, 400e6, 700e6, 1.1e9, 1.3e9)
	p.Rungs = []population.RungUse{
		{Rung: 0, Name: "360p", Bitrate: 800_000, Viewers: 30, Segments: 300, Seconds: 1200, Share: 0.25, Bytes: 120e6, PerViewerHour: 90e6},
		{Rung: 1, Name: "720p", Bitrate: 2_500_000, Viewers: 28, Segments: 900, Seconds: 3600, Share: 0.75, Bytes: 1.1e9, PerViewerHour: 820e6},
		{Rung: 2, Name: "1080p", Bitrate: 5_000_000, Viewers: 0, Segments: 0, Seconds: 0, Share: 0, Bytes: 0, PerViewerHour: 0},
	}
	return r
}

func TestTextPopulation_AttributesThePlaybackToTheRungs(t *testing.T) {
	var b bytes.Buffer
	if err := TextPopulation(&b, richReport(), false); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	out := b.String()
	for _, want := range []string{"rung", "360p", "720p", "1080p", "served", "viewers"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in the rung table:\n%s", want, out)
		}
	}
	// The rung nothing chose is the one worth arguing about, so it must be visible
	// and unmistakable.
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "1080p") {
			line = l
		}
	}
	if !strings.Contains(line, "nothing") && !strings.Contains(line, "0%") {
		t.Errorf("the unused rung reads %q — it has to say it served nobody", line)
	}
}

func TestTextPopulation_ManyIdleRungsBecomeOneLineThatStillNamesThem(t *testing.T) {
	// Apple's advanced example has 54 rungs, and on a 3 Mbps cell fifty of them
	// serve nothing: fifty rows of "nothing" bury the report. Compressing them is
	// fine; hiding them is not, because an unused rung is exactly what an operator
	// is deciding about.
	r := richReport()
	for i := 0; i < 9; i++ {
		r.Population.Rungs = append(r.Population.Rungs, population.RungUse{
			Rung: 3 + i, Name: fmt.Sprintf("idle%d", i), Bitrate: int64(6_000_000 + i*100_000),
		})
	}
	var b bytes.Buffer
	if err := TextPopulation(&b, r, false); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	out := b.String()
	rows := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "idle") && !strings.Contains(l, "served nothing") {
			rows++
		}
	}
	if rows > 0 {
		t.Errorf("%d idle rungs got a row of their own:\n%s", rows, out)
	}
	if !strings.Contains(out, "served nothing") {
		t.Errorf("the idle rungs vanished instead of being summarised:\n%s", out)
	}
	if !strings.Contains(out, "idle0") {
		t.Errorf("the summary does not name any of them, so nobody can act on it:\n%s", out)
	}
	if !strings.Contains(out, "10 rungs") {
		t.Errorf("the summary does not count them (1080p plus nine idle):\n%s", out)
	}
	if !strings.Contains(out, "--json") {
		t.Errorf("the summary does not say where the full list is:\n%s", out)
	}
	// The rungs that did serve playback keep their rows.
	for _, want := range []string{"360p", "720p"} {
		if !strings.Contains(out, want) {
			t.Errorf("the rung %s lost its row:\n%s", want, out)
		}
	}
}

func TestTextPopulation_NeverPrintsAQoEWithoutItsWeights(t *testing.T) {
	var b bytes.Buffer
	if err := TextPopulation(&b, richReport(), false); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "qoe") {
		t.Errorf("no QoE row:\n%s", out)
	}
	// The score is already Mbps-equivalent — a number like 1.90, not a bitrate.
	// Running it through the Mbps formatter divided it by a million and printed
	// 0.00 for every viewer, which a real run caught and no test had.
	// The first line starting with "qoe" is the table row; the second is the
	// sentence explaining the weights, and picking the last match found that one.
	qoeRow := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "qoe") && !strings.Contains(l, "Mbps-equivalent") {
			qoeRow = l
			break
		}
	}
	if qoeRow == "" {
		t.Fatalf("no qoe row:\n%s", out)
	}
	if !strings.Contains(qoeRow, "1.90") || !strings.Contains(qoeRow, "2.40") {
		t.Errorf("the qoe row is %q — it should carry the p50 of 1.9 and the max of 2.4, not a bitrate divided by a million", qoeRow)
	}
	if !strings.Contains(out, "4.3") || !strings.Contains(out, "per frozen second") {
		t.Errorf("the QoE weights are not printed beside the score:\n%s", out)
	}
	if !strings.Contains(out, "egress") {
		t.Errorf("no egress row — what an hour of watching costs to deliver:\n%s", out)
	}
}

func TestTextPopulation_SplitsTheAudienceByScreenOnlyWhenAsked(t *testing.T) {
	plain := richReport()
	var b bytes.Buffer
	if err := TextPopulation(&b, plain, false); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	if strings.Contains(b.String(), "phone") {
		t.Error("a report with no device mix mentions a device class — abrsim does not know who watches this stream")
	}

	withMix := richReport()
	withMix.Population.DeviceMix = "phone:50,tv:50"
	withMix.Population.Devices = []population.DeviceSpread{
		{Name: "phone", Ceiling: 720, Viewers: 15, Frozen: stat(15, 0, 0, 0, 4.2), QoE: stat(15, 1.1, 1.8, 0, 2.0), Egress: stat(15, 300e6, 500e6, 0, 700e6)},
		{Name: "tv", Ceiling: 0, Viewers: 15, Frozen: stat(15, 0, 1.4, 0, 21.4), QoE: stat(15, 0.4, 1.9, 0, 2.4), Egress: stat(15, 800e6, 1.1e9, 0, 1.3e9)},
	}
	b.Reset()
	if err := TextPopulation(&b, withMix, false); err != nil {
		t.Fatalf("TextPopulation: %v", err)
	}
	out := b.String()
	for _, want := range []string{"phone:50,tv:50", "phone", "tv", "720", "no cap"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in the device table:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "asked for") && !strings.Contains(out, "stated") {
		t.Errorf("the report does not say the mix was given rather than guessed:\n%s", out)
	}
}
