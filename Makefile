# Paths
CMD := ./cmd/api
BINARY := weather-api
BINDIR := bin

# Release cross-compile (override for your server, e.g. GOOS=darwin GOARCH=arm64)
GOOS ?= linux
GOARCH ?= amd64

.PHONY: run build test deploy clean

## run: start the API locally (listens on :8080)
run:
	go run $(CMD)

## build: compile to bin/$(BINARY) for the current OS/arch
build: $(BINDIR)/$(BINARY)

$(BINDIR)/$(BINARY):
	mkdir -p $(BINDIR)
	go build -o $(BINDIR)/$(BINARY) $(CMD)

## test: run all tests
test:
	go test ./...

## deploy: produce a stripped release binary for $(GOOS)/$(GOARCH) at bin/$(BINARY)-$(GOOS)-$(GOARCH)
deploy:
	mkdir -p $(BINDIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -trimpath -ldflags="-s -w" \
		-o $(BINDIR)/$(BINARY)-$(GOOS)-$(GOARCH) $(CMD)
	@echo "Artifact: $(BINDIR)/$(BINARY)-$(GOOS)-$(GOARCH)"

## clean: remove build outputs
clean: clean-bindir

clean-bindir:
	rm -rf $(BINDIR)
