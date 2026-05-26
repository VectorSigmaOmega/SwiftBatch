# Deployment Strategy

This document records the current operational plan for deploying Photon.

## Goal

Deploy Photon in a way that is:

- cheap enough for a portfolio project
- operationally credible
- easy to move between VPS providers
- simple enough to maintain alone

## Chosen Approach

The project will use:

- GitHub for source control
- `GHCR` for container images
- GitHub Actions for CI/CD
- one Linux VPS running `k3s`
- Kubernetes manifests in `deploy/k8s`
- SSH-triggered server-side deploy execution

## Why This Approach

This gives the project a strong backend and DevOps story without introducing unnecessary platform complexity.

It also avoids tying the repo to one hosting vendor. The deployment target can change as long as the target server can:

- run `k3s`
- pull images from `GHCR`
- accept deployment commands from GitHub Actions

## Provider Plan

The current practical plan is:

- use the SkyServer VPS as the first real deployment target
- keep the deployment flow provider-agnostic in case the project later moves to another VPS
- treat the server as replaceable infrastructure because the data model is intentionally ephemeral for the MVP

## Current Host Layout

The current target hostnames are:

- `photon.abhinash.dev` for the API
- `storage.photon.abhinash.dev` for MinIO object access
- `minio.photon.abhinash.dev` for the MinIO console
- `grafana.photon.abhinash.dev` for Grafana

These names should currently be managed in the `AWS Lightsail` DNS zone for `abhinash.dev`, which is the DNS UI currently in use.

## Current Server Access Model

The live VPS is a plain Linux server, not a managed cloud instance with extra control-plane tooling. That means the operational model is:

- SSH to the box as `deploy`
- use a dedicated SSH key
- use `sudo` for privileged operations
- let `k3s` provide the runtime and ingress layer
- let GitHub Actions upload the repo snapshot and invoke `scripts/deploy-k8s.sh`

Hardening details are documented separately in [Server Operations](server-operations.md).

## Data Model Assumption

The deployment strategy assumes:

- no user accounts
- no expectation that users return later
- no need to preserve old uploads and outputs across infrastructure moves

Because of that, the system can be treated as redeployable rather than state-heavy.

## Migration Model

Migration between providers should be:

1. provision the replacement server
2. install `k3s`
3. configure secrets and access
4. deploy the stack
5. run smoke checks
6. switch DNS in the active DNS zone manager
7. remove the old server later

This should be a human-triggered cutover, not a timer-based automatic event.

## Why Not Timed Automatic Migration

A fixed-date migration is risky because the new environment may not actually be ready. Common failure cases include:

- bad DNS
- bad registry credentials
- incomplete server setup
- cluster issues
- ingress misconfiguration

The right kind of automation is:

- reusable deployment to any target server
- fast cutover when the replacement target is healthy

Not:

- "on this date, move production no matter what"

## Cleanup Direction

Because the system is demo-oriented and anonymous, cleanup is now part of normal operation.

The cleanup runner deletes:

- expired uploads
- expired generated outputs
- old job records
- old attempt records
- stale DLQ entries

## Documentation Impact

This strategy affects future documentation in these ways:

- deployment docs should describe redeployability, not durable migration
- CI/CD docs should describe portable server targets
- demo docs should communicate that outputs are temporary
