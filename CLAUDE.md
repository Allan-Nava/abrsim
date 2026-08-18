# CLAUDE.md — abrsim

The operating brief for this repository lives in [AGENTS.md](AGENTS.md), which
is canonical. Read it before touching anything here.

The one-sentence version: **abrsim takes a real HLS ladder and a network trace,
simulates what an ABR player would do, and reports what it cost the viewer** —
deterministically, offline, zero dependencies. Every feature earns its place
against that sentence.

Four rules that are violated most often and cost most when they are:

- **Test first.** The failing test lands before the implementation, and it is
  run and seen failing *for the right reason*.
- **Determinism is the product.** No clock, no `math/rand`, no map iteration
  reaching the output from any simulation path.
- **Never invent a measurement.** `(value, false)` means "I could not measure
  this"; a simulation reported as a measurement is the one way this tool can lie.
- **Real streams before the tag.** Build the binary and run the smoke test —
  three design errors were found there on its first run and none by a unit test.

When AGENTS.md and this file disagree, AGENTS.md wins and this file gets fixed.
