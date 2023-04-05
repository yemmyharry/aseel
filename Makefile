proto:
	protoc pointSystem/pointSystemPb/point_system.proto --go_out=. --go-grpc_out=require_unimplemented_servers=false:.