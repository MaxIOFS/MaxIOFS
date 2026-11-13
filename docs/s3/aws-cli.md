# AWS CLI

The AWS CLI works with MaxIOFS unchanged: point it at the S3 port with an
endpoint URL. Both the high-level `aws s3` commands and the full
`aws s3api` work, as do `aws iam` and `aws sts`.

## Set up a profile

Create an access key in the console (your profile → **Create Access Key**),
then:

```bash
aws configure --profile maxiofs
# AWS Access Key ID:     AKIA...
# AWS Secret Access Key: ...
# Default region name:   us-east-1
# Default output format: json
```

Tell the profile where MaxIOFS is, once, in `~/.aws/config`:

```ini title="~/.aws/config"
[profile maxiofs]
region = us-east-1
endpoint_url = https://s3.example.com
```

`endpoint_url` in the config needs AWS CLI v2.13 / v1.29 or later. On older
versions add `--endpoint-url https://s3.example.com` to every command. The
examples below assume:

```bash
export AWS_PROFILE=maxiofs
```

!!! tip "Self-signed certificate"
    Against a test server with a self-signed certificate, add
    `--no-verify-ssl`, or better, `export AWS_CA_BUNDLE=/path/to/ca.pem`.

## Buckets

```bash
aws s3 mb s3://my-bucket                 # create
aws s3 ls                                # list buckets
aws s3 rb s3://my-bucket                 # delete (must be empty)
aws s3 rb s3://my-bucket --force         # delete with everything in it
```

## Objects

```bash
aws s3 cp report.pdf s3://my-bucket/docs/report.pdf     # upload
aws s3 cp s3://my-bucket/docs/report.pdf ./              # download
aws s3 ls s3://my-bucket/docs/                           # list a prefix
aws s3 ls s3://my-bucket --recursive --human-readable --summarize
aws s3 mv s3://my-bucket/a.txt s3://my-bucket/archive/a.txt
aws s3 rm s3://my-bucket/docs/report.pdf
aws s3 rm s3://my-bucket/tmp/ --recursive                # a whole prefix
```

### Sync folders

```bash
aws s3 sync ./site s3://my-bucket/site/                  # upload what changed
aws s3 sync s3://my-bucket/site/ ./site                  # download what changed
aws s3 sync ./site s3://my-bucket/site/ --delete         # also remove what is gone locally
```

### Large files

`aws s3 cp` and `sync` split files above 8 MB into parts and send them in
parallel. Tune it in the profile:

```ini title="~/.aws/config"
[profile maxiofs]
s3 =
  multipart_threshold = 64MB
  multipart_chunksize = 64MB
  max_concurrent_requests = 16
```

## Metadata and tags

```bash
aws s3api head-object --bucket my-bucket --key docs/report.pdf

aws s3 cp report.pdf s3://my-bucket/docs/ \
  --content-type application/pdf --metadata department=finance

aws s3api put-object-tagging --bucket my-bucket --key docs/report.pdf \
  --tagging 'TagSet=[{Key=project,Value=apollo}]'
aws s3api get-object-tagging --bucket my-bucket --key docs/report.pdf
```

## Versioning

```bash
aws s3api put-bucket-versioning --bucket my-bucket \
  --versioning-configuration Status=Enabled

aws s3api list-object-versions --bucket my-bucket --prefix docs/report.pdf

aws s3api get-object --bucket my-bucket --key docs/report.pdf \
  --version-id <version-id> report-old.pdf

# Remove one version for good
aws s3api delete-object --bucket my-bucket --key docs/report.pdf \
  --version-id <version-id>
```

Deleting an object without `--version-id` in a versioned bucket adds a
delete marker; the earlier versions stay.

## Object Lock

Object Lock is chosen when the bucket is created and cannot be turned off.

```bash
aws s3api create-bucket --bucket backups --object-lock-enabled-for-bucket

# Default retention for every new object
aws s3api put-object-lock-configuration --bucket backups \
  --object-lock-configuration '{"ObjectLockEnabled":"Enabled","Rule":{"DefaultRetention":{"Mode":"GOVERNANCE","Days":30}}}'

# Retention on one version
aws s3api put-object-retention --bucket backups --key db.dump \
  --retention '{"Mode":"COMPLIANCE","RetainUntilDate":"2027-01-01T00:00:00Z"}'

# Legal hold: keeps the version until it is lifted, whatever the retention
aws s3api put-object-legal-hold --bucket backups --key db.dump \
  --legal-hold Status=ON
```

| Mode | Who can delete before the date |
|------|-------------------------------|
| `GOVERNANCE` | A user allowed `s3:BypassGovernanceRetention`, sending `--bypass-governance-retention` |
| `COMPLIANCE` | Nobody, not even an administrator |

## Lifecycle

```bash
cat > lifecycle.json <<'EOF'
{
  "Rules": [
    {
      "ID": "expire-logs",
      "Status": "Enabled",
      "Filter": { "Prefix": "logs/" },
      "Expiration": { "Days": 90 }
    },
    {
      "ID": "old-versions",
      "Status": "Enabled",
      "Filter": {},
      "NoncurrentVersionExpiration": { "NoncurrentDays": 30 },
      "AbortIncompleteMultipartUpload": { "DaysAfterInitiation": 7 }
    }
  ]
}
EOF
aws s3api put-bucket-lifecycle-configuration --bucket my-bucket \
  --lifecycle-configuration file://lifecycle.json
aws s3api get-bucket-lifecycle-configuration --bucket my-bucket
```

## Bucket policy

```bash
cat > policy.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [{
    "Sid": "PublicRead",
    "Effect": "Allow",
    "Principal": "*",
    "Action": "s3:GetObject",
    "Resource": "arn:aws:s3:::my-bucket/public/*"
  }]
}
EOF
aws s3api put-public-access-block --bucket my-bucket \
  --public-access-block-configuration BlockPublicPolicy=false,RestrictPublicBuckets=false
aws s3api put-bucket-policy --bucket my-bucket --policy file://policy.json
```

A bucket that blocks public access, as buckets created in the console do by
default, refuses a public policy: the first command lifts that block.

## CORS

```bash
cat > cors.json <<'EOF'
{
  "CORSRules": [{
    "AllowedOrigins": ["https://app.example.com"],
    "AllowedMethods": ["GET", "PUT"],
    "AllowedHeaders": ["*"],
    "MaxAgeSeconds": 3000
  }]
}
EOF
aws s3api put-bucket-cors --bucket my-bucket --cors-configuration file://cors.json
```

## Presigned URLs

```bash
aws s3 presign s3://my-bucket/docs/report.pdf --expires-in 3600
```

More in [Presigned URLs](presigned-urls.md).

## IAM and STS

The same endpoint speaks IAM and STS:

```bash
aws iam list-users
aws iam create-user --user-name ci-runner
aws iam create-access-key --user-name ci-runner
aws sts get-session-token --duration-seconds 3600
```

See [STS](sts.md) and [IAM](../console/iam.md).

## Common errors

| Error | Usually means |
|-------|---------------|
| `SignatureDoesNotMatch` | Wrong secret key, or a proxy changed the `Host` header |
| `InvalidAccessKeyId` | The key was deleted, or belongs to another installation |
| `AccessDenied` | The key's user lacks permission, or Object Lock protects the version |
| `RequestTimeTooSkewed` | The client clock is more than 15 minutes off |
| `SlowDown` | The server asks clients to back off; the CLI retries by itself |
| `InsufficientStorage` (507) | The disk is 99.5% full: writes stop, reads and deletes work |
