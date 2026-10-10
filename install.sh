#!/bin/sh
# Installs the latest released bollworm binary for this machine.
#
#   curl -sSfL https://raw.githubusercontent.com/Smiduweorc/bollworm/master/install.sh | sh
#
# BOLLWORM_VERSION  release tag to install (default: latest)
# BINDIR            where to put the binary (default: /usr/local/bin)
set -eu

repo="Smiduweorc/bollworm"
bindir="${BINDIR:-/usr/local/bin}"
version="${BOLLWORM_VERSION:-latest}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*)
	echo "unsupported architecture: $arch" >&2
	exit 1
	;;
esac
case "$os" in
linux | darwin) ;;
*)
	echo "unsupported OS: $os; download a release from https://github.com/$repo/releases" >&2
	exit 1
	;;
esac

if [ "$version" = "latest" ]; then
	version=$(curl -sSfL "https://api.github.com/repos/$repo/releases/latest" |
		grep '"tag_name"' | head -1 | cut -d'"' -f4)
	[ -n "$version" ] || {
		echo "could not determine the latest version; set BOLLWORM_VERSION" >&2
		exit 1
	}
fi

tarball="bollworm_${version#v}_${os}_${arch}.tar.gz"
url="https://github.com/$repo/releases/download/$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -sSfL "$url/$tarball" -o "$tmp/$tarball"
curl -sSfL "$url/checksums.txt" -o "$tmp/checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
	(cd "$tmp" && grep " $tarball\$" checksums.txt | sha256sum -c -) >/dev/null
else
	(cd "$tmp" && grep " $tarball\$" checksums.txt | shasum -a 256 -c -) >/dev/null
fi || {
	echo "checksum verification failed for $tarball" >&2
	exit 1
}

tar -xzf "$tmp/$tarball" -C "$tmp"
if [ -w "$bindir" ]; then
	install -m 0755 "$tmp/bollworm" "$bindir/bollworm"
else
	sudo install -m 0755 "$tmp/bollworm" "$bindir/bollworm"
fi
echo "installed bollworm $version to $bindir/bollworm"
