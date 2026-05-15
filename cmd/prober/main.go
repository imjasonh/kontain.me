package main

import (
	"context"
	"fmt"
	"log"

	"github.com/chainguard-dev/terraform-infra-common/pkg/prober"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/validate"
	"github.com/sethvargo/go-envconfig"
)

var env = envconfig.MustProcess(context.Background(), &struct {
	Ref string `env:"REF"`
}{})

func main() {
	ref, err := name.ParseReference(env.Ref)
	if err != nil {
		log.Fatal(err)
	}

	prober.Go(context.Background(), prober.Func(func(ctx context.Context) error {
		log.Printf("probing %s", ref)
		img, err := remote.Image(ref)
		if err != nil {
			return fmt.Errorf("remote.Image: %w", err)
		}
		return validate.Image(img)
	}))
}
