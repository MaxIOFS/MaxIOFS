# First Steps

From a fresh installation to your first object, in seven steps. Everything
here happens in the web console at **http://your-server:8081**.

## 1. Sign in

Sign in with **admin / admin**. The dashboard opens, with a banner asking
for the encryption recovery bundle: step 3 takes care of it.

![First sign-in](../assets/screenshots/first-sign-in-light.png#only-light)
![First sign-in](../assets/screenshots/first-sign-in-dark.png#only-dark)

## 2. Change the administrator password

Open your profile (your name, top right → **My Profile**) and click
**Change Password**. Choose a password that follows the password policy of
**Settings → Security**.

![Profile page](../assets/screenshots/profile-light.png#only-light)
![Profile page](../assets/screenshots/profile-dark.png#only-dark)

## 3. Download the encryption recovery bundle

Every object is encrypted at rest, always. The key that protects them (the
KEK) lives in the database. If that database is ever lost, the recovery
bundle is the only way to read your objects again.

1. Go to **Settings → Security**.
2. Under **Encryption Recovery Bundle**, type a passphrase twice.
3. Click **Download bundle** and store the file **outside this server**,
   together with the passphrase.

![Encryption recovery bundle in Settings](../assets/screenshots/settings-light.png#only-light)
![Encryption recovery bundle in Settings](../assets/screenshots/settings-dark.png#only-dark)

!!! danger "Keep the passphrase"
    The bundle cannot be opened without its passphrase, and MaxIOFS does not
    store it. Download a new bundle each time you rotate the key.

## 4. Turn on two-factor authentication

Still in your profile, click **Enable 2FA**, scan the QR code with an
authenticator app (Google Authenticator, Microsoft Authenticator, 1Password,
Bitwarden…) and type the 6-digit code. Save the backup codes it shows: each
one signs you in once if you lose the phone.

![Two-factor setup](../assets/screenshots/two-factor-setup-light.png#only-light)
![Two-factor setup](../assets/screenshots/two-factor-setup-dark.png#only-dark)

## 5. Create a bucket

Go to **Buckets → Create Bucket**, type a name (lowercase letters, numbers,
dots and hyphens) and click **Create Bucket**. The tabs set versioning,
Object Lock, lifecycle and public access from the start; all of them except
Object Lock can be changed later. [Buckets](../console/buckets.md) explains
each option.

![Create a bucket](../assets/screenshots/bucket-create-light.png#only-light)
![Create a bucket](../assets/screenshots/bucket-create-dark.png#only-dark)

## 6. Create an access key

S3 clients sign their requests with an access key, not with your password.
In your profile, click **Create Access Key**.

!!! warning "Copy the secret now"
    The secret access key is shown only once. Copy both values before
    closing the window; if you lose the secret, delete the key and create a
    new one.

## 7. Upload your first object

=== "AWS CLI"

    Store the key in a profile of its own:

    ```bash
    aws configure --profile maxiofs
    # AWS Access Key ID:     <your access key>
    # AWS Secret Access Key: <your secret key>
    # Default region name:   us-east-1
    # Default output format: json
    ```

    Then point the CLI at MaxIOFS with `--endpoint-url`:

    ```bash
    export AWS_PROFILE=maxiofs
    export AWS_ENDPOINT_URL=http://your-server:8080   # AWS CLI v2.13+ and v1.29+

    aws s3 ls                                  # your buckets
    aws s3 cp report.pdf s3://my-bucket/       # upload a file
    aws s3 ls s3://my-bucket/                  # list it
    aws s3 sync ./photos s3://my-bucket/photos/  # upload a folder
    ```

    On older CLI versions, add `--endpoint-url http://your-server:8080` to
    each command instead of the variable.

=== "Web console"

    Open the bucket and click **Upload Files**. Drag files or a whole folder
    onto the window, or pick them with the buttons.

    ![Upload files](../assets/screenshots/bucket-upload-light.png#only-light)
    ![Upload files](../assets/screenshots/bucket-upload-dark.png#only-dark)

## Next

- [Console Guide](../console/index.md): every page of the console.
- [AWS CLI](../s3/aws-cli.md): more commands, versioning, Object Lock and
  multipart uploads.
- [TLS and Reverse Proxy](../deployment/reverse-proxy.md): before exposing the
  server beyond your network.
