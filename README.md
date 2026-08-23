# seamless-cors

English | [繁體中文](README.zh-TW.md)

`seamless-cors` is a local development and QA tool for testing browser requests
across origins without changing application URLs or adding CORS support to the
upstream server.

It uses a Proxy Auto-Configuration (PAC) file to route only the upstreams you
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

![Simple request demo](https://raw.githubusercontent.com/QzCurious/seamless-cors/HEAD/demo/cors-simple-requests/cors-simple-requests.gif)

### Preflighted request

A JSON `POST` that first causes the browser to send an `OPTIONS` preflight
request.

![Preflighted request demo](https://raw.githubusercontent.com/QzCurious/seamless-cors/HEAD/demo/cors-preflighted-requests/cors-preflighted-requests.gif)

## Installation

### npm

Install the command globally:

```sh
npm install --global seamless-cors
```

### Go install

With Go 1.23.1 or later installed, build and install the latest release:

```sh
go install github.com/QzCurious/seamless-cors/cmd/seamless-cors@latest
```

Ensure the Go binary directory (`$(go env GOPATH)/bin`) is on your `PATH`.

## Quick Start

Run directly with `npx` without installing the command globally:

```sh
npx seamless-cors start
```

Or, if installed, invoke the command directly:

```sh
seamless-cors start
```

On first start, seamless-cors offers to create the Global Upstream List at
`seamless-cors/upstreams.txt` under your platform's XDG configuration home. For
example, the default is `~/.config/seamless-cors/upstreams.txt` on Linux and
`~/Library/Application Support/seamless-cors/upstreams.txt` on macOS. The
former `~/.seamless-cors/upstreams.txt` path is not read or migrated.

Add the upstream hostnames or origins for which you want to enable CORS, one per
line, to that Global Upstream List:

```text
api.dev.example.com
https://api.example.com:8443
*.test.example.com
```

You can also create an optional `upstreams.txt` directly in the directory from
which you run `seamless-cors start`. The Global and Directory Upstream Lists are
watched and parsed independently, then merged without precedence; duplicate
normalized entries are active only once. The active directory is fixed until
the gateway is stopped and started again.

An explicit `https://` Origin Selector in either source expresses HTTPS Intent.
A Host Selector does not express HTTPS Intent, but it serves both HTTP and HTTPS
whenever HTTPS Readiness is ready.

To make HTTPS Readiness ready, run `seamless-cors install` and approve the
operating-system prompt for the seamless-cors development CA. If the gateway is
already running, HTTPS interception activates immediately. Without an
Installed User CA, the gateway keeps serving HTTP and warns when the upstream
list expresses HTTPS Intent.

Running `seamless-cors install` again can renew the UserCA without interrupting
HTTPS traffic. `seamless-cors uninstall` removes every seamless-cors UserCA;
when HTTPS interception is active, the command asks for confirmation and then
disables HTTPS immediately while the gateway continues serving HTTP.

When you are finished, press `Ctrl+C` to stop the gateway and remove its PAC
settings.

## Platform support

The managed platform integrations currently target macOS and Windows.

## License

[MIT](LICENSE)
