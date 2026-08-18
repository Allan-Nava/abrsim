package trace

import (
	"fmt"
	"sort"
	"strings"
)

// The built-in traces exist so somebody can get an answer before they own a
// measurement. They are *generated* — a recording checked into the repository
// would be a binary fixture with a .csv extension, unregenerable and unarguable.
//
// The variation inside them comes from wobble(), a fixed-seed integer hash, so
// two runs on two machines on two Go versions produce the same bytes. Nothing
// here may call math/rand or a clock.

type builtin struct {
	desc string
	gen  func() []Sample
}

var builtins = map[string]builtin{
	"flat-5m": {
		desc: "5 Mbps, unwavering, for five minutes — the control: any finding here is the ladder's, not the network's",
		gen:  func() []Sample { return []Sample{{0, 5e6}, {300, 5e6}} },
	},
	"steps-down": {
		desc: "a staircase from 10 Mbps to 300 kbps, thirty seconds a step — makes the player visit every rung it has",
		gen: func() []Sample {
			rates := []float64{10e6, 6e6, 4e6, 2.5e6, 1.5e6, 900e3, 500e3, 300e3}
			out := make([]Sample, 0, len(rates))
			for i, r := range rates {
				out = append(out, Sample{At: float64(i * 30), BPS: r})
			}
			return out
		},
	},
	"steps-up": {
		desc: "the staircase in reverse — shows how long a player takes to climb back after the network recovers",
		gen: func() []Sample {
			rates := []float64{300e3, 500e3, 900e3, 1.5e6, 2.5e6, 4e6, 6e6, 10e6}
			out := make([]Sample, 0, len(rates))
			for i, r := range rates {
				out = append(out, Sample{At: float64(i * 30), BPS: r})
			}
			return out
		},
	},
	"mobile-4g": {
		desc: "a 4G cell that averages ~3 Mbps and collapses to 400 kbps twice — the everyday case",
		gen: func() []Sample {
			out := make([]Sample, 0, 152)
			for i := 0; i < 150; i++ {
				at := float64(i) * 2
				base := 3.0e6
				switch {
				case i >= 40 && i < 55: // the first cell edge
					base = 500e3
				case i >= 95 && i < 108: // the second, in a busier cell
					base = 400e3
				}
				out = append(out, Sample{At: at, BPS: round(base * (0.75 + 0.5*wobble(i)))})
			}
			return out
		},
	},
	"train": {
		desc: "an intercity line: ~6 Mbps between tunnels, nothing at all inside them — the trace no adaptation can save",
		gen: func() []Sample {
			// Tunnels as [enter, leave) second pairs. Blackouts of 4, 9 and 6
			// seconds: the middle one outlasts any buffer a default player holds,
			// which is what makes this trace tell a buffer policy from a bitrate
			// policy rather than just ranking them.
			tunnels := [][2]float64{{38, 42}, {96, 105}, {171, 177}, {244, 248}}
			rate := func(i int) float64 { return round(6e6 * (0.7 + 0.6*wobble(i))) }

			out := make([]Sample, 0, 128)
			at, tn, i := 0.0, 0, 0
			for at < 300 {
				if tn < len(tunnels) && at >= tunnels[tn][0] {
					out = append(out, Sample{At: tunnels[tn][0], BPS: 0})
					at = tunnels[tn][1]
					tn++
					out = append(out, Sample{At: at, BPS: rate(i)})
					i++
					at += 3
					continue
				}
				out = append(out, Sample{At: at, BPS: rate(i)})
				i++
				at += 3
			}
			return out
		},
	},
	"office-wifi": {
		desc: "20 Mbps with somebody else's backup running: brief collapses to 1 Mbps, otherwise plenty",
		gen: func() []Sample {
			out := make([]Sample, 0, 120)
			for i := 0; i < 120; i++ {
				at := float64(i) * 2.5
				base := 20e6
				if i%17 == 0 || i%17 == 1 {
					base = 1e6
				}
				out = append(out, Sample{At: at, BPS: round(base * (0.85 + 0.3*wobble(i)))})
			}
			return out
		},
	},
	"dsl-evening": {
		desc: "a copper line degrading from 8 Mbps to 2.2 Mbps as the neighbourhood comes home, then recovering",
		gen: func() []Sample {
			out := make([]Sample, 0, 100)
			for i := 0; i < 100; i++ {
				at := float64(i) * 3
				// A shallow V: worst in the middle, back up by the end.
				x := float64(i)/99*2 - 1 // -1 .. 1
				base := 2.2e6 + 5.8e6*x*x
				out = append(out, Sample{At: at, BPS: round(base * (0.92 + 0.16*wobble(i)))})
			}
			return out
		},
	},
}

// wobble is a deterministic value in [0,1) derived from i alone: a fixed-seed
// integer hash (splitmix64's finaliser), so the built-ins are byte-identical
// on every machine and every Go release. math/rand is not stable enough a
// promise to hang reproducibility on.
func wobble(i int) float64 {
	x := uint64(i)*0x9E3779B97F4A7C15 + 0xD1B54A32D192ED03
	x ^= x >> 30
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 27
	x *= 0x94D049BB133111EB
	x ^= x >> 31
	return float64(x>>11) / float64(uint64(1)<<53)
}

// round snaps a rate to whole kilobits: a bandwidth reading carrying nine
// significant figures pretends to a precision no measurement has.
func round(bps float64) float64 {
	return float64(int64(bps/1000+0.5)) * 1000
}

// Names lists the built-in traces, sorted.
func Names() []string {
	out := make([]string, 0, len(builtins))
	for name := range builtins {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Describe is the one-line explanation of a built-in trace, or "" if there is
// no such trace.
func Describe(name string) string { return builtins[name].desc }

// Builtin returns the named built-in trace.
func Builtin(name string) (Trace, bool) {
	b, ok := builtins[name]
	if !ok {
		return Trace{}, false
	}
	return Trace{Name: name, Samples: b.gen()}, true
}

// ErrUnknown is the error a caller should show for a name that is neither a
// built-in nor a readable file: it lists what does exist, because a typo in a
// trace name is otherwise indistinguishable from a missing file.
func ErrUnknown(name string) error {
	return fmt.Errorf("no trace named %q — built-ins are: %s (or give a path to a CSV)",
		name, strings.Join(Names(), ", "))
}
