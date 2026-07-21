package pacrouting

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"

	"seamless-cors/internal/domainlist"
)

type Policy struct {
	entries []domainlist.Entry
}

type Options struct {
	ProxyListen string
	CATrusted   bool
	Entries     []domainlist.Entry
}

func NewPolicy(entries []domainlist.Entry) Policy {
	return Policy{entries: append([]domainlist.Entry(nil), entries...)}
}

func (p Policy) Entries() []domainlist.Entry {
	return append([]domainlist.Entry(nil), p.entries...)
}

func (p Policy) Matches(scheme, host, port string) bool {
	for _, entry := range p.entries {
		if entryMatches(entry, scheme, host, port) {
			return true
		}
	}
	return false
}

func Handler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
}

type DynamicHandler struct {
	body atomic.Value
}

func NewDynamicHandler(body string) *DynamicHandler {
	h := &DynamicHandler{}
	h.Set(body)
	return h
}

func (h *DynamicHandler) Set(body string) {
	h.body.Store(body)
}

func (h *DynamicHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.body.Load().(string)))
}

func Generate(opts Options) string {
	buckets := canonicalRouteBuckets(deriveRouteBuckets(NewPolicy(opts.Entries), opts.CATrusted))
	return fmt.Sprintf(
		pacTemplate,
		pacJSONLiteral("PROXY "+opts.ProxyListen),
		pacJSONLiteral(buckets.exactHosts),
		pacJSONLiteral(buckets.wildcardParents),
		pacJSONLiteral(buckets.origins),
	)
}

func RouteSetFingerprint(entries []domainlist.Entry, caTrusted bool) string {
	buckets := canonicalRouteBuckets(deriveRouteBuckets(NewPolicy(entries), caTrusted))
	data, err := json.Marshal(routeSetFingerprint{
		ExactHosts:      buckets.exactHosts,
		WildcardParents: buckets.wildcardParents,
		Origins:         buckets.origins,
	})
	if err != nil {
		panic(err)
	}
	return string(data)
}

const pacTemplate = `var proxy = %s;
var exactHosts = %s;
var wildcardParents = %s;
var origins = %s;

function FindProxyForURL(url, host) {
  host = host.toLowerCase();
  var scheme = url.substring(0, url.indexOf(':')).toLowerCase();
  var urlPort = portForURL(url, scheme);
  if (matchesOrigin(scheme, host, urlPort)) return proxy;
  if (matchesHostRoute(exactHosts, scheme, host)) return proxy;
  if (matchesWildcardRoute(wildcardParents, scheme, host)) return proxy;
  return 'DIRECT';
}

function portForURL(url, scheme) {
  var parsedPort = url.match(/^[a-zA-Z]+:\/\/\[[^\]]+\]:(\d+)/) || url.match(/^[a-zA-Z]+:\/\/[^\/:]+:(\d+)/);
  return parsedPort ? parsedPort[1] : (scheme == 'https' ? '443' : '80');
}

function routeAllows(route, scheme) {
  return (scheme == 'http' && route.http) || (scheme == 'https' && route.https);
}

function matchesHostRoute(routes, scheme, host) {
  for (var i = 0; i < routes.length; i++) {
    if (host == routes[i].host && routeAllows(routes[i], scheme)) return true;
  }
  return false;
}

function matchesWildcardRoute(routes, scheme, host) {
  for (var i = 0; i < routes.length; i++) {
    var suffix = '.' + routes[i].host;
    if (!routeAllows(routes[i], scheme) || !dnsDomainIs(host, suffix)) continue;
    var prefix = host.substring(0, host.length - suffix.length);
    if (prefix != '' && prefix.indexOf('.') == -1) return true;
  }
  return false;
}

function matchesOrigin(scheme, host, port) {
  for (var i = 0; i < origins.length; i++) {
    var route = origins[i];
    if (scheme == route.scheme && host == route.host && port == route.port) return true;
  }
  return false;
}
`

type routeBuckets struct {
	exactHosts      []hostRoute
	wildcardParents []hostRoute
	origins         []originRoute
}

type hostRoute struct {
	Host       string `json:"host"`
	AllowHTTP  bool   `json:"http"`
	AllowHTTPS bool   `json:"https"`
}

type originRoute struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   string `json:"port"`
}

type routeSetFingerprint struct {
	ExactHosts      []hostRoute   `json:"exactHosts"`
	WildcardParents []hostRoute   `json:"wildcardParents"`
	Origins         []originRoute `json:"origins"`
}

func deriveRouteBuckets(policy Policy, caTrusted bool) routeBuckets {
	buckets := routeBuckets{
		exactHosts:      []hostRoute{},
		wildcardParents: []hostRoute{},
		origins:         []originRoute{},
	}
	for _, entry := range policy.entries {
		if entry.Scheme != "" {
			if entry.Scheme == "https" && !caTrusted {
				continue
			}
			buckets.origins = append(buckets.origins, originRoute{
				Scheme: entry.Scheme,
				Host:   entry.Host,
				Port:   entry.Port,
			})
			continue
		}
		route := hostRoute{
			Host:       entry.Host,
			AllowHTTP:  true,
			AllowHTTPS: caTrusted,
		}
		if entry.Wildcard {
			route.Host = strings.TrimPrefix(entry.Host, "*.")
			buckets.wildcardParents = append(buckets.wildcardParents, route)
			continue
		}
		buckets.exactHosts = append(buckets.exactHosts, route)
	}
	return buckets
}

func canonicalRouteBuckets(buckets routeBuckets) routeBuckets {
	sort.Slice(buckets.exactHosts, func(i, j int) bool {
		return hostRouteKey(buckets.exactHosts[i]) < hostRouteKey(buckets.exactHosts[j])
	})
	sort.Slice(buckets.wildcardParents, func(i, j int) bool {
		return hostRouteKey(buckets.wildcardParents[i]) < hostRouteKey(buckets.wildcardParents[j])
	})
	sort.Slice(buckets.origins, func(i, j int) bool {
		return originRouteKey(buckets.origins[i]) < originRouteKey(buckets.origins[j])
	})
	buckets.exactHosts = dedupeHostRoutes(buckets.exactHosts)
	buckets.wildcardParents = dedupeHostRoutes(buckets.wildcardParents)
	buckets.origins = dedupeOriginRoutes(buckets.origins)
	return buckets
}

func dedupeHostRoutes(routes []hostRoute) []hostRoute {
	out := routes[:0]
	var last string
	for idx, route := range routes {
		key := hostRouteKey(route)
		if idx > 0 && key == last {
			continue
		}
		out = append(out, route)
		last = key
	}
	return out
}

func dedupeOriginRoutes(routes []originRoute) []originRoute {
	out := routes[:0]
	var last string
	for idx, route := range routes {
		key := originRouteKey(route)
		if idx > 0 && key == last {
			continue
		}
		out = append(out, route)
		last = key
	}
	return out
}

func hostRouteKey(route hostRoute) string {
	return fmt.Sprintf("%s|%t|%t", route.Host, route.AllowHTTP, route.AllowHTTPS)
}

func originRouteKey(route originRoute) string {
	return route.Scheme + "|" + route.Host + "|" + route.Port
}

func entryMatches(entry domainlist.Entry, scheme, host, port string) bool {
	if entry.Scheme != "" && !strings.EqualFold(entry.Scheme, scheme) {
		return false
	}
	if entry.Port != "" && entry.Port != port {
		return false
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	want := strings.ToLower(entry.Host)
	if !entry.Wildcard {
		return host == want
	}
	suffix := strings.TrimPrefix(want, "*.")
	if !strings.HasSuffix(host, "."+suffix) {
		return false
	}
	prefix := strings.TrimSuffix(host, "."+suffix)
	return prefix != "" && !strings.Contains(prefix, ".")
}

type pacLiteral interface {
	string | []hostRoute | []originRoute
}

func pacJSONLiteral[T pacLiteral](value T) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
