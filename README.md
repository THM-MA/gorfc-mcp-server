# gorfc-mcp-server

A [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that connects LLMs to SAP systems via RFC (Remote Function Call). It exposes SAP RFC operations as MCP tools over stdio transport, allowing AI agents to ping SAP systems, describe function modules, invoke BAPIs/RFCs, and retrieve call metrics.

## Prerequisites

- **Go 1.24+**
- **SAP NW RFC SDK** installed at `/usr/local/sap/nwrfcsdk/` (headers in `include/`, libraries in `lib/`)
- A valid `sapnwrfc.ini` configuration file in the working directory with SAP connection destinations

## Build

```bash
make build           # compiles to ./gorfc-mcp-server
make build-windows   # cross-compiles to ./gorfc-mcp-server.exe (Windows x86_64)
make clean           # removes built binaries
```

The build uses CGO with flags pointing to the SAP NW RFC SDK:

```
CGO_CFLAGS="-I/usr/local/sap/nwrfcsdk/include"
CGO_LDFLAGS="-L/usr/local/sap/nwrfcsdk/lib"
```

### Cross-compiling for Windows

The `build-windows` target cross-compiles for Windows x86_64 using MinGW. It requires:

- **MinGW-w64 cross-compiler** (`x86_64-w64-mingw32-gcc`). Install on Debian/Ubuntu:
  ```bash
  sudo apt install gcc-mingw-w64-x86-64
  ```
- **Windows build of the SAP NW RFC SDK** with headers and libraries available at the configured SDK path

## Configuration

The server reads SAP connection destinations from a `sapnwrfc.ini` file (standard SAP NW RFC SDK config format) in the working directory. Example:

```ini
DEST=MY_SYSTEM
TYPE=A
ASHOST=sap.example.com
SYSNR=00
CLIENT=100
USER=rfcuser
PASSWD=secret
LANG=EN
```

> **Note:** `sapnwrfc.ini` contains plaintext credentials and is gitignored. Never commit this file.

## Running

Pass the SAP destination name via the `SAP_DEST` environment variable or as the first CLI argument:

```bash
SAP_DEST=MY_SYSTEM ./gorfc-mcp-server
```

```bash
./gorfc-mcp-server MY_SYSTEM
```

Logs are written to stderr with a `[gorfc-mcp]` prefix.

## MCP Client Configuration

### Claude Desktop

Add to your Claude Desktop config (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "sap": {
      "command": "/path/to/gorfc-mcp-server",
      "args": ["MY_SYSTEM"],
      "env": {
        "LD_LIBRARY_PATH": "/usr/local/sap/nwrfcsdk/lib"
      }
    }
  }
}
```

### Claude Code

Add via the CLI:

```bash
claude mcp add sap /path/to/gorfc-mcp-server -- MY_SYSTEM
```

## Tools

| Tool | Description |
|------|-------------|
| `rfc_ping` | Verify SAP connectivity |
| `rfc_connection_info` | Get connection attributes and NW RFC SDK version |
| `rfc_describe` | Get function module metadata (parameters, types, directions) |
| `rfc_call` | Invoke an RFC function module with parameters |
| `metrics_get` | Return call statistics and performance metrics |

### rfc_ping

Pings the connected SAP system to verify connectivity. Takes no parameters.

### rfc_connection_info

Returns connection attributes (system ID, client, host, etc.) and the SAP NW RFC SDK version. Takes no parameters.

### rfc_describe

Returns the full metadata of an RFC function module, including all parameters with their types and directions.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `function_name` | string | yes | Name of the RFC function module (e.g. `STFC_CONNECTION`) |

### rfc_call

Invokes an RFC function module with the given parameters and returns the result. Parameter names are case-insensitive. Use `rfc_describe` first to understand the expected parameter types.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `function_name` | string | yes | Name of the RFC function module to call |
| `parameters` | object | no | Input parameters for the function call |

Parameter value type mapping:

| ABAP Type | JSON Value |
|-----------|------------|
| INT, INT1, INT2, INT8 | number |
| FLOAT, BCD, DECF | number |
| CHAR, STRING, NUM | string |
| DATE | string (`YYYYMMDD`) |
| TIME | string (`HHMMSS`) |
| BYTE, XSTRING | string (base64-encoded) |
| STRUCTURE | object |
| TABLE | array of objects |

### metrics_get

Returns in-memory call statistics: total/successful/failed call counts, total and average duration, and per-function call counts. Takes no parameters.

## Architecture

All logic lives in a single file: `cmd/gorfc-mcp-server/main.go`.

- **connManager** -- Thread-safe wrapper around `gorfc.Connection`. All RFC calls are serialized through a mutex since the SAP NW RFC SDK is not thread-safe per connection handle. Includes auto-reconnect with exponential backoff (3 retries, starting at 100ms).
- **coerceParams / coerceValue** -- Type coercion layer that converts JSON-deserialized Go types (`float64`, `string`, etc.) to the specific Go types `gorfc` expects. Recursively handles structures and tables.
- **validateParameters** -- Pre-call validation that all parameter names exist in the function description.
- **metrics** -- In-memory call counter tracking total/success/failure counts, durations, and per-function stats.

## Example Prompts

See [EXAMPLES.md](EXAMPLES.md) for example prompts to use with this server.

## License

[Apache License 2.0](LICENSE)
