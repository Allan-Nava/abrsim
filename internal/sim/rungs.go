package sim

// What each rung actually served, and what it cost to ship (AB-38, AB-39).
//
// `ladder-gap` names a hole: a rung that should exist and does not. These are the
// other half of the same argument — which of the rungs that *do* exist earned
// their place. A rung nothing ever selects costs encoding, storage and egress and
// buys no viewer anything; a rung carrying most of the playback is the one nobody
// should touch. Neither is a judgement this package makes: both are counted off
// the timeline the simulator already emits, and what a gigabyte is worth is a
// commercial question abrsim does not know the answer to.

// Use is one rung's share of a session.
type Use struct {
	Rung     int     `json:"rung"`
	Name     string  `json:"name"`
	Bitrate  int64   `json:"bitrate"`
	Segments int     `json:"segments"`
	Seconds  float64 `json:"seconds"` // of media played at this rung
	Share    float64 `json:"share"`   // of the session's media, 0..1
	Bytes    int64   `json:"bytes"`   // that crossed the wire for it
}

// RungUse attributes the session to its rungs, one entry per rung on the ladder
// **including the ones nothing chose** — leaving those out would hide exactly the
// rungs worth arguing about.
func (r Result) RungUse() []Use {
	out := make([]Use, len(r.Bitrates))
	for i := range out {
		out[i] = Use{Rung: i, Bitrate: r.Bitrates[i]}
		if i < len(r.Names) {
			out[i].Name = r.Names[i]
		}
	}
	for _, q := range r.Requests {
		if q.Rung < 0 || q.Rung >= len(out) {
			continue
		}
		out[q.Rung].Segments++
		out[q.Rung].Seconds += q.Duration
		out[q.Rung].Bytes += q.Bytes
	}
	if r.Media > 0 {
		for i := range out {
			out[i].Share = out[i].Seconds / r.Media
		}
	}
	return out
}

// BytesPerViewerHour is what an hour of one viewer's watching costs to deliver,
// from the bytes that really crossed the wire. (0, false) when nothing was
// watched: an egress rate for a session with no media is a division nobody asked
// for.
//
// Not a severity, and not a verdict. Paired with RungUse it makes dropping a rung
// a number in both directions — seconds of rebuffer added against gigabytes saved
// — and which of those matters more is the operator's contract, not ours.
func (r Result) BytesPerViewerHour() (float64, bool) {
	if r.Media <= 0 {
		return 0, false
	}
	return float64(r.Bytes) * 3600 / r.Media, true
}
