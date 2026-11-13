# maxiofs CLI

The `maxiofs` binary is the server and, through its subcommands, the
maintenance tools. Every subcommand works on the data directory and must run
with the **server stopped**.

```text
maxiofs [flags]              start the server
maxiofs recover              rebuild the metadata index from the object files
maxiofs reconcile            restore index entries of objects that are on disk
maxiofs repair-pointers      rebuild missing "latest version" pointers
maxiofs migrate-layout       move objects to the current on-disk layout
maxiofs completion <shell>   shell completion script
maxiofs --version
```

## Start the server

```bash
maxiofs --data-dir /var/lib/maxiofs
maxiofs --config /etc/maxiofs/config.yaml
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-c`, `--config` | | Configuration file |
| `-d`, `--data-dir` | | Data directory |
| `-l`, `--listen` | `:8080` | S3 API address |
| `--console-listen` | `:8081` | Web console address |
| `--log-level` | `info` | `debug`, `info`, `warn` or `error` |
| `--tls-cert`, `--tls-key` | | Serve HTTPS with this certificate and key |
| `-v`, `--version` | | Print the version and exit |

Flags win over `MAXIOFS_*` environment variables, which win over the
configuration file. The full list is in [Configuration](../reference/configuration.md).

```bash
# Listen only on localhost, with debug logs
maxiofs --data-dir ./data --listen 127.0.0.1:8080 --console-listen 127.0.0.1:8081 --log-level debug

# The same with environment variables
MAXIOFS_DATA_DIR=./data MAXIOFS_LOG_LEVEL=debug maxiofs
```

## recover

Rebuilds the metadata index (Pebble) from the object files, for when the
index is lost or damaged but the objects are intact. Buckets come back from
their markers, with their settings; objects and versions, with their Object
Lock retention, from each file's sidecar.

```bash
# 1. Check what it would do
maxiofs recover --data-dir /var/lib/maxiofs \
  --recovery-bundle /safe/maxiofs-recovery-bundle.json \
  --passphrase-file /safe/pass.txt --dry-run

# 2. Rebuild into a fresh directory
maxiofs recover --data-dir /var/lib/maxiofs \
  --recovery-bundle /safe/maxiofs-recovery-bundle.json \
  --passphrase-file /safe/pass.txt

# 3. Put it in place and start
mv /var/lib/maxiofs/metadata /var/lib/maxiofs/metadata.broken
mv /var/lib/maxiofs/metadata-recovered /var/lib/maxiofs/metadata
systemctl start maxiofs
```

| Flag | Meaning |
|------|---------|
| `--recovery-bundle` | The encryption [recovery bundle](../operations/encryption-keys.md). Needed to read encrypted objects: it restores the key and checks every object against it. |
| `--passphrase-file` | File holding the bundle's passphrase; without it, it is asked for |
| `--out-db` | Where to write the new index (default `<data-dir>/metadata-recovered`) |
| `--storage-root` | Where the objects are, when `storage.root` puts them outside `<data-dir>/objects` |
| `--dry-run` | Walk and verify, write nothing |
| `--verbose` | Log every object |

It never touches the existing index. It prints its progress every 30
seconds, with the time left. A run that stops continues where it stopped
when started again with the same flags. The full procedure is in
[Backup and Recovery](../operations/backup-recovery.md).

## reconcile

Undoes writes cut short between storing an object and indexing it, then
restores the index entry of any object that is on disk but no longer lists.

```bash
maxiofs reconcile --data-dir /var/lib/maxiofs
```

The server does this by itself after an unclean shutdown. Run it by hand
when an object you know is on disk does not appear. It only adds: it never
removes an entry, deletes a file or overwrites an existing entry.

## repair-pointers

Rebuilds the "latest version" pointer of keys whose versions are still in the
index but no longer list.

```bash
maxiofs repair-pointers --data-dir /var/lib/maxiofs --dry-run
maxiofs repair-pointers --data-dir /var/lib/maxiofs
```

It only writes missing pointers, copied from the newest surviving version,
Object Lock included. `--drop-orphans` also removes version entries that name
no bucket.

## migrate-layout

Moves objects from the old layout, where the key was the file path, to the
current one, where the file name is a digest of the key.

```bash
maxiofs migrate-layout --data-dir /var/lib/maxiofs --dry-run
maxiofs migrate-layout --data-dir /var/lib/maxiofs
```

The server does it by itself on the first start of a release that needs it.
Running it by hand first shows the outcome, or moves a large installation
outside a maintenance window. Nothing is removed before its replacement is in
place and verified, and an interrupted run continues where it stopped.

## Shell completion

```bash
maxiofs completion bash > /etc/bash_completion.d/maxiofs
maxiofs completion zsh  > "${fpath[1]}/_maxiofs"
maxiofs completion powershell | Out-String | Invoke-Expression
```
