# Compatibility

Verified runtime: Linux amd64, Go 1.26.8, the development workstation, local loopback nodes.

| OS | Arch | Build | Install script executed | Runtime E2E |
|---|---|---|---|---|
| Linux | amd64 | yes | installer logic reviewed; join/start used via the CLI in tests | yes, local |
| Linux | arm64 | cross-compile | no | no |
| macOS | amd64 | cross-compile | no | no |
| macOS | arm64 | cross-compile | no | no |
| Windows | amd64 | cross-compile | no | no |
| Windows | arm64 | cross-compile | no | no |

Node service managers:

- Linux user systemd: unit text is written by install.sh. Not started under systemd in CI.
- macOS LaunchAgent: plist is written by install.sh. Not loaded.
- Windows scheduled task: created by install.ps1. Not created.

PWA: manifest is emitted by vite-plugin-pwa. Installability was not checked on iOS Safari or Android Chrome.

Harness versions observed on the development machine are printed by `go test ./tests/integration -run TestDetectRealCLIs -v`. OpenCode was absent.
