# Console Guide

The web console runs on port **8081** (or behind your reverse proxy) and is
part of the same binary as the S3 API. Everything you can do with the S3 and
IAM APIs, and the administration that only exists in MaxIOFS, can be done
here.

![The console](../assets/screenshots/dashboard-light.png#only-light)
![The console](../assets/screenshots/dashboard-dark.png#only-dark)

## Layout

**Side menu.** The pages you may open. Collapse it with **Collapse** at the
bottom to gain room; the bottom also shows the version and whether a newer one
was released.

**Top bar**, from left to right:

| Control | What it does |
|---------|--------------|
| :material-weather-night: Theme | Light, dark, or the system's |
| Language | English, Español, Deutsch, Français, Italiano, Português, 日本語, 中文, Русский |
| :material-bell: Notifications | Live alerts: disk and quota warnings, integrity problems, security events |
| Your name | **My Profile** and **Log Out** |

**Banners** under the top bar tell you about things that need action: the
encryption key with no recovery bundle, the default admin password,
maintenance mode.

## Who sees what

The menu adapts to who signs in:

| Page | Global admin | Tenant admin | User |
|------|:---:|:---:|:---:|
| Dashboard, Buckets | :material-check: | :material-check: | :material-check: |
| Users, Access Keys | :material-check: | :material-check: (own tenant) | :material-check: (own account) |
| Groups, Tenants, Identity Providers | :material-check: | :material-check: (own tenant) | |
| Audit Logs | :material-check: | :material-check: (own tenant) | |
| IAM | :material-check: | | |
| Metrics, Security, Cluster, Settings | :material-check: | | |

- A **global administrator** has the `admin` role and no tenant. They manage
  the whole installation.
- A **tenant administrator** has the `admin` role inside a tenant. They manage
  that tenant's users, groups and buckets, and cannot leave it.
- A **user** sees the buckets their permissions reach and their own profile.

What a user can do inside a bucket is decided by IAM policies: see
[IAM](iam.md).

## Pages

<div class="grid cards" markdown>

-   [**Dashboard and Metrics**](dashboard.md)

    Totals, storage by bucket, and live system, storage and request metrics.

-   [**Buckets**](buckets.md)

    Create buckets and set versioning, Object Lock, lifecycle, policies,
    replication, notifications, inventory, website and quotas.

-   [**Objects and Sharing**](objects.md)

    Browse, upload, download, versions, tags, legal hold and share links.

-   [**Users, Groups and Tenants**](users.md)

    Accounts, access keys, temporary credentials, 2FA, groups and tenants
    with their quotas.

-   [**IAM**](iam.md)

    Permissions per user, managed policies and roles.

-   [**Identity Providers**](identity-providers.md)

    LDAP / Active Directory and OAuth2 / OIDC sign-in, group mappings and
    Auto Sync.

-   [**Security and Audit**](security.md)

    The security overview and the audit log.

-   [**Settings**](settings.md)

    Security, audit, storage, metrics, system, logging and email settings.

-   [**Cluster**](cluster.md)

    Nodes, high availability, storage classes and the cluster's health.

</div>
