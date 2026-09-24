package engine

// SeedWorkbook builds the demo workbook shown on first launch: a project
// budget with SUM subtotals, an IF status flag, ROUND tax math and a
// cross-sheet reference into a parameters sheet, plus a small lookup sheet
// exercising VLOOKUP / MATCH / INDEX.
func SeedWorkbook() *Workbook {
	wb := NewWorkbook("default")
	budget := wb.AddSheet("预算表", 1000, 26)
	params := wb.AddSheet("参数表", 1000, 26)
	demo := wb.AddSheet("查找演示", 1000, 26)

	// ---- 预算表: col A=项目 B=类别 C=数量 D=单价 E=小计 ---------------
	// Coordinates are 1-based to match the formula text literally.
	type cell struct {
		col, row int
		text     string
	}
	budgetCells := []cell{
		{1, 1, "项目预算表"},
		{1, 2, "项目"}, {2, 2, "类别"}, {3, 2, "数量"}, {4, 2, "单价"}, {5, 2, "小计"},

		{1, 4, "服务器"}, {2, 4, "硬件"}, {3, 4, "4"}, {4, 4, "12000"}, {5, 4, "=C4*D4"},
		{1, 5, "开发笔记本"}, {2, 5, "硬件"}, {3, 5, "6"}, {4, 5, "8500"}, {5, 5, "=C5*D5"},
		{1, 6, "云服务"}, {2, 6, "服务"}, {3, 6, "12"}, {4, 6, "3200"}, {5, 6, "=C6*D6"},
		{1, 7, "外包设计"}, {2, 7, "服务"}, {3, 7, "1"}, {4, 7, "15000"}, {5, 7, "=C7*D7"},
		{1, 8, "差旅"}, {2, 8, "其他"}, {3, 8, "3"}, {4, 8, "4500"}, {5, 8, "=C8*D8"},

		{1, 10, "税前小计"}, {5, 10, "=SUM(E4:E8)"},
		// tax rate comes from the parameters sheet (cross-sheet reference)
		{1, 11, "税率"}, {5, 11, "=参数表!$B$2"},
		{1, 12, "税额"}, {5, 12, "=ROUND(E10*E11,2)"},
		{1, 13, "预算合计"}, {5, 13, "=E10+E12"},
		{1, 14, "预算上限"}, {5, 14, "=参数表!$B$3"},
		{1, 15, "状态"}, {5, 15, `=IF(E13>E14,"超标","正常")`},
		{1, 17, "平均单价"}, {5, 17, "=AVERAGE(D4:D8)"},
		{1, 18, "最低单价"}, {5, 18, "=MIN(D4:D8)"},
		{1, 19, "最高单价"}, {5, 19, "=MAX(D4:D8)"},
		{1, 20, "条目数"}, {5, 20, "=COUNT(E4:E8)"},
		{1, 21, "数量基数"}, {3, 21, "5"},
		{1, 22, "数量差(绝对值)"}, {5, 22, "=ABS(C21-4)"},
	}
	for _, c := range budgetCells {
		wb.setInputLocked(budget, c.col-1, c.row-1, c.text)
	}

	// ---- 参数表: A=参数名 B=值 ----------------------------------------
	paramCells := []cell{
		{1, 1, "参数"}, {2, 1, "值"},
		{1, 2, "默认税率"}, {2, 2, "0.06"},
		{1, 3, "预算上限"}, {2, 3, "170000"},
		{1, 4, "币种"}, {2, 4, "CNY"},
	}
	for _, c := range paramCells {
		wb.setInputLocked(params, c.col-1, c.row-1, c.text)
	}

	// ---- 查找演示 -----------------------------------------------------
	demoCells := []cell{
		{1, 1, "等级"}, {2, 1, "折扣率"},
		{1, 2, "A"}, {2, 2, "0.15"},
		{1, 3, "B"}, {2, 3, "0.1"},
		{1, 4, "C"}, {2, 4, "0.05"},

		{6, 1, "查询等级"}, {7, 1, "B"},
		{6, 2, "VLOOKUP折扣"}, {7, 2, "=VLOOKUP(G1,A2:B4,2,FALSE)"},
		{6, 3, "MATCH位置"}, {7, 3, "=MATCH(G1,A2:A4,0)"},
		{6, 4, "INDEX取值"}, {7, 4, "=INDEX(B2:B4,2)"},
		{6, 5, "嵌套示例"}, {7, 5, `=IF(SUM(参数表!$B$3)>100000,"大预算","小预算")`},
	}
	for _, c := range demoCells {
		wb.setInputLocked(demo, c.col-1, c.row-1, c.text)
	}

	wb.rebuildGraph()
	wb.recalcAllLocked()
	return wb
}
