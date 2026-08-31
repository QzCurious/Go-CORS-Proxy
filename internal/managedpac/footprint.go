package managedpac

import (
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
)

const footprintFileName = "seamless-cors.pac"

// pacURL constructs the stable Managed PAC URL for a PAC listener endpoint
// (for example, "127.0.0.1:49152") and a run-local delivery generation. The marker
// path is owned here so Gateway does not need to know the footprint filename.
func pacURL(pacListen string, generation uint64) string {
	u := url.URL{
		Scheme: "http",
		Host:   pacListen,
		Path:   "/" + footprintFileName,
	}
	query := u.Query()
	query.Set("v", strconv.FormatUint(generation, 10))
	u.RawQuery = query.Encode()
	return u.String()
}

// isOwnedURL recognizes the stable loopback HTTP Managed PAC ownership marker.
// Ports, query generations, and any path prefix are intentionally ignored; the
// URL is owned when its final path component is the marker filename.
func isOwnedURL(raw string) bool {
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
