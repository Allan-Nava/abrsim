// Package output renders a report.
//
// Two rules, both inherited rather than invented: worst findings first, so the
// first line is the thing the operator has to look at; and colour only on a
// terminal, because JSON and anything piped ends up in an incident document
// where an escape sequence is noise.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Allan-Nava/abrsim/internal/finding"
	"github.com/Allan-Nava/abrsim/internal/manifest"
	"github.com/Allan-Nava/abrsim/internal/sim"
)

// Report is one run of one ladder over one trace.
type Report struct {
	Source   string            `json:"source"`
	Ladder   []manifest.Rung   `json:"ladder,omitempty"`
	Run      sim.Result        `json:"run"`
	Findings []finding.Finding `json:"findings"`
	Options  map[string]any    `json:"options,omitempty"`
}

// UseColour decides whether to colour, given whether the output is a terminal
// and the value of NO_COLOR.
func UseColour(isTTY bool, noColor string) bool { return isTTY && noColor == "" }

const (
	reset  = "\x1b[0m"
	dim    = "\x1b[2m"
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	green  = "\x1b[32m"
	purple = "\x1b[35m"
)

func colourFor(s finding.Status) string {
	switch s {
	case finding.BAD:
		return red
	case finding.WARN:
		return yellow
	case finding.ERROR:
		return purple
	default:
		return green
	}
}

func markFor(s finding.Status) string {
	switch s {
	case finding.BAD:
		return "🔴"
	case finding.WARN:
		return "🟡"
	case finding.ERROR:
		return "🟣"
	default:
		return "🟢"
	}
}

// Text renders for a terminal.
func Text(w io.Writer, r Report, colour bool) error {
	paint := func(c, s string) string {
		if !colour {
			return s
		}
		return c + s + reset
	}

	for _, f := range r.Findings {
		line := fmt.Sprintf("%s %-6s %-11s %-16s %s",
			markFor(f.Status), paint(colourFor(f.Status), string(f.Status)), f.Check, f.Target, f.Message)
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
		if f.Hint != "" {
			if _, err := fmt.Fprintln(w, paint(dim, "                            ↳ "+f.Hint)); err != nil {
				return err
			}
		}
	}

	c := finding.Summarize(r.Findings)
	if _, err := fmt.Fprintf(w, "\n%d checks: %d OK, %d WARN, %d BAD, %d ERROR — %s\n",
		len(r.Findings), c[finding.OK], c[finding.WARN], c[finding.BAD], c[finding.ERROR], summary(r.Run)); err != nil {
		return err
	}
	if r.Run.Estimated {
		_, err := fmt.Fprintln(w, paint(dim, "segment sizes are declared, not measured — run with --sizes measured for real byte counts"))
		return err
	}
	return nil
}

// summary is the one line of context every finding above it is relative to.
func summary(res sim.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d segments, %s, %s of media in %s over `%s` with %s",
		len(res.Requests), humanBytes(res.Bytes), secs(res.Media), secs(res.Wall), res.Trace, res.Algorithm)
	if res.MeanRungBitrate() > 0 {
		fmt.Fprintf(&b, ", mean rung %.1f Mbps", res.MeanRungBitrate()/1e6)
	}
	return b.String()
}

// JSON renders the whole report, timeline included: without the per-request
// detail a consumer can only re-derive the totals it was already given.
func JSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func secs(v float64) string {
	if v < 10 {
		return fmt.Sprintf("%.1fs", v)
	}
	return fmt.Sprintf("%.0fs", v)
}
