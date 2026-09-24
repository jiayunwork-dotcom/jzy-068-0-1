# 协作电子表格 CollabSheet

浏览器里的多人实时协作电子表格：React 虚拟滚动网格 + Go 1.22 / Gin / WebSocket 后端 +
PostgreSQL 16 持久化。核心是一个有依赖图、拓扑重算、循环引用检测和结构变更引用平移的
公式引擎。

## 一键启动

```bash
docker compose up --build
```

- 页面（Nginx 托管前端、反代 API 与 WebSocket）： http://localhost:3000
- 后端直连： http://localhost:8080 （`/` 同样托管前端，`/api/health` 健康检查）
- PostgreSQL： localhost:5432 （用户 `collab` / 库 `collabsheet`）

后端镜像构建时会自动运行全部 Go 自动化测试（拓扑重算、循环检测、行列插删引用调整、
最后写入胜出、跨用户撤销依赖回退）；任何一个不过，镜像都构建失败。

打开即见示例工作簿「预算表」：`SUM` 汇总、`AVERAGE/MAX/COUNT`、跨表取「设置」表税率与
预算上限的 `ROUND`/`IF` 判定，以及用「供应商」表做的 `VLOOKUP/INDEX/MATCH`。改任意单价或
数量，合计、税额、判定文字会立刻联动。

## 本地开发

```bash
# 后端（不设 DATABASE_DSN 时纯内存运行，方便开发）
cd backend
go run ./cmd/server                 # :8080

# 前端（Vite 热更新，自动代理 /ws 与 /api 到 8080）
cd frontend
npm install
npm run dev                         # :5173
```

接 PostgreSQL：

```bash
DATABASE_DSN="host=localhost port=5432 user=collab password=collabpw dbname=collabsheet sslmode=disable" \
  go run ./cmd/server
```

## 功能

- 真正可用的网格：默认 1000 行 × A–Z 26 列，点击/双击/直接键入进入编辑，回车提交并下移，
  Tab 横移，方向键导航，Delete 清空。
- 以 `=` 开头按公式处理；**编辑公式时点其它单元格会把该格地址插到光标处**；也可在顶部 fx
  公式栏编辑，两者共用同一份编辑文本。
- 只渲染可视区域 + 少量 overscan，滚动 1000 行不铺 DOM；表头/行头用 sticky 固定。
- 在线协作者的选中格显示为带其名字的彩色边框；右侧面板列出所有人与其位置，可改自己的名字。
- 行列插入/删除：内容搬运 + 全工作簿公式引用平移（相对引用移动、绝对引用不移动、
  跨表引用只随目标表移动、被删坐标变 `#REF!`）。
- 冲突：同格并发编辑按最后写入胜出，被覆盖一方收到明确的红色提示。
- 撤销/重做按用户各自独立（`Ctrl+Z` / `Ctrl+Shift+Z`）；甲撤销输入格时，引用它的乙的公式
  结果随依赖一起回退，但该公式格不会进入乙（或甲）的撤销栈。
- 断线自动指数退避重连：重连后用稳定的客户端 id 重新握手，服务端推全量快照补齐断线期间的
  改动，本地排队的编辑随后推上，仍走最后写入胜出。

### 公式

- 引用：相对 `A1`、绝对 `$A$1`（含 `$A1` / `A$1`）、区间 `A1:B10`、跨表 `设置!B3`、
  跨表区间 `供应商!A2:C4`。
- 数学：`SUM AVERAGE MIN MAX COUNT ROUND ABS`（另支持 `+ - * / ^ %`、括号、比较与 `&` 拼接）。
- 逻辑：`IF`（参数惰性求值，未选分支不会算）、`AND OR NOT`、`TRUE/FALSE`。
- 查找：`VLOOKUP`（精确/近似匹配）、`INDEX`、`MATCH`（0 / 1 / -1 三种匹配方式）。
- 任意嵌套，如 `=IF(SUM(A1:A10)>100,"超标","正常")`；字符串中的 `""` 转义、中文表名均支持。
- 错误值：`#DIV/0! #REF! #VALUE! #NAME? #N/A #CYCLE!`，会沿公式向上传播。

## 工程结构（按职责分包）

```
backend/
  cmd/server/            Gin 入口、WebSocket 升级、静态托管
  internal/formula/      公式内核（每类关注点独立成文件）
    lexer.go             词法
    parser.go            递归下降语法、运算符优先级、括号/跨表/区间
    ast.go               AST、引用遍历 MapRefs/WalkRefs、规范化打印
    eval.go              求值、运算符语义、矩阵
    functions.go         内置函数（数学/逻辑/查找）
    value.go             值类型与类型强转
    refs.go              行列插删的引用调整 Shift / AdjustReferences
  internal/engine/       依赖图 + 拓扑重算 + 循环检测
    engine.go            deps/rdeps 图维护、受影响集增量重算
    tarjan.go            迭代式 Tarjan SCC（拓扑序与成环判定合一，无栈溢出风险）
  internal/workbook/     文档模型、行列结构操作、示例工作簿
  internal/collab/       房间：编辑/冲突广播、每人独立撤销重做、在线状态、WS 适配
  internal/store/        PostgreSQL JSONB 快照持久化
frontend/src/
  components/
    VirtualGrid.jsx      虚拟滚动（只渲染可视区）
    Cell.jsx             单元格与编辑态（公式中点格插地址）
    FormulaBar.jsx       fx 公式栏
    PeerCursors.jsx      他人彩色光标
    PresencePanel.jsx    在线协作者
    Toolbar.jsx          工作表切换、插删行列、撤销重做、连接状态
  hooks/useCollabSheet.js  状态、WebSocket 自动重连与离线队列
  utils/                 地址换算、socket 封装
db/init.sql              持久化表结构（应用本身也会幂等建表）
docker-compose.yml       db + backend + frontend 一次构建拉起
```

### 三条硬约束是怎么落的

1. **拓扑重算**：每个公式格解析后登记 `deps`/反向 `rdeps`；改一格取其反向闭包，对闭包做
   Tarjan SCC，按「被依赖者先算」的分量顺序求值，依赖读到的一定是新值。
2. **循环引用**：SCC 多于一个节点（或单节点自环）即环，环内格标 `#CYCLE!`，环外依赖正常
   传播该错误；DFS 是迭代实现，不会递归到栈溢出或死循环。打破环后再次编辑会清掉陈旧标记。
3. **结构平移**：插删行列时先搬运本格内容，再对全工作簿每条公式经 `AdjustReferences`
   重写（绝对坐标不随插入移动、但坐标本身被删时变 `#REF!`；跨表引用仅随被改表移动）。

## 测试

```bash
cd backend && go test ./...
```

- `internal/engine/engine_test.go`：下游按拓扑序重算且不读旧值、直接环 / 长链间接环 / 自环
  检测、打破环后恢复、嵌套 IF+SUM。
- `internal/formula/refs_test.go`：插行/删行/插列/删列时相对、绝对、区间、跨表引用的平移规则。
- `internal/workbook/workbook_test.go`：结构操作后「内容位置 + 公式文本」一起正确变化。
- `internal/collab/room_test.go`：两客户端同格最后写入胜出且负者收到提示；甲撤销输入后乙的
  公式格结果随依赖回退、但不进乙的撤销栈；撤销重做互相独立。
- `internal/formula/functions_test.go`：优先级/结合性、字符串转义、全部内置函数、惰性 IF、
  除零与错误传播。
