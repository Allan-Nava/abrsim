#!/bin/sh
# release.sh — the release ritual, as a script instead of a paragraph.
#
# AGENTS.md says every commit is a tagged release with its own CHANGELOG section.
# That was seven commands run by hand and a changelog edited by hand, which is
# six chances to ship a tag whose notes describe a different version, or a
# ROADMAP.md that CI then rejects. So:
#
#   scripts/release.sh next <patch|minor|X.Y.Z>   print the version that bump gives
#   scripts/release.sh changelog                  print the top released version, and
#                                                 refuse if its section is unfinished
#   scripts/release.sh prepare <patch|minor|X.Y.Z>
#                                                 scaffold that section and both of its
#                                                 compare links
#   scripts/release.sh check                      every gate the ritual runs, no writes
#   scripts/release.sh tag [--commit <msgfile>]   verify, optionally commit, then tag
#   scripts/release.sh gates                      the gate list, as id/where/pattern —
#                                                 release_test.sh asserts ci.yml runs
#                                                 every row of it, in both directions
#
# It never pushes. Pushing is the maintainer's call and is what publishes the
# archives and the Homebrew cask, so a tag is cheap locally and consequential
# once it leaves.
#
# POSIX sh and awk only, like the rest of scripts/. RELEASE_CHANGELOG,
# RELEASE_LAST_TAG and RELEASE_DATE override the inputs so the version arithmetic
# and the changelog surgery can be tested against a fixture — without a git write
# and without touching this repository's own CHANGELOG.md.

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
changelog="${RELEASE_CHANGELOG:-$root/CHANGELOG.md}"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/abrsim-release.XXXXXX")
trap 'rm -rf "$tmp"' EXIT INT HUP TERM

# The scaffold marker. `check` and `tag` refuse while it is still in the file: a
# release whose notes say TODO is a release nobody can read.
placeholder="TODO"

die() { echo "release.sh: $*" >&2; exit 1; }

# repo_url comes from go.mod rather than from `git remote`, so it is the same on a
# fresh clone, in CI, and in a fixture with no remote at all.
repo_url() {
	mod=$(awk '/^module /{ print $2; exit }' "$root/go.mod")
	case "$mod" in
	github.com/*) echo "https://$mod" ;;
	*) die "cannot derive the repository URL from module $mod" ;;
	esac
}

last_tag() {
	if [ -n "${RELEASE_LAST_TAG:-}" ]; then
		echo "$RELEASE_LAST_TAG"
		return
	fi
	git -C "$root" describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"
}

today() { echo "${RELEASE_DATE:-$(date +%F)}"; }

is_version() {
	case "$1" in
	[0-9]*.[0-9]*.[0-9]*)
		# Three numeric fields and nothing else.
		echo "$1" | awk -F. 'NF != 3 { exit 1 } { for (i = 1; i <= 3; i++) if ($i !~ /^[0-9]+$/) exit 1 }'
		;;
	*) return 1 ;;
	esac
}

# next_version <patch|minor|X.Y.Z>
next_version() {
	bump=${1:-}
	[ -n "$bump" ] || die "next needs patch, minor or an explicit X.Y.Z"
	if is_version "$bump"; then
		echo "$bump"
		return
	fi
	case "$bump" in
	patch | minor) ;;
	*) die "bump is patch, minor or X.Y.Z, not $bump — a major is a decision, not an increment" ;;
	esac
	cur=$(last_tag | sed 's/^v//')
	is_version "$cur" || die "the latest tag is $(last_tag), which is not vX.Y.Z"
	echo "$cur" | awk -F. -v bump="$bump" '{
		if (bump == "patch") printf "%d.%d.%d\n", $1, $2, $3 + 1
		else printf "%d.%d.0\n", $1, $2 + 1
	}'
}

# top_version prints the highest `## [X.Y.Z]` heading in the changelog.
top_version() {
	awk 'match($0, /^## \[[0-9]+\.[0-9]+\.[0-9]+\]/) {
		v = substr($0, RSTART + 4, RLENGTH - 5)
		print v
		exit
	}' "$changelog"
}

# section_of <version> prints that release's section.
section_of() {
	awk -v v="$1" '
		$0 ~ "^## \\[" v "\\]" { inside = 1; next }
		inside && /^## \[/ { exit }
		inside { print }
	' "$changelog"
}

changelog_cmd() {
	v=$(top_version)
	[ -n "$v" ] || die "no released section in $(basename "$changelog")"
	body=$(section_of "$v")
	case "$body" in
	*"$placeholder"*) die "the [$v] section still carries the scaffold — write it before tagging it" ;;
	esac
	# A section with no entries at all is the same problem wearing a suit.
	echo "$body" | grep -q '^- ' || die "the [$v] section has no entries"
	echo "$v"
}

prepare_cmd() {
	v=$(next_version "${1:-}")
	prev=$(top_version)
	[ -n "$prev" ] || die "no previous release section to link against"
	grep -q "^## \[$v\]" "$changelog" && die "[$v] already has a section — nothing to prepare"
	# Refuse to go backwards: a changelog out of order is worse than none.
	if [ "$(printf '%s\n%s\n' "$prev" "$v" | sort -t. -k1,1n -k2,2n -k3,3n | tail -1)" != "$v" ] || [ "$v" = "$prev" ]; then
		die "$v is not above the latest section [$prev]"
	fi
	url=$(repo_url)

	awk -v v="$v" -v prev="$prev" -v date="$(today)" -v url="$url" -v ph="$placeholder" '
	/^## \[Unreleased\]/ {
		print
		print ""
		print "## [" v "] — " date
		print ""
		print "### Added"
		print ""
		print "- " ph ": what shipped, why it earns its place, and the AB-n it closes."
		next
	}
	$0 ~ "^\\[Unreleased\\]:" {
		print "[Unreleased]: " url "/compare/v" v "...HEAD"
		print "[" v "]: " url "/compare/v" prev "...v" v
		next
	}
	{ print }
	' "$changelog" >"$tmp/CHANGELOG.new"

	grep -q "^## \[$v\]" "$tmp/CHANGELOG.new" || die "the scaffold did not land — is there an ## [Unreleased] heading?"
	cat "$tmp/CHANGELOG.new" >"$changelog"
	echo "prepared [$v] in $(basename "$changelog") — write the entries, then: scripts/release.sh tag"
}

# gates is the single definition of what "the ritual" means: one row per gate, as
# `id <TAB> where <TAB> pattern`. `where` is ci for a gate the pipeline has to run
# too, tag for one that only makes sense at a tag; `pattern` is the literal text
# release_test.sh looks for in .github/workflows/ci.yml.
#
# It exists because ci.yml and this script enforced the same list by coincidence,
# maintained by hand. A gate added to one was invisible to the other, which is the
# exact drift docs.sh was written to stop for the documentation.
gates_cmd() {
	printf '%s\t%s\t%s\n' \
		backlog-lint      ci  './scripts/backlog.sh lint' \
		backlog-staleness ci  './scripts/backlog.sh check' \
		backlog-tooling   ci  'sh ./scripts/backlog_test.sh' \
		release-tooling   ci  'sh ./scripts/release_test.sh' \
		issues-tooling    ci  'sh ./scripts/backlog_issues_test.sh' \
		docs              ci  './scripts/docs.sh check' \
		goreleaser-config ci  'args: check' \
		gofmt             ci  'gofmt -l ./cmd ./internal' \
		vet               ci  'go vet ./...' \
		test-race         ci  'go test -race' \
		zero-deps         ci  'go.sum is not empty' \
		vulns             ci  'govulncheck ./...' \
		changelog         tag '-' \
		smoke             tag 'TestSmokeReferenceStreams'
}

check_cmd() {
	echo "== backlog =="
	"$root/scripts/backlog.sh" lint
	"$root/scripts/backlog.sh" check
	echo "== tooling tests =="
	sh "$root/scripts/backlog_test.sh" | tail -1
	sh "$root/scripts/release_test.sh" | tail -1
	echo "== docs =="
	"$root/scripts/docs.sh" check
	echo "== release config =="
	if command -v goreleaser >/dev/null 2>&1; then
		goreleaser check
	else
		echo "goreleaser not installed locally — CI checks it on every commit"
	fi
	echo "== zero dependencies =="
	grep -q '^require' "$root/go.mod" && die "go.mod grew a require block"
	[ -s "$root/go.sum" ] && die "go.sum is not empty"
	echo "no dependencies"
	echo "== vulnerabilities =="
	if command -v govulncheck >/dev/null 2>&1; then
		(cd "$root" && govulncheck ./...) >/dev/null && echo "govulncheck ./... clean"
	else
		echo "govulncheck not installed locally — CI runs it on every commit"
	fi
	echo "== go =="
	out=$(cd "$root" && gofmt -l ./cmd ./internal)
	[ -z "$out" ] && echo "gofmt clean" || die "not gofmt'd: $out"
	(cd "$root" && go vet ./...) && echo "vet clean"
	(cd "$root" && go test -race ./...) >"$tmp/test.log" 2>&1 || { cat "$tmp/test.log" >&2; die "tests failed"; }
	echo "tests pass (-race)"
	echo "== changelog =="
	echo "[$(changelog_cmd)] is written"
	echo
	echo "the smoke test against real streams is the one gate this cannot run for you:"
	echo "  go build -o /tmp/abrsim ./cmd/abrsim"
	echo "  ABRSIM_BIN=/tmp/abrsim go test -tags smoke -run TestSmokeReferenceStreams ./internal/analyze/ -v"
}

tag_cmd() {
	msgfile=""
	while [ $# -gt 0 ]; do
		case "$1" in
		--commit)
			[ -n "${2:-}" ] || die "--commit needs a file holding the commit message"
			msgfile=$2
			shift 2
			;;
		*) die "tag: unexpected argument $1" ;;
		esac
	done

	v=$(changelog_cmd)
	git -C "$root" rev-parse "v$v" >/dev/null 2>&1 && die "v$v already exists as a tag"

	check_cmd >"$tmp/check.log" 2>&1 || { cat "$tmp/check.log" >&2; die "the gates did not pass, so nothing was tagged"; }
	echo "gates passed for $v"

	if [ -n "$msgfile" ]; then
		[ -f "$msgfile" ] || die "no commit message at $msgfile"
		git -C "$root" add -A
		git -C "$root" commit -q -F "$msgfile"
		echo "committed"
	elif [ -n "$(git -C "$root" status --porcelain)" ]; then
		die "the working tree is dirty and no --commit <msgfile> was given"
	fi

	git -C "$root" tag -a "v$v" -m "Release $v"
	echo "tagged v$v on $(git -C "$root" rev-parse --short HEAD)"
	echo "not pushed: that is the maintainer's call, and it is what publishes the archives and the cask"
}

case "${1:-}" in
gates)     gates_cmd ;;
next)      shift; next_version "${1:-}" ;;
changelog) changelog_cmd ;;
prepare)   shift; prepare_cmd "${1:-}" ;;
check)     check_cmd ;;
tag)       shift; tag_cmd "$@" ;;
*)
	echo "usage: scripts/release.sh next <patch|minor|X.Y.Z>" >&2
	echo "       scripts/release.sh gates" >&2
	echo "       scripts/release.sh changelog" >&2
	echo "       scripts/release.sh prepare <patch|minor|X.Y.Z>" >&2
	echo "       scripts/release.sh check" >&2
	echo "       scripts/release.sh tag [--commit <msgfile>]" >&2
	exit 2
	;;
esac
