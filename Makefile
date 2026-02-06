.PHONY: build build-windows clean help

CGO_CFLAGS := -I/usr/local/sap/nwrfcsdk/include
CGO_LDFLAGS := -L/usr/local/sap/nwrfcsdk/lib
OUTPUT := gorfc-mcp-server

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" go build -o $(OUTPUT) ./cmd/gorfc-mcp-server

build-windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" go build -o $(OUTPUT).exe ./cmd/gorfc-mcp-server

clean:
	rm -f $(OUTPUT) $(OUTPUT).exe

help:
	@echo "Available targets:"
	@echo "  make build          - Compile the project with CGO flags"
	@echo "  make build-windows  - Cross-compile for Windows x86_64"
	@echo "  make clean          - Remove built binaries"
	@echo "  make help           - Show this help message"
