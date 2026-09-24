package store

import (
	"encoding/json"
	"testing"

	"collabsheet/internal/workbook"
)

// TestWorkbookJSONRoundTrip verifies the exact shape persisted to the JSONB
// column survives marshal/unmarshal (the real PostgreSQL path wraps this same
// payload in a jsonb parameter).
func TestWorkbookJSONRoundTrip(t *testing.T) {
	wb := workbook.SeedSample()
	data, err := json.Marshal(wb)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got workbook.Workbook
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Sheets) != len(wb.Sheets) {
		t.Fatalf("sheet count = %d, want %d", len(got.Sheets), len(wb.Sheets))
	}
	budget := got.SheetByName("预算表")
	if budget == nil {
		t.Fatal("预算表 sheet missing after round trip")
	}
	c := budget.Get(3, 6) // D7
	if c == nil || c.Raw != "=SUM(D3:D6)" {
		t.Fatalf("D7 raw = %v, want =SUM(D3:D6)", c)
	}
	settings := got.SheetByName("设置")
	if settings == nil {
		t.Fatal("设置 sheet missing")
	}
	if s := settings.Get(1, 1); s == nil || s.Raw != "0.06" {
		t.Fatalf("设置!B2 = %v, want 0.06", s)
	}
}
