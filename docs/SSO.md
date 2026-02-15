# MaxIOFS SSO / OAuth Authentication Guide

**Version**: 0.9.0
**Last Updated**: February 15, 2026

---

## Overview

MaxIOFS supports Single Sign-On (SSO) via OAuth2/OIDC providers. Users authenticate with their external identity provider (Google, Microsoft, etc.) and are either auto-provisioned or pre-authorized by an administrator.

**Supported providers**: Google Workspace, Microsoft Entra ID (Azure AD), and any custom OAuth2/OIDC provider.

**Key principle**: SSO users log in with their **email address** as their MaxIOFS username. This avoids username collisions between different identity providers.

---

## How SSO Login Works

1. User clicks the **"Sign in with Google"** (or Microsoft, etc.) button on the login page
2. User enters their **email address**
3. MaxIOFS redirects to the identity provider with the email pre-filled (`login_hint`)
4. User authenticates with the provider
5. The provider redirects back to MaxIOFS with the user's profile (email, name, groups)
6. MaxIOFS searches **all configured OAuth providers** to check if the user is authorized (see [Authorization Model](#authorization-model))
7. If authorized, the user is logged in. If it's their first login, an account is automatically created

---

## Setup Guide

### Step 1: Create the Identity Provider

1. Go to **Settings → Identity Providers**
2. Click **Add Provider**
3. Select **OAuth2** as the type
4. Fill in the provider configuration:

#### Google Workspace

| Field | Value |
|-------|-------|
| Name | Google |
| Preset | Google |
| Client ID | From [Google Cloud Console](https://console.cloud.google.com/apis/credentials) |
| Client Secret | From Google Cloud Console |
| Redirect URI | `https://your-maxiofs-domain/api/v1/auth/oauth/callback` |
| Scopes | `openid email profile` (add `https://www.googleapis.com/auth/admin.directory.group.readonly` for group-based access) |

**Google Cloud Console setup:**
1. Create a new OAuth 2.0 Client ID (Web application)
2. Add the redirect URI: `https://your-maxiofs-domain/api/v1/auth/oauth/callback`
3. Enable the Google People API (and Admin SDK if using groups)

#### Microsoft Entra ID (Azure AD)

| Field | Value |
|-------|-------|
| Name | Microsoft |
| Preset | Microsoft |
| Client ID | From [Azure Portal](https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade) |
| Client Secret | From Azure Portal |
| Redirect URI | `https://your-maxiofs-domain/api/v1/auth/oauth/callback` |
| Scopes | `openid email profile` (add `GroupMember.Read.All` for group-based access) |

**Azure Portal setup:**
1. Register a new application in Azure AD
2. Add the redirect URI as a Web platform redirect
3. Create a client secret under "Certificates & secrets"
4. Under "API Permissions", add Microsoft Graph: `openid`, `email`, `profile`

### Step 2: Test the Connection

1. Click the **plug icon** next to the provider in the list
2. Verify the connection test passes

### Step 3: Authorize Users

Choose one or both methods described in the [Authorization Model](#authorization-model) below.

---

## Authorization Model

There are **two ways** to authorize which users can log in via SSO. You can use either or both.

### Option A: Group-Based Access (Auto-Provisioning)

**Best for**: Authorizing entire teams or departments. Users are automatically created on first login.

1. Go to **Identity Providers → (your provider) → Group Mappings** (tree icon)
2. Click **Add Mapping**
3. Enter the external group identifier:
   - **Google**: Group email (e.g., `engineering@yourcompany.com`)
   - **Microsoft**: Group Object ID or display name
4. Select the **MaxIOFS role** for members of this group:
   - `admin` — Full access including user management
   - `user` — Standard access to buckets and objects
   - `readonly` — View-only access
5. Optionally enable **Auto Sync** to periodically sync group membership

**How it works**: When a user authenticates via SSO, MaxIOFS checks if they belong to any mapped group. If they do, an account is automatically created with:
- **Username** = their email address
- **Role** = the role from the matching group mapping
- **Tenant** = the provider's tenant

If the user belongs to multiple mapped groups, the highest-privilege role wins (admin > user > readonly).

**Example:**
```
Group Mapping: engineering@company.com → role: user
Group Mapping: platform-admins@company.com → role: admin

User: alice@company.com (member of engineering@company.com)
→ Auto-provisioned with role: user

User: bob@company.com (member of both groups)
→ Auto-provisioned with role: admin (highest privilege)

User: stranger@gmail.com (not in any mapped group)
→ Rejected: "You are not authorized to access this system"
```

### Option B: Individual User Access (Manual Import)

**Best for**: Authorizing specific people without needing group infrastructure.

1. **Create the user manually** in **Settings → Users → Create User**:
   - **Username**: The user's email address (e.g., `juan@gmail.com`)
   - **Role**: The desired role
   - **Password**: Leave empty (SSO users don't use passwords)
   - **Auth Provider**: Set to `oauth:{provider-id}` (the provider ID is shown in the Identity Providers list)
   - **External ID**: The user's email address

2. **Or use LDAP Browser** (for OAuth providers that support user search):
   - Go to **Identity Providers → (your provider) → Browse Users** (users icon)
   - Search for the user
   - Click **Import**

**How it works**: When the user authenticates via SSO, MaxIOFS finds their existing account by email and logs them in directly. No auto-provisioning needed.

### Combining Both Methods

Both methods work together:
- If a user is **already imported manually**, they log in immediately (SSO finds their existing account)
- If a user is **not imported** but belongs to a **mapped group**, they are auto-provisioned on first login
- If a user is **neither imported nor in a mapped group**, they are rejected

---

## Login Page Behavior

### SSO Buttons

When OAuth providers are configured and active, the login page shows **one button per provider type** below the standard username/password form (e.g., "Sign in with Google", "Sign in with Microsoft"). Even if multiple tenants configure the same provider type (e.g., multiple Google Workspace configurations), only one "Sign in with Google" button is shown.

When the user clicks an SSO button, they are prompted to enter their email address. MaxIOFS uses this email to:
- Pre-fill the provider's login page (`login_hint`)
- Determine which provider configuration to use (if multiple exist for the same type)

### Email Detection

If a user types an email address in the username field and submits the login form:
- MaxIOFS detects the `@` symbol and suggests using the SSO button instead
- The SSO buttons section is visually highlighted

If an existing SSO user tries to log in with username/password:
- MaxIOFS returns a message directing them to use the SSO button

---

## Role Priority

When a user matches multiple authorization rules, the highest-privilege role is assigned:

1. **admin** (highest)
2. **user**
3. **readonly** (lowest)

---

## Error Messages

| Error | Meaning | Solution |
|-------|---------|----------|
| `not_in_authorized_group` | User authenticated but is not in any authorized group and has no manual account | Admin must either add the user's group as a group mapping, or create the user manually |
| `no_group_mappings` | No group mappings are configured and user has no manual account | Admin must configure at least one group mapping or create the user manually |
| `email_conflict` | The user's email matches an existing local or LDAP account | Admin must resolve the conflict (rename or delete the existing account) |
| `missing_email` | The OAuth provider did not return an email address | Check provider configuration and user's account settings |
| `provisioning_failed` | Auto-provisioning failed (database error) | Check server logs for details |
| `oauth_denied` | User cancelled the SSO login | User should try again |
| `exchange_failed` | Token exchange with the provider failed | Check provider credentials and network connectivity |
| `provider_unavailable` | The SSO provider could not be reached | Check provider status and network connectivity |
| `account_inactive` | The user's account exists but is deactivated | Admin must reactivate the account |
| `account_locked` | Too many failed login attempts | Wait for the lockout period to expire |

---

## Multi-Tenant Configuration

Each identity provider can be scoped to a specific tenant:
- **Global providers** (no tenant): Available to all tenants
- **Tenant-scoped providers**: Only users in that tenant can use the provider

When a user is auto-provisioned, they are assigned to the provider's tenant.

### Multiple Tenants with the Same Provider Type

Multiple tenants can each configure their own Google (or Microsoft) OAuth provider. For example:
- **Tenant A**: Google OAuth for `companyA.com` Workspace
- **Tenant B**: Google OAuth for `companyB.com` Workspace

The login page shows a single "Sign in with Google" button. When the user enters their email and authenticates:
1. MaxIOFS checks **all** OAuth providers for an existing account matching that email
2. If no existing account, it checks **all** providers' group mappings for authorization
3. The user is assigned to the tenant of the provider where authorization was found

This means the email domain naturally routes users to the correct tenant — `alice@companyA.com` matches Tenant A's group mappings, and `bob@companyB.com` matches Tenant B's. No tenant information is exposed on the login page.

---

## Important Notes

- **SSO users cannot use password login** — they must always use the SSO button
- **One account per email** — if the same email exists across two OAuth providers, only the first account is used
- **LDAP users are unaffected** — LDAP authentication continues to work with username/password
- **Local users are unaffected** — standard username/password login works as before
- **2FA is supported** — SSO users can enable two-factor authentication after their first login
- **Existing imported users continue to work** — if users were imported before auto-provisioning was available, they still log in normally
