# A CI job publishes deployment events over HTTPS, only from the runner's network.
resource "pulse_http_token" "ci" {
  name        = "ci-deploys"
  description = "GitHub Actions deploy notifications"
  grants = [
    { action = "publish", subject = "evt.ci.deploy.>" },
  ]
  cidrs           = ["198.51.100.0/24"]
  expires_in_days = 90
}

output "ci_token" {
  value     = pulse_http_token.ci.secret
  sensitive = true
}
