TF_BIN ?= $(shell command -v terraform 2>/dev/null || command -v tofu 2>/dev/null)
export TF_ACC_TERRAFORM_PATH := $(TF_BIN)
ifneq (,$(findstring tofu,$(TF_BIN)))
export TF_ACC_PROVIDER_HOST := registry.opentofu.org
export TF_ACC_PROVIDER_NAMESPACE := hashicorp
endif

.PHONY: lint test docs check
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...
test:
	go test -race ./...
docs:
	go generate ./...
check: lint test
