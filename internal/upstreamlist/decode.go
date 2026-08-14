package upstreamlist

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

type InvalidEncodingError struct{}

func (*InvalidEncodingError) Error() string {
	return "invalid Upstream List: content must be UTF-8"
}

type parsedUpstreamList struct {
	HostSelectors   []HostSelector
	OriginSelectors []OriginSelector
	Warnings        []Warning
}

// decode parses the Upstream List format described in
// [upstream-list-format.md](upstream-list-format.md). It does not apply
// source-level deduplication. Invalid lines become warnings; source-level
// decoding failures remain errors.
func decode(data []byte) (parsedUpstreamList, error) {
	if !utf8.Valid(data) {
		return parsedUpstreamList{}, &InvalidEncodingError{}
	}

	var (
		hostSelectors   []HostSelector
		originSelectors []OriginSelector
		warnings        []Warning
	)

	// Decode raw selectors.
	for idx, line := range strings.Split(string(data), "\n") {
		lineNo := idx + 1
		selectorText := stripComment(line)
		if selectorText == "" {
			continue
		}

		if strings.Contains(selectorText, "://") {
			// Handle the line as an Origin Selector.
			selector, err := decodeOriginSelector(selectorText)
			if err != nil {
				warnings = append(warnings, Warning{
					Line:       lineNo,
					Text:       selectorText,
					Diagnostic: err.Error(),
				})
				continue
			}
			originSelectors = append(originSelectors, selector)
			continue
		}

		// Handle the line as a Host Selector.
		selector, err := decodeHostSelector(selectorText)
		if err != nil {
			warnings = append(warnings, Warning{
				Line:       lineNo,
				Text:       selectorText,
				Diagnostic: err.Error(),
			})
			continue
		}
		hostSelectors = append(hostSelectors, selector)
	}

	return parsedUpstreamList{
		HostSelectors:   hostSelectors,
		OriginSelectors: originSelectors,
		Warnings:        warnings,
	}, nil
}

func decodeOriginSelector(selectorText string) (OriginSelector, error) {
	u, err := url.ParseRequestURI(selectorText)
	if err != nil {
		return OriginSelector{}, err
	}
	if u.RequestURI() != "/" {
		return OriginSelector{}, fmt.Errorf("Origin Selector must contain only an HTTP(S) origin")
	}
	if strings.HasSuffix(u.Host, ":") {
		return OriginSelector{}, fmt.Errorf("Origin Selector port must not be empty")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return OriginSelector{}, fmt.Errorf("Origin Selector scheme must be http or https")
	}
	hostname, err := normalizedHostname(u)
	if err != nil {
		return OriginSelector{}, err
	}

	return OriginSelector{
		Scheme:   scheme,
		Hostname: hostname,
		Port:     normalizePort(u.Port()),
	}, nil
}

func normalizePort(port string) string {
	normalized := strings.TrimLeft(port, "0")
	if port != "" && normalized == "" {
		return "0"
	}
	return normalized
}

func decodeHostSelector(selectorText string) (HostSelector, error) {
	u, err := url.Parse("//" + selectorText)
	if err != nil {
		return HostSelector{}, err
	}

	parsedHostnameText := u.Hostname()
	if strings.Contains(parsedHostnameText, ":") {
		parsedHostnameText = "[" + parsedHostnameText + "]"
	}
	if selectorText != parsedHostnameText {
		return HostSelector{}, fmt.Errorf("Host Selector must contain only a hostname")
	}

	hostname, err := normalizedHostname(u)
	if err != nil {
		return HostSelector{}, err
	}
	hostnameMatch, hostname, err := decodeHostnameMatch(hostname)
	if err != nil {
		return HostSelector{}, err
	}
	return HostSelector{
		Hostname:      hostname,
		HostnameMatch: hostnameMatch,
	}, nil
}

func normalizedHostname(u *url.URL) (string, error) {
	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return "", fmt.Errorf("hostname is required")
	}
	for _, char := range hostname {
		if char > 127 {
			return "", fmt.Errorf("hostname must use ASCII or punycode")
		}
	}
	return hostname, nil
}

func decodeHostnameMatch(hostname string) (HostnameMatch, string, error) {
	match := HostnameExact
	switch {
	case strings.HasPrefix(hostname, "**."):
		match = HostnameRecursive
		hostname = strings.TrimPrefix(hostname, "**.")
	case strings.HasPrefix(hostname, "*."):
		match = HostnameSingleLevel
		hostname = strings.TrimPrefix(hostname, "*.")
	}
	if hostname == "" || strings.Contains(hostname, "*") {
		return 0, "", fmt.Errorf("wildcard must be * or ** followed by a hostname")
	}
	return match, hostname, nil
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
