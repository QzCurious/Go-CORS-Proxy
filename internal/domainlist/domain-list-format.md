# Domain List format

The Domain List is a UTF-8, line-oriented text format owned by the Domain List
module. Each active line decodes as a Host Selector, an Origin Selector, or a
Domain List Warning.

```text
# Host Selectors: HTTP and HTTPS, any port
api.example.test
*.qa.example.test
**.dev.example.test
localhost
[::1]

# Origin Selectors: one HTTP(S) origin
https://api.example.test
http://localhost:3000
http://[::1]:3000
```

## Lines and comments

- LF and CRLF line endings are accepted.
- Leading and trailing whitespace is ignored.
- Blank lines are ignored.
- `#` begins a comment at the start of a trimmed line or when preceded by a
  space or tab.
- A `#` without preceding whitespace is part of the selector and makes that
  line invalid because fragments are unsupported.

## Classification

An active line containing `://` is decoded as an Origin Selector. Every other
active line is decoded as a Host Selector. A line that does not satisfy its
selected form becomes a Domain List Warning; it is not retried as the other
form.

Consequently:

- `example.test` is a Host Selector.
- `https://example.test` is an Origin Selector.
- `example.test:8080` is invalid because Host Selectors have no port.
- `//example.test` is invalid because network-path references are unsupported.
- `ftp://example.test` is invalid because Origin Selectors are HTTP(S) only.

## Host Selectors

A Host Selector contains only a hostname and an optional leading hostname
match marker:

- `example.test` matches exactly that hostname.
- `*.example.test` matches exactly one leading subdomain label.
- `**.example.test` matches one or more leading subdomain labels.

A wildcard never includes the parent hostname. List the parent separately when
it should also match. Any other use of `*` is invalid.

The complete Host Selector text must equal the hostname representation
produced by URL parsing. Brackets are restored around a parsed IPv6 hostname
for this comparison. The leading `*.` and `**.` markers remain part of the
parsed hostname representation and are interpreted only after this shape
validation.

Host Selectors:

- have no scheme, port, user information, path, query, or fragment;
- accept non-empty ASCII hostnames as parsed by Go's `net/url`, including
  single-label names, IPv4 literals, and bracketed IPv6 literals;
- match HTTP and HTTPS on any port;
- produce an HTTPS PAC route only while Trusted HTTPS Interception is enabled.

## Origin Selectors

An Origin Selector contains:

- an `http` or `https` scheme;
- a hostname;
- an optional port;
- optionally, the root path `/`.

User information is accepted and discarded because it is not part of origin
identity. An Origin Selector is parsed as an absolute request URI and is valid
only when its request target is `/`. Both an omitted path and an explicit root
path produce that target. A non-root path or query produces a different target,
and a fragment fails request-URI parsing, so each makes the line invalid.

Host Selector wildcard semantics are never applied to an Origin Selector.
For example, if Go's `net/url` accepts `https://*.example.test`, the `*` remains
a literal hostname character rather than becoming a wildcard match.

An omitted port remains absent. An explicit port is stored as a normalized
decimal string with leading zeroes removed. A port delimiter without a port
produces a Domain List Warning. Thus `https://example.test:443` and
`https://example.test:0443` identify the same Origin Selector, while
`https://example.test` remains a distinct selector with no explicit port.

PAC Routing owns effective-port interpretation. An HTTP Origin Selector always
produces PAC routes. An HTTPS Origin Selector produces PAC routes only while
Trusted HTTPS Interception is enabled. A selector with an omitted or explicit
default port produces both implicit-port and explicit-port Origin Routes so
either equivalent PAC URL representation matches. A non-default-port selector
produces one Origin Route. Equivalent derived Origin Routes are deduplicated.

## Shared parsing and normalization

- URL-shaped decoding uses Go's `net/url`: Host Selectors are parsed as
  schemeless URL authorities, while Origin Selectors are parsed as absolute
  request URIs.
- Scheme and hostname letters are normalized to lowercase.
- Hostnames must be ASCII; Unicode hostnames must be written as punycode.
- User information is retained by neither selector type.
- No DNS lookup, DNS label-syntax validation, IDNA conversion, IP literal
  canonicalization, or explicit browser port-range validation is performed.

## Warnings and deduplication

Every invalid active line produces a Domain List Warning in source order. Each
warning contains the source line number, the active text after whitespace and
comment removal, and a diagnostic. Invalid UTF-8 makes the entire source
unusable instead of producing a line warning.

Host Selectors and Origin Selectors are deduplicated independently by their
normalized source-level values, preserving the first occurrence within each
collection. Port presence is part of Origin Selector identity, so an omitted
port and an explicit default port remain distinct selectors. A document may
produce both valid selectors and warnings.
