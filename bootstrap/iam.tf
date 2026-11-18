locals {
  github_deploy_roles = [
    "roles/artifactregistry.writer",
    "roles/dns.admin",
    "roles/iam.serviceAccountAdmin",
    "roles/monitoring.dashboardEditor",
    "roles/monitoring.editor",
    "roles/run.admin",
    "roles/servicemanagement.admin",
    "roles/serviceusage.serviceUsageAdmin",
    "roles/storage.admin",
  ]

  # Service accounts that Cloud Run services in the deploy stack run as.
  cloud_run_runtime_accounts = [
    "kontaindotme@${local.project_id}.iam.gserviceaccount.com",
  ]
}

resource "google_service_account" "gha_deployer" {
  project    = local.project_id
  account_id = "gha-deployer"
  disabled   = true
}

# Authoritative: any other binding on this account, such as token creator,
# is removed on apply.
data "google_iam_policy" "gha_deployer" {
  binding {
    role    = "roles/iam.workloadIdentityUser"
    members = ["principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.gha.name}/attribute.repository/${local.github_repository}"]
  }
}

resource "google_service_account_iam_policy" "gha_deployer" {
  service_account_id = google_service_account.gha_deployer.name
  policy_data        = data.google_iam_policy.gha_deployer.policy_data
}

resource "google_project_iam_member" "gha_deployer" {
  for_each = toset(local.github_deploy_roles)

  project = local.project_id
  role    = each.value
  member  = google_service_account.gha_deployer.member
}

resource "google_service_account_iam_member" "gha_deployer_act_as" {
  for_each = toset(local.cloud_run_runtime_accounts)

  service_account_id = "projects/${local.project_id}/serviceAccounts/${each.value}"
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.gha_deployer.member
}
