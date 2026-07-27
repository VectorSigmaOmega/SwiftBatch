# Server Operations

This document records the current manual access and hardening state of the live Photon VPS so future deployment work does not rely on memory.

## Current Server

- provider: `OVHcloud`
- OS: `Ubuntu 24.04`
- hostname: `vps-be504035`
- public IP: `144.217.4.173`

## DNS Control Plane

The public hostnames for this project are currently managed in the `AWS Lightsail` DNS zone UI for `abhinash.dev`, not in Route 53.

The current public names are:

- `photon.abhinash.dev`
- `storage.photon.abhinash.dev`
- `minio.photon.abhinash.dev`
- `grafana.photon.abhinash.dev`
- `swiftbatch.abhinash.dev` (legacy homepage redirect)

If a future note says "update DNS", it means:

- add or update `A` records in the Lightsail DNS zone shown in the AWS console
- point the records at `144.217.4.173`

## SSH Access Model

The server should now be accessed through the `ubuntu` user with a dedicated SSH key.

Expected login pattern:

```bash
ssh -i ~/.ssh/ovh_vps3_ed25519 ubuntu@144.217.4.173
```

Useful convenience entry for `~/.ssh/config`:

```sshconfig
Host photon-ovh
    HostName 144.217.4.173
    User ubuntu
    IdentityFile ~/.ssh/ovh_vps3_ed25519
    IdentitiesOnly yes
```

Then login becomes:

```bash
ssh photon-ovh
```

## Hardening Performed

The following baseline hardening was applied before application deployment:

- installed a dedicated SSH keypair for this VPS
- retained the OVH-provided non-root admin user: `ubuntu`
- confirmed passwordless `sudo` for `ubuntu`
- installed the server key for `ubuntu`
- updated the package set with `apt-get update` and `apt-get upgrade -y`
- installed and enabled `ufw`
- opened only:
  - `22/tcp`
  - `80/tcp`
  - `443/tcp`
- disabled remote `root` SSH login
- disabled SSH password authentication
- kept SSH public key authentication enabled

## Effective Security Posture

The intended current posture is:

- remote admin access goes through `ubuntu`
- `sudo` is used for privileged operations
- `root` does not log in over SSH
- password-based SSH login is disabled
- only SSH, HTTP, and HTTPS are open at the host firewall

## Important Operational Notes

- The root password that was used for first access should be considered temporary and effectively burned once exposed in setup history.
- The dedicated SSH key at `~/.ssh/ovh_vps3_ed25519` is now part of the server access path and should be retained carefully.
- CI/CD deploys by SSH to the `ubuntu` user, not `root`.

## Runtime Layout

Photon runs with Docker Compose.

- release directories: `/opt/photon/releases/`
- active release symlink: `/opt/photon/current`
- environment file: `/etc/photon/photon.env`
- compose project: `photon`

The public HTTP/S entry point is nginx. Compose binds service ports to `127.0.0.1` only:

- API/frontend: `127.0.0.1:18080`
- worker metrics: `127.0.0.1:18081`
- MinIO S3 API: `127.0.0.1:9000`
- MinIO console: `127.0.0.1:9001`

The nginx site config is tracked at `deploy/nginx/photon.conf` and is installed as `/etc/nginx/sites-available/photon`.

```bash
ssh photon-ovh
cd /opt/photon/current
./scripts/deploy-compose.sh
docker compose --project-name photon --env-file /etc/photon/photon.env -f deploy/docker/docker-compose.yml -f deploy/docker/docker-compose.production.yml ps
```

## If Access Stops Working

Check these in order:

1. confirm the server IP did not change
2. confirm the DNS zone still points to the right IP
3. confirm you are using the dedicated key, not another SSH key
4. confirm `ufw` still allows `22/tcp`
5. if console access is available from the provider, inspect:
   - `/etc/ssh/sshd_config.d/50-cloud-init.conf`
   - `/etc/ssh/sshd_config.d/99-hardening.conf`
   - `/etc/sudoers.d/90-deploy-nopasswd`
