#!/bin/sh
# Build and push the sandbox reset image for one release.
#
#   deploy/sandbox-reset/build.sh 0.30.0 \
#     asia-northeast1-docker.pkg.dev/PROJECT/REPO/demo-reset
#
# The CLI is the release's own linux/amd64 archive, checked against the
# release's checksums.txt, not a local build: the job should run the same
# bytes every other operator downloads. The seed is examples/demo at that
# release's tag, so the demo a release describes is the demo it serves.
# SEED=path overrides it with another bundle.
#
# Then point the job at the tag and run it once:
#
#   gcloud run jobs update demo-reset --image IMAGE:VERSION
#   gcloud run jobs execute demo-reset --wait
set -eu

version=${1:?usage: build.sh VERSION IMAGE}
image=${2:?usage: build.sh VERSION IMAGE}
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)

ctx=$(mktemp -d)
trap 'rm -rf "$ctx"' EXIT

base="https://github.com/na0fu3y/ochakai/releases/download/v${version}"
archive="ochakai_${version}_linux_amd64.tar.gz"
curl -fsSL -o "$ctx/$archive" "$base/$archive"
curl -fsSL -o "$ctx/checksums.txt" "$base/checksums.txt"
# Exactly one line must name the archive. An empty match would hand
# shasum nothing to check, and some implementations pass on that.
line=$(grep " ${archive}\$" "$ctx/checksums.txt" || true)
if [ "$(printf '%s' "$line" | grep -c .)" -ne 1 ]; then
	echo "build: checksums.txt has no single line for ${archive}" >&2
	exit 1
fi
(cd "$ctx" && printf '%s\n' "$line" | shasum -a 256 -c -)
tar -xzf "$ctx/$archive" -C "$ctx" ochakai
rm "$ctx/$archive" "$ctx/checksums.txt"

if [ -n "${SEED:-}" ]; then
	cp -R "$SEED" "$ctx/seed"
else
	git -C "$repo" archive "v${version}" examples/demo | tar -x -C "$ctx"
	mv "$ctx/examples/demo" "$ctx/seed"
	rmdir "$ctx/examples"
fi
echo "seed: $(find "$ctx/seed" -name '*.md' | wc -l | tr -d ' ') concept(s)"

cp "$here/Dockerfile" "$here/reset.sh" "$ctx/"
docker buildx build --platform linux/amd64 -t "${image}:${version}" --push "$ctx"
