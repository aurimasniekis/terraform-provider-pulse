# Changelog

All notable changes to this provider are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-10-25

First release, for [Pulse 0.1.0](https://github.com/aurimasniekis/go-pulse/releases/tag/v0.1.0).

- Resources: `pulse_client` (grants, `.creds`, rotation), `pulse_http_token`, `pulse_consumer`, `pulse_stream`,
  `pulse_kv_bucket`, `pulse_kv_entry`, `pulse_object_bucket`, `pulse_inbound_webhook`, `pulse_outbound_webhook`,
  `pulse_alert_rule`, `pulse_builtin_alert_rule`, `pulse_allowlist_entry`, `pulse_ban`, `pulse_user`,
  `pulse_user_identity`, `pulse_sso_provider`, `pulse_sso_mapping` and `pulse_auth_settings`.
- Data sources: `pulse_system_info`, `pulse_stream`, `pulse_client` and `pulse_user`.
- Every resource supports import. Credentials, tokens and generated webhook secrets are returned once and kept in
  state as sensitive values; destroying a client or HTTP token revokes it and removes its record.
- Authenticates with a Pulse personal access token with the `administrator` role on the loopback admin listener.

[Unreleased]: https://github.com/aurimasniekis/terraform-provider-pulse/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/aurimasniekis/terraform-provider-pulse/releases/tag/v0.1.0
