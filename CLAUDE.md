# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**gorfc-mcp-server** — a Model Context Protocol (MCP) server that bridges LLMs to SAP systems via RFC (Remote Function Call). It exposes SAP RFC operations as MCP tools over stdio transport, allowing AI agents to ping SAP systems, describe function modules, invoke BAPIs/RFCs, and retrieve call metrics.

Single-file Go application: all logic is in `cmd/gorfc-mcp-server/main.go`.

## Build

Requires the SAP NW RFC SDK installed at `/usr/local/sap/nwrfcsdk/` (headers in `include/`, libraries in `lib/`).

```bash
make build           # compiles to ./gorfc-mcp-server
make build-windows   # cross-compiles to ./gorfc-mcp-server.exe (Windows x86_64, requires MinGW: sudo apt install gcc-mingw-w64-x86-64)
make clean           # removes built binaries
```

CGO flags used: `CGO_CFLAGS="-I/usr/local/sap/nwrfcsdk/include"` and `CGO_LDFLAGS="-L/usr/local/sap/nwrfcsdk/lib"`.

There are no tests yet. The project has no lint or vet targets configured.

## Running

Two connection modes are supported.

**Mode 1 — ini-based (`SAP_DEST`):** reads the destination from `sapnwrfc.ini` in the working directory.

```bash
SAP_DEST=SID ./gorfc-mcp-server
# or
./gorfc-mcp-server SID
```

**Mode 2 — direct env vars:** supply connection parameters without an ini file.

```bash
# Direct application-server connection
SAP_ASHOST=sap.example.com SAP_SYSNR=00 \
  SAP_CLIENT=100 SAP_USER=rfcuser SAP_PASSWD=secret \
  ./gorfc-mcp-server

# Load-balancing / message-server connection
SAP_MSHOST=msg.example.com SAP_SYSID=SID SAP_GROUP=PUBLIC \
  SAP_CLIENT=100 SAP_USER=rfcuser SAP_PASSWD=secret \
  ./gorfc-mcp-server
```

Supported env vars for direct mode: `SAP_ASHOST`, `SAP_SYSNR`, `SAP_MSHOST`, `SAP_MSSERV`, `SAP_SYSID`, `SAP_GROUP`, `SAP_CLIENT` (required), `SAP_USER` (required), `SAP_PASSWD` (required), `SAP_LANG`.

Logs go to stderr with `[gorfc-mcp]` prefix.

## MCP Client Configuration

**Claude Desktop — ini-based** (`claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "sap": {
      "command": "/path/to/gorfc-mcp-server",
      "args": ["SID"],
      "env": { "LD_LIBRARY_PATH": "/usr/local/sap/nwrfcsdk/lib" }
    }
  }
}
```

**Claude Desktop — direct env vars:**
```json
{
  "mcpServers": {
    "sap": {
      "command": "/path/to/gorfc-mcp-server",
      "env": {
        "LD_LIBRARY_PATH": "/usr/local/sap/nwrfcsdk/lib",
        "SAP_ASHOST": "sap.example.com",
        "SAP_SYSNR": "00",
        "SAP_CLIENT": "100",
        "SAP_USER": "rfcuser",
        "SAP_PASSWD": "secret"
      }
    }
  }
}
```

**Claude Code CLI**: `claude mcp add sap /path/to/gorfc-mcp-server -- SID`

## Architecture

Everything lives in `cmd/gorfc-mcp-server/main.go`. Key components:

- **connManager**: Thread-safe wrapper around `gorfc.Connection`. Stores a `gorfc.ConnectionParameters` map and always connects via `gorfc.ConnectionFromParams`. Constructed with `newConnManager(dest)` (ini-based, sets `{"dest": dest}`) or `newConnManagerFromParams(params)` (direct params). Includes auto-reconnect with exponential backoff (3 retries, starting at 100ms).
- **connParamsFromEnv**: Reads `SAP_ASHOST` / `SAP_MSHOST` and related env vars (`SAP_SYSNR`, `SAP_CLIENT`, `SAP_USER`, `SAP_PASSWD`, `SAP_LANG`, `SAP_MSSERV`, `SAP_SYSID`, `SAP_GROUP`) and returns a `gorfc.ConnectionParameters` map. Returns `nil, nil` when neither `SAP_ASHOST` nor `SAP_MSHOST` is set so the caller falls back to `SAP_DEST`.
- **coerceParams / coerceValue**: Type coercion layer converting JSON-deserialized Go types (`float64`, `string`, etc.) to the specific Go types `gorfc` expects (e.g., `int32` for `RFCTYPE_INT`, `time.Time` for dates/times, `[]byte` for byte fields via base64). Recursively handles structures and tables.
- **validateParameters**: Pre-call check that all parameter names exist in the function description (after uppercasing).
- **metrics**: In-memory call counter tracking total/success/failure counts, durations, and per-function stats.
- **sanitizeABAPString**: Escapes characters for safely embedding strings into ABAP `WHERE` clauses / `RFC_READ_TABLE` filters.
- **parseReadTableResult**: Parses the legacy `DATA`/`WA` row format from `RFC_READ_TABLE` into `[]map[string]string` keyed by column name.

## MCP Tools Exposed

| Tool | Purpose |
|------|---------|
| `rfc_ping` | Verify SAP connectivity |
| `rfc_connection_info` | Get connection attributes + SDK version |
| `rfc_describe` | Get function module metadata (parameters, types, directions) |
| `rfc_call` | Invoke an RFC function with parameters |
| `get_table_metadata` | Retrieve field details for a table via `DDIF_FIELDINFO_GET` |
| `get_table_relations` | Retrieve foreign-key relationships via `FAPI_GET_FOREIGN_KEY_RELATIONS` |
| `search_sap_tables` | Search tables by description/business term via `RFC_READ_TABLE` on `DD02T` |
| `metrics_get` | Return call statistics |

## Parameter Type Mapping (rfc_call)

| ABAP Type | JSON Format |
|-----------|-------------|
| `INT`, `INT1`, `INT2`, `INT8` | number |
| `FLOAT`, `BCD`, `DECF` | number |
| `CHAR`, `STRING`, `NUM` | string |
| `DATE` | string `YYYYMMDD` |
| `TIME` | string `HHMMSS` |
| `BYTE`, `XSTRING` | string (base64) |
| `STRUCTURE` | object |
| `TABLE` | array of objects |

## Dependencies

- `github.com/thm-ma/gorfc` — Go bindings for SAP NW RFC SDK
- `github.com/modelcontextprotocol/go-sdk` v1.2.0 — MCP Go SDK

## Important Notes

- **`sapnwrfc.ini` contains plaintext credentials** — gitignored, must never be committed.
- `dev_rfc.log` is generated at runtime by the SAP NW RFC SDK — also gitignored.
- All RFC parameter names are uppercased before lookup against function metadata — `rfc_call` parameters are case-insensitive.
- Default RFC timeout is 30 seconds when no context deadline is set.
