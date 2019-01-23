workflow "New workflow" {
  on = "push"
  resolves = ["gcr.io/go-containerregistry/crane"]
}

action "gcr.io/go-containerregistry/crane" {
  uses = "gcr.io/go-containerregistry/crane"
  args = "manifest gcr.io/go-containerregistry/crane"
}
