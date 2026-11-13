# rclone, s3cmd and mc

Any S3 client works. These are the settings for the most common ones; replace
the endpoint and keys with yours.

## rclone

```ini title="~/.config/rclone/rclone.conf"
[maxiofs]
type = s3
provider = Other
access_key_id = AKIA...
secret_access_key = ...
endpoint = https://s3.example.com
region = us-east-1
force_path_style = true
```

Or interactively: `rclone config`, then **s3** → **Other**.

```bash
rclone lsd maxiofs:                            # buckets
rclone mkdir maxiofs:my-bucket
rclone copy ./photos maxiofs:my-bucket/photos --progress
rclone sync ./site maxiofs:my-bucket/site      # mirror, deleting extra files
rclone ls maxiofs:my-bucket
rclone mount maxiofs:my-bucket /mnt/bucket     # needs FUSE
```

Copy between two storages, such as AWS and MaxIOFS:

```bash
rclone copy aws:old-bucket maxiofs:new-bucket --progress --transfers 16
```

## s3cmd

```ini title="~/.s3cfg"
[default]
access_key = AKIA...
secret_key = ...
host_base = s3.example.com
host_bucket = s3.example.com
use_https = True
signature_v2 = False
```

`host_bucket` equal to `host_base` makes s3cmd use path-style addresses.

```bash
s3cmd ls
s3cmd mb s3://my-bucket
s3cmd put report.pdf s3://my-bucket/docs/
s3cmd sync ./site/ s3://my-bucket/site/
s3cmd get s3://my-bucket/docs/report.pdf
```

## MinIO client (mc)

```bash
mc alias set maxiofs https://s3.example.com AKIA... SECRET
mc ls maxiofs
mc mb maxiofs/my-bucket
mc cp report.pdf maxiofs/my-bucket/docs/
mc mirror ./site maxiofs/my-bucket/site
mc stat maxiofs/my-bucket/docs/report.pdf
```

`mc admin` commands are for MinIO servers only; manage MaxIOFS with the
console or `aws iam`.

## Path style and virtual-hosted style

S3 clients address a bucket either in the path (`https://s3.example.com/my-bucket/key`)
or in the host name (`https://my-bucket.s3.example.com/key`). MaxIOFS serves
both:

- **Path style** works everywhere, with no DNS setup. Use it unless a client
  insists otherwise.
- **Virtual-hosted style** needs a wildcard DNS name, `*.s3.example.com`,
  pointing at the server, and `public_api_url: https://s3.example.com` in the
  configuration. The TLS certificate must cover the wildcard too.
