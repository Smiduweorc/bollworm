#!/usr/bin/env sh
# Fuzz every Fuzz* target in the module for $1 each (default 30s).
# go test -fuzz takes one target per package per run, hence the loop.
# A crasher is written to the package's testdata/fuzz and fails the run.
set -eu
fuzztime="${1:-30s}"
for pkg in $(go list ./...); do
	for target in $(go test -list '^Fuzz' "$pkg" | grep '^Fuzz' || true); do
		echo "==> $pkg $target ($fuzztime)"
		go test -run='^$' -fuzz="^${target}\$" -fuzztime="$fuzztime" "$pkg"
	done
done
