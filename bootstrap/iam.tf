# What the deploy stack in the repository root calls:
#
#   artifactregistry.writer     ko pushes to gcr.io/kontaindotme
#   dns.admin                   the kontain.me zone and its records
#   iam.serviceAccountViewer    read the runtime accounts below
#   monitoring.dashboardEditor  a dashboard per service
#   run.admin                   services, their IAM policy, domain mappings
#   serviceUsageConsumer        call APIs; it can't turn any on
#   storage.admin               the image bucket, its IAM, and Terraform state
#
# The deploy account can't create service accounts or set IAM policy on any
# of them. This stack creates the accounts Cloud Run runs as and grants act-as
# on each one. iam.googleapis.com doesn't support resource conditions, so a
# project-level serviceAccountUser grant would cover every account, including
# the Compute Engine default account.
#
# Never grant a CI account editor, owner, iam.serviceAccountAdmin,
# iam.serviceAccountTokenCreator, iam.roleAdmin, projectIamAdmin without a
# modifiedGrantsByRole condition, or any role with iam.serviceAccountKeys.create.

locals {
  github_deploy_roles = [
    "roles/artifactregistry.writer",
    "roles/dns.admin",
    "roles/iam.serviceAccountViewer",
    "roles/monitoring.dashboardEditor",
    "roles/run.admin",
    "roles/serviceusage.serviceUsageConsumer",
    "roles/storage.admin",
  ]

  # Accounts that the deploy stack's Cloud Run services run as, and their
  # display names. The deploy stack reads them with data sources.
  runtime_accounts = {
    "kontaindotme" = ""
    "no-auth"      = "Prober SA with no permissions"
  }
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

resource "google_service_account" "runtime" {
  for_each = local.runtime_accounts

  project      = local.project_id
  account_id   = each.key
  display_name = each.value
}

resource "google_service_account_iam_member" "gha_deployer_act_as" {
  for_each = local.runtime_accounts

  service_account_id = google_service_account.runtime[each.key].name
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.gha_deployer.member
}
