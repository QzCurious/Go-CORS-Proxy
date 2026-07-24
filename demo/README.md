# Seamless CORS demonstrations

These standalone demonstrations cover two common CORS request scenarios:

- [Simple requests](cors-simple-requests/) use a cross-origin `GET` that the
  browser sends directly to the API.
- [Preflighted requests](cors-preflighted-requests/) use a JSON `POST` that
  makes the browser send an `OPTIONS` request first.

Both demonstrations run:

- a web app at `http://127.0.0.1:4000`
- a deliberately CORS-unaware API at
  `http://api.127.0.0.1.nip.io:4100`

The different hosts and ports make the browser request cross-origin. The API
does not return any `Access-Control-Allow-*` headers.

Run one demonstration at a time:

```sh
make demo-simple
make demo-preflight
```

Then open `http://127.0.0.1:4000` and follow the recording script in that
demonstration's directory.

The `nip.io` hostname resolves to `127.0.0.1` while avoiding browsers'
implicit proxy bypass for literal loopback hostnames. It requires working DNS.
