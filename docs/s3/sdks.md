# SDKs

The AWS SDKs work with MaxIOFS once they have the endpoint and path-style
addressing. Credentials come from an access key created in the console.

=== "Python (boto3)"

    ```python
    import boto3
    from botocore.config import Config

    s3 = boto3.client(
        "s3",
        endpoint_url="https://s3.example.com",
        aws_access_key_id="AKIA...",
        aws_secret_access_key="...",
        region_name="us-east-1",
        config=Config(s3={"addressing_style": "path"}),
    )

    s3.create_bucket(Bucket="my-bucket")
    s3.upload_file("report.pdf", "my-bucket", "docs/report.pdf")

    for obj in s3.list_objects_v2(Bucket="my-bucket", Prefix="docs/").get("Contents", []):
        print(obj["Key"], obj["Size"])

    s3.download_file("my-bucket", "docs/report.pdf", "copy.pdf")

    url = s3.generate_presigned_url(
        "get_object", Params={"Bucket": "my-bucket", "Key": "docs/report.pdf"}, ExpiresIn=3600
    )
    ```

    `upload_file` and `download_file` switch to multipart transfers on their
    own for large files.

=== "Go (SDK v2)"

    ```go
    package main

    import (
        "context"
        "fmt"
        "os"

        "github.com/aws/aws-sdk-go-v2/aws"
        "github.com/aws/aws-sdk-go-v2/config"
        "github.com/aws/aws-sdk-go-v2/credentials"
        "github.com/aws/aws-sdk-go-v2/service/s3"
    )

    func main() {
        ctx := context.Background()
        cfg, err := config.LoadDefaultConfig(ctx,
            config.WithRegion("us-east-1"),
            config.WithCredentialsProvider(
                credentials.NewStaticCredentialsProvider("AKIA...", "...", "")),
        )
        if err != nil {
            panic(err)
        }
        client := s3.NewFromConfig(cfg, func(o *s3.Options) {
            o.BaseEndpoint = aws.String("https://s3.example.com")
            o.UsePathStyle = true
        })

        f, _ := os.Open("report.pdf")
        defer f.Close()
        _, err = client.PutObject(ctx, &s3.PutObjectInput{
            Bucket: aws.String("my-bucket"),
            Key:    aws.String("docs/report.pdf"),
            Body:   f,
        })
        if err != nil {
            panic(err)
        }

        out, _ := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String("my-bucket")})
        for _, o := range out.Contents {
            fmt.Println(*o.Key, *o.Size)
        }
    }
    ```

=== "JavaScript (SDK v3)"

    ```javascript
    import { S3Client, PutObjectCommand, ListObjectsV2Command, GetObjectCommand } from "@aws-sdk/client-s3";
    import { getSignedUrl } from "@aws-sdk/s3-request-presigner";
    import { readFile } from "node:fs/promises";

    const s3 = new S3Client({
      endpoint: "https://s3.example.com",
      region: "us-east-1",
      forcePathStyle: true,
      credentials: { accessKeyId: "AKIA...", secretAccessKey: "..." },
    });

    await s3.send(new PutObjectCommand({
      Bucket: "my-bucket",
      Key: "docs/report.pdf",
      Body: await readFile("report.pdf"),
      ContentType: "application/pdf",
    }));

    const { Contents = [] } = await s3.send(new ListObjectsV2Command({ Bucket: "my-bucket" }));
    Contents.forEach((o) => console.log(o.Key, o.Size));

    const url = await getSignedUrl(s3,
      new GetObjectCommand({ Bucket: "my-bucket", Key: "docs/report.pdf" }), { expiresIn: 3600 });
    ```

=== "Java (SDK v2)"

    ```java
    import java.net.URI;
    import java.nio.file.Paths;
    import software.amazon.awssdk.auth.credentials.AwsBasicCredentials;
    import software.amazon.awssdk.auth.credentials.StaticCredentialsProvider;
    import software.amazon.awssdk.regions.Region;
    import software.amazon.awssdk.services.s3.S3Client;
    import software.amazon.awssdk.services.s3.model.PutObjectRequest;

    S3Client s3 = S3Client.builder()
        .endpointOverride(URI.create("https://s3.example.com"))
        .region(Region.US_EAST_1)
        .forcePathStyle(true)
        .credentialsProvider(StaticCredentialsProvider.create(
            AwsBasicCredentials.create("AKIA...", "...")))
        .build();

    s3.putObject(
        PutObjectRequest.builder().bucket("my-bucket").key("docs/report.pdf").build(),
        Paths.get("report.pdf"));
    ```

=== ".NET"

    ```csharp
    using Amazon.S3;
    using Amazon.S3.Transfer;

    var s3 = new AmazonS3Client("AKIA...", "...", new AmazonS3Config
    {
        ServiceURL = "https://s3.example.com",
        ForcePathStyle = true,
        AuthenticationRegion = "us-east-1",
    });

    await new TransferUtility(s3).UploadAsync("report.pdf", "my-bucket", "docs/report.pdf");
    ```

## Keep keys out of the code

Every SDK reads the standard variables and the shared profile, so the code
above can drop the literal keys:

```bash
export AWS_ACCESS_KEY_ID=AKIA...
export AWS_SECRET_ACCESS_KEY=...
export AWS_ENDPOINT_URL=https://s3.example.com
```

For applications that should not hold permanent keys at all, issue
[temporary credentials](sts.md).

## Checksums in recent SDKs

Recent SDK versions add CRC checksums to every upload by default, sent as
`aws-chunked` bodies with a trailer. MaxIOFS reads them; no client setting is
needed.
