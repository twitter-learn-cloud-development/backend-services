# M8-R2 认证与剩余产品契约

状态：Implemented（代码与本机离线验证，2026-10-14）；真实部署/跨设备验收未 Accepted。没有新增存储或 Proto 字段。

| 功能模块 | 轮次/子项 | 开发及验收 |
|---|---|---|
| 认证 | M8-R2-A 语义与 HTTP | Logout 表示清除当前客户端；服务端回执明确 `scope=local`、`token_revoked=false`。请求全设备撤销返回 501；无效身份返回 401。密码修改须当前 JWT 和旧密码，身份仅从可信 Context 注入，5 秒下游超时；错误密码 403、参数错误 400、依赖失败 503/504。 |
| Web | M8-R2-B 会话一致性 | 成对保存登录响应中的 token/user，失败清理残留；退出、失效、换账号增加会话代次。迟到登录/旧会话 401 不能恢复或注销新会话。清除持久状态、Socket 与客服身份；加入密码修改和退出语义提示。 |
| Mobile | M8-R2-C 会话一致性 | 同样使用会话代次；串行持久化并等待退出清理，401 仅清当前请求所属会话。路由监听认证变化，Socket 停止重连；提供密码修改/退出入口，禁止认证请求日志泄漏。 |
| 产品账本 | M8-R2-D 差异与边界 | 列明两端现有功能、欠缺入口和真实验收；用户数据删除继续 `execution_supported=false`。会话撤销/全设备退出独立 Planned，不把本地清理描述为安全撤销。 |

JWT 当前为无状态 RS256，由 JWKS 验签，无 refresh token、session ID、密码版本或撤销查询。退出和密码修改均不使已签发 Token 提前失效；它们在自然过期前仍可被持有者使用，已建立的其他客户端连接也不会被强制断开。密码修改成功后当前客户端清除会话并要求重新登录；新登录验证新密码。网络超时不能证明密码未写入，禁止自动重试。

客户端退出无需等待网络成功，且不保存旧 token 重试服务端回执。HTTP Logout 是显式能力回执，不能代表已清除浏览器或移动存储。调用方本地清理独立执行；401 失效只清本地，不再次调用 Logout。

兼容与回滚：只追加 HTTP 路径，不修改已发布 Proto 字段。旧客户端本地退出行为继续有效；新客户端不依赖 Logout 网络成功。密码仍由 User/MySQL 所有，Auth 转发，Gateway 不访问业务数据库。没有新 Schema/迁移。回滚不应恢复敏感日志或旧会话竞态。

User Repository 的 GetByID 将 SQL 未找到转换为 `domain.ErrUserNotFound`；改密 Service 单独识别该领域错误，数据库故障继续作为内部失败，经 HTTP 返回 503，不伪报用户不存在。Mobile 对新密码路径在滚动升级遇到非 JSON 错误响应保留可理解的失败提示。

待后续设计的服务端撤销必须覆盖 HTTP、可选认证、WebSocket 重新鉴权、存量 Token 迁移、密码变更原子性、多副本/缓存故障与恢复；在这些条件落实前保持 `revocation_supported=false`。

## 交付与差异账本

下表只说明当前入口与本轮认证边界，不把现有页面视为真实部署验收。

| 功能模块 | Web 当前入口 | Mobile 当前入口 | 剩余工作/轮次 |
|---|---|---|---|
| 登录/注册/当前客户端退出 | Login/Register、NavBar；损坏缓存清理、会话代次、跨标签身份重载；换身份重建页面/客服 iframe，Axios/SSE 401 绑定代次 | Login/Register、首页账号安全；串行持久化、旧响应隔离、认证驱动 Router、Socket 重连取消 | M8-R3 真实 token 过期/多标签/断网/换账号旅程；OS/浏览器持久化不是事务存储，失败需提示/重试 |
| 密码修改 | `/account/security` | `/account/security` | 本轮补齐，错误旧密码保留会话，成功回登录；真实 MySQL、并发密码修改与超时不确定回执待验收 |
| 全设备退出/令牌撤销/找回密码 | 无可用入口；服务端 `all` 明确 501 | 无可用入口 | Planned，须单独设计会话/版本/存量 Token 迁移与所有鉴权入口；本轮明确关闭，不作已实现声明 |
| Feed/搜索/资料/关注 | Home/Explore/Profile/FollowList | Home/Search/Profile | 已有入口；M2-R3 规模与稀疏深历史仍 Partial。Mobile 没有 Web FollowList 对等路由，本轮不冒充跨端全覆盖 |
| 收藏列表 | Bookmarks 路由 | 无独立收藏列表路由 | Planned 产品差异，不能用按钮状态代替持久列表 |
| 私信/通知 | Messages/Notifications、Socket | ChatList/ChatRoom/Notification、Socket | 退出/换账号关闭Socket、Mobile通知换代；M4-R2已补双端历史/会话分页、ready/前台有界恢复和去重（离线验证）。真实断网/跨设备/容量仍待验收；无设备ACK/持久推送，JWT自然到期关闭不等于提前撤销 |
| Agent/Workflow/治理 | Agent、WorkflowEditor、ProfileCatalogAdmin、ExtensionMarketplaceAdmin | AgentChat、WorkflowList；无 DSL 编辑器及上述治理路由 | 已有入口能力有差异，保留独立功能账本；真实 Provider/MCP/审批恢复归 M8-R3，不宣称全功能等价 |
| 用户数据删除 | 预检/协议仍受能力目录约束 | 不新增执行入口 | 当前空能力目录和不支持资源使 `execution_supported=false`；全写路径围栏与故障验收前继续关闭 |

代码证据：`web/src/{utils/authSession.ts,stores/user.ts,utils/request.ts,api/agent.ts,views/AccountSecurity.vue,App.vue}`；`mobile/lib/{features/auth,core/router,core/network,core/utils/storage.dart}`。删除边界沿 `internal/module/agent/service/data_deletion_request.go` 的空 CapabilityRegistry 与 `executableDataDeletionProtocol` 门禁，本轮未更改。

本机最终验证：Go全仓Test、Auth/User/Gateway与domain相关Vet、Auth/User/Gateway Race、Web build与13项状态测试通过；Flutter analyze零问题、全套25项通过。夹具覆盖可信身份/请求限制/403/501/下游超时、真实bcrypt新旧密码/数据库故障分类、损坏缓存、迟到登录/刷新/401、相同JWT不同代次、已排队退出与新登录竞争、密码成功后持久状态清除和真实路由回登录、Socket断开后取消重连/处置后不建连、旧网关非JSON错误。Socket真实服务/跨设备撤销不在离线证据内。
