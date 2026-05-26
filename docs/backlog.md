# Photon Backlog

This file tracks the remaining work for the MVP and the agreed delivery approach.

## Status

Completed:

- repository skeleton
- local Docker Compose stack
- Postgres schema and migrations
- job creation API
- Redis queue integration
- worker consumption and status transitions
- image transform pipeline
- output storage and result retrieval
- retries and DLQ
- metrics and structured logging
- Docker images
- single-node `k3s` manifests
- GitHub Actions CI/CD scaffolding
- first live CI/CD deployment run
- minimal browser frontend for upload, polling, retry, and results
- multi-file browser submission that fans out one normal job per file
- ephemeral data cleanup automation

In progress next:

- README finalization

Remaining after that:

- clean the worktree and resolve intentional vs accidental leftovers:
  - deleted `PHOTON_AGENT_BRIEF.md`
  - deleted `docs/future-project-efficiency.md`
  - untracked `Bills-1.png`
  - untracked `Bills-2.png`
- architecture diagram
- demo instructions
- frontend polish and `Engineering` page
- optionally rename the local checkout directory to match the product name so generated absolute paths stop carrying the old repo name

Footnote:

- the user-facing frontend now exists as a minimal product flow on `/`
- for portfolio value, the next frontend step should add polish plus a visible `Engineering` or `How It's Built` entry point that surfaces the system design without turning the homepage into a wall of infrastructure details

## Current Delivery Plan

The current deployment strategy is:

- use `GHCR` as the image registry
- build and push images from GitHub Actions
- deploy to a single Linux VPS running `k3s`
- keep the deployment provider-agnostic so it can move from one VPS vendor to another

The first realistic hosting plan is:

- initial deployment on the SkyServer VPS now that provisioning has completed
- keep the deployment flow portable so it can move later if the provider changes

## Migration Strategy

The agreed migration model is:

- no timed automatic cutover after a fixed date
- migration should be `switch-ready`, not blindly scheduled
- the system should be easy to redeploy on a new server
- DNS cutover should happen only after the replacement environment is healthy

This is safe because application data is intentionally treated as ephemeral for the MVP.

## Data Retention Direction

The current product assumption is:

- users are anonymous
- jobs are short-lived
- old uploads and outputs do not need long-term retention
- cross-provider migration does not require preserving old state

The cleanup runner now deletes:

- old uploaded source objects
- old generated outputs
- old jobs and job attempts
- old DLQ entries that are no longer useful for debugging

## CI/CD Notes

The CI/CD scaffolding now delivers:

- GitHub repo workflow for `go build` and basic validation
- Docker image build and push for:
  - `api`
  - `worker`
  - `cleanup`
  - `migrate`
- deployment workflow that can target a server by secret/config, not by hardcoded provider assumptions
- deployment steps that are reusable for both:
  - temporary AWS server
  - later SkyServer server

What still remains operationally is:

- keep the deployment workflow maintained as GitHub updates its Actions runtime

## Non-Goals For The Next Phase

Do not do these unless they become necessary:

- timed migration after 4 months
- persistent multi-environment state promotion logic
- managed-cloud-specific deployment logic
- data-preserving migration tooling

## Current Focus

The highest-value remaining work is now:

- README finalization
- architecture diagram
- demo instructions
- frontend polish and `Engineering` page
