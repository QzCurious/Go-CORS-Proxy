# User CA library options

## Decision

Keep the current small, project-owned authority and trust-store adapters. No mature Go library evaluated here satisfies the required combination of:

- one locally persisted self-signed CA pair with project-specific validation and atomic publication;
- the **current user's** root store on both macOS and Windows;
- enumeration of all certificates matching the ownership footprint;
- exact removal by SHA-1 fingerprint; and
- caller-visible classification of a denied/cancelled macOS trust prompt.

`github.com/smallstep/truststore` is the closest reusable trust-store package, but adopting it would replace only part of install/remove while losing required behavior. The standard-library authority implementation is already narrower than the abstractions offered by CA-server or automatic-TLS libraries.

## Capability comparison

| Candidate | CA generation, loading, persistence | macOS / Windows trust mutation | Enumerate and remove project footprint | Approval-denial classification | Fit |
|---|---|---|---|---|---|
| [`smallstep/truststore`](https://github.com/smallstep/truststore) | No generation or key persistence; only certificate read/save helpers | `Install`/`Uninstall` exist, but macOS uses `sudo` and `/Library/Keychains/System.keychain`, not the login keychain; Windows opens current-user `ROOT` through CryptoAPI | No public enumeration API; Windows uninstall matches serial number, not fingerprint | Exposes a generic `CmdError` with command output, but no denied/cancelled semantic error; APIs accept no context | Closest package, but not a drop-in |
| [`FiloSottile/mkcert`](https://github.com/FiloSottile/mkcert) | Creates and loads its own CA under `CAROOT` | Supports macOS and Windows, but macOS installs in the system keychain | Internal command code; no reusable package API; Windows removal scans by serial number | Process-oriented fatal errors, no caller-owned classification | Excellent reference implementation, unsuitable as a dependency |
| [`caddyserver/certmagic`](https://github.com/caddyserver/certmagic) | Manages issued TLS certificates and persistent storage; it expects an ACME-compatible CA or custom `Issuer` | None | None | None | Solves automatic server-certificate issuance/renewal, not local root ownership |
| [`smallstep/certificates`](https://github.com/smallstep/certificates) | Full online X.509/SSH CA server with provisioners, policy, database, and APIs | None | None | None | Far larger and differently scoped than one local signing pair |
| [`smallstep/certinfo`](https://github.com/smallstep/certinfo) | No; formats already-parsed certificates and CSRs as text | None | None | None | Inspection/display helper only |
| [`jittering/truststore`](https://github.com/jittering/truststore) | A library fork of mkcert that creates a CA and leaf certificates | Wraps mkcert-style system-store install/uninstall | No footprint enumeration or fingerprint-removal API | No semantic denial error | Broader than needed and appears inactive; it retains mkcert's store semantics |

## Evidence and implications

### Smallstep truststore

The public API accepts an `*x509.Certificate` or certificate filename and offers `Install` and `Uninstall`; it accepts no context and does not expose system-store listing or lookup ([API source](https://github.com/smallstep/truststore/blob/v0.13.0/truststore.go)). Its macOS implementation explicitly runs `sudo security add-trusted-cert -d -k /Library/Keychains/System.keychain` and removes from the admin domain ([macOS source](https://github.com/smallstep/truststore/blob/v0.13.0/truststore_darwin.go)). That conflicts with seamless-cors's login-keychain/current-user boundary.

On Windows it does use the native current-user `ROOT` store, but uninstall enumerates certificates and deletes every certificate with the same serial number ([Windows source](https://github.com/smallstep/truststore/blob/v0.13.0/truststore_windows.go)). Microsoft documents that `CertOpenSystemStore` accesses only current-user certificates ([Microsoft API](https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-certopensystemstorea)). Seamless-cors deliberately discovers a strict self-signed ownership footprint and removes exact SHA-1 fingerprints, so adapting the package would still require retaining custom enumeration and removal.

The package's typed command error preserves command output, but its exported errors do not identify user denial or cancellation ([error source](https://github.com/smallstep/truststore/blob/v0.13.0/errors.go)). The project would still need platform-specific interpretation to produce `ErrApprovalDenied`.

### mkcert and its library forks

mkcert proves the overall workflow is viable: it creates/loads a local CA and supports macOS and Windows stores ([CA source](https://github.com/FiloSottile/mkcert/blob/master/cert.go), [supported stores](https://github.com/FiloSottile/mkcert#supported-root-stores)). However, it is a `package main` CLI whose helpers terminate the process on errors. On macOS it targets `/Library/Keychains/System.keychain` through `sudo`; on Windows it removes by serial number ([macOS source](https://github.com/FiloSottile/mkcert/blob/master/truststore_darwin.go), [Windows source](https://github.com/FiloSottile/mkcert/blob/master/truststore_windows.go)). Copying or invoking it would not provide the context cancellation, exact discovery, or caller-owned errors required here.

`jittering/truststore` exposes mkcert through a library API, including CA creation and install/uninstall ([public API](https://github.com/jittering/truststore/blob/lib/lib.go)). Its latest published module version is from 2022, and its API remains centered on one mkcert-managed CA rather than enumerating a project's independently defined footprint ([Go package documentation](https://pkg.go.dev/github.com/jittering/truststore)). It is not a stronger foundation than the smaller adapters already present.

### Authority helpers

CertMagic is designed to obtain, renew, cache, and serve certificates from an ACME-compatible CA; its documented requirements include an ACME server and persistent storage ([CertMagic features and requirements](https://github.com/caddyserver/certmagic/blob/v0.25.4/README.md#features)). Its `Issuer` abstraction obtains an issued certificate from a CSR, not a locally trusted root ([Issuer source](https://github.com/caddyserver/certmagic/blob/v0.25.4/certmagic.go)). It has no OS trust-store lifecycle. Caddy's separate local-PKI module composes CertMagic storage, a Smallstep authority, and Smallstep truststore, illustrating that CertMagic alone does not provide this feature ([Caddy local-PKI source](https://github.com/caddyserver/caddy/blob/master/modules/caddypki/ca.go)).

Smallstep `certificates` is explicitly the `step-ca` online CA server and includes X.509/SSH issuance, ACME, provisioners, and operational policy ([official repository](https://github.com/smallstep/certificates)). Embedding it to replace a short `crypto/x509` self-signing path would introduce a server-scale domain model without helping OS trust mutation.

Smallstep `certinfo` exports certificate/CSR text rendering functions ([API source](https://github.com/smallstep/certinfo/blob/v1.16.0/certinfo.go)); it does not create, persist, or trust authorities. `go.step.sm/crypto/pemutil` can reduce a little PEM/key parsing boilerplate ([PEM utility source](https://github.com/smallstep/crypto/blob/master/pemutil/pem.go)), but it cannot enforce this project's identity, key-pair match, file modes, or atomic directory publication. That small reduction does not justify another dependency here.

## Recommendation

Retain `crypto/rsa`, `crypto/x509`, `crypto/tls`, and the project-owned durable publication logic for authority material. Retain the custom macOS `security` and Windows PowerShell adapters in a cohesive Trust Store module because they hide current-user root-store listing, addition, fingerprint removal, context handling, and platform approval-denial interpretation. UserCA, not Trust Store, applies the strict seamless-cors ownership footprint and classifies approval denial into its caller-owned semantic error.

If the scope later changes to **system-wide** trust and removal by supplied certificate (rather than enumeration/fingerprint reconciliation), reassess `smallstep/truststore`. Under the current requirements, it would be useful mainly as reference source, not as an imported abstraction.
