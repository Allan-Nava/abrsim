// Package trace is the network the simulation runs over: a piecewise-constant
// bandwidth function of time, and the arithmetic that turns it into transfer
// times.
//
// Time is float64 seconds throughout abrsim rather than time.Duration. The
// simulator adds thousands of intervals together and a nanosecond rounding on
// each one accumulates into a drift the checks would then report as a defect;
// float64 seconds is also the unit every published ABR simulation uses, which
// is what makes our numbers comparable with theirs.
package trace

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// Sample is a bandwidth reading: BPS applies from At until the next sample's
// At, or forever if there is no next sample.
type Sample struct {
	At  float64 `json:"at"`  // seconds
	BPS float64 `json:"bps"` // bits per second
}

// Trace is a bandwidth measurement over time, in ascending order of At.
//
// Before the first sample the first rate applies and after the last sample the
// last rate holds: a trace is a recording, not a promise about the times it did
// not cover, and holding the edge rates is the only extrapolation that does not
// invent an event.
type Trace struct {
	Name    string   `json:"name"`
	Samples []Sample `json:"samples"`
}

// RateAt is the bandwidth in bits per second at sec.
func (t Trace) RateAt(sec float64) float64 {
	if len(t.Samples) == 0 {
		return 0
	}
	i := t.index(sec)
	return t.Samples[i].BPS
}

// index is the sample applying at sec: the last one whose At is <= sec, or the
// first sample when sec precedes the trace.
func (t Trace) index(sec float64) int {
	lo, hi := 0, len(t.Samples)-1
	if sec < t.Samples[0].At {
		return 0
	}
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if t.Samples[mid].At <= sec {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// Span is the time from the first sample to the last, which is as much of the
// recording as actually carries information.
func (t Trace) Span() float64 {
	if len(t.Samples) < 2 {
		return 0
	}
	return t.Samples[len(t.Samples)-1].At - t.Samples[0].At
}

// Download reports how many seconds it takes to transfer bytes starting at
// start, walking the trace one sample at a time.
//
// The bool is false when no finite time completes the transfer — the trace ends
// on zero bandwidth. Returning the time up to the outage instead would report a
// permanent stall as a completed request, which is the one error that would
// make every downstream check optimistic.
func (t Trace) Download(start float64, bytes int64) (float64, bool) {
	if bytes <= 0 {
		return 0, true
	}
	if len(t.Samples) == 0 {
		return 0, false
	}

	remaining := float64(bytes) * 8 // bits
	now := start
	for i := t.index(now); ; i++ {
		rate := t.Samples[i].BPS

		// The last sample runs forever, so it either finishes the transfer or
		// nothing does.
		if i == len(t.Samples)-1 {
			if rate <= 0 {
				return 0, false
			}
			return now + remaining/rate - start, true
		}

		// How long this sample lasts from where we are. A sample entirely in
		// the past contributes nothing, which is what makes starting mid-sample
		// work without a special case.
		until := t.Samples[i+1].At
		window := until - now
		if window <= 0 {
			continue
		}
		if delivered := rate * window; delivered >= remaining {
			return now + remaining/rate - start, true
		} else {
			remaining -= delivered
			now = until
		}
	}
}

// MeanRate is the time-weighted average bandwidth over [from, to).
//
// Time-weighted, not sample-weighted: a trace with one long slow stretch and
// many short fast ones has a much lower average than its samples suggest, and
// the efficiency check divides by this number.
func (t Trace) MeanRate(from, to float64) float64 {
	if len(t.Samples) == 0 {
		return 0
	}
	if to <= from {
		return t.RateAt(from)
	}

	var bits float64
	now := from
	for i := t.index(now); now < to; i++ {
		end := to
		if i < len(t.Samples)-1 && t.Samples[i+1].At < end {
			end = t.Samples[i+1].At
		}
		if end > now {
			bits += t.Samples[i].BPS * (end - now)
			now = end
		} else if i >= len(t.Samples)-1 {
			break
		}
	}
	return bits / (to - from)
}

// Parse reads a trace from CSV: one `seconds,bits_per_second` row per reading.
//
// Blank lines and `#` comments are ignored, as is a single non-numeric header
// row. A rate may carry a `k`, `M` or `G` suffix (decimal, not binary — a
// bitrate has always been counted in thousands).
func Parse(r io.Reader, name string) (Trace, error) {
	tr := Trace{Name: name}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	line, rows := 0, 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		rows++
		fields := strings.Split(text, ",")
		if len(fields) != 2 {
			return Trace{}, fmt.Errorf("%s:%d: want two fields `seconds,bits_per_second`, got %d", name, line, len(fields))
		}

		at, errAt := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
		bps, errBPS := parseRate(strings.TrimSpace(fields[1]))
		if errAt != nil || errBPS != nil {
			// A header row is tolerable only as the first data row and only
			// when neither field is a number. One good field and one bad is a
			// corrupt reading, not a label, and skipping it would drop a
			// sample the recording says is there.
			if rows == 1 && errAt != nil && errBPS != nil {
				continue
			}
			if errAt != nil {
				return Trace{}, fmt.Errorf("%s:%d: %q is not a time in seconds", name, line, fields[0])
			}
			return Trace{}, fmt.Errorf("%s:%d: %q is not a bitrate", name, line, fields[1])
		}

		switch {
		case at < 0 || math.IsNaN(at) || math.IsInf(at, 0):
			return Trace{}, fmt.Errorf("%s:%d: time %v is not a point on the trace", name, line, at)
		case bps < 0 || math.IsNaN(bps) || math.IsInf(bps, 0):
			return Trace{}, fmt.Errorf("%s:%d: bitrate %v is not a rate", name, line, bps)
		case len(tr.Samples) > 0 && at <= tr.Samples[len(tr.Samples)-1].At:
			return Trace{}, fmt.Errorf("%s:%d: time %v does not follow %v — samples must ascend",
				name, line, at, tr.Samples[len(tr.Samples)-1].At)
		}
		tr.Samples = append(tr.Samples, Sample{At: at, BPS: bps})
	}
	if err := sc.Err(); err != nil {
		return Trace{}, fmt.Errorf("%s: %w", name, err)
	}
	if len(tr.Samples) == 0 {
		return Trace{}, fmt.Errorf("%s: no samples", name)
	}
	return tr, nil
}

// parseRate reads a bitrate, with an optional decimal k/M/G suffix.
func parseRate(s string) (float64, error) {
	mult := 1.0
	if s != "" {
		switch s[len(s)-1] {
		case 'k', 'K':
			mult, s = 1e3, s[:len(s)-1]
		case 'm', 'M':
			mult, s = 1e6, s[:len(s)-1]
		case 'g', 'G':
			mult, s = 1e9, s[:len(s)-1]
		}
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	return v * mult, nil
}
