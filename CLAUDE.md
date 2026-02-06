# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is **gorfc-mcp-server** — a Model Context Protocol (MCP) server that bridges LLMs to SAP systems via RFC (Remote Function Call). It exposes SAP RFC operations as MCP tools over stdio transport, allowing AI agents to ping SAP systems, describe function modules, invoke BAPIs/RFCs, and retrieve call metrics.

Single-file Go application: all logic is in `cmd/gorfc-mcp-server/main.go`.

## Build

Requires the SAP NW RFC SDK installed at `/usr/local/sap/nwrfcsdk/` (headers in `include/`, libraries in `lib/`).

```
make build    # compiles to ./gorfc-mcp-server
make clean    # removes binary
```

The build uses CGO with flags pointing to the NW RFC SDK:
- `CGO_CFLAGS="-I/usr/local/sap/nwrfcsdk/include"`
- `CGO_LDFLAGS="-L/usr/local/sap/nwrfcsdk/lib"`

There are no tests yet. The project has no lint or vet targets configured.

## Running

The server reads SAP connection destinations from `sapnwrfc.ini` (standard SAP NW RFC SDK config file in working directory). Pass the destination name via `SAP_DEST` env var or as the first CLI argument:

```
SAP_DEST=UI2 ./gorfc-mcp-server
```

Logs go to stderr with `[gorfc-mcp]` prefix.

## Architecture

Everything lives in `cmd/gorfc-mcp-server/main.go`. Key components:

- **connManager**: Thread-safe wrapper around `gorfc.Connection`. All RFC calls are serialized through its mutex since the SAP NW RFC SDK is not thread-safe per connection handle. Includes auto-reconnect with exponential backoff (3 retries, starting at 100ms).
- **coerceParams / coerceValue**: Type coercion layer that converts JSON-deserialized Go types (`float64`, `string`, etc.) to the specific Go types `gorfc` expects (e.g., `int32` for `RFCTYPE_INT`, `time.Time` for dates/times, `[]byte` for byte fields via base64). Recursively handles structures and tables.
- **validateParameters**: Pre-call check that all parameter names exist in the function description (after uppercasing).
- **metrics**: In-memory call counter tracking total/success/failure counts, durations, and per-function stats.

## MCP Tools Exposed

| Tool | Purpose |
|------|---------|
| `rfc_ping` | Verify SAP connectivity |
| `rfc_connection_info` | Get connection attributes + SDK version |
| `rfc_describe` | Get function module metadata (parameters, types) |
| `rfc_call` | Invoke an RFC function with parameters |
| `metrics_get` | Return call statistics |

## Dependencies

- `github.com/THM-MA/gorfc` — Go bindings for SAP NW RFC SDK
- `github.com/modelcontextprotocol/go-sdk` v1.2.0 — MCP Go SDK

## Important Notes

- **`sapnwrfc.ini` contains plaintext credentials** — gitignored, must never be committed.
- `dev_rfc.log` is generated at runtime by the SAP NW RFC SDK — also gitignored.
- All RFC parameter names are uppercased before lookup against function metadata — parameter names in `rfc_call` are case-insensitive.
- Default RFC timeout is 30 seconds when no context deadline is set.
