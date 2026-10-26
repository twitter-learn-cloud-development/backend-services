# M8-R3 离线验收工具交付与现场待办

日期：2026-10-15。用户明确选择“本轮先完成验收工具与离线验证”。

| 范围 | 状态 | 当前事实 |
|---|---|---|
| v2部署身份/现场观察验证 | Implemented（本机离线验证通过） | 发布清单/source/环境快照/部署ID、逐检查live陈述和原始证据摘要、24小时新鲜度、路径/JSON/签名门禁；31项/24拒绝用例通过 |
| v1兼容与发布关闭 | Implemented（本机夹具通过） | 旧记录可按原计划核验，但全部Passed仍不能通过新release-ready门禁 |
| 31项用户旅程目录/CI接线 | Implemented（配置；真实CI未执行） | 原26项加认证、搜索可见性、Feed/私信、Schema owner、共享准入；保留数据删除关闭 |
| 真实发布、staging与31项现场观察 | Not run | 未提供/审核真实已发布清单、环境快照或现场原始证据；没有签发真实验收记录 |
| 部署接受 | Not accepted | `deployment_accepted=false`；离线夹具的published/live字段均是合成测试数据 |

完整工具使用、参数、兼容与回退见 `DEPLOYMENT_ACCEPTANCE_RUNBOOK.md`。最终命令与结果统一记录于 `PROJECT_PROGRESS.md` 和 `ARCHITECTURE_REMEDIATION_PLAN.md`。

后续代码与现场前置：

- 后续M4-R2代码与离线验证已完成（见MESSENGER_RECOVERY_RUNBOOK），真实跨设备/断线补拉/容量未Accepted；不补造本现场账本的通过记录。
- M2-R3完整容量仍Partial；既有小数据集Live SQL不能代表深历史/稀疏关注/多副本容量。
- 当前源码未形成干净的已审核发布版本；M8-R1清单门禁不会为当前开发工作区补造可信发布清单。由用户提交/审核后，从对应版本的受信任CI取得已发布清单。
- 环境/配置/Secret版本/Schema owner和前版清单要从目标环境读回并独立审核；镜像拉取、备份恢复、权限拒绝、告警接收及Provider/MCP/审批恢复各自需要真实材料。
- JWT仍无服务端会话撤销，用户数据删除仍关闭；现场旅程必须验证并公开这些限制。

M8-R3本轮没有启动依赖、访问staging、执行DDL/业务写入/恢复、调用付费Provider、发送告警或发布/部署。后续M4-R2离线交付不改变本轮真实验收未执行的事实；M8-R3现场与M2-R3完整容量保持独立待办。
