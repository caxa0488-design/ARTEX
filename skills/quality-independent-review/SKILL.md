---
name: quality-independent-review
description: Prepare an independent human review packet from existing evidence and competing explanations; preserve unknowns and never change finding or coverage status automatically.
license: CC-BY-4.0
compatibility: ARTEX; manifest-only; explicit visibility; managed bindings opt into runtime snapshots
---
# 独立人工复核材料包

仅整理现有材料供独立复核，不执行验证、不请求新数据、不对外检索，也不自动修改发现/覆盖/评分。模型的同意不是独立验证，换一个模型也不自动满足独立性。

## 输入
需要发现ID和当前版本、任务/范围引用、原始证据引用、机器验证状态、人工处置历史、提出者理由与已知反例。不接收秘密原值；材料不足仍可以输出缺口包，不能伪造复核完成。

## 复核方法
1. 把待判断主张拆成事实、证据、解释和推测四栏。每项事实必须能回指已提供材料，引用存在不表示内容已验证。
2. 核对材料的目标/环境/时间与本任务范围是否一致。归属或许可不明确只标受阻，不能自行扩大授权或宣告发现无效。
3. 并列列出支持证据、反证、可替代解释和未解决矛盾。重复材料不是多份独立证据，历史相似结果不是本轮已验证。
4. 将人工处置候选/确认/驳回/无结论，与机器验证及未测/受阻/失败/完成的覆盖记录分别展示，不用任何一栏覆盖其他栏。
5. 证据不足时建议无结论或补充材料，不用置信分、固定严重度或硬性淘汰清单替代复核。即使建议驳回，也保留原主张和理由的历史。
6. 标注复核者与提出者是否独立、是否只重述同一模型输出；未知时如实写未知。最终决定只由平台授权的人工流程提交，本技能只输出建议。

## 输出契约：review_packet/v1
输出主张-证据矩阵、相反解释、范围/时效检查、独立性说明、建议处置及理由、待补材料。每条建议引用当前发现版本与证据ID；记录可能失效的版本，不编造摘要或复现成功。明确本次未执行独立验证、未修改任何状态、未发布报告。

## 来源与改编
改编自 Sachin Sharma / Claude-BugHunter 的 triage-validation 中范围核对与证据支撑结论的普通质量原则：
https://github.com/elementalsouls/Claude-BugHunter/blob/9bdb0a81658bc25f09c0b52d31fcde923d126e4f/skills/triage-validation/SKILL.md
内容许可 CC BY 4.0：https://creativecommons.org/licenses/by/4.0/ 。完整许可见 third_party/bughunter。只审阅和改编指定普通章节，未导入完整引擎或七问/攻击链；中文重写，不采用操作示例、外部投稿、自动淘汰、定时承诺或严重度捷径；补充无结论和人工/机器状态隔离。原作者不背书。
