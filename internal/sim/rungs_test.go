package sim

import "testing"

// AB-38 and AB-39: what each rung actually served, and what it cost to ship.
// Both come out of the timeline the simulator already emits, so they are
// measurements rather than models — and both are answers `ladder-gap` cannot give:
// it names a hole, this names the rungs pulling their weight.

func TestRungUse_CountsTheSecondsAndBytesPerRung(t *testing.T) {
	res := Result{
		Bitrates: []int64{1_000_000, 4_000_000},
		Names:    []string{"1000k", "4000k"},
		Media:    30,
		Requests: []Request{
			{Rung: 0, Duration: 10, Bytes: 1_250_000},
			{Rung: 1, Duration: 10, Bytes: 5_000_000},
			{Rung: 1, Duration: 10, Bytes: 5_000_000},
		},
	}
	use := res.RungUse()
	if len(use) != 2 {
		t.Fatalf("%d rungs, want 2", len(use))
	}
	if use[0].Name != "1000k" || use[0].Bitrate != 1_000_000 {
		t.Errorf("rung 0 = %+v", use[0])
	}
	if use[0].Seconds != 10 || use[1].Seconds != 20 {
		t.Errorf("seconds = %v and %v, want 10 and 20", use[0].Seconds, use[1].Seconds)
	}
	if use[0].Share != 10.0/30 || use[1].Share != 20.0/30 {
		t.Errorf("shares = %v and %v", use[0].Share, use[1].Share)
	}
	if use[0].Bytes != 1_250_000 || use[1].Bytes != 10_000_000 {
		t.Errorf("bytes = %v and %v", use[0].Bytes, use[1].Bytes)
	}
	if use[0].Segments != 1 || use[1].Segments != 2 {
		t.Errorf("segments = %d and %d", use[0].Segments, use[1].Segments)
	}
}

func TestRungUse_ARungNothingChoseIsPresentAndZero(t *testing.T) {
	// The rung nobody selected is the whole point of the measurement: leaving it
	// out of the table would hide the encoding, storage and egress it costs for
	// no viewer's benefit.
	res := Result{
		Bitrates: []int64{500_000, 1_000_000, 4_000_000},
		Names:    []string{"500k", "1000k", "4000k"},
		Media:    20,
		Requests: []Request{
			{Rung: 0, Duration: 10, Bytes: 625_000},
			{Rung: 2, Duration: 10, Bytes: 5_000_000},
		},
	}
	use := res.RungUse()
	if len(use) != 3 {
		t.Fatalf("%d rungs, want 3 — every rung on the ladder, chosen or not", len(use))
	}
	if use[1].Seconds != 0 || use[1].Bytes != 0 || use[1].Segments != 0 {
		t.Errorf("the unchosen rung reads %+v, want zeroes", use[1])
	}
	if use[1].Name != "1000k" {
		t.Errorf("the unchosen rung lost its name: %+v", use[1])
	}
}

func TestRungUse_NoTimelineIsNoAttribution(t *testing.T) {
	res := Result{Bitrates: []int64{1_000_000}, Names: []string{"1000k"}}
	use := res.RungUse()
	if len(use) != 1 || use[0].Seconds != 0 || use[0].Share != 0 {
		t.Errorf("RungUse on an empty run = %+v", use)
	}
}

func TestBytesPerViewerHour_IsTheEgressAnHourOfWatchingCosts(t *testing.T) {
	// 5 MB for 10 seconds of media is 1.8 GB an hour, and that is the number a
	// delivery bill is made of.
	res := Result{
		Bitrates: []int64{4_000_000},
		Names:    []string{"4000k"},
		Media:    10,
		Bytes:    5_000_000,
		Requests: []Request{{Rung: 0, Duration: 10, Bytes: 5_000_000}},
	}
	got, ok := res.BytesPerViewerHour()
	if !ok {
		t.Fatal("no figure for a run that fetched 10s of media")
	}
	if want := 5_000_000 * 360.0; got != want {
		t.Errorf("BytesPerViewerHour = %.0f, want %.0f", got, want)
	}
	if _, ok := (Result{}).BytesPerViewerHour(); ok {
		t.Error("a run with no media reported an egress rate — nothing was watched, so there is nothing per hour")
	}
}
