# CI/CD Setup

This document explains how the GitHub Actions scaffolding deploys Photon to the OVH VPS.

## Workflow Shape

The repo now contains two workflows:

- `.github/workflows/ci.yml`
  - runs on pull requests and non-`main` pushes
  - checks `gofmt`
  - runs `go test ./...`
- `.github/workflows/deploy.yml`
  - runs on pushes to `main`
  - can also be started manually with `workflow_dispatch`
  - builds the frontend and runs `go test ./...`
  - uploads the repo contents and a generated environment file to the VPS over SSH
  - runs `scripts/deploy-compose.sh` on the server

## Deployment Model

The deployment workflow does not keep runtime secrets in the repo.

Instead it:

- writes `/etc/photon/photon.env` from GitHub repository secrets
- extracts the source into `/opt/photon/releases/<sha>-<run-id>`
- updates `/opt/photon/current`
- starts the stack with Docker Compose
- waits for `GET /readyz` on the local API port

The production Compose override binds Postgres, Redis, MinIO, the API, and worker metrics to localhost-only ports so the host nginx layer remains the only public HTTP/S entry point.

## Required GitHub Repository Variables

Set these in:

- `Settings -> Secrets and variables -> Actions -> Variables`

Required variables:

- `PHOTON_DEPLOY_HOST`
  - current value: `144.217.4.173`
- `PHOTON_DEPLOY_USER`
  - current value: `ubuntu`
- `PHOTON_DEPLOY_PORT`
  - current value: `22`

## Required GitHub Repository Secrets

Set these in:

- `Settings -> Secrets and variables -> Actions -> Secrets`

Required secrets:

- `PHOTON_DEPLOY_SSH_PRIVATE_KEY`
  - the private key that matches the server access path for `ubuntu`
- `MINIO_ROOT_USER`
- `MINIO_ROOT_PASSWORD`
- `PHOTON_POSTGRES_PASSWORD`
- `PHOTON_STORAGE_ACCESS_KEY`
- `PHOTON_STORAGE_SECRET_KEY`

## Server-Side Deploy Entry Point

The server-side entry point is:

- [scripts/deploy-compose.sh](../scripts/deploy-compose.sh)

That script expects `/etc/photon/photon.env` to be present and readable by the deploy user. The GitHub Actions deploy workflow creates it before invoking the script remotely.

## First Deployment Checklist

Before expecting the deploy workflow to succeed, confirm:

1. DNS `A` records for `photon.abhinash.dev` and `storage.photon.abhinash.dev` point at the VPS
2. Docker and Docker Compose are healthy on the server
3. the `ubuntu` user can SSH in with the configured key
4. GitHub variables and secrets are configured
5. the repo is pushed to `main`

## Important Limitation

The workflows can build and deploy the repo, but they do not provision the server itself.

That means these still belong to manual infrastructure setup:

- Ubuntu server provisioning
- SSH hardening
- Docker installation
- nginx and TLS setup
- DNS setup
