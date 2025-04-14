FROM golang:1.23.3-alpine3.17 as builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o ./saturday.exe cmd/app/main.go

FROM alpine:latest
WORKDIR /root/

COPY --from=builder /app/ .
EXPOSE 50051
CMD ["./saturday.exe"]