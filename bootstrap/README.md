# GitHub Actions trust for kontain.me

This stack is the Workload Identity pool, its provider, and the
`gha-deployer` account that `deploy.yaml` uses to apply the stack in the
repository root. Apply it locally with your own credentials. Never apply it
from CI: the deploy account must not be able to change who can impersonate
it or what roles it holds.

```bash
cd bootstrap
terraform init
terraform plan
terraform apply
```

State lives in `gs://kontaindotme-tfstate` under the prefix `bootstrap`.

The provider accepts a token only from `.github/workflows/deploy.yaml` on
`main` in `imjasonh/kontain.me`, matched by name and by the numeric IDs of
the repository and its owner.
