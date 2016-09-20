// Code generated by protoc-gen-go.
// source: testserver.proto
// DO NOT EDIT!

/*
Package prototest is a generated protocol buffer package.

It is generated from these files:
	testserver.proto

It has these top-level messages:
	DoATrumpRequest
	DoATrumpResponse
*/
package prototest

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"
import proto1 "upspin.io/upspin/proto"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

type DoATrumpRequest struct {
	PeopleDemand string `protobuf:"bytes,1,opt,name=people_demand,json=peopleDemand" json:"people_demand,omitempty"`
}

func (m *DoATrumpRequest) Reset()                    { *m = DoATrumpRequest{} }
func (m *DoATrumpRequest) String() string            { return proto.CompactTextString(m) }
func (*DoATrumpRequest) ProtoMessage()               {}
func (*DoATrumpRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

type DoATrumpResponse struct {
	TrumpResponse string `protobuf:"bytes,1,opt,name=trump_response,json=trumpResponse" json:"trump_response,omitempty"`
}

func (m *DoATrumpResponse) Reset()                    { *m = DoATrumpResponse{} }
func (m *DoATrumpResponse) String() string            { return proto.CompactTextString(m) }
func (*DoATrumpResponse) ProtoMessage()               {}
func (*DoATrumpResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

func init() {
	proto.RegisterType((*DoATrumpRequest)(nil), "prototest.DoATrumpRequest")
	proto.RegisterType((*DoATrumpResponse)(nil), "prototest.DoATrumpResponse")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion3

// Client API for TestService service

type TestServiceClient interface {
	Ping(ctx context.Context, in *proto1.PingRequest, opts ...grpc.CallOption) (*proto1.PingResponse, error)
	DoATrump(ctx context.Context, in *DoATrumpRequest, opts ...grpc.CallOption) (*DoATrumpResponse, error)
}

type testServiceClient struct {
	cc *grpc.ClientConn
}

func NewTestServiceClient(cc *grpc.ClientConn) TestServiceClient {
	return &testServiceClient{cc}
}

func (c *testServiceClient) Ping(ctx context.Context, in *proto1.PingRequest, opts ...grpc.CallOption) (*proto1.PingResponse, error) {
	out := new(proto1.PingResponse)
	err := grpc.Invoke(ctx, "/prototest.TestService/Ping", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *testServiceClient) DoATrump(ctx context.Context, in *DoATrumpRequest, opts ...grpc.CallOption) (*DoATrumpResponse, error) {
	out := new(DoATrumpResponse)
	err := grpc.Invoke(ctx, "/prototest.TestService/DoATrump", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Server API for TestService service

type TestServiceServer interface {
	Ping(context.Context, *proto1.PingRequest) (*proto1.PingResponse, error)
	DoATrump(context.Context, *DoATrumpRequest) (*DoATrumpResponse, error)
}

func RegisterTestServiceServer(s *grpc.Server, srv TestServiceServer) {
	s.RegisterService(&_TestService_serviceDesc, srv)
}

func _TestService_Ping_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(proto1.PingRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TestServiceServer).Ping(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/prototest.TestService/Ping",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TestServiceServer).Ping(ctx, req.(*proto1.PingRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TestService_DoATrump_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DoATrumpRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TestServiceServer).DoATrump(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/prototest.TestService/DoATrump",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TestServiceServer).DoATrump(ctx, req.(*DoATrumpRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _TestService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "prototest.TestService",
	HandlerType: (*TestServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Ping",
			Handler:    _TestService_Ping_Handler,
		},
		{
			MethodName: "DoATrump",
			Handler:    _TestService_DoATrump_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: fileDescriptor0,
}

func init() { proto.RegisterFile("testserver.proto", fileDescriptor0) }

var fileDescriptor0 = []byte{
	// 213 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0xe2, 0x12, 0x28, 0x49, 0x2d, 0x2e,
	0x29, 0x4e, 0x2d, 0x2a, 0x4b, 0x2d, 0xd2, 0x2b, 0x28, 0xca, 0x2f, 0xc9, 0x17, 0xe2, 0x04, 0x53,
	0x20, 0x61, 0x29, 0xe5, 0xd2, 0x82, 0xe2, 0x82, 0xcc, 0x3c, 0xbd, 0xcc, 0x7c, 0x7d, 0x08, 0x4b,
	0x1f, 0x2c, 0x07, 0xe5, 0x40, 0xd4, 0x2b, 0x99, 0x71, 0xf1, 0xbb, 0xe4, 0x3b, 0x86, 0x14, 0x95,
	0xe6, 0x16, 0x04, 0xa5, 0x16, 0x96, 0xa6, 0x16, 0x97, 0x08, 0x29, 0x73, 0xf1, 0x16, 0xa4, 0xe6,
	0x17, 0xe4, 0xa4, 0xc6, 0xa7, 0xa4, 0xe6, 0x26, 0xe6, 0xa5, 0x48, 0x30, 0x2a, 0x30, 0x6a, 0x70,
	0x06, 0xf1, 0x40, 0x04, 0x5d, 0xc0, 0x62, 0x4a, 0x96, 0x5c, 0x02, 0x08, 0x7d, 0xc5, 0x05, 0xf9,
	0x79, 0xc5, 0xa9, 0x42, 0xaa, 0x5c, 0x7c, 0x25, 0x20, 0x81, 0xf8, 0x22, 0xa8, 0x08, 0x54, 0x27,
	0x6f, 0x09, 0xb2, 0x32, 0xa3, 0x76, 0x46, 0x2e, 0xee, 0x90, 0xd4, 0xe2, 0x92, 0xe0, 0xd4, 0xa2,
	0xb2, 0xcc, 0xe4, 0x54, 0x21, 0x43, 0x2e, 0x96, 0x80, 0xcc, 0xbc, 0x74, 0x21, 0x21, 0x88, 0x93,
	0xf4, 0x40, 0x1c, 0xa8, 0x5b, 0xa4, 0x84, 0x51, 0xc4, 0x20, 0x06, 0x28, 0x31, 0x08, 0xb9, 0x72,
	0x71, 0xc0, 0x6c, 0x17, 0x92, 0xd2, 0x83, 0x7b, 0x59, 0x0f, 0xcd, 0x2b, 0x52, 0xd2, 0x58, 0xe5,
	0x60, 0xc6, 0x24, 0xb1, 0x81, 0x65, 0x8d, 0x01, 0x01, 0x00, 0x00, 0xff, 0xff, 0xd1, 0xe2, 0xa6,
	0xb8, 0x47, 0x01, 0x00, 0x00,
}
