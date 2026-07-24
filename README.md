# seamless-cors

English | [繁體中文](README.zh-TW.md)

`seamless-cors` is a local development and QA tool for testing browser requests
across origins without changing application URLs or adding CORS support to the
upstream server.

It uses a Proxy Auto-Configuration (PAC) file to route only the domains you
choose through the local gateway. HTTP works directly, with HTTPS support as an
opt-in feature.

> [!NOTE]
> This project is under active development.

> [!WARNING]
> seamless-cors is intended only for local development and QA environments.

## Demos

### Simple request

A cross-origin `GET` that the browser sends directly to an API without CORS
response headers.

![Simple request demo](demo/cors-simple-requests/cors-simple-requests.gif)

### Preflighted request

A JSON `POST` that first causes the browser to send an `OPTIONS` preflight
request.

![Preflighted request demo](demo/cors-preflighted-requests/cors-preflighted-requests.gif)

## Quick Start

Download the archive for your platform and processor from the
[latest GitHub release](https://github.com/QzCurious/seamless-cors/releases/latest),
then extract it.

Move `seamless-cors` (`seamless-cors.exe` on Windows) to a directory in your
`PATH`, then start the gateway:

```sh
seamless-cors start
```

On first start, seamless-cors creates `~/.seamless-cors/config.yaml` and
`~/.seamless-cors/domains.txt`.

Add the upstream hostnames or origins for which you want to enable CORS, one per
line, to `~/.seamless-cors/domains.txt`:

```text
api.dev.example.com
https://api.example.com:8443
*.test.example.com
```

The domain list is watched for changes while the gateway is running.

To enable HTTPS interception, set `ca-trusted: true` in `config.yaml` and
restart the gateway. Your operating system will ask you to approve installing
the seamless-cors development CA.

When you are finished, press `Ctrl+C` to stop the gateway and remove its PAC
settings.

## Platform support

The managed platform integrations currently target macOS and Windows.

## License

[MIT](LICENSE)
