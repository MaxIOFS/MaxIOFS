# Veeam

MaxIOFS works as an S3-compatible object storage repository for Veeam Backup
& Replication, including **immutable backups** through Object Lock. It
speaks Veeam's Smart Object Storage API (SOSAPI), so Veeam reads the
repository's capacity and capabilities from it.

## 1. Prepare MaxIOFS

1. **A bucket with Object Lock.** In the console, **Buckets → Create
   Bucket**, name it (for example `veeam-backups`) and turn on
   **Object Lock** in the *Object Lock & WORM* tab. Leave the default
   retention off: Veeam sets the retention of each object itself.
2. **A user for Veeam.** **Users → Create User**, for example `veeam`, role
   *User*. In **IAM → Permissions**, add the bucket and give it full access
   to that bucket only.
3. **An access key** for that user: open the user → **Create Access Key**,
   and copy both values.
4. **Optional: a quota** on the bucket (**Settings → Quota**). Veeam then
   sees the quota as the repository's capacity.

!!! tip "Large repositories"
    Veeam stores many small objects. Above about 20 000 objects per bucket,
    raise the metadata cache in `config.yaml`:

    ```yaml
    storage:
      metadata_cache_size_mb: 1024
    ```

## 2. Add the repository in Veeam

In Veeam Backup & Replication:

1. **Backup Infrastructure → Backup Repositories → Add Repository →
   Object Storage → S3 Compatible**.
2. **Name**: for example `MaxIOFS`.
3. **Account**:
    - *Service point*: `https://s3.example.com` (the S3 port, or your proxy)
    - *Region*: `us-east-1`
    - *Credentials*: the access key and secret of the `veeam` user
4. **Bucket**: browse and choose `veeam-backups`, pick or create a folder.
5. Tick **Make recent backups immutable for** and choose the days.
6. Finish the wizard and use the repository in a backup job, directly or as
   the capacity tier of a scale-out repository.

## What Veeam sees

| | Comes from |
|---|---|
| Capacity and free space | The bucket's quota; else the tenant's quota less what it uses; else the free space of the disk (in a cluster, of the STANDARD storage class) |
| Immutability | Object Lock on the bucket |
| IAM / STS | Advertised when the IAM API is on (`security.iam_api_enabled`) and `public_api_url` is set |

## Immutability in practice

- Each backup object gets a retention date from Veeam. Until then nobody,
  administrators included, can delete or overwrite it in *Compliance* mode;
  in *Governance* mode only users allowed to bypass it can.
- Deleting a backup in Veeam before its immutability ends fails, as it
  should.
- A bucket created with Object Lock keeps it forever.

## Troubleshooting

| Symptom | Check |
|---------|-------|
| `SignatureDoesNotMatch` | The secret key; a proxy in front must forward the original `Host` |
| Certificate errors | Veeam must trust the certificate of the service point; use a certificate from a known authority or import the CA on the Veeam server |
| The immutability option is greyed out | The bucket was created without Object Lock: create a new one with it |
| `SlowDown` in the Veeam log | The server asks Veeam to slow down; Veeam retries by itself. It is back-pressure, not a failure |
| `507 InsufficientStorage` | The disk is 99.5% full: free space or grow the volume |
