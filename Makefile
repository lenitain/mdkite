BINARY := mdkite

.PHONY: build install check clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINARY) .

install: build
	mkdir -p $(HOME)/.local/bin
	install -m 755 $(BINARY) $(HOME)/.local/bin/$(BINARY)

check:
	go vet ./...
	test -z "$$(gofmt -l .)"

clean:
	rm -f $(BINARY)
