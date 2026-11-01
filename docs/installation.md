# Installation

## One command

The console prints:

```bash
curl -fsSL https://HOST/install.sh | sh -s -- --server https://HOST --code CODE
```

Windows:

```powershell
irm https://HOST/install.ps1 | iex; Install-Omnoda -Server https://HOST -Code CODE
```

The script:

1. Detects OS and architecture.
2. Downloads `omnoda` and `omnoda.sig` with retries.
3. Verifies an Ed25519 signature against the public key baked into the script.
4. Installs the binary atomically under `~/.local/bin` (or `%LOCALAPPDATA%\Omnoda\bin`) and rolls back if `omnoda version` fails.
5. Runs `omnoda join`.
6. Installs a **user** service.

## User service versus system service

Linux uses `systemctl --user` when `XDG_RUNTIME_DIR` is set. That lives with the user session. It is not started at boot unless the user enables lingering (`loginctl enable-linger`), which is an explicit admin action and is not done by the installer.

macOS uses a LaunchAgent in `~/Library/LaunchAgents`, which starts at login for that user.

Windows registers a scheduled task at logon for the current user. It does not install a SYSTEM service.

An administrator who wants a machine-wide service should copy the unit themselves and review the account it runs as. The installer will not do that.

## Release signing

```bash
bin/omnoda-sign -generate -key release.key
SIGN_KEY=release.key make release
```

`make release` writes `dist/releases/omnoda_<version>_<os>_<arch>` and a `.sig` file. Copy those into the server data directory `releases/` so `/releases/<file>` can serve them.

The public key committed for this POC is in `deploy/release.pub`. The private key is not in git. Treat the committed key as a development trust anchor and rotate it before any real deployment.

## Platforms

| OS | Arch | Install script | Runtime tested here |
|---|---|---|---|
| Linux | amd64, arm64 | install.sh | amd64 only |
| macOS | amd64, arm64 | install.sh | not run |
| Windows | amd64, arm64 | install.ps1 | not run |
