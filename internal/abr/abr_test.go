package abr

import "testing"

// A ladder with a deliberate hole between 900k and 2400k: the gap the whole
// tool exists to find, and the one place where the three algorithms disagree.
var ladder = []int64{400_000, 900_000, 2_400_000, 4_000_000, 6_000_000}

func TestAll_ReturnRungsThatExist(t *testing.T) {
	for _, name := range Names() {
		a, ok := New(name)
		if !ok {
			t.Fatalf("%s: listed by Names() but New says it does not exist", name)
		}
		if a.Name() != name {
			t.Errorf("%s: Name() = %q", name, a.Name())
		}
		for _, s := range []State{
			{},
			{Buffer: 0, BufferCap: 30, SegmentDur: 4, Last: -1},
			{Buffer: 30, BufferCap: 30, SegmentDur: 4, Last: 2},
			{Buffer: 12.5, BufferCap: 30, SegmentDur: 4, Last: 4},
		} {
			if got := a.Pick(ladder, s); got < 0 || got >= len(ladder) {
				t.Fatalf("%s: Pick returned rung %d, outside 0..%d for %+v", name, got, len(ladder)-1, s)
			}
			if got := a.Pick(ladder[:1], s); got != 0 {
				t.Errorf("%s: Pick returned %d on a one-rung ladder", name, got)
			}
		}
	}
}

func TestThroughput_StartsAtTheBottom(t *testing.T) {
	// Before anything has been downloaded there is no estimate, and guessing
	// high costs a viewer the one thing they notice most: the wait before the
	// first frame.
	a := &Throughput{}
	if got := a.Pick(ladder, State{Last: -1, BufferCap: 30, SegmentDur: 4}); got != 0 {
		t.Errorf("first pick = rung %d, want the lowest", got)
	}
}

func TestThroughput_PicksTheHighestRungUnderTheSafetyMargin(t *testing.T) {
	a := &Throughput{Safety: 0.9, Alpha: 1} // alpha 1: the estimate is the last sample
	a.Observe(3_000_000)

	// 3 Mbps × 0.9 = 2.7 Mbps: the 2400k rung fits, the 4000k rung does not.
	if got := a.Pick(ladder, State{Last: 0, BufferCap: 30, SegmentDur: 4}); got != 2 {
		t.Errorf("pick = rung %d (%d bps), want rung 2 (2400k)", got, ladder[got])
	}
}

func TestThroughput_NeverPicksNothing(t *testing.T) {
	// A network below the bottom rung still has to be given the bottom rung:
	// there is nothing else to play, and a player that requests no segment at
	// all is not a player.
	a := &Throughput{Safety: 0.9, Alpha: 1}
	a.Observe(50_000)
	if got := a.Pick(ladder, State{Last: 3, BufferCap: 30, SegmentDur: 4}); got != 0 {
		t.Errorf("pick = rung %d, want the lowest", got)
	}
}

func TestThroughput_EWMAIsSmoothedNotInstant(t *testing.T) {
	// The reason this algorithm oscillates less than "pick from the last
	// sample" is the smoothing. Without it a single fast segment would send
	// the player to the top rung and straight back down.
	a := &Throughput{Safety: 1, Alpha: 0.25}
	a.Observe(1_000_000)
	a.Observe(9_000_000)

	// 1M then a quarter of the way to 9M is 3M, not 9M.
	if got := a.Estimate(); got < 2.9e6 || got > 3.1e6 {
		t.Errorf("estimate after a spike = %.0f, want ~3e6 — the spike was not smoothed", got)
	}
}

func TestBufferBased_IgnoresThroughputEntirely(t *testing.T) {
	// This is the defining property, and the reason the algorithm is in the
	// tool at all: when it and the throughput rule disagree about a stall, the
	// difference is the estimator, not the network.
	slow, fast := &BufferBased{}, &BufferBased{}
	slow.Observe(100_000)
	fast.Observe(50_000_000)

	s := State{Buffer: 15, BufferCap: 30, SegmentDur: 4, Last: 2}
	if a, b := slow.Pick(ladder, s), fast.Pick(ladder, s); a != b {
		t.Fatalf("a throughput observation changed the pick: %d vs %d", a, b)
	}
}

func TestBufferBased_EmptyBufferTakesTheBottomAndFullTakesTheTop(t *testing.T) {
	a := &BufferBased{Reservoir: 5, Cushion: 20}

	if got := a.Pick(ladder, State{Buffer: 0, BufferCap: 30, SegmentDur: 4, Last: -1}); got != 0 {
		t.Errorf("empty buffer picked rung %d, want the lowest", got)
	}
	if got := a.Pick(ladder, State{Buffer: 30, BufferCap: 30, SegmentDur: 4, Last: 0}); got != len(ladder)-1 {
		t.Errorf("full buffer picked rung %d, want the highest", got)
	}
}

func TestBufferBased_IsMonotonicInBuffer(t *testing.T) {
	a := &BufferBased{Reservoir: 5, Cushion: 20}
	prev := -1
	for b := 0.0; b <= 40; b += 0.25 {
		got := a.Pick(ladder, State{Buffer: b, BufferCap: 40, SegmentDur: 4, Last: 2})
		if got < prev {
			t.Fatalf("buffer %.2fs picked rung %d after %d at a smaller buffer", b, got, prev)
		}
		prev = got
	}
}

func TestBOLA_IsMonotonicInBuffer(t *testing.T) {
	// BOLA's guarantee, and the first assertion worth making about it: the
	// chosen rung is non-decreasing in buffer level, because each rung's score
	// falls with the buffer at a rate 1/S_m and the higher rungs therefore gain
	// as it fills.
	a := NewBOLA()
	prev := -1
	for b := 0.0; b <= 60; b += 0.25 {
		got := a.Pick(ladder, State{Buffer: b, BufferCap: 60, SegmentDur: 4, Last: 2})
		if got < prev {
			t.Fatalf("buffer %.2fs picked rung %d after %d at a smaller buffer", b, got, prev)
		}
		prev = got
	}
}

func TestBOLA_FullBufferTakesTheTop(t *testing.T) {
	// The second guarantee: gp and Vp are chosen so that a buffer at the
	// player's target is enough to justify the highest rung.
	for _, cap := range []float64{20, 30, 40, 60} {
		a := NewBOLA()
		if got := a.Pick(ladder, State{Buffer: cap, BufferCap: cap, SegmentDur: 4, Last: 4}); got != len(ladder)-1 {
			t.Errorf("a full %.0fs buffer picked rung %d, want the highest", cap, got)
		}
	}
}

func TestBOLA_ClimbsOffTheBottomOnATwoRungLadder(t *testing.T) {
	// The case that caught the first implementation. Deriving gp and Vp from
	// "empty buffer picks the lowest rung, full buffer picks the highest"
	// pins the crossover at exactly the buffer cap when there are only two
	// rungs — and the arithmetic that finds it suffers a catastrophic
	// cancellation, so in practice the player never left the bottom rung at
	// all. A two-rung ladder is not exotic here: it is what a stream with a
	// hole in it looks like.
	two := []int64{800_000, 5_000_000}
	a := NewBOLA()
	if got := a.Pick(two, State{Buffer: 0, BufferCap: 30, SegmentDur: 4, Last: -1}); got != 0 {
		t.Errorf("an empty buffer picked rung %d on a two-rung ladder", got)
	}
	climbed := -1
	for b := 0.0; b <= 30; b += 0.25 {
		if a.Pick(two, State{Buffer: b, BufferCap: 30, SegmentDur: 4, Last: 0}) == 1 {
			climbed = int(b)
			break
		}
	}
	if climbed < 0 {
		t.Fatal("the player never reached the top rung at any buffer level up to the cap")
	}
	if climbed > 15 {
		t.Errorf("the player only reached the top rung at %ds of buffer, half the cap away", climbed)
	}
}

func TestBOLA_NeverStartsAtTheTop(t *testing.T) {
	// BOLA as dash.js ships it does *not* guarantee the lowest rung at an
	// empty buffer — the rung it starts on depends on the ladder and the
	// buffer target, and dash.js compensates with a separate throughput rule
	// at startup that this version does not model (AB-35). What must hold is
	// that an empty buffer never justifies the top rung, or the first request
	// of every session would be the largest segment in the ladder.
	for _, cap := range []float64{20, 30, 40, 60} {
		a := NewBOLA()
		if got := a.Pick(ladder, State{Buffer: 0, BufferCap: cap, SegmentDur: 4, Last: -1}); got == len(ladder)-1 {
			t.Errorf("an empty buffer with a %.0fs cap picked the top rung", cap)
		}
	}
}

func TestNew_UnknownAlgorithm(t *testing.T) {
	if a, ok := New("psychic"); ok {
		t.Fatalf("New invented an algorithm: %T", a)
	}
	if len(Names()) < 3 {
		t.Errorf("only %d algorithms: %v — one algorithm's opinion is not a measurement", len(Names()), Names())
	}
}

func TestReset_ForgetsTheEstimate(t *testing.T) {
	// A sweep runs one algorithm over many traces; carrying an estimate from
	// the end of one trace into the start of the next would make the results
	// depend on the order they were run in.
	a := &Throughput{Safety: 0.9, Alpha: 1}
	a.Observe(50_000_000)
	a.Reset()
	if got := a.Pick(ladder, State{Last: -1, BufferCap: 30, SegmentDur: 4}); got != 0 {
		t.Errorf("after Reset the first pick = rung %d, want the lowest", got)
	}
}

func TestBOLA_WillNotOutrunTheNetworkOnAnEmptyBuffer(t *testing.T) {
	// Buffer level alone is not enough. On a two-rung ladder whose top rung is
	// above the line, BOLA climbs as soon as the buffer passes its crossover,
	// takes a segment it cannot afford, empties the buffer, drops, climbs
	// again — 59 stalls in 60 segments on a perfectly ordinary 3 Mbps cell.
	//
	// dash.js does not ship BOLA on its own for exactly this reason: below its
	// minimum buffer it defers to the throughput estimate. That is the whole
	// safeguard, and without it the algorithm is unusable as a default.
	two := []int64{800_000, 5_000_000}
	a := NewBOLA()
	a.Observe(3_000_000)

	// Four seconds of buffer is past BOLA's crossover and far below its
	// minimum: the throughput estimate has the casting vote and 5 Mbps does
	// not fit in 3.
	if got := a.Pick(two, State{Buffer: 4, BufferCap: 30, SegmentDur: 4, Last: 0}); got != 0 {
		t.Errorf("with 4s buffered on a 3 Mbps line, BOLA took rung %d (%d bps)", got, two[got])
	}
	// Well past the minimum buffer there is enough cushion to risk it, and
	// BOLA's own judgement takes over again.
	if got := a.Pick(two, State{Buffer: 25, BufferCap: 30, SegmentDur: 4, Last: 0}); got != 1 {
		t.Errorf("with 25s buffered BOLA took rung %d, want the top — the safeguard has become a ceiling", got)
	}
}

func TestBOLA_SafeguardDoesNotFireWithoutAnEstimate(t *testing.T) {
	// Before anything has been measured there is nothing to defer to, and
	// treating "no estimate" as "no bandwidth" would pin the first several
	// segments of every session to the bottom rung.
	a := NewBOLA()
	if got := a.Pick(ladder, State{Buffer: 25, BufferCap: 30, SegmentDur: 4, Last: -1}); got == 0 {
		t.Error("with 25s buffered and no throughput estimate, BOLA still sat on the bottom rung")
	}
}
