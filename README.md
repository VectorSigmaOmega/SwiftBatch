<h1>
  <img
    src="https://github.com/user-attachments/assets/4899a7c5-272e-4f09-a6e4-4080a568668e"
    alt="Guardian logo"
    width="65"
    align="center"
  />
  &nbsp;Photon
</h1>

Photon is an asynchronous, distributed, fault-tolerant image processing pipeline built as a production-style portfolio project.

<img width="900" alt="Screenshot 2026-05-31 222053" src="https://github.com/user-attachments/assets/3e666f34-ab22-45a4-95b2-bf21ba3a355e" />

<br>
<br>
It accepts browser uploads, stores original images in object storage, creates durable jobs, pushes work through Redis, processes images in separate worker containers, stores generated outputs, exposes metrics, and deploys to a real VPS-backed `k3s` cluster through CI/CD.

The project is intentionally more than a CRUD demo. It is designed to show practical backend, infrastructure, and operations skills across Go, Redis, PostgreSQL, Docker, Kubernetes, GitHub Actions, object storage, observability, and VPS administration.

## What Photon Does

Photon turns uploaded images into generated variants without making the user wait on the request path.

1. The API returns a presigned upload URL for direct browser-to-MinIO upload.
2. The client uploads the source image to object storage.
3. The API creates a job record in PostgreSQL and enqueues the job ID in Redis.
4. Worker processes claim jobs asynchronously, download source objects, run ImageMagick transforms, and upload generated outputs.
5. The API exposes job status and presigned download URLs for completed outputs.
6. A cleanup service expires old jobs, uploaded sources, generated outputs, attempts, and dead-letter records.

Supported output formats include `jpg`, `png`, `webp`, and `avif`. Jobs can request multiple named variants with width, height, and quality settings.

## Why It Exists

Photon is built to demonstrate the kind of engineering that sits between application code and real operations:

- asynchronous job orchestration instead of synchronous request blocking
- distributed API and worker services connected through Redis
- durable job state, attempts, retries, and output metadata in PostgreSQL
- object-storage based upload and download flow using presigned URLs
- automatic retry and dead-letter handling for failed work
- stale job recovery when a worker dies mid-attempt
- metrics, health checks, readiness checks, and structured logs
- containerized local development with Docker Compose
- Kubernetes deployment manifests for a single-node `k3s` cluster
- CI/CD that builds immutable images, pushes to GHCR, and deploys to a VPS over SSH
- operational documentation for DNS, TLS, firewalling, SSH access, and server hardening

## Architecture

```text
Browser / React UI
        |
        | presigned upload + job API
        v
   Photon API  ----------------->  PostgreSQL
        |                              |
        | enqueue job ID               | jobs, attempts, outputs
        v                              |
 Redis queue / DLQ                     |
        |                              |
        | claim work                   |
        v                              |
Photon workers  -----------------------
        |
        | download source, generate variants, upload outputs
        v
 MinIO object storage
```

The API and workers are separate deployable units. Workers can be scaled independently from the API, and the queue boundary keeps image processing off the latency-sensitive request path.

## Fault Tolerance

Photon treats image processing as unreliable work that must be recoverable.

- Jobs move through explicit states: `queued`, `processing`, `completed`, `failed`, and `dead_lettered`.
- Each processing run creates a job attempt record.
- Failed attempts are retried until `PHOTON_REDIS_MAX_RETRIES` is exhausted.
- Exhausted jobs are pushed to a Redis dead-letter queue with failure metadata.
- Workers recover stale `processing` jobs when a lease expires before completion.
- The API marks jobs failed if persistence succeeds but queue enqueueing fails.
- Cleanup removes expired job history and object storage artifacts so the demo remains operational over time.

## Tech Stack

| Area | Tools |
| --- | --- |
| Backend services | Go, standard `net/http`, pgx, go-redis, MinIO SDK |
| Image processing | ImageMagick inside the worker image |
| Queueing | Redis queue plus dead-letter queue |
| Persistence | PostgreSQL migrations and job metadata |
| Object storage | MinIO with presigned upload and download URLs |
| Frontend | React, TypeScript, Vite, Tailwind, embedded into the Go API binary |
| Local runtime | Docker Compose |
| Deployment | Docker, GHCR, Kubernetes manifests, `k3s`, Traefik |
| Observability | Prometheus metrics, Grafana manifests, health and readiness probes, structured JSON logs |
| CI/CD | GitHub Actions for formatting, tests, frontend build, image publishing, and VPS deployment |
| Operations | Ubuntu VPS, SSH deploy user, firewall hardening, DNS, HTTPS via Let's Encrypt |

The core implementation is Go. The worker boundary is intentionally service-oriented: additional processors can be added later by consuming the same Redis job contract and persisting results through the same database/storage model.

## API Surface

The API serves both the browser UI and the JSON workflow endpoints.

| Endpoint | Purpose |
| --- | --- |
| `GET /` | Embedded browser UI |
| `POST /v1/uploads/presign` | Create a presigned source upload URL |
| `POST /v1/jobs` | Create and enqueue an image processing job |
| `GET /v1/jobs/{id}` | Read job status and metadata |
| `GET /v1/jobs/{id}/results` | Read output metadata and presigned download URLs |
| `POST /v1/jobs/{id}/retry` | Retry a failed or dead-lettered job |
| `GET /healthz` | Liveness check |
| `GET /readyz` | Dependency readiness check |
| `GET /metrics` | Prometheus metrics |

Rate limiting protects upload presign, job creation, and retry endpoints. The limiter respects forwarded client IP headers for operation behind Traefik.

## Local Development

Start the full stack:

```bash
docker compose -f deploy/docker/docker-compose.yml up --build
```

Docker Compose starts:

- PostgreSQL
- Redis
- MinIO
- bucket bootstrapper
- migration job
- API
- worker
- cleanup runner

Local URLs:

| Service | URL |
| --- | --- |
| Photon UI/API | `http://localhost:18080` |
| Worker metrics | `http://localhost:18081/metrics` |
| MinIO API | `http://localhost:9000` |
| MinIO console | `http://localhost:9001` |

Run the Go services outside Docker after starting infrastructure:

```bash
go run ./cmd/migrate up
go run ./cmd/api
go run ./cmd/worker
go run ./cmd/cleanup serve
```

Run tests:

```bash
go test ./...
```

Build the frontend:

```bash
cd frontend
npm ci
npm run build
```

## Deployment

Photon includes a deployable Kubernetes stack in `deploy/k8s` for a single-node `k3s` cluster.

The current deployment model uses:

- GitHub Actions
- GHCR image publishing
- SSH to a plain Ubuntu VPS
- `scripts/deploy-k8s.sh`
- Kubernetes secrets created from GitHub repository secrets
- Traefik ingress with Let's Encrypt HTTP-01 certificates
- Prometheus and Grafana manifests for observability

Public hostnames used by the current deployment plan:

- `photon.abhinash.dev` for the app/API
- `storage.photon.abhinash.dev` for MinIO object access
- `minio.photon.abhinash.dev` for the MinIO console
- `grafana.photon.abhinash.dev` for Grafana

The deploy workflow builds and pushes four images:

- `photon-api`
- `photon-worker`
- `photon-migrate`
- `photon-cleanup`

Then it uploads a deployment bundle to the VPS and applies the Kubernetes manifests with immutable image tags.

## Repository Layout

```text
cmd/
  api/        HTTP API and embedded frontend server
  worker/     asynchronous image processing worker
  cleanup/    retention and object cleanup process
  migrate/    database migration entry point
deploy/
  docker/     Dockerfiles and local Docker Compose stack
  k8s/        k3s-oriented Kubernetes manifests
docs/         build, deployment, CI/CD, and operations notes
frontend/     React/TypeScript product UI
internal/
  api/        HTTP handlers, static embedding, rate limiting
  cleanup/    retention runner
  config/     environment-driven configuration
  db/         PostgreSQL repositories and models
  imageproc/  transform validation and ImageMagick execution
  observability/
  platform/   logging, migrations, postgres connection helpers
  queue/      Redis queue and DLQ implementation
  storage/    MinIO client and presigned URL handling
  worker/     worker runtime and retry logic
migrations/   SQL schema migrations
scripts/      local and deployment helpers
```

## What This Demonstrates

Photon is a compact project, but it exercises real production concerns:

- Go service design with clean package boundaries
- asynchronous distributed processing with Redis
- fault tolerance through retries, attempts, DLQ, and stale job recovery
- PostgreSQL schema design and migration flow
- MinIO/S3-style object storage integration
- Docker image design for multiple service roles
- Kubernetes deployment on `k3s`
- CI/CD with GitHub Actions and GHCR
- VPS operations, DNS, TLS, SSH hardening, and firewall setup
- frontend integration with a backend-driven upload pipeline
- a clear extension path for additional worker types or image analysis stages

## Further Reading

- [Build Journal](docs/build-journal.md)
- [Backlog](docs/backlog.md)
- [CI/CD Setup](docs/ci-cd.md)
- [Deployment Strategy](docs/deployment-strategy.md)
- [Server Operations](docs/server-operations.md)
- [Kubernetes Manifests](deploy/k8s/README.md)
