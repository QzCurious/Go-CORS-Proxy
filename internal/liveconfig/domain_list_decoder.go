package liveconfig

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// DomainListWarning describes one ignored invalid Domain List line.
type DomainListWarning struct {
	Line       int
	Text       string
	Diagnostic string
}

type domainListDecodeResult struct {
	entries  []DomainListEntry
	warnings []DomainListWarning
}

// decodeDomainList owns the Domain List text format described in
// domain-list-format.md. Invalid lines become warnings; source-level decoding
// failures remain errors.
func decodeDomainList(data []byte) (domainListDecodeResult, error) {
	if !utf8.Valid(data) {
		return domainListDecodeResult{}, fmt.Errorf("invalid Domain List: content must be UTF-8")
	}
	return decodeDomainListText(string(data)), nil
}

func decodeDomainListText(contents string) domainListDecodeResult {
	var entries []DomainListEntry
	var warnings []DomainListWarning
	seen := map[DomainListEntry]struct{}{}

	for idx, line := range strings.Split(contents, "\n") {
		lineNo := idx + 1
		text := stripDomainListComment(line)
		if text == "" {
			continue
		}
		value, err := decodeDomainListEntry(text)
		if err != nil {
			warnings = append(warnings, DomainListWarning{
				Line:       lineNo,
				Text:       text,
				Diagnostic: err.Error(),
			})
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		entries = append(entries, value)
	}
	return domainListDecodeResult{entries: entries, warnings: warnings}
}

func decodeDomainListEntry(text string) (DomainListEntry, error) {
	text = strings.TrimSpace(text)
	if !strings.Contains(text, "://") && !strings.HasPrefix(text, "//") {
		text = "//" + text
	}

	u, err := url.Parse(text)
	if err != nil {
		return DomainListEntry{}, err
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "" && scheme != "http" && scheme != "https" {
		return DomainListEntry{}, fmt.Errorf("scheme must be http, https, or omitted")
	}
	if strings.Count(u.Host, ":") > 1 && !strings.HasPrefix(u.Host, "[") {
		return DomainListEntry{}, fmt.Errorf("IPv6 hostname must use brackets")
	}
	if u.Hostname() == "" {
		return DomainListEntry{}, fmt.Errorf("hostname is required")
	}
	if u.Path != "" && u.Path != "/" {
		return DomainListEntry{}, fmt.Errorf("path must be empty or /")
	}
	if u.RawQuery != "" {
		return DomainListEntry{}, fmt.Errorf("query must be empty")
	}
	if u.Fragment != "" {
		return DomainListEntry{}, fmt.Errorf("fragment must be empty")
	}

	hostname := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if hostname == "" {
		return DomainListEntry{}, fmt.Errorf("hostname is required")
	}
	hostMatch, hostname, err := decodeHostMatch(hostname)
	if err != nil {
		return DomainListEntry{}, err
	}
	return DomainListEntry{
		Scheme:    scheme,
		Hostname:  hostname,
		Port:      u.Port(),
		HostMatch: hostMatch,
	}, nil
}

func decodeHostMatch(hostname string) (HostMatch, string, error) {
	match := HostExact
	switch {
	case strings.HasPrefix(hostname, "**."):
		match = HostRecursive
		hostname = strings.TrimPrefix(hostname, "**.")
	case strings.HasPrefix(hostname, "*."):
		match = HostSingleLevel
		hostname = strings.TrimPrefix(hostname, "*.")
	}
	if hostname == "" || strings.Contains(hostname, "*") {
		return 0, "", fmt.Errorf("wildcard must be * or ** followed by a hostname")
	}
	return match, hostname, nil
}

func stripDomainListComment(line string) string {
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
