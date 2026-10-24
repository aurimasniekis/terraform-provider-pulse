variable "google_client_secret" {
  type      = string
  sensitive = true
}

variable "keycloak_client_secret" {
  type      = string
  sensitive = true
}

# Google Workspace sign-in. Register an OAuth client (web application) in the Google Cloud console
# with pulse_sso_provider.google.redirect_uri as its authorized redirect URI.
resource "pulse_sso_provider" "google" {
  slug          = "google"
  display_name  = "Google"
  issuer        = "https://accounts.google.com"
  client_id     = "1234567890-abc.apps.googleusercontent.com"
  client_secret = var.google_client_secret

  # Create users at their first sign-in when they belong to the workspace domain.
  provisioning = "jit"
  jit = {
    email_domains  = ["example.com"]
    hosted_domains = ["example.com"]
  }

  # Enable after a test login from the dashboard (Settings, Single sign-on).
  enabled = true
}

# A Keycloak realm on the home network with a private certificate authority.
resource "pulse_sso_provider" "keycloak" {
  slug                  = "keycloak"
  display_name          = "Home Keycloak"
  issuer                = "https://sso.home.arpa/realms/home"
  client_id             = "pulse"
  client_secret         = var.keycloak_client_secret
  scopes                = ["openid", "profile", "email", "groups"]
  groups_claim          = "groups"
  allow_private_network = true
  ca_bundle_pem         = file("${path.module}/home-ca.pem")
  end_session_enabled   = true
}

output "google_redirect_uri" {
  value = pulse_sso_provider.google.redirect_uri
}
