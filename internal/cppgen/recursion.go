package cppgen

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Recursive by-value DTOs cannot always be expressed as the C++ structs this
// plugin generates: std::optional, std::variant and std::map all require a
// *complete* value type, so a cycle running only through those would fail to
// compile (and describes an infinite structure anyway).
//
// std::vector, however, may hold an *incomplete* type as a struct member (this
// is guaranteed since C++17), so a `repeated` message field can break a cycle
// as long as we forward-declare the struct. We therefore split dependency edges
// into two kinds:
//
//   - hard edge: singular message (std::optional<T>), map value message
//     (std::map<K,T>), oneof message alternative (std::variant<...>), and a
//     `repeated` message whose target lives in a *different* file (the
//     #include model cannot supply an incomplete type across a mutual-include
//     boundary, so we play it safe). Hard edges require a complete type.
//   - soft edge: a `repeated` message whose target is in the *same* file
//     (std::vector<T> with a forward declaration). Soft edges break cycles.
//
// A message is unrepresentable iff it lies on a cycle of hard edges, or it
// (transitively, through any edge) depends on such a message. Those messages
// are dropped from the output; everything else is generated.

// cycleAnalysis records which messages must be dropped and why.
type cycleAnalysis struct {
	// dropped is the set of message full names excluded from generation.
	dropped map[protoreflect.FullName]bool
	// groups holds each unbreakable hard-edge cycle (SCC members), for warnings.
	groups [][]protoreflect.FullName
	// depCause maps a dependency-dropped message to a dropped message it
	// references (the reason it too had to be dropped), for warnings.
	depCause map[protoreflect.FullName]protoreflect.FullName
}

// msgNode holds the outgoing dependency edges of a single message.
type msgNode struct {
	// hard are targets that must be complete types.
	hard []protoreflect.FullName
	// all are every referenced message (hard and soft), for poison propagation.
	all []protoreflect.FullName
}

// analyzeCycles builds the message dependency graph, finds messages that lie on
// (or depend on) an unbreakable hard-edge cycle, and returns them as the drop
// set together with reporting information.
func analyzeCycles(files []*protogen.File) cycleAnalysis {
	nodes := buildGraph(files)

	hardCyclic, groups := hardCyclicSet(nodes)

	dropped := map[protoreflect.FullName]bool{}
	for n := range hardCyclic {
		dropped[n] = true
	}
	// Poison propagation: any message referencing a dropped message (through any
	// edge, including std::vector) cannot compile, so it is dropped too. Iterate
	// to a fixed point over all edges.
	depCause := map[protoreflect.FullName]protoreflect.FullName{}
	for changed := true; changed; {
		changed = false
		for name, node := range nodes {
			if dropped[name] {
				continue
			}
			for _, target := range node.all {
				if dropped[target] {
					dropped[name] = true
					depCause[name] = target
					changed = true
					break
				}
			}
		}
	}

	return cycleAnalysis{dropped: dropped, groups: groups, depCause: depCause}
}

// buildGraph enumerates every real message in every file and records its hard
// and soft dependency edges.
func buildGraph(files []*protogen.File) map[protoreflect.FullName]msgNode {
	nodes := map[protoreflect.FullName]msgNode{}
	for _, f := range files {
		for _, md := range allMessageDescs(f.Desc.Messages()) {
			nodes[md.FullName()] = messageEdges(md)
		}
	}
	return nodes
}

// messageEdges classifies each message-typed field of md into a hard or soft
// dependency edge.
func messageEdges(md protoreflect.MessageDescriptor) msgNode {
	var node msgNode
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		var target protoreflect.MessageDescriptor
		soft := false
		switch {
		case fd.IsMap():
			if mv := fd.MapValue(); isMessageField(mv) {
				target = mv.Message() // std::map value -> hard
			}
		case isMessageField(fd) && fd.IsList():
			target = fd.Message()
			// std::vector may hold an incomplete type, but only same-file targets
			// can be forward-declared; cross-file targets stay hard.
			soft = target.ParentFile().Path() == md.ParentFile().Path()
		case isMessageField(fd):
			target = fd.Message() // singular message -> std::optional -> hard
		}
		if target == nil {
			continue
		}
		node.all = append(node.all, target.FullName())
		if !soft {
			node.hard = append(node.hard, target.FullName())
		}
	}
	return node
}

// hardCyclicSet returns every message that lies on a cycle formed by hard edges
// (an SCC of size >= 2, or a hard self-edge), plus the member list of each such
// SCC for reporting. Uses Tarjan's strongly-connected-components algorithm.
func hardCyclicSet(nodes map[protoreflect.FullName]msgNode) (map[protoreflect.FullName]bool, [][]protoreflect.FullName) {
	type tarjanState struct {
		index, lowlink int
		onStack        bool
	}
	st := map[protoreflect.FullName]*tarjanState{}
	var stack []protoreflect.FullName
	counter := 0
	cyclic := map[protoreflect.FullName]bool{}
	var groups [][]protoreflect.FullName

	var selfLoop func(name protoreflect.FullName) bool
	selfLoop = func(name protoreflect.FullName) bool {
		for _, t := range nodes[name].hard {
			if t == name {
				return true
			}
		}
		return false
	}

	// Iterate over a sorted node list so results are deterministic.
	names := sortedNames(nodes)

	var strongconnect func(v protoreflect.FullName)
	strongconnect = func(v protoreflect.FullName) {
		s := &tarjanState{index: counter, lowlink: counter, onStack: true}
		st[v] = s
		counter++
		stack = append(stack, v)

		for _, w := range nodes[v].hard {
			ws, seen := st[w]
			switch {
			case !seen:
				strongconnect(w)
				if st[w].lowlink < s.lowlink {
					s.lowlink = st[w].lowlink
				}
			case ws.onStack:
				if ws.index < s.lowlink {
					s.lowlink = ws.index
				}
			}
		}

		if s.lowlink != s.index {
			return
		}
		// v is the root of an SCC; pop it.
		var scc []protoreflect.FullName
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			st[w].onStack = false
			scc = append(scc, w)
			if w == v {
				break
			}
		}
		if len(scc) > 1 || selfLoop(scc[0]) {
			for _, n := range scc {
				cyclic[n] = true
			}
			sort.Slice(scc, func(i, j int) bool { return scc[i] < scc[j] })
			groups = append(groups, scc)
		}
	}

	for _, v := range names {
		if _, seen := st[v]; !seen {
			strongconnect(v)
		}
	}
	return cyclic, groups
}

func sortedNames(nodes map[protoreflect.FullName]msgNode) []protoreflect.FullName {
	names := make([]protoreflect.FullName, 0, len(nodes))
	for n := range nodes {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}

// warnDropped reports the dropped messages to stderr. protoc forwards a
// plugin's stderr to the user, so cycles surface as warnings without failing
// the invocation.
func warnDropped(a cycleAnalysis) {
	for _, g := range a.groups {
		names := make([]string, len(g))
		for i, n := range g {
			names[i] = string(n)
		}
		fmt.Fprintf(os.Stderr,
			"protoc-gen-cppdto: warning: unbreakable message cycle (no repeated field), dropping: %s\n",
			strings.Join(names, ", "))
	}
	// Report dependency-dropped messages in deterministic order.
	deps := make([]protoreflect.FullName, 0, len(a.depCause))
	for n := range a.depCause {
		deps = append(deps, n)
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i] < deps[j] })
	for _, n := range deps {
		fmt.Fprintf(os.Stderr,
			"protoc-gen-cppdto: warning: dropping %s (depends on dropped %s)\n",
			n, a.depCause[n])
	}
}

// allMessageDescs returns every real (non-map-entry) message descriptor in the
// list, including nested ones.
func allMessageDescs(list protoreflect.MessageDescriptors) []protoreflect.MessageDescriptor {
	var out []protoreflect.MessageDescriptor
	for i := 0; i < list.Len(); i++ {
		md := list.Get(i)
		if md.IsMapEntry() {
			continue
		}
		out = append(out, md)
		out = append(out, allMessageDescs(md.Messages())...)
	}
	return out
}
