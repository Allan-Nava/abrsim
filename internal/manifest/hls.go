// Package manifest turns a published stream into the only two things a
// simulation needs: the ladder a player chooses between, and how big each
// segment of each rung is.
//
// It deliberately reads much less than a conformance checker would. Anything
// the player's *adaptation* cannot see — codec strings beyond identification,
// discontinuities, encryption — is not modelled here, because a number this
// tool prints has to be traceable to something a player actually reacts to.
package manifest

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Segment is one media segment as the player will request it.
type Segment struct {
	URI      string  `json:"uri"`
	Duration float64 `json:"duration"` // seconds, from EXTINF
	Bytes    int64   `json:"bytes"`
	// Measured distinguishes a size that was read — from a BYTERANGE, a
	// Content-Length — from one derived from the declared bitrate. Every
	// output that carries a byte count has to carry this with it: a simulation
	// reported as a measurement is the one way this tool can lie.
	Measured bool `json:"measured"`
}

// Rendition is one rung of the ladder.
type Rendition struct {
	Name      string  `json:"name"`
	Bandwidth int64   `json:"bandwidth"`         // BANDWIDTH: the declared peak
	Average   int64   `json:"average,omitempty"` // AVERAGE-BANDWIDTH, 0 when absent
	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	FrameRate float64 `json:"frame_rate,omitempty"`
	Codecs    string  `json:"codecs,omitempty"`
	URI       string  `json:"uri"`

	InitURI      string `json:"init_uri,omitempty"`
	InitBytes    int64  `json:"init_bytes,omitempty"`
	InitMeasured bool   `json:"init_measured,omitempty"`

	Segments []Segment `json:"segments"`
}

// SizeBitrate is the bitrate a segment size is estimated from: the declared
// average when there is one, the peak otherwise.
//
// BANDWIDTH is an upper bound by definition, so using it would make every
// simulation pessimistic by exactly the headroom the packager declared —
// a ladder would look worse for being honest about its peaks.
func (r Rendition) SizeBitrate() int64 {
	if r.Average > 0 {
		return r.Average
	}
	return r.Bandwidth
}

// Playlist is one media playlist: the segment list of a single rung.
type Playlist struct {
	Segments     []Segment
	InitURI      string
	InitBytes    int64
	InitMeasured bool
	TargetDur    float64
	Live         bool // no EXT-X-ENDLIST
}

// Ladder is what a player chooses between, sorted by bitrate ascending.
type Ladder struct {
	Source     string      `json:"source"`
	Renditions []Rendition `json:"renditions"`
}

// Rung is the compact description of a rendition, for reports: everything an
// operator needs to recognise it and nothing of the segment list, which would
// bury a JSON document under thousands of URIs nobody reads.
type Rung struct {
	Name      string `json:"name"`
	Bandwidth int64  `json:"bandwidth"`
	Average   int64  `json:"average,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Codecs    string `json:"codecs,omitempty"`
	Segments  int    `json:"segments"`
}

// Rungs summarises the ladder.
func (l Ladder) Rungs() []Rung {
	out := make([]Rung, 0, len(l.Renditions))
	for _, r := range l.Renditions {
		out = append(out, Rung{
			Name: r.Name, Bandwidth: r.Bandwidth, Average: r.Average,
			Width: r.Width, Height: r.Height, Codecs: r.Codecs,
			Segments: len(r.Segments),
		})
	}
	return out
}

// Duration is the presentation duration, taken from the first rendition: every
// rung of a well-formed ladder covers the same content.
func (l Ladder) Duration() float64 {
	if len(l.Renditions) == 0 {
		return 0
	}
	var total float64
	for _, s := range l.Renditions[0].Segments {
		total += s.Duration
	}
	return total
}

// FillDeclaredSizes gives every segment without a measured size an estimate
// from the rendition's declared bitrate, leaving Measured false.
func FillDeclaredSizes(r *Rendition) {
	bps := r.SizeBitrate()
	for i := range r.Segments {
		if r.Segments[i].Measured || r.Segments[i].Bytes > 0 {
			continue
		}
		r.Segments[i].Bytes = int64(float64(bps) * r.Segments[i].Duration / 8)
	}
}

// ParseMaster reads a master playlist into a ladder.
func ParseMaster(data []byte, base *url.URL) (Ladder, error) {
	lines := splitLines(data)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "#EXTM3U" {
		return Ladder{}, fmt.Errorf("not an HLS playlist: the first line is not #EXTM3U")
	}

	l := Ladder{Source: base.String()}
	var pending *Rendition

	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue

		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			attrs := parseAttributes(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			r := Rendition{
				Bandwidth: attrs.int("BANDWIDTH"),
				Average:   attrs.int("AVERAGE-BANDWIDTH"),
				Codecs:    attrs["CODECS"],
				FrameRate: attrs.float("FRAME-RATE"),
			}
			r.Width, r.Height = parseResolution(attrs["RESOLUTION"])
			if r.Bandwidth <= 0 {
				return Ladder{}, fmt.Errorf("a variant declares no BANDWIDTH: %s", line)
			}
			pending = &r

		// An I-frame rung is a trick-play track, not something a player adapts
		// into. Counting it would put a rung in the ladder no simulation can
		// ever choose and make every ladder-gap number wrong.
		case strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF:"):
			continue

		case strings.HasPrefix(line, "#"):
			continue

		default:
			if pending == nil {
				continue // a stray URI with no EXT-X-STREAM-INF above it
			}
			pending.URI = resolve(base, line)
			l.Renditions = append(l.Renditions, *pending)
			pending = nil
		}
	}
	if pending != nil {
		return Ladder{}, fmt.Errorf("the last #EXT-X-STREAM-INF has no URI after it")
	}
	if len(l.Renditions) == 0 {
		return Ladder{}, fmt.Errorf("no variants: this looks like a media playlist, not a master")
	}

	l.Renditions = dropAudioOnly(l.Renditions)
	sort.SliceStable(l.Renditions, func(i, j int) bool {
		return l.Renditions[i].Bandwidth < l.Renditions[j].Bandwidth
	})
	NameRenditions(l.Renditions)
	return l, nil
}

// videoCodecs are the sample-entry prefixes that mean there is a picture.
// Anything not on the list is not assumed to be audio — see hasVideo.
var videoCodecs = []string{
	"avc1", "avc3", "hvc1", "hev1", "hvt1", "vvc1", "vvi1",
	"vp8", "vp9", "vp09", "av01", "dvh1", "dvhe", "dva1", "dvav", "mp4v",
}

// hasVideo reports whether a CODECS attribute names a video codec.
//
// The (value, false) shape is the point: an empty or unrecognised CODECS list
// means *unknown*, and the caller keeps the variant. Dropping a rung on
// suspicion would silently shrink somebody's ladder, and a ladder-gap figure
// measured against a ladder we quietly edited is worse than no figure.
func hasVideo(codecs string) (bool, bool) {
	codecs = strings.TrimSpace(codecs)
	if codecs == "" {
		return false, false
	}
	known := false
	for _, c := range strings.Split(codecs, ",") {
		c = strings.ToLower(strings.TrimSpace(c))
		for _, v := range videoCodecs {
			if strings.HasPrefix(c, v) {
				return true, true
			}
		}
		// mp4a and the like are recognised audio: seeing one means the list is
		// intelligible, so a list of nothing but those is genuinely audio-only.
		if strings.HasPrefix(c, "mp4a") || strings.HasPrefix(c, "ac-3") ||
			strings.HasPrefix(c, "ec-3") || strings.HasPrefix(c, "ac-4") ||
			strings.HasPrefix(c, "opus") || strings.HasPrefix(c, "flac") ||
			strings.HasPrefix(c, "alac") || strings.HasPrefix(c, "dts") {
			known = true
		}
	}
	return false, known
}

// dropAudioOnly removes variants that carry sound and no picture.
//
// A player does not adapt between a picture and no picture, so an audio-only
// variant is not a rung — and Apple's own reference stream ships several
// alongside the video ladder. Left in, they become the bottom of the ladder and
// every startup and ladder-gap figure is measured from a 41 kbps step nothing
// would ever show. This was found by running against that stream, which is
// where this kind of error is always found.
//
// A presentation that is audio-only all the way through is kept whole: it is a
// real thing to be handed, and reporting "no variants" would be both wrong and
// unhelpful.
func dropAudioOnly(rs []Rendition) []Rendition {
	out := make([]Rendition, 0, len(rs))
	for _, r := range rs {
		video, known := hasVideo(r.Codecs)
		if !video && known && r.Height == 0 {
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return rs
	}
	return out
}

// NameRenditions gives every rung the shortest name that is still unique.
//
// A finding is filed under a rendition's name, so two rungs at one resolution
// sharing a name leave the operator guessing which half of the ladder to go and
// look at — the bitrate is what tells them apart, so that is what gets appended,
// and only where it is needed.
func NameRenditions(rs []Rendition) {
	count := map[string]int{}
	for i := range rs {
		count[shortName(rs[i])]++
	}
	for i := range rs {
		n := shortName(rs[i])
		if count[n] > 1 {
			n = fmt.Sprintf("%s@%dk", n, rs[i].Bandwidth/1000)
		}
		rs[i].Name = n
	}
}

func shortName(r Rendition) string {
	if r.Height > 0 {
		return fmt.Sprintf("%dp", r.Height)
	}
	return fmt.Sprintf("%dk", r.Bandwidth/1000)
}

// ParseMedia reads a media playlist into a segment list.
func ParseMedia(data []byte, base *url.URL) (Playlist, error) {
	lines := splitLines(data)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "#EXTM3U" {
		return Playlist{}, fmt.Errorf("not an HLS playlist: the first line is not #EXTM3U")
	}

	p := Playlist{Live: true}
	var dur float64
	var size int64
	var haveSize bool

	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue

		case strings.HasPrefix(line, "#EXTINF:"):
			v := strings.TrimPrefix(line, "#EXTINF:")
			if c := strings.IndexByte(v, ','); c >= 0 {
				v = v[:c]
			}
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return Playlist{}, fmt.Errorf("bad EXTINF %q: %w", line, err)
			}
			dur = f

		case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"):
			n, _, err := parseByterange(strings.TrimPrefix(line, "#EXT-X-BYTERANGE:"))
			if err != nil {
				return Playlist{}, fmt.Errorf("bad EXT-X-BYTERANGE %q: %w", line, err)
			}
			size, haveSize = n, true

		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			attrs := parseAttributes(strings.TrimPrefix(line, "#EXT-X-MAP:"))
			p.InitURI = resolve(base, attrs["URI"])
			if br := attrs["BYTERANGE"]; br != "" {
				if n, _, err := parseByterange(br); err == nil {
					p.InitBytes, p.InitMeasured = n, true
				}
			}

		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			p.TargetDur, _ = strconv.ParseFloat(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"), 64)

		case line == "#EXT-X-ENDLIST":
			p.Live = false

		case strings.HasPrefix(line, "#"):
			continue

		default:
			if dur <= 0 {
				continue // a URI with no EXTINF above it is not a media segment
			}
			p.Segments = append(p.Segments, Segment{
				URI:      resolve(base, line),
				Duration: dur,
				Bytes:    size,
				Measured: haveSize,
			})
			dur, size, haveSize = 0, 0, false
		}
	}
	if len(p.Segments) == 0 {
		return Playlist{}, fmt.Errorf("no segments in the media playlist")
	}
	return p, nil
}

// parseByterange reads `length[@offset]`.
func parseByterange(s string) (length, offset int64, err error) {
	s = strings.Trim(strings.TrimSpace(s), `"`)
	parts := strings.SplitN(s, "@", 2)
	length, err = strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, 0, err
	}
	if len(parts) == 2 {
		offset, err = strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	}
	return length, offset, err
}

func parseResolution(s string) (w, h int) {
	x := strings.IndexAny(s, "xX")
	if x < 0 {
		return 0, 0
	}
	w, _ = strconv.Atoi(strings.TrimSpace(s[:x]))
	h, _ = strconv.Atoi(strings.TrimSpace(s[x+1:]))
	return w, h
}

type attributes map[string]string

func (a attributes) int(k string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(a[k]), 10, 64)
	return n
}

func (a attributes) float(k string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(a[k]), 64)
	return f
}

// parseAttributes reads an HLS attribute list, respecting quoted values.
//
// The comma inside CODECS="avc1.640028,mp4a.40.2" is the trap: splitting the
// line on commas loses the audio codec and, worse, produces a key of
// `mp4a.40.2"` that a lenient parser then ignores in silence.
func parseAttributes(s string) attributes {
	out := attributes{}
	var key, val strings.Builder
	inKey, inQuote := true, false

	flush := func() {
		k := strings.TrimSpace(key.String())
		if k != "" {
			out[k] = strings.Trim(strings.TrimSpace(val.String()), `"`)
		}
		key.Reset()
		val.Reset()
		inKey = true
	}

	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			val.WriteRune(r)
		case r == '=' && inKey && !inQuote:
			inKey = false
		case r == ',' && !inQuote:
			flush()
		case inKey:
			key.WriteRune(r)
		default:
			val.WriteRune(r)
		}
	}
	flush()
	return out
}

func resolve(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || base == nil {
		return ref
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}

func splitLines(data []byte) []string {
	return strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
}
