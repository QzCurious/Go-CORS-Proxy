package upstreamlist

// DefaultContents is the exact initial contents disclosed and written by
// Gateway-owned Upstream List Creation.
const DefaultContents = `# One upstream host or origin per line.
#
# Host selectors always match HTTP on any port. Their HTTPS routes become
# active only when at least one valid HTTPS origin selector appears below:
# api.dev.example.com          # Exact hostname
# *.test.example.com           # One-label wildcard
# localhost                    # Local hostname
# 127.0.0.1                    # IPv4 address
# [::1]                        # IPv6 address
#
# Origin selectors match one HTTP(S) origin:
# https://api.example.com      # HTTPS with default port
# https://api.example.com:8443 # HTTPS with custom port
# http://localhost:3000        # Local HTTP origin
# http://127.0.0.1:3000        # IPv4 HTTP origin
# http://[::1]:3000            # IPv6 HTTP origin
`

// HostSelector selects an HTTP or HTTPS hostname on any port. Hostname never
// includes wildcard syntax. Wildcard selects exactly one leading label when
// true; otherwise the match is exact.
type HostSelector struct {
	Hostname string
	Wildcard bool
}

// OriginSelector selects one HTTP(S) origin. An empty Port means the source
// omitted the port; a non-empty Port is a normalized explicit port.
type OriginSelector struct {
	Scheme   string
	Hostname string
	Port     string
}

// Warning describes one ignored invalid Upstream List line.
type Warning struct {
	Line       int
	Text       string
	Diagnostic string
}

// Projection is the decoded, normalized Upstream List. Its zero value is the
// canonical Empty Upstream List.
type Projection struct {
	HostSelectors   []HostSelector
	OriginSelectors []OriginSelector
	Warnings        []Warning
}

// Project forms an Upstream List Projection from complete observed contents.
// A non-nil error means the returned Projection is unusable.
func Project(contents []byte) (Projection, error) {
	parsed, err := decode(contents)
	if err != nil {
		return Projection{}, err
	}
	return deduplicate(parsed), nil
}

func deduplicate(parsed parsedUpstreamList) Projection {
	var hosts []HostSelector
	seenHosts := make(map[HostSelector]struct{}, len(parsed.HostSelectors))
	for _, selector := range parsed.HostSelectors {
		if _, ok := seenHosts[selector]; !ok {
			seenHosts[selector] = struct{}{}
			hosts = append(hosts, selector)
		}
	}
	var origins []OriginSelector
	seenOrigins := make(map[OriginSelector]struct{}, len(parsed.OriginSelectors))
	for _, selector := range parsed.OriginSelectors {
		if _, ok := seenOrigins[selector]; !ok {
			seenOrigins[selector] = struct{}{}
			origins = append(origins, selector)
		}
	}
	return Projection{HostSelectors: hosts, OriginSelectors: origins, Warnings: parsed.Warnings}
}

// HTTPSIntent reports whether the projection contains at least one HTTPS Origin
// Selector.
func (u Projection) HTTPSIntent() bool {
	for _, selector := range u.OriginSelectors {
		if selector.Scheme == "https" {
			return true
		}
	}
	return false
}
