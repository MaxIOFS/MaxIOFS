# Data Integrity

MaxIOFS checks that what it stored is still what it reads back.

## On every read

Objects are encrypted in authenticated 64 KB chunks. A byte changed on disk
fails the read of that chunk instead of returning wrong data.

## The daily scan

A background scan reads every version of every bucket once a day and checks:

- that the file is there;
- that it decrypts, chunk by chunk;
- that its MD5 matches the ETag, recomputed for objects written in parts.

The first scan runs a day after the server starts. It reads all the data:
on several terabytes it takes hours, sharing the disks with normal traffic.
A server stop interrupts it cleanly.

When it finds a problem it:

- records it in the [audit log](../console/security.md#audit-log);
- raises an alert in the console's notifications and, with email configured,
  by email, saying what was found and why.

## Check one bucket now

In **Buckets**, the shield icon of a bucket runs **Verify integrity** on it in
the background; click it again to see progress and the result. The same from
the API:

```bash
curl -X POST -H "Authorization: Bearer <console token>" \
  https://console.example.com/api/v1/buckets/my-bucket/verify-integrity
```

Run it after moving data between disks, after a filesystem or disk error,
and from time to time on critical buckets.

## When something is found

| Finding | What to do |
|---------|------------|
| A missing file | Look for disk or filesystem errors (`dmesg`, `smartctl`). Restore the object from a backup or a replica. |
| A changed byte (fails to decrypt or MD5 mismatch) | The disk returned different data: check its health, and restore the object from a backup or a replica |
| An object that is on disk but does not list | Run [`maxiofs reconcile`](../cli/index.md#reconcile) with the server stopped |

In a cluster with copies or parity, the repair rebuilds a damaged copy from
the healthy ones.

## After a crash

A stop cut short (power loss, `kill -9`, out of memory) needs no action. On
the next start the server:

1. replays the index's write-ahead log;
2. finishes or undoes writes caught halfway;
3. checks, in the background while serving, that every object on disk is in
   the index, and re-indexes any that is not.

It logs:

```text
level=warning msg="Unclean shutdown detected — reconciling metadata store against on-disk objects in the background"
level=info msg="Unclean-shutdown reconciliation finished — store was consistent"
```

Stop the server with `systemctl stop maxiofs`, not `kill -9`, to skip all of
this.
