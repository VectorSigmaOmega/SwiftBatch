# Server Operations

This document records the current manual access and hardening state of the live Photon VPS so future deployment work does not rely on memory.

## Current Server

- provider: `SkyServer`
- OS: `Ubuntu 22.04`
- hostname: `node1.photon.abhinash.dev`
- public IP: `161.248.163.187`

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

## SSH Access Model

The server should now be accessed through the `deploy` user with a dedicated SSH key.

Expected login pattern:

```bash
ssh -i ~/.ssh/photon_skyserver_ed25519 deploy@161.248.163.187
```

Useful convenience entry for `~/.ssh/config`:

```sshconfig
Host photon-skyserver
    HostName 161.248.163.187
    User deploy
    IdentityFile ~/.ssh/photon_skyserver_ed25519
    IdentitiesOnly yes
```

Then login becomes:

```bash
ssh photon-skyserver
```

## Hardening Performed

The following baseline hardening was applied before `k3s` installation:

- created a dedicated SSH keypair for this VPS
- created a non-root admin user: `deploy`
- added `deploy` to the `sudo` group
- enabled passwordless `sudo` for `deploy`
- installed the server key for `deploy`
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

- remote admin access goes through `deploy`
- `sudo` is used for privileged operations
- `root` does not log in over SSH
- password-based SSH login is disabled
- only SSH, HTTP, and HTTPS are open at the host firewall

## Important Operational Notes

- The root password that was used for first access should be considered temporary and effectively burned once exposed in setup history.
- The dedicated SSH key at `~/.ssh/photon_skyserver_ed25519` is now part of the server access path and should be retained carefully.
- If CI/CD later deploys by SSH, it should target the `deploy` user, not `root`.

## Kubernetes Access

`k3s` is installed on the server.

The working kubeconfig path on the box is:

```text
/home/deploy/.kube/config
```

Examples:

```bash
ssh photon-skyserver
kubectl --kubeconfig=/home/deploy/.kube/config get nodes -o wide
kubectl --kubeconfig=/home/deploy/.kube/config get pods -A
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
