package cppgen

import (
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// Options controls code generation.
type Options struct {
	// NamespaceSuffix is the leaf C++ namespace appended to the proto package
	// for generated DTO types, e.g. package "a.b" -> namespace a::b::dto.
	// This keeps DTO types from colliding with the protobuf-generated types,
	// which live in a::b.
	NamespaceSuffix string
	// BytesType is the C++ type used for proto `bytes` fields.
	BytesType string
	// GenFormatters, when set, additionally emits `*.fmt.h` / `*.fmt.cc` files
	// implementing `logfmt::to_ostream` for the generated DTO types.
	GenFormatters bool
	// LogFormatInclude is the include path used to pull in the logfmt header
	// from generated formatter files, e.g. "log_format.hpp".
	LogFormatInclude string
}

// nsPrefix returns the C++ namespace prefix for a proto package, e.g.
// "a.b" -> "::a::b", "" -> "".
func nsPrefix(pkg protoreflect.FullName) string {
	if pkg == "" {
		return ""
	}
	return "::" + strings.ReplaceAll(string(pkg), ".", "::")
}

// relName strips the package prefix from a fully qualified descriptor name.
func relName(fullName, pkg protoreflect.FullName) string {
	s := string(fullName)
	if pkg != "" {
		s = strings.TrimPrefix(s, string(pkg)+".")
	}
	return s
}

// flatName renders a (possibly nested) type name with '_' separators, matching
// the flattened DTO struct/enum names, e.g. "Outer.Inner" -> "Outer_Inner".
func flatName(fullName, pkg protoreflect.FullName) string {
	return strings.ReplaceAll(relName(fullName, pkg), ".", "_")
}

// nestedName renders a nested type name with '::' separators, matching the
// protobuf-generated C++ nested class path, e.g. "Outer.Inner" -> "Outer::Inner".
func nestedName(fullName, pkg protoreflect.FullName) string {
	return strings.ReplaceAll(relName(fullName, pkg), ".", "::")
}

// dtoFQN returns the fully qualified DTO C++ type name for a message.
func (o Options) dtoFQN(md protoreflect.MessageDescriptor) string {
	pkg := md.ParentFile().Package()
	return nsPrefix(pkg) + "::" + o.NamespaceSuffix + "::" + flatName(md.FullName(), pkg)
}

// protoFQN returns the fully qualified protobuf-generated C++ type name for a message.
func protoFQN(md protoreflect.MessageDescriptor) string {
	pkg := md.ParentFile().Package()
	return nsPrefix(pkg) + "::" + nestedName(md.FullName(), pkg)
}

// dtoEnumFQN returns the fully qualified DTO C++ enum name.
func (o Options) dtoEnumFQN(ed protoreflect.EnumDescriptor) string {
	pkg := ed.ParentFile().Package()
	return nsPrefix(pkg) + "::" + o.NamespaceSuffix + "::" + flatName(ed.FullName(), pkg)
}

// protoEnumFQN returns the fully qualified protobuf-generated C++ enum name.
func protoEnumFQN(ed protoreflect.EnumDescriptor) string {
	pkg := ed.ParentFile().Package()
	return nsPrefix(pkg) + "::" + nestedName(ed.FullName(), pkg)
}

// cppElemType returns the DTO C++ type for a single field element, ignoring any
// repeated/optional/map container wrapping.
func (o Options) cppElemType(fd protoreflect.FieldDescriptor) string {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "::std::int32_t"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "::std::int64_t"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "::std::uint32_t"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "::std::uint64_t"
	case protoreflect.FloatKind:
		return "float"
	case protoreflect.DoubleKind:
		return "double"
	case protoreflect.StringKind:
		return "::std::string"
	case protoreflect.BytesKind:
		return o.BytesType
	case protoreflect.EnumKind:
		return o.dtoEnumFQN(fd.Enum())
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return o.dtoFQN(fd.Message())
	default:
		return "/* unsupported kind */"
	}
}

// memberType returns the DTO C++ type for a struct member, applying container
// and presence wrapping. Oneof members are handled separately.
func (o Options) memberType(fd protoreflect.FieldDescriptor) string {
	switch {
	case fd.IsMap():
		return "::std::map<" + o.cppElemType(fd.MapKey()) + ", " + o.cppElemType(fd.MapValue()) + ">"
	case fd.IsList():
		return "::std::vector<" + o.cppElemType(fd) + ">"
	case fd.HasPresence():
		// proto3 `optional` scalars/enums and all singular message fields.
		return "::std::optional<" + o.cppElemType(fd) + ">"
	default:
		return o.cppElemType(fd)
	}
}

// isMessageField reports whether a field's element type is a message.
func isMessageField(fd protoreflect.FieldDescriptor) bool {
	return fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind
}

// cppKeywords is a minimal set of C++ keywords that, when used as proto field
// names, would collide with the language and are suffixed with '_'.
var cppKeywords = map[string]bool{
	"alignas": true, "alignof": true, "and": true, "asm": true, "auto": true,
	"bool": true, "break": true, "case": true, "catch": true, "char": true,
	"class": true, "const": true, "constexpr": true, "continue": true,
	"default": true, "delete": true, "do": true, "double": true, "else": true,
	"enum": true, "explicit": true, "export": true, "extern": true, "false": true,
	"float": true, "for": true, "friend": true, "goto": true, "if": true,
	"inline": true, "int": true, "long": true, "mutable": true, "namespace": true,
	"new": true, "not": true, "nullptr": true, "operator": true, "or": true,
	"private": true, "protected": true, "public": true, "register": true,
	"return": true, "short": true, "signed": true, "sizeof": true, "static": true,
	"struct": true, "switch": true, "template": true, "this": true, "throw": true,
	"true": true, "try": true, "typedef": true, "typename": true, "union": true,
	"unsigned": true, "using": true, "virtual": true, "void": true, "volatile": true,
	"wchar_t": true, "while": true, "xor": true,
}

// sanitize makes a proto identifier safe to use as a C++ identifier.
func sanitize(name string) string {
	if cppKeywords[name] {
		return name + "_"
	}
	return name
}

// upperCamel converts a snake_case name to UpperCamelCase, matching protobuf's
// oneof case-constant naming (e.g. "sub_message" -> "SubMessage").
func upperCamel(s string) string {
	var b strings.Builder
	for _, part := range strings.Split(s, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	return b.String()
}
