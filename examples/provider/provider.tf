terraform {
  required_providers {
    pulse = {
      source  = "aurimasniekis/pulse"
      version = "~> 0.1"
    }
  }
}

# Pulse accepts personal access tokens only on its loopback admin listener: run Terraform on the server, or open a
# tunnel first with `ssh -L 8080:127.0.0.1:8080 you@vps.example.com`.
# admin_url and token can also come from PULSE_ADMIN_URL and PULSE_ADMIN_TOKEN (or PULSE_ADMIN_TOKEN_FILE).
provider "pulse" {
  admin_url  = "http://127.0.0.1:8080"
  token_file = pathexpand("~/.config/pulse/admin.token") # a pulse_pat_… token with the administrator role
}
