package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLiveGatePrecedesCredentialOrConnectorUse(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--viewer-id=1", "--dsn-env=DO_NOT_READ"}, &out)
	if err == nil || !strings.Contains(err.Error(), "--allow-live") || out.Len() != 0 {
		t.Fatalf("gate: %v %s", err, &out)
	}
}
func TestPlanSummaryRemovesSQLConditionsAndIdentifiers(t *testing.T) {
	rows, err := summarizePlan(`{"query_block":{"table":{"table_name":"tweets","access_type":"range","key":"PRIMARY","rows_examined_per_scan":50,"attached_condition":"viewer 123 private SQL"}}}`)
	if err != nil || len(rows) != 1 || rows[0].EstimatedRows != 50 {
		t.Fatal(rows, err)
	}
	data, _ := json.Marshal(rows)
	if strings.Contains(string(data), "private") || strings.Contains(string(data), "123") {
		t.Fatal("plan leaked condition")
	}
}
