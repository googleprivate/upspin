package prototest

// To regenerate the protocol buffer output for this package, run
//	go generate

//go:generate protoc testserver.proto --go_out=plugins=grpc:. -I . -I $GOPATH/src
