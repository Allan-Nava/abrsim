package output

import (
	"encoding/json"
	"fmt"
	"io"

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
		message := fmt.Sprintf("viewer %d: %s", c.WorstViewer, c.WorstMessage)
		if c.Loud > 0 {
			share = fmt.Sprintf("%d of %d viewers (%.0f%%)", c.Loud, p.Viewers, 100*float64(c.Loud)/float64(p.Viewers))
			message = c.WorstMessage
		}
		line := fmt.Sprintf("%s %-6s %-11s %-24s %s",
			markFor(c.Worst), paint(colourFor(c.Worst), string(c.Worst)), c.Check, share, message)
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
		if c.Loud > 0 {
			// The worst viewer is named because a population is reproducible:
			// somebody can go and look at exactly that session.
			at := fmt.Sprintf("                            ↳ worst at viewer %d", c.WorstViewer)
			if c.WorstTarget != "" {
				at += " · " + c.WorstTarget
			}
			if c.WorstHint != "" {
				at += " · " + c.WorstHint
			}
			if _, err := fmt.Fprintln(w, paint(dim, at)); err != nil {
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
	}
	if _, err := fmt.Fprintf(w, "\n%-14s %10s %10s %10s\n", "measurement", "min", "median", "max"); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(w, "%-14s %10s %10s %10s\n", row.label,
			statCell(row.stat.Min, row.unit), statCell(row.stat.Median, row.unit), statCell(row.stat.Max, row.unit)); err != nil {
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
	if _, err := fmt.Fprintf(w, "\n%d checks over %d viewers — %d segments each, %s, worst finding %s\n",
		len(p.Checks), p.Viewers, p.Segments, watched, p.Worst()); err != nil {
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
	}
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.1f", v)
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
