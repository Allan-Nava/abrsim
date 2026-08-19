// Package population simulates the same ladder for many viewers on variations of
// one network, and reports the spread rather than a single session.
//
// One run over one trace answers what one viewer got, and a ladder decision
// cannot rest on that: nobody encodes a rung for one viewer. What this package
// adds is the distribution — the viewer who waited six seconds for the first
// frame is invisible in a mean of 1.4, and that viewer is the reason an operator
// is reading the report at all.
//
// It reports min, median and max. Percentiles, and findings that carry the
// percentile they fired at, are AB-37: a p95 over eight viewers would be
// arithmetic pretending to be a measurement.
//
// Determinism survives the concurrency: the traces come from trace.Population's
// fixed-seed hash, each viewer gets its own algorithm instance because an
// adaptation algorithm is stateful, and results land in a slice indexed by viewer
// rather than in the order the goroutines finish.
package population

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"

	"github.com/Allan-Nava/abrsim/internal/abr"
	"github.com/Allan-Nava/abrsim/internal/analyze"
	"github.com/Allan-Nava/abrsim/internal/finding"
	"github.com/Allan-Nava/abrsim/internal/manifest"
	"github.com/Allan-Nava/abrsim/internal/sim"
	"github.com/Allan-Nava/abrsim/internal/trace"
)

// Stat is the spread of one measurement over the audience.
type Stat struct {
	Min    float64 `json:"min"`
	Median float64 `json:"median"`
	Max    float64 `json:"max"`
}

// Viewer is what one simulated session came to, without its request timeline: a
// document carrying two hundred timelines is not a document anybody reads. Run a
// single viewer for the full one.
type Viewer struct {
	Index     int            `json:"index"`
	Startup   float64        `json:"startup"`
	Frozen    float64        `json:"frozen"`
	Stalls    int            `json:"stalls"`
	Switches  int            `json:"switches"`
	Media     float64        `json:"media"`
	Wall      float64        `json:"wall"`
	Bytes     int64          `json:"bytes"`
	Delivered float64        `json:"delivered_bps"`
	Worst     finding.Status `json:"worst"`
}

// CheckSpread is one check over the whole audience: how many viewers it stayed
// quiet for, how many it went loud for, and the worst thing it had to say.
type CheckSpread struct {
	Check        string         `json:"check"`
	Worst        finding.Status `json:"worst"`
	Loud         int            `json:"above_ok"`
	OK           int            `json:"ok"`
	Warn         int            `json:"warn"`
	Bad          int            `json:"bad"`
	Error        int            `json:"error"`
	WorstTarget  string         `json:"worst_target,omitempty"`
	WorstMessage string         `json:"worst_message,omitempty"`
	WorstHint    string         `json:"worst_hint,omitempty"`
	WorstViewer  int            `json:"worst_viewer"`

	// worstValue is the measurement behind WorstMessage, kept so a later
	// viewer's finding of the same severity can be compared against it. Not
	// exported: the value is already in the message and in the distribution,
	// and a second copy in the JSON would be a second thing to keep in step.
	// A float and a bool rather than the *float64 the finding carries, so the
	// struct stays comparable — the determinism test compares whole spreads,
	// and two pointers to the same number are not equal.
	worstValue    float64
	worstHasValue bool
}

// Report is the whole audience.
type Report struct {
	Viewers   int    `json:"viewers"`
	Trace     string `json:"trace"`
	Algorithm string `json:"algorithm"`

	Startup    Stat `json:"startup"`
	Frozen     Stat `json:"frozen"`
	Stalls     Stat `json:"stalls"`
	SwitchRate Stat `json:"switches_per_min"`
	Delivered  Stat `json:"delivered_bps"`

	Checks []CheckSpread `json:"checks"`
	Runs   []Viewer      `json:"runs"`

	// Estimated is true when any segment size was derived from the declared
	// bitrate: it travels with the population for the same reason it travels
	// with a single run.
	Estimated bool `json:"estimated"`
	// Incomplete counts the viewers whose session could not finish because
	// their trace ran out of bandwidth. Those are not clean results.
	Incomplete int `json:"incomplete"`
	// Segments is how many the ladder holds, so the summary line can say what
	// each viewer watched.
	Segments int `json:"segments"`
}

// Worst is the highest severity anything reached anywhere in the audience. It is
// what --exit-on judges: a gate that only looked at the median viewer would pass
// a ladder that freezes for one person in twenty.
func (r Report) Worst() finding.Status {
	worst := finding.OK
	for _, c := range r.Checks {
		if finding.AtLeast(c.Worst, worst) {
			worst = c.Worst
		}
	}
	return worst
}

// Run simulates viewers sessions over variations of base and returns the spread.
func Run(l manifest.Ladder, base trace.Trace, algName string, opts sim.Options, viewers int) (Report, error) {
	if viewers < 1 {
		return Report{}, fmt.Errorf("a population needs at least one viewer, not %d", viewers)
	}
	if _, ok := abr.New(algName); !ok {
		return Report{}, fmt.Errorf("no algorithm %q", algName)
	}

	traces := trace.Population(base, viewers)
	results := make([]sim.Result, viewers)
	found := make([][]finding.Finding, viewers)
	errs := make([]error, viewers)

	// Sessions are independent, so they fan out — but nothing about the output
	// may depend on which finished first, which is why every goroutine writes
	// to its own index and the reading happens afterwards.
	workers := runtime.GOMAXPROCS(0)
	if workers > viewers {
		workers = viewers
	}
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for v := range next {
				// A fresh algorithm per viewer: they carry throughput
				// estimates and buffer state, so a shared one would make
				// viewer 7 depend on viewer 6.
				alg, _ := abr.New(algName)
				res, err := sim.Run(l, traces[v], alg, opts)
				if err != nil {
					errs[v] = err
					continue
				}
				results[v] = res
				found[v] = analyze.Run(res, traces[v], l)
			}
		}()
	}
	for v := 0; v < viewers; v++ {
		next <- v
	}
	close(next)
	wg.Wait()

	// The first failure by index, never the first to arrive: two runs of the
	// same population have to report the same error.
	for v, err := range errs {
		if err != nil {
			return Report{}, fmt.Errorf("viewer %d: %w", v, err)
		}
	}
	if len(results) == 0 {
		return Report{}, errors.New("no viewers were simulated")
	}

	return summarise(l, base, algName, results, found), nil
}

func summarise(l manifest.Ladder, base trace.Trace, algName string, results []sim.Result, found [][]finding.Finding) Report {
	rep := Report{
		Viewers:   len(results),
		Trace:     base.Name,
		Algorithm: algName,
		Runs:      make([]Viewer, len(results)),
	}
	if len(l.Renditions) > 0 {
		rep.Segments = len(l.Renditions[0].Segments)
	}

	startup := make([]float64, len(results))
	frozen := make([]float64, len(results))
	stalls := make([]float64, len(results))
	switches := make([]float64, len(results))
	delivered := make([]float64, len(results))

	spreads := map[string]*CheckSpread{}
	var order []string

	for v, res := range results {
		rate := 0.0
		if res.Media > 0 {
			rate = float64(res.Switches) / (res.Media / 60)
		}
		startup[v], frozen[v] = res.Startup, res.StallTime
		stalls[v], switches[v] = float64(res.Stalls), rate
		delivered[v] = res.DeliveredBitrate()

		if res.Estimated {
			rep.Estimated = true
		}
		if res.Incomplete {
			rep.Incomplete++
		}

		rep.Runs[v] = Viewer{
			Index: v, Startup: res.Startup, Frozen: res.StallTime,
			Stalls: res.Stalls, Switches: res.Switches,
			Media: res.Media, Wall: res.Wall, Bytes: res.Bytes,
			Delivered: delivered[v], Worst: finding.Worst(found[v]),
		}

		for _, f := range found[v] {
			s, ok := spreads[f.Check]
			if !ok {
				s = &CheckSpread{Check: f.Check, Worst: finding.OK}
				spreads[f.Check] = s
				order = append(order, f.Check)
			}
			switch f.Status {
			case finding.WARN:
				s.Warn++
			case finding.BAD:
				s.Bad++
			case finding.ERROR:
				s.Error++
			default:
				s.OK++
			}
			if f.Status != finding.OK {
				s.Loud++
			}
			// Keep the worst thing this check said anywhere, and which
			// viewer said it: a count with no sentence beside it is a
			// number nobody can act on.
			//
			// "Worst" is the highest severity and then, within it, the
			// largest measurement — every check above OK measures something
			// where more is worse. Keeping the first viewer to cross the
			// threshold instead put "2.4s frozen" on a headline whose own
			// table said the worst viewer froze for 69 seconds, and a report
			// that disagrees with itself teaches an operator to distrust both
			// halves. Quiet checks keep the first viewer's sentence: there is
			// nothing to rank, and a stable row is worth more than an
			// arbitrary one.
			switch {
			case s.WorstMessage == "" && s.Worst == f.Status:
				keepWorst(s, f, v)
			case finding.Severity(f.Status) > finding.Severity(s.Worst):
				keepWorst(s, f, v)
			case f.Status == s.Worst && f.Status != finding.OK && worse(f, s):
				keepWorst(s, f, v)
			}
		}
	}

	rep.Startup, rep.Frozen = statOf(startup), statOf(frozen)
	rep.Stalls, rep.SwitchRate = statOf(stalls), statOf(switches)
	rep.Delivered = statOf(delivered)

	// Worst first, then the check that is loud for the most viewers, then by
	// name — sorted from a slice built in first-seen order rather than by
	// ranging a map, because two runs must produce the same bytes.
	sort.SliceStable(order, func(a, b int) bool {
		x, y := spreads[order[a]], spreads[order[b]]
		if sx, sy := finding.Severity(x.Worst), finding.Severity(y.Worst); sx != sy {
			return sx > sy
		}
		if x.Loud != y.Loud {
			return x.Loud > y.Loud
		}
		return x.Check < y.Check
	})
	for _, name := range order {
		rep.Checks = append(rep.Checks, *spreads[name])
	}
	return rep
}

func keepWorst(s *CheckSpread, f finding.Finding, viewer int) {
	s.Worst, s.WorstTarget, s.WorstMessage, s.WorstHint, s.WorstViewer = f.Status, f.Target, f.Message, f.Hint, viewer
	s.worstHasValue = f.Value != nil
	if f.Value != nil {
		s.worstValue = *f.Value
	} else {
		s.worstValue = 0
	}
}

// worse reports whether f measures more than the finding already kept. A finding
// with no value cannot beat one that has it: the number is what makes the
// comparison possible.
func worse(f finding.Finding, s *CheckSpread) bool {
	if f.Value == nil {
		return false
	}
	if !s.worstHasValue {
		return true
	}
	return *f.Value > s.worstValue
}

// statOf is the spread of one measurement. Median is the average of the two
// middle values on an even count, which is the convention every reader expects.
func statOf(vs []float64) Stat {
	if len(vs) == 0 {
		return Stat{}
	}
	s := make([]float64, len(vs))
	copy(s, vs)
	sort.Float64s(s)
	med := s[len(s)/2]
	if len(s)%2 == 0 {
		med = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return Stat{Min: s[0], Median: med, Max: s[len(s)-1]}
}
