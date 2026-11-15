# Getting started

[Overview](../../README.md) · [User guide](usage.md) · [Deployment reference](../../deploy/README.md)

This guide sets up a new Linux amd64 installation with released Docker images, local accounts, and an HTTPS reverse proxy. A scanner and OCR service are optional: verify file import and ordinary PDF export first.

## Prerequisites

- A Linux amd64 host with Docker and Docker Compose v2.
- An HTTPS hostname and reverse proxy. Browser camera access requires a secure context.
- Two separate local storage directories: persistent identity/account state and temporary private documents. The filesystem must support SQLite locking and durability; NFS is outside the storage contract.
- For scanning, a supported scanner reachable from the worker host.

## 1. Download and verify

Download `scanner-deployment.tar` and `SHA256SUMS` from the [same release](https://github.com/otama-homelab/scanner-kiosk/releases/latest). Verify the selected archive before extracting it:

```sh
awk '$2 == "scanner-deployment.tar" { print; found=1 } END { if (!found) exit 1 }' SHA256SUMS | sha256sum --check -
mkdir scanner-kiosk
tar -xf scanner-deployment.tar -C scanner-kiosk
cd scanner-kiosk
```

You can also work from a repository checkout. Running released images does not require compiling the application.

## 2. Prepare storage and configuration

For a new installation using container UID/GID `10001:10001`:

```sh
sudo install -d -m 700 -o 10001 -g 10001 /srv/scanner/state /srv/scanner/documents
```

Check UID/GID mappings for rootless Docker or user namespaces first. Do not recursively change ownership or permissions on existing data.

Create `scanner.env`:

```dotenv
SCANNER_IMAGE=ghcr.io/otama-homelab/scanner-kiosk@sha256:REPLACE_WITH_RELEASE_CORE_DIGEST
SCANNER_PUBLIC_URL=https://scanner.example.com
SCANNER_STATE_HOST_PATH=/srv/scanner/state
SCANNER_DOCUMENT_HOST_PATH=/srv/scanner/documents
SCANNER_AUTHENTICATE_TYPE=simple
SCANNER_ACQUISITION_ENABLED=false
SCANNER_SCANNERS_JSON=[]
PUID=10001
PGID=10001
```

Replace the image placeholder with the release's **repository digest**, not its image configuration ID. Set the hostname and paths for your installation. Keep acquisition disabled until the services and storage are ready.

```sh
chmod 600 scanner.env
docker compose --env-file scanner.env -f deploy/compose.yaml config --quiet
docker compose --env-file scanner.env -f deploy/compose.yaml up -d
```

The API (`serve`) and worker use the same core image, runtime identity, and storage. Compose reads `scanner.env`; the executable does not automatically load `.env` files. See [configuration variables](../../.env.example).

## 3. Create a local account

In Bash, read the password without echoing it and pass it through standard input:

```bash
read -r -s -p 'Scanner password: ' scanner_password
printf '\n'
printf '%s\n' "$scanner_password" | docker compose --env-file scanner.env -f deploy/compose.yaml run --rm --no-deps -T api user create admin
unset scanner_password
```

`admin` is the account name; it does not grant special privileges based on its name. For OIDC, follow the authentication section of the [deployment reference](../../deploy/README.md) instead.

## 4. Connect HTTPS

Compose binds the API to `127.0.0.1:8080` on the host. With Caddy running on that same host:

```caddyfile
scanner.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Configure DNS and certificate issuance for your environment. If the proxy runs in another container, its `127.0.0.1` does not refer to the Docker host; configure an appropriate network and upstream address.

Open your configured URL and sign in. Check service startup and API readiness:

```sh
docker compose --env-file scanner.env -f deploy/compose.yaml logs --tail=50 api worker
curl --fail https://scanner.example.com/health/ready
```

An API health response does not prove scanner availability or successful worker acquisition.

## 5. Enable acquisition and save your first document

After checking storage permissions, capacity, and worker startup, set `SCANNER_ACQUISITION_ENABLED=true` in `scanner.env` and apply the same Compose command again.

1. Import a small image, choose document output, and save a PDF without OCR.
2. Open the downloaded PDF and check its contents.
3. Select a scanner and capture a page, if scanning is needed.

For Bonjour eSCL discovery on Docker, review the [network requirements](../../deploy/README.md#scanner-discovery) and add the discovery override:

```sh
docker compose --env-file scanner.env -f deploy/compose.yaml -f deploy/compose.discovery.yaml up -d
```

Do not reuse the old scanservjs output volume as Scanner Kiosk storage.

## Add optional OCR

After ordinary export works, follow the [OCR configuration reference](../../deploy/README.md).

- **Tesseract:** use the Tesseract service image from the same release as the core image.
- **LLM:** configure a compatible vision endpoint, exact response model identity, destination label, worker credential, and a suitable licensed TrueType font for the PDF text layer. The selected font must cover the document's characters.

LLM OCR sends images to that endpoint. Check its processing and retention policy before enabling it. See [usage and supplements](usage.md#llm-interpretation-supplements).

## Upgrade and back up

Stop new intake and let active jobs finish before an upgrade. Preserve a consistent state/document relationship and update all roles to the same release. A schema change can make an image-only rollback unsafe.

Persistent identity state is the normal backup scope. Private documents are temporary and need a separate retention/disposal policy. Follow the [deployment reference](../../deploy/README.md) for storage, backup, and recovery details; review the [changelog](../../CHANGELOG.md) before updating.

For operational symptoms, see [troubleshooting](usage.md#troubleshooting).
