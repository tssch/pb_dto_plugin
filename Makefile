BIN := bin/protoc-gen-cppdto
GEN := gen

.PHONY: build gen test clean

build:
	GOTOOLCHAIN=local go build -o $(BIN) .

# Generate DTO + conversion sources (and the protobuf C++ sources) for the
# example proto. --plugin points protoc at our binary explicitly so it does not
# need to be on PATH.
gen: build
	mkdir -p $(GEN)
	protoc \
		--plugin=protoc-gen-cppdto=$(BIN) \
		--cppdto_out=$(GEN) \
		--cppdto_opt=gen_formatters=true \
		--cpp_out=$(GEN) \
		--proto_path=example \
		bank.proto

# Compile and run the C++ round-trip test. Requires protobuf C++ dev libraries.
test: gen
	g++ -std=c++17 -I$(GEN) -Iinclude $$(pkg-config --cflags protobuf) \
		example/roundtrip_test.cc $(GEN)/bank.conv.cc $(GEN)/bank.fmt.cc $(GEN)/bank.pb.cc \
		$$(pkg-config --libs protobuf) -lpthread -o bin/roundtrip
	./bin/roundtrip

clean:
	rm -rf $(BIN) $(GEN)
