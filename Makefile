# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

run-server:
	go run cmd/webhook/main.go -config config/config-example.yaml

go-test: go-tidy
	go test -v ./...

go-fmt: go-tidy
	go fmt ./...

go-tidy:
	go mod tidy

build-webhook:
	go build -o knurse cmd/webhook/main.go

build-setup-ca-certs:
	go build -o setup-ca-certs cmd/setup-ca-certs/main.go