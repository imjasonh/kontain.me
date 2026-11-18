// Created by bootstrap/, which also lets the deploy account act as it.
data "google_service_account" "no-auth" {
  account_id = "no-auth"
}

removed {
  from = google_service_account.no-auth
  lifecycle {
    destroy = false
  }
}

/*
module "prober" {
  source = "git::https://github.com/chainguard-dev/terraform-infra-common//modules/prober?ref=main"

  regions = module.networking.regions

  name            = "random-prober"
  project_id      = var.project_id
  service_account = data.google_service_account.no-auth.email

  importpath  = "github.com/imjasonh/kontain.me/cmd/prober"
  working_dir = path.module

  period = "600s" // 10 minutes

  env = { "REF" : "random.${var.domain}/random:1x1" }

  enable_alert          = true
  notification_channels = tolist(var.notification_channels)
}
*/
