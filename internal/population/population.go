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
	"math"
	"runtime"
	"sort"
	"sync"

	"github.com/Allan-Nava/abrsim/internal/abr"
	"github.com/Allan-Nava/abrsim/internal/analyze"
	"github.com/Allan-Nava/abrsim/internal/device"
	"github.com/Allan-Nava/abrsim/internal/finding"
	"github.com/Allan-Nava/abrsim/internal/manifest"
	"github.com/Allan-Nava/abrsim/internal/sim"
	"github.com/Allan-Nava/abrsim/internal/trace"
)

// Stat is the spread of one measurement over the audience.
//
// The percentiles are **nearest-rank order statistics**: every figure reported
// here is a value some real viewer had. An interpolated median of [1,2,3,4] is
// 2.5, which nobody in that audience experienced, and inventing a measurement is
// the one thing this tool must not do.
//
// P95 and P99 are pointers because a small audience cannot support them. A "p95"
// over ten viewers is the maximum wearing a better name — the tail has no
// resolution there — so it is reported as absent rather than as a number, which
// is the same `(value, false)` protocol the rest of the codebase uses for "I
// could not measure this". In JSON they come out as null.
type Stat struct {
	Viewers int      `json:"viewers"`
	Min     float64  `json:"min"`
	P50     float64  `json:"p50"`
	P95     *float64 `json:"p95"`
	P99     *float64 `json:"p99"`
	Max     float64  `json:"max"`
}

// p95Needs and p99Needs are the smallest audiences in which one viewer *is* the
// top 5% and the top 1%. Below them the percentile is not reported at all.
const (
	p95Needs = 20
	p99Needs = 100
)

// Viewer is what one simulated session came to, without its request timeline: a
// document carrying two hundred timelines is not a document anybody reads. Run a
// single viewer for the full one.
type Viewer struct {
	Index     int            `json:"index"`
	Startup   float64        `json:"startup"`
	Frozen    float64        `json:"frozen"`
	Stalls    int            `json:"stalls"`
	Switches  int            `json:"switches"`
	Device    string         `json:"device,omitempty"`
	Media     float64        `json:"media"`
	Wall      float64        `json:"wall"`
	Bytes     int64          `json:"bytes"`
	Delivered float64        `json:"delivered_bps"`
	QoE       float64        `json:"qoe"`
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

	// AtP50, AtP95 and AtP99 are this check's severity at those points of the
	// audience, which is the sentence AB-37 exists for: not "this went BAD
	// somewhere" but "at the 95th percentile of your viewers, this is BAD".
	// Empty when the audience cannot support the percentile.
	AtP50 finding.Status `json:"at_p50"`
	AtP95 finding.Status `json:"at_p95,omitempty"`
	AtP99 finding.Status `json:"at_p99,omitempty"`
	// FiresFrom is the percentile this check starts speaking above: a check
	// quiet for 60% of the audience fires from p60 up. Zero when it is quiet
	// everywhere.
	FiresFrom float64 `json:"fires_from,omitempty"`

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

// RungUse is one rung's share of the audience: the seconds it served, how many
// viewers ever chose it, and the bytes it cost to ship.
type RungUse struct {
	Rung     int     `json:"rung"`
	Name     string  `json:"name"`
	Bitrate  int64   `json:"bitrate"`
	Viewers  int     `json:"viewers"`
	Segments int     `json:"segments"`
	Seconds  float64 `json:"seconds"`
	Share    float64 `json:"share"`
	Bytes    int64   `json:"bytes"`
	// PerViewerHour is this rung's bytes per hour of watching across the whole
	// audience — the figure a delivery bill is made of, per rung.
	PerViewerHour float64 `json:"bytes_per_viewer_hour"`
}

// DeviceSpread is one class of screen inside the audience.
type DeviceSpread struct {
	Name    string `json:"name"`
	Ceiling int    `json:"ceiling"`
	Viewers int    `json:"viewers"`
	Frozen  Stat   `json:"frozen"`
	QoE     Stat   `json:"qoe"`
	Egress  Stat   `json:"bytes_per_viewer_hour"`
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

	// QoE is the linear score of AB-41 over the audience, and QoEWeights are the
	// prices behind it. They travel together on purpose: a score printed without
	// the judgement that produced it is a grade nobody can defend.
	QoE        Stat               `json:"qoe"`
	QoEWeights analyze.QoEWeights `json:"qoe_weights"`

	// Egress is what an hour of one viewer's watching costs to deliver, spread
	// over the audience (AB-39). No severity: what a gigabyte is worth is a
	// commercial question and abrsim does not know the contract.
	Egress Stat `json:"bytes_per_viewer_hour"`

	Checks []CheckSpread `json:"checks"`
	// Rungs is what each rung of the ladder actually served across the whole
	// audience (AB-38), including the rungs nothing ever chose — those are the
	// ones worth arguing about, and leaving them out would hide them.
	Rungs []RungUse `json:"rungs"`
	Runs  []Viewer  `json:"runs"`

	// DeviceMix is the mix as it was asked for, and Devices the audience split by
	// it (AB-40). Both are empty unless --devices was given: abrsim does not know
	// who watches this stream, and inventing an audience is inventing a
	// measurement.
	DeviceMix string         `json:"device_mix,omitempty"`
	Devices   []DeviceSpread `json:"devices,omitempty"`

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
// Every viewer gets the whole ladder: with no device mix stated there is nothing to
// cap it with.
func Run(l manifest.Ladder, base trace.Trace, algName string, opts sim.Options, viewers int) (Report, error) {
	return RunWith(l, base, algName, opts, viewers, device.Mix{})
}

// RunWith is Run with a device mix: each viewer is given a class of screen, and a
// screen that cannot show a rung does not fetch it (AB-40).
func RunWith(l manifest.Ladder, base trace.Trace, algName string, opts sim.Options, viewers int, mix device.Mix) (Report, error) {
	if viewers < 1 {
		return Report{}, fmt.Errorf("a population needs at least one viewer, not %d", viewers)
	}
	if _, ok := abr.New(algName); !ok {
		return Report{}, fmt.Errorf("no algorithm %q", algName)
	}

	traces := trace.Population(base, viewers)
	// One ladder per viewer, because a phone's ladder is not a television's. With
	// no mix this is the same ladder n times, which is what it was before.
	devices := mix.Assign(viewers)
	ladders := make([]manifest.Ladder, viewers)
	for v := range ladders {
		ladders[v] = l
		if v < len(devices) {
			if spec, ok := device.Class(devices[v]); ok {
				ladders[v] = l.CapHeight(spec.Ceiling)
			}
		}
	}
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
				res, err := sim.Run(ladders[v], traces[v], alg, opts)
				if err != nil {
					errs[v] = err
					continue
				}
				results[v] = res
				found[v] = analyze.Run(res, traces[v], ladders[v])
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

	return summarise(l, base, algName, results, found, mix, devices), nil
}

func summarise(l manifest.Ladder, base trace.Trace, algName string, results []sim.Result, found [][]finding.Finding, mix device.Mix, devices []string) Report {
	rep := Report{
		Viewers:   len(results),
		Trace:     base.Name,
		Algorithm: algName,
		Runs:      make([]Viewer, len(results)),
	}
	if len(l.Renditions) > 0 {
		rep.Segments = len(l.Renditions[0].Segments)
	}

	weights := analyze.DefaultQoEWeights()
	rep.QoEWeights = weights
	qoe := make([]float64, 0, len(results))
	egress := make([]float64, 0, len(results))
	// Keyed by the *full* ladder: with a device mix two viewers have different
	// ladders, so rung 2 of one is not rung 2 of another. Indexing by position
	// would attribute a television's 1080p seconds to a phone's top rung.
	full := l.Rungs()
	rungs := make([]RungUse, len(full))
	at := make(map[string]int, len(full))
	for i, r := range full {
		rungs[i] = RungUse{Rung: i, Name: r.Name, Bitrate: r.Bandwidth}
		at[r.Name] = i
	}
	startup := make([]float64, len(results))
	frozen := make([]float64, len(results))
	stalls := make([]float64, len(results))
	switches := make([]float64, len(results))
	delivered := make([]float64, len(results))

	spreads := map[string]*CheckSpread{}
	statuses := map[string][]finding.Status{}
	var order []string

	for v, res := range results {
		rate := 0.0
		if res.Media > 0 {
			rate = float64(res.Switches) / (res.Media / 60)
		}
		startup[v], frozen[v] = res.Startup, res.StallTime
		stalls[v], switches[v] = float64(res.Stalls), rate
		delivered[v] = res.DeliveredBitrate()

		score, scored := analyze.QoE(res, weights)
		if scored {
			qoe = append(qoe, score)
		}
		if e, ok := res.BytesPerViewerHour(); ok {
			egress = append(egress, e)
		}
		// Attribution is summed across the audience rather than averaged: the
		// question is how much playback a rung served, and a mean of shares
		// would weight a viewer who watched ten seconds like one who watched an
		// hour.
		for _, u := range res.RungUse() {
			i, ok := at[u.Name]
			if !ok {
				continue // a rung this run had and the ladder does not: not ours to attribute
			}
			r := &rungs[i]
			r.Segments += u.Segments
			r.Seconds += u.Seconds
			r.Bytes += u.Bytes
			if u.Segments > 0 {
				r.Viewers++
			}
		}

		if res.Estimated {
			rep.Estimated = true
		}
		if res.Incomplete {
			rep.Incomplete++
		}

		class := ""
		if v < len(devices) {
			class = devices[v]
		}
		rep.Runs[v] = Viewer{
			Index: v, Device: class, Startup: res.Startup, Frozen: res.StallTime,
			Stalls: res.Stalls, Switches: res.Switches,
			Media: res.Media, Wall: res.Wall, Bytes: res.Bytes,
			Delivered: delivered[v], QoE: score,
			Worst: finding.Worst(found[v]),
		}

		for _, f := range found[v] {
			s, ok := spreads[f.Check]
			if !ok {
				s = &CheckSpread{Check: f.Check, Worst: finding.OK}
				spreads[f.Check] = s
				order = append(order, f.Check)
			}
			statuses[f.Check] = append(statuses[f.Check], f.Status)
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

	// Shares over the audience's whole playback, and each rung's egress per hour
	// of watching — the same arithmetic as one session, one level up.
	var media float64
	for _, res := range results {
		media += res.Media
	}
	for i := range rungs {
		if media > 0 {
			rungs[i].Share = rungs[i].Seconds / media
			rungs[i].PerViewerHour = float64(rungs[i].Bytes) * 3600 / media
		}
	}
	rep.Rungs = rungs
	rep.Egress = statOf(egress)
	rep.QoE = statOf(qoe)

	// The audience split by screen, ordered by the mix as it was given rather than
	// by however a map iterates: two runs must produce the same bytes.
	if len(mix.Shares) > 0 {
		rep.DeviceMix = mix.String()
		for _, share := range mix.Shares {
			spec, _ := device.Class(share.Name)
			d := DeviceSpread{Name: share.Name, Ceiling: spec.Ceiling}
			var fr, qo, eg []float64
			for v, res := range results {
				if v >= len(devices) || devices[v] != share.Name {
					continue
				}
				d.Viewers++
				fr = append(fr, res.StallTime)
				if score, ok := analyze.QoE(res, weights); ok {
					qo = append(qo, score)
				}
				if e, ok := res.BytesPerViewerHour(); ok {
					eg = append(eg, e)
				}
			}
			d.Frozen, d.QoE, d.Egress = statOf(fr), statOf(qo), statOf(eg)
			rep.Devices = append(rep.Devices, d)
		}
	}

	rep.Startup, rep.Frozen = statOf(startup), statOf(frozen)
	rep.Stalls, rep.SwitchRate = statOf(stalls), statOf(switches)
	rep.Delivered = statOf(delivered)

	// Each check's severity at the points of the audience worth naming. The
	// statuses are sorted by severity so the percentile means the same thing it
	// does for the numbers: the nth worst viewer, not the nth to be simulated.
	for name, ss := range statuses {
		sort.SliceStable(ss, func(i, j int) bool {
			return finding.Severity(ss[i]) < finding.Severity(ss[j])
		})
		s := spreads[name]
		s.AtP50, _ = statusAt(ss, 50)
		if v, ok := statusAt(ss, 95); ok {
			s.AtP95 = v
		}
		if v, ok := statusAt(ss, 99); ok {
			s.AtP99 = v
		}
		if s.Loud > 0 {
			s.FiresFrom = 100 * float64(s.OK) / float64(len(ss))
		}
	}

	// Worst at the p95 first — the percentile an operator is paid to care about —
	// then worst anywhere, then the check loud for the most viewers, then by name.
	// Sorted from a slice built in first-seen order rather than by ranging a map,
	// because two runs must produce the same bytes.
	sort.SliceStable(order, func(a, b int) bool {
		x, y := spreads[order[a]], spreads[order[b]]
		if sx, sy := finding.Severity(x.AtP95), finding.Severity(y.AtP95); sx != sy {
			return sx > sy
		}
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

// statOf is the spread of one measurement.
func statOf(vs []float64) Stat {
	if len(vs) == 0 {
		return Stat{}
	}
	s := make([]float64, len(vs))
	copy(s, vs)
	sort.Float64s(s)
	out := Stat{Viewers: len(s), Min: s[0], P50: rank(s, 50), Max: s[len(s)-1]}
	if len(s) >= p95Needs {
		v := rank(s, 95)
		out.P95 = &v
	}
	if len(s) >= p99Needs {
		v := rank(s, 99)
		out.P99 = &v
	}
	return out
}

// rank is the nearest-rank percentile of an ascending slice: the ceil(p/100 × n)th
// value, so the answer is always one of the observations rather than a number
// between two of them.
func rank(sorted []float64, p float64) float64 {
	n := len(sorted)
	i := int(math.Ceil(p / 100 * float64(n)))
	if i < 1 {
		i = 1
	}
	if i > n {
		i = n
	}
	return sorted[i-1]
}

// statusAt is a check's severity at one point of the audience: the statuses of
// every viewer, sorted worst-last, read at the same nearest rank the numbers use.
// ok is false when the audience is too small for that percentile.
func statusAt(sorted []finding.Status, p float64) (finding.Status, bool) {
	n := len(sorted)
	switch {
	case n == 0:
		return "", false
	case p >= 99 && n < p99Needs:
		return "", false
	case p >= 95 && n < p95Needs:
		return "", false
	}
	i := int(math.Ceil(p / 100 * float64(n)))
	if i < 1 {
		i = 1
	}
	if i > n {
		i = n
	}
	return sorted[i-1], true
}
