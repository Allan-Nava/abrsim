# Backlog — abrsim

Single source of truth for what is planned. Items keep a stable `AB-n` id so
commits, the CHANGELOG and issues can reference them. New ideas go here rather
than into scattered TODO comments.

[ROADMAP.md](ROADMAP.md) is a **generated** view of this file, grouped by
milestone. Do not edit it by hand — run `scripts/backlog.sh roadmap` after
touching this file, or CI will fail.

## How to write an item

```
## M2 — Title of the milestone <!-- ms: target=v0.2.0 phase=now -->

- [ ] **AB-12 — Short name**: what it is, why it earns its place, what it
  needs to touch. <!-- ab: prio=high size=L labels=manifest -->
```

- The **id never changes**. Adding an item means taking the next free number,
  never reusing a retired one. Moving an item to a different milestone is fine;
  renumbering it is not.
- `- [ ]` is open, `- [x]` is shipped, and a shipped item carries the release it
  went out in: `ver=0.1.0`.
- Metadata lives in a trailing `<!-- ab: ... -->` comment (invisible when
  rendered, trivially parseable). Keys: `prio` (`high|med|low`), `size`
  (`S|M|L|XL`), `labels` (comma-separated, from the vocabulary below), `ver`
  (shipped items only).
- Milestone metadata is a trailing `<!-- ms: ... -->` on the heading. Keys:
  `target` (the release it aims at, or `ongoing`) and `phase`
  (`shipped|now|next|later|ongoing`).
- Labels: `trace`, `manifest`, `abr`, `sim`, `check`, `output`, `cli`,
  `delivery`, `integration`, `tests`, `docs`, `release`, `project`.

`scripts/backlog.sh lint` enforces all of the above; `scripts/backlog.sh next`
prints what to pick up.

## M1 — The deterministic core <!-- ms: target=v0.1.0 phase=shipped -->

Everything needed to answer the one question: *given this ladder and this
network, what does the viewer get?* No network access during the simulation
itself — the run has to be reproducible byte for byte.

- [x] **AB-1 — Network trace reader**: a trace is a piecewise-constant bandwidth
  function of time, read from CSV (`seconds,bits_per_second`). The one
  non-trivial operation is integrating it: how long does `n` bytes take
  starting at `t`, crossing any number of sample boundaries. Everything else in
  the simulator is arithmetic on top of that, so it is the first thing tested.
  <!-- ab: prio=high size=M labels=trace ver=0.1.0 -->
- [x] **AB-2 — Built-in trace library**: a handful of named traces so a user has
  something to run against before they own a measurement — a clean line, a
  mobile cell that collapses, a step ladder that walks the whole ABR range.
  They are *generated*, not sampled recordings: a trace nobody can regenerate is
  a binary fixture with a `.csv` extension.
  <!-- ab: prio=high size=S labels=trace ver=0.1.0 -->
- [x] **AB-3 — HLS ladder reader**: master playlist to a sorted ladder
  (`BANDWIDTH`, `AVERAGE-BANDWIDTH`, `RESOLUTION`, `CODECS`, `FRAME-RATE`), then
  each media playlist to a segment list with `EXTINF` durations and
  `EXT-X-BYTERANGE` sizes where they are stated. Attribute lists with quoted
  commas are the usual trap. <!-- ab: prio=high size=L labels=manifest ver=0.1.0 -->
- [x] **AB-4 — Segment sizes, declared versus measured**: without the real bytes
  a segment's size is `BANDWIDTH × duration ÷ 8`, which is an estimate and must
  travel as one. `--sizes measured` spends a `HEAD` (or a one-byte range
  request) per segment and uses `Content-Length`; a size that could not be
  measured stays declared and says so in the output. A simulation reported as
  measurement is the one way this tool can lie.
  <!-- ab: prio=high size=M labels=manifest,delivery ver=0.1.0 -->
- [x] **AB-5 — The playback simulator**: the loop every result comes out of —
  buffer level, startup threshold, download time from the trace, stalls when the
  buffer empties mid-download, an upper buffer cap that idles the player and
  moves it along the trace. Emits a per-segment timeline, not just totals: the
  totals are derived, and a check that cannot point at the moment it went wrong
  is not worth printing. <!-- ab: prio=high size=L labels=sim ver=0.1.0 -->
- [x] **AB-6 — Throughput ABR**: pick the highest rung under an EWMA of measured
  throughput times a safety factor. The naive baseline, and the one whose
  failure mode — oscillation — is the reason the other two exist.
  <!-- ab: prio=high size=M labels=abr ver=0.1.0 -->
- [x] **AB-7 — Buffer-based ABR**: BBA-style reservoir and cushion, choosing on
  buffer level alone. Immune to throughput mis-estimation, and the control group
  that tells you whether a stall was the network or the estimator.
  <!-- ab: prio=high size=M labels=abr ver=0.1.0 -->
- [x] **AB-8 — BOLA**: the Lyapunov formulation from Spiteri et al., in the
  parametrisation dash.js actually ships, so the numbers are comparable with a
  player somebody is really running. External authority beats a round trip
  against our own model. <!-- ab: prio=high size=M labels=abr ver=0.1.0 -->
- [x] **AB-9 — Finding model**: one `Finding` is one observation about one
  target, severity `OK < WARN < BAD < ERROR`, worst-first sort, `Value`/`Unit`
  carrying the measurement so machine consumers never parse prose. `ERROR` means
  the analysis could not run, which is why it sorts above `BAD`.
  <!-- ab: prio=high size=S labels=check ver=0.1.0 -->
- [x] **AB-10 — Checks over a run**: `rebuffer` (how long the picture was
  frozen), `startup` (time to first frame), `switches` (oscillation),
  `efficiency` (delivered bitrate against available bandwidth) and `ladder-gap`
  — the one that earns the tool its place, because it names the rung that does
  not exist and the seconds the viewer paid for its absence.
  <!-- ab: prio=high size=L labels=check ver=0.1.0 -->
- [x] **AB-11 — Renderers**: worst findings first, colour only on a TTY with
  `NO_COLOR` honoured, and a JSON document with the full per-segment timeline
  for anything downstream. <!-- ab: prio=high size=M labels=output ver=0.1.0 -->
- [x] **AB-12 — CLI**: `abrsim run <manifest> --trace <name-or-file>`, `--abr`,
  `--sizes`, `--startup-buffer`, `--buffer-cap`, `--seconds`, `--json`,
  `--exit-on`. Exit 0 whenever the simulation ran: findings are output, not
  failure. <!-- ab: prio=high size=M labels=cli ver=0.1.0 -->

- [x] **AB-33 — Reference-stream smoke test**: run the built binary against
  Apple's MPEG-TS and fMP4/HEVC examples under a build tag, and assert a
  per-stream baseline of the checks allowed to exceed OK plus the list of checks
  that must not fall silent. Not "nothing above OK" — a reference stream is
  entitled to stall on a trace that goes dark — and the silence half is what
  catches a parser that quietly stopped reading. Three design errors were found
  here on the first run: audio-only variants in the ladder, an efficiency ratio
  built on declared bitrates, and a top-rung exclusion with a magic threshold.
  <!-- ab: prio=high size=M labels=tests ver=0.1.0 -->

## M2 — Faithfulness <!-- ms: target=v0.3.0 phase=next -->

Everything that stands between "a plausible number" and "a number an encoder
setting should be changed on".

- [ ] **AB-13 — DASH input**: `SegmentTemplate` with `$Number$`/`$Time$`,
  `SegmentTimeline` with `@r`, `SegmentList`, `BaseURL` chains. Same ladder
  model as HLS behind the same interface — a multi-period MPD is several
  ladders end to end and has to be simulated as such, not flattened.
  <!-- ab: prio=high size=L labels=manifest -->
- [ ] **AB-14 — Request latency**: a per-request RTT and a connection setup cost,
  because on short segments the fixed cost dominates and a 1-second CMAF ladder
  behaves nothing like a 6-second one at the same bandwidth. Without it the
  simulator systematically flatters low-latency configurations.
  <!-- ab: prio=high size=M labels=sim,delivery -->
- [ ] **AB-15 — Audio shares the pipe**: video-only simulation over-states the
  bandwidth available to video by whatever the audio rendition costs, which is
  most visible exactly where it matters, at the bottom of the ladder.
  <!-- ab: prio=high size=M labels=sim -->
- [ ] **AB-16 — Validation against published results**: reproduce the figures
  Spiteri et al. and the dash.js test harness report on the traces they used.
  A simulator agreeing with itself proves nothing; this is the item that makes
  the rest of the numbers quotable. <!-- ab: prio=high size=L labels=tests,abr -->
- [ ] **AB-17 — Live and low-latency playback**: a live edge that advances in
  real time, so the buffer cannot be filled ahead and a stall costs latency
  rather than time. Chunked-transfer arrival within a segment is what makes
  LL-HLS/LL-DASH behave differently at all.
  <!-- ab: prio=med size=L labels=sim -->
- [ ] **AB-18 — Startup policy**: which rung the player asks for first, and how
  long it holds it. A ladder whose lowest rung is 800 kbps has a startup
  problem no steady-state analysis will ever show.
  <!-- ab: prio=med size=M labels=abr -->
- [ ] **AB-19 — Trace generator spec**: `--trace 'gen:5000@0,800@30,3000@60'`
  and friends, so a regression can plant the exact network shape that broke a
  stream without shipping a file. <!-- ab: prio=med size=S labels=trace,cli -->
- [ ] **AB-32 — A rung the line can only just carry is not a rung**: with a
  5 Mbps connection against a top rung of exactly 5 Mbps the player can never
  sustain it — every segment takes as long to fetch as to play — so it sits
  below and `ladder-gap` stays quiet, because from its point of view the ladder
  had somewhere to go. Only `efficiency` notices, and it cannot say why. The
  check needs the same headroom margin above the *next* rung that it already
  applies to the current one. Found by the CLI test on the first end-to-end run.
  <!-- ab: prio=high size=S labels=check -->
- [ ] **AB-34 — Give `efficiency` an opinion it can defend**: the check reports
  delivered against available bandwidth and never exceeds OK, because both
  attempts at a severity fired on healthy reference streams — judging the whole
  session flags every stream watched on a line well above its ladder, and
  judging only the playback below the top rung flags the ramp-up instead, since
  a buffer-based algorithm is always below the top while it climbs. Separating a
  slow ramp from real waste needs a definition of steady state: the playback
  after the player first stops climbing, or after the buffer first reaches its
  cap. Until then the number is stated and the judgement is not made.
  <!-- ab: prio=high size=M labels=check -->
- [ ] **AB-35 — The startup rule a player applies before BOLA**: dash.js does
  not hand the first requests to BOLA at all — it runs a throughput rule until
  playback is established, and BOLA's own judgement takes over after. abrsim
  models the low-buffer safeguard but not the startup state, so the rung the
  first segment is fetched at is BOLA's rather than a real player's, and that is
  the one figure the `startup` check exists to report.
  <!-- ab: prio=high size=M labels=abr -->
- [ ] **AB-20 — Seeking and mid-stream joins**: start at an offset, or jump, and
  measure what the viewer waits. The rebuffer nobody counts is the one after a
  scrub. <!-- ab: prio=low size=M labels=sim -->

## M3 — Comparison and CI <!-- ms: target=v0.4.0 phase=later -->

One run answers a question; the tool earns a place in a pipeline when it can
answer *did this change make it better*.

- [ ] **AB-21 — Sweep**: every trace against every ABR in one run, rendered as a
  matrix. One simulation is an anecdote; the matrix is where a ladder's real
  weak spot shows up. <!-- ab: prio=high size=M labels=cli,output -->
- [ ] **AB-22 — Compare two ladders**: simulate two manifests over the same
  traces and report the deltas, so "we dropped the 1600k rung" becomes a number
  in seconds of rebuffer. <!-- ab: prio=high size=M labels=cli,output -->
- [ ] **AB-23 — Budgets as config**: a checked-in file of thresholds
  (max rebuffer, max startup, min efficiency) per trace, and an exit code when
  one is breached. This is what makes it a CI gate on the encoding ladder
  rather than a thing someone runs after an incident.
  <!-- ab: prio=high size=M labels=cli -->
- [ ] **AB-24 — Timeline export**: per-second CSV of buffer, rung and throughput
  for plotting, and markdown output for pasting into an incident document.
  <!-- ab: prio=med size=S labels=output -->

## M4 — Integration <!-- ms: target=v0.5.0 phase=later -->

- [ ] **AB-25 — Read sizes from a segcheck report**: segcheck already downloaded
  and measured the segments; taking its JSON turns AB-4's estimate into real
  bytes for free, and makes the two tools one workflow.
  <!-- ab: prio=high size=M labels=integration -->
- [ ] **AB-26 — Propose the missing rung**: given a ladder-gap finding, compute
  the bitrate that would have removed it and what it would have cost. Naming a
  defect is worth less than naming the fix, and ladder-bench is where the
  proposal gets built. <!-- ab: prio=med size=L labels=check,integration -->
- [ ] **AB-27 — GitHub Action**: run a sweep against the budgets on every encoder
  configuration change. <!-- ab: prio=med size=S labels=integration,delivery -->
- [ ] **AB-28 — Container image**: a static image for pipelines that will not
  install a binary. <!-- ab: prio=low size=S labels=delivery -->

## M6 — The audience, not the session <!-- ms: target=v0.2.0 phase=now -->

**This milestone went first.** M2 was the one in flight and the maintainer chose
to start here instead, so the targets moved rather than the meanings: M6 aims at
`v0.2.0`, M2 at `v0.3.0`, M3 at `v0.4.0`, M4 at `v0.5.0`. Milestone numbers are
identities, not an order of service.

One run over one trace says what one viewer got, and that is an anecdote a
ladder decision cannot rest on. Nobody encodes a rung for one viewer: the rung
someone is about to delete is defended or condemned by the tail of the audience,
not by its median, and the cost of keeping it is paid per viewer-hour. This
milestone turns a run into a distribution — same determinism, same refusal to
invent a measurement, one more axis.

- [x] **AB-36 — A population, not a viewer**: `--viewers n` runs n sessions over
  traces drawn around a named built-in, and reports the run as a distribution.
  The nth viewer's trace comes from the same fixed-seed integer hash the built-in
  traces already use, so `--viewers 500` twice is the same 500 viewers on any
  machine and any Go release — a population nobody can regenerate is a benchmark
  nobody can argue with. Sessions are independent, so they fan out across
  goroutines and the results are ordered by index before anything reads them:
  the goroutine that finishes first must not decide what the output says.
  Shipped with the scales **stratified** rather than drawn independently (an
  independent draw left `--viewers 30` with a mean scale of 1.14 — thirty people
  on a better line than the one measured, and no bottom tail at all), min/median/
  max rather than percentiles (AB-37), and a per-viewer summary in the JSON rather
  than n request timelines. On Apple's reference stream over `steps-down` it found
  what it was built to find: viewer 0 froze for nothing, 29 of 50 viewers
  rebuffered, and the worst lost 69s of 210 to a frozen picture.
  <!-- ab: prio=high size=L labels=sim,trace,cli ver=0.2.0 -->
- [ ] **AB-37 — The p95 first, and the mean nowhere**: over a population every
  check reports p50/p95/p99 rather than an average, and a finding carries the
  percentile it fired at. A mean startup time of 2.1s hides the one viewer in
  twenty who waited nine seconds and left, and that viewer is the entire reason
  an operator is reading this. Worst first, as everywhere else: the p99 line
  comes before the p50 one. <!-- ab: prio=high size=M labels=check,output -->
- [ ] **AB-38 — Rung attribution**: how many seconds of the audience's playback
  each rung actually served, per rung, from the timeline the simulator already
  emits. A rung nothing ever selects costs encoding, storage and egress and buys
  no viewer anything; a rung carrying 60% of playback is the one nobody should
  touch. `ladder-gap` names a hole — this names the rungs that are pulling their
  weight, which is the other half of the same argument.
  <!-- ab: prio=high size=M labels=check,output -->
- [ ] **AB-39 — What the ladder costs to ship**: bytes delivered per viewer-hour,
  per rung, from `Result.DeliveredBitrate` and never from a declared `BANDWIDTH`
  — the trap that once produced an `efficiency` reading of 129%. Paired with
  AB-38 it makes dropping a rung a number in both directions: seconds of
  rebuffer added against gigabytes of egress saved. No severity: what a gigabyte
  is worth is a commercial judgement and abrsim does not know the contract.
  <!-- ab: prio=med size=M labels=check,delivery -->
- [ ] **AB-40 — The screen is part of the ladder**: a viewer on a phone cannot
  see the difference between the 1080p rung and the 720p one, so counting them
  in "the top rung served 40% of playback" overstates what the top rung buys.
  A device mix caps the useful rung per population segment. The mix is an input
  and never a guess: with none stated the report is per device class and makes no
  single judgement, because inventing an audience is inventing a measurement.
  <!-- ab: prio=med size=M labels=sim,cli -->
- [ ] **AB-41 — QoE as one number, with its weights printed next to it**: the
  linear QoE the published ABR literature scores against — rung utility, minus a
  rebuffer penalty, minus a switch penalty — so an abrsim population is
  comparable with a published result instead of only with itself. The weights are
  a judgement someone is entitled to disagree with, so they live in the same
  named threshold struct as every other one and print alongside the score. No
  severity until AB-16 shows our numbers land where the papers' do; a score with
  no opinion beats a grade nobody can defend.
  <!-- ab: prio=med size=M labels=check,abr -->

## M7 — A ladder you can defend <!-- ms: target=v0.6.0 phase=later -->

Everything up to here **names** a defect. An encoding team then has to decide what
to do about it, and that decision — add a rung, drop one, move two — is where the
money is and where this tool currently stops. `ladder-gap` says a rung is missing;
AB-26 proposes one rung; neither answers *what should this ladder be*.

This milestone searches. It needs M6's audience to score against (a ladder tuned
to one viewer is tuned to nothing) and M3's budgets to search under, so it comes
after both — and it inherits their honesty: a recommendation is a judgement
someone is entitled to disagree with, so the arithmetic behind it has to be on the
table, and the trade-off has to be shown rather than resolved on the operator's
behalf.

- [ ] **AB-46 — Candidate ladders, generated deterministically**: from the ladder
  in hand, enumerate the neighbours worth trying — a rung inserted at a
  geometrically sensible bitrate, a rung dropped, a rung moved, the top capped —
  bounded so the search is finite and seeded so it is the same search twice. No
  `math/rand` and no clock, like everything else that reaches the output. This is
  the piece every other item here consumes. <!-- ab: prio=high size=M labels=check,output -->
- [ ] **AB-47 — Score a candidate over an audience, not a session**: each
  candidate ladder run over the viewer population (AB-36) across a set of traces,
  scored on what M6 measures — frozen seconds, startup, the QoE of AB-41, the
  egress of AB-39. The search is exactly as trustworthy as those numbers, which is
  why it waits for them rather than inventing a score of its own.
  <!-- ab: prio=high size=L labels=abr,check -->
- [ ] **AB-48 — The frontier, never “the best ladder”**: report the Pareto front of
  viewer cost against delivery cost — rebuffer seconds and QoE against gigabytes
  of egress and rungs to encode — and let the operator pick. Naming a single
  winner hides a judgement that is not ours to make: what a gigabyte is worth is a
  commercial question and abrsim does not know the contract.
  <!-- ab: prio=high size=M labels=output,check -->
- [ ] **AB-49 — The smallest ladder that still passes**: given AB-23's budgets, the
  fewest rungs that keep every one of them met. The rung-removal question asked in
  reverse, and the one an encoding bill is actually made of.
  <!-- ab: prio=med size=M labels=check,cli -->
- [ ] **AB-50 — Explain the recommendation in the units of the complaint**: for
  every proposed change, the seconds of rebuffer it removes, the share of playback
  it moves and from which rung to which, per trace. A recommendation an operator
  cannot argue with is one they are being asked to trust, and this project does not
  ask for trust. <!-- ab: prio=high size=M labels=output,check -->
- [ ] **AB-51 — Say when the recommendation is overfitted**: a ladder searched
  against six traces is a ladder tuned to six traces. Hold traces out of the
  search, score the winner against them too, and say plainly when it only wins on
  the ones it was tuned on — the difference between a recommendation and a
  coincidence. <!-- ab: prio=high size=M labels=tests,check -->

## M5 — Project and release <!-- ms: target=ongoing phase=ongoing -->

- [ ] **AB-29 — CI gates**: `gofmt`, `go vet`, tests with a coverage floor,
  `-race`, the cross-build matrix, `govulncheck`, the zero-dependency check and
  `backlog.sh lint` + `check`. <!-- ab: prio=high size=M labels=project,tests -->
- [x] **AB-30 — README and the docs page**: the README table of checks and the
  static Pages site, both moving in the same commit as any check, flag or
  default they describe. Shipped as `docs/index.html` — one hand-written file,
  no build step, nothing fetched at view time — with the "both moving" half
  turned from an intention into a gate: `scripts/docs.sh check` reads the check,
  trace, algorithm and flag names out of the Go sources and fails when the page
  or the README does not name them, and fails the other way too when the page
  documents a flag the CLI has lost.
  <!-- ab: prio=high size=M labels=docs ver=0.1.2 -->
- [x] **AB-31 — Release pipeline**: goreleaser, the cross-platform archives and
  the Homebrew tap, with the same ad-hoc-signing caveat segcheck hit. Shipped as
  `.goreleaser.yaml` (six archives, checksums, and a Homebrew **cask** — not a
  formula, which describes something built from source) plus
  `.github/workflows/release.yml`, whose first job is the reference-stream smoke
  test: "real streams before the tag" was a manual step, and a manual step is the
  one that gets skipped on the day it mattered. `goreleaser check` runs in CI on
  every commit, because a deprecated stanza discovered on the first pushed tag is
  discovered with the tag already public. The cask carries segcheck's quarantine
  postflight: Homebrew stages the binary with `com.apple.quarantine`, the binary
  is only ad-hoc signed, and Gatekeeper then kills the first run with SIGKILL and
  no message at all — which reads as a broken build rather than an unsigned one.
  <!-- ab: prio=med size=M labels=release ver=0.1.3 -->
- [x] **AB-42 — An install command is a claim, so gate it**: `docs.sh` now checks
  every install line against the thing that would ship it — `brew install` against
  the cask in `.goreleaser.yaml` *and* against the exact `owner/tap/name`
  coordinate goreleaser will publish, `go install` against the module path in
  `go.mod`, `docker run` against an image the release actually builds (there is
  none yet, so the claim is currently unmakeable, which is the point). A command
  that fails on the reader's machine is worse than an undocumented one: they blame
  the tool. Found while adding it: the reverse flag check read `--cask` as an
  abrsim flag the CLI had lost, so it now ignores lines that invoke somebody
  else's program. <!-- ab: prio=high size=S labels=docs,release ver=0.1.3 -->
- [ ] **AB-45 — The site's figures should be generated, not pasted**: the trace
  charts on the Pages site are the built-in traces' own samples and the timeline
  figure is the `--json` of a real run, which is why they are worth looking at —
  and both were drawn by a script that is not in the repository, so a trace that
  changes shape leaves a picture that quietly lies. `docs.sh` checks names, not
  geometry. Either a committed generator (POSIX sh + awk, like the rest of
  `scripts/`, reading a dump the binary can already produce) or `abrsim traces
  --svg`. The commands that produced the current figures are in HTML comments
  beside them, so the data is reproducible even before the generator exists.
  <!-- ab: prio=med size=M labels=docs,output -->
- [ ] **AB-43 — Signed archives and an SBOM**: cosign keyless over the checksum
  file and a syft SBOM per archive, so "zero dependencies" is something a consumer
  can verify instead of something the README asserts, and someone who did not
  build the binary has a reason to trust it. Deliberately left out of AB-31: each
  addition is a way for a first release to fail half-published, and the archives
  are worth more than the signature until somebody is downloading them.
  <!-- ab: prio=med size=S labels=release -->
- [ ] **AB-44 — A macOS binary Gatekeeper accepts on its own merits**: the cask's
  `xattr -dr com.apple.quarantine` postflight is a workaround for an ad-hoc
  signature, and it only helps people who install through Homebrew — anyone who
  downloads the archive still gets the silent SIGKILL. The fix is a Developer ID
  signature and notarisation, which needs a paid Apple account, so this is a
  decision to buy something rather than an afternoon's work.
  <!-- ab: prio=low size=M labels=release,delivery -->
