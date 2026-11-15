# Install with Docker

[Overview](../../README.md) · [Docker Compose](docker-compose.md) · [User guide](usage.md) · [Deployment reference](../../deploy/README.md)

The [README quick start](../../README.md#quick-start-docker) contains the complete `docker run` commands for a new installation. This guide explains their configuration and operation. Compose is an [alternative](docker-compose.md), not a requirement.

## Prerequisites

- Linux amd64 with Docker Engine.
- A released core image, selected by its immutable repository digest.
- Two separate local storage directories with supported SQLite locking/durability. NFS is outside the storage contract.
- An HTTPS hostname and reverse proxy. Browser camera access requires a secure context.

Start with file import and ordinary PDF export. A physical scanner and OCR service are optional.

## Storage and runtime identity

Both the API and worker must use the same image, UID/GID, and state/document mounts. The default container identity is `10001:10001`.

| Host path in the example | Container path | Purpose |
| --- | --- | --- |
| `/srv/scanner/state` | `/var/lib/scanner/state` | Persistent account/identity state |
| `/srv/scanner/documents` | `/var/lib/scanner/documents` | Temporary private originals and outputs |
| Container-private tmpfs | `/tmp` | Scratch space; 512MiB per role |

For new, empty storage:

```sh
sudo install -d -m 700 -o 10001 -g 10001 /srv/scanner/state /srv/scanner/documents
```

With rootless Docker or user namespaces, establish the host/container UID mapping first. Do not recursively change ownership or permissions on existing data. Preserve the selected identity across upgrades. Use `--mount type=bind` as shown in the README: Docker rejects a missing bind source instead of silently creating a directory with unexpected ownership.

Do not reuse the old scanservjs output volume.

## Configuration

The quick start uses `scanner.env` for shared, non-secret application settings. Docker passes those values into each container; the executable does not load `.env` files itself. `SCANNER_IMAGE` is a shell variable consumed by `docker run`, not an application setting.

- `SCANNER_PUBLIC_URL`: the exact HTTPS origin used in the browser.
- `SCANNER_STATE_DIR` / `SCANNER_DOCUMENT_DIR`: **container** paths matching the mounts.
- `SCANNER_AUTHENTICATE_TYPE=simple`: local-account login. OIDC is available through the [authentication reference](../../deploy/README.md).
- `SCANNER_ACQUISITION_ENABLED=true`: explicitly enables acquisition for this installation; it does not initiate a capture on startup.
- `SCANNER_DISCOVERY_ENABLED=false` and `SCANNER_SCANNERS_JSON=[]`: start without automatically discovering devices. File import remains available.
- `SCANNER_OCR_PLUGINS=`: ordinary export without OCR.

See the [environment reference](../../.env.example) for limits and retention settings. Keep API-only and worker-only secrets separate; do not later add all credentials to a shared environment file.

The commands use a read-only root, private executable scratch tmpfs, dropped capabilities, no privilege escalation, resource limits, and a 180-second stop timeout. The worker also has a Go soft-memory budget distinct from its container/native-child limit. Both roles restart unless explicitly stopped.

## First sign-in and export

Use the README's `docker exec -i scanner-api /usr/local/bin/scanner user create admin` example to create an account without exposing a password in arguments. `admin` is the account name; the name itself does not grant special privileges.

The API is published only at `127.0.0.1:8080`. Point an HTTPS reverse proxy at that host address. If the proxy runs in another container, configure a suitable Docker network/upstream instead of using its own loopback address.

Check both roles and API readiness:

```sh
docker logs --tail=50 scanner-api
docker logs --tail=50 scanner-worker
curl --fail https://scanner.example.com/health/ready
```

Open the public URL, sign in, upload a small image, save a PDF, and open the downloaded file. API health alone does not prove worker acquisition or scanner readiness.

## Add a scanner

A Docker bridge can reach configured LAN scanner endpoints, but Bonjour/mDNS discovery does not normally cross that bridge. Choose either:

- Register the scanner's supported eSCL endpoint using `SCANNER_SCANNERS_JSON` as shown in the [environment reference](../../.env.example).
- For Linux LAN discovery, set `SCANNER_DISCOVERY_ENABLED=true` and recreate the **worker** with `--network host`, retaining all its other flags, image, identity, and mounts. This follows the existing [discovery networking contract](../../deploy/README.md#scanner-discovery). The worker publishes no HTTP port; keep the API's loopback binding.

Stop competing scanner writers before enabling capture. Confirm device identity and readiness, then make an explicit scan request; startup does not automatically capture a document.

## Add optional OCR

First verify ordinary export, then follow the [OCR reference](../../deploy/README.md).

- **Tesseract:** deploy its separate service image from the same release, with its own authentication. Give the worker the service endpoint and named credential. Supply provider/model metadata to the API only through the existing worker catalog.
- **LLM:** configure a compatible vision endpoint, exact response-model identity, destination label, worker credential, and a suitable licensed TrueType font. Mount the font read-only; it must cover the document's characters.

Keep OCR credentials on the worker. LLM OCR sends images to the selected endpoint, so review its processing/retention policy first. Supplements and their indexing implications are described in the [user guide](usage.md#llm-interpretation-supplements).

## Change configuration or upgrade

Editing an environment file does not change a running container. Stop new intake, let active jobs finish, then stop/recreate the API and worker with the reviewed flags and image. Keep both mounted directories and their identity intact. Update every role to the same release, and review the [changelog](../../CHANGELOG.md).

The two named containers in the quick start are application instances; deleting an instance is not a storage migration. Do not delete or replace the bind-mounted directories to upgrade an image. A schema change can make an image-only rollback unsafe; use the [backup/recovery contract](../../deploy/README.md).

Persistent identity state is the normal backup scope. Temporary private documents need a separate retention/disposal policy. See [troubleshooting](usage.md#troubleshooting) for operational symptoms and [release artifacts](../distribution.md) for distribution verification.
