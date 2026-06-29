# code-lc4ri CLI / TUI (Go)
#
#   make build     -> ./code-lc4ri
#   make install   -> $GOBIN/code-lc4ri (or $GOPATH/bin)
#   make run FILE=../example/docker_two_tier_tutorial_en.md
#   make fmt vet

BINARY := code-lc4ri
PKG    := .

.PHONY: build install run fmt vet clean

build:
	go build -o $(BINARY) $(PKG)

install:
	go install $(PKG)

run: build
	./$(BINARY) tui $(FILE)

fmt:
	gofmt -w *.go

vet:
	go vet ./...

clean:
	rm -f $(BINARY)
