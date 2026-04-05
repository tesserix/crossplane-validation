VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
IMAGE_REPO ?= ghcr.io/tesserix/crossplane-validate-operator
IMAGE_TAG ?= $(VERSION)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build build-cli build-operator test lint proto docker-build docker-push helm-lint install clean

build: build-cli build-operator

build-cli:
	go build $(LDFLAGS) -o bin/crossplane-validate ./cmd/crossplane-validate

build-operator:
	go build $(LDFLAGS) -o bin/crossplane-validate-operator ./cmd/crossplane-validate-operator

test:
	go test ./... -v -race

lint:
	go vet ./...
	gofmt -l .

proto:
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/proto/v1/service.proto

docker-build:
	docker build -t $(IMAGE_REPO):$(IMAGE_TAG) .

docker-push: docker-build
	docker push $(IMAGE_REPO):$(IMAGE_TAG)

helm-lint:
	helm lint deploy/helm/crossplane-validate-operator

install:
	helm upgrade --install crossplane-validate-operator \
		deploy/helm/crossplane-validate-operator \
		--namespace crossplane-system \
		--create-namespace

uninstall:
	helm uninstall crossplane-validate-operator -n crossplane-system

clean:
	rm -rf bin/
