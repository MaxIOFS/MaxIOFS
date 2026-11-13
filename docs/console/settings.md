# Settings

**Settings** (global administrators) holds what can be changed while the
server runs. Changes are stored in the database, take effect at once and,
in a cluster, reach every node. What is read only at start (ports, paths,
TLS) stays in `config.yaml`: see [Configuration](../reference/configuration.md).

![Settings: Security](../assets/screenshots/settings-light.png#only-light)
![Settings: Security](../assets/screenshots/settings-dark.png#only-dark)

Each setting shows its key (such as `security.session_timeout`) and type.

## Security

| Setting | Key |
|---------|-----|
| Password length and required characters | `security.password_min_length`, `security.password_require_uppercase`, `security.password_require_numbers`, `security.password_require_special` |
| Failed sign-ins before a lock, and for how long | `security.max_failed_attempts`, `security.lockout_duration` |
| Session timeout and token lifetime | `security.session_timeout`, `security.access_token_lifetime` |
| Require 2FA for administrators | `security.require_2fa_admin` |
| Rate limits for sign-ins and the API | `security.ratelimit_enabled`, `security.ratelimit_login_per_minute`, `security.ratelimit_api_per_second` |
| Temporary credentials: longest session, federation | `security.sts_max_session_duration`, `security.sts_federation_enabled` |
| The AWS IAM API on the S3 port | `security.iam_api_enabled` |

The same tab holds the encryption key:

- **Encryption Recovery Bundle**: download a passphrase-protected copy of
  the key. Do it after the first start and after every rotation.
- **Rotate key**: creates a new key version. New objects use it; existing
  ones are re-wrapped in the background and stay readable all along. See
  [Encryption Keys](../operations/encryption-keys.md).
- **Background Encryption**: the progress of that re-wrapping.

## Audit

Whether the audit log is on, how many days it keeps entries, and whether it
records S3 operations and console operations.

## Storage

Defaults for new buckets (versioning, Object Lock retention days), and
`storage.abort_incomplete_multipart_days`: the days after which uploads left
incomplete are aborted in buckets with no rule of their own for it. `0`, the
default, aborts nothing, as in AWS.

## Metrics

Whether metrics are collected and how often, and the **token** Prometheus
must send to read `/metrics`. Without a token, `/metrics` answers `401`.

## System

| Setting | Key |
|---------|-----|
| Maintenance mode: reads work, writes are refused, a banner warns everyone | `system.maintenance_mode` |
| Disk use that raises a warning, and a critical alert | `system.disk_warning_threshold`, `system.disk_critical_threshold` |
| Largest body of a single S3 `PUT` or `POST`, 5 GB by default. Each part of a multipart upload counts on its own. | `system.max_upload_size_mb` |

## Logging

The server's log level and format, and where else to send logs:

- **Syslog**: host, port, protocol and tag.
- **HTTP**: a URL that receives logs in batches, with an optional token.
- **Frontend logs**: errors of the console in users' browsers.

## Email

The SMTP server alerts are sent through: host, port, TLS mode, user,
password and sender address. **Send Test Email** checks it.

![Settings: Email](../assets/screenshots/settings-email-light.png#only-light)
![Settings: Email](../assets/screenshots/settings-email-dark.png#only-dark)
