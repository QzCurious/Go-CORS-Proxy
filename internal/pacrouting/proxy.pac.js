/**
 * @typedef {Object} PACRoute
 * @property {'http' | 'https'} scheme
 * @property {string} hostname
 * @property {string | null} port
 * @property {boolean} wildcard
 */

/**
 * @typedef {Object} PACRequest
 * @property {'http' | 'https'} scheme
 * @property {string} hostname
 * @property {string} port
 */

/**
 * Configuration injected before this program in the Generated PAC.
 *
 * @type {{
 *   proxy: string,
 *   routes: PACRoute[],
 * }}
 */
var VIEW_BAG;

var proxy = 'PROXY ' + VIEW_BAG.proxy;

/** @type {PACRoute[]} */
var routes = VIEW_BAG.routes;

var defaultPorts = {
  http: '80',
  https: '443'
};

/**
 * Determines how a request should be routed.
 *
 * @param {string} url The requested URL.
 * @param {string} hostname The hostname extracted from the requested URL.
 * @returns {'DIRECT' | `PROXY ${string}`}
 * @see https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Proxy_servers_and_tunneling/Proxy_Auto-Configuration_PAC_file
 */
function FindProxyForURL(url, hostname) {
  var request = normalizeRequest(url, hostname);
  if (request == null) return 'DIRECT';

  for (var i = 0; i < routes.length; i++) {
    if (matchesRoute(routes[i], request)) return proxy;
  }
  return 'DIRECT';
}

/**
 * @param {string} url
 * @param {string} hostname
 * @returns {PACRequest | null}
 */
function normalizeRequest(url, hostname) {
  url = url.toLowerCase();
  hostname = hostname.toLowerCase();

  var schemeEnd = url.indexOf('://');
  var scheme = url.substring(0, schemeEnd);
  if (scheme != 'http' && scheme != 'https') return null;

  var authorityStart = schemeEnd + 3;
  var authorityEnd = url.length;

  for (var i = authorityStart; i < url.length; i++) {
    var character = url.charAt(i);
    if (character == '/' || character == '?' || character == '#') {
      authorityEnd = i;
      break;
    }
  }

  var portStart = url.lastIndexOf(hostname, authorityEnd) + hostname.length;
  if (url.charAt(portStart) == ']') portStart++;
  var port = defaultPorts[scheme];
  if (url.charAt(portStart) == ':') {
    port = url.substring(portStart + 1, authorityEnd);
  }
  return {scheme: scheme, hostname: hostname, port: port};
}

/**
 * @param {PACRoute} route
 * @param {PACRequest} request
 * @returns {boolean}
 */
function matchesRoute(route, request) {
  if (request.scheme != route.scheme) return false;
  if (route.port != null && request.port != route.port) return false;

  // Exact matches include only the configured hostname itself.
  if (!route.wildcard) return request.hostname == route.hostname;

  var suffix = '.' + route.hostname;
  if (request.hostname.length <= suffix.length) return false;
  if (request.hostname.substring(request.hostname.length - suffix.length) != suffix) return false;

  // Single-level matches include exactly one label before the configured hostname.
  var prefix = request.hostname.substring(0, request.hostname.length - suffix.length);
  return prefix.indexOf('.') == -1;
}
