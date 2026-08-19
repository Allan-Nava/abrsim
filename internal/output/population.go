package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Allan-Nava/abrsim/internal/finding"
	"github.com/Allan-Nava/abrsim/internal/manifest"
	"github.com/Allan-Nava/abrsim/internal/population"
)

// PopulationReport is one ladder over an audience.
type PopulationReport struct {
	Source     string            `json:"source"`
	Ladder     []manifest.Rung   `json:"ladder,omitempty"`
	Population population.Report `json:"population"`
	Options    map[string]any    `json:"options,omitempty"`
}

// TextPopulation renders an audience for a terminal.
//
// Same two rules as a single run — worst first, colour only for people — with one
// addition: every loud check says how much of the audience it happened to. "12 of
// 30 viewers" is the sentence a ladder decision can be argued from; "BAD" on its
// own is not.
func TextPopulation(w io.Writer, r PopulationReport, colour bool) error {
	paint := func(c, s string) string {
		if !colour {
			return s
		}
		return c + s + reset
	}
	p := r.Population

	if _, err := fmt.Fprintf(w, "%d viewers over `%s` with %s — the spread, not one session\n\n",
		p.Viewers, p.Trace, p.Algorithm); err != nil {
		return err
	}

	for _, c := range p.Checks {
		share := "every viewer quiet"
		// The sentence beside a quiet check is one viewer's, so it says whose.
		// Without that it reads as a statement about the whole audience, which
		// is the mistake this whole feature exists to stop making.
		var line string
		message := fmt.Sprintf("viewer %d: %s", c.WorstViewer, c.WorstMessage)
		if c.Loud > 0 {
			share = fmt.Sprintf("%d of %d viewers (%.0f%%)", c.Loud, p.Viewers, 100*float64(c.Loud)/float64(p.Viewers))
			message = c.WorstMessage
		}
		// The severity at the percentiles, which is the sentence AB-37 exists
		// for: "at the 95th percentile of your audience this is BAD", with the
		// p50 beside it so the median hiding the tail is visible rather than
		// implied. The p95 comes first because it is the one an operator is paid
		// to care about.
		at := "p95 " + statusCell(c.AtP95) + " · p50 " + statusCell(c.AtP50)
		if c.AtP99 != "" {
			at = "p99 " + statusCell(c.AtP99) + " · " + at
		}
		line = fmt.Sprintf("%s %-6s %-11s %-30s %-24s %s",
			markFor(c.Worst), paint(colourFor(c.Worst), string(c.Worst)), c.Check, at, share, message)
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
		if c.Loud > 0 {
			// The worst viewer is named because a population is reproducible:
			// somebody can go and look at exactly that session.
			note := fmt.Sprintf("                            ↳ fires from p%.0f up · worst at viewer %d", c.FiresFrom, c.WorstViewer)
			if c.WorstTarget != "" {
				note += " · " + c.WorstTarget
			}
			if c.WorstHint != "" {
				note += " · " + c.WorstHint
			}
			if _, err := fmt.Fprintln(w, paint(dim, note)); err != nil {
				return err
			}
		}
	}

	rows := []struct {
		label string
		stat  population.Stat
		unit  string
	}{
		{"startup", p.Startup, "s"},
		{"frozen", p.Frozen, "s"},
		{"stalls", p.Stalls, ""},
		{"switches/min", p.SwitchRate, ""},
		{"delivered", p.Delivered, "Mbps"},
		{"qoe", p.QoE, "score"},
		{"egress", p.Egress, "GiB/h"},
	}
	// Ascending, because a table of numbers read out of order is a table people
	// misread. "The p95 first" is honoured where a reader looks first: the check
	// lines above, which lead with it, and the summary line below, which quotes it.
	if _, err := fmt.Fprintf(w, "\n%-14s %10s %10s %10s %10s %10s\n",
		"measurement", "min", "p50", "p95", "p99", "max"); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(w, "%-14s %10s %10s %10s %10s %10s\n", row.label,
			statCell(row.stat.Min, row.unit), statCell(row.stat.P50, row.unit),
			pctCell(row.stat.P95, row.unit), pctCell(row.stat.P99, row.unit),
			statCell(row.stat.Max, row.unit)); err != nil {
			return err
		}
	}
	// Say what the missing columns would need rather than leaving a dash to be
	// interpreted: an absent percentile is a limit of the audience, not of the
	// stream, and this tool never reports the first as if it were the second.
	if p.Startup.P95 == nil || p.Startup.P99 == nil {
		need := "a p99 needs 100 viewers"
		if p.Startup.P95 == nil {
			need = "a p95 needs 20 viewers and " + need
		}
		if _, err := fmt.Fprintf(w, "%s\n", paint(dim, fmt.Sprintf("— : %s; with %d there is no tail to read at that resolution", need, p.Viewers))); err != nil {
			return err
		}
	}

	// Not everybody necessarily watched the same amount: a viewer whose trace
	// ran out stops early, and claiming one figure for all of them would be the
	// median hiding the tail again, one line further down.
	lo, hi := p.Runs[0].Media, p.Runs[0].Media
	for _, v := range p.Runs {
		if v.Media < lo {
			lo = v.Media
		}
		if v.Media > hi {
			hi = v.Media
		}
	}
	watched := secs(lo) + " of media each"
	if lo != hi {
		watched = secs(lo) + "–" + secs(hi) + " of media"
	}
	// A score never appears without the judgement that produced it.
	if p.QoE.Viewers > 0 {
		if _, err := fmt.Fprintf(w, "%s\n", paint(dim, fmt.Sprintf(
			"qoe is Mbps-equivalent: a 2.4 is worth a steady 2.4 Mbps with no stalls and no switching, charging %.1f per frozen second and %.1f per Mbps switched (the literature's weights, not ours)",
			p.QoEWeights.Rebuffer, p.QoEWeights.Switch))); err != nil {
			return err
		}
	}

	// What each rung actually served. `ladder-gap` names a hole; this names the
	// rungs that earned their place, and the ones that did not.
	if len(p.Rungs) > 0 {
		if _, err := fmt.Fprintf(w, "\n%-12s %10s %10s %10s %10s %12s\n",
			"rung", "bitrate", "served", "share", "viewers", "GiB/h"); err != nil {
			return err
		}
		var idle []string
		for _, u := range p.Rungs {
			if u.Segments == 0 {
				idle = append(idle, u.Name)
				continue
			}
			if _, err := fmt.Fprintf(w, "%-12s %10s %10s %10s %10s %12s\n",
				u.Name, fmt.Sprintf("%.1fM", float64(u.Bitrate)/1e6), secs(u.Seconds),
				fmt.Sprintf("%.0f%%", u.Share*100), fmt.Sprintf("%d", u.Viewers),
				fmt.Sprintf("%.2f", u.PerViewerHour/(1<<30))); err != nil {
				return err
			}
		}
		// The rungs nothing chose get one line rather than one row each: Apple's
		// advanced example has 54 rungs and fifty of them serve nothing on a 3 Mbps
		// cell, which buries the report. They are still *named* — an unused rung is
		// what an operator is deciding about, so hiding it would be worse than the
		// clutter.
		if len(idle) > 0 {
			shown := idle
			more := ""
			if len(shown) > 6 {
				shown, more = shown[:6], fmt.Sprintf(" and %d more", len(idle)-6)
			}
			if _, err := fmt.Fprintf(w, "%s\n", paint(dim, fmt.Sprintf(
				"%d rungs served nothing at all: %s%s — they cost encoding, storage and egress and bought no viewer anything on this trace (--json lists every rung)",
				len(idle), strings.Join(shown, ", "), more))); err != nil {
				return err
			}
		}
	}

	// The audience by screen, and only when somebody said what the screens are.
	if len(p.Devices) > 0 {
		if _, err := fmt.Fprintf(w, "\n%-10s %8s %8s %12s %10s %12s\n",
			"screen", "ceiling", "viewers", "frozen p50", "qoe p50", "GiB/h p50"); err != nil {
			return err
		}
		for _, d := range p.Devices {
			ceiling := "no cap"
			if d.Ceiling > 0 {
				ceiling = fmt.Sprintf("%dp", d.Ceiling)
			}
			if _, err := fmt.Fprintf(w, "%-10s %8s %8d %12s %10s %12s\n",
				d.Name, ceiling, d.Viewers, secs(d.Frozen.P50),
				fmt.Sprintf("%.2f", d.QoE.P50), fmt.Sprintf("%.2f", d.Egress.P50/(1<<30))); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "%s\n", paint(dim, fmt.Sprintf(
			"the mix `%s` is what you asked for, not what abrsim guessed: a rung taller than a screen is not fetched by it",
			p.DeviceMix))); err != nil {
			return err
		}
	}

	tail := ""
	if p.Startup.P95 != nil && p.Frozen.P95 != nil {
		tail = fmt.Sprintf(" — at the p95: %s to the first frame, %s frozen",
			secs(*p.Startup.P95), secs(*p.Frozen.P95))
	}
	if _, err := fmt.Fprintf(w, "\n%d checks over %d viewers — %d segments each, %s, worst finding %s%s\n",
		len(p.Checks), p.Viewers, p.Segments, watched, p.Worst(), tail); err != nil {
		return err
	}
	if p.Incomplete > 0 {
		if _, err := fmt.Fprintln(w, paint(dim, fmt.Sprintf("%d viewers could not finish: their trace ran out of bandwidth, so those figures are floors", p.Incomplete))); err != nil {
			return err
		}
	}
	if p.Estimated {
		_, err := fmt.Fprintln(w, paint(dim, "segment sizes are declared, not measured — run with --sizes measured for real byte counts"))
		return err
	}
	return nil
}

// statCell formats one number of the distribution table. Bits per second become
// Mbps because nobody reads 2400000, and a whole number stays whole so a count of
// stalls does not read as 6.0 of something.
func statCell(v float64, unit string) string {
	switch unit {
	case "Mbps":
		return fmt.Sprintf("%.2f", v/1e6)
	case "s":
		return secs(v)
	case "GiB/h":
		return fmt.Sprintf("%.2f", v/(1<<30))
	case "score":
		// Already Mbps-equivalent: dividing it by a million printed 0.00 for a
		// whole audience, and the run that caught it was a real one.
		return fmt.Sprintf("%.2f", v)
	}
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.1f", v)
}

// pctCell formats a percentile the audience may not support. An em dash rather
// than a zero: a number that was not measured must not look like a measurement.
func pctCell(v *float64, unit string) string {
	if v == nil {
		return "—"
	}
	return statCell(*v, unit)
}

// statusCell keeps the severity columns aligned when a percentile is absent.
func statusCell(s finding.Status) string {
	if s == "" {
		return "—"
	}
	return string(s)
}

// JSONPopulation renders the audience. Per-viewer *summaries*, never their
// request timelines: two hundred timelines is not a document anybody reads, and
// the single-viewer report already carries the full one.
func JSONPopulation(w io.Writer, r PopulationReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Worst is what --exit-on judges over an audience: the highest severity anything
// reached anywhere in it, because a gate that only read the median viewer would
// pass a ladder that freezes for one person in twenty.
func WorstOf(p population.Report) finding.Status { return p.Worst() }
