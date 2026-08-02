package managedpac

import (
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
)

const footprintFileName = "seamless-cors.pac"

// PACURL constructs the stable Managed PAC URL for a PAC listener endpoint
// (for example, "127.0.0.1:49152") and a run-local URL version. The marker
// path is owned here so Gateway does not need to know the footprint filename.
func PACURL(pacListen string, version uint64) string {
	u := url.URL{
		Scheme: "http",
		Host:   pacListen,
		Path:   "/" + footprintFileName,
	}
	query := u.Query()
	query.Set("v", strconv.FormatUint(version, 10))
	u.RawQuery = query.Encode()
	return u.String()
}

// IsOwnedURL recognizes the stable loopback HTTP Managed PAC ownership marker.
// Ports, query versions, and any path prefix are intentionally ignored; the
// URL is owned when its final path component is the marker filename.
func IsOwnedURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return path.Base(u.EscapedPath()) == footprintFileName
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && path.Base(u.EscapedPath()) == footprintFileName
}
