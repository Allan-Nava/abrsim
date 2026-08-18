package manifest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// server publishes a two-rung ladder whose segments have real, known lengths.
func server(t *testing.T, heads *int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360
low/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720
high/index.m3u8
`)
	})
	for _, rung := range []string{"low", "high"} {
		mux.HandleFunc("/"+rung+"/index.m3u8", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.000,\nseg0.ts\n#EXTINF:4.000,\nseg1.ts\n#EXT-X-ENDLIST\n")
		})
	}
	mux.HandleFunc("/low/seg0.ts", segment(heads, 111_111))
	mux.HandleFunc("/low/seg1.ts", segment(heads, 222_222))
	mux.HandleFunc("/high/seg0.ts", segment(heads, 333_333))
	mux.HandleFunc("/high/seg1.ts", segment(heads, 444_444))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func segment(heads *int64, n int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			atomic.AddInt64(heads, 1)
		}
		w.Header().Set("Content-Length", fmt.Sprint(n))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			w.Write(make([]byte, n))
		}
	}
}

func TestLoad_ReadsTheWholeLadder(t *testing.T) {
	var heads int64
	srv := server(t, &heads)

	l, err := Load(context.Background(), srv.URL+"/master.m3u8", LoadOptions{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(l.Renditions) != 2 {
		t.Fatalf("got %d rungs, want 2", len(l.Renditions))
	}
	for _, r := range l.Renditions {
		if len(r.Segments) != 2 {
			t.Errorf("%s: %d segments, want 2", r.Name, len(r.Segments))
		}
		for i, s := range r.Segments {
			if s.Bytes <= 0 {
				t.Errorf("%s segment %d has no size at all", r.Name, i)
			}
			if s.Measured {
				t.Errorf("%s segment %d claims to be measured, but nothing was requested", r.Name, i)
			}
		}
	}
	if heads != 0 {
		t.Errorf("%d HEAD requests were sent without --sizes measured", heads)
	}
}

func TestLoad_MeasuredSizesAreTheRealOnes(t *testing.T) {
	var heads int64
	srv := server(t, &heads)

	l, err := Load(context.Background(), srv.URL+"/master.m3u8", LoadOptions{Measure: true})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := map[string][]int64{"360p": {111_111, 222_222}, "720p": {333_333, 444_444}}
	for _, r := range l.Renditions {
		for i, s := range r.Segments {
			if !s.Measured {
				t.Errorf("%s segment %d is still an estimate after --sizes measured", r.Name, i)
			}
			if s.Bytes != want[r.Name][i] {
				t.Errorf("%s segment %d = %d bytes, want %d", r.Name, i, s.Bytes, want[r.Name][i])
			}
		}
	}
	if heads != 4 {
		t.Errorf("%d HEAD requests, want one per segment", heads)
	}
}

func TestLoad_AMediaPlaylistIsNotALadder(t *testing.T) {
	// One rung is not something a player chooses between. Simulating it would
	// produce numbers about adaptation that never happened.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.000,\nseg0.ts\n#EXT-X-ENDLIST\n")
	}))
	defer srv.Close()

	_, err := Load(context.Background(), srv.URL+"/index.m3u8", LoadOptions{})
	if err == nil {
		t.Fatal("Load accepted a media playlist as a ladder")
	}
	if !strings.Contains(err.Error(), "master") {
		t.Errorf("the error does not say what to point at instead: %v", err)
	}
}

func TestLoad_SendsItsUserAgentAndHeaders(t *testing.T) {
	// A check has to be distinguishable from real traffic in an access log,
	// and credentials arrive as headers because a flag lands in shell history.
	var ua, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, auth = r.UserAgent(), r.Header.Get("Authorization")
		fmt.Fprint(w, "#EXTM3U\n")
	}))
	defer srv.Close()

	_, _ = Load(context.Background(), srv.URL+"/master.m3u8", LoadOptions{
		UserAgent: "abrsim/9.9.9",
		Headers:   map[string]string{"Authorization": "Bearer hunter2"},
	})
	if ua != "abrsim/9.9.9" {
		t.Errorf("User-Agent = %q", ua)
	}
	if auth != "Bearer hunter2" {
		t.Errorf("Authorization = %q", auth)
	}
}

func TestLoad_ServerErrorSaysWhichRequestFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := Load(context.Background(), srv.URL+"/master.m3u8", LoadOptions{})
	if err == nil {
		t.Fatal("Load ignored a 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("the error does not carry the status: %v", err)
	}
}

func TestLoad_AFailedHeadLeavesTheSizeDeclared(t *testing.T) {
	// A limit of the tool is not a defect in the stream: a segment whose size
	// could not be read falls back to the estimate and says so, rather than
	// failing the whole run.
	mux := http.NewServeMux()
	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nindex.m3u8\n")
	})
	mux.HandleFunc("/index.m3u8", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXTINF:4.000,\nseg0.ts\n#EXT-X-ENDLIST\n")
	})
	mux.HandleFunc("/seg0.ts", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	l, err := Load(context.Background(), srv.URL+"/master.m3u8", LoadOptions{Measure: true})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := l.Renditions[0].Segments[0]
	if s.Measured {
		t.Error("a segment whose HEAD returned 410 is marked measured")
	}
	if s.Bytes <= 0 {
		t.Error("the fallback estimate was not applied")
	}
}
