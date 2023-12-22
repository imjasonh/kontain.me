package main

import (
	"context"
	"fmt"
	"log"

	"github.com/chainguard-dev/terraform-google-prober/pkg/prober"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/validate"
	"github.com/kelseyhightower/envconfig"
)

func main() {
	var env struct {
		Ref string `env:"REF"`
	}
	if err := envconfig.Process("", &env); err != nil {
		log.Fatal(err)
	}
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
