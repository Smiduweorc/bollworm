#!/usr/bin/env bash
# Cuts a release: checks, changelog, release commit, annotated tag. Pushes
# nothing unless PUSH=1; pushing the tag is what starts GoReleaser.
#
#   scripts/release.sh v1.2.3
#   scripts/release.sh patch|minor|major
#
# The tag is the version. Go has no manifest to bump; the build stamps the
# tag in through -ldflags. The PKGBUILD's pkgver and sha256sums are updated
# after the tag is published, because the checksum is of GitHub's tarball.
set -euo pipefail

branch_wanted="${RELEASE_BRANCH:-master}"
push="${PUSH:-0}"

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

command -v git-cliff >/dev/null 2>&1 || die "git-cliff not found. Run 'make setup' first."

arg="${1:-}"
[ -n "$arg" ] || die "usage: scripts/release.sh <vX.Y.Z|major|minor|patch>"

latest="$(git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0)"
case "$arg" in
major | minor | patch)
	IFS='.' read -r major minor patch <<<"${latest#v}"
	patch="${patch%%-*}"
	case "$arg" in
	major) version="v$((major + 1)).0.0" ;;
	minor) version="v${major}.$((minor + 1)).0" ;;
	patch) version="v${major}.${minor}.$((patch + 1))" ;;
	esac
	;;
*) version="$arg" ;;
esac

[[ "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$ ]] ||
	die "version must look like v1.2.3 or v1.2.3-rc.1, got '$version'"

branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "$branch_wanted" ] || die "releases are cut from '$branch_wanted'; this is '$branch'"
[ -z "$(git status --porcelain)" ] || die "the working tree is dirty; commit or stash first"
! git rev-parse -q --verify "refs/tags/$version" >/dev/null || die "tag $version already exists"

echo "Releasing $version (previous: $latest)"
make preflight

git-cliff --tag "$version" -o CHANGELOG.md
git add CHANGELOG.md
git commit -m "chore(release): prepare for $version"
git-cliff --unreleased --tag "$version" --strip all | git tag -a "$version" --cleanup=verbatim -F -

if [ "$push" = "1" ]; then
	git push origin "$branch" "$version"
else
	echo "Tagged $version locally. Push with: git push origin $branch $version"
fi
