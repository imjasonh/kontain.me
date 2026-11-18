# GitHub Actions trust for the deploy stack in the repository root.
#
# Apply this stack locally with your own credentials, never from CI, so the
# deploy account can't change who may impersonate it.
#
# The provider accepts only deploy.yaml on main in imjasonh/kontain.me,
# matched by name and by the numeric IDs of the repository and its owner, so
# a repository that takes over the name after a rename or deletion is
# rejected. A workflow_dispatch run from any other branch is rejected too, as
# is any job that doesn't select the GitHub environment "gcp".

locals {
  project_id     = "kontaindotme"
  project_number = "7577580991"

  github_repository          = "imjasonh/kontain.me"
  github_repository_id       = "160939184"
  github_repository_owner_id = "210737"
  github_workflow            = ".github/workflows/deploy.yaml"
  github_environment         = "gcp"
}

resource "google_iam_workload_identity_pool" "gha" {
  project                   = local.project_id
  workload_identity_pool_id = "gha-pool"
}

resource "google_iam_workload_identity_pool_provider" "gha" {
  project                            = local.project_id
  workload_identity_pool_id          = google_iam_workload_identity_pool.gha.workload_identity_pool_id
  workload_identity_pool_provider_id = "gha-provider"
  display_name                       = "gha-provider"

  attribute_mapping = {
    "google.subject"       = "assertion.sub"
    "attribute.aud"        = "assertion.aud"
    "attribute.repository" = "assertion.repository"
  }

  attribute_condition = join(" && ", [
    "assertion.repository_owner_id == \"${local.github_repository_owner_id}\"",
    "assertion.repository_id == \"${local.github_repository_id}\"",
    "assertion.repository == \"${local.github_repository}\"",
    "assertion.ref == \"refs/heads/main\"",
    "assertion.ref_type == \"branch\"",
    "assertion.workflow_ref == \"${local.github_repository}/${local.github_workflow}@refs/heads/main\"",
    "assertion.environment == \"${local.github_environment}\"",
  ])

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

# APIs the deploy stack uses. The deploy account can't turn on APIs.
resource "google_project_service" "deploy" {
  for_each = toset([
    "dns.googleapis.com",
    "run.googleapis.com",
  ])

  project            = local.project_id
  service            = each.key
  disable_on_destroy = false
}
