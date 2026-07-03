# protoc-gen-cppdto

A `protoc` plugin (written in Go) that generates plain C++ DTO structs matching
proto3 messages, plus conversion functions to and from the protobuf-generated
C++ types.

The generated DTO header depends **only on the C++ standard library**, so code
that uses the DTOs does not have to include or link against protobuf. The
conversion layer is generated separately and is the only part that touches
`*.pb.h`.

## Status

Scaffold. Supports **proto3 only**. Well-known types are **not** supported yet.
Recursive (self-referential) message graphs are rejected with a clear error,
because the by-value DTO representation cannot express them.

## Type mapping

| proto3                       | C++ DTO                                   |
| ---------------------------- | ----------------------------------------- |
| `repeated T`                 | `std::vector<T>`                          |
| `map<K, V>`                  | `std::map<K, V>`                          |
| `optional` scalar/enum       | `std::optional<T>`                        |
| singular message             | `std::optional<T>` (messages have presence) |
| singular scalar/enum         | `T` (implicit presence, zero-initialized) |
| `oneof`                      | `std::variant<std::monostate, ...>`       |
| `enum`                       | `enum class : int32_t`                    |
| `string`                     | `std::string`                             |
| `bytes`                      | `std::string` (configurable)              |
| nested message / enum        | flattened: `Outer.Inner` → `Outer_Inner`  |

Scalar integers map to fixed-width `<cstdint>` types. Every struct member is
value-initialized (`{}`), so a default-constructed DTO matches proto3 zero
defaults.

## Generated files

For `foo.proto` the plugin emits:

- `foo.dto.h` — pure structs/enums, standard-library includes only.
- `foo.conv.h` — declarations of `from_proto` / `to_proto`.
- `foo.conv.cc` — definitions; includes both `foo.dto.h` and `foo.pb.h`.

With `--cppdto_opt=gen_formatters=true` it additionally emits:

- `foo.fmt.h` / `foo.fmt.cc` — `logfmt::to_ostream` overloads for every DTO
  message and enum, for structured (indented, recursive) logging.

Conversions are free functions in the DTO namespace, found via ADL:

```cpp
void from_proto(const pb::Foo& src, dto::Foo& dst);
void to_proto(const dto::Foo& src, pb::Foo* dst);
```

DTOs live in the proto package namespace with a `::dto` suffix
(e.g. package `a.b` → `a::b::dto`) so they never collide with the
protobuf-generated types in `a::b`.

## Build & try it

```sh
make build      # builds bin/protoc-gen-cppdto
make gen        # generates gen/ (DTO + conv + pb) from example/bank.proto
make test       # compiles & runs the C++ round-trip test
```

Manual invocation:

```sh
protoc \
  --plugin=protoc-gen-cppdto=./bin/protoc-gen-cppdto \
  --cppdto_out=OUT_DIR \
  --proto_path=DIR \
  your.proto
```

### Round-trip test

`make test` regenerates, then compiles `example/roundtrip_test.cc` together with
the generated conversion and protobuf sources and runs it. It builds a proto,
converts proto → DTO → proto, and asserts the re-serialized bytes match.

## Options

Passed via `--cppdto_opt=key=value`:

| option                   | default        | meaning                                   |
| ------------------------ | -------------- | ----------------------------------------- |
| `dto_namespace_suffix`   | `dto`          | leaf namespace appended to the proto package |
| `bytes_type`             | `::std::string`| C++ type used for `bytes` fields          |
| `gen_formatters`         | `false`        | also emit `*.fmt.h/.cc` with `logfmt::to_ostream` |
| `log_format_include`     | `log_format.hpp` | include path for the logfmt header in `*.fmt.h` |

### Formatters (`gen_formatters=true`)

Emits free `to_ostream(logfmt::context&, const T&)` overloads in the DTO
namespace, found via ADL, so any DTO (including nested messages, `optional`,
`oneof`, maps, and vectors) can be rendered:

```cpp
#include "foo.fmt.h"
std::cout << logfmt::to_string(my_dto);           // indented, multi-line
std::string s = logfmt::to_string_one_line(my_dto);
```

Enums render as their value name, a `oneof` as `field.name: value` (labelled by
active alternative), and an empty `optional`/unset `oneof` as `null`/`(unset)`.
The formatter files depend on `include/log_format.hpp` (point the compiler at it
with `-Iinclude`, or override the include path via `log_format_include`); the
`*.dto.h` / `*.conv.*` files remain protobuf- and logfmt-free.

## Not yet handled (intentional)

- proto2 syntax (rejected with an error).
- Well-known types (`Timestamp`, `Duration`, wrappers, `Any`, `Struct`, ...).
- Recursive message graphs (rejected — would need `unique_ptr`/indirection).
- Extensions, unknown-field preservation, services.
- Generated `operator==`, `std::hash` (could be added). Structured logging via
  `to_ostream` is available behind `gen_formatters` (see above).
