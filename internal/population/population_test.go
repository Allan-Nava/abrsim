package population

import (
	"fmt"
	"testing"

	"github.com/Allan-Nava/abrsim/internal/abr"
	"github.com/Allan-Nava/abrsim/internal/finding"
	"github.com/Allan-Nava/abrsim/internal/manifest"
	"github.com/Allan-Nava/abrsim/internal/sim"
	"github.com/Allan-Nava/abrsim/internal/trace"
)

// ladder builds a two-rung ladder whose arithmetic is known by construction: at
// 1 Mbps and 4 Mbps with four-second segments, a segment is exactly 500000 and
// 2000000 bytes.
func ladder(segments int) manifest.Ladder {
	l := manifest.Ladder{Source: "test://ladder"}
	for _, bps := range []int64{1_000_000, 4_000_000} {
		r := manifest.Rendition{
			Name: fmt.Sprintf("%dk", bps/1000), Bandwidth: bps, Average: bps,
			Width: 640, Height: 360, Codecs: "avc1.4d401e", URI: "test://media",
		}
		for i := 0; i < segments; i++ {
			r.Segments = append(r.Segments, manifest.Segment{
				URI: fmt.Sprintf("test://seg%d", i), Duration: 4,
				Bytes: bps * 4 / 8, Measured: true,
			})
		}
		l.Renditions = append(l.Renditions, r)
	}
	return l
}

func opts() sim.Options { return sim.Options{StartupBuffer: 2, BufferCap: 30}.Defaults() }

func TestRun_OneViewerIsTheRunThisToolAlwaysDid(t *testing.T) {
	l, base := ladder(30), mustTrace(t, "mobile-4g")

	got, err := Run(l, base, "bola", opts(), 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Viewers != 1 {
		t.Fatalf("Viewers = %d", got.Viewers)
	}

	alg, _ := abr.New("bola")
	want, err := sim.Run(l, base, alg, opts())
	if err != nil {
		t.Fatalf("sim.Run: %v", err)
	}
	if got.Runs[0].Startup != want.Startup || got.Runs[0].Frozen != want.StallTime ||
		got.Runs[0].Stalls != want.Stalls || got.Runs[0].Switches != want.Switches {
		t.Errorf("one-viewer population = %+v, single simulation gave startup=%v frozen=%v stalls=%d switches=%d — --viewers 1 has to stay the run it always was",
			got.Runs[0], want.Startup, want.StallTime, want.Stalls, want.Switches)
	}
	if got.Startup.Min != want.Startup || got.Startup.P50 != want.Startup || got.Startup.Max != want.Startup {
		t.Errorf("one viewer's distribution = %+v, want min, p50 and max all equal to %v", got.Startup, want.Startup)
	}
	if got.Startup.P95 != nil || got.Startup.P99 != nil {
		t.Error("one viewer produced a p95 or a p99: one session is not a distribution")
	}
}

func TestRun_ViewersComeBackInIndexOrderNotFinishOrder(t *testing.T) {
	l, base := ladder(24), mustTrace(t, "mobile-4g")
	const n = 40

	got, err := Run(l, base, "throughput", opts(), n)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got.Runs) != n {
		t.Fatalf("%d runs, want %d", len(got.Runs), n)
	}
	for i, r := range got.Runs {
		if r.Index != i {
			t.Fatalf("Runs[%d].Index = %d — the goroutine that finished first must not decide what the report says", i, r.Index)
		}
	}

	// Viewer 7 simulated on its own has to be viewer 7 in the population: the
	// algorithm is stateful, so every viewer needs its own instance.
	pop := trace.Population(base, n)
	alg, _ := abr.New("throughput")
	solo, err := sim.Run(l, pop[7], alg, opts())
	if err != nil {
		t.Fatalf("sim.Run: %v", err)
	}
	if got.Runs[7].Startup != solo.Startup || got.Runs[7].Frozen != solo.StallTime || got.Runs[7].Switches != solo.Switches {
		t.Errorf("viewer 7 in the population = %+v, on its own = startup %v frozen %v switches %d — state is leaking between viewers",
			got.Runs[7], solo.Startup, solo.StallTime, solo.Switches)
	}
}

func TestRun_IsDeterministic(t *testing.T) {
	l, base := ladder(20), mustTrace(t, "train")
	a, err := Run(l, base, "buffer", opts(), 25)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	b, err := Run(l, base, "buffer", opts(), 25)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sameStat(a.Startup, b.Startup) || !sameStat(a.Frozen, b.Frozen) || !sameStat(a.Delivered, b.Delivered) {
		t.Errorf("two runs of the same population disagree: %+v then %+v", a, b)
	}
	for i := range a.Checks {
		if a.Checks[i] != b.Checks[i] {
			t.Errorf("check %d: %+v then %+v", i, a.Checks[i], b.Checks[i])
		}
	}
	for i := range a.Runs {
		if a.Runs[i] != b.Runs[i] {
			t.Errorf("viewer %d: %+v then %+v", i, a.Runs[i], b.Runs[i])
		}
	}
}

func TestRun_EveryCheckStillSpeaks(t *testing.T) {
	l, base := ladder(20), mustTrace(t, "mobile-4g")
	got, err := Run(l, base, "bola", opts(), 12)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"coverage", "efficiency", "ladder-gap", "rebuffer", "sizes", "startup", "switches"}
	seen := map[string]bool{}
	for _, c := range got.Checks {
		seen[c.Check] = true
		if c.OK+c.Warn+c.Bad+c.Error != got.Viewers {
			t.Errorf("%s: %d+%d+%d+%d findings across %d viewers — a check that goes quiet for some of them is indistinguishable from one that passed",
				c.Check, c.OK, c.Warn, c.Bad, c.Error, got.Viewers)
		}
	}
	for _, name := range want {
		if !seen[name] {
			t.Errorf("no %s in the population report", name)
		}
	}
	if len(got.Checks) != len(want) {
		t.Errorf("%d checks, want %d", len(got.Checks), len(want))
	}
}

func TestRun_WorstFirstAndTheCountThatMatters(t *testing.T) {
	// The staircase down, with a top rung the bottom of it cannot carry: 12 of
	// these 30 viewers freeze and 18 do not, which is the entire argument for
	// looking at an audience. The median viewer here has nothing to report.
	l, base := ladder(40), mustTrace(t, "steps-down")
	got, err := Run(l, base, "bola", opts(), 30)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i := 1; i < len(got.Checks); i++ {
		if finding.Severity(got.Checks[i-1].Worst) < finding.Severity(got.Checks[i].Worst) {
			t.Errorf("checks are not worst-first: %s (%s) before %s (%s)",
				got.Checks[i-1].Check, got.Checks[i-1].Worst, got.Checks[i].Check, got.Checks[i].Worst)
		}
	}
	var reb CheckSpread
	for _, c := range got.Checks {
		if c.Check == "rebuffer" {
			reb = c
		}
	}
	if reb.Loud == 0 || reb.Loud == got.Viewers {
		t.Errorf("rebuffer went loud for %d of %d viewers: a fixture where everybody or nobody freezes proves nothing about a population — %+v", reb.Loud, got.Viewers, reb)
	}
	if reb.Worst != finding.BAD {
		t.Errorf("rebuffer's worst status across the audience is %s, want BAD — somebody in here froze for a long time", reb.Worst)
	}
	if med := got.Frozen.P50; med != 0 {
		t.Errorf("the median viewer froze for %.2fs: this fixture is meant to show a median that looks clean while the tail does not", med)
	}
	if got.Frozen.Max <= 0 {
		t.Error("the worst viewer froze for nothing at all, so there is no tail to report")
	}
	if reb.WorstMessage == "" {
		t.Error("the worst rebuffer finding carries no message — a count with no sentence beside it is a number an operator cannot act on")
	}
}

func TestRun_RefusesAnEmptyAudienceAndAnUnknownAlgorithm(t *testing.T) {
	l, base := ladder(6), mustTrace(t, "flat-5m")
	for _, n := range []int{0, -3} {
		if _, err := Run(l, base, "bola", opts(), n); err == nil {
			t.Errorf("Run with %d viewers returned no error", n)
		}
	}
	if _, err := Run(l, base, "nope", opts(), 4); err == nil {
		t.Error("Run with an unknown algorithm returned no error")
	}
}

func TestStat_EveryFigureIsAViewerThatExisted(t *testing.T) {
	// Nearest-rank order statistics, no interpolation. An interpolated median of
	// [1,2,3,4] is 2.5 — a number no viewer in that audience experienced — and
	// inventing a measurement is the one thing this tool must not do.
	cases := []struct {
		in      []float64
		lo, p50 float64
		hi      float64
	}{
		{[]float64{5}, 5, 5, 5},
		{[]float64{3, 1, 2}, 1, 2, 3},
		{[]float64{4, 1, 3, 2}, 1, 2, 4},
		{[]float64{2, 2, 2, 2}, 2, 2, 2},
	}
	for _, c := range cases {
		got := statOf(c.in)
		if got.Min != c.lo || got.P50 != c.p50 || got.Max != c.hi {
			t.Errorf("statOf(%v) = min %v p50 %v max %v, want %v %v %v", c.in, got.Min, got.P50, got.Max, c.lo, c.p50, c.hi)
		}
		for _, v := range []float64{got.Min, got.P50, got.Max} {
			found := false
			for _, in := range c.in {
				if in == v {
					found = true
				}
			}
			if !found {
				t.Errorf("statOf(%v) reported %v, which no viewer had", c.in, v)
			}
		}
	}
	if got := statOf(nil); got.Viewers != 0 || got.P95 != nil {
		t.Errorf("statOf(nil) = %+v, want an empty stat with no percentiles", got)
	}
}

func TestStat_APercentileTheAudienceCannotSupportIsNotReported(t *testing.T) {
	// A p95 over ten viewers is the maximum wearing a better name: the tail has
	// no resolution there. Twenty viewers is the first audience in which one
	// viewer *is* the top 5%, and a hundred for the top 1%.
	seq := func(n int) []float64 {
		out := make([]float64, n)
		for i := range out {
			out[i] = float64(i)
		}
		return out
	}
	if got := statOf(seq(10)); got.P95 != nil || got.P99 != nil {
		t.Errorf("ten viewers produced a p95/p99: %+v — that is a resolution the audience does not have", got)
	}
	got := statOf(seq(20))
	if got.P95 == nil {
		t.Fatal("twenty viewers should support a p95")
	}
	if *got.P95 != 18 {
		t.Errorf("p95 of 0..19 = %v, want 18 (nearest rank: the 19th of twenty)", *got.P95)
	}
	if got.P99 != nil {
		t.Errorf("twenty viewers produced a p99 of %v", *got.P99)
	}
	if got := statOf(seq(100)); got.P99 == nil || *got.P99 != 98 {
		t.Errorf("p99 of 0..99 = %v, want 98", got.P99)
	}
}

func TestRun_EachCheckCarriesItsSeverityAtThePercentiles(t *testing.T) {
	// The point of AB-37: not "this went BAD somewhere" but "at the 95th
	// percentile of your audience this is BAD", with the p50 beside it so a
	// reader sees the median hiding the tail.
	l, base := ladder(40), mustTrace(t, "steps-down")
	got, err := Run(l, base, "bola", opts(), 40)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var reb CheckSpread
	for _, c := range got.Checks {
		if c.Check == "rebuffer" {
			reb = c
		}
	}
	if reb.AtP50 != finding.OK {
		t.Errorf("rebuffer at p50 is %s, want OK — this fixture's median viewer is fine, which is the whole point", reb.AtP50)
	}
	if reb.AtP95 != finding.BAD {
		t.Errorf("rebuffer at p95 is %s, want BAD", reb.AtP95)
	}
	if reb.AtP99 != "" {
		t.Errorf("rebuffer reported a p99 (%s) with only 40 viewers", reb.AtP99)
	}
	if reb.Loud > 0 {
		want := 100 * float64(reb.OK) / float64(got.Viewers)
		if reb.FiresFrom < want-0.01 || reb.FiresFrom > want+0.01 {
			t.Errorf("FiresFrom = %.1f, want %.1f (the share of the audience it stays quiet for)", reb.FiresFrom, want)
		}
	}
}

func TestRun_ChecksAreOrderedByWhatHappensAtTheP95(t *testing.T) {
	l, base := ladder(40), mustTrace(t, "steps-down")
	got, err := Run(l, base, "bola", opts(), 40)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i := 1; i < len(got.Checks); i++ {
		a, b := got.Checks[i-1], got.Checks[i]
		if finding.Severity(a.AtP95) < finding.Severity(b.AtP95) {
			t.Errorf("%s (p95 %s) comes before %s (p95 %s): worst first means worst at the percentile an operator is paid to care about",
				a.Check, a.AtP95, b.Check, b.AtP95)
		}
	}
}

// sameStat compares two distributions including the percentiles a small audience
// leaves unreported, which is why Stat cannot be compared with ==.
func sameStat(a, b Stat) bool {
	same := func(x, y *float64) bool {
		if x == nil || y == nil {
			return x == nil && y == nil
		}
		return *x == *y
	}
	return a.Min == b.Min && a.P50 == b.P50 && a.Max == b.Max &&
		a.Viewers == b.Viewers && same(a.P95, b.P95) && same(a.P99, b.P99)
}

func mustTrace(t *testing.T, name string) trace.Trace {
	t.Helper()
	tr, ok := trace.Builtin(name)
	if !ok {
		t.Fatalf("no built-in trace %q", name)
	}
	return tr
}

func TestRun_TheWorstFindingIsTheWorstCaseNotTheFirstOffender(t *testing.T) {
	// Found by running against a real stream: 29 of 50 viewers rebuffered, the
	// table said the worst froze for 69 seconds, and the headline sentence
	// quoted a viewer who froze for 2.4 — the first one to cross the threshold.
	// A report whose worst line and whose maximum disagree teaches an operator
	// to distrust both.
	l, base := ladder(40), mustTrace(t, "steps-down")
	got, err := Run(l, base, "bola", opts(), 50)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var reb CheckSpread
	for _, c := range got.Checks {
		if c.Check == "rebuffer" {
			reb = c
		}
	}
	if reb.Loud == 0 {
		t.Fatal("nobody rebuffered, so this proves nothing")
	}
	if got.Runs[reb.WorstViewer].Frozen != got.Frozen.Max {
		t.Errorf("the worst rebuffer line names viewer %d, frozen for %.1fs, while the audience's worst is %.1fs",
			reb.WorstViewer, got.Runs[reb.WorstViewer].Frozen, got.Frozen.Max)
	}

	var startup CheckSpread
	for _, c := range got.Checks {
		if c.Check == "startup" {
			startup = c
		}
	}
	if startup.Loud > 0 && got.Runs[startup.WorstViewer].Startup != got.Startup.Max {
		t.Errorf("the worst startup line names viewer %d at %.2fs, while the slowest first frame in the audience was %.2fs",
			startup.WorstViewer, got.Runs[startup.WorstViewer].Startup, got.Startup.Max)
	}
}

func TestRun_AQuietCheckQuotesTheFirstViewer(t *testing.T) {
	l, base := ladder(20), mustTrace(t, "flat-5m")
	got, err := Run(l, base, "bola", opts(), 10)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, c := range got.Checks {
		if c.Loud == 0 && c.WorstViewer != 0 {
			t.Errorf("%s is quiet everywhere but quotes viewer %d — with nothing to rank, the sentence should be the first viewer's so the row is stable", c.Check, c.WorstViewer)
		}
	}
}
