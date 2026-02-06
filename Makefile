.PHONY: build clean help

CGO_CFLAGS := -I/usr/local/sap/nwrfcsdk/include
CGO_LDFLAGS := -L/usr/local/sap/nwrfcsdk/lib
OUTPUT := gorfc-mcp-server

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" go build -o $(OUTPUT) ./cmd/gorfc-mcp-server

clean:
	rm -f $(OUTPUT)

help:
	@echo "Available targets:"
	@echo "  make build  - Compile the project with CGO flags"
	@echo "  make clean  - Remove the built binary"
	@echo "  make help   - Show this help message"
