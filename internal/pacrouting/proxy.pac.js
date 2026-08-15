/**
 * @typedef {Object} HostRoute
 * @property {'http' | 'https'} Scheme
 * @property {string} Hostname
 * @property {boolean} Wildcard
 */

/**
 * Configuration injected before this program in the Generated PAC.
 *
 * @type {{
 *   Proxy: string,
 *   HostRoutes: HostRoute[],
 *   OriginRoutes: string[],
 * }}
 */
var VIEW_BAG;

var proxy = 'PROXY ' + VIEW_BAG.Proxy;

/** @type {HostRoute[]} */
var hostRoutes = VIEW_BAG.HostRoutes;

/** @type {string[]} */
var originRoutes = VIEW_BAG.OriginRoutes;

/**
 * Determines how a request should be routed.
 *
 * @param {string} url The requested URL.
 * @param {string} host The hostname extracted from the requested URL.
 * @returns {'DIRECT' | `PROXY ${string}`}
 * @see https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Proxy_servers_and_tunneling/Proxy_Auto-Configuration_PAC_file
 */
function FindProxyForURL(url, host) {
  url = url.toLowerCase();
  host = host.toLowerCase();

  for (var i = 0; i < originRoutes.length; i++) {
    if (matchesOriginRoute(url, originRoutes[i])) return proxy;
  }

  var scheme = url.substring(0, url.indexOf(':'));
  for (var j = 0; j < hostRoutes.length; j++) {
    if (matchesHostRoute(hostRoutes[j], scheme, host)) return proxy;
  }
  return 'DIRECT';
}

/**
 * @param {string} url
 * @param {string} originRoute
 * @returns {boolean}
 */
function matchesOriginRoute(url, originRoute) {
  return url == originRoute || url.indexOf(originRoute + '/') == 0;
}

/**
 * @param {HostRoute} route
 * @param {string} scheme
 * @param {string} host
 * @returns {boolean}
 */
function matchesHostRoute(route, scheme, host) {
  if (scheme != route.Scheme) return false;

  // Exact matches include only the configured hostname itself.
  if (!route.Wildcard) return host == route.Hostname;

  var suffix = '.' + route.Hostname;
  if (!dnsDomainIs(host, suffix)) return false;

  // Single-level matches include exactly one label before the configured hostname.
  var prefix = host.substring(0, host.length - suffix.length);
  return prefix.indexOf('.') == -1;
}
