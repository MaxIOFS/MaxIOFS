# Backup and Recovery

## What to protect

| Item | Where | Why |
|------|-------|-----|
| **Encryption recovery bundle** + passphrase | Downloaded from **Settings → Security** | Without it, losing the database means losing every object |
| Database | `<data-dir>/db/maxiofs.db` | Users, keys, tenants, policies, settings, the encryption key |
| Metadata index | `<data-dir>/metadata/` | The index of every object and version |
| Objects | `<data-dir>/objects/` (or `storage.root`) | The data, each file with its `.metadata` sidecar |
| Audit log | `<data-dir>/audit.db` | Who did what |
| Configuration | `/etc/maxiofs/config.yaml` | Ports, paths, URLs |

Keep the recovery bundle and its passphrase **off the server**, apart from
the backups: it is the one thing that cannot be rebuilt.

## Backing up

Back up all of `<data-dir>` at the same moment. Copying the parts at
different times leaves an index that does not match the objects.

=== "Filesystem snapshot (recommended)"

    A snapshot of the volume holding the data directory (LVM, ZFS, Btrfs, a
    cloud disk snapshot) is taken in an instant while the server runs:

    ```bash
    # LVM example
    lvcreate --snapshot --size 20G --name maxiofs-snap /dev/vg0/maxiofs
    mount -o ro /dev/vg0/maxiofs-snap /mnt/snap
    rsync -a /mnt/snap/ backup-host:/backups/maxiofs/$(date +%F)/
    umount /mnt/snap && lvremove -y /dev/vg0/maxiofs-snap
    ```

    A restored snapshot starts like after a power cut: the server replays
    its log and checks the index against the files by itself.

=== "Cold copy"

    The simplest consistent copy, at the cost of a short stop:

    ```bash
    sudo systemctl stop maxiofs
    sudo tar -czf /backups/maxiofs-$(date +%F).tar.gz -C /var/lib maxiofs
    sudo systemctl start maxiofs
    ```

=== "Another S3 site"

    [Bucket replication](../console/buckets.md#replication) keeps a live copy
    of chosen buckets on AWS S3, another S3 service or another MaxIOFS. It
    protects the data, not the users and settings: combine it with one of the
    other two.

For a short window without writes during a snapshot, turn on maintenance mode
(**Settings → System**): reads keep working.

## Restoring a backup

1. Stop MaxIOFS.
2. Restore the whole data directory, and `config.yaml`.
3. Start MaxIOFS and check that users, buckets and objects are there.

## Rebuilding a lost index

If the metadata index or the database is lost or damaged but the object
files survived, a backup is not needed: `maxiofs recover` rebuilds the index
from the files.

```bash
sudo systemctl stop maxiofs

# Dry run: walks and verifies, writes nothing
sudo -u maxiofs maxiofs recover --data-dir /var/lib/maxiofs \
  --recovery-bundle /safe/maxiofs-recovery-bundle.json \
  --passphrase-file /safe/pass.txt --dry-run

# The rebuild, into <data-dir>/metadata-recovered
sudo -u maxiofs maxiofs recover --data-dir /var/lib/maxiofs \
  --recovery-bundle /safe/maxiofs-recovery-bundle.json \
  --passphrase-file /safe/pass.txt

# Swap it in
sudo mv /var/lib/maxiofs/metadata /var/lib/maxiofs/metadata.broken
sudo mv /var/lib/maxiofs/metadata-recovered /var/lib/maxiofs/metadata
sudo systemctl start maxiofs
```

Every 30 seconds it logs how far it is:

```text
level=info msg="Recovery progress" bucket="backups (5 of 6)" eta="2026-11-08 14:44" left=2m33s objects="759259 of 1189243" percent="37.1%" rate=9800/s
```

A run that stops continues where it stopped when started again with the same
flags. Pass `--storage-root` when the objects are outside
`<data-dir>/objects`.

### What comes back

| Comes back | From |
|------------|------|
| Buckets and their settings: owner, versioning, Object Lock, policy, lifecycle, CORS, website, notifications, tags, quota | The bucket's marker file |
| Objects and versions, with size, ETag, content type, dates | Each file's sidecar |
| Object Lock retention and legal hold of each version | Each file's sidecar |
| The encryption keys | The recovery bundle |

| Does not come back | Why |
|--------------------|-----|
| Delete markers | They exist only in the index: an object deleted in a versioned bucket reappears. Delete it again if needed. |
| User metadata, object tags and ACLs | They live only in the index |
| Users, keys, tenants (if the database was lost too) | Create them again; buckets of a missing owner come back owned by `admin` |

Objects whose key cannot be verified are still indexed and listed as failures
in the report, so nothing disappears silently.

## Cluster

In a cluster, every node holds the index of every bucket, and the
[protection](../console/cluster.md#high-availability) keeps each object on
several nodes. Losing one node loses nothing:

- **A node that comes back** is caught up by the others: the writes and
  deletions it missed are sent to it.
- **A node lost for good**: remove it from the cluster, add a new blank node,
  and the repair rebuilds the missing copies or shards.

Back up each node as above for disasters that reach every node at once.
