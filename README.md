<p align="center">
  <img src="docs/assets/logo.svg" alt="" width="96" height="96">
</p>

<h1 align="center">abrsim</h1>

<p align="center"><strong>Your ladder is fine at 100 Mbps. What does it do on a train?</strong></p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/github/go-mod/go-version/Allan-Nava/abrsim?color=10b981">
  <img alt="Zero dependencies" src="https://img.shields.io/badge/dependencies-0-10b981">
  <a href="LICENSE"><img alt="License: PolyForm Noncommercial 1.0.0" src="https://img.shields.io/badge/license-PolyForm%20Noncommercial%201.0.0-f59e0b"></a>
</p>

<p align="center"><a href="https://allan-nava.github.io/abrsim/">allan-nava.github.io/abrsim</a></p>

---

**abrsim takes a real manifest and a network trace, simulates what an ABR player
would do, and reports what it cost the viewer.** Deterministically, offline,
with no player and no traffic shaper: the same inputs give the same numbers on
every machine, which is what makes it something you can put in CI.

```
$ abrsim run https://cdn.example/master.m3u8 --trace mobile-4g --abr bola

🔴 BAD    ladder-gap  360p..1080p      no rung between 360p and 1080p (6.2× apart): 116s of playback stuck on 360p while the line carried 2.9 Mbps
                            ↳ a rung near 2.6 Mbps would have been chosen for 48% of this session; the viewer watched 116s of it at 360p instead
🔴 BAD    rebuffer    mobile-4g        7 stalls, 17s frozen in 240s of playback (7.3%)
                            ↳ the picture is stopped for this long: it is the defect a viewer notices first and forgives least
🔴 BAD    switches    session          41 rung changes in 240s (10.2/min)
                            ↳ visible as the picture repeatedly softening and sharpening
🟢 OK     coverage    mobile-4g        240s of playback, entirely within the trace
🟢 OK     efficiency  session          2.5 Mbps delivered against 2.4 Mbps averaged over the session (104%)
🟢 OK     sizes       session          segment sizes are declared, not measured
🟢 OK     startup     session          1.1s to the first frame, starting on 360p

7 checks: 4 OK, 0 WARN, 3 BAD, 0 ERROR — 60 segments, 71.0 MiB, 240s of media in 250s over `mobile-4g` with bola, mean rung 2.5 Mbps
```

Three symptoms, one cause: the ladder jumps 6.2× between 360p and 1080p, so on a
3 Mbps cell the player is either stuck well below what the line can carry or
reaching for a rung it cannot sustain, and it oscillates between the two.

## Why this exists

To answer "does my ladder hold up on a bad connection?" you normally put a real
player behind a traffic shaper and watch it. That is slow, not reproducible, and
you cannot put it in a pipeline. So the question mostly does not get asked, and
the ladder someone chose in 2019 stays where it is.

abrsim asks it in a second. It does not download the media — it reads the
manifest, works out how big each segment is, and plays the whole session
forward on arithmetic. The headline finding is `ladder-gap`: not "you had
stalls", but **the rung that does not exist, and the seconds a viewer paid for
its absence**.

It is deliberately not a conformance checker. Segment bytes are
[segcheck](https://github.com/Allan-Nava/segcheck)'s job, manifests and fleets
are [checkfleet](https://github.com/Allan-Nava/checkfleet)'s, and how a ladder
was encoded is
[ladder-bench](https://github.com/Allan-Nava/ladder-bench)'s. abrsim only asks
what a *player* does with what those tools describe.

## Install

```sh
# macOS — Homebrew cask, published from the release tags
brew install --cask allan-nava/tap/abrsim

# Go — every platform, including Linux Homebrew users
go install github.com/Allan-Nava/abrsim/cmd/abrsim@latest

# Docker — linux/amd64 and linux/arm64, 8 MB, distroless and non-root
docker run --rm ghcr.io/allan-nava/abrsim:latest \
  run https://cdn.example/master.m3u8 --trace mobile-4g

# Or a prebuilt archive for linux/darwin/windows on amd64/arm64
# https://github.com/Allan-Nava/abrsim/releases
```

One static binary, no dependencies — `go.mod` has no `require` block and CI
enforces it. No ffmpeg, no browser, no network during the simulation itself.

The cask is macOS-only on purpose: `go install` and the archives already cover
Linux and Windows, and a second packaging path exists to become the one that goes
stale.

The image is `gcr.io/distroless/static:nonroot` with the binary copied in — no
shell, no package manager, runs as 65532, and about 8 MB. It is built from the same
cross-compiled binaries as the archives of that tag, so an image and its archive can
never be different builds. `distroless` rather than `scratch` because abrsim fetches
manifests over HTTPS and `scratch` has no CA certificates: every run against a real
CDN would fail on an unverifiable certificate, which is a limit of the image
reported as a defect in the stream — the one thing this tool refuses to do. After
every release, CI pulls the published image and runs a reference stream through it.

## Checks

| Check | What it answers |
|---|---|
| `ladder-gap` | Was the player stuck below a rung the line could carry, with nothing in between? Names both ends of the hole, the seconds lost, and the bitrate a rung there would have had. |
| `rebuffer` | How long was the picture frozen, and how many times? |
| `startup` | How long before the first frame, and which rung was asked for first? |
| `switches` | How often did the rung change per minute? Oscillation is the throughput estimate chasing noise. |
| `efficiency` | What was delivered against what the line offered. **Reports a number, makes no judgement** — see [AB-34](BACKLOG.md). |
| `sizes` | Were the byte counts measured, or derived from the manifest's declared bitrate? |
| `coverage` | Did the inputs cover the whole run — or did the trace run out, or the network die? |

Severity is `OK < WARN < BAD < ERROR`, worst first. `ERROR` means a check could
not run, which is why it sorts above `BAD`: a hole in the coverage matters more
than a defect you can see.

**Exit code is 0 whenever the simulation ran.** Findings are output, not
failure. `--exit-on warn|bad|error` is what makes it a gate.

## Network traces

`--trace` takes a built-in name or a path to a CSV of `seconds,bits_per_second`
(`#` comments and a `k`/`M`/`G` suffix are fine). The built-ins are *generated*
from a fixed-seed hash, so they are byte-identical everywhere — a recorded trace
checked into a repository is a binary fixture with a `.csv` extension.

| Trace | What it is |
|---|---|
| `flat-5m` | 5 Mbps, unwavering. The control: any finding here is the ladder's, not the network's. |
| `steps-down` / `steps-up` | A staircase across the whole ABR range, thirty seconds a step. |
| `mobile-4g` | ~3 Mbps with two collapses to 400 kbps. The everyday case. |
| `train` | ~6 Mbps between tunnels, nothing at all inside them. The trace no adaptation can save. |
| `office-wifi` | 20 Mbps with somebody else's backup running. |
| `dsl-evening` | 8 Mbps degrading to 2.2 as the neighbourhood comes home. |

`abrsim traces` prints them with their spans.

## Algorithms

Run the same ladder through more than one. Where they disagree, the difference
is the estimator rather than the network.

| `--abr` | What it does |
|---|---|
| `bola` (default) | Lyapunov optimisation over buffer level (Spiteri et al., INFOCOM 2016) in the parametrisation dash.js ships, including the low-buffer throughput safeguard dash.js wraps around it — without which the algorithm death-spirals on any ladder whose top rung is above the line. |
| `throughput` | Highest rung under a smoothed estimate of measured throughput. The naive baseline, and the one that oscillates. |
| `buffer` | BBA-style reservoir and cushion, on buffer level alone. Immune to throughput mis-estimation, slow to climb. |

## Flags

```
abrsim run <master.m3u8> [flags]

  --trace NAME|PATH      built-in trace or a CSV (default mobile-4g)
  --abr NAME             throughput, buffer, bola (default bola)
  --sizes declared|measured
                         declared derives sizes from the manifest's bitrate;
                         measured spends one HEAD per segment for real byte counts
  --startup-buffer DUR   media buffered before playback begins (default 2s)
  --buffer-cap DUR       what the player holds before it stops requesting (default 30s)
  --play DUR             stop after this much media; 0 plays it all (default 0)
  --viewers N            simulate N people on variations of the same network and
                         report the spread instead of one session (default 1)
  --devices MIX          the screens that audience watches on, as
                         phone:50,tv:30,desktop:20 — shares must add up to 100
  --header 'K: V'        sent on every request; repeatable
  --timeout DUR          per-request timeout (default 30s)
  --json                 the full report, per-request timeline included
  --no-color             NO_COLOR is honoured too
  --exit-on LEVEL        exit non-zero at ok|warn|bad|error
```

Credentials go in `--header` values your shell reads from the environment, never
into a flag that lands in shell history or a CI log.

## An audience, not a viewer

One session over one trace is an anecdote — a true one, and still an anecdote.
`--viewers N` simulates N people on variations of the same network and reports the
spread, because the rung you are about to delete is defended or condemned by the
tail of an audience rather than by its median:

```
$ abrsim run https://cdn.example/master.m3u8 --trace steps-down --viewers 200

200 viewers over `steps-down` with bola — the spread, not one session

🔴 BAD    rebuffer    p99 BAD · p95 BAD · p50 WARN   112 of 200 viewers (56%)  5 stalls, 97s frozen in 210s of playback (46.2%)
                            ↳ fires from p44 up · worst at viewer 112 · steps-down · the picture is stopped for this long: it is the defect a viewer notices first and forgives least
🟡 WARN   coverage    p99 WARN · p95 OK · p50 OK     3 of 200 viewers (2%)     the trace ends at 210s and the session ran 297s: 29% of it was extrapolated
🟢 OK     ladder-gap  p99 OK · p95 OK · p50 OK       every viewer quiet        viewer 0: no time spent below a rung the network could have carried
…

measurement           min        p50        p95        p99        max
startup              0.3s       0.5s       0.9s       0.9s       1.0s
frozen               0.0s       1.7s        60s        80s        97s
stalls                  0          1          4          4          5
switches/min          0.9        1.1        1.4        1.7        2.0
delivered            1.38       1.55       1.64       1.64       1.64

7 checks over 200 viewers — 181 segments each, 210s of media each, worst finding BAD — at the p95: 0.9s to the first frame, 60s frozen
```

That is a real run against Apple's public reference stream. **Viewer 0 is the
trace exactly as measured, and it froze for nothing at all** — the single-viewer
report on the same inputs says the stream is fine, while at the 95th percentile of
this audience the picture is frozen for a minute of the three and a half.

- Viewer 0 is always the trace as measured, so `--viewers 1` is byte-for-byte the
  run this tool has always done.
- The scales are **stratified** and permuted, from the same fixed-seed hash the
  built-in traces use: `--viewers 500` is the same 500 viewers on every machine,
  and even `--viewers 8` spans the range instead of being eight people on a good
  line. A viewer's scale depends on how many viewers there are — viewer 5 of 30 is
  not viewer 5 of 200 — which is stated rather than hidden.
- Every check speaks for every viewer; a loud one reports **how much of the
  audience** it happened to, and names the viewer with the worst case so you can
  go and look at that session.
- `--exit-on` judges the whole audience. A gate that read only the median viewer
  would pass a ladder that freezes for one person in twenty.
- `--json` carries the distribution and a per-viewer summary, **not** two hundred
  request timelines. Run a single viewer for the full one.

### What each rung earned, and what it cost

`ladder-gap` names a hole. These name the rungs that *do* exist and what they were
worth — the other half of the same argument:

```
rung            bitrate     served      share    viewers        GiB/h
360p               0.6M       499s         5%         50         0.01
540p               0.9M      1238s        12%         50         0.04
720p               1.0M      5647s        54%         50         0.22
1080p              1.9M      3093s        30%         20         0.22
1 rungs served nothing at all: 234p — they cost encoding, storage and egress and
bought no viewer anything on this trace (--json lists every rung)

screen      ceiling  viewers   frozen p50    qoe p50    GiB/h p50
phone          720p       30         0.0s       1.00         0.39
tv           no cap       20         1.0s       1.63         0.64
```

- **Rung attribution**: seconds of the audience's playback each rung served, its
  share, how many viewers ever chose it, and the egress it costs per viewer-hour.
  A rung nothing selected is named rather than dropped from the table — it is
  exactly the one somebody is deciding about.
- **Cost, in the units of a delivery bill**: bytes per viewer-hour, from the bytes
  that really crossed the wire. Paired with the attribution it makes dropping a
  rung a number in **both** directions. No severity: what a gigabyte is worth is a
  commercial question and abrsim does not know the contract.
- **`--devices phone:60,tv:40`**: a screen does not fetch a rung it cannot show, so
  a phone half of the audience neither needs nor pays for the top rung. Above, the
  phones froze for nothing and cost 0.39 GiB an hour; the televisions froze a
  second and cost 0.64. The mix is **an input and never a guess** — with no
  `--devices` no screen is assumed, and the shares have to add up to 100 rather
  than being quietly normalised.
- **A QoE score, with its weights printed next to it**: the linear QoE the
  published ABR literature optimises against, in Mbps-equivalent — a 2.4 means the
  session was worth as much as watching a steady 2.4 Mbps with no stalls and no
  switching. The weights (4.3 per frozen second, 1.0 per Mbps switched) are the
  literature's, they print with the score, and no severity is attached to it until
  [AB-16](BACKLOG.md) shows our numbers land where the papers' do.

### The percentiles, and where they stop

- **Every check carries its severity at p50, p95 and p99**, worst percentile
  first, and the checks are ordered by what happens at the **p95** rather than by
  what happened to somebody: "at the 95th percentile of your audience this is BAD"
  is the sentence a ladder decision is made from. Each loud check also says the
  percentile it starts firing from — quiet for 44% of viewers means it fires from
  p44 up.
- **Every figure is a viewer that existed.** The percentiles are nearest-rank
  order statistics with no interpolation: an interpolated median of `[1,2,3,4]` is
  2.5, a number nobody in that audience experienced, and inventing a measurement
  is the one thing this tool must not do.
- **A percentile the audience cannot support is not reported.** A "p95" over ten
  viewers is the maximum wearing a better name, so a p95 needs 20 viewers and a
  p99 needs 100. Below that the column is `—` and the report says what it would
  have needed — a limit of the audience, never dressed up as a limit of the stream.
  In `--json` those come out as explicit `null`.
- No means anywhere. A mean startup of 2.1s hides the viewer who waited nine
  seconds and left, and that viewer is the entire reason an operator is reading
  this.

## What it does not model

Every one of these makes the simulation *kinder* to the stream than reality is,
so the figures are a floor on the trouble, not a ceiling:

- **Request latency** ([AB-14](BACKLOG.md)) — no RTT or connection setup, so short
  CMAF segments look better than they are.
- **Audio sharing the connection** ([AB-15](BACKLOG.md)) — video gets the whole pipe.
- **A live edge** ([AB-17](BACKLOG.md)) — VOD semantics only; the buffer can be
  filled as fast as the line allows.
- **The startup rule a real player applies before BOLA** ([AB-35](BACKLOG.md)) —
  dash.js runs a throughput rule until playback is established, so the rung the
  first segment is fetched at is BOLA's rather than a shipped player's.
- **DASH** ([AB-13](BACKLOG.md)) — HLS for now.

And unless you pass `--sizes measured`, segment sizes are
`AVERAGE-BANDWIDTH × duration ÷ 8`, which is an estimate. Every report says
which it used, in the output rather than in the documentation: a simulation
reported as a measurement is the one way this tool could lie.

## Development

```sh
go test -race ./...
go build -o /tmp/abrsim ./cmd/abrsim
ABRSIM_BIN=/tmp/abrsim go test -tags smoke -run TestSmokeReferenceStreams ./internal/analyze/ -v
```

The smoke test runs against Apple's public reference streams. Three design
errors were found there on the first run and none of them by a unit test — see
[AGENTS.md](AGENTS.md) for why that is the rule rather than the exception.

[BACKLOG.md](BACKLOG.md) is the source of truth for planned work;
[ROADMAP.md](ROADMAP.md) is generated from it. Both are edited through the tooling
rather than by hand, and so is a release:

```sh
scripts/backlog.sh add M2 "Short name" --prio high --size M --labels sim --body "why it earns its place."
scripts/backlog.sh done AB-13 --ver 0.3.0
scripts/release.sh prepare patch   # scaffolds the CHANGELOG section and its links
scripts/release.sh check           # every gate in one command
scripts/release.sh tag --commit /tmp/msg.txt
```

Every write lints before it lands and regenerates `ROADMAP.md`; nothing pushes.
The tooling has its own tests — `scripts/backlog_test.sh`, `scripts/release_test.sh`,
POSIX sh like everything in `scripts/` — and they run in CI.

## License

[PolyForm Noncommercial 1.0.0](LICENSE).
