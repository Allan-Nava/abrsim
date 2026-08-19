package trace

import "sort"

// A ladder is not shipped to one viewer. The rung somebody is about to delete is
// defended or condemned by the tail of an audience rather than by its median, so
// a single session over a single trace is an anecdote — a true one, and still an
// anecdote.
//
// Population turns one trace into n of them: the same network, seen by n
// different people. Viewer 0 is the trace exactly as measured, so `--viewers 1`
// is byte-for-byte the run this tool has always done. Every other viewer gets a
// per-viewer scale — one person is on a better cell than another all session —
// and a per-sample jitter, because two people on the same cell do not see the
// same dip at the same instant.
//
// The variation comes from wobble(), the fixed-seed integer hash the built-in
// traces themselves are generated with: `--viewers 500` is the same 500 viewers
// on every machine and every Go release. A population nobody can regenerate is a
// benchmark nobody can argue with.

// Population derives n traces from base. Viewer 0 is base itself. n <= 0 gives
// nil rather than an error: the caller decides whether that is a usage mistake.
//
// The scales are *stratified* rather than drawn independently: the n-1 derived
// viewers take evenly spaced quantiles of the scale range, in an order the hash
// permutes. Independent draws needed a couple of hundred viewers before both
// tails reliably existed — the first thirty draws of the obvious seeding came out
// with a mean scale of 1.14, so `--viewers 30` was thirty people on a better line
// than the one measured, and the tail the population exists for was missing.
// Stratifying means even `--viewers 8` spans the range.
//
// The cost is stated rather than hidden: a viewer's scale depends on how many
// viewers there are, so viewer 5 of 30 is not viewer 5 of 200. Within one n the
// population is identical everywhere, which is the property the reports rest on.
func Population(base Trace, n int) []Trace {
	if n <= 0 {
		return nil
	}
	out := make([]Trace, n)
	out[0] = base
	for v, scale := range scales(n) {
		out[v+1] = derive(base, v+1, scale)
	}
	return out
}

// scaleLow and scaleHigh bound how far a population reaches: the worst line is a
// little over half the measured one and the best half again as good. No wider —
// past that it stops being a spread around this network and becomes a different
// network, which is what --trace is for.
const (
	scaleLow, scaleHigh   = 0.6, 1.5
	jitterLow, jitterHigh = 0.85, 1.15
)

// scales returns the n-1 stratified scale factors, permuted. The permutation
// matters: a caller that looks at the first ten viewers of two hundred must not
// be looking at the ten worst lines.
func scales(n int) []float64 {
	k := n - 1
	if k <= 0 {
		return nil
	}
	order := make([]int, k)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return wobble(order[a]+1) < wobble(order[b]+1)
	})
	out := make([]float64, k)
	for pos, stratum := range order {
		q := (float64(stratum) + 0.5) / float64(k)
		out[pos] = scaleLow + (scaleHigh-scaleLow)*q
	}
	return out
}

func derive(base Trace, v int, scale float64) Trace {
	out := Trace{Name: base.Name, Samples: make([]Sample, len(base.Samples))}
	for i, s := range base.Samples {
		// A tunnel is a tunnel for everyone: no scale applied to nothing can
		// produce something, and inventing bandwidth inside an outage would
		// quietly remove the one trace no adaptation can save.
		if s.BPS == 0 {
			out.Samples[i] = s
			continue
		}
		j := jitterLow + (jitterHigh-jitterLow)*wobble(jitterSeed*v+i+1)
		out.Samples[i] = Sample{At: s.At, BPS: round(s.BPS * scale * j)}
	}
	return out
}

// jitterSeed keeps one viewer's per-sample draws clear of the next viewer's: with
// a stride smaller than the longest trace, viewer 3's dip would be viewer 4's.
const jitterSeed = 104729
