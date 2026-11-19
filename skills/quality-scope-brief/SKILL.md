---
name: quality-scope-brief
description: Prepare a non-executing authorization and ownership brief from provided records; do not infer scope from names, shared infrastructure or reachability.
license: CC-BY-4.0
compatibility: ARTEX; manifest-only; explicit visibility; managed bindings opt into runtime snapshots
---
# 授权范围与归属简报

只处理已经提供的授权和资产记录；不访问目标、解析名称、查询外部数据源或执行命令。品牌匹配、技术关联、工具可达与获得授权是四回事。本技能不生成测试步骤或选择攻击目标。

## 输入契约
需要项目/任务ID、授权文件引用及版本、有效时段、允许的主体/协议/端口、排除项、已有资产记录和来源时间。身份只记引用与权限类别，不接收密钥。缺少授权版本或归属依据时写未知，不补造事实。

## 处理方式
1. 按已提供的稳定资产ID整理别名，不把同名组织、相近域名、共享证书或共享地址段自动合并为同一主体。
2. 分别记录归属依据与授权依据：确属某主体仍不代表获准；获准的范围也不能自动扩展至其供应商或其他环境。
3. 为每项输入保留原始引用、声明人/来源、记录时间、适用环境与矛盾。仅据现有材料标为明确包含、明确排除或等待人工确认；它们是简报分类，不是平台授权结果。
4. 将动态地址变化、重定向和第三方依赖列为重评估条件，不自动跟随或扩大范围。过期材料不能作为当前许可。
5. 提交给有权操作者复核：允许内容、禁止内容、未知项、时段与预算分别可见。任何歧义保持受阻，不将人工讨论或模型意见升级为授权。

## 输出契约：scope_brief/v1
输出授权摘要、资产归属对照、范围矩阵、冲突/缺口、待人工确认五部分。矩阵逐项包括资产ID、归属引用、授权引用与版本、包含/排除/未知、有效时段、依赖与理由。无证据字段用未知；最后说明本次未执行测试，简报不授予权限、不更新scope或资产图。

## 来源与改编
改编自 Sachin Sharma / Claude-BugHunter 的 recon-scope-triage 中归属需证据、同名不等于同主体的原则，仅采用非操作性部分：
https://github.com/elementalsouls/Claude-BugHunter/blob/9bdb0a81658bc25f09c0b52d31fcde923d126e4f/skills/recon-scope-triage/SKILL.md
内容许可 CC BY 4.0：https://creativecommons.org/licenses/by/4.0/ 。完整许可与修改记录见 third_party/bughunter。中文重写并扩展授权/归属分离；不采用任何主动检查、外部搜索、敏感数据源或操作流程。原作者不为本适配背书。
