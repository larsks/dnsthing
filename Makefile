PKG = $(shell grep '^module ' go.mod | cut -f2 -d ' ')

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
