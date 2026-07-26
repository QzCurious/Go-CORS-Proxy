package liveconfig

// DomainListEntry is normalized routing data. When Scheme is empty, Hostname is
// hostname shorthand (including any leading "*.") and Port is empty. Otherwise
// Scheme is "http" or "https", Hostname contains no brackets or wildcard, and
// Port contains the explicit or normalized default port.
type DomainListEntry struct {
	Scheme   string
	Hostname string
	Port     string
}

func sameDomainListEntries(left, right []DomainListEntry) bool {
	if len(left) != len(right) {
		return false
	}
	entries := make(map[DomainListEntry]struct{}, len(left))
	for _, entry := range left {
		entries[entry] = struct{}{}
	}
	for _, entry := range right {
		if _, ok := entries[entry]; !ok {
			return false
		}
	}
	return true
}

func sameDomainListWarnings(left, right []DomainListWarning) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}
