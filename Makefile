BINARY_NAME=monocle
.DEFAULT_GOAL=index

build:
	go build -o ./bin/${BINARY_NAME} ./cmd/app/main.go
	echo "builded successfully"

index: build
	./bin/${BINARY_NAME}

panic-test: build
	./bin/${BINARY_NAME} > ./logs/panic.txt 2>&1

index-gui: build
	./bin/${BINARY_NAME} --gui

search: build
	./bin/${BINARY_NAME} -i

python-run:
	./internal/app/semantic_embeddings/.venv/bin/python ./internal/app/semantic_embeddings/app.py