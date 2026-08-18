// Package abr holds the adaptation algorithms the simulation runs.
//
// Three of them, deliberately. One algorithm's opinion is not a measurement:
// when the throughput rule stalls and the buffer rule does not, the difference
// is the estimator rather than the network, and that distinction is most of
// what an operator wants out of the tool.
package abr

import (
	"math"
	"sort"
)

// State is everything an algorithm may see at a decision point.
//
// It deliberately does not carry a bandwidth figure: an algorithm that wants
// one has to build it from the Observe calls it was given, exactly as a real
// player does, because a simulator that hands the player the true future
// bandwidth is not simulating adaptation at all.
type State struct {
	Buffer     float64 // seconds of media already buffered
	BufferCap  float64 // seconds the player will hold at most
	SegmentDur float64 // seconds, the segment about to be requested
	Index      int     // its index in the playlist
	Last       int     // the rung chosen last time, -1 before the first pick
}

// Algorithm chooses a rung. Bitrates are in bits per second and ascending.
type Algorithm interface {
	Name() string
	// Pick returns an index into bitrates.
	Pick(bitrates []int64, s State) int
	// Observe records the throughput of a completed download, in bits per
	// second.
	Observe(bps float64)
	// Reset forgets everything: a sweep runs one algorithm over many traces,
	// and an estimate carried from the end of one into the start of the next
	// would make the results depend on the order they were run in.
	Reset()
}

// New builds a named algorithm with its default parameters.
func New(name string) (Algorithm, bool) {
	switch name {
	case "throughput":
		return &Throughput{}, true
	case "buffer":
		return &BufferBased{}, true
	case "bola":
		return NewBOLA(), true
	}
	return nil, false
}

// Names lists the algorithms, sorted.
func Names() []string {
	out := []string{"bola", "buffer", "throughput"}
	sort.Strings(out)
	return out
}

// Describe is the one-line explanation shown by `abrsim algorithms`.
func Describe(name string) string {
	switch name {
	case "throughput":
		return "pick the highest rung under a smoothed estimate of measured throughput — the naive baseline, and the one that oscillates"
	case "buffer":
		return "pick on buffer level alone (BBA-style reservoir and cushion) — immune to throughput mis-estimation, slow to climb"
	case "bola":
		return "Lyapunov optimisation over buffer level (Spiteri et al.), in the parametrisation dash.js ships"
	}
	return ""
}

// ---------------------------------------------------------------------------

// Throughput picks the highest rung whose bitrate fits under a smoothed
// estimate of measured throughput, discounted by a safety factor.
type Throughput struct {
	Safety float64 // fraction of the estimate to spend, default 0.9
	Alpha  float64 // EWMA weight of the newest sample, default 0.2

	ewma float64
	seen bool
}

func (t *Throughput) Name() string { return "throughput" }

func (t *Throughput) safety() float64 {
	if t.Safety > 0 {
		return t.Safety
	}
	return 0.9
}

func (t *Throughput) alpha() float64 {
	if t.Alpha > 0 {
		return t.Alpha
	}
	return 0.2
}

// Estimate is the current smoothed throughput in bits per second.
func (t *Throughput) Estimate() float64 { return t.ewma }

func (t *Throughput) Observe(bps float64) {
	if bps <= 0 {
		return
	}
	if !t.seen {
		t.ewma, t.seen = bps, true
		return
	}
	a := t.alpha()
	t.ewma = a*bps + (1-a)*t.ewma
}

func (t *Throughput) Reset() { t.ewma, t.seen = 0, false }

func (t *Throughput) Pick(bitrates []int64, s State) int {
	// Nothing has been downloaded yet, so there is no estimate to pick from.
	// Guessing high costs the viewer the one thing they notice most, the wait
	// before the first frame.
	if !t.seen {
		return 0
	}
	budget := t.ewma * t.safety()
	pick := 0
	for i, b := range bitrates {
		if float64(b) <= budget {
			pick = i
		}
	}
	return pick
}

// ---------------------------------------------------------------------------

// BufferBased picks on buffer level alone: the lowest rung below the reservoir,
// the highest above reservoir+cushion, linear in between.
//
// It never looks at throughput, which is the whole point — it is the control
// group that says whether a stall was the network or the estimate of it.
type BufferBased struct {
	Reservoir float64 // seconds below which only the lowest rung is safe
	Cushion   float64 // seconds over which the ladder is spread
}

func (b *BufferBased) Name() string    { return "buffer" }
func (b *BufferBased) Observe(float64) {}
func (b *BufferBased) Reset()          {}

func (b *BufferBased) params(s State) (reservoir, cushion float64) {
	reservoir, cushion = b.Reservoir, b.Cushion
	if reservoir <= 0 {
		reservoir = 0.15 * s.BufferCap
	}
	if cushion <= 0 {
		cushion = 0.60 * s.BufferCap
	}
	return reservoir, cushion
}

func (b *BufferBased) Pick(bitrates []int64, s State) int {
	last := len(bitrates) - 1
	if last <= 0 {
		return 0
	}
	reservoir, cushion := b.params(s)
	if cushion <= 0 {
		return 0
	}
	switch {
	case s.Buffer <= reservoir:
		return 0
	case s.Buffer >= reservoir+cushion:
		return last
	}
	frac := (s.Buffer - reservoir) / cushion
	i := int(frac * float64(len(bitrates)))
	if i > last {
		i = last
	}
	return i
}

// ---------------------------------------------------------------------------

// BOLA is the Lyapunov formulation of Spiteri, Urgaonkar and Sitaraman
// ("BOLA: Near-Optimal Bitrate Adaptation for Online Videos", INFOCOM 2016),
// in the parametrisation dash.js ships.
//
// At each decision it maximises
//
//	(Vp·(v_m + gp) − Q) / S_m
//
// over the rungs, where Q is the buffer in seconds, S_m the rung's bitrate and
// v_m = ln(S_m/S_0) its utility. Because each rung's score falls with Q at a
// rate 1/S_m, the chosen rung is non-decreasing in buffer level — that is the
// algorithm's guarantee, and the first thing worth asserting about it.
//
// The parameters are dash.js's, not a derivation of our own. The first attempt
// here solved the paper's two requirements — an empty buffer picks the lowest
// rung, a full one the highest — directly against the ladder. That is correct
// arithmetic and a broken player: with only two rungs the two requirements pin
// the crossover at exactly the buffer cap, and finding it involves subtracting
// two numbers near 1.7e6 whose difference is near 6e-4, so the answer carried
// about six significant figures and the player never left the bottom rung at
// all. A two-rung ladder is not exotic here — it is what a stream with a hole
// in it looks like. Where an external authority exists, use it.
//
// The cost of taking dash.js's parameters is that an empty buffer does not
// always pick the lowest rung: which rung it picks depends on the ladder and
// the buffer target, and dash.js compensates with a separate throughput rule at
// startup that this version does not model (AB-35).
type BOLA struct {
	// MinBuffer is dash.js's MINIMUM_BUFFER_S: the buffer level the parameters
	// are anchored to, and below which the throughput estimate has the casting
	// vote — see the note on the safeguard below.
	MinBuffer float64

	// tp is the throughput estimate the safeguard consults. BOLA's own
	// decision ignores it, which is the algorithm's whole point; the safeguard
	// is what dash.js wraps around it before shipping it.
	tp Throughput
}

// NewBOLA builds the algorithm. It carries no state: every decision is a pure
// function of the ladder and the buffer, which is what makes a BOLA run
// reproducible without a Reset.
func NewBOLA() *BOLA { return &BOLA{} }

func (b *BOLA) Name() string { return "bola" }

// Observe feeds the low-buffer safeguard. BOLA proper does not look at
// throughput; this is dash.js's wrapper, not the paper's algorithm.
func (b *BOLA) Observe(bps float64) { b.tp.Observe(bps) }

func (b *BOLA) Reset() { b.tp.Reset() }

// dash.js's MINIMUM_BUFFER_S and MINIMUM_BUFFER_PER_BITRATE_LEVEL_S.
const (
	bolaMinBuffer   = 10.0
	bolaPerRungTerm = 2.0
)

func (b *BOLA) Pick(bitrates []int64, s State) int {
	last := len(bitrates) - 1
	if last <= 0 || s.BufferCap <= 0 || bitrates[0] <= 0 {
		return 0
	}

	minBuf := b.MinBuffer
	if minBuf <= 0 {
		minBuf = bolaMinBuffer
	}

	// Utilities, normalised so the lowest rung has utility zero.
	util := make([]float64, len(bitrates))
	for i, r := range bitrates {
		util[i] = math.Log(float64(r) / float64(bitrates[0]))
	}
	if util[last] <= 0 {
		return 0 // every rung is the same bitrate: nothing to choose between
	}

	bufferTime := math.Max(s.BufferCap, minBuf+bolaPerRungTerm*float64(len(bitrates)))
	gp := (util[last] - 1) / (bufferTime/minBuf - 1)
	if gp <= 0 {
		return 0
	}
	vp := minBuf / gp

	best, bestScore := 0, math.Inf(-1)
	for i, r := range bitrates {
		// Q is in seconds here, as in dash.js — not in segments. Ties go to the
		// higher rung, which is what makes a ladder of equal-utility rungs
		// resolve upwards rather than sitting at the bottom.
		if score := (vp*(util[i]+gp) - s.Buffer) / float64(r); score >= bestScore {
			best, bestScore = i, score
		}
	}

	// Below the minimum buffer, defer to the throughput estimate.
	//
	// Buffer level alone is not enough, and this is not a refinement: on a
	// two-rung ladder whose top rung is above the line, BOLA climbs past its
	// crossover, takes a segment it cannot afford, empties the buffer, drops
	// and climbs again — 59 stalls in 60 segments on an ordinary 3 Mbps cell.
	// dash.js does not ship BOLA on its own for exactly this reason. With no
	// estimate yet there is nothing to defer to, and treating that as "no
	// bandwidth" would pin the start of every session to the bottom rung.
	if s.Buffer < minBuf && b.tp.seen {
		if capped := b.tp.Pick(bitrates, s); capped < best {
			best = capped
		}
	}
	return best
}
