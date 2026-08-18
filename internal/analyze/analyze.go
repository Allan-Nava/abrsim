// Package analyze turns a simulation into findings.
//
// The division of labour is deliberate: sim reports what happened, analyze says
// whether it was a problem and whose. Every check here answers the same
// question in a different place — *what did this ladder cost the viewer on this
// network* — and every one of them speaks on every run, at OK when there is
// nothing wrong. A check that goes quiet on a stream it did not like is a hole
// nobody notices.
package analyze

import (
	"fmt"

	"github.com/Allan-Nava/abrsim/internal/finding"
	"github.com/Allan-Nava/abrsim/internal/manifest"
	"github.com/Allan-Nava/abrsim/internal/sim"
	"github.com/Allan-Nava/abrsim/internal/trace"
)

// Thresholds are where a measurement becomes a finding. They are here, named
// and in one place, because every one of them is a judgement someone is
// entitled to disagree with.
type Thresholds struct {
	StartupWarn float64 // seconds to the first frame
	StartupBad  float64
	StallWarn   float64 // fraction of playback spent frozen
	StallBad    float64
	SwitchWarn  float64 // switches per minute
	SwitchBad   float64
	// GapWaste is the share of playback that has to be spent stuck below a hole
	// before the hole is worth reporting, and GapRatio how wide the hole has to
	// be — a ladder whose neighbours are 1.4× apart has no hole worth the name.
	GapWaste     float64
	GapRatioWarn float64
	GapRatioBad  float64
}

// DefaultThresholds are the ones the CLI uses.
//
// The startup figures come from the industry's own target (two seconds is the
// number every player vendor quotes); the stall figures from the observation
// that a session losing more than one percent of its playback to freezing is
// one viewers abandon. They are defaults, not laws: AB-23 makes them a file.
func DefaultThresholds() Thresholds {
	return Thresholds{
		StartupWarn:  2,
		StartupBad:   4,
		StallWarn:    0.001,
		StallBad:     0.01,
		SwitchWarn:   4,
		SwitchBad:    10,
		GapWaste:     0.15,
		GapRatioWarn: 1.8,
		GapRatioBad:  2.5,
	}
}

// Run analyses one simulation, worst findings first.
func Run(res sim.Result, tr trace.Trace, l manifest.Ladder) []finding.Finding {
	return RunWith(res, tr, l, DefaultThresholds())
}

// RunWith is Run with explicit thresholds.
func RunWith(res sim.Result, tr trace.Trace, l manifest.Ladder, th Thresholds) []finding.Finding {
	fs := []finding.Finding{
		checkRebuffer(res, th),
		checkStartup(res, th),
		checkSwitches(res, th),
		checkEfficiency(res, tr, th),
		checkLadderGap(res, tr, th),
		checkSizes(res),
		checkCoverage(res, tr),
	}
	finding.SortWorstFirst(fs)
	return fs
}

// ---------------------------------------------------------------------------

func checkRebuffer(res sim.Result, th Thresholds) finding.Finding {
	f := finding.Finding{
		Check:  "rebuffer",
		Target: res.Trace,
		Value:  finding.Num(round1(res.StallTime)),
		Unit:   "s",
	}
	if res.Media <= 0 {
		f.Status, f.Message = finding.ERROR, "nothing played, so nothing could freeze"
		return f
	}
	share := res.StallTime / res.Media

	switch {
	case res.Stalls == 0:
		f.Status = finding.OK
		f.Message = fmt.Sprintf("no rebuffering in %s of playback", secs(res.Media))
		return f
	case share >= th.StallBad:
		f.Status = finding.BAD
	case share >= th.StallWarn:
		f.Status = finding.WARN
	default:
		f.Status = finding.OK
	}
	f.Message = fmt.Sprintf("%d stall%s, %s frozen in %s of playback (%.1f%%)",
		res.Stalls, plural(res.Stalls), secs(res.StallTime), secs(res.Media), share*100)
	f.Hint = "the picture is stopped for this long: it is the defect a viewer notices first and forgives least"
	return f
}

func checkStartup(res sim.Result, th Thresholds) finding.Finding {
	f := finding.Finding{
		Check:  "startup",
		Target: "session",
		Value:  finding.Num(round1(res.Startup)),
		Unit:   "s",
	}
	if !res.Started {
		f.Status = finding.ERROR
		f.Message = "playback never started, so there is no time to first frame to report"
		f.Value = nil
		return f
	}

	rung := ""
	if len(res.Requests) > 0 && res.Requests[0].Rung < len(res.Names) {
		rung = fmt.Sprintf(", starting on %s", res.Names[res.Requests[0].Rung])
	}
	switch {
	case res.Startup >= th.StartupBad:
		f.Status = finding.BAD
	case res.Startup >= th.StartupWarn:
		f.Status = finding.WARN
	default:
		f.Status = finding.OK
	}
	f.Message = fmt.Sprintf("%s to the first frame%s", secs(res.Startup), rung)
	if f.Status != finding.OK {
		f.Hint = "the wait before anything appears, and the point most abandonment happens: a lower bottom rung buys it back"
	}
	return f
}

func checkSwitches(res sim.Result, th Thresholds) finding.Finding {
	f := finding.Finding{Check: "switches", Target: "session", Unit: "switches/min"}
	if res.Media <= 0 {
		f.Status, f.Message = finding.ERROR, "nothing played, so nothing could switch"
		return f
	}
	rate := float64(res.Switches) / (res.Media / 60)
	f.Value = finding.Num(round1(rate))

	switch {
	case rate >= th.SwitchBad:
		f.Status = finding.BAD
	case rate >= th.SwitchWarn:
		f.Status = finding.WARN
	default:
		f.Status = finding.OK
	}
	f.Message = fmt.Sprintf("%d rung change%s in %s (%.1f/min)", res.Switches, plural(res.Switches), secs(res.Media), rate)
	if f.Status != finding.OK {
		f.Hint = "visible as the picture repeatedly softening and sharpening; often the throughput estimate chasing noise rather than the network really moving"
	}
	return f
}

// checkEfficiency reports what the session spent against what the line offered.
//
// It never exceeds OK, and that is a deliberate limitation rather than an
// oversight. Two attempts at a severity here both fired on healthy reference
// streams: judging the whole session flags every stream watched on a line far
// above its ladder, and restricting the judgement to playback below the top
// rung flags the ramp-up instead, because a buffer-based algorithm is *always*
// below the top while it climbs and the climb always looks wasteful. Telling a
// slow ramp from a real waste needs a definition of steady state that this
// version does not have (AB-34).
//
// So the number is reported and the judgement is not made. A measurement with
// an honest "no opinion" is worth more than a severity that cries wolf: the
// first false positive is the last time anybody reads the output.
func checkEfficiency(res sim.Result, tr trace.Trace, _ Thresholds) finding.Finding {
	f := finding.Finding{Check: "efficiency", Target: "session", Unit: "%", Status: finding.OK}
	avail := tr.MeanRate(0, res.Wall)
	// The bytes that really crossed the wire, not the rungs' declared
	// bitrates: BANDWIDTH is an upper bound a packager is entitled to
	// over-declare, and a ratio built on it read 129% on Apple's advanced
	// example while looking perfectly fine.
	played := res.DeliveredBitrate()
	if avail <= 0 || res.Media <= 0 {
		f.Status, f.Message = finding.ERROR, "no bandwidth to compare the delivered bitrate against"
		return f
	}

	share := played / avail
	f.Value = finding.Num(round1(share * 100))

	// Time spent below the top rung, which is the context that makes the
	// percentage readable: a low figure with the player at the ceiling
	// throughout means the line was bigger than the ladder, and a low figure
	// with most of the session below it is worth going and looking at.
	var below float64
	top := len(res.Bitrates) - 1
	for _, q := range res.Requests {
		if q.Rung < top {
			below += q.Duration
		}
	}

	switch {
	case share > 1:
		// Real, and worth explaining rather than printing bare: the player is
		// spending buffer it built while the line was faster.
		f.Message = fmt.Sprintf("%s delivered against %s averaged over the session (%.0f%%): the player is spending buffer built while the line was faster",
			mbps(played), mbps(avail), share*100)
	case below <= 0:
		f.Message = fmt.Sprintf("%s delivered against %s available (%.0f%%), on the top rung throughout",
			mbps(played), mbps(avail), share*100)
	default:
		f.Message = fmt.Sprintf("%s delivered against %s available (%.0f%%), %s of %s below the top rung",
			mbps(played), mbps(avail), share*100, secs(below), secs(res.Media))
	}
	return f
}

func checkSizes(res sim.Result) finding.Finding {
	f := finding.Finding{Check: "sizes", Target: "session", Status: finding.OK}
	if res.Estimated {
		f.Message = "segment sizes are declared, not measured: derived from the manifest's bitrate rather than downloaded"
		f.Hint = "run with --sizes measured to replace the estimate with real Content-Lengths; a variable-bitrate encode can differ from its declaration by a lot on any single segment"
		return f
	}
	f.Message = "every segment size was measured"
	return f
}

// checkCoverage says how much of the run the inputs actually covered.
//
// Three ways they may not have, and none of them is a defect in the stream:
// the network died and nothing could finish; the run was cut short as asked;
// or the session outlived the recording and the rest of the network was
// extrapolated from its last sample. The third is the quiet one — a
// twenty-minute session against a five-minute trace has three quarters of its
// bandwidth invented, and every figure above is that much guesswork.
func checkCoverage(res sim.Result, tr trace.Trace) finding.Finding {
	f := finding.Finding{Check: "coverage", Target: res.Trace}

	if res.Incomplete {
		f.Status = finding.ERROR
		f.Message = fmt.Sprintf("the run stopped after %d of the ladder's segments: the trace ends on zero bandwidth, so no finite time completes the next download", len(res.Requests))
		f.Hint = "everything past that point is unknown rather than fine — extend the trace or shorten the run with --play"
		return f
	}

	// A single-sample trace is a declared constant — "assume 5 Mbps" — not a
	// recording that stopped, so there is nothing to have run out of.
	end := 0.0
	if len(tr.Samples) > 1 {
		end = tr.Samples[len(tr.Samples)-1].At
	}
	if end > 0 && res.Wall > end {
		share := (res.Wall - end) / res.Wall
		f.Value = finding.Num(round1(share * 100))
		f.Unit = "%"
		f.Message = fmt.Sprintf("the trace ends at %s and the session ran %s: %.0f%% of it was extrapolated from the last sample",
			secs(end), secs(res.Wall), share*100)
		if share >= 0.25 {
			f.Status = finding.WARN
			f.Hint = "hold the run to what was really measured with --play, or record a longer trace: past the end the network is an assumption, not a reading"
			return f
		}
		f.Status = finding.OK
		return f
	}

	if res.Truncated {
		f.Status = finding.OK
		f.Message = fmt.Sprintf("stopped at %s of playback as asked; the totals are not the whole asset's", secs(res.Media))
		return f
	}

	f.Status = finding.OK
	f.Message = fmt.Sprintf("%s of playback, entirely within the trace", secs(res.Media))
	return f
}

// ---------------------------------------------------------------------------

// checkLadderGap is the check the tool exists for.
//
// For every moment of playback it asks what the network could have sustained
// and what the player was actually given. Where the player sat on a rung while
// the line could carry appreciably more, and the *next rung up* was out of
// reach anyway, the difference is not the network's fault and not the
// algorithm's: there is no rung between the two, and the seconds spent below
// the hole are what that costs.
//
// The two exclusions matter more than the arithmetic. A player capped by the
// network is not evidence of a missing rung, and a player already on the top
// rung has run out of ladder rather than fallen into a hole — reporting either
// would send somebody to add a rung that nothing would ever choose.
// gapHeadroom is how much more than the played rung the line has to carry
// before the spare bandwidth is worth a rung at all. Below it the network is
// the limit, and a finding would send somebody to encode a rung nothing would
// ever choose.
const gapHeadroom = 1.3

func checkLadderGap(res sim.Result, tr trace.Trace, th Thresholds) finding.Finding {
	f := finding.Finding{Check: "ladder-gap", Target: "ladder", Unit: "s"}
	top := len(res.Bitrates) - 1
	if top < 1 || res.Media <= 0 {
		f.Status = finding.OK
		f.Message = "a one-rung ladder has nothing to fall between"
		return f
	}

	// Per rung: seconds spent there while the line could have carried the rung
	// above, and the bandwidth that was going spare.
	wasted := make([]float64, len(res.Bitrates))
	spare := make([]float64, len(res.Bitrates))
	for _, q := range res.Requests {
		if q.Rung >= top {
			continue // out of ladder, not in a hole
		}
		// What the line sustained while this segment was in flight. Measured
		// from the trace rather than from the player's estimate: the question
		// is what the network could have delivered, not what the algorithm
		// believed about it.
		avail := tr.MeanRate(q.Start, q.Start+q.Elapsed)
		here := float64(res.Bitrates[q.Rung])
		next := float64(res.Bitrates[q.Rung+1])
		switch {
		case avail < here*gapHeadroom:
			// The line has no meaningful room above the rung being played. The
			// network is the limit and no rung would have changed anything.
			continue
		case avail >= next:
			// The rung above would have fitted and was not taken. That is the
			// algorithm's business, or the safety margin's — the ladder had
			// somewhere to go.
			continue
		}
		wasted[q.Rung] += q.Duration
		spare[q.Rung] += avail * q.Duration
	}

	worst, worstAt := 0.0, -1
	for i, w := range wasted {
		if w > worst {
			worst, worstAt = w, i
		}
	}
	if worstAt < 0 {
		f.Status = finding.OK
		f.Value = finding.Num(0)
		f.Message = "no time spent below a rung the network could have carried"
		return f
	}

	lo, hi := res.Bitrates[worstAt], res.Bitrates[worstAt+1]
	ratio := float64(hi) / float64(lo)
	share := worst / res.Media
	avgSpare := spare[worstAt] / worst

	f.Target = fmt.Sprintf("%s..%s", res.Names[worstAt], res.Names[worstAt+1])
	f.Value = finding.Num(round1(worst))
	switch {
	case share < th.GapWaste || ratio < th.GapRatioWarn:
		f.Status = finding.OK
		f.Message = fmt.Sprintf("%s spent on %s with room for %s (%.1f× apart) — not enough to be worth a rung",
			secs(worst), res.Names[worstAt], res.Names[worstAt+1], ratio)
	case ratio >= th.GapRatioBad:
		f.Status = finding.BAD
		f.Message = fmt.Sprintf("no rung between %s and %s (%.1f× apart): %s of playback stuck on %s while the line carried %s",
			res.Names[worstAt], res.Names[worstAt+1], ratio, secs(worst), res.Names[worstAt], mbps(avgSpare))
		f.Hint = fmt.Sprintf("a rung near %s would have been chosen for %.0f%% of this session; the viewer watched %s of it at %s instead",
			mbps(avgSpare*0.9), share*100, secs(worst), res.Names[worstAt])
	default:
		f.Status = finding.WARN
		f.Message = fmt.Sprintf("the step from %s to %s (%.1f×) left %s of playback on %s while the line carried %s",
			res.Names[worstAt], res.Names[worstAt+1], ratio, secs(worst), res.Names[worstAt], mbps(avgSpare))
		f.Hint = "a narrower step here would let the player spend what the connection actually has"
	}
	return f
}

// ---------------------------------------------------------------------------

func secs(v float64) string {
	if v < 10 {
		return fmt.Sprintf("%.1fs", v)
	}
	return fmt.Sprintf("%.0fs", v)
}

func mbps(bps float64) string {
	if bps < 1e6 {
		return fmt.Sprintf("%.0f kbps", bps/1e3)
	}
	return fmt.Sprintf("%.1f Mbps", bps/1e6)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }
