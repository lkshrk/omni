#!/usr/bin/env bash
# Keeps a working-tree install of omni current. A machine opts in by having LOCAL_BIN at all.
set -euo pipefail

[ -z "${CI:-}" ] || exit 0
target="${LOCAL_BIN:-$HOME/.local/bin/omni}"
[ -e "$target" ] || exit 0

# post-checkout passes old HEAD, new HEAD and a branch flag; a file checkout moves nothing.
if [ "$#" -ge 3 ] && { [ "$1" = "$2" ] || [ "$3" = "0" ]; }; then
	exit 0
fi

# reference-transaction passes its state and lists "<old> <new> <ref>" on stdin; only a tag moves the version.
if [ "$#" -eq 1 ]; then
	[ "$1" = "committed" ] || exit 0
	grep -q ' refs/tags/' || exit 0
fi

repo=$(git rev-parse --show-toplevel)
if ! make -C "$repo" --no-print-directory install-local LOCAL_BIN="$target"; then
	echo "refresh-local-omni: rebuilding $target failed; it still runs the previous build" >&2
	exit 1
fi
