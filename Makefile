proto:
	protoc pointSystem/pointSystemPb/point_system.proto --go_out=. --go-grpc_out=require_unimplemented_servers=false:.

run:
	go run main.go

up:
	docker-compose up -d

down:
	docker-compose down