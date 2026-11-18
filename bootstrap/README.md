# GitHub Actions trust for kontain.me

This stack is the Workload Identity pool, its provider, the `gha-deployer`
account that `deploy.yaml` uses, the service accounts the Cloud Run services
run as, and the APIs the deploy stack uses. Apply it locally with your own
credentials. Never apply it from CI: the deploy account must not be able to
change who can impersonate it, what roles it holds, or which accounts it can
act as.

```bash
cd bootstrap
terraform init
terraform plan
terraform apply
```

State lives in `gs://kontaindotme-tfstate` under the prefix `bootstrap`.

The provider accepts a token only from `.github/workflows/deploy.yaml` on
`main` in `imjasonh/kontain.me`, in the GitHub environment `gcp`, matched by
name and by the numeric IDs of the repository and its owner. The `gcp`
environment must allow deployments from `main` only.

## Adopting the existing resources

In `kontaindotme` these resources predate this stack. Import them once,
before the first apply. A new project doesn't need this step.

```bash
P=kontaindotme
D=gha-deployer@$P.iam.gserviceaccount.com
terraform import google_iam_workload_identity_pool.gha projects/$P/locations/global/workloadIdentityPools/gha-pool
terraform import google_iam_workload_identity_pool_provider.gha projects/$P/locations/global/workloadIdentityPools/gha-pool/providers/gha-provider
terraform import 'google_project_service.deploy["dns.googleapis.com"]' $P/dns.googleapis.com
terraform import 'google_project_service.deploy["run.googleapis.com"]' $P/run.googleapis.com
terraform import google_service_account.gha_deployer projects/$P/serviceAccounts/$D
terraform import google_service_account_iam_policy.gha_deployer projects/$P/serviceAccounts/$D
for r in artifactregistry.writer dns.admin monitoring.dashboardEditor run.admin storage.admin; do
  terraform import "google_project_iam_member.gha_deployer[\"roles/$r\"]" "$P roles/$r serviceAccount:$D"
done
for sa in kontaindotme no-auth; do
  terraform import "google_service_account.runtime[\"$sa\"]" projects/$P/serviceAccounts/$sa@$P.iam.gserviceaccount.com
  terraform import "google_service_account_iam_member.gha_deployer_act_as[\"$sa\"]" \
    "projects/$P/serviceAccounts/$sa@$P.iam.gserviceaccount.com roles/iam.serviceAccountUser serviceAccount:$D"
done
```

Then apply this stack before the deploy stack runs again. The deploy stack
reads the runtime accounts with data sources and drops its old resources for
them with `removed` blocks.
