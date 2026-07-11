package cppgen

import (
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// msgField builds a singular (optional<T>) message field.
func msgField(name, typeName string, num int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		Number:   proto.Int32(num),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		TypeName: proto.String(typeName),
	}
}

// repeatedMsgField builds a repeated (vector<T>) message field.
func repeatedMsgField(name, typeName string, num int32) *descriptorpb.FieldDescriptorProto {
	f := msgField(name, typeName, num)
	f.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	return f
}

func scalarField(name string, num int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(num),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	}
}

// buildPlugin compiles a single proto3 file with the given messages into a
// protogen.Plugin, mirroring how main.go drives the real code path.
func buildPlugin(t *testing.T, pkg string, msgs []*descriptorpb.DescriptorProto) *protogen.Plugin {
	t.Helper()
	fd := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String(pkg),
		Syntax:      proto.String("proto3"),
		MessageType: msgs,
		Options:     &descriptorpb.FileOptions{GoPackage: proto.String("cppdto/test")},
	}
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate:  []string{"test.proto"},
		ProtoFile:       []*descriptorpb.FileDescriptorProto{fd},
		CompilerVersion: &pluginpb.Version{Major: proto.Int32(3), Minor: proto.Int32(21), Patch: proto.Int32(0)},
	}
	gen, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatalf("protogen.New: %v", err)
	}
	return gen
}

func TestAnalyzeCycles(t *testing.T) {
	const pkg = "test"
	msgs := []*descriptorpb.DescriptorProto{
		// Node: repeated self-reference -> breakable, kept.
		{Name: proto.String("Node"), Field: []*descriptorpb.FieldDescriptorProto{
			repeatedMsgField("children", ".test.Node", 1),
		}},
		// A <-> B: singular mutual recursion -> unbreakable, dropped.
		{Name: proto.String("A"), Field: []*descriptorpb.FieldDescriptorProto{
			msgField("b", ".test.B", 1),
		}},
		{Name: proto.String("B"), Field: []*descriptorpb.FieldDescriptorProto{
			msgField("a", ".test.A", 1),
		}},
		// C depends on dropped A -> dropped.
		{Name: proto.String("C"), Field: []*descriptorpb.FieldDescriptorProto{
			msgField("a", ".test.A", 1),
		}},
		// Branch <-> Leaf: one edge is repeated -> breakable, both kept.
		{Name: proto.String("Branch"), Field: []*descriptorpb.FieldDescriptorProto{
			msgField("leaf", ".test.Leaf", 1),
		}},
		{Name: proto.String("Leaf"), Field: []*descriptorpb.FieldDescriptorProto{
			repeatedMsgField("branches", ".test.Branch", 1),
		}},
		// Ok: acyclic -> kept.
		{Name: proto.String("Ok"), Field: []*descriptorpb.FieldDescriptorProto{
			scalarField("x", 1),
			msgField("node", ".test.Node", 2),
		}},
	}
	gen := buildPlugin(t, pkg, msgs)

	got := analyzeCycles(gen.Files)

	want := map[string]bool{
		pkg + ".A": true,
		pkg + ".B": true,
		pkg + ".C": true,
	}
	for _, name := range []string{"Node", "A", "B", "C", "Branch", "Leaf", "Ok"} {
		full := pkg + "." + name
		if got.dropped[protoreflect.FullName(full)] != want[full] {
			t.Errorf("dropped[%s] = %v, want %v", full, got.dropped[protoreflect.FullName(full)], want[full])
		}
	}

	// C must be recorded as dependency-dropped because of A.
	if cause := got.depCause[protoreflect.FullName(pkg+".C")]; cause != protoreflect.FullName(pkg+".A") {
		t.Errorf("depCause[C] = %q, want %q", cause, pkg+".A")
	}
	// A and B form the single unbreakable group.
	if len(got.groups) != 1 || len(got.groups[0]) != 2 {
		t.Fatalf("groups = %v, want one group of size 2", got.groups)
	}
}
