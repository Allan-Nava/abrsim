// Package sim is the playback model: the loop every number abrsim prints comes
// out of.
//
// The model is the one used throughout the ABR literature, kept deliberately
// plain so its numbers are comparable with published results rather than with
// our own cleverness:
//
//   - The player requests one segment at a time and waits for it whole.
//   - Playback begins once the buffer first reaches the startup threshold, and
//     the wait until then is startup, never rebuffering.
//   - While a download is in flight the buffer drains in real time. If it
//     empties before the segment arrives, the difference is a stall: the
//     picture is frozen for exactly that long.
//   - A player whose buffer is full stops requesting and waits, which moves it
//     along the trace. Without that a VOD asset downloads in seconds and the
//     simulation reports a network nobody has.
//
// What it does not model is listed in BACKLOG.md rather than hidden here:
// request latency (AB-14), audio sharing the connection (AB-15), a live edge
// (AB-17). Each of those makes the model kinder to the stream than reality is,
// so every figure below is a floor on the trouble, not a ceiling.
package sim

import (
	"fmt"

	"github.com/Allan-Nava/abrsim/internal/abr"
	"github.com/Allan-Nava/abrsim/internal/manifest"
	"github.com/Allan-Nava/abrsim/internal/trace"
)

// Options are the player's policy, the part an operator can actually change.
type Options struct {
	StartupBuffer float64 // seconds of media required before playback begins
	BufferCap     float64 // seconds the player will hold before it stops requesting
	MaxSeconds    float64 // stop after this much media; 0 means the whole asset
}

// Defaults fills in anything left at zero. The figures are the ones a default
// hls.js/dash.js install behaves like, not ones chosen to make a ladder look
// good.
func (o Options) Defaults() Options {
	if o.StartupBuffer <= 0 {
		o.StartupBuffer = 2
	}
	if o.BufferCap <= 0 {
		o.BufferCap = 30
	}
	return o
}

// Request is one segment fetch, with everything needed to explain it later.
// It is a comparable struct on purpose: two runs of the same inputs must
// produce identical requests, and the test says so field by field.
type Request struct {
	Index        int     `json:"index"`
	Rung         int     `json:"rung"`
	Bytes        int64   `json:"bytes"`
	Measured     bool    `json:"measured"`
	Start        float64 `json:"start"`      // wall clock when the request went out
	Elapsed      float64 `json:"elapsed"`    // seconds it took
	Throughput   float64 `json:"throughput"` // bits per second, as the player measured it
	BufferBefore float64 `json:"buffer_before"`
	BufferAfter  float64 `json:"buffer_after"`
	Stall        float64 `json:"stall"`    // seconds of frozen picture this download caused
	Duration     float64 `json:"duration"` // seconds of media the segment carries
	// Wait is how long the player sat on a full buffer before issuing this
	// request. It is not a defect — it is the player behaving — but it is what
	// keeps the wall clock honest on an asset that downloads faster than it
	// plays.
	Wait float64 `json:"wait"`
}

// Result is one simulation.
type Result struct {
	Algorithm string    `json:"algorithm"`
	Trace     string    `json:"trace"`
	Bitrates  []int64   `json:"bitrates"`
	Names     []string  `json:"rendition_names"`
	Requests  []Request `json:"requests"`

	Started   bool    `json:"started"`    // playback ever began
	Startup   float64 `json:"startup"`    // seconds to the first frame
	Stalls    int     `json:"stalls"`     // how many times the picture froze
	StallTime float64 `json:"stall_time"` // seconds it was frozen for
	Switches  int     `json:"switches"`
	Media     float64 `json:"media"` // seconds of content fetched
	Wall      float64 `json:"wall"`  // seconds of clock the session took
	Bytes     int64   `json:"bytes"`

	// Estimated is true when any segment size was derived from the declared
	// bitrate rather than measured. It travels with every result because a
	// simulation reported as a measurement is the one way this tool can lie.
	Estimated bool `json:"estimated"`
	// Truncated: the run stopped at MaxSeconds, so the totals are not the
	// whole asset's.
	Truncated bool `json:"truncated"`
	// Incomplete: a download could never finish because the trace ended on
	// zero bandwidth. Everything after that point is unknown, not fine.
	Incomplete bool `json:"incomplete"`
}

// DeliveredBitrate is the bytes that actually crossed the wire, per second of
// media played.
//
// This, not MeanRungBitrate, is what anything comparing against real bandwidth
// has to use: a rung's BANDWIDTH is a declared upper bound, and a packager
// entitled to over-declare it can push a ratio built on it past 100%.
func (r Result) DeliveredBitrate() float64 {
	if r.Media <= 0 {
		return 0
	}
	return float64(r.Bytes) * 8 / r.Media
}

// MeanRungBitrate is the time-weighted average of the rungs' *declared*
// bitrates, which is what an operator recognises as "the ladder step the viewer
// was on". It is a label, not a measurement — see DeliveredBitrate.
func (r Result) MeanRungBitrate() float64 {
	if r.Media <= 0 {
		return 0
	}
	var bits float64
	for _, q := range r.Requests {
		bits += float64(r.Bitrates[q.Rung]) * q.Duration
	}
	return bits / r.Media
}

// Run plays the ladder over the trace with the given algorithm.
func Run(l manifest.Ladder, tr trace.Trace, alg abr.Algorithm, opts Options) (Result, error) {
	opts = opts.Defaults()
	if len(l.Renditions) == 0 {
		return Result{}, fmt.Errorf("the ladder has no rungs to choose between")
	}

	// Every rung has to cover the same content for a switch to be possible at
	// all; where they disagree, the shortest is as far as the simulation can
	// honestly go.
	n := len(l.Renditions[0].Segments)
	for _, r := range l.Renditions[1:] {
		if len(r.Segments) < n {
			n = len(r.Segments)
		}
	}
	if n == 0 {
		return Result{}, fmt.Errorf("the ladder has no segments to fetch")
	}

	res := Result{Algorithm: alg.Name(), Trace: tr.Name}
	for _, r := range l.Renditions {
		res.Bitrates = append(res.Bitrates, r.Bandwidth)
		res.Names = append(res.Names, r.Name)
	}

	alg.Reset()
	var now, buffer float64
	started := false
	last := -1

	for i := 0; i < n; i++ {
		dur := l.Renditions[0].Segments[i].Duration

		// A player whose buffer is full does not issue the request: it waits,
		// and the trace moves on beneath it. Gating here rather than idling
		// after the fetch is what real players do, and it is why the buffer
		// peaks at the cap plus one segment rather than exactly at the cap —
		// you cannot fetch a four-second segment without overshooting a
		// threshold by up to four seconds.
		var wait float64
		if started && buffer > opts.BufferCap {
			wait = buffer - opts.BufferCap
			now += wait
			buffer = opts.BufferCap
		}

		rung := alg.Pick(res.Bitrates, abr.State{
			Buffer:     buffer,
			BufferCap:  opts.BufferCap,
			SegmentDur: dur,
			Index:      i,
			Last:       last,
		})
		if rung < 0 {
			rung = 0
		}
		if rung >= len(res.Bitrates) {
			rung = len(res.Bitrates) - 1
		}

		seg := l.Renditions[rung].Segments[i]
		bytes := seg.Bytes
		// The initialisation segment is fetched before the first media segment
		// and is therefore part of the time to first frame.
		if i == 0 {
			bytes += l.Renditions[rung].InitBytes
		}

		elapsed, ok := tr.Download(now, bytes)
		if !ok {
			res.Incomplete = true
			break
		}

		q := Request{
			Index:        i,
			Rung:         rung,
			Bytes:        bytes,
			Measured:     seg.Measured,
			Start:        now,
			Elapsed:      elapsed,
			Duration:     dur,
			Wait:         wait,
			BufferBefore: buffer,
		}
		if elapsed > 0 {
			q.Throughput = float64(bytes) * 8 / elapsed
		}
		if !seg.Measured {
			res.Estimated = true
		}

		// Playback drains the buffer while the download is in flight — but only
		// once it has started. The wait before the first frame is startup, and
		// counting it as a stall would double-report the number an operator is
		// most likely to act on.
		if started {
			if buffer >= elapsed {
				buffer -= elapsed
			} else {
				q.Stall = elapsed - buffer
				buffer = 0
				res.Stalls++
				res.StallTime += q.Stall
			}
		}

		now += elapsed
		buffer += dur
		res.Media += dur
		res.Bytes += bytes

		if !started && buffer >= opts.StartupBuffer {
			started = true
			res.Startup = now
		}
		if last >= 0 && rung != last {
			res.Switches++
		}
		last = rung

		q.BufferAfter = buffer
		res.Requests = append(res.Requests, q)
		alg.Observe(q.Throughput)

		if opts.MaxSeconds > 0 && res.Media >= opts.MaxSeconds {
			res.Truncated = i < n-1
			break
		}
	}

	// A run whose first frame never arrived has no startup figure, and zero
	// would read as instant — which is why Started is a field rather than
	// something a reader infers from Startup being small.
	res.Started = started
	if !started {
		res.Startup = 0
		res.Incomplete = true
	}
	res.Wall = now
	return res, nil
}
