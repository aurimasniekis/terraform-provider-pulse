#!/usr/bin/env bash
# Runs examples/homelab against a throwaway nats-server + `pulsed serve` (dev profile):
# terraform init → plan → apply → plan (must be empty) → destroy.
# The provider is used through a dev_overrides CLI config in a temporary TF_CLI_CONFIG_FILE (~/.terraformrc is never
# touched); a local filesystem mirror lets `terraform init` succeed for the unpublished provider.
# Needs: bin/pulsed, bin/nats-server and bin/terraform-provider-pulse (make build pulse-bin), terraform, python3, curl,
# openssl.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
pulsed="${PULSE_PULSED_BINARY:-$root/bin/pulsed}"
nats="${PULSE_NATS_SERVER_BINARY:-$root/bin/nats-server}"
provider="$root/bin/terraform-provider-pulse"
free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'; }

work=$(mktemp -d)
chmod 700 "$work"
cleanup() {
  [[ -n "${pulsed_pid:-}" ]] && kill "$pulsed_pid" 2>/dev/null || true
  [[ -n "${nats_pid:-}" ]] && kill "$nats_pid" 2>/dev/null || true
  wait 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

NATS=$(free_port); MON=$(free_port); WS=$(free_port); ADMIN=$(free_port); METRICS=$(free_port)
openssl rand -hex 32 > "$work/master.key"
# pulsed refuses unknown PULSE_* variables (the provider's, CI's PULSE_COMMIT, ...), so run it with only its own.
pulse_env=(
  PULSE_PROFILE=dev PULSE_LOG_LEVEL=warn PULSE_MASTER_KEY_FILE="$work/master.key"
  PULSE_DATA_DIR="$work/pulsed" PULSE_NATS_DIR="$work/nats" PULSE_RAW_HOST=localhost
  PULSE_NATS_CLIENT_LISTEN="127.0.0.1:$NATS" PULSE_NATS_MONITOR_LISTEN="127.0.0.1:$MON"
  PULSE_NATS_WEBSOCKET_LISTEN="127.0.0.1:$WS" PULSE_NATS_URL="tls://127.0.0.1:$NATS" PULSE_NATS_TLS_NAME=nats
  PULSE_NATS_WEBSOCKET_URL="http://127.0.0.1:$WS" PULSE_ADMIN_ADDR="127.0.0.1:$ADMIN"
  PULSE_ADMIN_URL="http://127.0.0.1:$ADMIN" PULSE_METRICS_ADDR="127.0.0.1:$METRICS" PULSE_PUBLIC_ADDR=off
  PULSE_EVENTS_MAX_BYTES=256MiB
)
unset_pulse=()
while IFS= read -r v; do unset_pulse+=(-u "$v"); done < <(compgen -e | grep '^PULSE_' || true)
run_pulsed() { env ${unset_pulse[@]+"${unset_pulse[@]}"} "${pulse_env[@]}" "$pulsed" "$@"; }

run_pulsed init >"$work/init.log" 2>&1 || { echo "pulsed init failed:"; cat "$work/init.log"; exit 1; }
"$nats" -c "$work/nats/nats.conf" >"$work/nats.log" 2>&1 &
nats_pid=$!
run_pulsed serve >"$work/pulsed.log" 2>&1 &
pulsed_pid=$!
ready=false
for _ in $(seq 1 100); do curl -sf "http://127.0.0.1:$METRICS/readyz" >/dev/null && { ready=true; break; }; sleep 0.2; done
$ready || { echo "pulsed did not become ready:"; cat "$work/pulsed.log"; exit 1; }
out=$(run_pulsed admin token --email terraform@example.com --create --role administrator --days 1 --name homelab 2>&1) ||
  { echo "pulsed admin token failed:"; echo "$out"; exit 1; }
token=$(grep -o 'pulse_pat_[A-Za-z0-9_-]*' <<<"$out") || { echo "no token in output:"; echo "$out"; exit 1; }

mirror="$work/mirror/registry.terraform.io/aurimasniekis/pulse/0.0.1/$(go env GOOS)_$(go env GOARCH)"
mkdir -p "$mirror"
cp "$provider" "$mirror/terraform-provider-pulse_v0.0.1"
cat > "$work/dev.tfrc" <<HCL
provider_installation {
  dev_overrides {
    "aurimasniekis/pulse" = "$root/bin"
  }
  filesystem_mirror {
    path    = "$work/mirror"
    include = ["aurimasniekis/pulse"]
  }
  direct {
    exclude = ["aurimasniekis/pulse"]
  }
}
HCL
mkdir "$work/tf"
cp "$root"/examples/homelab/*.tf "$work/tf/"
cd "$work/tf"
export TF_CLI_CONFIG_FILE="$work/dev.tfrc" CHECKPOINT_DISABLE=1 TF_IN_AUTOMATION=1
export PULSE_ADMIN_URL="http://127.0.0.1:$ADMIN" PULSE_ADMIN_TOKEN="$token"
terraform init -input=false -no-color >/dev/null
terraform plan -input=false -no-color -out=tfplan | grep -E '^Plan:'
terraform apply -input=false -no-color tfplan | grep -E '^Apply complete'
set +e
terraform plan -input=false -no-color -detailed-exitcode >"$work/replan.txt"
code=$?
set -e
if [[ $code -ne 0 ]]; then
  echo "plan after apply is not empty (exit $code):"; cat "$work/replan.txt"; exit 1
fi
echo "Plan after apply: no changes."
terraform destroy -auto-approve -input=false -no-color | grep -E '^Destroy complete'
