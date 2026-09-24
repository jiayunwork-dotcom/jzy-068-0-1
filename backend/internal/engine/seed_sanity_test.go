package engine

import "testing"

func TestSeedSanity(t *testing.T) {
	wb := SeedWorkbook()
	s := wb.Sheet("预算表")
	cases := []struct {
		col, row int
		want     string
	}{
		{4, 3, "48000"},   // E4 服务器小计
		{4, 5, "38400"},   // E6 云服务
		{4, 9, "165900"},  // E10 税前小计
		{4, 10, "0.06"},   // E11 税率 跨表
		{4, 11, "9954"},   // E12 税额
		{4, 12, "175854"}, // E13 合计
		{4, 14, "超标"},     // E15 状态
		{4, 19, "5"},      // E20 COUNT
	}
	for _, c := range cases {
		cell := s.cell(c.col, c.row)
		if cell == nil {
			t.Fatalf("cell %d,%d missing", c.col, c.row)
		}
		if cell.Display != c.want {
			t.Errorf("cell col=%d row=%d: got %q want %q (input=%s)",
				c.col, c.row, cell.Display, c.want, cell.Input)
		}
	}

	demo := wb.Sheet("查找演示")
	if got := demo.cell(6, 1).Display; got != "0.1" { // H2 VLOOKUP G1=B
		t.Errorf("vlookup = %q want 0.1", got)
	}
	if got := demo.cell(6, 2).Display; got != "2" {
		t.Errorf("match = %q want 2", got)
	}
	if got := demo.cell(6, 3).Display; got != "0.1" {
		t.Errorf("index = %q want 0.1", got)
	}
	if got := demo.cell(6, 4).Display; got != "大预算" {
		t.Errorf("nested cross-sheet = %q want 大预算", got)
	}
}
