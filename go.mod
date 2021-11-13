module github.com/imjasonh/kontain.me

go 1.15

require (
	cloud.google.com/go v0.97.0
	cloud.google.com/go/storage v1.18.2
	github.com/containerd/stargz-snapshotter/estargz v0.9.0 // indirect
	github.com/dustin/go-humanize v1.0.0
	github.com/google/go-containerregistry v0.6.1-0.20210922191434-34b7f00d7a60
	github.com/google/go-github/v32 v32.1.0
	github.com/google/ko v0.9.3
	github.com/imjasonh/delay v0.0.0-20210102151318-8339250e8458
	github.com/sigstore/cosign v1.3.1
	github.com/sigstore/fulcio v0.1.2-0.20210831152525-42f7422734bb
	github.com/tmc/dot v0.0.0-20180926222610-6d252d5ff882
	golang.org/x/mod v0.5.1
	golang.org/x/net v0.0.0-20211007125505-59d4e928ea9d // indirect
	golang.org/x/oauth2 v0.0.0-20211028175245-ba495a64dcb5
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	google.golang.org/api v0.60.0
	gopkg.in/yaml.v2 v2.4.0

)

replace k8s.io/client-go => k8s.io/client-go v0.22.3
