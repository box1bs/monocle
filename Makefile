BINARY_NAME=monocle
.DEFAULT_GOAL=index

build:
	go build -o ./bin/${BINARY_NAME} ./cmd/app/main.go
	echo "builded successfully"

index: build
	./bin/${BINARY_NAME}

search: build
	./bin/${BINARY_NAME} -i