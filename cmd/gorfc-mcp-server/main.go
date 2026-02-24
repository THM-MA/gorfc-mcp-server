package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	gorfc "github.com/thm-ma/gorfc/gorfc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var logger = log.New(os.Stderr, "[gorfc-mcp] ", log.LstdFlags)

// ─── Connection Manager ───────────────────────────────────────────────────────

// connManager is a thread-safe wrapper around gorfc.Connection.
// All RFC calls are serialized through the mutex since the SAP NW RFC SDK is
// not thread-safe per connection handle. Auto-reconnect uses exponential
// backoff (3 retries, starting at 100 ms).
type connManager struct {
	mu         sync.Mutex
	conn       *gorfc.Connection
	connParams gorfc.ConnectionParameters
}

// newConnManager connects using a destination name from sapnwrfc.ini.
func newConnManager(dest string) (*connManager, error) {
	return newConnManagerFromParams(gorfc.ConnectionParameters{"dest": dest})
}

// newConnManagerFromParams connects using explicit SAP connection parameters.
func newConnManagerFromParams(params gorfc.ConnectionParameters) (*connManager, error) {
	cm := &connManager{connParams: params}
	if err := cm.connect(); err != nil {
		return nil, err
	}
	return cm, nil
}

func (cm *connManager) connect() error {
	conn, err := gorfc.ConnectionFromParams(cm.connParams)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	cm.conn = conn
	return nil
}

// connParamsFromEnv builds ConnectionParameters from SAP_* environment variables.
// Returns nil, nil when neither SAP_ASHOST nor SAP_MSHOST is set (caller should
// fall back to SAP_DEST). Returns an error for a partial / invalid configuration.
//
// Direct application-server connection (SAP_ASHOST):
//
//	SAP_ASHOST  – application server hostname
//	SAP_SYSNR   – system number (optional, defaults to "00")
//	SAP_CLIENT  – SAP client / mandant (required)
//	SAP_USER    – logon user (required)
//	SAP_PASSWD  – logon password (required)
//	SAP_LANG    – logon language (optional)
//
// Load-balancing / message-server connection (SAP_MSHOST):
//
//	SAP_MSHOST  – message server hostname
//	SAP_MSSERV  – message server service / port (optional)
//	SAP_SYSID   – SAP system ID (optional)
//	SAP_GROUP   – logon group (optional)
//	SAP_CLIENT  – SAP client / mandant (required)
//	SAP_USER    – logon user (required)
//	SAP_PASSWD  – logon password (required)
//	SAP_LANG    – logon language (optional)
func connParamsFromEnv() (gorfc.ConnectionParameters, error) {
	ashost := os.Getenv("SAP_ASHOST")
	mshost := os.Getenv("SAP_MSHOST")
	if ashost == "" && mshost == "" {
		return nil, nil
	}

	client := os.Getenv("SAP_CLIENT")
	user := os.Getenv("SAP_USER")
	passwd := os.Getenv("SAP_PASSWD")

	var missing []string
	if client == "" {
		missing = append(missing, "SAP_CLIENT")
	}
	if user == "" {
		missing = append(missing, "SAP_USER")
	}
	if passwd == "" {
		missing = append(missing, "SAP_PASSWD")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}

	params := gorfc.ConnectionParameters{
		"client": client,
		"user":   user,
		"passwd": passwd,
	}

	if ashost != "" {
		params["ashost"] = ashost
		if sysnr := os.Getenv("SAP_SYSNR"); sysnr != "" {
			params["sysnr"] = sysnr
		}
	} else {
		params["mshost"] = mshost
		if sysid := os.Getenv("SAP_SYSID"); sysid != "" {
			params["sysid"] = sysid
		}
		if msserv := os.Getenv("SAP_MSSERV"); msserv != "" {
			params["msserv"] = msserv
		}
		if group := os.Getenv("SAP_GROUP"); group != "" {
			params["group"] = group
		}
	}

	if lang := os.Getenv("SAP_LANG"); lang != "" {
		params["lang"] = lang
	}

	return params, nil
}

// withConn runs fn under the mutex, retrying up to 3 times with reconnect on
// communication failures.
func (cm *connManager) withConn(fn func(*gorfc.Connection) error) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	backoff := 100 * time.Millisecond
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if !isConnErr(lastErr) {
				return lastErr
			}
			logger.Printf("reconnect attempt %d (backoff %v)", attempt, backoff)
			time.Sleep(backoff)
			backoff *= 2
			if err := cm.connect(); err != nil {
				lastErr = err
				continue
			}
		}
		if err := fn(cm.conn); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

// isConnErr returns true for connection-level errors that may be resolved by
// reconnecting (communication failure, invalid handle).
func isConnErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "RFC_COMMUNICATION_FAILURE") ||
		strings.Contains(s, "RFC_INVALID_HANDLE") ||
		strings.Contains(s, "HANDLE_MISMATCH")
}

func (cm *connManager) ping(ctx context.Context) error {
	return cm.withConn(func(c *gorfc.Connection) error { return c.Ping() })
}

func (cm *connManager) connectionAttributes(ctx context.Context) (gorfc.ConnectionAttributes, error) {
	var out gorfc.ConnectionAttributes
	err := cm.withConn(func(c *gorfc.Connection) error {
		var e error
		out, e = c.GetConnectionAttributes()
		return e
	})
	return out, err
}

func (cm *connManager) describe(ctx context.Context, funcName string) (gorfc.FunctionDescription, error) {
	var out gorfc.FunctionDescription
	err := cm.withConn(func(c *gorfc.Connection) error {
		var e error
		out, e = c.GetFunctionDescription(funcName)
		return e
	})
	return out, err
}

func (cm *connManager) call(ctx context.Context, funcName string, params map[string]interface{}) (map[string]interface{}, error) {
	var out map[string]interface{}
	err := cm.withConn(func(c *gorfc.Connection) error {
		var e error
		out, e = c.Call(funcName, params)
		return e
	})
	return out, err
}

// ─── Metrics ─────────────────────────────────────────────────────────────────

type metrics struct {
	mu          sync.Mutex
	total       int64
	success     int64
	failure     int64
	totalDur    time.Duration
	perFunction map[string]int64
}

func newMetrics() *metrics {
	return &metrics{perFunction: make(map[string]int64)}
}

func (m *metrics) record(name string, dur time.Duration, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.total++
	m.totalDur += dur
	if err == nil {
		m.success++
	} else {
		m.failure++
	}
	m.perFunction[name]++
}

func (m *metrics) snapshot() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	var avg float64
	if m.total > 0 {
		avg = float64(m.totalDur.Milliseconds()) / float64(m.total)
	}
	pf := make(map[string]int64, len(m.perFunction))
	for k, v := range m.perFunction {
		pf[k] = v
	}
	return map[string]interface{}{
		"total":             m.total,
		"success":           m.success,
		"failure":           m.failure,
		"total_duration_ms": m.totalDur.Milliseconds(),
		"avg_duration_ms":   avg,
		"per_function":      pf,
	}
}

// ─── Parameter validation & type coercion ────────────────────────────────────

// validateParameters checks that every key in params exists as a parameter
// of funcDesc (case-insensitive).
func validateParameters(params map[string]interface{}, funcDesc gorfc.FunctionDescription) error {
	for key := range params {
		upper := strings.ToUpper(key)
		found := false
		for _, p := range funcDesc.Parameters {
			if p.Name == upper {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown parameter %q for function %q", key, funcDesc.Name)
		}
	}
	return nil
}

// coerceParams uppercases parameter names and converts JSON-deserialized values
// to the Go types expected by gorfc.
func coerceParams(params map[string]interface{}, funcDesc gorfc.FunctionDescription) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(params))
	for key, val := range params {
		upper := strings.ToUpper(key)
		var pd *gorfc.ParameterDescription
		for i := range funcDesc.Parameters {
			if funcDesc.Parameters[i].Name == upper {
				pd = &funcDesc.Parameters[i]
				break
			}
		}
		if pd == nil {
			out[upper] = val
			continue
		}
		coerced, err := coerceValue(val, pd.ParameterType, pd.TypeDesc)
		if err != nil {
			return nil, fmt.Errorf("parameter %q: %w", upper, err)
		}
		out[upper] = coerced
	}
	return out, nil
}

// coerceValue converts a single value from its JSON type to what gorfc needs.
func coerceValue(val interface{}, rfcType string, typeDesc gorfc.TypeDescription) (interface{}, error) {
	if val == nil {
		return nil, nil
	}
	switch rfcType {
	case "RFCTYPE_INT", "RFCTYPE_INT2", "RFCTYPE_INT8":
		switch v := val.(type) {
		case float64:
			return int(v), nil
		case int:
			return v, nil
		case int64:
			return int(v), nil
		case string:
			var i int
			if _, err := fmt.Sscan(v, &i); err != nil {
				return nil, fmt.Errorf("expected integer, got %q", v)
			}
			return i, nil
		}
		return int(0), fmt.Errorf("cannot coerce %T to INT", val)

	case "RFCTYPE_INT1":
		switch v := val.(type) {
		case float64:
			return int(v), nil
		case int:
			return v, nil
		case string:
			var i int
			if _, err := fmt.Sscan(v, &i); err != nil {
				return nil, fmt.Errorf("expected integer, got %q", v)
			}
			return i, nil
		}
		return int(0), fmt.Errorf("cannot coerce %T to INT1", val)

	case "RFCTYPE_FLOAT", "RFCTYPE_BCD", "RFCTYPE_DECF16", "RFCTYPE_DECF34":
		switch v := val.(type) {
		case float64:
			return v, nil
		case int:
			return float64(v), nil
		case string:
			return v, nil // gorfc handles string for BCD/DECF
		}
		return val, nil

	case "RFCTYPE_CHAR", "RFCTYPE_STRING", "RFCTYPE_NUM", "RFCTYPE_UTCLONG":
		switch v := val.(type) {
		case string:
			return v, nil
		case float64:
			return fmt.Sprintf("%g", v), nil
		default:
			return fmt.Sprintf("%v", v), nil
		}

	case "RFCTYPE_DATE":
		switch v := val.(type) {
		case string:
			t, err := time.Parse("20060102", v)
			if err != nil {
				return nil, fmt.Errorf("DATE must be YYYYMMDD, got %q: %w", v, err)
			}
			return t, nil
		case time.Time:
			return v, nil
		}
		return nil, fmt.Errorf("cannot coerce %T to DATE", val)

	case "RFCTYPE_TIME":
		switch v := val.(type) {
		case string:
			t, err := time.Parse("150405", v)
			if err != nil {
				return nil, fmt.Errorf("TIME must be HHMMSS, got %q: %w", v, err)
			}
			return t, nil
		case time.Time:
			return v, nil
		}
		return nil, fmt.Errorf("cannot coerce %T to TIME", val)

	case "RFCTYPE_BYTE", "RFCTYPE_XSTRING":
		switch v := val.(type) {
		case string:
			b, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				return nil, fmt.Errorf("BYTE/XSTRING must be base64-encoded: %w", err)
			}
			return b, nil
		case []byte:
			return v, nil
		}
		return nil, fmt.Errorf("cannot coerce %T to BYTE/XSTRING", val)

	case "RFCTYPE_STRUCTURE":
		m, ok := val.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected object for STRUCTURE, got %T", val)
		}
		result := make(map[string]interface{}, len(m))
		for k, v := range m {
			upper := strings.ToUpper(k)
			fieldType, fieldTypeDesc := "", gorfc.TypeDescription{}
			for _, f := range typeDesc.Fields {
				if f.Name == upper {
					fieldType = f.FieldType
					fieldTypeDesc = f.TypeDesc
					break
				}
			}
			coerced, err := coerceValue(v, fieldType, fieldTypeDesc)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", upper, err)
			}
			result[upper] = coerced
		}
		return result, nil

	case "RFCTYPE_TABLE":
		arr, ok := val.([]interface{})
		if !ok {
			return nil, fmt.Errorf("expected array for TABLE, got %T", val)
		}
		result := make([]interface{}, len(arr))
		for i, row := range arr {
			coerced, err := coerceValue(row, "RFCTYPE_STRUCTURE", typeDesc)
			if err != nil {
				return nil, fmt.Errorf("row %d: %w", i, err)
			}
			result[i] = coerced
		}
		return result, nil
	}

	// Unknown type — pass through unchanged.
	return val, nil
}

// ─── RFC_READ_TABLE helpers ───────────────────────────────────────────────────

// sanitizeABAPString escapes single quotes for use in ABAP WHERE clauses.
func sanitizeABAPString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// parseReadTableResult parses the DATA rows from an RFC_READ_TABLE result into
// a slice of maps. Field offsets and lengths are taken from the FIELDS response
// table returned by RFC_READ_TABLE itself.
func parseReadTableResult(result map[string]interface{}) ([]map[string]string, error) {
	type fieldMeta struct {
		name   string
		offset int
		length int
	}
	var fields []fieldMeta
	if raw, ok := result["FIELDS"]; ok {
		if arr, ok := raw.([]interface{}); ok {
			for _, fRaw := range arr {
				f, ok := fRaw.(map[string]interface{})
				if !ok {
					continue
				}
				name, _ := f["FIELDNAME"].(string)
				offsetStr, _ := f["OFFSET"].(string)
				lengthStr, _ := f["LENGTH"].(string)
				var offset, length int
				fmt.Sscan(strings.TrimSpace(offsetStr), &offset)
				fmt.Sscan(strings.TrimSpace(lengthStr), &length)
				fields = append(fields, fieldMeta{
					name:   strings.TrimSpace(name),
					offset: offset,
					length: length,
				})
			}
		}
	}

	dataRaw, ok := result["DATA"]
	if !ok {
		return nil, nil
	}
	rows, ok := dataRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected DATA type: %T", dataRaw)
	}

	out := make([]map[string]string, 0, len(rows))
	for _, rowRaw := range rows {
		row, ok := rowRaw.(map[string]interface{})
		if !ok {
			continue
		}
		wa, _ := row["WA"].(string)
		record := make(map[string]string, len(fields))
		for _, f := range fields {
			if f.offset < len(wa) {
				end := f.offset + f.length
				if end > len(wa) {
					end = len(wa)
				}
				record[f.name] = strings.TrimSpace(wa[f.offset:end])
			} else {
				record[f.name] = ""
			}
		}
		out = append(out, record)
	}
	return out, nil
}

// ─── MCP result helpers ───────────────────────────────────────────────────────

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func jsonResult(v interface{}) *mcp.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return textResult(fmt.Sprintf("json marshal error: %v", err))
	}
	return textResult(string(b))
}

func errResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	dest := os.Getenv("SAP_DEST")
	if dest == "" && len(os.Args) > 1 {
		dest = os.Args[1]
	}

	var cm *connManager
	var connErr error

	if dest != "" {
		logger.Printf("connecting to SAP destination %q", dest)
		cm, connErr = newConnManager(dest)
	} else {
		params, err := connParamsFromEnv()
		if err != nil {
			logger.Fatalf("SAP connection config error: %v", err)
		}
		if params == nil {
			logger.Fatal("SAP connection required: set SAP_DEST (or pass as argument) for " +
				"ini-based connections, or set SAP_ASHOST + SAP_CLIENT + SAP_USER + SAP_PASSWD " +
				"(or SAP_MSHOST for load-balancing) for direct connections")
		}
		host := params["ashost"]
		if host == "" {
			host = params["mshost"]
		}
		logger.Printf("connecting directly to SAP (host=%s, client=%s, user=%s)",
			host, params["client"], params["user"])
		cm, connErr = newConnManagerFromParams(params)
	}

	if connErr != nil {
		logger.Fatalf("failed to connect: %v", connErr)
	}
	logger.Printf("connected")

	m := newMetrics()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "gorfc-mcp-server",
		Version: "1.0.0",
	}, nil)

	// ── rfc_ping ──────────────────────────────────────────────────────────────
	server.AddTool(&mcp.Tool{
		Name:        "rfc_ping",
		Description: "Verify SAP connectivity by pinging the connected system.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		t0 := time.Now()
		err := cm.ping(ctx)
		m.record("rfc_ping", time.Since(t0), err)
		if err != nil {
			return errResult(err), nil
		}
		return textResult("PONG — SAP system is reachable."), nil
	})

	// ── rfc_connection_info ───────────────────────────────────────────────────
	server.AddTool(&mcp.Tool{
		Name:        "rfc_connection_info",
		Description: "Get SAP connection attributes (SID, client, host, user) and NW RFC SDK version.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		t0 := time.Now()
		attrs, err := cm.connectionAttributes(ctx)
		m.record("rfc_connection_info", time.Since(t0), err)
		if err != nil {
			return errResult(err), nil
		}
		major, minor, patch := gorfc.GetNWRFCLibVersion()
		return jsonResult(map[string]interface{}{
			"connection":  attrs,
			"sdk_version": fmt.Sprintf("%d.%d.%d", major, minor, patch),
		}), nil
	})

	// ── rfc_describe ──────────────────────────────────────────────────────────
	server.AddTool(&mcp.Tool{
		Name:        "rfc_describe",
		Description: "Get function module metadata: parameters, types, directions, and optionally field details for structures/tables.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"function_name":{"type":"string","description":"Name of the RFC function module (e.g. STFC_CONNECTION)"}},"required":["function_name"]}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			FunctionName string `json:"function_name"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err)), nil
		}
		if args.FunctionName == "" {
			return errResult(fmt.Errorf("function_name is required")), nil
		}
		funcName := strings.ToUpper(args.FunctionName)
		t0 := time.Now()
		desc, err := cm.describe(ctx, funcName)
		m.record("rfc_describe", time.Since(t0), err)
		if err != nil {
			return errResult(err), nil
		}
		return jsonResult(desc), nil
	})

	// ── rfc_call ──────────────────────────────────────────────────────────────
	server.AddTool(&mcp.Tool{
		Name:        "rfc_call",
		Description: "Invoke an RFC function module with parameters and return the result. Parameter names are case-insensitive.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"function_name":{"type":"string","description":"Name of the RFC function module to call"},"parameters":{"type":"object","description":"Input parameters (IMPORT/CHANGING/TABLE). DATE fields use YYYYMMDD, TIME fields use HHMMSS, BYTE/XSTRING fields use base64."}},"required":["function_name"]}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			FunctionName string                 `json:"function_name"`
			Parameters   map[string]interface{} `json:"parameters"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err)), nil
		}
		if args.FunctionName == "" {
			return errResult(fmt.Errorf("function_name is required")), nil
		}
		funcName := strings.ToUpper(args.FunctionName)
		if args.Parameters == nil {
			args.Parameters = map[string]interface{}{}
		}

		desc, err := cm.describe(ctx, funcName)
		if err != nil {
			return errResult(fmt.Errorf("describe %q: %w", funcName, err)), nil
		}
		if err := validateParameters(args.Parameters, desc); err != nil {
			return errResult(err), nil
		}
		coerced, err := coerceParams(args.Parameters, desc)
		if err != nil {
			return errResult(fmt.Errorf("coerce parameters: %w", err)), nil
		}

		t0 := time.Now()
		result, err := cm.call(ctx, funcName, coerced)
		m.record(funcName, time.Since(t0), err)
		if err != nil {
			return errResult(err), nil
		}
		return jsonResult(result), nil
	})

	// ── get_table_metadata ────────────────────────────────────────────────────
	server.AddTool(&mcp.Tool{
		Name:        "get_table_metadata",
		Description: "Retrieve field details (name, type, length, domain, description) for a SAP table via DDIF_FIELDINFO_GET.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"table_name":{"type":"string","description":"SAP table name (e.g. SFLIGHT)"},"language":{"type":"string","description":"Language key for descriptions (default: D)"}},"required":["table_name"]}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			TableName string `json:"table_name"`
			Language  string `json:"language"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err)), nil
		}
		if args.TableName == "" {
			return errResult(fmt.Errorf("table_name is required")), nil
		}
		if args.Language == "" {
			args.Language = "D"
		}
		t0 := time.Now()
		result, err := cm.call(ctx, "DDIF_FIELDINFO_GET", map[string]interface{}{
			"TABNAME": strings.ToUpper(args.TableName),
			"LANGU":   args.Language,
		})
		m.record("get_table_metadata", time.Since(t0), err)
		if err != nil {
			return errResult(err), nil
		}
		return jsonResult(result), nil
	})

	// ── get_table_relations ───────────────────────────────────────────────────
	server.AddTool(&mcp.Tool{
		Name:        "get_table_relations",
		Description: "Retrieve foreign-key relationships and cardinalities for a SAP table via FAPI_GET_FOREIGN_KEY_RELATIONS.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"table_name":{"type":"string","description":"SAP table name"}},"required":["table_name"]}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			TableName string `json:"table_name"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err)), nil
		}
		if args.TableName == "" {
			return errResult(fmt.Errorf("table_name is required")), nil
		}
		t0 := time.Now()
		result, err := cm.call(ctx, "FAPI_GET_FOREIGN_KEY_RELATIONS", map[string]interface{}{
			"TABNAME": strings.ToUpper(args.TableName),
		})
		m.record("get_table_relations", time.Since(t0), err)
		if err != nil {
			return errResult(err), nil
		}
		return jsonResult(result), nil
	})

	// ── search_sap_tables ─────────────────────────────────────────────────────
	server.AddTool(&mcp.Tool{
		Name:        "search_sap_tables",
		Description: "Search SAP tables by description/business term via RFC_READ_TABLE on DD02T. Use % as wildcard (e.g. '%material%').",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"search_term":{"type":"string","description":"Text to search for with LIKE semantics (% as wildcard)"},"language":{"type":"string","description":"Language key (default: D)"},"max_results":{"type":"integer","description":"Maximum results to return (default: 100)"}},"required":["search_term"]}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			SearchTerm string `json:"search_term"`
			Language   string `json:"language"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err)), nil
		}
		if args.SearchTerm == "" {
			return errResult(fmt.Errorf("search_term is required")), nil
		}
		if args.Language == "" {
			args.Language = "D"
		}
		if args.MaxResults <= 0 {
			args.MaxResults = 100
		}

		where := fmt.Sprintf("DDLANGUAGE = '%s' AND DDTEXT LIKE '%s'",
			sanitizeABAPString(args.Language),
			sanitizeABAPString(args.SearchTerm))

		t0 := time.Now()
		result, err := cm.call(ctx, "RFC_READ_TABLE", map[string]interface{}{
			"QUERY_TABLE": "DD02T",
			"ROWCOUNT":    args.MaxResults,
			"OPTIONS":     []interface{}{map[string]interface{}{"TEXT": where}},
			"FIELDS": []interface{}{
				map[string]interface{}{"FIELDNAME": "TABNAME"},
				map[string]interface{}{"FIELDNAME": "DDTEXT"},
			},
		})
		m.record("search_sap_tables", time.Since(t0), err)
		if err != nil {
			return errResult(err), nil
		}

		rows, err := parseReadTableResult(result)
		if err != nil {
			return errResult(err), nil
		}
		return jsonResult(rows), nil
	})

	// ── metrics_get ───────────────────────────────────────────────────────────
	server.AddTool(&mcp.Tool{
		Name:        "metrics_get",
		Description: "Return RFC call statistics: total/success/failure counts, durations, and per-function call counts.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return jsonResult(m.snapshot()), nil
	})

	logger.Printf("MCP server starting (stdio)")
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		logger.Fatalf("server error: %v", err)
	}
}
