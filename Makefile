VERSION ?= 0.2.0
BIN := bin
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: all test web server node release verify tidy

all: test web server node

tidy:
	go mod tidy

test:
	go test ./...

web:
	cd apps/web && npm install && npm run build

server:
	go build -o $(BIN)/omnoda-server ./cmd/omnoda-server

node:
	go build -o $(BIN)/omnoda ./cmd/omnoda
	go build -o $(BIN)/omnoda-sign ./cmd/omnoda-sign
	go build -o $(BIN)/omnoda-verify ./cmd/omnoda-verify
	go build -o $(BIN)/omnoda-fake-harness ./cmd/omnoda-fake-harness

release: node
	@test -n "$(SIGN_KEY)" || (echo "set SIGN_KEY to the release private key path"; exit 1)
	mkdir -p dist/releases
	@for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; \
	  ext=""; if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
	  out=dist/releases/omnoda_$(VERSION)_$${os}_$${arch}$$ext; \
	  echo "build $$out"; \
	  GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -o $$out ./cmd/omnoda; \
	  $(BIN)/omnoda-sign -key $(SIGN_KEY) -file $$out -out $$out.sig; \
	done

verify:
	go vet ./...
	cd apps/web && npm run typecheck
