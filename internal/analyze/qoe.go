package analyze

import (
	"math"

	"github.com/Allan-Nava/abrsim/internal/sim"
)

// One number for a session (AB-41).
//
// This is the linear QoE the published ABR literature optimises against — rung
// utility, minus a rebuffer penalty, minus a penalty for switching — in the
// parametrisation Pensieve uses, so an abrsim run is comparable with a published
// result instead of only with itself.
//
// Everything is divided by the seconds of playback, which makes the score
// readable rather than merely ordinal: **a QoE of 2.4 means the session was worth
// as much as watching a steady 2.4 Mbps with no stalls and no switching.** A score
// nobody can say a sentence about is a score nobody can act on.
//
// It carries no severity. The weights are a judgement somebody is entitled to
// disagree with, so they travel with the score everywhere it is printed, and no
// threshold is attached to it until AB-16 shows our numbers land where the papers'
// do. A grade nobody can defend is worse than a measurement with no opinion.

// QoEWeights are the two prices in the score, both in Mbps-equivalent.
type QoEWeights struct {
	// Rebuffer is charged per second of frozen picture. Pensieve's 4.3 says one
	// second of freezing costs as much as losing 4.3 Mbps of picture quality for
	// a second — which is why a stall dominates any bitrate gain that caused it.
	Rebuffer float64 `json:"rebuffer_per_second"`
	// Switch is charged per Mbps of rung change, in either direction: climbing
	// is visible too.
	Switch float64 `json:"switch_per_mbps"`
}

// DefaultQoEWeights are the literature's, not ours. Changing them silently would
// make every comparison with a published result wrong.
func DefaultQoEWeights() QoEWeights {
	return QoEWeights{Rebuffer: 4.3, Switch: 1.0}
}

// QoE scores one session. (0, false) when nothing was watched: a score for a
// session with no playback is a division nobody asked for.
func QoE(res sim.Result, w QoEWeights) (float64, bool) {
	if res.Media <= 0 || len(res.Requests) == 0 {
		return 0, false
	}
	mbps := func(rung int) float64 {
		if rung < 0 || rung >= len(res.Bitrates) {
			return 0
		}
		return float64(res.Bitrates[rung]) / 1e6
	}

	var utility, switching, stalls float64
	prev := -1
	for _, q := range res.Requests {
		utility += mbps(q.Rung) * q.Duration
		stalls += q.Stall
		if prev >= 0 {
			switching += math.Abs(mbps(q.Rung) - mbps(prev))
		}
		prev = q.Rung
	}
	// The stall total from the timeline, but fall back to the result's own figure
	// so a caller that built a Result by hand still gets the penalty it wrote.
	if res.StallTime > stalls {
		stalls = res.StallTime
	}
	return (utility - w.Rebuffer*stalls - w.Switch*switching) / res.Media, true
}
