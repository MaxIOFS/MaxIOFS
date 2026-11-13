# Spark and Hadoop (S3A)

Hadoop's S3A connector, and Spark through it, read and write MaxIOFS like
any S3 store. It was checked with the hadoop-aws integration tests of Hadoop
3.4.3, and with Spark 3.5 and 4.1 writing and reading Parquet through both
the file and the magic committers.

## Hadoop configuration

```xml title="core-site.xml"
<configuration>
  <property>
    <name>fs.s3a.endpoint</name>
    <value>https://s3.example.com</value>
  </property>
  <property>
    <name>fs.s3a.endpoint.region</name>
    <value>us-east-1</value>
  </property>
  <property>
    <name>fs.s3a.path.style.access</name>
    <value>true</value>
  </property>
  <property>
    <name>fs.s3a.access.key</name>
    <value>AKIA...</value>
  </property>
  <property>
    <name>fs.s3a.secret.key</name>
    <value>...</value>
  </property>
</configuration>
```

```bash
hadoop fs -ls s3a://my-bucket/
hadoop fs -put data.csv s3a://my-bucket/raw/
```

## Spark

```bash
spark-submit \
  --packages org.apache.hadoop:hadoop-aws:3.4.3 \
  --conf spark.hadoop.fs.s3a.endpoint=https://s3.example.com \
  --conf spark.hadoop.fs.s3a.endpoint.region=us-east-1 \
  --conf spark.hadoop.fs.s3a.path.style.access=true \
  --conf spark.hadoop.fs.s3a.access.key=AKIA... \
  --conf spark.hadoop.fs.s3a.secret.key=... \
  job.py
```

Match the `hadoop-aws` version to the Hadoop your Spark is built with.

```python title="job.py"
from pyspark.sql import SparkSession

spark = SparkSession.builder.appName("maxiofs-demo").getOrCreate()

df = spark.read.option("header", True).csv("s3a://my-bucket/raw/data.csv")
df.write.mode("overwrite").parquet("s3a://my-bucket/curated/data/")
spark.read.parquet("s3a://my-bucket/curated/data/").show()
```

## Committers

For jobs that write many files, use the **magic committer**: it writes
straight to the destination with multipart uploads and needs no rename.

```bash
  --conf spark.hadoop.fs.s3a.committer.name=magic \
  --conf spark.hadoop.fs.s3a.committer.magic.enabled=true \
  --conf spark.sql.sources.commitProtocolClass=org.apache.spark.internal.io.cloud.PathOutputCommitProtocol \
  --conf spark.sql.parquet.output.committer.class=org.apache.spark.internal.io.cloud.BindingParquetOutputCommitter
```

The last two need Spark's `spark-hadoop-cloud` module on the classpath.

## Notes

- Without TLS, set `fs.s3a.connection.ssl.enabled=false` and use an
  `http://` endpoint.
- Objects moved to a colder [storage class](../console/cluster.md#storage-classes)
  are read like any other; there is no restore step.
- Prefer temporary credentials for shared clusters: S3A takes them with
  `fs.s3a.aws.credentials.provider=org.apache.hadoop.fs.s3a.TemporaryAWSCredentialsProvider`
  and `fs.s3a.session.token`. See [STS](../s3/sts.md).
