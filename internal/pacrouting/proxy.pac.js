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

/**
 * Determines how a request should be routed.
 *
 * @param {string} url The requested URL.
 * @param {string} host The hostname extracted from the requested URL.
 * @returns {'DIRECT' | `PROXY ${string}`}
 * @see https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Proxy_servers_and_tunneling/Proxy_Auto-Configuration_PAC_file
 */
function FindProxyForURL(url, host) {
  var request = normalizeRequest(url, host);
  if (request == null) return 'DIRECT';

  for (var i = 0; i < routes.length; i++) {
    if (matchesRoute(routes[i], request)) return proxy;
  }
  return 'DIRECT';
}

/**
 * @param {string} url
 * @param {string} host
 * @returns {PACRequest | null}
 */
function normalizeRequest(url, host) {
  url = url.toLowerCase();
  host = host.toLowerCase();

  var schemeEnd = url.indexOf(':');
  if (schemeEnd <= 0 || url.substring(schemeEnd, schemeEnd + 3) != '://') return null;

  var scheme = url.substring(0, schemeEnd);
  if (scheme != 'http' && scheme != 'https') return null;
  if (host == '') return null;

  var authorityStart = schemeEnd + 3;
  var authorityEnd = url.length;
  for (var i = authorityStart; i < url.length; i++) {
    var character = url.charAt(i);
    if (character == '/' || character == '?' || character == '#') {
      authorityEnd = i;
      break;
    }
  }
  var authority = url.substring(authorityStart, authorityEnd);
  var identityEnd = authority.lastIndexOf('@');
  if (identityEnd != -1) authority = authority.substring(identityEnd + 1);
  if (authority == '') return null;

  var portText = null;
  if (authority.charAt(0) == '[') {
    var bracketEnd = authority.indexOf(']');
    if (bracketEnd <= 1) return null;
    var ipv6Remainder = authority.substring(bracketEnd + 1);
    if (ipv6Remainder != '') {
      if (ipv6Remainder.charAt(0) != ':') return null;
      portText = ipv6Remainder.substring(1);
    }
  } else {
    var portSeparator = authority.lastIndexOf(':');
    if (portSeparator != -1) portText = authority.substring(portSeparator + 1);
  }

  var port = defaultPort(scheme);
  if (portText != null) {
    port = normalizeExplicitPort(portText);
    if (port == null) return null;
  }
  return {scheme: scheme, hostname: host, port: port};
}

/**
 * @param {string} scheme
 * @returns {string}
 */
function defaultPort(scheme) {
  return scheme == 'https' ? '443' : '80';
}

/**
 * @param {string} portText
 * @returns {string | null}
 */
function normalizeExplicitPort(portText) {
  if (portText == '') return null;
  for (var i = 0; i < portText.length; i++) {
    var character = portText.charAt(i);
    if (character < '0' || character > '9') return null;
  }
  var port = parseInt(portText, 10);
  if (port < 1 || port > 65535) return null;
  return String(port);
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
