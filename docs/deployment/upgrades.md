# Upgrades

An upgrade replaces the binary and restarts the service. On its first start
the new version brings the data up to date by itself: database migrations
and, when a release changes it, the on-disk layout. Read the
[changelog](../changelog.md) of every version you skip before upgrading.

!!! tip "Before you start"
    Download a fresh encryption recovery bundle (**Settings → Security**) and
    check that your last backup is recent.

## Single node

=== "Debian / Ubuntu"

    ```bash
    wget https://github.com/MaxIOFS/MaxIOFS/releases/download/v1.10.0/maxiofs_v1.10.0_amd64.deb
    sudo dpkg -i maxiofs_v1.10.0_amd64.deb
    ```

    The package keeps `/etc/maxiofs/config.yaml` and restarts the service if
    it was running.

=== "RHEL / Rocky / Alma"

    ```bash
    sudo rpm -U https://github.com/MaxIOFS/MaxIOFS/releases/download/v1.10.0/maxiofs-1.10.0-1.x86_64.rpm
    sudo systemctl restart maxiofs
    ```

=== "Docker"

    ```bash
    docker pull maxiofs/maxiofs:latest
    docker stop maxiofs && docker rm maxiofs
    docker run -d --name maxiofs ...   # the same command as before
    ```

    With Compose: `docker compose pull && docker compose up -d`.

=== "Binary"

    ```bash
    wget -O maxiofs.new https://github.com/MaxIOFS/MaxIOFS/releases/download/v1.10.0/maxiofs-linux-amd64-v1.10.0
    sudo install -m 755 maxiofs.new /usr/local/bin/maxiofs
    sudo systemctl restart maxiofs
    ```

Then check the version in the console (**About**, or the bottom of the side
menu) and watch the log for errors:

```bash
sudo journalctl -u maxiofs -n 100
```

### Maintenance mode

To keep clients from writing while you upgrade, turn on maintenance mode in
**Settings → System** before, and off after. While it is on:

- S3 reads (`GET`, `HEAD`) work; writes and deletes are refused.
- The console shows a banner to everyone and refuses changes, except
  settings and sign-in.

A short upgrade does not need it: a restart takes seconds, and S3 clients
retry.

## Cluster

Upgrade one node at a time, so the others keep serving:

1. Take the node out of the load balancer.
2. Upgrade it as above and wait until **Cluster → Nodes** shows it healthy.
3. Put it back in the load balancer.
4. Repeat with the next node.

!!! warning "Same version everywhere"
    Upgrade every node soon after the first. Turn on parity or create a
    storage class only once all nodes run the same version.

## Downgrades

A newer version can change the database or the layout in a way an older one
cannot read. Going back means restoring the backup taken before the upgrade.
