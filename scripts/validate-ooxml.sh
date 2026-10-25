#!/usr/bin/env bash
# Renders the OpenAPI, OpenRPC and .proto fixtures in testdata/ and validates
# each WordprocessingML part against the ECMA-376 transitional schema. Word
# refuses or "repairs" a file whose elements are out of schema order, and
# well-formed XML does not catch that; this does.
#
# The schemas come from python-docx's repository, pinned to one commit so the
# check cannot change under us. They are cached in build/xsd.
set -euo pipefail

commit=e45454602b53e8e572b179ccf1c91093ec9f4ed7
base="https://raw.githubusercontent.com/python-openxml/python-docx/$commit/ref/xsd"
root=$(cd "$(dirname "$0")/.." && pwd)
xsd="$root/build/xsd"
out="$root/build/validate"

command -v xmllint >/dev/null || { echo "xmllint not found; install libxml2" >&2; exit 1; }
command -v curl >/dev/null || { echo "curl not found" >&2; exit 1; }

mkdir -p "$xsd"
fetch() {
	[ -s "$xsd/$1" ] || curl -sSfL --retry 3 -o "$xsd/$1" "$base/$1"
}
fetch wml.xsd
# Follow imports until no schema names one we do not have.
while :; do
	missing=0
	while read -r f; do
		[ "$f" = xml.xsd ] && continue
		if [ ! -s "$xsd/$f" ]; then
			fetch "$f"
			missing=1
		fi
	done < <(grep -ho 'schemaLocation="[^"]*"' "$xsd"/*.xsd | sed 's/.*="//; s/"$//' | sort -u)
	[ "$missing" = 0 ] && break
done

# The OOXML schemas use xml:space without importing the XML namespace
# schema, which xmllint needs declared. The 0,/re/ address is GNU sed.
cat >"$xsd/xml.xsd" <<'EOF'
<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="http://www.w3.org/XML/1998/namespace">
	<xs:attribute name="space"><xs:simpleType><xs:restriction base="xs:NCName"><xs:enumeration value="default"/><xs:enumeration value="preserve"/></xs:restriction></xs:simpleType></xs:attribute>
</xs:schema>
EOF
for f in wml.xsd shared-math.xsd; do
	grep -q 'schemaLocation="xml.xsd"' "$xsd/$f" ||
		sed -i.bak '0,/<xsd:import/s//<xsd:import namespace="http:\/\/www.w3.org\/XML\/1998\/namespace" schemaLocation="xml.xsd"\/>\n  <xsd:import/' "$xsd/$f"
done

rm -rf "$out"
mkdir -p "$out"
go build -o "$out/bollworm" "$root/cmd/bollworm"

failed=0
# strava.json is left out: its $refs are fetched from developers.strava.com,
# and this check must not depend on someone else's server.
for spec in "$root"/testdata/aur.json "$root"/testdata/petstore3.json "$root"/testdata/features.yaml \
	"$root"/testdata/openrpc-petstore.json "$root"/testdata/openrpc-features.yaml \
	"$root"/testdata/proto/library/v1/library.proto "$root"/testdata/proto/haberdasher/v1/haberdasher.proto; do
	name=$(basename "$spec")
	args=()
	case "$spec" in *.proto) args=(--proto-path "$root/testdata/proto") ;; esac
	for paper in a4 letter; do
		dir="$out/$name.$paper"
		mkdir -p "$dir"
		"$out/bollworm" render "$spec" -o "$dir.docx" --paper "$paper" "${args[@]}"
		(cd "$dir" && unzip -q "$dir.docx")
		for part in "$dir"/word/*.xml; do
			if ! xmllint --noout --schema "$xsd/wml.xsd" "$part" 2>"$dir.err"; then
				echo "INVALID $name ($paper): ${part#"$dir"/}" >&2
				cat "$dir.err" >&2
				failed=1
			fi
		done
	done
done
[ "$failed" = 0 ] && echo "all parts valid"
exit "$failed"
