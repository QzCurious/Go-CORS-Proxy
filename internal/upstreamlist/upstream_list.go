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

// OriginSelector selects one HTTP(S) origin. Port is the normalized effective
// port from 1 through 65535, including the scheme default when source text
// omits it.
type OriginSelector struct {
	Scheme   string
	Hostname string
	Port     uint16
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

// Merge forms one projection from source projections in the supplied order.
// Equivalent selectors keep their first occurrence. Warnings remain ordered
// with their source projections; callers retain source attribution separately.
func Merge(projections ...Projection) Projection {
	hostCount, originCount, warningCount := 0, 0, 0
	for _, projection := range projections {
		hostCount += len(projection.HostSelectors)
		originCount += len(projection.OriginSelectors)
		warningCount += len(projection.Warnings)
	}
	hosts := make([]HostSelector, 0, hostCount)
	origins := make([]OriginSelector, 0, originCount)
	warnings := make([]Warning, 0, warningCount)
	seenHosts := make(map[HostSelector]struct{}, hostCount)
	seenOrigins := make(map[OriginSelector]struct{}, originCount)
	for _, projection := range projections {
		for _, selector := range projection.HostSelectors {
			if _, ok := seenHosts[selector]; ok {
				continue
			}
			seenHosts[selector] = struct{}{}
			hosts = append(hosts, selector)
		}
		for _, selector := range projection.OriginSelectors {
			if _, ok := seenOrigins[selector]; ok {
				continue
			}
			seenOrigins[selector] = struct{}{}
			origins = append(origins, selector)
		}
		warnings = append(warnings, projection.Warnings...)
	}
	if len(hosts) == 0 {
		hosts = nil
	}
	if len(origins) == 0 {
		origins = nil
	}
	if len(warnings) == 0 {
		warnings = nil
	}
	return Projection{HostSelectors: hosts, OriginSelectors: origins, Warnings: warnings}
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
