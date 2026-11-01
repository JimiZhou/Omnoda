# Deployment

## Binary

```bash
./bin/omnoda-server -addr 127.0.0.1:8787 -data /var/lib/omnoda -public-url https://omnoda.example.com
```

`-public-url` is copied into enrollment commands. It is not a license server.

## Docker Compose

`deploy/docker-compose.yml` publishes `127.0.0.1:8787` only. Build requires network access to fetch Go modules and npm packages.

## TLS

Public use needs HTTPS and WSS. Terminate TLS at Caddy or nginx and proxy to the process. See `deploy/Caddyfile.example`.

When `OMNODA_PUBLIC_URL` starts with `https://`, the session cookie is marked Secure.

Do not put the process on `0.0.0.0` without the login gate in front of it. The login gate is always on; TLS is still required so the cookie and the node socket are not readable on the network.

## Backups

Copy the SQLite file and `seal.key` together. The seal key decrypts the model provider secret. Lose it and the stored API key cannot be recovered. Node private keys never leave the devices.
