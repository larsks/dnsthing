PKG = $(shell awk '/^module/ {nf=split($$2, parts, "/"); print parts[nf]}' go.mod)

VERSION = $(shell git describe --tags --exact-match 2> /dev/null || echo dev)

GOFILES = $(shell go list -f '{{range .GoFiles}}{{$$.Dir}}/{{.}}{{"\n"}}{{end}}' ./...)

GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
GOARM  ?= $(shell go env GOARM)

EXE = $(PKG)$(BUILDSUFFIX)

COMPILE =	go build -o $@ -ldflags '-X $(PKG)/version.Version=$(VERSION)'

all: $(EXE)

lint:
	golangci-lint run

test:
	go test ./...

$(EXE): $(GOFILES)
	$(COMPILE)

clean:
	rm -f $(EXE)

.PHONY: all clean lint test
