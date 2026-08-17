# Upstream List format

The Upstream List is a UTF-8, line-oriented text format owned by the Upstream List
module. Each active line decodes as a Host Selector, an Origin Selector, or a
Upstream List Warning.

```text
# Host Selectors: HTTP and HTTPS, any port
api.example.test
*.qa.example.test
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

## Selector recognition

An active line is accepted only when it satisfies the complete Host Selector or
Origin Selector form. Text that satisfies neither form becomes an Upstream List
Warning with the diagnostic
`invalid selector: expected a hostname or HTTP(S) origin`.

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

A wildcard never includes the parent hostname. List the parent separately when
it should also match. Any other use of `*` is invalid and produces an Upstream
List Warning.

The complete Host Selector text contains only its optional leading `*.` marker
and hostname. IPv6 literals use brackets in the source text.

Host Selectors:

- have no scheme, port, user information, path, query, or fragment;
- accept non-empty ASCII hostnames, including single-label names, IPv4 literals,
  and bracketed IPv6 literals;
- require conservative DNS or IP hostnames, reject underscores and trailing
  dots, and allow wildcard syntax only for DNS-name bases;
- match HTTP and HTTPS on any port;
- produce an HTTPS PAC route only while Trusted HTTPS Interception is enabled.

## Origin Selectors

An Origin Selector contains:

- an `http` or `https` scheme;
- a hostname;
- an optional port;
- optionally, the root path `/`.

User information is accepted and discarded because it is not part of origin
identity. Both an omitted path and an explicit root path are accepted. A
non-root path, query, or fragment makes the line invalid.

Wildcard syntax is invalid in an Origin Selector and produces an Upstream List
Warning requiring a Host Selector. Origin Selectors use the same conservative
DNS/IP hostname validation as Host Selectors.

An omitted port remains absent. An explicit port must be a decimal integer from
1 through 65535 and is stored with leading zeroes removed. An empty, zero, or
out-of-range explicit port produces an Upstream List Warning. Thus
`https://example.test:443` and `https://example.test:0443` identify the same
Origin Selector, while `https://example.test` remains a distinct selector with
no explicit port.

PAC Routing owns effective-port interpretation. An HTTP Origin Selector always
produces PAC routes. An HTTPS Origin Selector produces PAC routes only while
Trusted HTTPS Interception is enabled. Each active Origin Selector produces one
exact-port PAC Route using its explicit port or the scheme's default when the
port is omitted. Selectors with omitted and explicit default ports therefore
derive equivalent PAC Routes, which PAC Routing deduplicates.

## Shared validation and normalization

- Scheme and hostname letters are normalized to lowercase.
- Hostnames must be ASCII; Unicode hostnames must be written as punycode.
- User information is retained by neither selector type.
- No DNS lookup, IDNA conversion, or IP literal canonicalization is performed.

## Warnings and deduplication

Every invalid active line produces an Upstream List Warning in source order. Each
warning contains the source line number, the active text after whitespace and
comment removal, and the generic selector diagnostic. Invalid UTF-8 makes the
entire source unusable instead of producing a line warning.

Host Selectors and Origin Selectors are deduplicated independently by their
normalized source-level values, preserving the first occurrence within each
collection. Port presence is part of Origin Selector identity, so an omitted
port and an explicit default port remain distinct selectors. A document may
produce both valid selectors and warnings.
