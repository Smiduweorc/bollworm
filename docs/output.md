# What ends up in the document

## How it works

Every format is read into one model, and one renderer lays that model out.
An HTTP endpoint, a JSON-RPC method and a gRPC rpc are all a call; a tag and
a service are both a chapter; a JSON Schema and a protobuf message are both
a data model. So the three manuals have the same shape, and a feature such as
deprecation, a "See also" link or the warning for something the manual
cannot show works the same way in each.

| In the manual | OpenAPI | OpenRPC | Protobuf |
| --- | --- | --- | --- |
| Title page | `info` | `info` | the package, and its version (`v1`, `v2beta1`) |
| Introduction | `info.description`, `servers` | `info.description`, `servers` | the package comment, and the files described |
| Authentication chapter | `securitySchemes`, `security` | (OpenRPC has none) | (protobuf has none) |
| One chapter per | tag | tag | service |
| One section per | endpoint, and webhook | method | rpc |
| Data models chapter | `components.schemas` | `components.schemas` | messages and enums |
| Descriptions | CommonMark | CommonMark | leading and trailing comments |

Bollworm tells the formats apart by the file's extension, or, for any other
name and for stdin, by its content: the `openapi` or `openrpc` field, a
`syntax` or `edition` line, or the bytes of a descriptor set.

## In every manual

- **The title page** has the title and version, then the summary, contact,
  licence and terms of service when the spec gives them.
- **The introduction** has the description, a table of servers with their
  variables, and a "See also" line for `externalDocs`, each when the spec
  has it.
- **One chapter per group of calls**, in the order the groups are declared
  (or first used), then the calls in no group. A call in several groups is
  described in the chapter of its first one, and the chapters of the others
  list it with a link there.
- **Each call** has its name and description, a deprecation notice when it is
  deprecated, and a "See also" line for its `externalDocs`.
- **The data models chapter** has a section per model. A reference to a model
  anywhere in the manual links to its section.
- **Descriptions** are CommonMark (with GitHub's tables and strikethrough),
  turned into Word paragraphs, lists, tables and links.
- **Examples** are printed as JSON in the order they were written.

## Calls

**An OpenAPI endpoint** gets its method and path, the authentication it
needs, its parameters (with their `style` and `explode` when given), its
request body (with its multipart or form `encoding`), and a table of
responses with their headers and bodies, then each response's examples and
`links`, each link pointing at its target's section. Its `callbacks` follow,
laid out like the endpoint. A non-JSON example (XML, CSV, plain text) is
printed as it is. `webhooks` get a chapter of their own.

**An OpenRPC method** gets the name a client calls, a table of its params in
order with whether they go by name, by position or either, its result, and a
table of its errors with their `data`. A method with no `result` is described
as a notification. A method's own `servers` replace the document's for it.
Each example pairing is printed as the `params` a client sends and the
`result` it gets back, shaped as an object or an array to match the param
structure; an example given only as an `externalValue` is a link to it.

**A gRPC rpc** gets its gRPC path, its request and response messages, the
streaming kind (none, server, client or bidirectional), and a table of its
HTTP mappings when it has a `google.api.http` option. Connect and Twirp
services are protobuf services, so their `.proto` files render the same
way; the path in each section is the gRPC one, which Connect also serves,
and which Twirp serves under `/twirp` by default.

## Data models

**OpenAPI and OpenRPC** models are JSON Schema, and both formats get the same
treatment. Each model has a table of fields. Nested anonymous objects are
spelled out as `address.city`. Keywords beside a `$ref` (`readOnly`,
`example`, `nullable` and the rest) apply on top of the model.
`additionalProperties: false` and `unevaluatedProperties: false` are printed
as "no other properties", the `false` schema as "nothing", and every entry of
`examples` is shown. A `discriminator` is a table of its values and the
models they select, and `xml` names, attributes, wrapping and namespaces are
spelled out.

**Protobuf** messages have a table of fields with their numbers and types:
`repeated`, `map<K, V>`, `oneof` membership and `required` (proto2) are
spelled out. Enums have a table of values and their numbers. Nested types are
models of their own, named `Book.Format`. The well-known types
(`google.protobuf.Timestamp`, `Duration`, `Any` and the rest) are shown by
name and not expanded. `google.api.field_behavior` marks a field required
(`REQUIRED`) or is spelled out in its description (`OPTIONAL`,
`OUTPUT_ONLY`, `INPUT_ONLY`, `IMMUTABLE`, `IDENTIFIER`, `UNORDERED_LIST`,
`NON_EMPTY_DEFAULT`). Custom options, such as `buf.validate` rules, and a
method's `idempotency_level` are listed under what they are set on, in
protobuf text format: `(buf.validate.field): {string: {min_len: 1}}`.

## `$ref`s in OpenAPI and OpenRPC

`$ref`s are followed, recursive and mutually recursive schemas included. In
OpenRPC that covers the methods themselves and everything under
`components`: content descriptors, errors, example pairings, examples and
tags. A `$ref` may use a JSON pointer, an `$anchor` name, or a schema's
`$id`; a `$dynamicRef` is followed like a `$ref`. They are followed:

- inside the spec, always;
- to another file, always, relative to the file the `$ref` is in (or to the
  working directory for a spec on stdin);
- to an `https://` URL, only with `--allow-remote-refs`. Plain `http://` is
  never fetched, and a document fetched from the network may only refer to
  other `https://` URLs, never to a file on your machine.

A schema found in another file becomes a data model of its own, named by the
`$ref` (`athlete.json#/DetailedAthlete` is `DetailedAthlete`). Two different
schemas with one name are told apart by file: `Fault (errors.json)`.

## Warnings

Bollworm warns, on stderr as `bollworm: warning: ...`, when the spec says
something the manual cannot show as written: two operations with one
`operationId`, a link to an operation that does not exist, a discriminator
mapping that resolves to nothing, a parameter with more than one media type,
an `$id` that is not a usable location, a relative link with no
`--link-base` to resolve it, or a protobuf option whose definition is
missing. In CI, pass `--strict` to make those fail the build.
