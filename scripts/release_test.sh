#!/bin/sh
# release_test.sh — tests for release.sh's version arithmetic and its CHANGELOG
# surgery. The gates it runs and the tag it writes are exercised by using it for
# real; what is tested here is everything that edits a file or computes a number,
# because those are the parts that can quietly produce a release whose changelog
# describes a different version from its tag.
#
# POSIX sh and awk only. No git writes: every case runs against a fixture through
# RELEASE_CHANGELOG, RELEASE_LAST_TAG and RELEASE_DATE.

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
script="$root/scripts/release.sh"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/abrsim-release-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT INT HUP TERM

failures=0
checks=0
fail() { failures=$((failures + 1)); echo "FAIL: $1" >&2; }

assert_eq() {        # <name> <want> <got>
	checks=$((checks + 1))
	if [ "$2" = "$3" ]; then
		echo "ok   $1"
	else
		fail "$1 — want [$2], got [$3]"
	fi
}

assert_contains() {  # <name> <needle> <file>
	checks=$((checks + 1))
	if grep -qF -- "$2" "$3"; then
		echo "ok   $1"
	else
		fail "$1 — expected to find: $2"
		sed 's/^/       /' "$3" >&2
	fi
}

assert_fails() {     # <name> <command...>
	name=$1
	shift
	checks=$((checks + 1))
	if "$@" >"$tmp/out" 2>"$tmp/err"; then
		fail "$name — succeeded and should not have: $*"
		sed 's/^/       /' "$tmp/out" >&2
	else
		echo "ok   $name"
	fi
}

fixture() {
	cat >"$tmp/CHANGELOG.md" <<'EOF'
# Changelog

All notable changes to this project are documented here.

## [Unreleased]

## [0.2.1] — 2026-08-19

### Added

- The previous release.

## [0.2.0] — 2026-08-18

### Added

- The one before that.

[Unreleased]: https://github.com/Allan-Nava/abrsim/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/Allan-Nava/abrsim/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/Allan-Nava/abrsim/releases/tag/v0.2.0
EOF
}

run() { RELEASE_CHANGELOG="$tmp/CHANGELOG.md" RELEASE_LAST_TAG="${LAST:-v0.2.1}" RELEASE_DATE="2026-09-01" "$script" "$@"; }

echo "== next =="
fixture
assert_eq "patch from v0.2.1" "0.2.2" "$(run next patch)"
assert_eq "minor from v0.2.1" "0.3.0" "$(run next minor)"
assert_eq "an explicit version passes through" "1.4.7" "$(run next 1.4.7)"
# The assignment goes on the command substitution, never on the assert: in POSIX
# sh a variable assignment prefixed to a *function* call stays in effect after the
# function returns, and an earlier version of this line leaked LAST=v0.9.9 into
# every test below it — the prepare cases then quietly asserted against 0.10.0.
assert_eq "minor rolls the middle" "0.10.0" "$(LAST=v0.9.9 run next minor)"
unset LAST || true
assert_fails "next refuses a bump it does not know" run next major
assert_fails "next refuses a version that is not X.Y.Z" run next 1.4

echo "== changelog =="
fixture
assert_eq "the top released version is read from the file" "0.2.1" "$(run changelog)"
fixture
# A section still carrying the scaffold must not be tagged: that is a release
# whose notes say TODO.
awk '{ print } /^## \[0.2.1\]/ { print ""; print "- TODO: fill this in." }' "$tmp/CHANGELOG.md" >"$tmp/c2" && mv "$tmp/c2" "$tmp/CHANGELOG.md"
assert_fails "changelog refuses a section with the scaffold still in it" run changelog

echo "== prepare =="
fixture
if ! run prepare patch >"$tmp/out" 2>"$tmp/err"; then
	fail "prepare patch failed"
	sed 's/^/       /' "$tmp/err" >&2
fi
assert_contains "the new section is there, dated" "## [0.2.2] — 2026-09-01" "$tmp/CHANGELOG.md"
assert_contains "with a scaffold to fill in" "TODO" "$tmp/CHANGELOG.md"
assert_contains "Unreleased now compares against the new tag" "[Unreleased]: https://github.com/Allan-Nava/abrsim/compare/v0.2.2...HEAD" "$tmp/CHANGELOG.md"
assert_contains "and the new version has its own compare link" "[0.2.2]: https://github.com/Allan-Nava/abrsim/compare/v0.2.1...v0.2.2" "$tmp/CHANGELOG.md"
assert_contains "the previous link is untouched" "[0.2.1]: https://github.com/Allan-Nava/abrsim/compare/v0.2.0...v0.2.1" "$tmp/CHANGELOG.md"
checks=$((checks + 1))
if [ "$(grep -c '^## \[Unreleased\]' "$tmp/CHANGELOG.md")" = "1" ]; then
	echo "ok   Unreleased is still there exactly once"
else
	fail "prepare disturbed the Unreleased heading"
fi

echo "== prepare refuses to make a mess =="
fixture
assert_fails "prepare refuses a version that already has a section" run prepare 0.2.1
assert_fails "prepare refuses a version below the latest" run prepare 0.1.9
assert_fails "prepare refuses a bump it does not know" run prepare sideways
checks=$((checks + 1))
if grep -q "TODO" "$tmp/CHANGELOG.md"; then
	fail "a refused prepare wrote to the changelog anyway"
else
	echo "ok   a refused prepare left the changelog alone"
fi

echo "== gates =="
# release.sh check and ci.yml enforce the same list, and until now that was a
# coincidence maintained by hand: a gate added to one was invisible to the other.
# `gates` is the single definition, and this asserts CI actually runs every row of
# it — in both directions, so a gate added to the workflow and not to the ritual
# fails too.
ci="${RELEASE_CI_FILE:-$root/.github/workflows/ci.yml}"
checks=$((checks + 1))
if [ -s "$ci" ]; then
	echo "ok   ci.yml is where it is expected to be"
else
	fail "no ci.yml at $ci"
fi

"$script" gates >"$tmp/gates.tsv" 2>"$tmp/gates.err" || fail "gates failed: $(cat "$tmp/gates.err")"
checks=$((checks + 1))
if [ -s "$tmp/gates.tsv" ]; then
	echo "ok   gates lists something"
else
	fail "gates printed nothing"
fi

while IFS='	' read -r id where pattern; do
	[ "$where" = "ci" ] || continue
	checks=$((checks + 1))
	if grep -qF -- "$pattern" "$ci"; then
		echo "ok   ci.yml runs the $id gate"
	else
		fail "the $id gate is in the ritual and not in ci.yml — expected to find: $pattern"
	fi
done <"$tmp/gates.tsv"

# The other direction: every script ci.yml invokes has to be a declared gate,
# or the ritual is quietly weaker than the pipeline.
grep -oE '(\./|sh \./)scripts/[a-z_]+\.sh [a-z]*' "$ci" | sed 's/^sh //' | sort -u >"$tmp/ci-scripts.txt"
while read -r invocation; do
	[ -n "$invocation" ] || continue
	checks=$((checks + 1))
	if grep -qF -- "$invocation" "$tmp/gates.tsv"; then
		echo "ok   the ritual knows about \`$invocation\`"
	else
		fail "ci.yml runs \`$invocation\` and release.sh gates does not list it"
	fi
done <"$tmp/ci-scripts.txt"

# And prove the gate fires: a workflow missing one of the rows has to fail the
# comparison, or the whole thing is decoration. The nested run is guarded — the
# first attempt at this test re-ran the whole file, self-check included, and
# recursed until it was killed.
if [ -z "${RELEASE_TEST_NESTED:-}" ]; then
	echo "== the drift gate itself =="
	grep -v 'docs.sh check' "$ci" >"$tmp/ci-missing-docs.yml"
	checks=$((checks + 1))
	if RELEASE_TEST_NESTED=1 RELEASE_CI_FILE="$tmp/ci-missing-docs.yml" sh "$root/scripts/release_test.sh" >/dev/null 2>&1; then
		fail "a ci.yml with the docs gate removed still passed"
	else
		echo "ok   a ci.yml missing a declared gate fails"
	fi
fi

echo
if [ "$failures" -gt 0 ]; then
	echo "release_test.sh: $failures of $checks checks failed" >&2
	exit 1
fi
echo "release_test.sh: $checks checks passed"
