package manifest

import (
	"math"
	"net/url"
	"strings"
	"testing"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("url %q: %v", s, err)
	}
	return u
}

const master = `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-INDEPENDENT-SEGMENTS

#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aac",NAME="English",DEFAULT=YES,URI="audio/en.m3u8"

#EXT-X-STREAM-INF:BANDWIDTH=5200000,AVERAGE-BANDWIDTH=4500000,RESOLUTION=1920x1080,FRAME-RATE=25.000,CODECS="avc1.640028,mp4a.40.2",AUDIO="aac"
v4/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360,CODECS="avc1.4d401e,mp4a.40.2",AUDIO="aac"
https://other.example/v1/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2400000,AVERAGE-BANDWIDTH=2100000,RESOLUTION=1280x720,CODECS="avc1.4d401f,mp4a.40.2",AUDIO="aac"
v3/index.m3u8

#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=180000,RESOLUTION=1280x720,CODECS="avc1.4d401f",URI="iframe/index.m3u8"
`

func TestParseMaster_SortsAscendingAndReadsAttributes(t *testing.T) {
	l, err := ParseMaster([]byte(master), mustURL(t, "https://cdn.example/hls/master.m3u8"))
	if err != nil {
		t.Fatalf("ParseMaster: %v", err)
	}

	// Three variants. The I-frame rung is not one of them: a player never
	// adapts into a trick-play track, so counting it would put a rung in the
	// ladder that no simulation can ever choose.
	if len(l.Renditions) != 3 {
		t.Fatalf("got %d renditions, want 3: %+v", len(l.Renditions), l.Renditions)
	}

	wantBW := []int64{800_000, 2_400_000, 5_200_000}
	for i, w := range wantBW {
		if l.Renditions[i].Bandwidth != w {
			t.Errorf("rendition %d bandwidth = %d, want %d (ladder must sort ascending)", i, l.Renditions[i].Bandwidth, w)
		}
	}

	top := l.Renditions[2]
	if top.Width != 1920 || top.Height != 1080 {
		t.Errorf("top resolution = %dx%d, want 1920x1080", top.Width, top.Height)
	}
	if top.Average != 4_500_000 {
		t.Errorf("top AVERAGE-BANDWIDTH = %d, want 4500000", top.Average)
	}
	if top.FrameRate != 25 {
		t.Errorf("top FRAME-RATE = %v, want 25", top.FrameRate)
	}
	// The comma inside the quoted CODECS list is the classic attribute-list
	// trap: split on it and the ladder silently loses its audio codec.
	if top.Codecs != "avc1.640028,mp4a.40.2" {
		t.Errorf("top CODECS = %q, want the whole quoted list", top.Codecs)
	}
	if top.URI != "https://cdn.example/hls/v4/index.m3u8" {
		t.Errorf("top URI = %q, want it resolved against the master", top.URI)
	}
	if l.Renditions[0].URI != "https://other.example/v1/index.m3u8" {
		t.Errorf("absolute variant URI = %q, want it left alone", l.Renditions[0].URI)
	}
	if l.Renditions[0].Name != "360p" || top.Name != "1080p" {
		t.Errorf("names = %q..%q, want 360p..1080p", l.Renditions[0].Name, top.Name)
	}
}

func TestParseMaster_TwoRungsAtOneResolutionGetTwoNames(t *testing.T) {
	// A finding is filed under a rendition's name, so a ladder with two 720p
	// rungs and one name for both leaves the operator guessing which half of
	// the ladder to go and look at.
	in := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720
a.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=3600000,RESOLUTION=1280x720
b.m3u8
`
	l, err := ParseMaster([]byte(in), mustURL(t, "https://cdn.example/master.m3u8"))
	if err != nil {
		t.Fatalf("ParseMaster: %v", err)
	}
	if l.Renditions[0].Name == l.Renditions[1].Name {
		t.Fatalf("both rungs are called %q", l.Renditions[0].Name)
	}
	for _, r := range l.Renditions {
		if !strings.HasPrefix(r.Name, "720p") {
			t.Errorf("name %q no longer says what resolution it is", r.Name)
		}
	}
}

func TestParseMaster_NoResolutionFallsBackToBitrate(t *testing.T) {
	in := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1500000\nonly.m3u8\n"
	l, err := ParseMaster([]byte(in), mustURL(t, "https://cdn.example/master.m3u8"))
	if err != nil {
		t.Fatalf("ParseMaster: %v", err)
	}
	if got := l.Renditions[0].Name; got != "1500k" {
		t.Errorf("name = %q, want %q", got, "1500k")
	}
}

func TestParseMaster_Rejects(t *testing.T) {
	for name, in := range map[string]string{
		"not a playlist":         "hello\n",
		"no variants":            "#EXTM3U\n#EXT-X-VERSION:6\n",
		"stream-inf with no URI": "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\n",
		"no bandwidth":           "#EXTM3U\n#EXT-X-STREAM-INF:RESOLUTION=1280x720\na.m3u8\n",
	} {
		t.Run(name, func(t *testing.T) {
			if l, err := ParseMaster([]byte(in), mustURL(t, "https://x/y.m3u8")); err == nil {
				t.Fatalf("accepted %q: %+v", in, l)
			}
		})
	}
}

const media = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-MAP:URI="init.mp4",BYTERANGE="1024@0"
#EXTINF:6.000,
seg1.m4s
#EXTINF:6.000,
seg2.m4s
#EXTINF:3.480,
seg3.m4s
#EXT-X-ENDLIST
`

func TestParseMedia_DurationsAndInit(t *testing.T) {
	p, err := ParseMedia([]byte(media), mustURL(t, "https://cdn.example/hls/v4/index.m3u8"))
	if err != nil {
		t.Fatalf("ParseMedia: %v", err)
	}
	if len(p.Segments) != 3 {
		t.Fatalf("got %d segments, want 3", len(p.Segments))
	}
	if p.Segments[2].Duration != 3.48 {
		t.Errorf("last EXTINF = %v, want 3.48", p.Segments[2].Duration)
	}
	if p.Segments[0].URI != "https://cdn.example/hls/v4/seg1.m4s" {
		t.Errorf("segment URI = %q, want it resolved", p.Segments[0].URI)
	}
	// The init segment is downloaded before the first media segment and is
	// therefore part of the time to first frame — the one number a startup
	// check exists to report.
	if p.InitURI != "https://cdn.example/hls/v4/init.mp4" {
		t.Errorf("InitURI = %q", p.InitURI)
	}
	if p.InitBytes != 1024 || !p.InitMeasured {
		t.Errorf("init size = %d (measured %v), want 1024 from the BYTERANGE", p.InitBytes, p.InitMeasured)
	}
}

func TestParseMedia_ByterangeSizesAreMeasured(t *testing.T) {
	in := `#EXTM3U
#EXT-X-TARGETDURATION:6
#EXTINF:6.000,
#EXT-X-BYTERANGE:1200000@0
media.ts
#EXTINF:6.000,
#EXT-X-BYTERANGE:900000
media.ts
#EXT-X-ENDLIST
`
	p, err := ParseMedia([]byte(in), mustURL(t, "https://cdn.example/i.m3u8"))
	if err != nil {
		t.Fatalf("ParseMedia: %v", err)
	}
	for i, want := range []int64{1_200_000, 900_000} {
		s := p.Segments[i]
		if s.Bytes != want || !s.Measured {
			t.Errorf("segment %d = %d bytes (measured %v), want %d measured", i, s.Bytes, s.Measured, want)
		}
	}
}

func TestFillDeclaredSizes_UsesAverageBandwidthWhenThereIsOne(t *testing.T) {
	// BANDWIDTH is a peak and an upper bound; using it as a segment size makes
	// every simulation pessimistic by whatever headroom the encoder declared.
	// AVERAGE-BANDWIDTH is the estimator, and either way the size is an
	// estimate and has to travel as one.
	r := &Rendition{Bandwidth: 5_200_000, Average: 4_000_000}
	r.Segments = []Segment{{Duration: 6}, {Duration: 3}}
	FillDeclaredSizes(r)

	if got, want := r.Segments[0].Bytes, int64(4_000_000*6/8); got != want {
		t.Errorf("declared size = %d, want %d", got, want)
	}
	if got, want := r.Segments[1].Bytes, int64(4_000_000*3/8); got != want {
		t.Errorf("declared size of the short segment = %d, want %d", got, want)
	}
	for i, s := range r.Segments {
		if s.Measured {
			t.Errorf("segment %d claims to be measured, but nothing was downloaded", i)
		}
	}
}

func TestFillDeclaredSizes_LeavesMeasuredSegmentsAlone(t *testing.T) {
	r := &Rendition{Bandwidth: 5_000_000}
	r.Segments = []Segment{{Duration: 6, Bytes: 123, Measured: true}, {Duration: 6}}
	FillDeclaredSizes(r)

	if r.Segments[0].Bytes != 123 {
		t.Errorf("a measured size was overwritten with an estimate: %d", r.Segments[0].Bytes)
	}
	if r.Segments[1].Bytes == 0 {
		t.Error("the unmeasured segment was left with no size at all")
	}
}

func TestLadder_Duration(t *testing.T) {
	l := Ladder{Renditions: []Rendition{{Segments: []Segment{{Duration: 6}, {Duration: 6}, {Duration: 3.5}}}}}
	if got := l.Duration(); math.Abs(got-15.5) > 1e-9 {
		t.Errorf("Duration = %v, want 15.5", got)
	}
}

func TestParseMaster_AudioOnlyVariantsAreNotRungs(t *testing.T) {
	// Apple's own reference stream carries audio-only variants alongside the
	// video ladder. They are not rungs: a player does not adapt between a
	// picture and no picture, and counting them puts a 41 kbps step at the
	// bottom that every startup and ladder-gap figure is then measured from.
	//
	// Found by running against the real stream, which is where this kind of
	// error is always found.
	in := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=41000,CODECS="mp4a.40.2"
audio-only.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=640x360,CODECS="avc1.4d401e,mp4a.40.2"
low.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080,CODECS="hvc1.2.4.L123.B0,ec-3"
high.m3u8
`
	l, err := ParseMaster([]byte(in), mustURL(t, "https://cdn.example/master.m3u8"))
	if err != nil {
		t.Fatalf("ParseMaster: %v", err)
	}
	if len(l.Renditions) != 2 {
		t.Fatalf("got %d rungs, want 2: %+v", len(l.Renditions), l.Renditions)
	}
	if l.Renditions[0].Bandwidth != 900_000 {
		t.Errorf("the bottom rung is %d bps, want the 900k video variant", l.Renditions[0].Bandwidth)
	}
	// HEVC with E-AC-3 is a video variant; a codec list this parser does not
	// recognise must never be dropped on suspicion.
	if l.Renditions[1].Bandwidth != 5_000_000 {
		t.Errorf("the HEVC rung was dropped: %+v", l.Renditions)
	}
}

func TestParseMaster_AVariantWithNoCodecsIsKept(t *testing.T) {
	// Nothing states what is in it, so nothing can be concluded. Dropping it
	// would be inventing a fact; keeping it is the only honest option.
	in := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=900000\na.m3u8\n"
	l, err := ParseMaster([]byte(in), mustURL(t, "https://cdn.example/master.m3u8"))
	if err != nil {
		t.Fatalf("ParseMaster: %v", err)
	}
	if len(l.Renditions) != 1 {
		t.Fatalf("a variant declaring no CODECS was dropped")
	}
}

func TestParseMaster_AnAudioOnlyLadderIsNotEmpty(t *testing.T) {
	// An audio-only presentation is a real thing to be handed. Filtering every
	// variant away would report "no variants", which is both wrong and
	// unhelpful — the ladder is what it is.
	in := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=64000,CODECS="mp4a.40.2"
a.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=128000,CODECS="mp4a.40.2"
b.m3u8
`
	l, err := ParseMaster([]byte(in), mustURL(t, "https://cdn.example/master.m3u8"))
	if err != nil {
		t.Fatalf("ParseMaster: %v", err)
	}
	if len(l.Renditions) != 2 {
		t.Fatalf("got %d rungs, want both audio rungs kept", len(l.Renditions))
	}
}
