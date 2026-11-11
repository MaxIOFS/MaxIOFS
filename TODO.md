# TODO

Pending work only. Completed work belongs in `CHANGELOG.md`.

## Later

- [ ] Decide when to drop `internal/layout/`. It reads the previous on-disk layout, so while it ships an installation on 1.6.0 or older can upgrade straight to any release. Removing it makes 1.7.x a required stop.

## Console themes (after 1.10.0, likely 1.10.1)

Asked in the project's discussions: a simple way to put an organization's logo, favicon and name on the console and its sign-in page, without rebuilding it. Not a full white-labeling system.

- [ ] Settings → Appearance: a new tab listing the themes, with Default selected. Selecting a theme makes it the active one.
- [ ] "Upload theme" button: a modal with the theme's name, the brand name (it replaces "MaxIOFS" in the title, the menu and the sign-in page), the logo and the favicon.
- [ ] One theme for the whole installation; per-tenant themes would need their own sign-in portals.
- [ ] With a theme of its own, the sign-in page shows its logo and name, and "Powered by MaxIOFS".
- [ ] Themes are files in `<data_dir>/themes/<theme>/` (`theme.json` and the images), not in the database. Default is built into the binary.
- [ ] Cluster: a theme uploaded on one node is sent to the others through the cluster's internal API (port 8082, TLS and HMAC), as settings and IAM are; each node writes its own folder. A node that was down gets what it lacks when the nodes compare their themes. A deleted theme goes through the deletion log. The active theme is a setting, carried by the settings sync.

Notes from the code:
- The Settings page builds its tabs from a fixed list (`categoryInfo`): Appearance is a new tab with its own content, as the security tab shows the recovery bundle.
- The active theme is a `system_settings` row the generic settings list does not offer for editing; the theme endpoints change it.
- A theme upload on a node that is not the coordinator is forwarded to it with its body (up to 8 MB) and its `Content-Type`, as any configuration write.
- The logo is drawn in several components (sidebar, top bar, sign-in, password change…): one component for it.

## Identity providers

- [ ] SCIM 2.0 (`/scim/v2`): the provider pushes creates, deactivations and group changes.

## Encryption

- [ ] Implement SSE-KMS.
- [ ] Evaluate recovery-key escrow.
- [ ] Evaluate Shamir split for recovery bundles.
- [ ] Evaluate recovery bundle restore from the console.
