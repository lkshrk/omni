#!/usr/bin/env bash
# apm-wheel.sh - build and publish the pinned APM fork wheel that Omni installs.
# Usage: ./scripts/apm-wheel.sh build [OUT_DIR]   build from the pinned fork commit and print the sha256
#        ./scripts/apm-wheel.sh verify            build, then fail unless the digest matches apmPackagePin
#        ./scripts/apm-wheel.sh publish           verify, then upload to the apm-cli-<version> release
# The pin lives in internal/app/apm_version.go; this script reads it rather than repeating it.
# Bumping APM: set the commit below and apmVersionPin, run `build`, put the new URL and digest in apmPackagePin,
# then `publish`.
set -euo pipefail

# The fork commit the pinned wheel is built from.
FORK_REPO="${APM_FORK_REPO:-https://github.com/lkshrk/apm.git}"
FORK_COMMIT="ae0188af269cab0f7a6c0cec9cff9f4076954626"
# Fixed so the zip timestamps, and therefore the digest, do not depend on when the wheel is built.
export SOURCE_DATE_EPOCH=1700000000

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_GO="${ROOT}/internal/app/apm_version.go"

const_value() {
  sed -n "s/^const $1 = \"\\([^\"]*\\)\"\$/\\1/p" "${VERSION_GO}"
}

PIN="$(const_value apmPackagePin)"
VERSION="$(const_value apmVersionPin)"
[[ -n "${PIN}" && -n "${VERSION}" ]] || { echo "failed to read the APM pins from ${VERSION_GO}" >&2; exit 1; }
PIN_DIGEST="${PIN##*#sha256=}"
PIN_URL="${PIN%%#*}"
TAG="apm-cli-${VERSION//+/-}"
FILE="apm_cli-${VERSION}-py3-none-any.whl"

build() {
  local out="$1" src
  src="$(mktemp -d)"
  trap 'rm -rf "${src}"' RETURN
  git clone --quiet "${FORK_REPO}" "${src}/repo"
  git -C "${src}/repo" checkout --quiet "${FORK_COMMIT}"
  # Export the tree so untracked files and git metadata never reach the wheel.
  mkdir "${src}/tree"
  git -C "${src}/repo" archive "${FORK_COMMIT}" | tar -x -C "${src}/tree"
  # setuptools is chatty on stderr; show the log only when the build fails.
  if ! (cd "${src}/tree" && uv build --wheel --out-dir "${out}") >"${src}/build.log" 2>&1; then
    cat "${src}/build.log" >&2
    exit 1
  fi
  [[ -f "${out}/${FILE}" ]] || { echo "build did not produce ${FILE}" >&2; exit 1; }
}

digest_of() { sha256sum "$1" | cut -d' ' -f1; }

cmd="${1:-}"
case "${cmd}" in
  build)
    out="${2:-$(mktemp -d)}"
    mkdir -p "${out}"
    build "${out}"
    echo "${out}/${FILE}"
    echo "sha256 $(digest_of "${out}/${FILE}")"
    ;;
  verify | publish)
    out="$(mktemp -d)"
    trap 'rm -rf "${out}"' EXIT
    build "${out}"
    got="$(digest_of "${out}/${FILE}")"
    if [[ "${got}" != "${PIN_DIGEST}" ]]; then
      echo "built ${FILE} has sha256 ${got}; apmPackagePin expects ${PIN_DIGEST}" >&2
      exit 1
    fi
    echo "ok: ${FILE} reproduces apmPackagePin (${got})"
    if [[ "${cmd}" == publish ]]; then
      [[ "${PIN_URL}" == *"/releases/download/${TAG}/"* ]] || { echo "apmPackagePin URL does not point at release ${TAG}" >&2; exit 1; }
      # Not an Omni release: never Latest, and not a vX.Y.Z tag, so the release workflow ignores it.
      gh release create "${TAG}" "${out}/${FILE}" --target main --latest=false \
        --title "apm-cli ${VERSION} (pinned APM fork build)" \
        --notes "Pinned build of ${FORK_REPO%.git}@${FORK_COMMIT}, reproducible with scripts/apm-wheel.sh. Omni downloads and verifies this wheel; it is not an Omni release."
    fi
    ;;
  *)
    sed -n '2,8p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
