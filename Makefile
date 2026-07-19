default: build

build:
	go build -o terraform-provider-featureflip .

install:
	go install .

lint:
	gofmt -l . && test -z "$$(gofmt -l .)"
	go vet ./...

test:
	go test ./internal/... -count=1 -v

# Requires the local API stack: run scripts/local-api.sh first, which writes .env.acceptance
testacc:
	set -a && . ./.env.acceptance && set +a && TF_ACC=1 go test ./internal/provider -count=1 -v -timeout 30m $(TESTARGS)

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name featureflip

.PHONY: default build install lint test testacc docs
