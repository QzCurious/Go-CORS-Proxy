package upstreamlist

type HostnameMatch uint8

const (
	HostnameExact HostnameMatch = iota
	HostnameSingleLevel
	HostnameRecursive
)

// HostSelector selects an HTTP or HTTPS hostname on any port.
// Hostname never includes wildcard syntax.
type HostSelector struct {
	Hostname      string
	HostnameMatch HostnameMatch
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

// Entries is the normalized routing input owned by the Upstream List.
//
// The slices are private so a snapshot cannot be changed by a consumer after
// it has been decoded.  PAC Routing receives this value rather than learning
// about the source representation or the line-level diagnostics.
type Entries struct {
	hostSelectors   []HostSelector
	originSelectors []OriginSelector
}

// NewEntries constructs an immutable set of normalized Upstream List entries.
// Callers that construct entries directly are responsible for satisfying the
// same normalized value contract as Decode.  The constructor only takes
// ownership of copies of the supplied slices; it does not reinterpret values.
func NewEntries(hostSelectors []HostSelector, originSelectors []OriginSelector) Entries {
	return Entries{
		hostSelectors:   append([]HostSelector(nil), hostSelectors...),
		originSelectors: append([]OriginSelector(nil), originSelectors...),
	}
}

// HostSelectors returns a copy of the normalized Host Selector entries.
func (e Entries) HostSelectors() []HostSelector {
	return append([]HostSelector(nil), e.hostSelectors...)
}

// OriginSelectors returns a copy of the normalized Origin Selector entries.
func (e Entries) OriginSelectors() []OriginSelector {
	return append([]OriginSelector(nil), e.originSelectors...)
}

// Count returns the number of normalized entries in this set.
func (e Entries) Count() int {
	return len(e.hostSelectors) + len(e.originSelectors)
}

// HTTPSIntent reports whether at least one HTTPS Origin Selector is present.
// Host Selectors and HTTP Origin Selectors do not express HTTPS intent.
func (e Entries) HTTPSIntent() bool {
	for _, selector := range e.originSelectors {
		if selector.Scheme == "https" {
			return true
		}
	}
	return false
}

// Same reports whether two entry sets contain the same normalized selector
// sets. Source ordering does not affect entry identity.
func (e Entries) Same(other Entries) bool {
	return sameHostSelectors(e.hostSelectors, other.hostSelectors) &&
		sameOriginSelectors(e.originSelectors, other.originSelectors)
}

// UpstreamList is the decoded, normalized Upstream List. It owns both the
// active semantic entries and line-level warnings; consumers cannot mutate the
// list through the accessors.
type UpstreamList struct {
	entries  Entries
	warnings []Warning
}

// NewUpstreamList constructs an immutable decoded Upstream List value.
func NewUpstreamList(entries Entries, warnings []Warning) UpstreamList {
	return UpstreamList{
		entries:  NewEntries(entries.hostSelectors, entries.originSelectors),
		warnings: append([]Warning(nil), warnings...),
	}
}

// Entries returns the active normalized routing entries.
func (u UpstreamList) Entries() Entries {
	return NewEntries(u.entries.hostSelectors, u.entries.originSelectors)
}

// Warnings returns copies of line-level diagnostics for ignored source lines.
func (u UpstreamList) Warnings() []Warning {
	return append([]Warning(nil), u.warnings...)
}

// Count returns the number of active normalized routing entries.
func (u UpstreamList) Count() int {
	return u.entries.Count()
}

// HTTPSIntent reports whether the list contains at least one HTTPS Origin
// Selector.
func (u UpstreamList) HTTPSIntent() bool {
	return u.entries.HTTPSIntent()
}

// SameEntries reports whether two Upstream Lists contain the same normalized
// selector sets. Source ordering and warnings do not affect entry identity.
func SameEntries(left, right UpstreamList) bool {
	return left.entries.Same(right.entries)
}

func sameHostSelectors(left, right []HostSelector) bool {
	if len(left) != len(right) {
		return false
	}
	selectors := make(map[HostSelector]struct{}, len(left))
	for _, selector := range left {
		selectors[selector] = struct{}{}
	}
	for _, selector := range right {
		if _, ok := selectors[selector]; !ok {
			return false
		}
	}
	return true
}

func sameOriginSelectors(left, right []OriginSelector) bool {
	if len(left) != len(right) {
		return false
	}
	selectors := make(map[OriginSelector]struct{}, len(left))
	for _, selector := range left {
		selectors[selector] = struct{}{}
	}
	for _, selector := range right {
		if _, ok := selectors[selector]; !ok {
			return false
		}
	}
	return true
}
