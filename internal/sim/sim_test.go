package sim

import (
	"math"
	"testing"

	"github.com/Allan-Nava/abrsim/internal/abr"
	"github.com/Allan-Nava/abrsim/internal/manifest"
	"github.com/Allan-Nava/abrsim/internal/trace"
)

func closeTo(t *testing.T, got, want float64, what string) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("%s = %.9f, want %.9f", what, got, want)
	}
}

// oneRung builds a ladder of a single 1 Mbps rung with n four-second segments.
// At 1 Mbps a four-second segment is exactly 500000 bytes, so every number in
// the tests below can be worked out on paper — which is the only way to know
// the simulator is right rather than merely self-consistent.
func oneRung(n int) manifest.Ladder {
	r := manifest.Rendition{Name: "1000k", Bandwidth: 1_000_000}
	for i := 0; i < n; i++ {
		r.Segments = append(r.Segments, manifest.Segment{Duration: 4})
	}
	manifest.FillDeclaredSizes(&r)
	return manifest.Ladder{Renditions: []manifest.Rendition{r}}
}

func flat(bps float64) trace.Trace {
	return trace.Trace{Name: "flat", Samples: []trace.Sample{{At: 0, BPS: bps}}}
}

func opts() Options {
	return Options{StartupBuffer: 4, BufferCap: 30}
}

func TestRun_ExactTimelineOnAFlatTrace(t *testing.T) {
	// 2 Mbps against a 1 Mbps rung: every four-second segment takes two
	// seconds, so the buffer grows by two seconds a segment and nothing ever
	// stalls. Five segments: twenty seconds of media in ten of wall clock.
	res, err := Run(oneRung(5), flat(2e6), &abr.Throughput{}, opts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	closeTo(t, res.Startup, 2, "time to first frame")
	closeTo(t, res.StallTime, 0, "stall time")
	closeTo(t, res.Wall, 10, "wall clock")
	closeTo(t, res.Media, 20, "media played")
	if res.Stalls != 0 || res.Switches != 0 {
		t.Errorf("stalls = %d, switches = %d, want 0 and 0", res.Stalls, res.Switches)
	}
	if res.Bytes != 5*500_000 {
		t.Errorf("bytes = %d, want %d", res.Bytes, 5*500_000)
	}
	if len(res.Requests) != 5 {
		t.Fatalf("got %d requests, want 5", len(res.Requests))
	}
	// Twenty seconds of media were fetched in ten seconds of clock, and
	// playback started at t=2 — so eight seconds have been watched and twelve
	// are still in the buffer.
	closeTo(t, res.Requests[4].BufferAfter, 12, "final buffer")
}

func TestRun_OutageStallsForExactlyWhatTheBufferCannotCover(t *testing.T) {
	// 2 Mbps, then nothing at all from t=4 to t=14, then 2 Mbps again.
	//
	//   seg0  0.0 → 2.0   buffer 4      playback starts at 2.0
	//   seg1  2.0 → 4.0   buffer 6
	//   seg2  4.0 → 16.0  ten seconds of blackout plus two of transfer, against
	//                     six seconds of buffer: six seconds of frozen picture
	//
	// If this number is wrong every rebuffer figure the tool prints is wrong.
	tr := trace.Trace{Name: "outage", Samples: []trace.Sample{
		{At: 0, BPS: 2e6}, {At: 4, BPS: 0}, {At: 14, BPS: 2e6},
	}}

	res, err := Run(oneRung(5), tr, &abr.Throughput{}, opts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Stalls != 1 {
		t.Errorf("stalls = %d, want 1", res.Stalls)
	}
	closeTo(t, res.StallTime, 6, "stall time")
	closeTo(t, res.Startup, 2, "time to first frame")
	if got := res.Requests[2].Stall; math.Abs(got-6) > 1e-6 {
		t.Errorf("the stall was attributed to request %d, not the one that caused it (%.3fs on request 2)", stalled(res), got)
	}
}

// stalled is the index of the first request that stalled, for the failure
// message above.
func stalled(r Result) int {
	for i, q := range r.Requests {
		if q.Stall > 0 {
			return i
		}
	}
	return -1
}

func TestRun_TheWaitForTheFirstFrameIsStartupNotRebuffering(t *testing.T) {
	// 200 kbps: the first four-second segment takes twenty seconds to arrive.
	// That wait is startup. Counting it as a stall as well would double-report
	// the one number an operator is most likely to act on — and the second
	// segment, which really does drain the buffer dry, is a rebuffer and has
	// to stay one.
	tr := trace.Trace{Name: "slow start", Samples: []trace.Sample{
		{At: 0, BPS: 200e3}, {At: 30, BPS: 5e6},
	}}
	res, err := Run(oneRung(4), tr, &abr.Throughput{}, opts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Requests[0].Stall != 0 {
		t.Errorf("the first segment was charged %.2fs of stall; it is startup", res.Requests[0].Stall)
	}
	closeTo(t, res.Startup, 20, "time to first frame")
	if res.Requests[1].Stall <= 0 {
		t.Error("the second segment drained the buffer to nothing and was not counted as a rebuffer")
	}
}

func TestRun_AFullBufferStopsThePlayerRequesting(t *testing.T) {
	// A player with a full buffer stops requesting and waits, which moves it
	// along the trace. Without that the simulation downloads a whole VOD asset
	// in the first ten seconds and reports a network nobody has.
	o := opts()
	o.BufferCap = 8
	res, err := Run(oneRung(10), flat(50e6), &abr.Throughput{}, o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The cap is where the player stops *requesting*, so the level peaks one
	// segment above it: you cannot fetch a four-second segment without
	// overshooting a threshold by up to four seconds. Asserting a hard ceiling
	// at the cap would be asserting a player nobody ships.
	ceiling := o.BufferCap + 4
	for i, q := range res.Requests {
		if q.BufferAfter > ceiling+1e-9 {
			t.Fatalf("request %d left %.3fs buffered, above the %.0fs cap plus one %.0fs segment", i, q.BufferAfter, o.BufferCap, q.Duration)
		}
	}
	var waited float64
	for _, q := range res.Requests {
		waited += q.Wait
	}
	if waited <= 0 {
		t.Error("nothing ever waited on an 8s cap fed by a 50 Mbps line")
	}

	// And the waiting has to show up on the clock: the same run with a buffer
	// nobody fills finishes sooner.
	o.BufferCap = 10_000
	free, err := Run(oneRung(10), flat(50e6), &abr.Throughput{}, o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if free.Wall >= res.Wall {
		t.Errorf("an 8s cap finished in %.3fs and an unbounded one in %.3fs — the cap never idled the player", res.Wall, free.Wall)
	}
}

func TestRun_DeadNetworkIsIncompleteNotSilent(t *testing.T) {
	// The trace ends on zero bandwidth: no finite time completes the download.
	// Reporting the segments that did arrive as a finished run would turn a
	// total outage into a clean result.
	tr := trace.Trace{Name: "dead", Samples: []trace.Sample{{At: 0, BPS: 2e6}, {At: 3, BPS: 0}}}
	res, err := Run(oneRung(10), tr, &abr.Throughput{}, opts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Incomplete {
		t.Fatal("the run claims to have completed over a network that stopped for good")
	}
	if len(res.Requests) >= 10 {
		t.Errorf("got %d requests over a dead network", len(res.Requests))
	}
}

func TestRun_MaxSecondsTruncates(t *testing.T) {
	o := opts()
	o.MaxSeconds = 12
	res, err := Run(oneRung(100), flat(10e6), &abr.Throughput{}, o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Truncated {
		t.Error("a truncated run does not say so, and its totals will be read as the whole asset")
	}
	if res.Media > 12+4 { // the segment that crosses the limit is still fetched whole
		t.Errorf("media = %.1fs, want it to stop at %.0fs", res.Media, o.MaxSeconds)
	}
}

// ladder builds a three-rung ladder so switching can be observed.
func ladder(n int) manifest.Ladder {
	l := manifest.Ladder{}
	for _, bps := range []int64{500_000, 2_000_000, 6_000_000} {
		r := manifest.Rendition{Bandwidth: bps}
		for i := 0; i < n; i++ {
			r.Segments = append(r.Segments, manifest.Segment{Duration: 4})
		}
		manifest.FillDeclaredSizes(&r)
		l.Renditions = append(l.Renditions, r)
	}
	manifest.NameRenditions(l.Renditions)
	return l
}

func TestRun_ClimbsAndCountsSwitches(t *testing.T) {
	// Plenty of bandwidth: the player starts at the bottom, because nothing has
	// been measured yet, and has to walk up. Every step is a switch.
	res, err := Run(ladder(12), flat(20e6), &abr.Throughput{}, opts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Requests[0].Rung != 0 {
		t.Errorf("first request took rung %d, want the bottom", res.Requests[0].Rung)
	}
	last := res.Requests[len(res.Requests)-1].Rung
	if last != 2 {
		t.Errorf("last request took rung %d, want the top on a 20 Mbps line", last)
	}
	if res.Switches == 0 {
		t.Error("the player climbed from the bottom to the top without a single switch being counted")
	}
	if res.StallTime != 0 {
		t.Errorf("%.3fs of stall on a network four times the top rung", res.StallTime)
	}
}

func TestRun_EveryAlgorithmSurvivesEveryBuiltInTrace(t *testing.T) {
	// The cheapest guard there is against a panic, a negative buffer or a NaN
	// reaching a renderer, and it costs one loop.
	for _, name := range abr.Names() {
		for _, tn := range trace.Names() {
			tr, _ := trace.Builtin(tn)
			a, _ := abr.New(name)
			res, err := Run(ladder(60), tr, a, opts())
			if err != nil {
				t.Fatalf("%s over %s: %v", name, tn, err)
			}
			for i, q := range res.Requests {
				switch {
				case math.IsNaN(q.BufferAfter) || q.BufferAfter < -1e-9:
					t.Fatalf("%s over %s: request %d left buffer %v", name, tn, i, q.BufferAfter)
				case math.IsNaN(q.Elapsed) || q.Elapsed < 0:
					t.Fatalf("%s over %s: request %d took %v", name, tn, i, q.Elapsed)
				case q.Rung < 0 || q.Rung > 2:
					t.Fatalf("%s over %s: request %d took rung %d", name, tn, i, q.Rung)
				}
			}
			if res.StallTime < 0 || math.IsNaN(res.StallTime) {
				t.Fatalf("%s over %s: stall time %v", name, tn, res.StallTime)
			}
		}
	}
}

func TestRun_IsReproducible(t *testing.T) {
	// The promise the whole tool rests on.
	tr, _ := trace.Builtin("mobile-4g")
	a, _ := abr.New("bola")
	first, err := Run(ladder(40), tr, a, opts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	a2, _ := abr.New("bola")
	second, err := Run(ladder(40), tr, a2, opts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if first.Wall != second.Wall || first.StallTime != second.StallTime || len(first.Requests) != len(second.Requests) {
		t.Fatalf("two identical runs differ: %.9f/%.9f vs %.9f/%.9f", first.Wall, first.StallTime, second.Wall, second.StallTime)
	}
	for i := range first.Requests {
		if first.Requests[i] != second.Requests[i] {
			t.Fatalf("request %d differs between two identical runs:\n %+v\n %+v", i, first.Requests[i], second.Requests[i])
		}
	}
}

func TestRun_RejectsAnEmptyLadder(t *testing.T) {
	if _, err := Run(manifest.Ladder{}, flat(1e6), &abr.Throughput{}, opts()); err == nil {
		t.Fatal("Run accepted a ladder with no rungs")
	}
}
