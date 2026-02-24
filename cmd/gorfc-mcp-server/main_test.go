//go:build integration

package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// sapnwrfc.ini lives in the repo root; the SDK reads it from CWD.
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	if err := os.Chdir(root); err != nil {
		panic("chdir to repo root: " + err.Error())
	}
	os.Exit(m.Run())
}

// connManagerOrSkip returns a connected connManager using SAP_DEST (ini-based)
// or SAP_ASHOST/SAP_MSHOST + credentials (direct). Skips the test if neither is set.
//
// Run with: SAP_DEST=SID go test -tags integration ./cmd/gorfc-mcp-server/
// or:        SAP_ASHOST=host SAP_SYSNR=00 SAP_CLIENT=100 SAP_USER=u SAP_PASSWD=p \
//              go test -tags integration ./cmd/gorfc-mcp-server/
func connManagerOrSkip(t *testing.T) *connManager {
	t.Helper()
	if dest := os.Getenv("SAP_DEST"); dest != "" {
		cm, err := newConnManager(dest)
		if err != nil {
			t.Fatalf("newConnManager: %v", err)
		}
		return cm
	}
	params, err := connParamsFromEnv()
	if err != nil {
		t.Fatalf("connection params from env: %v", err)
	}
	if params == nil {
		t.Skip("neither SAP_DEST nor SAP_ASHOST/SAP_MSHOST set — skipping integration test")
	}
	cm, err := newConnManagerFromParams(params)
	if err != nil {
		t.Fatalf("newConnManagerFromParams: %v", err)
	}
	return cm
}

func TestPingSuccess(t *testing.T) {
	cm := connManagerOrSkip(t)
	if err := cm.ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

// TestDescribeFunctionModule mirrors RfcGetFunctionDesc + iterating parameter
// directions (RFC_IMPORT, RFC_EXPORT, RFC_CHANGING, RFC_TABLES) from the C sample.
func TestDescribeFunctionModule(t *testing.T) {
	cm := connManagerOrSkip(t)

	desc, err := cm.describe(context.Background(), "STFC_CONNECTION")
	if err != nil {
		t.Fatalf("describe STFC_CONNECTION: %v", err)
	}

	if desc.Name != "STFC_CONNECTION" {
		t.Errorf("desc.Name = %q, want STFC_CONNECTION", desc.Name)
	}

	var imports, exports []string
	for _, p := range desc.Parameters {
		switch p.Direction {
		case "RFC_IMPORT":
			imports = append(imports, p.Name)
		case "RFC_EXPORT":
			exports = append(exports, p.Name)
		}
	}

	wantImport := "REQUTEXT"
	if !containsStr(imports, wantImport) {
		t.Errorf("IMPORT parameters %v do not contain %q", imports, wantImport)
	}
	for _, want := range []string{"ECHOTEXT", "RESPTEXT"} {
		if !containsStr(exports, want) {
			t.Errorf("EXPORT parameters %v do not contain %q", exports, want)
		}
	}
}

// TestCallFunctionModule mirrors the full C sample flow:
// get description → fill IMPORT params → RfcInvoke → read EXPORT params.
// Uses STFC_CONNECTION: REQUTEXT (import) is echoed back as ECHOTEXT (export).
func TestCallFunctionModule(t *testing.T) {
	cm := connManagerOrSkip(t)
	ctx := context.Background()

	const funcName = "STFC_CONNECTION"
	const reqText = "hello gorfc"

	// Step 1: get function description (RfcGetFunctionDesc equivalent).
	desc, err := cm.describe(ctx, funcName)
	if err != nil {
		t.Fatalf("describe %q: %v", funcName, err)
	}

	// Step 2: validate and coerce IMPORT parameters (fillImports equivalent).
	params := map[string]interface{}{"REQUTEXT": reqText}
	if err := validateParameters(params, desc); err != nil {
		t.Fatalf("validateParameters: %v", err)
	}
	coerced, err := coerceParams(params, desc)
	if err != nil {
		t.Fatalf("coerceParams: %v", err)
	}

	// Step 3: invoke (RfcInvoke equivalent) — RFC_OK path.
	result, err := cm.call(ctx, funcName, coerced)
	if err != nil {
		t.Fatalf("call %q: %v", funcName, err)
	}

	// Step 4: read EXPORT parameters (printExports equivalent).
	echoRaw, ok := result["ECHOTEXT"]
	if !ok {
		t.Fatalf("result missing ECHOTEXT; got keys: %v", mapKeys(result))
	}
	echo, ok := echoRaw.(string)
	if !ok {
		t.Fatalf("ECHOTEXT type = %T, want string", echoRaw)
	}
	if !strings.Contains(echo, reqText) {
		t.Errorf("ECHOTEXT = %q, want it to contain %q", echo, reqText)
	}
	if _, ok := result["RESPTEXT"]; !ok {
		t.Errorf("result missing RESPTEXT; got keys: %v", mapKeys(result))
	}
}

// TestCallAbapException mirrors the RFC_ABAP_EXCEPTION branch of the C switch block.
// STFC_EXCEPTION always raises the named ABAP exception "EXAMPLE".
func TestCallAbapException(t *testing.T) {
	cm := connManagerOrSkip(t)
	ctx := context.Background()

	_, err := cm.call(ctx, "STFC_EXCEPTION", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected ABAP exception error, got nil")
	}
	// gorfc surfaces RFC_ABAP_EXCEPTION — the error string contains the exception key.
	if !strings.Contains(err.Error(), "RFC_ABAP_EXCEPTION") {
		t.Errorf("error %q does not contain RFC_ABAP_EXCEPTION", err.Error())
	}

	// The C sample's RFC_ABAP_EXCEPTION branch does NOT reconnect — connection must
	// still be valid after a named ABAP exception.
	if pingErr := cm.ping(ctx); pingErr != nil {
		t.Errorf("ping after ABAP exception failed: %v", pingErr)
	}
}


// ── helpers ───────────────────────────────────────────────────────────────────

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
