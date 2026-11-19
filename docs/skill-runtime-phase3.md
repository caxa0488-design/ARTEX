# 通用质量技能与受管运行时快照（第三批）

本批在已合并的知识目录、管理版本与人工复核之上，交付真正可发现的两个质量技能，以及实际 agent 装配/调用路径的 manifest 内容快照。没有导入利用工具、完整攻击技能集、旧数据库、配置或二进制；没有开启任何新技能、外部任务或模型调用。

## 实际引入内容和许可

| ARTEX 技能目录 | 固定来源 | 默认状态 |
| --- | --- | --- |
| `quality-evidence-hygiene` | Claude-BugHunter `skills/evidence-hygiene/SKILL.md` 的证据隐私/质量原则删减改编 | 未启用、无默认 agent 绑定 |
| `quality-evidence-report` | Claude-BugHunter `skills/report-writing/SKILL.md` 的事实优先/报告结构原则删减改编 | 未启用、无默认 agent 绑定 |

来源仓库 https://github.com/elementalsouls/Claude-BugHunter ，固定 commit `9bdb0a81658bc25f09c0b52d31fcde923d126e4f`，作者 Sachin Sharma。保留 CC BY 4.0 内容许可、署名、固定链接、原文与改编 SHA256、修改说明及不背书声明，详见 `third_party/bughunter/`。代码 MIT 与内容 CC BY 4.0 不能混为一谈；没有复制其引擎代码，ARTEX 代码维持原许可。这不是对任意第三方内容或商业组合的法律保证。

两个中文技能只整理已提供且有权使用的材料，输出草稿/人工检查项；不连接目标、不抓取链接、不执行测试、不自动改处置或覆盖状态、不外发。不含操作命令、攻击样例或原文某些不安全的隐私例外。真实 Norma `skill.LoadDir` 识别其标准 `name/description/license/compatibility` frontmatter；没有发明 `role` 语法，也没有加入 `builtinSkillVisibility`。

Drive 包按新增授权有界选读了一个普通 Markdown：`deploypkg/app/skills/chains-meta/SKILL.md`，2566 字节，成员 SHA256 `2a810a0f0ae55b458d6b0bbc5385ae8e5ef95d75d12b49171e9b392ad461c739`。已校验相对路径、普通文件、祖先非链接与 16 KiB 上限；仅 `extractfile` 读入独立审查目录，未展开程序、配置、数据库或执行指令。其正文无显式许可，归档清单也没有该技能局部许可文件，且是本批不纳入的操作性/外部资源索引。因此未复制、未绑定、未启用；未跟读引用内容。用户读取/集成授权不等于第三方版权许可。归档 SHA256 `1c3cd021f84bfcde65be9a7a57494da0d342707637f771c457279237422ca4e6`、683326279 字节及逐项状态记录在 `third_party/drive-review.json`。其他 Drive 技能不能据此声称已审查或融合。

## 管理与运行契约

1. 管理员先审查本地目录，在既有技能可见性界面决定是否启用；版本登记、绑定均不会替操作者启用/禁用技能。
2. `POST /api/managed-skills/{name}` 继续由服务器从真实安装内容计算既有 `tree-v1` hash，而非接受客户端 hash。管理目录仍可登记多文件版本，但本批运行绑定只接受单 `SKILL.md`、UTF-8、最多 128 KiB、目录名与 manifest name 一致，且无 MCP 和 `${SKILL_DIR}` 资源依赖。其余布局显式拒绝，不静默忽略。
3. `PUT /api/managed-skills/{name}/bindings/{key}` 使用真实 agent/version 和现有乐观 revision 检查；不改变权限或可见性。已有受管多文件绑定在装配时报明确错误，不能声称已支持完整资源包；应保留为登记信息或由管理员显式清除/改为受支持版本。
4. 真实装配在任何 MCP 连接/模型请求前读取受管内容，校验当前绑定、可见性、未撤销版本和真实 hash，将实际 manifest 字节持久化为 pin。`mainagent/planner/worker/chat` 四个入口均传播 `AssemblyError`，不是仅写一条日志后继续模型请求。
5. pin 的作用域是 task 或 chat session + agent + skill。唯一键固定同一上下文版本/hash/字节；重复装配幂等复用快照，数据库 trigger 拒绝 UPDATE。只要恢复仍使用该作用域，不能自动用新绑定版本替换旧内容。已知历史 `skill_usage found=true` 但尚无 pin 的上下文拒绝事后补写版本证明；建议为旧会话新建上下文，不把缺少旧审计当作从未加载的证明。
6. Norma 注册器从捕获的内存正文供给 `Skill`，不保留 live `Dir`。每次真实 Skill 调用前复查当前 DB 策略及当前文件 hash，成功且能持久化 loaded 记录才返回指导正文。源变更/缺失、撤销、同上下文解绑/换版/隐藏均拒绝，不静默回退。
7. 非受管技能保留既有发现和可见性行为。管理员显式清除版本绑定并不会关闭技能可见性：无旧 pin 的新上下文可按旧非受管方式加载；已有 pin 的上下文仍拒绝降级。界面对此警告。要完全停用还需关闭既有可见性；撤销版本不自动改其他可见性。

## 持久化与界面

迁移 `0010_managed_skill_runtime.sql` 只新增 pins/events/索引/保护函数和 trigger，不重写既有表或迁移。pin 引用真实 task、agent、version；快照不通过 HTTP 返回。任务/agent 删除的既有级联语义仍适用；这不是对特权数据库操作者的防篡改日志。

现有“Skill Versions & Bindings”页面接通任务 ID 查询：`GET /api/operator/tasks/{id}/skill-runtime`，沿用管理路由 JWT 与 `no-store`。只读白名单返回版本/摘要/来源/许可及最近 100 条 pin/event，不返回 manifest、配置或凭据。界面保留版本、绑定和撤销管理并解释运行时布局限制。

`assembled` 表示该单项版本已验证并写入 pin，不保证整体 agent 后续装配/模型启动成功；`loaded` 表示调用返回了技能指导，不证明模型执行、独立验证或本轮覆盖完成。两者绝不能等同 executed/confirmed。当前 UI 提供 task 视图；chat pin 有 DB 契约和测试，但没有新增 chat 管理列表。

## 离线验收与明确限制

- 测试真实 Norma 发现、真实 server agent 装配与 Skill.Call、真实 PostgreSQL 持久化/恢复、hash/正文/版本比对及 assembled/loaded；不调用真实模型或目标。
- 覆盖篡改、缺失、撤销、同 task 清空绑定/隐藏、非法包、MCP/resource 包、旧未版本化使用、chat 作用域、并发 pin 幂等、不可变 UPDATE、鉴权/API 不回传正文，以及非受管技能兼容。
- 前端契约校验事件语义、来源/许可/实际改编 hash 和默认关闭；类型检查及静态构建不等于浏览器 E2E。
- 这是 manifest-only 的真实内容快照，不是完整技能 artifact 市场、进程/网络沙箱或 per-attempt epoch。恢复仍要求当前来源、绑定与授权有效，来源离线/损坏时安全拒绝，未实现无源恢复。
- 不会撤回已经交给模型、历史记录或压缩记忆的文字；不声称撤销能中止进行中的模型调用或隔离它已知的内容。检查与并发策略变更存在通常的操作边界，不是跨模型交付的全局原子撤销。
- pin 在逐技能装配时写入；后续其他技能或 MCP 装配失败不会把已写单项记录伪装成完整运行成功。
- 只对新捕获的版本提供上述证明，不补造升级前所有历史。保留 task 级语义，不把跨重试/历史证据改称本轮已测。
- 技能正文也会进入数据库/备份，应当禁止放入任何秘密，沿用平台访问控制/保留策略；尚未新增 chat pin 自动清理策略。
- CVE/知识本地批量导入、完整资源包、调度器重写、远控与代理池均未纳入本批；没有自动合并或部署。
