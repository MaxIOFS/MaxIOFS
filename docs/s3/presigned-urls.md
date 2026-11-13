# Presigned URLs

A presigned URL lets anyone with the link do one thing, such as download or
upload one object, for a limited time, without credentials. It is signed with
your access key and carries your permissions.

## Download link

```bash
aws s3 presign s3://my-bucket/docs/report.pdf --expires-in 3600
```

```text
https://s3.example.com/my-bucket/docs/report.pdf?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=...&X-Amz-Expires=3600&X-Amz-Signature=...
```

Anyone can open it with a browser or `curl` until it expires. The maximum
for Signature V4 is 7 days (`604800` seconds).

## Upload link

The CLI only presigns downloads; SDKs presign any operation. A link that lets
someone upload one file:

=== "Python"

    ```python
    url = s3.generate_presigned_url(
        "put_object",
        Params={"Bucket": "uploads", "Key": "incoming/photo.jpg", "ContentType": "image/jpeg"},
        ExpiresIn=900,
    )
    ```

=== "JavaScript"

    ```javascript
    import { PutObjectCommand } from "@aws-sdk/client-s3";
    import { getSignedUrl } from "@aws-sdk/s3-request-presigner";

    const url = await getSignedUrl(s3,
      new PutObjectCommand({ Bucket: "uploads", Key: "incoming/photo.jpg", ContentType: "image/jpeg" }),
      { expiresIn: 900 });
    ```

The person uploading then sends:

```bash
curl -X PUT -H "Content-Type: image/jpeg" --upload-file photo.jpg "<url>"
```

## Browser form uploads (POST policy)

For an HTML form that uploads straight to a bucket, the server signs a POST
policy with conditions (key prefix, size range, expiration), and the browser
posts the file with it:

```python
post = s3.generate_presigned_post(
    "uploads", "incoming/${filename}",
    Conditions=[["content-length-range", 1, 10 * 1024 * 1024]],
    ExpiresIn=600,
)
# post["url"] and post["fields"] go into the <form>
```

```html
<form action="{{ url }}" method="post" enctype="multipart/form-data">
  <!-- one hidden input per entry of post["fields"] -->
  <input type="file" name="file">
  <button>Upload</button>
</form>
```

MaxIOFS checks the policy's expiration and every condition, and refuses a
file outside the size range.

## Things to know

- **The link lives as long as the key.** Deleting the access key that signed
  it voids every link it signed, before they expire.
- **The address in the link** comes from the endpoint the client used. To
  hand out links that work outside your network, sign them against the public
  name (`https://s3.example.com`), not `localhost`.
- **Block Public Access** does not affect presigned URLs: they carry a
  signature, so they are not public access.
- For links managed from the console, with no expiry and a button to end
  them, use [share links](../console/objects.md#share-an-object).
