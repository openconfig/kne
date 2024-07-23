package proto

//go:generate protoc --go_out=./alpine --go_opt=paths=source_relative ./alpine.proto
//go:generate protoc --go_out=./topo --go_opt=paths=source_relative ./topo.proto
//go:generate protoc --go_out=./ceos --go_opt=paths=source_relative ./ceos.proto
//go:generate protoc --go_out=./controller --go-grpc_out=./controller --go-grpc_opt=paths=source_relative --go_opt=paths=source_relative ./controller.proto
//go:generate protoc --go_out=./wire --go-grpc_out=./wire --go-grpc_opt=paths=source_relative --go_opt=paths=source_relative ./wire.proto
//go:generate protoc --go_out=./forward --go-grpc_out=./forward --go-grpc_opt=paths=source_relative --go_opt=paths=source_relative ./forward.proto
//go:generate protoc --go_out=./event --go-grpc_out=./event --go-grpc_opt=paths=source_relative --go_opt=paths=source_relative ./event.proto
