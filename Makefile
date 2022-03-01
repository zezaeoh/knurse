# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

run-server:
	go run cmd/webhook/main.go -config config/config-example.yaml

go-test: go-tidy
	go test -v ./...

go-build:
	go build -o knurse cmd/webhook/main.go

go-fmt: go-tidy
	go fmt ./...

go-tidy:
	go mod tidy
