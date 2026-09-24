# 多人实时协作电子表格（CollabSheet）

浏览器里的多人实时协作电子表格。前端 React 虚拟滚动网格 + 公式点选编辑 + 彩色协作光标；后端用
Go 1.22 / Gin / WebSocket 负责公式计算、依赖图、拓扑重算、循环引用检测、行列插删的引用平移，
协作状态与工作簿数据持久化到 PostgreSQL 16。

---

## 一键启动

需要 Docker（含 Compose v2）。在仓库根目录：

```bash
docker compose up --build
```

这会构建并启动三个组件：

| 服务 | 端口 | 说明 |
|------|------|------|
| `app`（前端 + 后端） | http://localhost:8080 | 打开即可看到预置的项目预算工作簿 |
| `db`（PostgreSQL 16） | 5432 | 工作簿与版本号持久化（带健康检查，app 等它就绪后再启动） |

镜像构建阶段会自动运行后端全部单元测试（拓扑重算、循环检测、行列插删引用平移、最后写入胜出、
跨用户撤销回退），任何一条不过构建即失败。

多开几个浏览器标签页（建议各自改一下右上角的名字）即可看到：

- 一个人改数字，其他人屏幕上公式结果实时联动；
- 每个人的选中格是带名字的彩色光标，正在编辑的格有虚线框；
- 两个人同时改同一格，后写者胜出，被覆盖方右下角弹出明确提示；
- 撤销/重做各自独立，互不影响。

### 不依赖 Docker 的本地开发

```bash
# 1. Postgres（自建实例或任意 PG16），设置连接串：
export DATABASE_DSN="postgres://user:pass@localhost:5432/db?sslmode=disable"

# 2. 后端
cd backend && go run ./cmd/server        # :8080

# 3. 前端（热更新，自动代理 /ws 到 :8080）
cd frontend && npm install && npm run dev # :5173
```

---

## 预置示例工作簿

任何人首次拉起服务都会看到同一份示例（之后所有编辑落库，重启后从数据库恢复、不会重新种子）：

- **预算表**：项目明细 `小计 = 数量*单价`，`SUM` 税前小计、`ROUND` 税额、合计，
  `IF(合计>上限,"超标","正常")` 条件状态，以及 AVERAGE/MIN/MAX/COUNT/ABS 汇总。
- **参数表**：税率、预算上限、币种；被预算表用**跨表绝对引用** `=参数表!$B$2`、
  `=参数表!$B$3` 引用。
- **查找演示**：`VLOOKUP`、`MATCH`、`INDEX`，以及嵌套的
  `=IF(SUM(参数表!$B$3)>100000,"大预算","小预算")`。

可以试着把参数表的预算上限改小/改大，预算表的「状态」格会立刻在「超标 / 正常」之间切换。

---

## 操作方式

- **选中**：单击单元格；方向键 / Tab 移动，Enter 进入编辑。
- **输入**：直接敲字符即开始编辑；以 `=` 开头按公式处理。
- **公式点选**：在公式编辑态（输入了 `=`）点击其它单元格，会把该格地址（如 `C7`）插到光标处，
  被引用的格高亮；可继续输入运算符、再点其它格，支持区间与嵌套。
- **提交 / 取消**：Enter 提交，Esc 取消，失焦提交。
- **行列插删**：顶部按钮，输入 1 开始的序号（在该行/列**之前**插入）。
- **撤销 / 重做**：顶部按钮或 Ctrl/⌘+Z、Ctrl/⌘+Y，**只作用于自己**。
- 删除内容：选中格后按 Delete/Backspace。

---

## 公式引擎

### 引用

| 类型 | 例子 |
|------|------|
| 相对引用 | `A1` |
| 绝对引用 | `$A$1` |
| 混合锚点 | `$A1`、`A$1`（列、行分别锚定） |
| 区间引用 | `A1:B10` |
| 跨工作表 | `参数表!$B$2`、`Sheet2!A1`（中文表名、`'含空格 名'!A1` 均可） |

### 函数

- 数学：`SUM AVERAGE MIN MAX COUNT ROUND ABS`
- 逻辑：`IF AND OR NOT`（`IF` 惰性求值，未命中的分支不会被计算）
- 查找：`VLOOKUP INDEX MATCH`（VLOOKUP/MATCH 支持精确与近似匹配）
- 运算符：`+ - * / ^ % &` 与 `= <> < <= > >=`，`^` 右结合，字符串用双引号、内部 `""` 转义。
- 支持任意深度嵌套，例如 `IF(SUM(A1:A10)>100,"超标","正常")`。
- 错误值：`#DIV/0! #VALUE! #REF! #N/A #NAME? #NUM! #CIRCULAR!`，按格传播。

### 三条硬约束如何保证

1. **拓扑重算，杜绝旧值。**
   每次写入先更新该格的依赖出边，再沿反向边求受影响子图，对子图做迭代版
   Tarjan 强连通分量（SCC）分解，然后在缩点 DAG 上 Kahn 拓扑排序——**被依赖者先算、
   依赖者后算**。求值时所有读都命中本次遍历已写回的新值（菱形依赖 A→B,C→D 也只会读到新值），
   不会递归触发计算，因此不可能“拿旧值算新值”。

2. **引用环当场识别、绝不栈溢出。**
   求值不是递归驱动的，而是先算 SCC：大小 > 1 的分量（直接环 A1↔B1、间接环 A1→B1→C1→…→A1）
   以及自环（`=A1+1`）一律标记为 `#CIRCULAR!`，完全不进入求值，因此没有无限递归；
   环外依赖该环的公式把 `#CIRCULAR!` 当普通错误传播。打破环（改掉其中一格）后立即恢复计算。

3. **行列插删自动平移引用。**
   结构编辑分两部分：物理移动/删除存储格；对**所有工作表**的每个公式 AST 调用
   `ShiftAST` 按各自锚点重写——相对锚点在插入点及其之后平移、在删除点被删则整引用变 `#REF!`，
   绝对锚点保持不动，跨表引用当手术发生在它指向的表时同样平移。之后重建依赖图并全量重算。
   例：在第 3 行前插一行，`=A3` → `=A4`，`=$A$3` 不动，区间 `A1:A4` → `A1:A5`；
   删掉第 3 行，`=A3+A4` → `=#REF!+A3`。

### 模块拆分（引擎没有巨型文件）

```
backend/internal/formula/
  value.go     值类型 / 类型强转 / 矩阵
  ref.go       引用表示与 A1 坐标编解码
  ast.go       AST、规范化打印、引用遍历
  lexer.go     词法分析（含 $ 锚点、中文表名、字符串转义、错误字面量）
  parser.go    递归下降语法分析（优先级、括号、函数参数）
  eval.go      求值器、运算符
  functions.go 内建函数（数学/逻辑/查找）
  deps.go      从 AST 提取依赖
  shift.go     行列插删的引用重写规则
backend/internal/engine/
  workbook.go  工作簿/表/格模型
  graph.go     跨表依赖图（正向 out + 反向 in）
  tarjan.go    迭代 Tarjan SCC（环检测）
  recalc.go    SCC + Kahn 拓扑重算
  surgery.go   行列插删 + 全表公式重写
  snapshot.go  快照/序列化/读取
  seed.go      预置示例工作簿
```

---

## 实时协作语义

- **广播**：一次提交在服务端串行落库 → 拓扑重算 → 把受影响的所有格（含别人的下游公式）
  一条 `changes` 广播给房间内所有人。
- **最后写入胜出（LWW）**：同一格的上一任作者若与本次作者不同且在线，会收到
  `overwritten` 通知（含胜出者名字、旧值、新值）；服务端不做合并。
- **按用户独立撤销/重做**：每个连接有自己私有的撤销栈，提交时压栈、清空自己的重做栈。
  撤销只是“以旧值再做一次普通引擎写入”，所以**所有下游（包括别人的公式格）都会随依赖图
  一起回退**，但这次回退不压入任何人的撤销栈——满足“甲撤销、乙的公式结果跟着回退、
  但该回退不进入乙的撤销栈”。若该格在你编辑之后又被别人改过，这条过期撤销会被安全跳过，
  不会覆盖更新的内容。
- **断线重连**：客户端 ID 持久化在浏览器。断线期间的编辑在本地排队，重连握手后推送，
  冲突仍走最后写入胜出；重连会重新拉取整份快照，把断线期间别人的改动无缝补齐，
  在线状态（光标/正在编辑）随 presence 广播恢复。
- **结构操作**（行列插删）是全房间共享事件，广播新维度与全量重算结果；这类改动牵涉所有人的格，
  不纳入单用户撤销栈，而是整体持久化。

---

## 自动化测试

### Go 单元测试（核心关系逐条锁定，`docker build` 时自动运行）

```bash
cd backend && go test ./...
```

- **拓扑重算 / 不用旧值**：`TestTopologicalRecalc`（断言 A1→B1→C1→D1 的重算先后顺序与最终值）、
  `TestNoStaleValueDiamond`（菱形依赖）、`TestDependentChainRollback`（间接下游回退）。
- **循环检测**：`TestDirectCycle`、`TestIndirectCycle`（四节点环）、`TestSelfCycle`，
  并验证环外格传播 `#CIRCULAR!`、打破环后恢复；全程不递归求值，因此不死循环。
- **行列插删引用平移**：`TestInsertRowShiftsRefs`、`TestInsertRowAbsoluteDoesNotMove`、
  `TestDeleteRowShiftsAndRefErrors`、`TestInsertColShiftsRefs`、`TestRangeShiftsOnInsert`、
  `TestRangeDeleteContraction`、`TestMixedAnchorShifts`（混合锚点）、
  `TestCrossSheetRefShiftsOnOtherSheetInsert`（跨表）、`TestFormulaAboveInsertionUntouched`。
- **最后写入胜出**：`TestLastWriterWins`——两客户端同改一格，后写者生效且另一方收到通知。
- **跨用户撤销回退**：`TestCrossUserUndoRollback`——甲改 A1、乙写 `=A1*2`，甲撤销后
  B1 随依赖回退，且乙的撤销栈深度不变；另含 `TestUndoRedoIsolation`、
  `TestUndoSkippedWhenCellMovedOn`、`TestReconnectResyncs`、`TestStructureBroadcast`。
- **公式语言**：`internal/formula/formula_test.go` 覆盖优先级/结合性、括号、字符串特殊字符、
  数学/逻辑/查找函数、惰性 IF、跨表与绝对引用、深度嵌套。

可用 `go test -race ./...` 跑竞态检测（已验证通过）。

### 端到端冒烟（可选）

对一个运行中的服务跑真实 WebSocket 双客户端剧本（种子值、广播、LWW 通知、跨用户撤销回退、
插行、重连快照）：

```bash
# 先启动服务（干净数据库）
go run -tags e2e ./cmd/e2e
```

期望最后输出 `E2E_OK`。

---

## 前端结构

```
frontend/src/
  state/types.ts        协议与视图类型、A1 地址工具
  state/store.ts        外部 store（useSyncExternalStore）：快照/变更/在线状态/通知
  state/ws.ts           WebSocket 连接、指数退避重连、离线编辑排队重发
  components/Grid.tsx               虚拟滚动网格（只渲染可视行 + 可视列）、键盘导航
  components/FormulaEditor.tsx      公式编辑、点选插地址、被引用格高亮
  components/CollaboratorCursors.tsx 他人彩色光标与“正在编辑”框
  App.tsx                          工具栏、工作表标签、插删行列、撤销重做、在线名单、冲突提示
```

网格默认 1000 行 × 26 列，DOM 中只保留滚动视口附近的单元格（行、列双向窗口化 + overscan），
表头与行号用 sticky 固定，因此上千行滚动顺滑。

---

## WebSocket 协议（摘要）

客户端 → 服务端：`hello / setCell / structure / cursor / editing / undo / redo / ping`
服务端 → 客户端：`snapshot / changes / structureDone / presence / overwritten /
undoState / ack / error / pong`，均为单个 JSON 信封（见 `backend/internal/collab/messages.go`）。

## 持久化

PostgreSQL 三张表：`workbooks(id, version)`、`sheets(workbook_id, sheet_index, name, rows, cols)`、
`cells(workbook_id, sheet_index, col, row, input)`。只存原始输入：依赖图与所有计算结果在加载时
重建（`rebuildGraph` + 全量拓扑重算）。单元格改动走 upsert，行列插删后整表重写并更新版本号。
