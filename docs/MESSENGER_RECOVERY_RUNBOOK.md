# M4-R2 私信跨端与实时恢复

日期：2026-10-15，Implemented（代码与本机离线验证）。真实服务/跨设备/容量未验收，M4整体仍Partial。

| 功能模块 | 轮次 | 开发计划与验收 |
|---|---|---|
| Messenger | M4-R2-A 推送边界 | MySQL入库后在统一2秒预算内尝试两方Redis发布，保留请求Trace、不生成无界goroutine；成功/无订阅/失败有低基数指标，发布失败不把入库成功变成可安全重发 |
| Gateway | M4-R2-B Socket生命周期 | 订阅ACK后ready，单writer/有界写/心跳/read上限/认证到期关闭、reader退出取消；去正文日志，避免double-close与goroutine泄漏 |
| Web | M4-R2-C 分页与同步 | 会话/消息历史显式续页、字符串ID排序/去重；ready/重连/重新前台及30秒前台刷新补拉，消息补拉最多3页/次并可继续；换会话/身份/卸载隔离旧响应，发送失败保留草稿且不自动重试 |
| Mobile | M4-R2-D 分页与同步 | 同样的消息补拉/连续游标与会话分页；Socket ready广播、前台恢复/显式重试，服务端HTTP和Socket消息按ID合并，不忽略本账号其他设备发送 |
| 契约与验证 | M4-R2-E | 明确“本页已读”不是展示确认/设备送达，现有RPC不变；状态/Widget、httptest/miniredis、相关Go Race/Vet、Web build/test、Flutter analyze/test |

架构：MySQL持久历史为事实，Redis Pub/Sub是可丢失提示，不是持久投递/离线收件箱。已有消息ID倒序接口可用来补拉：保存上次完整同步首页的最大ID为锚点，从cursor=0逐页回扫直到触达锚点或结束；每次最多3页，未完成保留同步cursor/锚点及“继续同步”，期间Socket的新ID不能移动旧锚点。旧历史cursor独立，恢复不跳过旧页。会话列表刷新首页使用新快照，清理旧分页链；不会用旧快照游标拼接新首页。

迁移/回退：无新表/Proto字段；先升级Gateway ready（旧客户端忽略未知事件），再更新客户端。新客户端也在transport ready/页面进入/前台周期补拉，兼容旧Gateway。既有索引需按M4-R1手工计划上线，不能由离线测试推断已执行。回退保留消息与索引，无卷删除。

已知限制：ID上界不等于事务提交序列，低ID迟到提交可能在已扫窗口之外；需后续提交序号/增量日志协议才能提供完整性保证。刷新/继续同步可重新读回；有限的历史读取不是永久送达保证。GET只将本页接收者消息标记已读，Socket到达和UI显示本身不确认已读；不提供实时已读回执/设备ACK。发送没有幂等键，超时/网络断开结果可能已提交；不自动重发，手工重发可能重复。需要强送达/安全重发时另轮设计Outbox/持久收件箱/消息幂等键/ACK与保留策略，不能靠PubSub或客户端去重宣称exactly-once。

## 实现与操作边界

Messenger在消息入库后保留Trace，在一个总计2秒的独立上下文内依次尝试接收者和发送者的提示发布；同一身份只发布一次。Redis失败或零订阅不改变已入库成功，日志不含正文、Token、频道或原始错误。HTTP发送体最多32 KiB，正文非空且UTF-8字节最多16 KiB；接收者非零。限制只约束新发送，不批量修改旧消息。

Gateway先等待Redis订阅ACK（最多3秒），升级后发送`{"type":"ready","history_sync_required":true}`。单writer以5秒写超时推送、每25秒Ping，60秒无Pong断开；读取上限4 KiB，提示超过64 KiB丢弃并计数。缓冲64条、向缓冲发送最多1秒，不能承诺提示不丢失。JWT到期关闭并要求重新认证，不提供提前撤销；客户端身份失效后须沿既有退出流程停止重连。reader退出取消订阅路径，handler返回关闭连接并等待reader，避免重复关闭done通道。

客户端历史按规范uint64十进制字符串比较，合并相同ID，已读标记只从false向true合并；不使用浮点ID。消息首页、续页、恢复补拉失败均保留已成功数据和可重试游标。Socket、本账号另一设备和HTTP发送响应共用合并路径。换会话/卸载丢弃迟到结果，身份变更沿M8-R2会话代次与路由销毁隔离；旧会话列表快照刷新后重新开始分页。每次恢复最多3页，长断线区间需按“继续同步”或等待下一次前台刷新；每页HTTP在原有超时内执行，总操作不是单个3页事务。

## 指标与告警

| 指标 | 固定outcome值 | 解释 |
|---|---|---|
| `messenger_realtime_publish_total` | `published/no_subscriber/error/unavailable/encode_error` | published只表示Redis有订阅者接受提示，非设备收到；离线无订阅为正常场景 |
| `gateway_realtime_events_total` | `connected/closed/subscribe_error/write_error/oversize` | 生命周期与推送故障次数，非在线设备数或端到端送达率 |

新增规则`deploy/prometheus/rules/twitter-clone-messenger.yml`沿现有rules目录通配加载、Messenger :2115和Gateway :2110抓取。`MessengerRealtimeHintsUnavailable`和`GatewayRealtimeDegraded`对10分钟窗口超过5次错误持续5分钟告警。规则文件静态核对不等于PromQL执行或告警已送达；真实Prometheus加载、阈值调优、Alertmanager接收需要现场证据。Redis内部重订阅不会发新的应用ready，前台周期补拉提供恢复机会。

## 本轮实际验证

使用现有依赖，没有更新vendor、npm或Flutter依赖。Go命令均显式设置`GOCACHE=<repo>/tmp/go-build-cache`，回环夹具与SDK缓存使用获准执行环境。

```powershell
go test ./internal/module/messenger/... ./internal/gateway/... -count=1
go vet ./internal/module/messenger/... ./internal/gateway/...
go test -race ./internal/module/messenger/... ./internal/gateway/... -count=1
go test ./cmd/gateway ./cmd/messenger-service -run '^$'
# web/
npm.cmd test
npm.cmd run build
# mobile/，使用现有Flutter 3.44.2 SDK
flutter analyze --no-pub
flutter test --no-pub
```

以上全部通过：Web18项（新增消息状态5项）、Flutter32项（新增消息状态5项、聊天Widget2项），Flutter分析零问题；Go覆盖Redis关闭后仍返回已存消息、共享发布预算/零订阅、精确ID、ready/提示/到期关闭与reader退出，目标Vet/Race及两入口编译通过。客户端覆盖大ID去重、页内已读合并、3页预算后继续、失败游标重试、处置后迟到结果、非法页拒绝；Mobile Widget覆盖ready补拉本账号另一设备消息、Socket重复合并与发送失败保留草稿。格式化和本轮空白检查通过。Web旧Browserslist数据提示不影响构建，未借此升级依赖。

## 现场验收待办

在独立授权的测试环境记录版本、数据集和固定消息ID：应用M4-R1索引前后EXPLAIN/耗时，双端连续翻页/深历史、后台返回、断网超过3页、Redis重启/无订阅/慢连接、双设备同账号发送、到期重认证、发送超时后的历史读回。核对无重复显示、续拉游标、旧响应隔离和页内已读边界；专门测试低ID延迟提交并记录现有局限。采集实际指标与告警接收，验证Gateway多副本与连接/缓冲/数据库容量。全部结果不得从miniredis、httptest或Widget夹具推定，当前没有真实服务写入、DDL、部署或验收签发。
