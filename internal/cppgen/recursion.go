package cppgen

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// checkAcyclic rejects message graphs that (transitively) contain themselves.
//
// Recursive message types cannot be expressed as the by-value DTO structs this
// plugin generates: std::optional, std::variant and std::map all require a
// complete value type, so a cycle would fail to compile (and would describe an
// infinite structure anyway). We surface a clear error naming the cycle instead.
func checkAcyclic(files []*protogen.File) error {
	const (
		white = iota
		gray
		black
	)
	state := map[protoreflect.FullName]int{}
	var stack []protoreflect.MessageDescriptor

	var visit func(md protoreflect.MessageDescriptor) error
	visit = func(md protoreflect.MessageDescriptor) error {
		switch state[md.FullName()] {
		case black:
			return nil
		case gray:
			return cycleError(stack, md)
		}
		state[md.FullName()] = gray
		stack = append(stack, md)

		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			var target protoreflect.MessageDescriptor
			switch {
			case fd.IsMap():
				if mv := fd.MapValue(); isMessageField(mv) {
					target = mv.Message()
				}
			case isMessageField(fd):
				target = fd.Message()
			}
			if target != nil {
				if err := visit(target); err != nil {
					return err
				}
			}
		}

		stack = stack[:len(stack)-1]
		state[md.FullName()] = black
		return nil
	}

	for _, f := range files {
		if !f.Generate {
			continue
		}
		for _, md := range allMessageDescs(f.Desc.Messages()) {
			if err := visit(md); err != nil {
				return err
			}
		}
	}
	return nil
}

// cycleError renders the detected cycle as a readable error.
func cycleError(stack []protoreflect.MessageDescriptor, repeat protoreflect.MessageDescriptor) error {
	start := 0
	for i, m := range stack {
		if m.FullName() == repeat.FullName() {
			start = i
			break
		}
	}
	var names []string
	for _, m := range stack[start:] {
		names = append(names, string(m.FullName()))
	}
	names = append(names, string(repeat.FullName()))
	return fmt.Errorf("recursive message types are not supported (cycle: %s)", strings.Join(names, " -> "))
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
