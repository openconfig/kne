package alpine

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type AlpineDataplaneConfig struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Name string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"` // Name of the container in the Alpine Pod.
	Image   string   `protobuf:"bytes,2,opt,name=image,proto3" json:"image,omitempty"`     // Docker image to use for this container.
	Command []string `protobuf:"bytes,3,rep,name=command,proto3" json:"command,omitempty"` // Command to pass into the container.
	Args    []string `protobuf:"bytes,4,rep,name=args,proto3" json:"args,omitempty"`       // Command args to pass into the container.
}


func (x *AlpineDataplaneConfig) Reset() {
	*x = AlpineDataplaneConfig{}
	if protoimpl.UnsafeEnabled {
		mi := &file_alpine_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *AlpineDataplaneConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AlpineDataplaneConfig) ProtoMessage() {}

func (x *AlpineDataplaneConfig) ProtoReflect() protoreflect.Message {
	mi := &file_alpine_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

var File_alpine_proto protoreflect.FileDescriptor

var file_alpine_proto_rawDesc = []byte{
	0x0a, 0x0a, 0x63, 0x65, 0x6f, 0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x04, 0x63, 0x65,
	0x6f, 0x73, 0x22, 0xd0, 0x01, 0x0a, 0x0d, 0x43, 0x45, 0x6f, 0x73, 0x4c, 0x61, 0x62, 0x43, 0x6f,
	0x6e, 0x66, 0x69, 0x67, 0x12, 0x53, 0x0a, 0x10, 0x74, 0x6f, 0x67, 0x67, 0x6c, 0x65, 0x5f, 0x6f,
	0x76, 0x65, 0x72, 0x72, 0x69, 0x64, 0x65, 0x73, 0x18, 0x01, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x28,
	0x2e, 0x63, 0x65, 0x6f, 0x73, 0x2e, 0x43, 0x45, 0x6f, 0x73, 0x4c, 0x61, 0x62, 0x43, 0x6f, 0x6e,
	0x66, 0x69, 0x67, 0x2e, 0x54, 0x6f, 0x67, 0x67, 0x6c, 0x65, 0x4f, 0x76, 0x65, 0x72, 0x72, 0x69,
	0x64, 0x65, 0x73, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x52, 0x0f, 0x74, 0x6f, 0x67, 0x67, 0x6c, 0x65,
	0x4f, 0x76, 0x65, 0x72, 0x72, 0x69, 0x64, 0x65, 0x73, 0x12, 0x26, 0x0a, 0x0f, 0x77, 0x61, 0x69,
	0x74, 0x5f, 0x66, 0x6f, 0x72, 0x5f, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x73, 0x18, 0x02, 0x20, 0x03,
	0x28, 0x09, 0x52, 0x0d, 0x77, 0x61, 0x69, 0x74, 0x46, 0x6f, 0x72, 0x41, 0x67, 0x65, 0x6e, 0x74,
	0x73, 0x1a, 0x42, 0x0a, 0x14, 0x54, 0x6f, 0x67, 0x67, 0x6c, 0x65, 0x4f, 0x76, 0x65, 0x72, 0x72,
	0x69, 0x64, 0x65, 0x73, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65, 0x79,
	0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x14, 0x0a, 0x05, 0x76,
	0x61, 0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x08, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75,
	0x65, 0x3a, 0x02, 0x38, 0x01, 0x42, 0x26, 0x5a, 0x24, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e,
	0x63, 0x6f, 0x6d, 0x2f, 0x6f, 0x70, 0x65, 0x6e, 0x63, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x2f, 0x6b,
	0x6e, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x63, 0x65, 0x6f, 0x73, 0x62, 0x06, 0x70,
	0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_alpine_proto_rawDescOnce sync.Once
	file_alpine_proto_rawDescData = file_alpine_proto_rawDesc
)

func file_alpine_proto_rawDescGZIP() []byte {
	file_alpine_proto_rawDescOnce.Do(func() {
		file_alpine_proto_rawDescData = protoimpl.X.CompressGZIP(file_alpine_proto_rawDescData)
	})
	return file_alpine_proto_rawDescData
}

var file_alpine_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_alpine_proto_goTypes = []interface{}{
	(*AlpineDataplaneConfig)(nil), // 0: alpine.AlpineDataplaneConfig
	nil,                   // 1: alpine.AlpineDataplaneConfig.ToggleOverridesEntry
}
var file_alpine_proto_depIdxs = []int32{
	1, // 0: alpine.AlpineDataplaneConfig.toggle_overrides:type_name -> alpine.AlpineDataplaneConfig.ToggleOverridesEntry
	1, // [1:1] is the sub-list for method output_type
	1, // [1:1] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_alpine_proto_init() }
func file_alpine_proto_init() {
	if File_alpine_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_alpine_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*AlpineDataplaneConfig); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_alpine_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_alpine_proto_goTypes,
		DependencyIndexes: file_alpine_proto_depIdxs,
		MessageInfos:      file_alpine_proto_msgTypes,
	}.Build()
	File_alpine_proto = out.File
	file_alpine_proto_rawDesc = nil
	file_alpine_proto_goTypes = nil
	file_alpine_proto_depIdxs = nil
}
