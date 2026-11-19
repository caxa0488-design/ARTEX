# 技能内容集成：可复核清单

本批是PR3之后的内容切片，不是全量攻击技能迁移。清单固定BugHunter commit `9bdb0a81658bc25f09c0b52d31fcde923d126e4f` 和Drive归档SHA256；逐项路径/字节数/审阅范围/许可依据/适配状态见 inventory.json。Git blob SHA1不是文件SHA256；未读取正文的条目不编SHA256，也不声称安全/可用。

统计：{"BugHunter_manifests": 83, "BugHunter_hunt_directory_names": 58, "Drive_manifests": 46, "Drive_chains_directory_names": 8, "BugHunter_selected_section_reviews_total": 4, "BugHunter_new_section_reviews": 2, "Drive_manifest_reviews_total": 3, "Drive_new_manifest_reviews": 2, "new_adapted_skills": 2, "new_original_skills": 1}

## 本批真正新增
- quality-scope-brief：BugHunter普通归属核对原则的CC BY4中文删改适配；从已有材料生成授权与归属分离简报，不查询/测试目标。
- quality-independent-review：BugHunter指定范围/证据质量章节的CC BY4删改适配；独立人工复核包，证据、反例、未知分栏，不自动确认/驳回。
- quality-task-handoff：ARTEX原创AGPL-3.0-only通用交接流程；已有任务/证据/权限/版本的交接与恢复前核对，不复制Drive文本或操作接口。

三个目录都是单一SKILL.md，无MCP或运行资源；默认关闭、无agent绑定，不导入包内role/enabled。管理员显式可见性及版本绑定沿用PR3界面；测试使用真实planner装配和Skill调用，不调用模型或目标。发现/加载技能不证明执行测试或覆盖完成。

## 审查边界
BugHunter本批仅阅读两份来源的指定普通章节，其他章节并未全面安全审计；前批两项质量改编保留。全库NOTICE说明代码MIT/内容CC BY4，但不据此为未读第三方内容作全面许可保证。完整许可、署名、固定来源与原/改编摘要见 third_party/bughunter；此批额外两项只采用选定通用原则，不搬操作样例或完整校验引擎。许可文本末尾空行曾规范化，条款完整。

Drive本批新增读 asset-correlation、passive-recon两个完整小manifest；加前批chains-meta共三项，不能代表其余43项已审查。逐成员先验证普通文件/安全相对路径/非链接祖先/16KiB上限，仅extractfile读Markdown。没有执行命令或跟读引用、配置、数据库、二进制。三个已读manifest均未确定再分发许可，且包含本批不导入的操作性内容/接口；均未复制、翻译或启用。不能推断其他技能不适合，也不能把用户集成授权当作第三方许可。原创handoff只取一般“已给资料需要来源/交接边界”需求，既非上述技能的正文适配，也不代表吸收其操作能力。

## 验收与限制
新增测试逐一证明原生解析/单资源完整、默认不可见、显式真实agent绑定、真实内容摘要一致、Skill指导加载及PG快照、变更/撤销拒绝。离线契约验证清单计数、状态、来源hash、许可和无缺失运行资源。未评价真实模型输出质量、未跑目标任务。保留PR3单manifest/任务级pin/当前源有效/无法撤回已交付指导等限制。无新增迁移、工具管理或自动执行链；不合并部署。
