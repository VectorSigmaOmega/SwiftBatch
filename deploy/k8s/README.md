# Kubernetes Manifests

These manifests target a single-node `k3s` cluster and deploy the full Photon demo stack:

- `photon-api`
- `photon-worker`
- `photon-cleanup`
- `postgres`
- `redis`
- `minio`
- `prometheus`
- `grafana`
- Traefik `Ingress` resources for the API, MinIO object endpoint, MinIO console, and Grafana

The manifests assume the default `local-path` storage class that ships with `k3s`.

## Image Strategy

The app manifests reference stable image names:

- `photon-api:latest`
- `photon-worker:latest`
- `photon-cleanup:latest`
- `photon-migrate:latest`

The intended deployment path is not "apply the repo as-is with committed secrets." Instead:

- `scripts/deploy-k8s.sh` generates a temporary kustomization
- that kustomization rewrites the three app images to registry-backed immutable tags
- the script creates Kubernetes secrets from runtime environment variables
- then the script applies the manifests and waits for rollout

This keeps sensitive values out of the repo while preserving simple base YAML.

You can still use the manifests manually if you want, but then you must:

1. create `photon-secrets` yourself
2. replace the app image names with real registry tags before applying

If you prefer building directly on the cluster host, you can still:

Example import flow on a host that has both Docker and `k3s`:

```bash
docker build -t photon-api:latest -f deploy/docker/Dockerfile.api .
docker build -t photon-worker:latest -f deploy/docker/Dockerfile.worker .
docker build -t photon-cleanup:latest -f deploy/docker/Dockerfile.cleanup .
docker build -t photon-migrate:latest -f deploy/docker/Dockerfile.migrate .

docker save photon-api:latest | sudo k3s ctr images import -
docker save photon-worker:latest | sudo k3s ctr images import -
docker save photon-cleanup:latest | sudo k3s ctr images import -
docker save photon-migrate:latest | sudo k3s ctr images import -
```

## Before Apply

Edit these files first:

- [config.yaml](config.yaml)
- [ingress.yaml](ingress.yaml)

The repo is currently prewired for these hosts:

- `photon.abhinash.dev`
- `storage.photon.abhinash.dev`
- `minio.photon.abhinash.dev`
- `grafana.photon.abhinash.dev`
- `swiftbatch.abhinash.dev` (legacy redirect to `photon.abhinash.dev`)

If those change later, update both [config.yaml](config.yaml) and [ingress.yaml](ingress.yaml). Keep `PHOTON_STORAGE_PUBLIC_BASE_URL` aligned with the MinIO object ingress host.

## Deploy

Recommended:

```bash
export GF_SECURITY_ADMIN_USER=...
export GF_SECURITY_ADMIN_PASSWORD=...
export MINIO_ROOT_USER=...
export MINIO_ROOT_PASSWORD=...
export PHOTON_POSTGRES_PASSWORD=...
export PHOTON_STORAGE_ACCESS_KEY=...
export PHOTON_STORAGE_SECRET_KEY=...
export PHOTON_API_IMAGE=ghcr.io/vectorsigmaomega/photon-api:<tag>
export PHOTON_WORKER_IMAGE=ghcr.io/vectorsigmaomega/photon-worker:<tag>
export PHOTON_CLEANUP_IMAGE=ghcr.io/vectorsigmaomega/photon-cleanup:<tag>
export PHOTON_MIGRATE_IMAGE=ghcr.io/vectorsigmaomega/photon-migrate:<tag>

./scripts/deploy-k8s.sh
```

Manual fallback:

```bash
kubectl apply -k deploy/k8s
kubectl -n photon get pods
```

The API pod runs the migration binary as an init container before the main server starts. The worker and cleanup runner wait for the schema and MinIO bucket before they start processing.
The API root path serves the minimal browser frontend, and the MinIO deployment applies global CORS for `MINIO_API_CORS_ALLOW_ORIGIN` so browser-based presigned uploads work.

Because the published GHCR images are currently public, the cluster does not need an image pull secret. If those packages are made private later, reintroduce a pull secret at that time.

## Access

- API: `https://photon.abhinash.dev`
- MinIO object endpoint for presigned URLs: `https://storage.photon.abhinash.dev`
- MinIO console: `https://minio.photon.abhinash.dev`
- Grafana: `https://grafana.photon.abhinash.dev`
- Prometheus: `kubectl -n photon port-forward svc/photon-prometheus 9090:9090`

## TLS Note

The repo now includes [traefik-config.yaml](traefik-config.yaml), which adds a `HelmChartConfig` for the bundled `k3s` Traefik install:

- persistent ACME storage at `/data/acme.json`
- Let's Encrypt HTTP-01 certificate issuance
- HTTP-to-HTTPS redirection on the `web` entrypoint

The ingress resources are configured for the `websecure` entrypoint and reference the `letsencrypt` certificate resolver.
