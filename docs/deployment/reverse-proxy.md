# TLS and Reverse Proxy

Serve MaxIOFS over HTTPS before clients reach it from outside your network.
There are two ways:

- **A reverse proxy** (recommended): nginx, HAProxy, Caddy or a load
  balancer ends TLS and forwards to MaxIOFS on localhost.
- **TLS in MaxIOFS itself**: give it a certificate and key.

## Tell MaxIOFS who the proxy is

MaxIOFS takes the client's address from `X-Forwarded-For`, and the scheme
from `X-Forwarded-Proto`, only when the request comes from a proxy listed in
`trusted_proxies`. List the address the proxy reaches MaxIOFS from:

```yaml title="config.yaml"
trusted_proxies: ["127.0.0.1"]          # proxy on the same host
# trusted_proxies: ["10.0.0.5", "10.0.1.0/24"]   # a WAF or load balancer
```

Without it, the audit log, the sign-in rate limits and the `aws:SourceIp`
condition of bucket policies all see the proxy instead of the client.

Set the public addresses too, so links and presigned URLs point at the proxy:

```yaml title="config.yaml"
public_api_url: https://s3.example.com
public_console_url: https://console.example.com
```

## nginx

Two names, one for S3 and one for the console:

```nginx title="/etc/nginx/sites-available/maxiofs"
# S3 API
server {
    listen 443 ssl http2;
    server_name s3.example.com;

    ssl_certificate     /etc/letsencrypt/live/s3.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/s3.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    # Objects of any size, streamed without buffering
    client_max_body_size 0;
    proxy_request_buffering off;
    proxy_buffering off;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

# Web console
server {
    listen 443 ssl http2;
    server_name console.example.com;

    ssl_certificate     /etc/letsencrypt/live/console.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/console.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    client_max_body_size 0;
    proxy_request_buffering off;

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        # Live notifications are a long-lived stream
        proxy_read_timeout 1h;
    }
}

server {
    listen 80;
    server_name s3.example.com console.example.com;
    return 301 https://$host$request_uri;
}
```

```bash
sudo ln -s /etc/nginx/sites-available/maxiofs /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

!!! note "The `Host` header"
    S3 signatures include the host name. Always forward the original `Host`
    (`proxy_set_header Host $host`), or every signed request fails with
    `SignatureDoesNotMatch`.

### Certificates with Let's Encrypt

```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d s3.example.com -d console.example.com
```

## HAProxy

```haproxy title="/etc/haproxy/haproxy.cfg"
frontend https
    bind *:443 ssl crt /etc/haproxy/certs/
    http-request set-header X-Forwarded-Proto https
    option forwardfor
    use_backend console if { hdr(host) -i console.example.com }
    default_backend s3

backend s3
    option httpchk GET /health
    server maxiofs 127.0.0.1:8080 check

backend console
    option httpchk GET /api/v1/health
    timeout server 1h
    server maxiofs 127.0.0.1:8081 check
```

For a cluster, list every node in both backends: see [Cluster](cluster.md).

## TLS in MaxIOFS

Without a proxy, give MaxIOFS the certificate and key:

=== "config.yaml"

    ```yaml
    enable_tls: true
    cert_file: /etc/maxiofs/tls/fullchain.pem
    key_file: /etc/maxiofs/tls/privkey.pem
    ```

=== "Flags"

    ```bash
    maxiofs --data-dir /var/lib/maxiofs \
      --tls-cert /etc/maxiofs/tls/fullchain.pem \
      --tls-key /etc/maxiofs/tls/privkey.pem
    ```

Both ports then serve HTTPS. The `maxiofs` user must be able to read the
key file. Restart the service after renewing the certificate.

## Static websites

Buckets can be served as websites under their own host names, such as
`my-site.web.example.com`. Point a wildcard DNS name at the proxy and set the
base name:

```yaml title="config.yaml"
website_hostname: web.example.com
```

Forward `*.web.example.com` to the S3 port like `s3.example.com`. The
website itself is turned on per bucket: see [Buckets](../console/buckets.md#static-website).
