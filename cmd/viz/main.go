package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	humanize "github.com/dustin/go-humanize"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/tmc/dot"
)

func main() {
	http.Handle("/", http.FileServer(http.Dir("/var/run/ko")))
	http.Handle("/viz", &server{
		info:  log.New(os.Stdout, "I ", log.Ldate|log.Ltime|log.Lshortfile),
		error: log.New(os.Stderr, "E ", log.Ldate|log.Ltime|log.Lshortfile),
	})
	log.Println("Starting...")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}
	log.Printf("Listening on port %s", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

type server struct {
	info, error *log.Logger
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.info.Println("handler:", r.Method, r.URL)
	if r.Method != http.MethodPost {
		http.Error(w, "must be post", http.StatusMethodNotAllowed)
		return
	}

	r.ParseForm()
	refs, err := images(r.FormValue("images"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	d, err := genDot(ctx, refs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := graphviz(strings.NewReader(d), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func images(s string) ([]name.Reference, error) {
	log.Printf("in: %q", s) // TODO

	lines := strings.Split(s, "\n")
	var refs []name.Reference
	uniq := map[string]bool{} // dedupe
	for i, l := range lines {
		l := strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		ref, err := name.ParseReference(l)
		if err != nil {
			return nil, fmt.Errorf("line %d (%q): %v", i, l, err)
		}
		if !uniq[ref.String()] {
			uniq[ref.String()] = true
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

func genDot(ctx context.Context, refs []name.Reference) (string, error) {
	g := dot.NewGraph("images")
	g.SetType(dot.DIGRAPH)

	scratch := dot.NewNode("scratch")
	scratch.Set("shape", "octagon")
	scratch.Set("style", "filled")
	scratch.Set("color", "coral")
	g.AddNode(scratch)

	edges := map[string]bool{}
	for _, ref := range refs {
		layers, err := getLayers(ctx, ref)
		if err != nil {
			return "", fmt.Errorf("Failed to get layers for %q, ignoring: %v\n", ref, err)
		}
		var totalSize uint64
		for i, this := range layers {
			totalSize += uint64(this.Size)
			if i == len(layers)-1 {
				continue
			}
			next := layers[i+1]
			k := short(this) + short(next)
			if !edges[k] {
				edges[k] = true

				src := dot.NewNode(short(next))
				dst := dot.NewNode(short(this))
				e := dot.NewEdge(src, dst)

				g.AddNode(src)
				g.AddNode(dst)
				g.AddEdge(e)
			}
		}
		bottom := short(layers[0])
		k := "scratch" + bottom
		if !edges[k] {
			n := dot.NewNode(bottom)
			e := dot.NewEdge(n, scratch)
			g.AddNode(n)
			g.AddEdge(e)
			edges[k] = true
		}
		lbl := dot.NewNode(fmt.Sprintf("%s\n%s", ref.String(), humanize.Bytes(totalSize)))
		lbl.Set("shape", "box")
		lbl.Set("style", "filled")
		lbl.Set("color", "cornflowerblue")
		if strings.Contains(ref.Context().String(), "gcr.io") {
			lbl.Set("URL", "https://"+ref.String())
		}
		g.AddNode(lbl)

		top := layers[len(layers)-1]
		e := dot.NewEdge(lbl, dot.NewNode(short(top))) // already added
		e.Set("style", "dotted")
		g.AddEdge(e)
	}
	return g.String(), nil
}

func short(layer v1.Descriptor) string {
	return fmt.Sprintf("%s\n%s", layer.Digest.String()[7:19], humanize.Bytes(uint64(layer.Size)))
}

func getLayers(ctx context.Context, ref name.Reference) ([]v1.Descriptor, error) {
	i, err := remote.Image(ref, remote.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("remote.Image: %v", err)
	}

	m, err := i.Manifest()
	if err != nil {
		return nil, fmt.Errorf("image.Manifest: %v", err)
	}
	return m.Layers, nil
}

func graphviz(in io.Reader, out io.Writer) error {
	cmd := exec.Command("/bin/dot", "-Tsvg")
	cmd.Stdin = in
	cmd.Stdout = out
	return cmd.Run()
}
