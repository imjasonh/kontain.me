module github.com/imjasonh/kontain.me

go 1.17

replace github.com/awslabs/amazon-ecr-credential-helper/ecr-login => github.com/awslabs/amazon-ecr-credential-helper/ecr-login v0.0.0-20220216180153-3d7835abdf40

require (
	chainguard.dev/apko v0.2.1
	cloud.google.com/go/compute v1.7.0
	cloud.google.com/go/storage v1.23.0
	github.com/dustin/go-humanize v1.0.0
	github.com/google/go-containerregistry v0.11.0
	github.com/google/go-github/v32 v32.1.0
	github.com/google/ko v0.12.0
	github.com/imjasonh/delay v0.0.0-20210102151318-8339250e8458
	github.com/tmc/dot v0.0.0-20210901225022-f9bc17da75c0
	golang.org/x/mod v0.6.0-dev.0.20220419223038-86c51ed26bb4
	golang.org/x/oauth2 v0.0.0-20220722155238-128564f6959c
	golang.org/x/sync v0.0.0-20220722155255-886fb9371eb4
	google.golang.org/api v0.92.0
	gopkg.in/yaml.v2 v2.4.0
)

require (
	cloud.google.com/go v0.103.0 // indirect
	cloud.google.com/go/iam v0.3.0 // indirect
	github.com/asaskevich/govalidator v0.0.0-20210307081110-f21760c49a8d // indirect
	github.com/common-nighthawk/go-figure v0.0.0-20210622060536-734e95fb86be // indirect
	github.com/containerd/stargz-snapshotter/estargz v0.12.0 // indirect
	github.com/docker/cli v20.10.17+incompatible // indirect
	github.com/docker/distribution v2.8.1+incompatible // indirect
	github.com/docker/docker v20.10.17+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.6.4 // indirect
	github.com/dominodatalab/os-release v0.0.0-20190522011736-bcdb4a3e3c2f // indirect
	github.com/go-openapi/analysis v0.21.3 // indirect
	github.com/go-openapi/errors v0.20.2 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.20.0 // indirect
	github.com/go-openapi/loads v0.21.1 // indirect
	github.com/go-openapi/runtime v0.24.1 // indirect
	github.com/go-openapi/spec v0.20.6 // indirect
	github.com/go-openapi/strfmt v0.21.3 // indirect
	github.com/go-openapi/swag v0.22.1 // indirect
	github.com/go-openapi/validate v0.22.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-cmp v0.5.8 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.1.0 // indirect
	github.com/googleapis/gax-go/v2 v2.4.0 // indirect
	github.com/googleapis/go-type-adapters v1.0.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/klauspost/compress v1.15.8 // indirect
	github.com/letsencrypt/boulder v0.0.0-20220723181115-27de4befb95e // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.3-0.20220114050600-8b9d41f48198 // indirect
	github.com/package-url/packageurl-go v0.1.1-0.20220203205134-d70459300c8a // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/sigstore/cosign v1.11.0 // indirect
	github.com/sigstore/rekor v0.10.0 // indirect
	github.com/sigstore/sigstore v1.4.0 // indirect
	github.com/sirupsen/logrus v1.9.0 // indirect
	github.com/spf13/cobra v1.5.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/theupdateframework/go-tuf v0.3.1 // indirect
	github.com/titanous/rocacheck v0.0.0-20171023193734-afe73141d399 // indirect
	github.com/vbatts/tar-split v0.11.2 // indirect
	gitlab.alpinelinux.org/alpine/go v0.3.1 // indirect
	go.lsp.dev/uri v0.3.0 // indirect
	go.mongodb.org/mongo-driver v1.10.0 // indirect
	go.opencensus.io v0.23.0 // indirect
	golang.org/x/crypto v0.0.0-20220722155217-630584e8d5aa // indirect
	golang.org/x/net v0.0.0-20220805013720-a33c5aa5df48 // indirect
	golang.org/x/sys v0.0.0-20220728004956-3c1f35247d10 // indirect
	golang.org/x/term v0.0.0-20220526004731-065cf7ba2467 // indirect
	golang.org/x/text v0.3.7 // indirect
	golang.org/x/tools v0.1.12 // indirect
	golang.org/x/xerrors v0.0.0-20220609144429-65e65417b02f // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20220720214146-176da50484ac // indirect
	google.golang.org/grpc v1.48.0 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/square/go-jose.v2 v2.6.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	sigs.k8s.io/release-utils v0.7.3 // indirect
)
