// Package device is the screen the viewer is watching on (AB-40).
//
// A phone cannot show the difference between the 1080p rung and the 720p one, so
// counting a phone viewer's playback towards "the top rung served 40% of the
// audience" overstates what that rung buys — and the top rung is usually the most
// expensive one on the ladder. The ceiling is where more pixels stop being worth
// their bytes on that class of screen.
//
// The mix is an **input and never a guess**. abrsim does not know who watches this
// stream, and inventing an audience is inventing a measurement: with no --devices
// the simulation is exactly what it was before, one ladder for everybody.
package device

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Spec is one class of screen.
type Spec struct {
	// Ceiling is the tallest rung worth fetching, in pixels. 0 means no cap.
	Ceiling  int
	Describe string
}

// The ceilings are deliberately conservative — they say "past here the viewer
// cannot see the difference", not "past here the device refuses to decode". A
// wrong ceiling in the kind direction over-counts the top rung, which is the
// error that flatters the ladder, so each one is a judgement stated in the open.
var classes = map[string]Spec{
	"phone":   {720, "a handset: past 720p the pixels are smaller than the eye can resolve at arm's length"},
	"tablet":  {1080, "a tablet held at reading distance"},
	"laptop":  {1440, "a laptop panel"},
	"desktop": {2160, "a desktop monitor, which may well be 4K"},
	"tv":      {0, "a television: the one screen that wants everything the ladder has"},
}

// Names lists the classes, sorted, because a map's order is not an order.
func Names() []string {
	out := make([]string, 0, len(classes))
	for n := range classes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Class looks one up.
func Class(name string) (Spec, bool) {
	c, ok := classes[name]
	return c, ok
}

// Share is one class's slice of the audience.
type Share struct {
	Name    string `json:"name"`
	Percent int    `json:"percent"`
}

// Mix is an audience: which screens, in what proportion.
type Mix struct {
	Shares []Share `json:"shares"`
}

// Parse reads `phone:50,tv:30,desktop:20`. The percentages have to add up to 100:
// an audience that adds up to 80 is an audience with a fifth of it unaccounted
// for, and quietly normalising it would put a number in the report that nobody
// asked for.
func Parse(spec string) (Mix, error) {
	var mix Mix
	total := 0
	if strings.TrimSpace(spec) == "" {
		return mix, fmt.Errorf("no device mix given")
	}
	for _, part := range strings.Split(spec, ",") {
		name, pct, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			return Mix{}, fmt.Errorf("want `class:percent`, got %q", part)
		}
		name = strings.TrimSpace(name)
		if _, known := classes[name]; !known {
			return Mix{}, fmt.Errorf("no device class %q — try one of: %s", name, strings.Join(Names(), ", "))
		}
		n, err := strconv.Atoi(strings.TrimSpace(pct))
		if err != nil {
			return Mix{}, fmt.Errorf("%q: %q is not a percentage", name, pct)
		}
		if n < 0 {
			return Mix{}, fmt.Errorf("%q: %d%% is not an audience", name, n)
		}
		for _, s := range mix.Shares {
			if s.Name == name {
				return Mix{}, fmt.Errorf("%q appears twice", name)
			}
		}
		mix.Shares = append(mix.Shares, Share{Name: name, Percent: n})
		total += n
	}
	if total != 100 {
		return Mix{}, fmt.Errorf("the shares add up to %d%%, not 100%% — abrsim will not invent the rest of the audience", total)
	}
	return mix, nil
}

// String is the mix as it was given, so a report can say what it was asked for
// rather than paraphrasing it.
func (m Mix) String() string {
	parts := make([]string, 0, len(m.Shares))
	for _, s := range m.Shares {
		parts = append(parts, fmt.Sprintf("%s:%d", s.Name, s.Percent))
	}
	return strings.Join(parts, ",")
}

// Assign hands a class to each of n viewers.
//
// Stratified, not sampled: 50% of forty viewers is twenty phones, exactly, because
// a share that only holds on average is a share nobody can check. Then permuted by
// the same fixed-seed hash the traces use, so the classes are not handed out in
// blocks — somebody looking at the first ten viewers of two hundred must not be
// looking at ten phones.
func (m Mix) Assign(n int) []string {
	if n <= 0 || len(m.Shares) == 0 {
		return nil
	}
	// Largest-remainder apportionment: hand out the floors, then the seats the
	// rounding left over, biggest remainder first. It is the same problem as
	// allocating parliamentary seats and it has the same honest answer.
	counts := make([]int, len(m.Shares))
	rem := make([]float64, len(m.Shares))
	assigned := 0
	for i, s := range m.Shares {
		exact := float64(s.Percent) * float64(n) / 100
		counts[i] = int(exact)
		rem[i] = exact - float64(counts[i])
		assigned += counts[i]
	}
	for assigned < n {
		best, bestRem := -1, -1.0
		for i := range counts {
			if rem[i] > bestRem {
				best, bestRem = i, rem[i]
			}
		}
		counts[best]++
		rem[best] = -1
		assigned++
	}

	seats := make([]string, 0, n)
	for i, s := range m.Shares {
		for j := 0; j < counts[i]; j++ {
			seats = append(seats, s.Name)
		}
	}
	order := make([]int, len(seats))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return shuffle(order[a]) < shuffle(order[b])
	})
	out := make([]string, len(seats))
	for pos, seat := range order {
		out[pos] = seats[seat]
	}
	return out
}

// shuffle is the same splitmix64 finalizer the built-in traces are generated with:
// a fixed-seed integer hash, so the permutation is the same on every machine and
// no clock or math/rand is involved.
func shuffle(i int) float64 {
	x := uint64(i)*0x9E3779B97F4A7C15 + 0xD1B54A32D192ED03
	x ^= x >> 30
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 27
	x *= 0x94D049BB133111EB
	x ^= x >> 31
	return float64(x>>11) / float64(uint64(1)<<53)
}
