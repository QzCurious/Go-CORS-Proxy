package domainlist

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Entry is one normalized Domain List rule.
type Entry struct {
	// Raw is the original non-comment Domain List text.
	Raw string

	// Scheme restricts matches to "http" or "https" when set.
	Scheme string

	// Host is the normalized lowercase hostname, or leading wildcard hostname.
	Host string

	// Port restricts matches to a specific port when set.
	Port string

	// Wildcard reports whether Host starts with "*.".
	Wildcard bool
}

type LineError struct {
	Line int
	Text string
	Err  error
}

func (e LineError) Error() string {
	return fmt.Sprintf("line %d: %s: %v", e.Line, e.Text, e.Err)
}

func ParseList(contents string) ([]Entry, []LineError) {
	var entries []Entry
	var errs []LineError
	seen := map[entryKey]struct{}{}

	for idx, line := range strings.Split(contents, "\n") {
		lineNo := idx + 1
		text := stripComment(line)
		if text == "" {
			continue
		}
		entry, err := ParseEntry(text)
		if err != nil {
			errs = append(errs, LineError{Line: lineNo, Text: text, Err: err})
			continue
		}
		key := keyForEntry(entry)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		entries = append(entries, entry)
	}
	return entries, errs
}

type entryKey struct {
	Scheme   string
	Host     string
	Port     string
	Wildcard bool
}

func keyForEntry(entry Entry) entryKey {
	return entryKey{
		Scheme:   entry.Scheme,
		Host:     entry.Host,
		Port:     entry.Port,
		Wildcard: entry.Wildcard,
	}
}

func SameEntries(left, right []Entry) bool {
	if len(left) != len(right) {
		return false
	}
	entries := make(map[entryKey]struct{}, len(left))
	for _, entry := range left {
		entries[keyForEntry(entry)] = struct{}{}
	}
	for _, entry := range right {
		if _, ok := entries[keyForEntry(entry)]; !ok {
			return false
		}
	}
	return true
}

func ParseEntry(text string) (Entry, error) {
	text = strings.TrimSpace(text)
	if strings.Contains(text, "://") {
		return parseOrigin(text)
	}
	return parseHostname(text)
}

func parseOrigin(text string) (Entry, error) {
	u, err := url.Parse(text)
	if err != nil {
		return Entry{}, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Entry{}, fmt.Errorf("origin scheme must be http or https")
	}
	if u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return Entry{}, fmt.Errorf("entry must be an origin without path, query, or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, "*") {
		return Entry{}, fmt.Errorf("wildcards require hostname shorthand")
	}
	port := u.Port()
	if port == "" {
		port = defaultPort(u.Scheme)
	}
	return Entry{Raw: text, Scheme: u.Scheme, Host: host, Port: port}, nil
}

func parseHostname(text string) (Entry, error) {
	if strings.Contains(text, "/") || strings.Contains(text, ":") {
		return Entry{}, fmt.Errorf("host shorthand must not include scheme, port, path, or IPv6")
	}
	host := strings.TrimSuffix(strings.ToLower(text), ".")
	if host == "" {
		return Entry{}, fmt.Errorf("host is required")
	}
	if strings.Contains(host, "#") || strings.ContainsAny(host, " \t") {
		return Entry{}, fmt.Errorf("host contains invalid characters")
	}
	wildcard := strings.HasPrefix(host, "*.")
	if strings.Contains(strings.TrimPrefix(host, "*."), "*") {
		return Entry{}, fmt.Errorf("wildcard must be a single leading label")
	}
	if wildcard && strings.Count(host, ".") < 2 {
		return Entry{}, fmt.Errorf("wildcard must include a concrete parent domain")
	}
	if !wildcard && net.ParseIP(host) != nil && strings.Contains(host, ":") {
		return Entry{}, fmt.Errorf("IPv6 entries require full origin syntax")
	}
	return Entry{Raw: text, Host: host, Wildcard: wildcard}, nil
}

func stripComment(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		return ""
	}
	for idx := 1; idx < len(line); idx++ {
		if line[idx] == '#' && (line[idx-1] == ' ' || line[idx-1] == '\t') {
			line = line[:idx]
			break
		}
	}
	return strings.TrimSpace(line)
}

func defaultPort(scheme string) string {
	switch scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}
