package analyze

import (
	"strings"
	"testing"

	"github.com/Allan-Nava/abrsim/internal/abr"
	"github.com/Allan-Nava/abrsim/internal/finding"
	"github.com/Allan-Nava/abrsim/internal/manifest"
	"github.com/Allan-Nava/abrsim/internal/sim"
	"github.com/Allan-Nava/abrsim/internal/trace"
)

// ladderOf builds a ladder from bitrates, with n four-second segments a rung.
func ladderOf(n int, bitrates ...int64) manifest.Ladder {
	l := manifest.Ladder{Source: "test"}
	for _, bps := range bitrates {
		r := manifest.Rendition{Bandwidth: bps}
		for i := 0; i < n; i++ {
			r.Segments = append(r.Segments, manifest.Segment{Duration: 4})
		}
		manifest.FillDeclaredSizes(&r)
		l.Renditions = append(l.Renditions, r)
	}
	// Sizes are filled first and only then marked measured: flagging them
	// before would make FillDeclaredSizes skip them and leave every segment at
	// zero bytes. Marking them at all keeps these tests off the `sizes`
	// finding and about the check each one is named for.
	for i := range l.Renditions {
		for j := range l.Renditions[i].Segments {
			l.Renditions[i].Segments[j].Measured = true
		}
	}
	manifest.NameRenditions(l.Renditions)
	return l
}

// healthy is a well-spaced ladder: no ratio between neighbours above 1.8.
func healthy(n int) manifest.Ladder {
	return ladderOf(n, 400_000, 700_000, 1_200_000, 2_000_000, 3_400_000, 5_500_000)
}

func flat(bps float64) trace.Trace {
	return trace.Trace{Name: "flat", Samples: []trace.Sample{{At: 0, BPS: bps}}}
}

func run(t *testing.T, l manifest.Ladder, tr trace.Trace, alg string) []finding.Finding {
	t.Helper()
	a, ok := abr.New(alg)
	if !ok {
		t.Fatalf("no algorithm %q", alg)
	}
	return runAlg(t, l, tr, a)
}

func runAlg(t *testing.T, l manifest.Ladder, tr trace.Trace, a abr.Algorithm) []finding.Finding {
	t.Helper()
	res, err := sim.Run(l, tr, a, sim.Options{StartupBuffer: 4, BufferCap: 30})
	if err != nil {
		t.Fatalf("sim.Run: %v", err)
	}
	return Run(res, tr, l)
}

func pick(fs []finding.Finding, check string) (finding.Finding, bool) {
	for _, f := range fs {
		if f.Check == check {
			return f, true
		}
	}
	return finding.Finding{}, false
}

func TestRun_HealthyStreamHasNoProblems(t *testing.T) {
	// A well-spaced ladder on a line comfortably above its top rung. A checker
	// that cries wolf here is worse than no checker: the first false positive
	// is the last time anyone reads the output.
	fs := run(t, healthy(60), flat(9e6), "bola")
	for _, f := range fs {
		if f.Status != finding.OK {
			t.Errorf("%s %s: %s — %s", f.Status, f.Check, f.Target, f.Message)
		}
	}
	if len(fs) < 5 {
		t.Errorf("only %d findings: a check that falls silent is as bad as one that lies", len(fs))
	}
}

func TestRun_EveryCheckAlwaysSpeaks(t *testing.T) {
	// Each check reports at every severity or not at all; a check that goes
	// quiet on a stream it did not like is a hole nobody notices.
	want := []string{"rebuffer", "startup", "switches", "efficiency", "ladder-gap"}
	for _, tn := range trace.Names() {
		tr, _ := trace.Builtin(tn)
		fs := run(t, healthy(80), tr, "throughput")
		for _, check := range want {
			if _, ok := pick(fs, check); !ok {
				t.Errorf("%s: no %s finding at all", tn, check)
			}
		}
	}
}

func TestRebuffer_NamesTheFrozenSeconds(t *testing.T) {
	// Two long blackouts against a 30-second buffer cap.
	tr := trace.Trace{Name: "tunnels", Samples: []trace.Sample{
		{At: 0, BPS: 6e6}, {At: 30, BPS: 0}, {At: 90, BPS: 6e6},
	}}
	fs := run(t, healthy(40), tr, "throughput")

	f, ok := pick(fs, "rebuffer")
	if !ok {
		t.Fatal("no rebuffer finding")
	}
	if f.Status != finding.BAD {
		t.Errorf("rebuffer is %s over a sixty-second blackout, want BAD", f.Status)
	}
	if f.Value == nil || *f.Value <= 0 {
		t.Fatalf("rebuffer carries no measurement: %+v", f)
	}
	if f.Unit != "s" {
		t.Errorf("rebuffer unit = %q, want seconds", f.Unit)
	}
	if f.Hint == "" {
		t.Error("rebuffer has no hint — a number without a consequence is not a finding")
	}
}

func TestStartup_MeasuresTheWaitForTheFirstFrame(t *testing.T) {
	// A line that only just carries the bottom rung: the first segment crawls.
	fs := run(t, healthy(20), flat(200e3), "throughput")

	f, ok := pick(fs, "startup")
	if !ok {
		t.Fatal("no startup finding")
	}
	if f.Status == finding.OK {
		t.Errorf("startup is OK on a 200 kbps line: %s", f.Message)
	}
	if f.Value == nil || *f.Value < 4 {
		t.Errorf("startup value = %v, want the several seconds it really took", f.Value)
	}
}

func TestLadderGap_NamesTheRungThatDoesNotExist(t *testing.T) {
	// A hole by construction: 800k, then nothing until 5000k. On a 3 Mbps line
	// the player is stuck at 800k for the whole run — the network could carry
	// nearly four times that, and there is no rung to spend it on.
	//
	// This is the finding the tool exists for, so it has to name both ends of
	// the hole and the seconds the viewer paid for it.
	l := ladderOf(60, 400_000, 800_000, 5_000_000)
	fs := run(t, l, flat(3e6), "throughput")

	f, ok := pick(fs, "ladder-gap")
	if !ok {
		t.Fatal("no ladder-gap finding")
	}
	if f.Status != finding.BAD {
		t.Errorf("ladder-gap is %s: %s", f.Status, f.Message)
	}
	if !strings.Contains(f.Target, "800k") || !strings.Contains(f.Target, "5000k") {
		t.Errorf("target = %q, want both ends of the hole named", f.Target)
	}
	if f.Value == nil || *f.Value < 60 {
		t.Errorf("value = %v seconds, want most of the run", f.Value)
	}
	if !strings.Contains(f.Message, "3.0 Mbps") && !strings.Contains(f.Message, "2.9 Mbps") {
		t.Errorf("message does not say what the network could actually carry: %q", f.Message)
	}
}

func TestLadderGap_QuietWhenTheNetworkIsTheLimit(t *testing.T) {
	// The same hole, but the line only carries 900 kbps. The player sits at
	// 800k because that is all there is room for, not because a rung is
	// missing — reporting a gap here would send someone to add a rung that
	// would never be chosen.
	l := ladderOf(60, 400_000, 800_000, 5_000_000)
	fs := run(t, l, flat(900e3), "throughput")

	f, ok := pick(fs, "ladder-gap")
	if !ok {
		t.Fatal("no ladder-gap finding")
	}
	if f.Status != finding.OK {
		t.Errorf("ladder-gap is %s on a line that cannot carry the next rung anyway: %s", f.Status, f.Message)
	}
}

func TestLadderGap_QuietAtTheTopOfTheLadder(t *testing.T) {
	// Ten megabits against a ladder that stops at 5.5: the player sits at the
	// top with headroom to spare. That is the ladder ending, not a hole in it,
	// and it is a question about encoding cost rather than a defect.
	fs := run(t, healthy(40), flat(20e6), "throughput")

	f, ok := pick(fs, "ladder-gap")
	if !ok {
		t.Fatal("no ladder-gap finding")
	}
	if f.Status != finding.OK {
		t.Errorf("ladder-gap is %s when the player is already on the top rung: %s", f.Status, f.Message)
	}
}

func TestSwitches_CountsOscillation(t *testing.T) {
	// A line that flips between two rungs every four seconds, read by an
	// estimator with no smoothing at all (Alpha 1: the estimate *is* the last
	// sample). That combination is precisely the failure this check exists to
	// name — an estimator chasing noise rather than the network really moving.
	//
	// The default EWMA absorbs a flap this fast and barely switches at all,
	// which is the right behaviour and the reason it is the default; asserting
	// against it here would be testing the smoothing, not the check.
	var samples []trace.Sample
	for i := 0; i < 60; i++ {
		bps := 1.0e6
		if i%2 == 0 {
			bps = 6e6
		}
		samples = append(samples, trace.Sample{At: float64(i) * 4, BPS: bps})
	}
	fs := runAlg(t, healthy(50), trace.Trace{Name: "flapping", Samples: samples}, &abr.Throughput{Alpha: 1})

	f, ok := pick(fs, "switches")
	if !ok {
		t.Fatal("no switches finding")
	}
	if f.Status == finding.OK {
		t.Errorf("switches is OK on a flapping line: %s", f.Message)
	}
	if f.Unit != "switches/min" {
		t.Errorf("unit = %q, want a rate — a raw count means nothing without the duration", f.Unit)
	}
}

func TestEfficiency_ReportsWhatWasLeftOnTheTable(t *testing.T) {
	fs := run(t, healthy(40), flat(9e6), "throughput")
	f, ok := pick(fs, "efficiency")
	if !ok {
		t.Fatal("no efficiency finding")
	}
	if f.Value == nil || *f.Value <= 0 || *f.Value > 100 {
		t.Errorf("efficiency value = %v, want a percentage", f.Value)
	}
	if f.Unit != "%" {
		t.Errorf("unit = %q", f.Unit)
	}
}

func TestRun_IncompleteRunIsAnErrorNotACleanResult(t *testing.T) {
	// The network stopped for good. ERROR, because the coverage has a hole —
	// not BAD, which would send someone hunting a defect in a stream nobody
	// managed to finish looking at.
	tr := trace.Trace{Name: "dead", Samples: []trace.Sample{{At: 0, BPS: 4e6}, {At: 20, BPS: 0}}}
	fs := run(t, healthy(60), tr, "throughput")

	f, ok := pick(fs, "coverage")
	if !ok {
		t.Fatal("no coverage finding on a run that never finished")
	}
	if f.Status != finding.ERROR {
		t.Errorf("coverage is %s, want ERROR", f.Status)
	}
}

func TestRun_EstimatedSizesAreDeclaredAsSuch(t *testing.T) {
	// Nothing was downloaded, so every byte count is derived from BANDWIDTH.
	// A simulation reported as a measurement is the one way this tool can lie,
	// so it has to say so in the output rather than in the documentation.
	l := manifest.Ladder{}
	for _, bps := range []int64{500_000, 2_000_000} {
		r := manifest.Rendition{Bandwidth: bps}
		for i := 0; i < 30; i++ {
			r.Segments = append(r.Segments, manifest.Segment{Duration: 4})
		}
		manifest.FillDeclaredSizes(&r)
		l.Renditions = append(l.Renditions, r)
	}
	manifest.NameRenditions(l.Renditions)

	fs := run(t, l, flat(5e6), "throughput")
	f, ok := pick(fs, "sizes")
	if !ok {
		t.Fatal("no sizes finding on a run with no measured segment")
	}
	if f.Status != finding.OK {
		t.Errorf("sizes is %s: an estimate is a limit of the run, not a defect in the stream", f.Status)
	}
	if !strings.Contains(strings.ToLower(f.Message), "declared") {
		t.Errorf("message does not say the sizes were declared rather than measured: %q", f.Message)
	}
}

func TestRun_SortsWorstFirst(t *testing.T) {
	tr := trace.Trace{Name: "tunnels", Samples: []trace.Sample{
		{At: 0, BPS: 6e6}, {At: 30, BPS: 0}, {At: 90, BPS: 6e6},
	}}
	fs := run(t, healthy(40), tr, "throughput")
	for i := 1; i < len(fs); i++ {
		if finding.Severity(fs[i-1].Status) < finding.Severity(fs[i].Status) {
			t.Fatalf("finding %d (%s) sorts above %d (%s)", i-1, fs[i-1].Status, i, fs[i].Status)
		}
	}
}

func TestCoverage_SaysWhenTheRunOutlivedTheTrace(t *testing.T) {
	// A trace holds its last rate forever, which is the only honest
	// extrapolation — but a session that plays for twenty minutes against a
	// five-minute recording has three quarters of its network invented, and
	// every figure above is that much guesswork. Saying so is the difference
	// between a measurement and a number.
	tr, _ := trace.Builtin("train") // 300s
	fs := run(t, healthy(400), tr, "throughput")

	f, ok := pick(fs, "coverage")
	if !ok {
		t.Fatal("no coverage finding")
	}
	if f.Status != finding.WARN {
		t.Errorf("coverage is %s on a run that outlived its trace four times over: %s", f.Status, f.Message)
	}
	if f.Value == nil || *f.Value < 50 {
		t.Errorf("value = %v, want the percentage of the run that was extrapolated", f.Value)
	}
	if !strings.Contains(f.Message, "300") && !strings.Contains(f.Message, "extrapolat") {
		t.Errorf("the message does not say the trace ran out: %q", f.Message)
	}
}

func TestCoverage_SpeaksOnEveryRun(t *testing.T) {
	fs := run(t, healthy(20), flat(9e6), "bola")
	f, ok := pick(fs, "coverage")
	if !ok {
		t.Fatal("coverage went quiet on a run it had nothing to complain about")
	}
	if f.Status != finding.OK {
		t.Errorf("coverage is %s: %s", f.Status, f.Message)
	}
}

func TestCoverage_ASingleSampleTraceIsNotARecordingThatRanOut(t *testing.T) {
	// One sample is a declared constant — "assume 5 Mbps" — not a measurement
	// that stopped. Warning about extrapolating it would fire on every
	// synthetic run anybody ever does.
	fs := run(t, healthy(200), flat(5e6), "throughput")
	f, _ := pick(fs, "coverage")
	if f.Status != finding.OK {
		t.Errorf("coverage is %s over a flat trace: %s", f.Status, f.Message)
	}
}

func TestEfficiency_MeasuresBytesDeliveredNotBitratesDeclared(t *testing.T) {
	// A rung's BANDWIDTH is a declared upper bound, and Apple's own advanced
	// example over-declares it enough to make the ratio exceed 100% — which
	// read as fine, because it was OK-level. The numerator has to be the bytes
	// that actually crossed the wire.
	//
	// Here every rung declares 4 Mbps and every segment really carries 1 Mbps,
	// against a 4 Mbps line. Efficiency is 25%, not 100%.
	l := manifest.Ladder{}
	r := manifest.Rendition{Bandwidth: 4_000_000}
	for i := 0; i < 40; i++ {
		r.Segments = append(r.Segments, manifest.Segment{Duration: 4, Bytes: 500_000, Measured: true})
	}
	l.Renditions = append(l.Renditions, r, r)
	manifest.NameRenditions(l.Renditions)

	fs := run(t, l, flat(4e6), "throughput")
	f, ok := pick(fs, "efficiency")
	if !ok {
		t.Fatal("no efficiency finding")
	}
	if f.Value == nil {
		t.Fatal("efficiency carries no measurement")
	}
	if *f.Value > 40 {
		t.Errorf("efficiency = %.0f%%, want ~25%% — the declared bitrate was used instead of the bytes delivered", *f.Value)
	}
}

func TestEfficiency_NeverReadsAsImpossible(t *testing.T) {
	// Playing more bits per second of media than the line averaged is real —
	// the player draws on buffer built while it was faster — but printed as a
	// bare percentage above 100 it reads as a bug in the tool, and readers
	// stop trusting the rest of the report.
	tr := trace.Trace{Name: "collapse", Samples: []trace.Sample{
		{At: 0, BPS: 30e6}, {At: 12, BPS: 250e3},
	}}
	fs := run(t, healthy(30), tr, "throughput")
	f, _ := pick(fs, "efficiency")
	if f.Value != nil && *f.Value > 100 && !strings.Contains(f.Message, "buffer") {
		t.Errorf("efficiency reads %.0f%% with no explanation: %q", *f.Value, f.Message)
	}
}

func TestEfficiency_NeverExceedsOK(t *testing.T) {
	// Efficiency reports a number and makes no judgement — see the comment on
	// checkEfficiency. Both attempts at a severity fired on healthy reference
	// streams, so until AB-34 defines steady state the check states what it
	// measured and stops there. A caller who wants an opinion has ladder-gap,
	// which distinguishes a hole in the ladder from a rung the player declined.
	for _, tn := range trace.Names() {
		tr, _ := trace.Builtin(tn)
		for _, alg := range abr.Names() {
			for _, l := range []manifest.Ladder{healthy(19), healthy(80), ladderOf(40, 400_000, 800_000, 8_000_000)} {
				f, _ := pick(run(t, l, tr, alg), "efficiency")
				if f.Status != finding.OK && f.Status != finding.ERROR {
					t.Errorf("%s over %s: efficiency is %s — %s", alg, tn, f.Status, f.Message)
				}
			}
		}
	}
}
