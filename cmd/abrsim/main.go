// Command abrsim simulates what an ABR player does with a ladder over a
// network, and reports what it cost the viewer.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Allan-Nava/abrsim/internal/abr"
	"github.com/Allan-Nava/abrsim/internal/analyze"
	"github.com/Allan-Nava/abrsim/internal/finding"
	"github.com/Allan-Nava/abrsim/internal/manifest"
	"github.com/Allan-Nava/abrsim/internal/output"
	"github.com/Allan-Nava/abrsim/internal/population"
	"github.com/Allan-Nava/abrsim/internal/sim"
	"github.com/Allan-Nava/abrsim/internal/trace"
)

// version is stamped by the release build.
var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run is main with its edges injected, so the exit codes and the output are
// testable without a subprocess.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	switch args[0] {
	case "run":
		return cmdRun(args[1:], stdout, stderr)
	case "traces":
		return cmdTraces(stdout)
	case "algorithms":
		return cmdAlgorithms(stdout)
	case "version", "--version", "-version":
		fmt.Fprintf(stdout, "abrsim %s\n", version)
		return 0
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	}
	fmt.Fprintf(stderr, "abrsim: unknown command %q\n\n", args[0])
	usage(stderr)
	return 2
}

func usage(w io.Writer) {
	fmt.Fprint(w, `abrsim — what your encoding ladder does to a viewer on a real network.

  abrsim run <master.m3u8> [flags]   simulate a playback session
  abrsim traces                      list the built-in network traces
  abrsim algorithms                  list the adaptation algorithms
  abrsim version

Flags for `+"`run`"+`:
  --trace NAME|PATH      a built-in trace or a CSV of `+"`seconds,bits_per_second`"+` (default mobile-4g)
  --abr NAME             adaptation algorithm: throughput, buffer, bola (default bola)
  --sizes declared|measured
                         declared derives segment sizes from the manifest's bitrate;
                         measured spends one HEAD per segment for the real byte counts
  --startup-buffer DUR   media buffered before playback begins (default 2s)
  --buffer-cap DUR       what the player will hold before it stops requesting (default 30s)
  --play DUR             stop after this much media; 0 plays the whole asset (default 0)
  --viewers N            simulate N people on variations of the same network and
                         report the spread instead of one session (default 1)
  --header 'K: V'        sent on every request; repeatable. Credentials go here,
                         never in a flag value that lands in shell history
  --timeout DUR          per-request timeout (default 30s)
  --json                 the full report, per-request timeline included
  --no-color             never colour, whatever the terminal says (NO_COLOR is honoured too)
  --exit-on LEVEL        exit non-zero when a finding reaches ok|warn|bad|error.
                         Without it the exit code is 0 whenever the simulation ran:
                         findings are output, not failure.

Segment sizes are estimates unless --sizes measured is given, and every report
says which it used. A simulation reported as a measurement is the one way this
tool could lie.
`)
}

func cmdTraces(w io.Writer) int {
	fmt.Fprintln(w, "Built-in network traces. Each one is generated, so it is the same on every")
	fmt.Fprintln(w, "machine; --trace also takes a path to a CSV of `seconds,bits_per_second`.")
	fmt.Fprintln(w)
	for _, name := range trace.Names() {
		tr, _ := trace.Builtin(name)
		fmt.Fprintf(w, "  %-12s %s\n", name, trace.Describe(name))
		fmt.Fprintf(w, "  %-12s %.0fs, %d samples\n\n", "", tr.Span(), len(tr.Samples))
	}
	return 0
}

func cmdAlgorithms(w io.Writer) int {
	fmt.Fprintln(w, "Adaptation algorithms. Run the same ladder through more than one: where they")
	fmt.Fprintln(w, "disagree, the difference is the estimator rather than the network.")
	fmt.Fprintln(w)
	for _, name := range abr.Names() {
		fmt.Fprintf(w, "  %-12s %s\n", name, abr.Describe(name))
	}
	return 0
}

type headerList map[string]string

func (h headerList) String() string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func (h headerList) Set(v string) error {
	k, val, ok := strings.Cut(v, ":")
	if !ok {
		return fmt.Errorf("want `Name: value`, got %q", v)
	}
	h[strings.TrimSpace(k)] = strings.TrimSpace(val)
	return nil
}

func cmdRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { usage(stderr) }

	var (
		traceName = fs.String("trace", "mobile-4g", "")
		algName   = fs.String("abr", "bola", "")
		sizes     = fs.String("sizes", "declared", "")
		startup   = fs.Duration("startup-buffer", 2*time.Second, "")
		cap_      = fs.Duration("buffer-cap", 30*time.Second, "")
		play      = fs.Duration("play", 0, "")
		viewers   = fs.Int("viewers", 1, "")
		timeout   = fs.Duration("timeout", 30*time.Second, "")
		asJSON    = fs.Bool("json", false, "")
		noColour  = fs.Bool("no-color", false, "")
		exitOn    = fs.String("exit-on", "", "")
		headers   = headerList{}
	)
	fs.Var(headers, "header", "")

	// `abrsim run <manifest> --trace x` is the order anybody types, and Go's
	// flag package stops parsing at the first non-flag argument. Lifting the
	// operand out first is what makes both orders work.
	source, args := liftOperand(args)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if source == "" && fs.NArg() == 1 {
		source = fs.Arg(0)
	}
	if source == "" || fs.NArg() > 1 {
		fmt.Fprintln(stderr, "abrsim run: one master playlist, please")
		return 2
	}

	tr, err := loadTrace(*traceName)
	if err != nil {
		fmt.Fprintf(stderr, "abrsim: %v\n", err)
		return 2
	}
	alg, ok := abr.New(*algName)
	if !ok {
		fmt.Fprintf(stderr, "abrsim: no algorithm %q — try one of: %s\n", *algName, strings.Join(abr.Names(), ", "))
		return 2
	}
	measure := false
	switch *sizes {
	case "declared":
	case "measured":
		measure = true
	default:
		fmt.Fprintf(stderr, "abrsim: --sizes is declared or measured, not %q\n", *sizes)
		return 2
	}
	if *viewers < 1 {
		fmt.Fprintf(stderr, "abrsim: --viewers is how many people watch, so at least 1, not %d\n", *viewers)
		return 2
	}
	threshold := finding.Status(strings.ToUpper(*exitOn))
	if *exitOn != "" && finding.Severity(threshold) == 0 && threshold != finding.OK {
		fmt.Fprintf(stderr, "abrsim: --exit-on is ok, warn, bad or error, not %q\n", *exitOn)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	ladder, err := manifest.Load(ctx, source, manifest.LoadOptions{
		Headers:   headers,
		UserAgent: "abrsim/" + version,
		Measure:   measure,
		Timeout:   *timeout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "abrsim: %v\n", err)
		return 1
	}

	simOpts := sim.Options{
		StartupBuffer: startup.Seconds(),
		BufferCap:     cap_.Seconds(),
		MaxSeconds:    play.Seconds(),
	}

	// An audience rather than a viewer. `--viewers 1` deliberately falls through
	// to the single-run path below: one viewer is the trace as measured, and its
	// report is the one this tool has always printed, timeline included.
	if *viewers > 1 {
		pop, err := population.Run(ladder, tr, alg.Name(), simOpts, *viewers)
		if err != nil {
			fmt.Fprintf(stderr, "abrsim: %v\n", err)
			return 1
		}
		rep := output.PopulationReport{
			Source:     ladder.Source,
			Ladder:     ladder.Rungs(),
			Population: pop,
			Options: map[string]any{
				"trace":          tr.Name,
				"algorithm":      alg.Name(),
				"sizes":          *sizes,
				"startup_buffer": startup.Seconds(),
				"buffer_cap":     cap_.Seconds(),
				"viewers":        *viewers,
			},
		}
		if *asJSON {
			if err := output.JSONPopulation(stdout, rep); err != nil {
				fmt.Fprintf(stderr, "abrsim: %v\n", err)
				return 1
			}
		} else {
			colour := output.UseColour(isTerminal(stdout) && !*noColour, os.Getenv("NO_COLOR"))
			if err := output.TextPopulation(stdout, rep, colour); err != nil {
				fmt.Fprintf(stderr, "abrsim: %v\n", err)
				return 1
			}
		}
		// The whole audience, not its median: a gate that read only the middle
		// viewer would pass a ladder that freezes for one person in twenty.
		if *exitOn != "" && finding.AtLeast(pop.Worst(), threshold) {
			return 1
		}
		return 0
	}

	res, err := sim.Run(ladder, tr, alg, simOpts)
	if err != nil {
		fmt.Fprintf(stderr, "abrsim: %v\n", err)
		return 1
	}

	rep := output.Report{
		Source:   ladder.Source,
		Ladder:   ladder.Rungs(),
		Run:      res,
		Findings: analyze.Run(res, tr, ladder),
		Options: map[string]any{
			"trace":          tr.Name,
			"algorithm":      alg.Name(),
			"sizes":          *sizes,
			"startup_buffer": startup.Seconds(),
			"buffer_cap":     cap_.Seconds(),
		},
	}

	if *asJSON {
		if err := output.JSON(stdout, rep); err != nil {
			fmt.Fprintf(stderr, "abrsim: %v\n", err)
			return 1
		}
	} else {
		colour := output.UseColour(isTerminal(stdout) && !*noColour, os.Getenv("NO_COLOR"))
		if err := output.Text(stdout, rep, colour); err != nil {
			fmt.Fprintf(stderr, "abrsim: %v\n", err)
			return 1
		}
	}

	// Exit 0 whenever the simulation ran. Findings are output, not failure —
	// only --exit-on changes that, and only for the level it was given.
	if *exitOn != "" && finding.AtLeast(finding.Worst(rep.Findings), threshold) {
		return 1
	}
	return 0
}

// liftOperand pulls a leading non-flag argument out of args and returns it
// separately, so `run <manifest> --flags` parses the same as `run --flags
// <manifest>`.
func liftOperand(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

// loadTrace resolves --trace: a built-in name first, then a path.
func loadTrace(name string) (trace.Trace, error) {
	if tr, ok := trace.Builtin(name); ok {
		return tr, nil
	}
	f, err := os.Open(name)
	if err != nil {
		return trace.Trace{}, trace.ErrUnknown(name)
	}
	defer f.Close()
	return trace.Parse(f, name)
}

// isTerminal reports whether w is a character device. Colour is for people.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
