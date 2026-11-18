import {
  to = google_iam_workload_identity_pool.gha
  id = "projects/${local.project_id}/locations/global/workloadIdentityPools/gha-pool"
}

import {
  to = google_iam_workload_identity_pool_provider.gha
  id = "projects/${local.project_id}/locations/global/workloadIdentityPools/gha-pool/providers/gha-provider"
}

import {
  to = google_service_account.gha_deployer
  id = "projects/${local.project_id}/serviceAccounts/gha-deployer@${local.project_id}.iam.gserviceaccount.com"
}

import {
  to = google_service_account_iam_policy.gha_deployer
  id = "projects/${local.project_id}/serviceAccounts/gha-deployer@${local.project_id}.iam.gserviceaccount.com"
}

import {
  for_each = toset(local.github_deploy_roles)
  to       = google_project_iam_member.gha_deployer[each.value]
  id       = "${local.project_id} ${each.value} serviceAccount:gha-deployer@${local.project_id}.iam.gserviceaccount.com"
}

import {
  for_each = toset(local.cloud_run_runtime_accounts)
  to       = google_service_account_iam_member.gha_deployer_act_as[each.value]
  id       = "projects/${local.project_id}/serviceAccounts/${each.value} roles/iam.serviceAccountUser serviceAccount:gha-deployer@${local.project_id}.iam.gserviceaccount.com"
}
