package workbook

// SeedSample returns a ready-to-use demo workbook: a project budget sheet
// with SUM totals, IF over-budget flags and cross-sheet references to a
// settings sheet (tax rate + budget cap) and a vendors sheet (VLOOKUP).
func SeedSample() *Workbook {
	wb := New()

	budget := NewSheet("预算表", DefaultRows)
	settings := NewSheet("设置", DefaultRows)
	vendors := NewSheet("供应商", DefaultRows)
	wb.Sheets = []*Sheet{budget, settings, vendors}

	set := func(sh *Sheet, addr, raw string) {
		c, r, err := ParseAddr(addr)
		if err != nil {
			panic(err)
		}
		sh.SetRaw(c, r, raw)
	}

	// ---- 设置 sheet: named parameters referenced cross-sheet ----
	set(settings, "A1", "参数")
	set(settings, "B1", "数值")
	set(settings, "A2", "税率")
	set(settings, "B2", "0.06")
	set(settings, "A3", "预算上限")
	set(settings, "B3", "20000")
	set(settings, "A4", "项目名称")
	set(settings, "B4", "一期建设")

	// ---- 供应商 sheet: lookup table ----
	set(vendors, "A1", "类别")
	set(vendors, "B1", "供应商")
	set(vendors, "C1", "单价")
	set(vendors, "A2", "服务器")
	set(vendors, "B2", "云栈科技")
	set(vendors, "C2", "12000")
	set(vendors, "A3", "域名")
	set(vendors, "B3", "西部数据")
	set(vendors, "C3", "200")
	set(vendors, "A4", "设计")
	set(vendors, "B4", "墨匠工作室")
	set(vendors, "C4", "3000")

	// ---- 预算表 ----
	set(budget, "A1", `="项目预算："&设置!B4`)
	set(budget, "A2", "项目")
	set(budget, "B2", "数量")
	set(budget, "C2", "单价")
	set(budget, "D2", "小计")
	set(budget, "E2", "供应商")

	rows := []struct {
		name  string
		qty   string
		price string
	}{
		{"服务器", "1", "12000"},
		{"域名", "2", "200"},
		{"设计", "1", "3000"},
		{"差旅", "3", "800"},
	}
	for i, item := range rows {
		row := 3 + i
		set(budget, Addr(0, row-1), item.name)
		set(budget, Addr(1, row-1), item.qty)
		set(budget, Addr(2, row-1), item.price)
		set(budget, Addr(3, row-1), "=B"+itoa(row)+"*C"+itoa(row))
		set(budget, Addr(4, row-1), "=VLOOKUP(A"+itoa(row)+",供应商!A2:C4,2,FALSE)")
	}

	set(budget, "A7", "合计")
	set(budget, "D7", "=SUM(D3:D6)")
	set(budget, "A8", "平均单项")
	set(budget, "D8", "=AVERAGE(D3:D6)")
	set(budget, "A9", "最大支出")
	set(budget, "D9", "=MAX(D3:D6)")
	set(budget, "A10", "项目数")
	set(budget, "D10", "=COUNT(D3:D6)")

	set(budget, "A12", "税额(含税)")
	set(budget, "D12", "=ROUND(D7*设置!B2,2)")
	set(budget, "A13", "含税总额")
	set(budget, "D13", "=D7+D12")
	set(budget, "A14", "预算判定")
	set(budget, "D14", `=IF(D13>设置!B3,"超标","正常")`)
	set(budget, "A15", "余量")
	set(budget, "D15", "=设置!B3-D13")
	set(budget, "A16", "供应商最高价")
	set(budget, "D16", "=MAX(供应商!C2:C4)")
	set(budget, "A17", "MATCH 示例：设计在第几行")
	set(budget, "D17", "=MATCH(\"设计\",供应商!A2:A4,0)")
	set(budget, "A18", "INDEX 示例：第 2 个供应商")
	set(budget, "D18", "=INDEX(供应商!B2:B4,2)")

	return wb
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
