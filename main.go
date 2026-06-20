// Command protoc-gen-cppdto is a protoc plugin that generates C++ DTO structs
// matching proto3 messages, plus conversion functions to and from the
// protobuf-generated C++ types.
//
// Mapping summary:
//
//	repeated T          -> std::vector<T>
//	map<K, V>           -> std::map<K, V>
//	optional scalar     -> std::optional<T>   (proto3 `optional`)
//	singular message    -> std::optional<T>   (message fields always have presence)
//	singular scalar     -> T                   (implicit presence)
//	oneof               -> std::variant<std::monostate, ...>
//	enum                -> enum class : int32_t
//
// The generated `*.dto.h` header depends only on the C++ standard library so
// consumers of the DTOs do not need to link against protobuf. The conversion
// functions live in `*.conv.h` / `*.conv.cc`, which include both the DTO header
// and the protobuf-generated `*.pb.h`.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/tssch/pb_dto_plugin/internal/cppgen"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "protoc-gen-cppdto:", err)
		os.Exit(1)
	}
}

func run() error {
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	var req pluginpb.CodeGeneratorRequest
	if err := proto.Unmarshal(in, &req); err != nil {
		return err
	}

	// protogen requires a Go import path for every file, even though we emit
	// C++. Synthesize one per proto file via the standard "M" mappings so the
	// user does not have to add a go_package option to their .proto files.
	injectGoPackages(&req)

	var flags flag.FlagSet
	nsSuffix := flags.String("dto_namespace_suffix", "dto",
		"leaf C++ namespace appended to the proto package for generated DTOs")
	bytesType := flags.String("bytes_type", "::std::string",
		"C++ type used for proto `bytes` fields")

	gen, err := protogen.Options{ParamFunc: flags.Set}.New(&req)
	if err != nil {
		return err
	}
	// Tell protoc we understand proto3 `optional`, so it reports presence.
	gen.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

	if err := cppgen.Run(gen, cppgen.Options{
		NamespaceSuffix: *nsSuffix,
		BytesType:       *bytesType,
	}); err != nil {
		gen.Error(err)
	}

	resp := gen.Response()
	out, err := proto.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	return err
}

// injectGoPackages prepends an "M<file>=<import path>" mapping for every proto
// file to the request parameter, so protogen can resolve a Go import path. The
// values are never used for C++ output.
func injectGoPackages(req *pluginpb.CodeGeneratorRequest) {
	var maps []string
	for _, f := range req.GetProtoFile() {
		name := f.GetName()
		importPath := "cppdto/" + strings.TrimSuffix(name, ".proto")
		maps = append(maps, "M"+name+"="+importPath)
	}
	param := strings.Join(maps, ",")
	if existing := req.GetParameter(); existing != "" {
		param = existing + "," + param
	}
	req.Parameter = proto.String(param)
}
