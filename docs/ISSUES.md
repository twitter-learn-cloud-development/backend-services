# 开发过程问题与解决方案清单

> 记录 twitter-clone 云原生项目开发过程中遇到的问题及解决方案

---

## 1. Tweet Service 重复声明错误

| 项目 | 内容 |
|------|------|
| **问题** | `tweet_service.go` 和 `tweet_service_mq.go` 存在重复的类型/方法声明，导致编译失败 |
| **原因** | MQ 版本 (`tweet_service_mq.go`) 是实际使用的实现，旧文件未清理 |
| **解决** | 将 `tweet_service.go` 内容替换为最小化占位，保留文件历史 |

## 2. User Service 导入缺失

| 项目 | 内容 |
|------|------|
| **问题** | 重构 `user_service.go` 日志后编译失败，缺少 `regexp`、`strings`、`bcrypt` 等导入 |
| **原因** | 替换日志代码时意外删除了其他必要的 import |
| **解决** | 逐一恢复缺失的 import 包，确认编译通过 |

## 3. Minikube 镜像拉取失败 (ErrImagePull)

| 项目 | 内容 |
|------|------|
| **问题** | `kubectl set image` 后 Pod 显示 `ErrImagePull`，无法拉取 `trace-v1` 镜像 |
| **原因** | Docker Desktop 构建的镜像在宿主机 Docker daemon，Minikube（Docker driver）使用独立的 daemon |
| **解决** | 使用 `minikube image load <image>` 将镜像从宿主机加载到 Minikube |

## 4. Grafana init-chown-data 崩溃

| 项目 | 内容 |
|------|------|
| **问题** | Grafana Pod 持续 `Init:CrashLoopBackOff`，`init-chown-data` 容器失败 |
| **原因** | Minikube PV 权限限制，init 容器无法对挂载卷执行 `chown` |
| **解决** | 在 `grafana-values.yaml` 中设置 `initChownData.enabled: false` 并 `persistence.enabled: false` |

## 5. Grafana runAsUser 违反非 Root 策略

| 项目 | 内容 |
|------|------|
| **问题** | 尝试 `securityContext.runAsUser: 0` 修复权限问题，Pod 报 `CreateContainerConfigError` |
| **原因** | Grafana Helm chart 有 non-root 安全策略，`runAsUser: 0` 违反该策略 |
| **解决** | 移除 `runAsUser: 0`，改用禁用 persistence 的方式彻底规避 PV 权限问题 |

## 6. helm/minikube 命令不在 PATH

| 项目 | 内容 |
|------|------|
| **问题** | PowerShell 中 `helm` 和 `minikube` 命令报 `CommandNotFoundException` |
| **原因** | 可执行文件位于 `E:\K8s-Tools\` 但未加入系统 PATH |
| **解决** | 使用完整路径 `E:\K8s-Tools\helm.exe` 和 `E:\K8s-Tools\minikube.exe` 执行命令 |

## 7. PowerShell curl 别名冲突

| 项目 | 内容 |
|------|------|
| **问题** | `curl -H "Content-Type: application/json"` 失败 |
| **原因** | PowerShell 中 `curl` 是 `Invoke-WebRequest` 的别名，参数格式不兼容 |
| **解决** | 使用 `Invoke-RestMethod -ContentType "application/json"` 替代 |

## 8. Loki TraceID Derived Field 匹配率 0%

| 项目 | 内容 |
|------|------|
| **问题** | Grafana Loki 的 TraceID derived field 显示 0% 匹配率 |
| **原因** | 日志以 Docker JSON 格式存储，`trace_id` 前后有转义引号 `\":\"`（5个非字母字符），正则 `"trace_id":"(\w+)"` 无法匹配 |
| **解决** | 改用 `trace_id\W+(\w+)`，`\W+` 能匹配一个或多个非字母字符，正确跳过转义分隔符 |

## 9. Grafana → Jaeger 查询 EOF 错误

| 项目 | 内容 |
|------|------|
| **问题** | 点击 TraceID 链接跳转 Jaeger 时报 `Get "http://twitter-clone-jaeger-query.default:16686/...": EOF` |
| **原因** | `twitter-clone-jaeger-query` 是 Headless Service（ClusterIP: None），Grafana proxy 模式无法正常访问 |
| **解决** | 创建带 ClusterIP 的新 Service `jaeger-query-clusterip`，更新 Grafana Jaeger 数据源 URL 指向该 Service |

## 10. Jaeger Pod CrashLoopBackOff（健康检查端口不匹配）

| 项目 | 内容 |
|------|------|
| **问题** | Jaeger Pod 持续 `CrashLoopBackOff`，每次启动约75秒后被杀死 |
| **原因** | Helm chart 配置 liveness/readiness probe 端口为 `13133`，但 Jaeger v1.66.0 的 Admin 健康端点实际监听在 `14269` |
| **解决** | 使用 `kubectl patch deployment` 将 probe 端口修正为 `14269`，保存到 `deploy/patches/jaeger-probe-fix.yaml` |

## 11. Jaeger Trace Export Failed (404 Not Found)

| 项目 | 内容 |
|------|------|
| **问题** | Grafana/Loki 能看到 `trace_id`，但点击跳转 Jaeger 显示 `404 Not Found`。Gateway 日志报错 `connection refused`。 |
| **原因** | 1. 代码使用 Jaeger Agent (UDP 6831) 模式，但 Jaeger `all-in-one` 镜像默认 Agent 仅监听 localhost，跨 Pod 无法访问。<br>2. 尝试连接 Service 时，Service 缺少 selector 导致 endpoints 为空。 |
| **解决** | 1. 修改 `pkg/trace` 代码，改用 Jaeger Collector (HTTP 14268) 模式。<br>2. 更新 `jaeger-query-clusterip` Service 暴露 14268 端口并修复 selector。<br>3. 更新所有服务镜像 (`trace-v2`) 并注入 `JAEGER_COLLECTOR_ENDPOINT` 环境变量。 |

## 12. Login 后用户信息存储错误

| 项目 | 内容 |
|------|------|
| **问题** | 登录成功后所有依赖用户 ID 的操作（查看资料、发推等）均失败 |
| **原因** | `Login.vue` 执行 `userStore.setUser(userRes.data)` 存的是 `{ user: {...} }` 而非用户对象，导致 `userStore.user.id` 为 undefined |
| **解决** | 改为 `userStore.setUser(userRes.data.user)`，正确提取嵌套的用户数据 |

## 13. 前端 API 路径与后端路由不匹配（4处）

| 项目 | 内容 |
|------|------|
| **问题** | 关注、取关、检查关注状态、取消点赞等功能请求 404 |
| **原因** | `user.ts` 和 `tweet.ts` 中的 API 路径/方法与后端 `router.go` 路由定义不一致：`POST /follow` → 应为 `POST /follows`、`POST /unfollow/:id` → 应为 `DELETE /follows/:id`、`GET /users/:id/following` → 应为 `GET /follows/:id/status`、`POST /tweets/:id/unlike` → 应为 `DELETE /tweets/:id/like` |
| **解决** | 统一前端所有 API 路径和 HTTP 方法与后端路由定义一致 |

## 14. GetProfile 在认证中间件之后

| 项目 | 内容 |
|------|------|
| **问题** | 未登录用户无法查看他人资料，Profile 页面显示空白 |
| **原因** | `router.go` 中 `users.GET("/:id", ...)` 在 `jwtMW.AuthRequired()` 之后注册，要求认证才能访问 |
| **解决** | 使用子路由组 `authedUsers` 隔离认证接口（`/me`），`/:id` 保持公开注册 |

## 15. Like/Comment 500 错误（数据库表缺失）

| 项目 | 内容 |
|------|------|
| **问题** | 点赞推文返回 500 Internal Server Error：`Table 'twitter.likes' doesn't exist` |
| **原因** | `tweet-service/main.go` 的 `AutoMigrate` 只迁移了 `Tweet` 和 `Follow`，没有包含 `Like` 和 `Comment` 实体 |
| **解决** | 在 `tweet-service/main.go` 和 `consumer/main.go` 的 `AutoMigrate` 中添加 `&domain.Like{}, &domain.Comment{}` |

## 16. 推文显示 "Unknown @unknown"（缺少用户信息）

| 项目 | 内容 |
|------|------|
| **问题** | 所有推文的作者名显示为 "Unknown @unknown"，头像为默认灰色 |
| **原因** | `tweet_handler.go` 的 `formatTweet()` 只返回 `user_id`，不包含用户名/头像等信息。Tweet proto 也不包含这些字段 |
| **解决** | 给 `TweetHandler` 注入 `UserClient`，新增 `enrichTweetsWithUserInfo()` 方法在 gateway 层批量查询用户信息并注入到推文响应中 |

## 17. 前端组件功能缺失（按钮无效、趋势硬编码）

| 项目 | 内容 |
|------|------|
| **问题** | TweetCard 评论/转推/分享按钮点击无反应；Profile 编辑资料按钮无效、Tab不切换；侧边栏趋势数据为硬编码假数据 |
| **原因** | 前端组件只写了 UI 外壳，缺少事件处理函数和业务逻辑 |
| **解决** | 重写 `TweetCard.vue`（评论弹窗/分享复制/转推提示）、`Profile.vue`（编辑资料弹窗/Tab切换/`res.data.user`提取）、`MainLayout.vue`（从 `/trends` API 动态获取趋势数据） |

## 18. 编辑资料保存后字段交换（bio ↔ avatar）

| 项目 | 内容 |
|------|------|
| **问题** | 编辑资料保存后，个人简介和头像 URL 的值互换 |
| **原因** | `saveProfile` 用 `editForm` 手动赋值更新 `user.value`，且前端发送了后端不支持的 `username` 字段，导致数据错乱 |
| **解决** | 删除 `username` 编辑字段（后端 `UpdateProfileRequest` 不支持），保存后用后端响应 `res.data.user` 直接覆盖 `user.value` |

## 19. 书签路由 404 (POST /tweets/:id/bookmark)

| 项目 | 内容 |
|------|------|
| **问题** | 点击书签按钮返回 404 |
| **原因** | 书签路由在 `router.go` 中通过独立的 `v1.Group("/tweets")` 注册，与主 tweets 路由组产生冲突，Gin 的 `/:id` 通配符优先匹配拦截了 `/:id/bookmark` |
| **解决** | 将 `POST/DELETE /:id/bookmark` 移入已有的 tweets 路由组内注册，避免重复 Group 冲突 |


## 20. 关注接口 500 + 关注状态刷新丢失

| 项目 | 内容 |
|------|------|
| **问题** | 关注按钮点击返回 500，且即使偶尔成功，刷新页面后关注状态消失 |
| **原因** | 前端 `followUser` 发送 `followee_id: parseInt(userId)`，Snowflake ID 超过 JS `Number.MAX_SAFE_INTEGER` (2^53-1)，导致精度丢失，后端收到错误 ID 后 gRPC 调用失败返回 500。关注记录未写入 DB，所以刷新后 `IsFollowing` 返回 false |
| **解决** | 后端 `FollowRequest.FolloweeID` 改为 `string` 类型，用 `strconv.ParseUint` 解析；前端直接发送字符串 `followee_id: userId` |

## 21. 书签/通知 500 Panic

| 项目 | 内容 |
|------|------|
| **问题** | `AddBookmark` 接口返回 500，Gateway 日志无报错（因为 panic 导致进程重启或被 recover 吞掉） |
| **原因** | `bookmarkRepo.Create` 调用 `snowflake.GenerateID()`，但 Gateway `main.go` 未调用 `snowflake.Init()`，导致 `node` 为 nil 发生 panic |
| **解决** | 在 `cmd/gateway/main.go` 中添加 `snowflake.MustInit(1)` 初始化代码 |

## 22. 编辑资料字段交换 (bio ↔ avatar)

| 项目 | 内容 |
|------|------|
| **问题** | 保存个人资料后，头像URL写入了 bio 字段，bio 内容写入了 avatar 字段，导致页面显示混乱 |
| **原因** | `internal/module/user/grpc/user.go:80` 调用 `s.svc.UpdateProfile(ctx, req.UserId, req.Avatar, req.Bio)`，而 Service 函数签名是 `UpdateProfile(ctx, userID, bio, avatar)` — 参数顺序反了 |
| **解决** | 修正为 `s.svc.UpdateProfile(ctx, req.UserId, req.Bio, req.Avatar)` |

## 23. 评论作者显示 unknown

| 项目 | 内容 |
|------|------|
| **问题** | 推文详情页评论列表中，所有评论作者显示为 "unknown" |
| **原因** | `domainCommentToProto` 不填充用户信息字段，gateway `GetTweetComments` 也没有查询用户信息聚合 |
| **解决** | 在 `GetTweetComments` handler 中批量查询 `userClient.GetProfile` 并注入 `user` 对象到评论 JSON |

## 24. 点赞状态刷新后丢失

| 项目 | 内容 |
|------|------|
| **问题** | 刷新页面后，之前点赞的推文的红心变回未点赞状态 |
| **原因** | `GetFeedsRequest` 缺少 `requesting_user_id` 字段，gateway 无法将当前用户 ID 传给 tweet-service 判断点赞状态 |
| **解决** | 在 gateway 的 `enrichTweetsWithUserInfo` 中直接查询 likes 表批量注入 `is_liked` 状态 |

## 25. 书签状态刷新后丢失

| 项目 | 内容 |
|------|------|
| **问题** | 收藏的推文刷新后书签图标变回未收藏状态 |
| **原因** | TweetCard.vue `isBookmarked` 硬编码为 `false`，后端 `formatTweet` 不返回 `is_bookmarked` |
| **解决** | gateway 批量查 bookmarks 表注入 `is_bookmarked`，TweetCard 从 props 读取，Bookmarks 页强制 true |

## 26. 通知未读计数不即时消除

| 项目 | 内容 |
|------|------|
| **问题** | 进入通知页阅读后，NavBar 的红色未读徽章不会立即消除 |
| **原因** | NavBar 仅靠 30 秒轮询刷新计数，markAsRead 后不会触发即时刷新 |
| **解决** | 添加 `notifications-read` 自定义事件监听 + route watcher 离开通知页时立即刷新 |

## 27. API Regressions & 404/500 Bugs

| 项目 | 内容 |
|------|------|
| **问题** | 1. `PUT /users/me` 报 404 NotFound <br>2. `POST /tweets/:id/retweet` 报 500 Internal Server Error <br>3. 用户搜索列表无法显示“已关注”状态 <br>4. 首页推文不显示“已投票”状态和百分比 <br>5. Messenger 前端请求 `/conversations` 报 404 <br>6. WebSocket 无法连接导致控制台不断刷屏 |
| **原因** | 1-2. 网关 `router.go` 路由丢失/映射错误；TweetHandler 缺失转发方法 <br>3-4. 网关转发 gRPC 请求后未查表聚合 `is_following` 和 `poll_votes` 数据 <br>5. 前端 API 路径未匹配网关新增的 `/messenger` 分组 <br>6. 网关未实例化并挂载 `WebSocketHandler` |
| **解决** | 1-2. 恢复正确网关路由映射，补充 `RetweetTweet`/`UnretweetTweet` 方法 <br>3. `SearchUsers` 网关接口追加并发调用 FollowService 获取关注状态 <br>4. 网关 `enrichTweetsWithUserInfo` 内直连数据库查询 `poll_votes` 并注入 <br>5. 修改前端 `messenger.ts` 的 api 请求路径加上 `/messenger` <br>6. 在 `main.go` 实例化 `WebSocketHandler` 并映射至 `/api/v1/ws` |

## 28. 推文详情页(TweetDetail)前端交互失效

| 项目 | 内容 |
|------|------|
| **问题** | 1. 评论无法指定人回复 (仅能发帖)<br>2. 贴子内部的“转推”按钮无反应<br>3. "推文串"(Thread) 功能失效，串内回复按钮无反应 |
| **原因** | 1. `TweetDetail.vue` 使用了 Vue 插件自动导入逻辑的遗漏，导致 `ReplyModal` 组件未正确渲染。<br>2. `TweetCard` 未向上 `emit('reply')` 导致串联组件的回复按钮脱节。<br>3. 详情页的 `handleRetweet` 逻辑未实现，且评论组件缺少获取并处理 `parent_id` 的入口。 |
| **解决** | 1. 显式导入 `TweetCard` 和 `ReplyModal` 至 `TweetDetail.vue`。<br>2. 补充 `TweetCard.vue` 中的 `@click="handleReplyClick"` 分发 `reply` 事件。<br>3. 在推文详情页增加 `handleRetweet` 接口调用、实现内嵌评论的 `handleReplyToComment(comment)` 定向回复(挂载 `@username` 及传输 `parent_id`)。 |

## 29. 评论回复后立刻显示 unknown 信息丢失

| 项目 | 内容 |
|------|------|
| **问题** | 用户在推文详情页发表评论后，最新推入列表的评论，用户名和头像都显示为 `unknown`。 |
| **原因** | `v1/tweets/:id/comments` (Gateway) 接收 Tweet Service 的 `CreateComment` gRPC 响应后，直接原样格式化返回。由于底层 Service 仅写入 `UserID` 并未回填用户详情（Profile/Avatar），前端缺乏数据导致 fallback 为 fallback。 |
| **解决** | 在 `tweet_handler.go` (`CreateComment`) 响应前，增加一步 `userClient.GetBatchUsers` 调用拿取对应的用户资料并组装至 `comment` 返回对象上。 |

## 30. 创建评论报错 400 Bad Request

| 项目 | 内容 |
|------|------|
| **问题** | 点击或者发布评论时报 `400 Bad Request` 且发送失败。 |
| **原因** | 网关 `CreateCommentRequest` struct 中的 `ParentID` 声明为 `uint64`。由于 Twitter Snowflake ID 精度的需求，前端以字符串形式 (`"2024791560905822208"`) 回传或回传被漏设，导致 Go 的 JSON 反序列化因为类型不匹配失败。 |
| **解决** | 将结构体的 `ParentID` 改为 `string`，接收后使用 `strconv.ParseUint` 手动强转以增加容错和解析成功率。 |

## 31. 推文详情页(TweetDetail)不显示投票进度

| 项目 | 内容 |
|------|------|
| **问题** | 首页信息流正常显示已投票的进度条和百分比，但点进帖子详情页(TweetDetail)却变成了只显示选项（未投票的初始外观）。 |
| **原因** | 网关的 `GetTweet` `GET /api/v1/tweets/:id` 接口实现中，过去手写了“作者信息”、“Like”、“Bookmark”、“Retweet”的拼装，唯独漏掉了读取 `poll_votes` 表。 |
| **解决** | 移除 `GetTweet` 中冗余重复的拼装代码，统一复用首页流使用的 `enrichTweetsWithUserInfo` 函数，该函数内置了一并加载各种所有互动状态（含投票）的完备逻辑。 |

## 32. 关注列表 (Followees) 请求 404 Not Found

| 项目 | 内容 |
|------|------|
| **问题** | 从个人主页点击“正在关注”标签，控制台报 `/api/v1/users/:id/following` 404 错误且列表为空。 |
| **原因** | 前端 `api/user.ts` 中的 `getFollowees` 请求的 URL 路径为 `/following`，而 API Gateway `router.go` 实际注册的路径叫 `/followees`。 |
| **解决** | 修改前端请求路径，匹配后端的 `/api/v1/users/:id/followees` 契约。 |

## 33. 粉丝列表 (Followers) 请求 500 Internal Server Error

| 项目 | 内容 |
|------|------|
| **问题** | 点击“关注者”页面时，请求后台报错 500。 |
| **原因** | 跟踪到 `follow-service` 的 `follow_repo.go` 中，`GetFollowers` 的 GORM 查询语句手误将 `deleted_at = 0` 写成了 `deleted_id = 0`，引发数据库字段不存在报错。 |
| **解决** | 将 Repo 查询中的 `deleted_id` 修正为软删除字段 `deleted_at` 即可。 |

## 34. 关注/粉丝列表数据请求成功但页面依然为空白

| 项目 | 内容 |
|------|------|
| **问题** | 关注者和正在关注列表的请求返回了 200 OK，并且 `follow_ids` 内有数据，但页面依然显示“这里好像什么都没有”。 |
| **原因** | 后端 API 网关直接将 `[]uint64` 类型的 Snowflake ID 作为 JSON Number 数组返回给了前端。由于 JS 的 `Number` 类型最高仅支持 53 位精度，在解析超过精度的推特雪花 ID（如 `2023661202374135808`）时被截断成了错误数字（如 `2023661202374135800`）。前端拿着这些错误截断的 IDs 去请求 `getBatchUsers`，自然匹配不到任何用户，于是结果为空。 |
| **解决** | 修改 `follow_handler.go`，在组装 JSON 之前，先利用 `strconv.FormatUint(id, 10)` 将所有 `uint64` 轮询转为 `[]string`，从而避免了前端 JS 的反序列化精度丢失问题。 |

## 35. 关注列表/粉丝列表的人员缺少“已关注”状态

| 项目 | 内容 |
|------|------|
| **问题** | 在列表里看到的“关注者”或者“正在关注”的人，右侧的按钮清一色显示“关注”而不是“已关注”，无法正确进行取消关注操作。 |
| **原因** | 有两个原因叠加：1. 原本网关层的 `GetBatchUsers` 与 `GetProfile` 接口仅仅是转发了获取档案 RPC，遗漏了判断关注状态 (`is_following`) 的业务逻辑。2. 就算代码里加了 `middleware.GetUserID(c)` 去获取当前登录账号，因为 `/users/:id` 和 `/users/batch` 被划分为 `公开接口`，它们路由本身压根没有挂载 JWT Token 解析中间件，所以 `GetUserID` 永远返回 0。 |
| **解决** | 第一步：在 `user_handler.go` 中加入起协程并发调用 `followClient.IsFollowing` 去实时查状态并组装的逻辑。第二步：在 `router.go` 的公开路由组前加上 `users.Use(jwtMW.AuthOptional())` 可选鉴权中间件。这样当游客访问时不阻拦，但当登录用户访问时能够成功剥离出身份去查出准确的跟随、点赞状态。 |
