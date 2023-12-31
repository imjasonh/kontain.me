resource "google_service_account" "no-auth" {
  account_id   = "no-auth"
  display_name = "Prober SA with no permissions"
}

module "prober" {
  //source  = "chainguard-dev/prober/google"
  //version = "v0.5.0"

  source = "git::https://github.com/chainguard-dev/terraform-google-prober?ref=main"

  name            = "random-prober"
  project_id      = var.project_id
  service_account = google_service_account.no-auth.email

  importpath  = "github.com/imjasonh/kontain.me/cmd/prober"
  working_dir = path.module

  period = "600s" // 10 minutes

  env = { "REF" : "random.${var.domain}/random:1x1" }

  enable_alert          = true
  notification_channels = tolist(var.notification_channels)
}
