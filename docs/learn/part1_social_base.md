# Part 1: 社交系统核心微服务拆解 (Day 1 - Day 10)

---

## 🗓️ Day 1: BFF API Gateway 网关路由转发与统一 JWT 鉴权隔离

### 🎯 学习目标与安排
- **文件范围**：[gateway/main.go](file:///e:/GOProject/云原生/twitter-clone/cmd/gateway/main.go) (入口), [router.go](file:///e:/GOProject/云原生/twitter-clone/internal/gateway/router/router.go) (路由映射), [auth.go](file:///e:/GOProject/云原生/twitter-clone/internal/gateway/middleware/auth.go) (中间件)
- **核心目标**：理解 API 网关作为 BFF (Backend For Frontend) 的转发机制与统一的安全防御边界。

### 🛣️ 请求链路全景 (Request Flow)
1. 客户端发送携带 Bearer Token 的 HTTP 请求至 `gateway` (:9638)。
2. 网关中间件拦截：CORS 跨域过滤 ➔ RateLimit 校验 ➔ JWT 鉴权解析。
3. `auth.go` 验证 Signature 签名，解析出 `user_id` 和 `username`。
4. 校验通过：将解析出的用户上下文写入 Gin 的 `Context`。
5. 路由路由：根据服务路径转发至各 gRPC handler（如 `UserClient`, `TweetClient`），并将用户 ID 传导至 gRPC 请求上下文中。

### 🔑 核心重点 (Key Focus)
- **JWT 签名防伪**：理解 JWT 生成和强校验（HS256）。
- **进程安全防死锁**：分析中间件对连接耗尽、Panic 捕获与恢复的设计，防止单个接口挂死导致整个网关进程僵死。

### ✨ 技术亮点 (Architecture Highlights)
- **纯 BFF 代理转型**：网关完全去数据库化，下架所有 GORM 数据库连接，退化为无状态的高速 gRPC 代理转发层，吞吐量提升显著。

### ⚠️ 避坑指南 (Gotchas)
- **Token 失效假通过**：如果 JWT 解析函数在遇到过期错误时只是打印 Log 而没有调用 `c.AbortWithStatusJSON(401)` 提前阻断，后面的 Handler 依然会被调用，造成越权执行。

### 🚀 进阶玩法 (Advanced Play)
- **基于 JWKS 的动态验签**：在微服务多网关集群下，引入 JWKS 规范，由统一认证中心下发公钥，所有网关节点异步拉取并缓存，达成零共享秘钥的安全验签。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：故意在网关 `auth.go` 中将解析 Token 得到的 `claims.UserID` 改为硬编码的假 ID，重启网关访问 `/api/v1/users/me` 观察获取的用户档案是否发生错位。

---

## 🗓️ Day 2: User Service 用户注册登录、密码哈希与雪花 ID 精度纠偏

### 🎯 学习目标与安排
- **文件范围**：[user_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/user/service/user_service.go) (业务实现), [user_repository.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/user/repository/user_repository.go) (持久化), [snowflake.go](file:///e:/GOProject/云原生/twitter-clone/pkg/snowflake/snowflake.go) (ID生成)
- **核心目标**：彻底理清用户加密安全和 Snowflake 64位大整数在 JavaScript 中的“精度溢出截断”。

### 🛣️ 请求链路全景 (Request Flow)
1. 网关接收到注册请求，调用 `UserService.Register`。
2. 业务端检验：用户名是否重复，生成 Snowflake 用户 ID。
3. 密码安全加固：使用 Bcrypt 高强度哈希算法（带 Salt）对原始密码加密，并写入 MySQL。
4. 登录验证：提取密文密码，调用 `bcrypt.CompareHashAndPassword` 执行哈希比对。
5. ID 传输与纠偏：网关发现 uint64 ID 过大（> 2^53 - 1），序列化时转换为 String 格式返回前端，确保前端 JS 能完整读取。

### 🔑 核心重点 (Key Focus)
- **密码哈希防破解**：吃透为什么不能用 MD5/SHA256 保存密码（易遭彩虹表攻击），而必须使用带动态随机 Salt 且计算密集的 Bcrypt。
- **雪花 ID 的唯一性**：看雪花 ID 中时间戳、工作机器 ID 和序列号的位分配，防止高并发下 ID 冲突。

### ✨ 技术亮点 (Architecture Highlights)
- **JS 精度泄露防御 (Snowflake Stringifying)**：网关通过类型拦截器在 JSON 序列化阶段将 19 位的雪花 ID 自动转换为 String 格式输出，完美规避了前端 JavaScript 反序列化时对大整数的精度丢失和截断。

### ⚠️ 避坑指南 (Gotchas)
- **Snowflake 机器 ID 重复**：在 K8s 容器环境下，如果多个服务的 Pod 共享相同的主机名且未对 Worker ID 进行动态漂移计算，会导致两台不同的机器分发到相同的 Worker ID，引发严重的 ID 生成冲突冲突。

### 🚀 进阶玩法 (Advanced Play)
- **分布式自增 ID 漂移防回拨**：在雪花算法上增加“时钟回拨自愈”机制，检测到系统时间发生回拨时，自动使用缓存的序列号在微秒内继续自增，或者拒绝服务 1 秒等待时钟追齐。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：暂时关停网关层的 uint64 转 String 格式转换，发送注册请求并观察浏览器 Network 里的 ID 和本地数据库的真实的 ID 是否出现了最后几位不一致（精度丢失）。

---

## 🗓️ Day 3: User Service 个人资料卡更新、头像媒体文件上传与安全边界

### 🎯 学习目标与安排
- **文件范围**：[user_handler.go](file:///e:/GOProject/云原生/twitter-clone/internal/gateway/handler/user_handler.go) (上传端), [user_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/user/service/user_service.go) (字段维护)
- **核心目标**：掌握 Multipart 文件上传的流量拦截、高弹性校验和 Web 安全防御边界。

### 🛣️ 请求链路全景 (Request Flow)
1. 客户端通过 `multipart/form-data` 发送头像图片到 `/api/v1/users/upload_avatar`。
2. 网关拦截：提取文件扩展名、大小限制。
3. 磁盘写入：把文件重命名为 UUID.ext 写入网关 `/app/uploads` 目录。
4. RPC 记录：调用 `UserService` 更新对应用户的 `avatar` 地址。
5. 前端展示：静态文件目录挂载至 Gin，前端可通过 `/uploads/xxx.jpg` 直接读取。

### 🔑 核心重点 (Key Focus)
- **文件上传漏洞防范**：深刻理解为什么绝对不能以用户原名保存文件（防止被上传 webshell），必须随机重命名。
- **并发写盘**：在高并发上传时，防范零碎小文件直接占满系统磁盘 inode 空间或引起句柄耗尽。

### ✨ 技术亮点 (Architecture Highlights)
- **严格的安全沙箱**：头像/简介更新服务具有独立的 GORM 字段强隔离防写保护，防止大模型或者前端非法传入 `role` / `is_admin` 等危险字段越权篡改用户角色权限。

### ⚠️ 避坑指南 (Gotchas)
- **多参数更新顺序错位**：在调用 Service 接口更新资料时，务必仔细检查函数入参的顺序。如 `UpdateProfile(ctx, userID, avatar, bio)` 与 `UpdateProfile(ctx, userID, bio, avatar)` 如果由于拼写发生顺序倒置，会导致头像内容写入简介，简介写入头像链接。

### 🚀 进阶玩法 (Advanced Play)
- **对象存储 (OSS/S3) 直传加签 (Presigned URL)**：生产环境下，网关不承载大文件的文件流。由网关向 OSS 申请带有时效的临时写入凭证（加签 URL），前端直传文件到 OSS，完成后回调网关写入 URL 地址，极大节省网关网络带宽。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：尝试编写脚本上传一个包含恶意的 `.php` 后缀文件，验证网关是否成功执行文件后缀名黑名单拦截（只允许 `.jpg`, `.png`, `.jpeg`）。

---

## 🗓️ Day 4: Tweet Service 推文极速发布、富文本解析与事件解耦

### 🎯 学习目标与安排
- **文件范围**：[tweet_service_mq.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/tweet/service/tweet_service_mq.go) (写链路), [rabbitmq.go](file:///e:/GOProject/云原生/twitter-clone/pkg/rabbitmq/rabbitmq.go) (通信底座)
- **核心目标**：吃透基于消息队列（RabbitMQ）的“事件解耦写扩散”流程。

### 🛣️ 请求链路全景 (Request Flow)
1. 客户端发起发帖请求 `/api/v1/tweets`。
2. `tweet-service` 处理：利用雪花算法生成推文 ID，写入本地 MySQL 的 `tweets` 表中。
3. 数据库事务提交：若成功，立即向 RabbitMQ 的 `exchange.tweet.fanout` 交换机投递一条发帖消息。
4. 异步扇出消费：多个异步消费者（如 TimelineConsumer 扇出推入粉丝 ZSet、OutboxWorker 同步向量、Notification 收集 @/话题等）并发捕获此事件，业务链路彻底解耦。

### 🔑 核心重点 (Key Focus)
- **事务一致性边界**：确保发布消息（Publish）绝对不被包在 MySQL 的局部本地事务块内，因为如果事务回滚但消息已被发往队列，会导致数据静默不一致。
- **RabbitMQ 生产者投递确认 (Confirm)**：学习如何确认消息已被 MQ 物理接收。

### ✨ 技术亮点 (Architecture Highlights)
- **全站写异步扇出机制**：发帖主干操作为纯粹的单 SQL 写入，发帖时延降到 15ms 内。所有的二级扩散和向量化对账全在 MQ 消费侧解决，抗并发发帖性能出色。

### ⚠️ 避坑指南 (Gotchas)
- **消息生产无限重试假死**：如果在 MQ 连不上时，生产者采取无限制死循环同步等待，会导致发推主线程直接挂死。必须设置最大尝试次数和失败降级策略。

### 🚀 进阶玩法 (Advanced Play)
- **发件箱表事务保障 (Transactional Outbox Patterns)**：即使 RabbitMQ 重启，也能通过在本地库里的 `outbox_tasks` 任务对账恢复积压消息，确保消息“有且仅有一次”投递。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：在本地发帖，查看 RabbitMQ 管理控制台（:15672）中的 Message 轨迹，截图确认交换机是如何将该发帖事件异步路由投递给多个活跃消费队列。

---

## 🗓️ Day 5: Tweet Service 评论系统架构设计（二级树形评论与总数统计）

### 🎯 学习目标与安排
- **文件范围**：[comment_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/tweet/service/comment_service.go) (评论逻辑), [comment_repository.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/tweet/repository/comment_repository.go) (树形查询)
- **核心目标**：掌握社交网络中多级评论树的存储设计与高吞吐计数器的并发更新。

### 🛣️ 请求链路全景 (Request Flow)
1. 用户对某推文发表评论（一级评论：`parent_id=0`；二级回复：`parent_id=comment_id`）。
2. `tweet-service` 将评论实体存入 `comments` 表。
3. 计数器并发增量：在同一个数据库连接中，异步启动或同步调用 `tweetRepo.IncrementCommentCount(tweetID)`，对推文的主评论计数字段执行原子累加。
4. 拉取推文：按发表时间/点赞热度，扁平分页提取 parent_id=0 的评论，并递归带出前几条最热的子评论。

### 🔑 核心重点 (Key Focus)
- **无限级与二级评论的权衡**：看表结构中 `parent_id` 及其索引设计，分析为什么大型社交平台在前端展现上往往强行收缩为二级嵌套，以规避递归 SQL。
- **分布式计数并发冲突**：在高并发热门推文下，大量评论同时写入，如何防范 MySQL Row Lock (行锁) 抢占导致的事务堆积。

### ✨ 技术亮点 (Architecture Highlights)
- **原子增量防错漏**：更新评论计数时，使用底层的 `UPDATE tweets SET comment_count = comment_count + 1 WHERE id = ?` 而非先 Read 修改后再 Write，彻底杜绝了并发读写下的数据脏覆盖。

### ⚠️ 避坑指南 (Gotchas)
- **评论作者数据未知 (Unknown)**：在将评论数据返回给网关层时，如果只返回了 `user_id` 而未在 gateway 层批量拉取 `user-service` 补充用户名/头像，前端评论列表会全部显示为 "unknown @unknown"。

### 🚀 进阶玩法 (Advanced Play)
- **Redis Cache-Aside 计数分流**：计数全部在 Redis 内存通过 `INCRBY` 执行，并在空闲期通过 Pipeline 批量定时刷回（Flush）MySQL 数据库，消除 MySQL 单行热点锁死瓶颈。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：针对推文详情页，删除一条子评论，检查主推文的 `comment_count` 字段是否原子扣减，且对应的二级评论是否被级联逻辑妥善处理。

---

## 🗓️ Day 6: Like System 高并发点赞（Redis 读写缓存与 RabbitMQ 异步落盘）

### 🎯 学习目标与安排
- **文件范围**：[like_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/tweet/service/like_service.go) (缓存判定), [like_repository.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/tweet/repository/like_repository.go) (落盘补偿)
- **核心目标**：吃透典型的“点赞/互动”行为的超高性能设计（Cache-Aside 写缓存 + 消息队列异步刷盘）。

### 🛣️ 请求链路全景 (Request Flow)
1. 客户端点赞某推文：`POST /api/v1/tweets/:id/like`。
2. `tweet-service` 直接在 Redis 内存中记录点赞状态（`SADD tweet:likes:tweetID userID`）。
3. 累加点赞数：在 Redis 中执行 `INCR tweet:like_count:tweetID`。
4. 网关当即收到 200 OK 成功响应，耗时仅 2ms。
5. 异步同步落盘：同时向 RabbitMQ 发送 `LikeEvent` 消息，由消费进程捕获后批量以批量更新或慢更新方式安全刷入 MySQL，并保存 `likes` 关系表，达成最终一致性。

### 🔑 核心重点 (Key Focus)
- **双写一致性**：解析如果 Redis 写入成功但 MQ 发送失败，如何通过“异常自愈对账”或者以 Redis 数据为准进行对齐。
- **幂等防护**：高频点击点赞/取消点赞时，确保后台 MQ 消费者通过 `SnowflakeID` 或主键校验防止出现脏数据冲突。

### ✨ 技术亮点 (Architecture Highlights)
- **Redis 原子性去重**：通过 Redis 的 Set 数据结构实现单用户对单帖子只能点赞一次的防重复逻辑。写缓存 + 异步刷盘使点赞抗压能力提升多个数量级。

### ⚠️ 避坑指南 (Gotchas)
- **点赞状态刷新后红心丢失**：网关在 Enrich 推文信息时，如果由于缺少用户 ID 参数而没有去 Likes 表中判断 `is_liked` 状态，页面刷新后点赞的红心会瞬间退回灰心状态。

### 🚀 进阶玩法 (Advanced Play)
- **布隆过滤器 (Bloom Filter) 防空穿透**：点赞量极大的高并发流量下，通过布隆过滤器在内存中提前拦截“根本不存在的推文 ID”的点赞请求，完全释放 Redis 不必要的 Key 申请压力。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：对同一条推文高频点击“点赞-取消-点赞”，检查后台 Redis 集合的状态和 MySQL 点赞表的变化，验证在极速点击下点赞计数依然正确不发生脑裂。

---

## 🗓️ Day 7: Bookmark System 书签系统与 Retweet 转发多态表映射

### 🎯 学习目标与安排
- **文件范围**：[bookmark_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/tweet/service/bookmark_service.go) (收藏), [retweet_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/tweet/service/retweet_service.go) (多态转发实现)
- **核心目标**：掌握多功能关联表结构的设计、转发推文（Retweet）与原推文（Original Tweet）在多态查询下的映射逻辑。

### 🛣️ 请求链路全景 (Request Flow)
1. 客户端发起转推动作 `/api/v1/tweets/:id/retweet`。
2. 表单多态设计：`retweets` 表记录 `user_id`（谁转的）与 `original_tweet_id`（原推ID），同时在主 `tweets` 表写入一条特殊属性行。
3. 混合查询拉取：当拉取用户的 Timeline 时，系统并行拉取真实推文和转发推文关联。
4. 递归回表 Enrich：如果是转帖类型，将原推 ID 递归传入 `tweetClient.GetTweet` 查获原贴内容，在 Gateway 层合并格式为 `{ type: "retweet", original_tweet: {...} }` 并输出。

### 🔑 核心重点 (Key Focus)
- **多态实体表映射 (Polymorphic Mapping)**：理解原推被物理删除时，如何通过级联删除或在代码层返回“该推文已被作者删除”保证页面不会 Panic 崩溃。
- **无 SQL 拼接的高性能合并**：避免在 BFF 层循环内发起嵌套 SQL 查询（导致著名的 N+1 SQL 灾难）。

### ✨ 技术亮点 (Architecture Highlights)
- **Gateway BFF 聚合器设计**：转推的底层装配和原作者资料填充全部在 API Gateway 层内存批量聚合并填充，`tweet-service` 本身依然只负责极简的实体存储，完美契合微服务强数据隔离架构规范。

### ⚠️ 避坑指南 (Gotchas)
- **雪花 ID 机器节点未初始化 (Panic)**：书签或转推在创建时，如果调用了全局的 `snowflake.GenerateID()`，但系统启动入口（如 Gateway main.go）未对其进行 WorkerNode 节点注册初始化，会直接导致 node 空指针 Panic 异常重启。

### 🚀 进阶玩法 (Advanced Play)
- **转推链降维 (Retweet Chain Flattening)**：如果 A 转发了 B 转发的 C，展现时强行降维为 A 转发 C，在底层查询时省去多级指针的深度递归遍历。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：发布一条推文，由另一个用户执行转发。随后将原推文删除。
- **验证**：检查转发者的个人时间线，验证原帖被删除后，转贴信息是否能优雅降级展示“此内容已被原作者删除”，且整页 feeds 加载无抛错。

---

## 🗓️ Day 8: Follow Service 关注系统底层的社交图谱关系与粉丝列表设计

### 🎯 学习目标与安排
- **文件范围**：[follow_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/follow/service/follow_service.go) (关系逻辑), [follow_repository.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/follow/repository/follow_repository.go) (双向索引持久化)
- **核心目标**：理解关注和粉丝表的高并发查询与分布式缓存对账机制。

### 🛣️ 请求链路全景 (Request Flow)
1. 用户 A 关注用户 B：`POST /api/v1/follows/:id`。
2. 事务更新：在 `follows` 关系表中插入记录 `(follower_id=A, followee_id=B)`。
3. Redis 双向累加：在 Redis 缓存中，更新 A 的关注列表 ZSet（`user:following:A`）以及 B 的粉丝列表 ZSet（`user:followers:B`）。
4. 更新计数器：原子增加 A 的关注数和 B 的粉丝数。
5. 事件异步清理：通过 Redis Pub/Sub 或队列异步刷新 A 的 Timeline 缓存。

### 🔑 核心重点 (Key Focus)
- **双向关系高并发读**：看底层索引对 `follower_id` 和 `followee_id` 的 B-Tree 分配，防范粉丝列表分页查询时的全表扫描。
- **缓存最终一致性**：了解如何在分布式锁及后台守护协程中执行 `SyncGlobalCelebrities`，防止 Redis 中的关注状态与物理数据库发生偏差。

### ✨ 技术亮点 (Architecture Highlights)
- **高并发图关系优化**：将原本极其繁重的社交图谱多表 JOIN 查询（A是否关注了B，B是否关注了C等）全数在 Redis ZSet 内存中实现，一维查获，保障了高频对话时权限过滤的毫秒级拦截。

### ⚠️ 避坑指南 (Gotchas)
- **关注接口大整数精度截断**：由于关注接口参数可能传入 uint64 雪花 ID，如果前端发送非字符串格式而服务端也未作转换，会导致 ID 丢失精度。必须让入参全部以 String 传入并通过 `strconv.ParseUint` 恢复。

### 🚀 进阶玩法 (Advanced Play)
- **社交二度人脉计算 (Friend of Friend)**：利用 Redis 的 `SINTER`（交集）或 `SUNION`（并集）功能，在内存中极速计算出“共同关注”以及“你可能认识的人”。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：用户 A 关注用户 B，检查 Redis 中 `user:following:A` 与 `user:followers:B` 是否同步新增对方，且二者的关注/粉丝统计数原子自增。

---

## 🗓️ Day 9: Notification System 实时互动通知网关设计（MQ + WebSocket）

### 🎯 学习目标与安排
- **文件范围**：[notification_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/notification/service/notification_service.go) (分发侧), [notification_handler.go](file:///e:/GOProject/云原生/twitter-clone/internal/gateway/handler/notification_handler.go) (WebSocket 连接管理)
- **核心目标**：吃透分布式实时消息分发网格以及 WebSocket 链接的并发安全生命周期管理。

### 🛣️ 请求链路全景 (Request Flow)
1. 某用户给作者点赞，触发发帖队列事件广播。
2. `notification-service` 异步消费该事件，将互动通知存入 MySQL，并实时判定作者是否在线。
3. 若作者在线（在网关注册了 WebSocket 链接），将消息发布至 Redis 通道。
4. 网关节点订阅到消息，从内存长连接池中查获该作者的 `*websocket.Conn` 管道，将通知数据实时推送给前端。
5. 客户端界面无刷新实时弹出气泡提醒。

### 🔑 核心重点 (Key Focus)
- **长连接高吞吐并发安全**：WebSocket 会话的读写是非线程安全的。重点阅读对长连接连接加锁写入（Mutex Wrap）的保障。
- **连接泄漏防范**：如何妥善捕获客户端网络断开、浏览器刷新等异常事件，及时关闭并从全局 Connection Map 中注销管道，防止内存暴涨。

### ✨ 技术亮点 (Architecture Highlights)
- **WebSocket BFF 去状态化**：网关只负责持有最轻量的 WebSocket 连接句柄，而复杂的通知存储、红点未读计数计算完全剥离下沉给 `notification-service`，方便网关的水平动态扩缩容。

### ⚠️ 避坑指南 (Gotchas)
- **协程僵死泄漏**：当 WebSocket 客户端离线但服务端未设置 `ReadDeadline` / `WriteDeadline` 时，服务发送心跳探测会因超时无限挂起，导致对应的 goroutine 无法回收而发生永久性协程泄露。

### 🚀 进阶玩法 (Advanced Play)
- **分布式集群 WebSocket 网格 (Redis Pub/Sub 广播定位)**：如果系统部署了 3 个 Gateway 实例，用户 A 连接在 Gateway-1，用户 B 在 Gateway-2。当 A 给 B 发送通知时，利用 Redis PubSub 将通知事件向全集群广播，各 Gateway 实例收到广播后在本地查找该连接，实现跨节点通信。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：启动两个不同的浏览器账号，B 建立 WebSocket 连接。A 给 B 点赞，检查 B 端的浏览器网络通道，截图确认是否在零延迟内收到了服务器实时推送的推送 JSON 事件包。

---

## 🗓️ Day 10: Messenger System 社交私信系统架构（WebSocket 单聊与离线消息拉取）

### 🎯 学习目标与安排
- **文件范围**：[messenger_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/messenger/service/messenger_service.go) (历史拉取), [messenger_handler.go](file:///e:/GOProject/云原生/twitter-clone/internal/gateway/handler/messenger_handler.go) (实时私信分发)
- **核心目标**：研究微服务下大吞吐双向实时单聊私信系统的收发时序及离线消息补发机制。

### 🛣️ 请求链路全景 (Request Flow)
1. 发送方通过 WebSocket 发送一条私信消息。
2. 网关接收到消息：
   - 立即调用 `messenger-service` 将消息落库，并状态置为 `unread`（未读）。
   - 同时，检查接收方是否建立 WebSocket 会话。
3. 若接收方在线，实时转发其消息，并发送“已送达（Delivered）”回执；若离线，将消息置为未读离线消息。
4. 接收方上线后，自动向网关发送同步请求，网关调服务批量拉取在该离线时间段内所有未读的历史私信数据进行回填补齐，并标记为已读。

### 🔑 核心重点 (Key Focus)
- **消息发送的时序性 (Message Ordering)**：确保在高并发或者网络重连时，同一个会话中的私信消息不发生时序错乱。
- **分布式未读计数器**：大吞吐私信下，如何原子处理各个独立单聊会话中未读消息数字的计算。

### ✨ 技术亮点 (Architecture Highlights)
- **实时同步与离线拉取双轨合并**：基于离线消息主动同步拉取 + 在线 WebSocket 管道直接推送的双轨机制，完美兼容弱网环境下的极速聊天体验。

### ⚠️ 避坑指南 (Gotchas)
- **死锁竞争**：在网关内部使用 Map 存储千万级用户长连接。如果多个协程同时进行写操作而未加读写锁（RWMutex）保护，会导致典型的 `concurrent map read and map write` Panic 崩溃。

### 🚀 进阶玩法 (Advanced Play)
- **私信端到端加密 (E2E Encryption)**：在客户端生成非对称公私钥对，私信内容仅在客户端通过接收方的公钥加密，网关和数据库仅传输和存储密文，彻底规避了服务端泄密隐患。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：让用户 A 给离线状态的用户 B 发送 5 条私信。随后让用户 B 建立 WebSocket 链接。
- **验证**：通过日志证实 B 端上线后是否一次性收到了离线消息的批量聚合补发，且数据库中的 `unread` 状态自动归零。
