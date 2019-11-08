package main

import (
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
		info:     log.New(os.Stdout, "I ", log.Ldate|log.Ltime|log.Lshortfile),
		error:    log.New(os.Stderr, "E ", log.Ldate|log.Ltime|log.Lshortfile),
		squished: false,
	})
	http.Handle("/vizsquish", &server{
		info:     log.New(os.Stdout, "I ", log.Ldate|log.Ltime|log.Lshortfile),
		error:    log.New(os.Stderr, "E ", log.Ldate|log.Ltime|log.Lshortfile),
		squished: true,
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
	squished    bool
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.info.Println("handler:", r.Method, r.URL)
	if r.Method != http.MethodPost {
		http.Error(w, "must be post", http.StatusMethodNotAllowed)
		return
	}

	r.ParseForm()
	log.Println(r.Form) // TODO
	refs, err := images(r.FormValue("images"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	d, err := genDot(refs, s.squished)
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

/*
 * Apparently having a set data type is too much to ask from Go.
 */
var setEntryExists = struct{}{}

/*
 * Represents a node in the layer dependency graph
 *
 * It may contain multiple refs/layers and an aggregate size in the event of a squished graph.
 * It also marks refs clearly from refs.
 */
type node struct {
	layers []string
	size   uint64

	inEdgeSet  map[*edge]struct{}
	outEdgeSet map[*edge]struct{}
}

func (this *node) computeTotalSize() uint64 {
	s := this.size
	for e := range this.outEdgeSet {
		s += e.dst.computeTotalSize()
	}
	return s
}

func (this *node) isTop() bool {
	return len(this.inEdgeSet) == 0
}

func (this *node) getLabel() string {
	sb := strings.Builder{}
	for _, s := range this.layers {
		sb.WriteString(s)
		sb.WriteString("\n")
	}

	if this.isTop() {
		s := this.computeTotalSize()
		sb.WriteString("TotalSize: ")
		sb.WriteString(humanize.Bytes(s))

	} else {
		sb.WriteString(humanize.Bytes(this.size))
	}
	return sb.String()
}

/*
 * Represents an edge in the layer dependency graph
 *
 * Technically this class could be avoidud by having the in-out edges be pointers to the nodes
 * but having this class makes it easier to denote squishable edges.
 */
type edge struct {
	src *node
	dst *node
}

func (this *edge) isSquishable() bool {
	return len(this.src.outEdgeSet) == 1 && len(this.dst.inEdgeSet) == 1 && len(this.src.inEdgeSet) > 0
}

/*
 * Represents a layer dependency graph
 */
type graph struct {
	layer2node map[string]*node

	nodeSet map[*node]struct{}
	edgeSet map[*edge]struct{}
}

func NewGraph() *graph {
	g := new(graph)
	g.layer2node = make(map[string]*node)
	g.nodeSet = make(map[*node]struct{})
	g.edgeSet = make(map[*edge]struct{})
	return g
}

func (this *graph) findSquishables() []*edge {
	squishable := []*edge{}
	for e := range this.edgeSet {
		if e.isSquishable() {
			squishable = append(squishable, e)
		}
	}
	return squishable
}

func (this *graph) connectNodes(from *node, to *node) {
	for e := range from.outEdgeSet {
		if e.dst == to {
			return
		}
	}

	e := new(edge)
	e.src = from
	e.dst = to
	from.outEdgeSet[e] = setEntryExists
	to.inEdgeSet[e] = setEntryExists

	this.edgeSet[e] = setEntryExists
}

func (this *graph) addOrGetNode(size uint64, layer string) *node {
	if n, ok := this.layer2node[layer]; ok {
		return n
	}
	n := new(node)
	n.layers = []string{layer}
	n.size = size
	n.inEdgeSet = make(map[*edge]struct{})
	n.outEdgeSet = make(map[*edge]struct{})
	this.layer2node[layer] = n
	this.nodeSet[n] = setEntryExists
	return n
}

func (this *graph) collapseEdge(e *edge) {
	keptNode := e.src
	deldNode := e.dst

	keptNode.outEdgeSet = deldNode.outEdgeSet
	keptNode.size += deldNode.size
	keptNode.layers = append(keptNode.layers, deldNode.layers...)

	for oe := range keptNode.outEdgeSet {
		oe.src = keptNode
	}

	delete(this.nodeSet, deldNode)
	delete(this.edgeSet, e)
}

func (this *graph) squishGraph() {
	changed := true
	for changed {
		changed = false
		for _, e := range this.findSquishables() {
			this.collapseEdge(e)
			changed = true
		}
	}
}

func shortSha(layer v1.Descriptor) string {
	return layer.Digest.String()[7:19]
}

func (this *graph) addSubgraphFromRefs(refs []name.Reference) error {
	rootNode := this.addOrGetNode(0, "scratch")

	for i, ref := range refs {
		log.Println("Processing ref: ", ref.String(), " progress: ", float32(i)/float32(len(refs))*100.0, "%")

		layers, err := getLayers(ref)
		if err != nil {
			return fmt.Errorf("Failed to get layers for %q, ignoring: %v\n", ref, err)
		}

		refNode := this.addOrGetNode(0, ref.String())

		for i, current := range layers {
			nCurrent := this.addOrGetNode(uint64(current.Size), shortSha(current))

			if i == 0 {
				this.connectNodes(nCurrent, rootNode)
			}

			if i == len(layers)-1 {
				this.connectNodes(refNode, nCurrent)
			} else {
				next := layers[i+1]
				nNext := this.addOrGetNode(uint64(next.Size), shortSha(next))
				this.connectNodes(nNext, nCurrent)
			}

		}

	}
	return nil
}

func (this *graph) renderToDot() (g *dot.Graph, _ error) {
	g = dot.NewGraph("images")
	g.SetType(dot.DIGRAPH)

	node2dot := map[*node]*dot.Node{}

	log.Println("Graph nodes: ", len(this.nodeSet))
	log.Println("Graph edges: ", len(this.edgeSet))

	for n := range this.nodeSet {
		gn := dot.NewNode(n.getLabel())

		if n.isTop() {
			gn.Set("shape", "box")
			gn.Set("style", "filled")
			gn.Set("color", "cornflowerblue")
		} else if n == this.layer2node["scratch"] {
			gn.Set("shape", "octagon")
			gn.Set("style", "filled")
			gn.Set("color", "coral")
		}

		g.AddNode(gn)
		node2dot[n] = gn
	}

	for e := range this.edgeSet {
		ge := dot.NewEdge(node2dot[e.src], node2dot[e.dst])
		if e.src.isTop() {
			ge.Set("style", "dotted")
		}

		g.AddEdge(ge)
	}
	return g, nil
}

func genDot(refs []name.Reference, squish bool) (string, error) {
	gr := NewGraph()
	gr.addSubgraphFromRefs(refs)
	if squish {
		gr.squishGraph()
	}
	if d, error := gr.renderToDot(); error == nil {
		log.Println("Output:\n", d.String())
		return d.String(), nil
	} else {
		return "", error
	}
}

func getLayers(ref name.Reference) ([]v1.Descriptor, error) {
	i, err := remote.Image(ref)
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
	cmd := exec.Command("dot", "-Tsvg")
	cmd.Stdin = in
	cmd.Stdout = out
	return cmd.Run()
}
