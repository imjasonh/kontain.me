# Deploy

```
export KO_DOCKER_REPO=gcr.io/kontaindotme
```

Build all images

```
ls cmd/ | xargs -I{} ko publish -P -t latest ./cmd/{}
```

Deploy services

```
terraform apply
```

