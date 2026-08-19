package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ladderDir writes a small master and two media playlists to a temp directory
// so a whole run can be exercised without a network. The hole between 800k and
// 5000k is deliberate: this is the ladder the tool exists to complain about.
func ladderDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("master.m3u8", `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360
low.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080
high.m3u8
`)
	var media strings.Builder
	media.WriteString("#EXTM3U\n#EXT-X-TARGETDURATION:4\n")
	for i := 0; i < 60; i++ {
		media.WriteString("#EXTINF:4.000,\nseg.ts\n")
	}
	media.WriteString("#EXT-X-ENDLIST\n")
	write("low.m3u8", media.String())
	write("high.m3u8", media.String())
	return filepath.Join(dir, "master.m3u8")
}

func exec(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRun_FindsTheHoleInTheLadder(t *testing.T) {
	// mobile-4g averages around 3 Mbps: squarely inside the 800k..5000k hole,
	// which is where a missing rung shows up. flat-5m would not do — a line
	// exactly equal to the top rung leaves the player unable to sustain it, and
	// that is a different complaint (AB-32).
	code, out, errs := exec(t, "run", ladderDir(t), "--trace", "mobile-4g", "--abr", "throughput")
	if code != 0 {
		t.Fatalf("exit %d, want 0 — findings are output, not failure\nstderr: %s", code, errs)
	}
	if !strings.Contains(out, "ladder-gap") {
		t.Errorf("no ladder-gap finding on a ladder with a 6× hole:\n%s", out)
	}
	if !strings.Contains(out, "BAD") {
		t.Errorf("nothing above OK on a 5 Mbps line against a 800k/5000k ladder:\n%s", out)
	}
	if !strings.Contains(out, "declared") {
		t.Errorf("the output does not say the sizes were declared rather than measured:\n%s", out)
	}
}

func TestRun_ExitsZeroEvenWithFindings(t *testing.T) {
	// Exit 0 whenever the simulation ran. Only --exit-on changes that.
	if code, _, _ := exec(t, "run", ladderDir(t), "--trace", "train"); code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
}

func TestRun_ExitOn(t *testing.T) {
	code, out, _ := exec(t, "run", ladderDir(t), "--trace", "mobile-4g", "--exit-on", "bad")
	if code == 0 {
		t.Errorf("--exit-on bad exited 0 with a BAD finding present:\n%s", out)
	}
	if code, _, _ := exec(t, "run", ladderDir(t), "--trace", "mobile-4g", "--exit-on", "error"); code != 0 {
		t.Errorf("--exit-on error exited %d with no ERROR finding", code)
	}
}

func TestRun_JSONParsesAndCarriesTheTimeline(t *testing.T) {
	code, out, errs := exec(t, "run", ladderDir(t), "--trace", "flat-5m", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var doc struct {
		Source string `json:"source"`
		Run    struct {
			Requests []map[string]any `json:"requests"`
		} `json:"run"`
		Findings []map[string]any `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("the JSON we emit does not parse: %v\n%s", err, out)
	}
	if len(doc.Run.Requests) == 0 || len(doc.Findings) == 0 {
		t.Errorf("JSON is missing the timeline or the findings: %d requests, %d findings", len(doc.Run.Requests), len(doc.Findings))
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("JSON output carries ANSI escapes")
	}
}

func TestRun_UnknownTraceListsTheOnesThatExist(t *testing.T) {
	code, _, errs := exec(t, "run", ladderDir(t), "--trace", "no-such-thing")
	if code == 0 {
		t.Error("an unknown trace exited 0")
	}
	if !strings.Contains(errs, "mobile-4g") {
		t.Errorf("the error does not list the built-ins, so a typo is indistinguishable from a missing file: %s", errs)
	}
}

func TestRun_UnknownAlgorithmListsTheOnesThatExist(t *testing.T) {
	code, _, errs := exec(t, "run", ladderDir(t), "--abr", "psychic")
	if code == 0 {
		t.Error("an unknown algorithm exited 0")
	}
	if !strings.Contains(errs, "bola") {
		t.Errorf("the error does not list the algorithms: %s", errs)
	}
}

func TestTraces_AndAlgorithms_AreDocumented(t *testing.T) {
	code, out, _ := exec(t, "traces")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"mobile-4g", "train", "flat-5m"} {
		if !strings.Contains(out, want) {
			t.Errorf("`traces` does not list %q:\n%s", want, out)
		}
	}
	code, out, _ = exec(t, "algorithms")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"bola", "buffer", "throughput", "Spiteri"} {
		if !strings.Contains(out, want) {
			t.Errorf("`algorithms` does not mention %q:\n%s", want, out)
		}
	}
}

func TestUsage(t *testing.T) {
	if code, _, errs := exec(t); code == 0 || !strings.Contains(errs, "abrsim run") {
		t.Errorf("no arguments should print usage and exit non-zero: %d %q", code, errs)
	}
	code, out, _ := exec(t, "--help")
	if code != 0 {
		t.Errorf("--help exited %d", code)
	}
	// Every flag the README documents has to be here, or one of the two is a
	// bug report waiting to be filed.
	for _, want := range []string{"--trace", "--abr", "--sizes", "--startup-buffer", "--buffer-cap", "--exit-on", "--header", "--json"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help does not document %s:\n%s", want, out)
		}
	}
}

func TestRun_HeaderFlagWantsAColon(t *testing.T) {
	if code, _, errs := exec(t, "run", ladderDir(t), "--header", "Authorization Bearer x"); code == 0 {
		t.Errorf("a malformed --header was accepted: %s", errs)
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := exec(t, "version")
	if code != 0 || !strings.Contains(out, "abrsim") {
		t.Errorf("version = %d %q", code, out)
	}
}

// --- AB-36: an audience rather than a viewer ---------------------------------

func TestRun_ViewersReportsTheSpreadAndNamesTheWorstViewer(t *testing.T) {
	code, out, errs := exec(t, "run", ladderDir(t), "--trace", "steps-down", "--abr", "bola", "--viewers", "24")
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", code, errs)
	}
	for _, want := range []string{"24 viewers", "steps-down", "median", "viewers ("} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "worst at viewer") {
		t.Errorf("the report never says which viewer the worst finding came from:\n%s", out)
	}
}

func TestRun_OneViewerIsIdenticalToNotAskingForAPopulation(t *testing.T) {
	dir := ladderDir(t)
	_, plain, _ := exec(t, "run", dir, "--trace", "mobile-4g", "--abr", "bola", "--json")
	_, one, _ := exec(t, "run", dir, "--trace", "mobile-4g", "--abr", "bola", "--json", "--viewers", "1")
	if plain != one {
		t.Error("--viewers 1 produced a different document from no --viewers at all: the default has to stay the run this tool always did")
	}
}

func TestRun_ViewersJSONCarriesTheDistributionButNotEveryTimeline(t *testing.T) {
	code, out, errs := exec(t, "run", ladderDir(t), "--trace", "mobile-4g", "--viewers", "12", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var doc struct {
		Population struct {
			Viewers int `json:"viewers"`
			Startup struct {
				Min, Median, Max float64
			} `json:"startup"`
			Checks []struct {
				Check string `json:"check"`
				Loud  int    `json:"above_ok"`
			} `json:"checks"`
			Runs []struct {
				Index int `json:"index"`
			} `json:"runs"`
		} `json:"population"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("the population document does not parse: %v", err)
	}
	if doc.Population.Viewers != 12 || len(doc.Population.Runs) != 12 {
		t.Errorf("viewers = %d, runs = %d, want 12 of each", doc.Population.Viewers, len(doc.Population.Runs))
	}
	if len(doc.Population.Checks) != 7 {
		t.Errorf("%d checks, want 7 — every check speaks over an audience too", len(doc.Population.Checks))
	}
	if doc.Population.Startup.Max < doc.Population.Startup.Min {
		t.Errorf("startup spread is inside out: %+v", doc.Population.Startup)
	}
	if strings.Contains(out, "\"requests\"") {
		t.Error("twelve request timelines in one document: run a single viewer for the timeline")
	}
}

func TestRun_ViewersExitOnJudgesTheWholeAudience(t *testing.T) {
	// The ladder has a hole in it, so ladder-gap is BAD for somebody in any
	// population over a trace that walks the range.
	code, _, _ := exec(t, "run", ladderDir(t), "--trace", "steps-down", "--viewers", "16", "--exit-on", "bad")
	if code != 1 {
		t.Errorf("exit %d, want 1 — a gate that only read the median viewer would pass a ladder that freezes for one person in twenty", code)
	}
}

func TestRun_ViewersWantsAtLeastOneViewer(t *testing.T) {
	for _, n := range []string{"0", "-4"} {
		code, _, errs := exec(t, "run", ladderDir(t), "--viewers", n)
		if code != 2 {
			t.Errorf("--viewers %s: exit %d, want 2 (a usage error)", n, code)
		}
		if !strings.Contains(errs, "viewers") {
			t.Errorf("--viewers %s: stderr does not mention the flag: %s", n, errs)
		}
	}
}
