# Omnoda Seed (v0.2)

Seed is a single-device runtime. It embeds the v0.1 control plane, a local capability registry, and the evolver store. It does not require a remote server, Docker, Redis, or an external database.

```bash
omnoda seed            # listen on 127.0.0.1:8787
omnoda status
omnoda cap list
omnoda cap inspect <id>
omnoda cap install <dir>
omnoda cap enable <id> [version]
omnoda cap disable <id>
omnoda cap test <id>
omnoda cap versions <id>
omnoda cap rollback <id> <version>
omnoda cap export <id> <file>
omnoda cap import <file>
```

`omnoda seed` is idempotent. Repeating it keeps the database, node identity, and capability packages.

## No-model mode

The console, capability list, and built-in capabilities work with no API key. Chat replies and capability generation return an error. They do not fall back to a canned natural-language template.

## Model mode

Set the provider in Settings (OpenAI-compatible base URL, model, key). The key is sealed with AES-256-GCM. A legacy XOR seal is migrated on read. A failed migration leaves the original ciphertext in place.

## Capability packages

`capability.json` uses `apiVersion: omnoda.dev/v1alpha1`. Unknown permission fields, absolute paths, and unsupported runtimes are rejected. Declaring a permission does not grant it.

Generated code runs only through the platform sandbox. On Linux that is `bwrap` when it is installed. If no backend is available, untrusted code is refused. A path-checked subprocess is not reported as a sandbox.

## Evolution

States: `requested`, `researching`, `designing`, `implementing`, `testing`, `verifying`, `awaiting_approval`, `activated`, `rejected`, `failed`.

Builder tests and verifier fixtures are separate. A failed verifier does not install an enabled capability. Approval is a separate call.

The deterministic sensor-log fixture (`OMNODA_ALLOW_FIXTURE_EVOLUTION=1`) is a test double. It is not a model result. Real acquisition is E2E-verified only after a recorded model call.

## Git

PR #1 (`feat/omnoda-poc-v0.1`) was open and unmerged when this branch was cut. `feat/omnoda-seed-v0.2` is based on that branch. After PR #1 merges, rebase this branch onto `main`.
