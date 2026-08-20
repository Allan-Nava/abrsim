//go:build docker

// Run against a published image:
//
//	ABRSIM_IMAGE=ghcr.io/allan-nava/abrsim:0.4.1 go test -tags docker -run TestDockerImage ./internal/analyze/ -v
//
// The image answers to the same contract as the binary CI builds from source, so it
// is checked with the same kind of test rather than a different one: a real
// reference stream, the JSON document, and the seven checks all speaking. An image
// that ships a binary nobody ran is an archive with a registry in front of it.
//
// It skips without ABRSIM_IMAGE, so `go test -tags docker ./...` on a workstation
// with no image built is quiet rather than red.
package analyze

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

func TestDockerImage(t *testing.T) {
	image := os.Getenv("ABRSIM_IMAGE")
	if image == "" {
		t.Skip("set ABRSIM_IMAGE to the image to check")
	}
	const stream = "https://devstreaming-cdn.apple.com/videos/streaming/examples/bipbop_16x9/bipbop_16x9_variant.m3u8"

	out, err := exec.Command("docker", "run", "--rm", image,
		"run", stream, "--trace", "mobile-4g", "--play", "40s", "--json").Output()
	if err != nil {
		t.Fatalf("docker run %s: %v", image, err)
	}

	var doc struct {
		Source string `json:"source"`
		Run    struct {
			Requests []struct {
				Bytes int64 `json:"bytes"`
			} `json:"requests"`
		} `json:"run"`
		Findings []struct {
			Check  string `json:"check"`
			Status string `json:"status"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("the image's --json does not parse: %v\n%s", err, out)
	}
	if doc.Source != stream {
		t.Errorf("source = %q", doc.Source)
	}
	if len(doc.Run.Requests) == 0 {
		t.Error("no requests in the timeline: the image ran and simulated nothing")
	}
	if len(doc.Findings) != 7 {
		t.Errorf("%d findings, want 7 — every check speaks, in the image too", len(doc.Findings))
	}
	// A TLS failure inside the image looks like an ERROR finding rather than a
	// crash, and that is exactly the failure mode a scratch base would have given
	// us: the container missing CA certificates, reported as if the stream were
	// broken.
	for _, f := range doc.Findings {
		if f.Status == "ERROR" {
			t.Errorf("%s came back ERROR: %s", f.Check, out)
		}
	}
}
