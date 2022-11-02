package proto

//go:generate protoc --go_out=./topo --go_opt=paths=source_relative ./topo.proto
//go:generate protoc --go_out=./ceos --go_opt=paths=source_relative ./ceos.proto
//go:generate protoc --go_out=./controller --go-grpc_out=./controller --go-grpc_opt=paths=source_relative --go_opt=paths=source_relative ./controller.proto
