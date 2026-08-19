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

# Or a prebuilt archive for linux/darwin/windows on amd64/arm64
# https://github.com/Allan-Nava/abrsim/releases
```

One static binary, no dependencies — `go.mod` has no `require` block and CI
enforces it. No ffmpeg, no browser, no network during the simulation itself.

The cask is macOS-only on purpose: `go install` and the archives already cover
Linux and Windows, and a second packaging path exists to become the one that goes
stale.

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
$ abrsim run https://cdn.example/master.m3u8 --trace steps-down --viewers 50

50 viewers over `steps-down` with bola — the spread, not one session

🔴 BAD    rebuffer    29 of 50 viewers (58%)   4 stalls, 69s frozen in 210s of playback (32.8%)
                            ↳ worst at viewer 17 · steps-down · the picture is stopped for this long: it is the defect a viewer notices first and forgives least
🟢 OK     ladder-gap  every viewer quiet       viewer 0: no time spent below a rung the network could have carried
…

measurement           min     median        max
startup              0.3s       0.5s       1.0s
frozen               0.0s       1.7s        69s
stalls                  0          1          4
switches/min          0.9        1.1        1.4
delivered            1.39       1.55       1.64
```

That is a real run against Apple's public reference stream. **Viewer 0 is the
trace exactly as measured, and it froze for nothing at all** — the single-viewer
report on the same inputs says the stream is fine, while 29 of 50 viewers
rebuffer and the worst loses 69 seconds of 210 to a frozen picture.

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

Percentiles — a p95 reported before any mean, and findings that carry the
percentile they fired at — are [AB-37](BACKLOG.md). Min, median and max are what
a small audience can honestly support.

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
[ROADMAP.md](ROADMAP.md) is generated from it.

## License

[PolyForm Noncommercial 1.0.0](LICENSE).
