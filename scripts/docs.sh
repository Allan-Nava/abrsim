#!/bin/sh
# docs.sh — keep the Pages site and the README in step with the binary.
#
#   scripts/docs.sh check    fail when the page and the source disagree (the CI gate)
#   scripts/docs.sh names    print what the source says exists, for eyeballing
#
# It also asserts that every install command is backed by something that ships:
# `brew install` needs the cask in .goreleaser.yaml, and the coordinate in the
# docs has to be the one goreleaser will publish; `go install` needs the module
# path from go.mod; `docker run` needs an image the release actually builds. A
# command a reader runs and watches fail is worse than an undocumented one.
#
# The page is hand-written prose, not generated: a landing page derived
# mechanically from the README reads like neither. What is mechanical is the
# vocabulary — the checks, the traces, the algorithms and the flags — and that is
# exactly the part that goes stale in silence when a check is renamed. So the
# names come from the Go sources and this script asserts the page and the README
# both use them, in both directions for flags.
#
# It also asserts the page stays self-contained. "One static file, no third-party
# requests" is a claim the page makes about itself, and a claim in a document
# nobody checks is a claim that quietly stops being true.
#
# POSIX sh and awk only — this repository has no dependencies, and neither does
# its tooling. DOCS_PAGE, DOCS_README and DOCS_SRC override the inputs so the
# script can be tested against a deliberately broken copy.

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
src="${DOCS_SRC:-$root}"
page="${DOCS_PAGE:-$root/docs/index.html}"
readme="${DOCS_README:-$root/README.md}"

bad=0
err() { echo "docs.sh: $*" >&2; bad=$((bad + 1)); }

# --- what the source says exists ------------------------------------------------

checks() {
	awk -F'"' '/Check:[ \t]*"/ { print $2 }' "$src/internal/analyze/analyze.go" | sort -u
}

traces() {
	awk -F'"' '
		/^var builtins = map\[string\]builtin\{/ { inside = 1; next }
		inside && /^\}/                          { inside = 0 }
		inside && /^\t"/                         { print $2 }
	' "$src/internal/trace/builtin.go" | sort -u
}

algorithms() {
	awk '
		/out := \[\]string\{/ {
			n = split($0, part, "\"")
			for (i = 2; i < n; i += 2) print part[i]
		}
	' "$src/internal/abr/abr.go" | sort -u
}

# Flags as the `--help` text lists them: two spaces, then the flag.
flags() {
	awk '/^  --/ { print $1 }' "$src/cmd/abrsim/main.go" | sort -u
}

# Flags the page claims exist. The <style> block is skipped: CSS custom
# properties are spelled `--accent` too, and every one of them would otherwise
# read as a flag the CLI has lost.
page_flags() {
	awk '
		/<style>/  { instyle = 1 }
		/<\/style>/ { instyle = 0; next }
		instyle    { next }
		# Somebody else\'"'"'s flags are not ours: `brew install --cask` and
		# `docker run --rm` are on the page because they are how a reader installs
		# and runs this, and reading `--cask` as a missing abrsim flag is how this
		# check first failed after the install block landed.
		/brew |docker |git / { next }
		{
			s = $0
			while (match(s, /--[a-z][a-z-]+/)) {
				print substr(s, RSTART, RLENGTH)
				s = substr(s, RSTART + RLENGTH)
			}
		}
	' "$page" | sort -u
}

# --- the gate -------------------------------------------------------------------

present() {          # present <file> <needle> <what>
	grep -qF -- "$2" "$1" || err "$3 \`$2\` is missing from $(basename "$1")"
}

# A name has to appear as a name, not merely as a word. `train` is in the page's
# headline — "what does it do on a train?" — so a bare substring search called the
# trace documented after it had been renamed. Names are looked for as
# `<code>train</code>` on the page and as `train` in backticks in the README,
# which is how both files already write them.
present_name() {     # present_name <needle> <what>
	grep -qF -- "<code>$1</code>" "$page" || err "$2 \`$1\` is not named in $(basename "$page")"
	grep -qF -- "\`$1\`" "$readme" || err "$2 \`$1\` is not named in $(basename "$readme")"
}

check() {
	[ -f "$page" ] || { echo "docs.sh: no page at $page" >&2; exit 1; }

	checks     | while read -r name; do present_name "$name" "check"; done
	traces     | while read -r name; do present_name "$name" "trace"; done
	algorithms | while read -r name; do present_name "$name" "algorithm"; done
	flags | while read -r flag; do
		present "$page" "$flag" "flag"
		present "$readme" "$flag" "flag"
	done

	# The other direction, for flags only: a flag the page documents and the
	# binary does not have is worse than an undocumented one, because a reader
	# has no way to find out it is gone.
	known=$(flags)
	page_flags | while read -r flag; do
		echo "$known" | grep -qx -- "$flag" ||
			err "the page documents \`$flag\`, which no longer exists in the CLI"
	done

	# Self-contained, as the page says it is.
	grep -nE '<script|@import|<link[^>]+stylesheet|(src|href)="https?://[^"]*\.(css|js|woff2?|png|jpg|svg)' "$page" |
		while IFS= read -r hit; do
			err "the page pulls something off-origin: $hit"
		done

	# Every asset it references has to exist next to it.
	awk '{
		s = $0
		while (match(s, /(src|href)="assets\/[^"]+"/)) {
			hit = substr(s, RSTART, RLENGTH)
			sub(/^[a-z]+="/, "", hit); sub(/"$/, "", hit)
			print hit
			s = substr(s, RSTART + RLENGTH)
		}
	}' "$page" | sort -u | while read -r asset; do
		[ -f "$(dirname "$page")/$asset" ] || err "the page references $asset, which is not there"
	done

	grep -q "<title>" "$page" || err "the page has no <title>"

	# An install command is a claim about something that ships, so it is checked
	# against the thing that ships it rather than taken on trust. A `brew install`
	# line with no cask behind it is the worst kind of documentation: it fails on
	# the reader's machine, in a way they will blame on the tool.
	module=$(awk '/^module /{ print $2 }' "$src/go.mod")
	cask=$(awk '
		/^homebrew_casks:/            { inside = 1; next }
		inside && /^[a-z_]+:/         { inside = 0 }
		inside && /^  - name:/        { print $3 }
	' "$src/.goreleaser.yaml" 2>/dev/null || true)
	tap=$(awk '
		/^homebrew_casks:/     { inside = 1; next }
		inside && /^[a-z_]+:/  { inside = 0 }
		inside && /^      owner:/ { owner = $2 }
		inside && /^      name:/  { name = $2 }
		END { if (owner != "" && name != "") print tolower(owner) "/" name }
	' "$src/.goreleaser.yaml" 2>/dev/null || true)

	for doc in "$page" "$readme"; do
		base=$(basename "$doc")

		if grep -q "brew install" "$doc"; then
			if [ -z "$cask" ]; then
				err "$base offers \`brew install\` and .goreleaser.yaml declares no cask"
			else
				# Homebrew reads `owner/tap/name` as the repository
				# `Owner/homebrew-tap`, so the coordinate in the docs is derivable
				# from the config and must match it exactly.
				case "$tap" in
				*/homebrew-tap) want="${tap%/homebrew-tap}/tap/$cask" ;;
				*)              want="" ; err "the cask publishes to \`$tap\`, which is not a Homebrew tap repository" ;;
				esac
				[ -z "$want" ] || grep -qF -- "$want" "$doc" ||
					err "$base names a cask other than \`$want\`, which is the one that gets published"
			fi
		fi

		if grep -q "go install" "$doc"; then
			grep -qF -- "$module/cmd/abrsim@latest" "$doc" ||
				err "$base offers a \`go install\` for something other than $module/cmd/abrsim"
		fi

		if grep -q "docker run" "$doc"; then
			grep -q "^dockers" "$src/.goreleaser.yaml" 2>/dev/null ||
				err "$base offers a container image that nothing in .goreleaser.yaml builds"
		fi
	done

	# The subshells above cannot raise `bad` in this shell, so count the
	# diagnostics instead of trusting a variable a pipeline reset.
	return 0
}

case "${1:-check}" in
check)
	out=$(check 2>&1 >/dev/null || true)
	if [ -n "$out" ]; then
		echo "$out" >&2
		n=$(echo "$out" | grep -c 'docs.sh:' || true)
		echo "docs.sh: $n problem(s) — the page and the binary disagree" >&2
		exit 1
	fi
	echo "docs.sh OK — $(checks | wc -l | tr -d ' ') checks, $(traces | wc -l | tr -d ' ') traces, $(algorithms | wc -l | tr -d ' ') algorithms, $(flags | wc -l | tr -d ' ') flags named consistently in $(basename "$page") and $(basename "$readme"); every install command backed by what actually ships"
	;;
names)
	echo "checks:     $(checks | tr '\n' ' ')"
	echo "traces:     $(traces | tr '\n' ' ')"
	echo "algorithms: $(algorithms | tr '\n' ' ')"
	echo "flags:      $(flags | tr '\n' ' ')"
	;;
*)
	echo "usage: scripts/docs.sh [check|names]" >&2
	exit 2
	;;
esac
