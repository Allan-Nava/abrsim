package trace

import (
	"math"
	"strings"
	"testing"
)

// closeTo compares seconds with a tolerance well below anything the simulator
// can observe: the point of the trace is that two runs of it are identical, so
// a millisecond of slack is generous.
func closeTo(t *testing.T, got, want float64, what string) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("%s = %.9f, want %.9f", what, got, want)
	}
}

func TestDownload_WithinOneSample(t *testing.T) {
	// 1 Mbps flat. One megabit is 125000 bytes and takes exactly one second.
	tr := Trace{Samples: []Sample{{At: 0, BPS: 1e6}}}

	got, ok := tr.Download(0, 125_000)
	if !ok {
		t.Fatal("Download reported it could not complete on a flat non-zero trace")
	}
	closeTo(t, got, 1.0, "elapsed")
}

func TestDownload_CrossesSampleBoundaries(t *testing.T) {
	// 2 Mbps for the first second, 1 Mbps thereafter. 500000 bytes is 4 Mbit:
	// 2 Mbit go in the first second, the remaining 2 Mbit take two more.
	tr := Trace{Samples: []Sample{{At: 0, BPS: 2e6}, {At: 1, BPS: 1e6}}}

	got, ok := tr.Download(0, 500_000)
	if !ok {
		t.Fatal("Download reported it could not complete")
	}
	closeTo(t, got, 3.0, "elapsed")
}

func TestDownload_StartingMidSample(t *testing.T) {
	// Same trace, but the request starts half a second in: only 1 Mbit is left
	// at the fast rate, so 3 Mbit remain at 1 Mbps.
	tr := Trace{Samples: []Sample{{At: 0, BPS: 2e6}, {At: 1, BPS: 1e6}}}

	got, ok := tr.Download(0.5, 500_000)
	if !ok {
		t.Fatal("Download reported it could not complete")
	}
	closeTo(t, got, 3.5, "elapsed")
}

func TestDownload_PastTheEndHoldsTheLastRate(t *testing.T) {
	// A trace is a measurement, not a promise about the future. Holding the
	// last rate is the only honest extrapolation, and the alternative — the
	// download never completing — would report a stall the network never had.
	tr := Trace{Samples: []Sample{{At: 0, BPS: 4e6}, {At: 10, BPS: 1e6}}}

	got, ok := tr.Download(100, 125_000)
	if !ok {
		t.Fatal("Download reported it could not complete past the last sample")
	}
	closeTo(t, got, 1.0, "elapsed")
}

func TestDownload_ZeroBytesTakesNoTime(t *testing.T) {
	tr := Trace{Samples: []Sample{{At: 0, BPS: 1e6}}}
	got, ok := tr.Download(0, 0)
	if !ok {
		t.Fatal("a zero-byte download must succeed")
	}
	closeTo(t, got, 0, "elapsed")
}

func TestDownload_DeadTailCannotComplete(t *testing.T) {
	// The trace drops to nothing and stays there. There is no elapsed time that
	// finishes this download, and inventing one — or silently returning the
	// time up to the outage — would report a stall as a completed request.
	tr := Trace{Samples: []Sample{{At: 0, BPS: 1e6}, {At: 1, BPS: 0}}}

	if got, ok := tr.Download(0, 10_000_000); ok {
		t.Fatalf("Download returned %.3fs over a dead tail, want ok=false", got)
	}
}

func TestDownload_SurvivesAnOutageThatEnds(t *testing.T) {
	// Zero bandwidth for five seconds, then it comes back. The download takes
	// the outage plus the transfer: this is exactly the shape that produces a
	// rebuffer, so getting it wrong makes the tool useless.
	tr := Trace{Samples: []Sample{{At: 0, BPS: 0}, {At: 5, BPS: 1e6}}}

	got, ok := tr.Download(0, 125_000)
	if !ok {
		t.Fatal("Download reported it could not complete across an outage that ends")
	}
	closeTo(t, got, 6.0, "elapsed")
}

func TestRateAt(t *testing.T) {
	tr := Trace{Samples: []Sample{{At: 0, BPS: 3e6}, {At: 10, BPS: 8e5}, {At: 20, BPS: 2e6}}}

	for _, c := range []struct {
		at   float64
		want float64
	}{
		{-1, 3e6}, // before the trace starts, the first sample applies
		{0, 3e6},
		{9.999, 3e6},
		{10, 8e5}, // a sample applies from its own timestamp onwards
		{19.5, 8e5},
		{20, 2e6},
		{1000, 2e6}, // past the end, the last rate holds
	} {
		if got := tr.RateAt(c.at); got != c.want {
			t.Errorf("RateAt(%v) = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestMeanRate(t *testing.T) {
	// 4 Mbps for ten seconds then 1 Mbps for ten: the average over the twenty
	// is 2.5 Mbps. The efficiency check divides by this, so an average taken
	// over samples instead of over time would flatter a trace with one long
	// slow stretch and many short fast ones.
	tr := Trace{Samples: []Sample{{At: 0, BPS: 4e6}, {At: 10, BPS: 1e6}}}
	closeTo(t, tr.MeanRate(0, 20), 2.5e6, "MeanRate")
	closeTo(t, tr.MeanRate(0, 10), 4e6, "MeanRate over the first sample only")
	closeTo(t, tr.MeanRate(5, 15), 2.5e6, "MeanRate straddling the boundary")
	closeTo(t, tr.MeanRate(3, 3), 4e6, "MeanRate over a zero-length window")
}

func TestParse(t *testing.T) {
	in := `# a trace someone recorded on a train
# unit is bits per second
seconds,bps
0,3000000
10,800k
20,2.5M

30,1G
`
	tr, err := Parse(strings.NewReader(in), "train")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if tr.Name != "train" {
		t.Errorf("Name = %q, want %q", tr.Name, "train")
	}
	want := []Sample{{0, 3e6}, {10, 8e5}, {20, 2.5e6}, {30, 1e9}}
	if len(tr.Samples) != len(want) {
		t.Fatalf("got %d samples, want %d: %+v", len(tr.Samples), len(want), tr.Samples)
	}
	for i, w := range want {
		if tr.Samples[i] != w {
			t.Errorf("sample %d = %+v, want %+v", i, tr.Samples[i], w)
		}
	}
}

func TestParse_Rejects(t *testing.T) {
	for name, in := range map[string]string{
		"empty":           "# nothing but a comment\n",
		"one field":       "0\n",
		"bad time":        "later,1000\n",
		"bad rate":        "0,fast\n",
		"negative time":   "-1,1000\n",
		"negative rate":   "0,-1000\n",
		"out of order":    "0,1000\n5,2000\n3,3000\n",
		"duplicate time":  "0,1000\n5,2000\n5,3000\n",
		"unknown suffix":  "0,10Q\n",
		"too many fields": "0,1000,extra\n",
	} {
		t.Run(name, func(t *testing.T) {
			if tr, err := Parse(strings.NewReader(in), "x"); err == nil {
				t.Fatalf("Parse accepted %q: %+v", in, tr)
			}
		})
	}
}

func TestParse_FirstSampleNeedNotStartAtZero(t *testing.T) {
	// A recording that begins at t=12 is still a valid trace; RateAt holds the
	// first rate backwards, so the simulation simply starts inside it.
	tr, err := Parse(strings.NewReader("12,1000000\n20,500000\n"), "x")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := tr.RateAt(0); got != 1e6 {
		t.Errorf("RateAt(0) = %v, want the first rate 1e6", got)
	}
}
