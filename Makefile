# Copyright Contributors to the Open Cluster Management project

include ./Configfile
#-include /opt/build-harness/Makefile.prow

# Image URL to use all building/pushing image targets
IMG ?= quay.io/morvencao/alerts-collector:latest

.PHONY: fmt vet unit-tests e2e-tests build docker-build docker-pus cleanh

# Run go fmt against code
fmt:
	@go fmt ./...

# Run go vet against code
vet:
	@go vet ./...

# Run unit tests
unit-tests: fmt vet
	@echo "Run unit-tests"
	@go test -race ./...

# Run e2e tests
e2e-tests:
	@echo "TODO: Run e2e-tests"

# Build the binary
build: unit-tests
	@CGO_ENABLED=0 go build -a -installsuffix cgo -i -o bin/alerts-collector ./cmd/main.go

# Build the docker image
docker-build: build
	@docker build -f Dockerfile -t ${IMG} .

# Push the docker image
docker-push: docker-build
	@docker push ${IMG}

clean:
	@rm -rf ./bin
