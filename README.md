# Terraform provider for Pulse

Manage a [Pulse](https://aurimasniekis.github.io/go-pulse/) server (a personal durable event bus on NATS JetStream) as
code: clients and their grants, HTTP tokens, streams, consumers, KV and object buckets, webhooks in and out, alert rules,
firewall allowlist entries and bans, dashboard users, OpenID Connect single sign-on and sign-in settings.

Built with [terraform-plugin-framework](https://github.com/hashicorp/terraform-plugin-framework) (protocol 6). Tested
against Pulse's admin API with Terraform 1.11.

## Usage

```hcl
terraform {
  required_providers {
    pulse = {
      source = "aurimasniekis/pulse"
    }
  }
}

provider "pulse" {
  admin_url  = "http://127.0.0.1:8080"                  # or PULSE_ADMIN_URL (this is the default)
  token_file = pathexpand("~/.config/pulse/admin.token") # or token / PULSE_ADMIN_TOKEN / PULSE_ADMIN_TOKEN_FILE
}

resource "pulse_client" "door_bot" {
  name = "door-bot"
  grants = [
    { action = "publish", subject = "evt.home.door.>" },
  ]
}
```

The provider authenticates with a Pulse **personal access token** (`pulse_pat_…`) with the `administrator` role, the
same kind the `pulse` CLI uses. Create one on the dashboard's **Account → Personal access tokens** page, or on the
server:

```sh
docker compose exec pulsed pulsed admin token --email terraform@example.com --create --role administrator --days 30
```

Pulse accepts personal access tokens **only on its loopback admin listener**, so run Terraform on the server or through
an SSH tunnel:

```sh
ssh -N -L 8080:127.0.0.1:8080 you@vps.example.com &
terraform apply
```

See [`examples/`](examples) for one example per resource and [`examples/homelab`](examples/homelab/main.tf) for a
complete setup: tools with least-privilege credentials, a consumer, streams and buckets, webhooks, alerts, a firewall
entry and dashboard access for a household through single sign-on.

### Resources and data sources

| Resource | What it manages | Import ID |
|---|---|---|
| `pulse_client` | A tool's NATS identity, its grants and `.creds` | `<client id or name>` |
| `pulse_http_token` | A bearer token for the public HTTP API | `<token id>` |
| `pulse_consumer` | A durable pull consumer (e.g. a client's `<id>_<name>`) | `<stream>/<name>` |
| `pulse_stream` | A user stream next to `EVENTS` | `<name>` |
| `pulse_kv_bucket` | A key-value bucket | `<name>` |
| `pulse_kv_entry` | One key in a KV bucket | `<bucket>/<key>` |
| `pulse_object_bucket` | An object store bucket | `<name>` |
| `pulse_inbound_webhook` | A signed webhook endpoint that publishes to a subject | `<hook id>` |
| `pulse_outbound_webhook` | Delivery of events to an HTTPS URL | `<hook id>` |
| `pulse_alert_rule` | A custom threshold alert rule | `<rule id>` |
| `pulse_builtin_alert_rule` | Settings of a built-in alert rule | `<kind>` |
| `pulse_allowlist_entry` | A network that is never banned | `<network>` |
| `pulse_ban` | A manual ban | `<network>` |
| `pulse_user` | A dashboard user and their roles | `<user id or email>` |
| `pulse_user_identity` | A single sign-on identity linked to a user | `<user id>/<provider id>/<subject>` |
| `pulse_sso_provider` | An OpenID Connect provider | `<provider id or slug>` |
| `pulse_sso_mapping` | A provider group → role mapping | `<provider id>/<mapping id>` |
| `pulse_auth_settings` | Sign-in settings (SSO-only) | any |

Data sources: `pulse_system_info` (version, NATS URL, public URL, internal CA), `pulse_stream` (any stream, including
`EVENTS`), `pulse_client` (by ID or name), `pulse_user` (by ID or email).

Full documentation is generated into [`docs/`](docs).

### Behaviour worth knowing

- **Secrets are shown once.** A client's `.creds`, an HTTP token, and generated webhook secrets are returned by Pulse
  only when they are created or rotated. They are stored in state (sensitive) and never re-read, so protect the state.
  Rotate with `creds_version` (clients) or `secret_version` (webhooks). After `terraform import` they are unknown until
  rotated. An SSO provider's `client_secret` is write-only in Pulse and kept from configuration.
- **Revocation.** Destroying a client or HTTP token revokes it and then removes its record, so its name can be used
  again. A client or token revoked outside Terraform is planned for re-creation under the same name.
- **Immutable objects.** HTTP tokens, SSO mappings, user identities and bans cannot be edited in Pulse; any change
  replaces them. A consumer's start position (`deliver_policy`, `opt_start_seq`, `opt_start_time`) and a stream's
  `storage` force replacement too.
- **Defaults.** Optional attributes default to Pulse's own defaults (for example `ack_wait_seconds = 30`,
  `max_bytes = -1`), so removing a setting from configuration restores the default. Destroying a
  `pulse_builtin_alert_rule` restores the rule's built-in settings; destroying `pulse_auth_settings` turns password
  sign-in back on.
- **Drift.** Objects deleted outside Terraform are removed from state on refresh and planned for re-creation. A KV
  value changed by a tool is put back on the next apply.
- **Retries.** Reads and deletes are retried with exponential backoff on network errors and 5xx answers. Creates and
  updates are not idempotent in Pulse, so they are retried only after a `429` (refused before anything happened).

## Development

Requirements: Go 1.27, Terraform ≥ 1.5, `golangci-lint` v2, and a local checkout of Pulse for the acceptance tests.

```sh
make build          # bin/terraform-provider-pulse
make test           # unit tests (API client against httptest, schema and config validation)
make lint           # golangci-lint
make docs           # regenerate docs/ with tfplugindocs
make pulse-src      # export Pulse @ PULSE_COMMIT from PULSE_REPO (default ../pulse) into .pulse-src
make pulse-bin      # build bin/pulsed and bin/nats-server (the version Pulse pins) from .pulse-src
make testacc        # acceptance tests (TF_ACC=1) against a real pulsed
make homelab        # init/plan/apply/plan/destroy examples/homelab against a throwaway pulsed
```

### Acceptance tests

`TF_ACC=1 go test ./...` runs `pulsed init`, starts `nats-server` with the generated configuration and `pulsed serve`
in the dev profile from `TestMain` (free ports, temporary directories), mints an administrator and a viewer personal
access token with `pulsed admin token`, and points the provider at the loopback admin listener. Single sign-on tests run
a fake OpenID provider in the test process. Set `PULSE_PULSED_BINARY`/`PULSE_NATS_SERVER_BINARY` to use other binaries
and `TF_ACC_TERRAFORM_PATH` to choose the Terraform CLI (the harness uses the one on `PATH`, so nothing is downloaded).

Every resource is tested for create, read, in-place update (where Pulse supports it), import with `ImportStateVerify`,
destroy, drift (the object is deleted through the API and the next plan must re-create it), replacement of immutable
attributes and the errors Pulse returns.

### Using a local build

Point Terraform at the build with a CLI configuration file and `TF_CLI_CONFIG_FILE` (leave `~/.terraformrc` alone):

```hcl
provider_installation {
  dev_overrides {
    "aurimasniekis/pulse" = "/path/to/terraform-provider-pulse/bin"
  }
  direct {}
}
```

With `dev_overrides`, skip `terraform init` (or add a `filesystem_mirror` for the provider, as
[`scripts/verify-homelab.sh`](scripts/verify-homelab.sh) does).

## License

Apache License 2.0. Copyright 2026 Aurimas Niekis.
