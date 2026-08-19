#!/bin/sh
# backlog_test.sh — tests for the subcommands that *write* BACKLOG.md: `add`,
# `done`, `milestone` and `retarget`.
#
# The read-only half of backlog.sh has been exercised on every commit since it
# existed. The writing half is new, and it is the half that can lose work: an
# `add` that renumbers an existing item breaks the one promise BACKLOG.md makes
# to every commit message and CHANGELOG entry that references an id, and a `done`
# that writes malformed metadata takes the CI gate down with it. So every one of
# these runs against a fixture, and the last test is the one that matters most:
# a command that would produce a backlog `lint` rejects must leave the file it
# was given exactly as it found it.
#
# POSIX sh and awk only, like the rest of scripts/.

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
script="$root/scripts/backlog.sh"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/abrsim-backlog-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT INT HUP TERM

failures=0
checks=0

fail() {
	failures=$((failures + 1))
	echo "FAIL: $1" >&2
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

assert_absent() {    # <name> <needle> <file>
	checks=$((checks + 1))
	if grep -qF -- "$2" "$3"; then
		fail "$1 — did not expect to find: $2"
	else
		echo "ok   $1"
	fi
}

assert_ok() {        # <name> <command...>
	name=$1
	shift
	checks=$((checks + 1))
	if "$@" >"$tmp/out" 2>"$tmp/err"; then
		echo "ok   $name"
	else
		fail "$name — command failed: $*"
		sed 's/^/       /' "$tmp/err" >&2
	fi
}

assert_fails() {     # <name> <command...>
	name=$1
	shift
	checks=$((checks + 1))
	if "$@" >"$tmp/out" 2>"$tmp/err"; then
		fail "$name — command succeeded and should not have: $*"
		sed 's/^/       /' "$tmp/out" >&2
	else
		echo "ok   $name"
	fi
}

# fixture writes a small, valid backlog to $tmp/BACKLOG.md.
fixture() {
	cat >"$tmp/BACKLOG.md" <<'EOF'
# Backlog — fixture

## M1 — The core <!-- ms: target=v0.1.0 phase=shipped -->

- [x] **AB-1 — First thing**: the one that shipped.
  <!-- ab: prio=high size=S labels=trace ver=0.1.0 -->

## M2 — Next up <!-- ms: target=v0.2.0 phase=now -->

Some prose about the milestone.

- [ ] **AB-2 — Second thing**: still open.
  <!-- ab: prio=med size=M labels=sim -->
- [ ] **AB-3 — Third thing**: also open.
  <!-- ab: prio=low size=L labels=cli,output -->

## M9 — Ongoing <!-- ms: target=ongoing phase=ongoing -->

- [ ] **AB-4 — Housekeeping**: forever.
  <!-- ab: prio=med size=S labels=project -->
EOF
	rm -f "$tmp/ROADMAP.md"
	touch "$tmp/ROADMAP.md"
}

run() {              # run the script against the fixture
	BACKLOG_FILE="$tmp/BACKLOG.md" ROADMAP_FILE="$tmp/ROADMAP.md" "$script" "$@"
}

echo "== add =="
fixture
assert_ok "add takes the next free id" run add M2 "Fourth thing" --prio high --size M --labels check,output --body "what it is and why it earns its place."
assert_contains "the new item is AB-5" '**AB-5 — Fourth thing**' "$tmp/BACKLOG.md"
assert_contains "with its metadata" '<!-- ab: prio=high size=M labels=check,output -->' "$tmp/BACKLOG.md"
assert_contains "as an open checkbox" '- [ ] **AB-5' "$tmp/BACKLOG.md"
assert_ok "the backlog still lints" run lint
assert_ok "and the roadmap was regenerated with it" grep -qF "AB-5" "$tmp/ROADMAP.md"
assert_contains "it landed inside M2, not at the end of the file" "$(printf 'AB-5')" "$tmp/BACKLOG.md"
checks=$((checks + 1))
if awk '/^## M9/{ exit found ? 0 : 1 } /AB-5/{ found = 1 }' "$tmp/BACKLOG.md"; then
	echo "ok   AB-5 sits above the next milestone heading"
else
	fail "AB-5 was appended outside M2"
fi
assert_fails "add refuses an unknown milestone" run add M42 "Nope" --prio high --size S --labels cli --body "x"
assert_fails "add refuses an unknown label" run add M2 "Nope" --prio high --size S --labels wardrobe --body "x"
assert_fails "add refuses a bad priority" run add M2 "Nope" --prio urgent --size S --labels cli --body "x"
assert_absent "and a refused add changed nothing" "Nope" "$tmp/BACKLOG.md"

echo "== add wraps prose like the rest of the file =="
fixture
assert_ok "add with a long body" run add M2 "Long one" --prio med --size L --labels sim --body "$(printf 'This body is deliberately much longer than eighty characters so that the wrapping has something to do, and it should come out indented by two spaces on every continuation line exactly like every other item in this file.')"
checks=$((checks + 1))
if [ -z "$(awk 'length > 82 { print }' "$tmp/BACKLOG.md")" ]; then
	echo "ok   no line grew past 82 columns"
else
	fail "add produced lines longer than the file's own style"
	awk 'length > 82 { print "       " $0 }' "$tmp/BACKLOG.md" >&2
fi
assert_ok "long-bodied add still lints" run lint

echo "== done =="
fixture
assert_ok "done ticks an item and stamps the release" run done AB-2 --ver 0.2.0
assert_contains "the checkbox is ticked" '- [x] **AB-2' "$tmp/BACKLOG.md"
assert_contains "the version is in the metadata" 'labels=sim ver=0.2.0' "$tmp/BACKLOG.md"
assert_absent "the other open item is untouched" '- [x] **AB-3' "$tmp/BACKLOG.md"
assert_ok "the backlog still lints" run lint
assert_fails "done refuses an item that is already done" run done AB-1 --ver 0.2.0
assert_fails "done refuses an id that does not exist" run done AB-99 --ver 0.2.0
assert_fails "done refuses a version that is not X.Y.Z" run done AB-3 --ver v0.2
assert_absent "and a refused done left AB-3 open" '- [x] **AB-3' "$tmp/BACKLOG.md"

echo "== done --note =="
fixture
assert_ok "done can append what shipped" run done AB-3 --ver 0.2.0 --note "Shipped as a flag, with the caveat that it only covers HLS."
assert_contains "the note is in the body" "Shipped as a flag" "$tmp/BACKLOG.md"
assert_contains "and the metadata still carries the version" 'ver=0.2.0' "$tmp/BACKLOG.md"
assert_ok "a noted done lints" run lint

echo "== milestone =="
fixture
assert_ok "milestone creates a section" run milestone M3 "A new direction" --target v0.3.0 --phase later --intro "Why this milestone exists."
assert_contains "the heading is there" '## M3 — A new direction <!-- ms: target=v0.3.0 phase=later -->' "$tmp/BACKLOG.md"
assert_contains "with its prose" "Why this milestone exists." "$tmp/BACKLOG.md"
checks=$((checks + 1))
if awk '/^## M9/{ exit seen ? 0 : 1 } /^## M3 /{ seen = 1 }' "$tmp/BACKLOG.md"; then
	echo "ok   it goes before the ongoing milestone, which stays last"
else
	fail "the new milestone was placed after the ongoing one"
fi
assert_ok "an empty milestone lints" run lint
assert_ok "and items can be added straight into it" run add M3 "First of the new" --prio high --size S --labels check --body "x."
assert_fails "milestone refuses an id that exists" run milestone M2 "Clash" --target v0.9.0 --phase later
assert_fails "milestone refuses a target that is not vX.Y.Z" run milestone M4 "Bad target" --target 0.4 --phase later
assert_fails "milestone refuses an unknown phase" run milestone M4 "Bad phase" --target v0.4.0 --phase soon

echo "== retarget =="
fixture
assert_ok "retarget moves a milestone's target and phase" run retarget M2 --target v0.5.0 --phase next
assert_contains "the metadata is rewritten" '## M2 — Next up <!-- ms: target=v0.5.0 phase=next -->' "$tmp/BACKLOG.md"
assert_ok "a retargeted backlog lints" run lint
assert_fails "retarget refuses an unknown milestone" run retarget M42 --target v0.5.0
assert_fails "retarget refuses a bad phase" run retarget M2 --phase whenever

echo "== the note about a finished milestone =="
fixture
assert_ok "a milestone in flight with one item shipped lints" run done AB-2 --ver 0.2.0
checks=$((checks + 1))
if run lint >"$tmp/lint.out" 2>&1 && ! grep -q "consider marking" "$tmp/lint.out"; then
	echo "ok   no advice to ship a milestone that still has open items"
else
	fail "lint advises shipping M2 while AB-3 is still open — advice that, followed, makes lint fail"
	sed 's/^/       /' "$tmp/lint.out" >&2
fi

# The actionable case: everything in the milestone is done and nobody moved the
# phase. That is worth one line.
assert_ok "close the last open item too" run done AB-3 --ver 0.2.0
checks=$((checks + 1))
if run lint >"$tmp/lint.out" 2>&1 && grep -q "M2 is finished" "$tmp/lint.out"; then
	echo "ok   a milestone whose every item is done says so"
else
	fail "a finished milestone produced no note"
	sed 's/^/       /' "$tmp/lint.out" >&2
fi

echo "== a write that would not lint is not a write =="
fixture
cp "$tmp/BACKLOG.md" "$tmp/before.md"
# An unknown label is rejected by lint, which is the last line of defence: the
# candidate file must never reach BACKLOG.md.
assert_fails "a write that would not lint is refused" run add M2 "Nope" --prio high --size S --labels wardrobe --body "x."
checks=$((checks + 1))
if cmp -s "$tmp/before.md" "$tmp/BACKLOG.md"; then
	echo "ok   the fixture is byte-identical after the refused write"
else
	fail "a refused write left the backlog modified"
	diff -u "$tmp/before.md" "$tmp/BACKLOG.md" >&2 || true
fi

# An em dash inside a title is legal — the parser splits on the first one, so the
# id is still unambiguous. What matters is that it round-trips rather than being
# silently mangled.
assert_ok "a title containing an em dash is accepted" run add M2 "Broken — title" --prio high --size S --labels cli --body "x."
assert_contains "and keeps both halves of its name" "Broken — title" "$tmp/BACKLOG.md"
assert_ok "with the backlog still linting" run lint
assert_ok "and the roadmap still agreeing" run check

echo
if [ "$failures" -gt 0 ]; then
	echo "backlog_test.sh: $failures of $checks checks failed" >&2
	exit 1
fi
echo "backlog_test.sh: $checks checks passed"
