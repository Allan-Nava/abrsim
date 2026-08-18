//go:build smoke

// Run with:
//
//	go build -o /tmp/abrsim ./cmd/abrsim
//	ABRSIM_BIN=/tmp/abrsim go test -tags smoke -run TestSmokeReferenceStreams ./internal/analyze/ -v
//
// Every design error this project has had was found here rather than by a unit
// test: audio-only variants sitting at the bottom of the ladder, and an
// efficiency ratio built on declared bitrates that read 129%. A round trip
// against our own builders cannot catch a shared misreading.
//
// The assertion is not "nothing above OK" — a reference stream is entitled to
// stall on a trace that goes dark. It is a per-stream baseline of the checks
// allowed to exceed OK, plus the list of checks that must not fall silent. A
// finding outside the baseline is a regression; so is a check that stops
// speaking, and that is the half which catches a parser that quietly stopped
// reading.
package analyze

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

type stream struct {
	name  string
	url   string
	trace string
	abr   string
	// allowed are the checks permitted to exceed OK on this stream.
	allowed map[string]bool
}

var streams = []stream{
	{
		name:  "apple-ts-bipbop",
		url:   "https://devstreaming-cdn.apple.com/videos/streaming/examples/bipbop_16x9/bipbop_16x9_variant.m3u8",
		trace: "mobile-4g", abr: "bola",
		allowed: map[string]bool{"rebuffer": true},
	},
	{
		name:  "apple-fmp4-hevc",
		url:   "https://devstreaming-cdn.apple.com/videos/streaming/examples/bipbop_adv_example_hevc/master.m3u8",
		trace: "mobile-4g", abr: "bola",
		// The advanced example ships a wide ladder and over-declares BANDWIDTH;
		// on a collapsing cell it legitimately stalls and switches a lot.
		allowed: map[string]bool{"rebuffer": true, "switches": true},
	},
	{
		name:  "apple-ts-bipbop-clean-line",
		url:   "https://devstreaming-cdn.apple.com/videos/streaming/examples/bipbop_16x9/bipbop_16x9_variant.m3u8",
		trace: "flat-5m", abr: "bola",
		// A flat line well above the top rung: nothing at all is allowed to
		// exceed OK, which is the false-positive guard.
		allowed: map[string]bool{},
	},
}

// mustSpeak lists the checks that have to appear on every run. A check that
// falls silent is a hole nobody notices, and silence is what a broken parser
// looks like from the outside.
var mustSpeak = []string{"rebuffer", "startup", "switches", "efficiency", "ladder-gap", "sizes", "coverage"}

func TestSmokeReferenceStreams(t *testing.T) {
	bin := os.Getenv("ABRSIM_BIN")
	if bin == "" {
		t.Skip("set ABRSIM_BIN to the built binary")
	}

	for _, s := range streams {
		t.Run(s.name, func(t *testing.T) {
			out, err := exec.Command(bin, "run", s.url,
				"--trace", s.trace, "--abr", s.abr, "--play", "3m", "--json").Output()
			if err != nil {
				t.Fatalf("abrsim: %v", err)
			}

			var doc struct {
				Findings []struct {
					Check   string `json:"check"`
					Status  string `json:"status"`
					Target  string `json:"target"`
					Message string `json:"message"`
				} `json:"findings"`
				Run struct {
					Requests []struct {
						Rung int `json:"rung"`
					} `json:"requests"`
					Started bool `json:"started"`
				} `json:"run"`
				Ladder []struct {
					Name   string `json:"name"`
					Height int    `json:"height"`
				} `json:"ladder"`
			}
			if err := json.Unmarshal(out, &doc); err != nil {
				t.Fatalf("output does not parse: %v", err)
			}

			seen := map[string]bool{}
			for _, f := range doc.Findings {
				seen[f.Check] = true
				if f.Status != "OK" && !s.allowed[f.Check] {
					t.Errorf("new finding outside the baseline: %s %s %s — %s", f.Status, f.Check, f.Target, f.Message)
				}
			}
			for _, check := range mustSpeak {
				if !seen[check] {
					t.Errorf("%s fell silent", check)
				}
			}

			if !doc.Run.Started {
				t.Error("playback never started")
			}
			if len(doc.Run.Requests) == 0 {
				t.Fatal("no segments were fetched")
			}
			// Every rung of a video ladder has a picture. Apple's stream ships
			// audio-only variants next to it, and letting one into the ladder
			// puts a 41 kbps step at the bottom that every startup and
			// ladder-gap figure is then measured from.
			for _, r := range doc.Ladder {
				if r.Height == 0 {
					t.Errorf("rung %q has no resolution: an audio-only variant is in the video ladder", r.Name)
				}
			}
		})
	}
}
