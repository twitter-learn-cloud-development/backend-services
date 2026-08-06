d

# Twitter Clone API 接口文档

## 概述

| 项目               | 说明                                                                                                |
| ------------------ | --------------------------------------------------------------------------------------------------- |
| **Base URL** | `http://twitter-clone.local/api/v1` (Ingress) 或 `http://<minikube-ip>:30638/api/v1` (NodePort) |
| **认证方式** | Bearer Token (JWT)，通过`Authorization: Bearer <token>` 请求头传递                                |
| **数据格式** | JSON                                                                                                |
| **网关端口** | 9638                                                                                                |

---

## 1. 认证接口 (Auth)

### 1.1 用户注册

```
POST /api/v1/auth/register
```

**请求体：**

```json
{
  "username": "string (必填, 3-20字符)",
  "email": "string (必填, 邮箱格式)",
  "password": "string (必填, ≥6字符)"
}
```

**成功响应 (200)：**

```json
{
  "user": {
    "id": 1,
    "username": "john",
    "email": "john@example.com",
    "avatar": "",
    "bio": "",
    "created_at": 1704067200,
    "updated_at": 1704067200
  }
}
```

---

### 1.2 用户登录

```
POST /api/v1/auth/login
```

**请求体：**

```json
{
  "email": "string (必填)",
  "password": "string (必填)"
}
```

**成功响应 (200)：**

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": 1,
    "username": "john",
    "email": "john@example.com",
    "avatar": "",
    "bio": ""
  }
}
```

---

## 2. 用户接口 (Users)

### 2.1 获取用户资料（公开）

```
GET /api/v1/users/:id
```

**路径参数：**

| 参数   | 类型   | 说明    |
| ------ | ------ | ------- |
| `id` | uint64 | 用户 ID |

**成功响应 (200)：**

```json
{
  "user": {
    "id": 1,
    "username": "john",
    "email": "john@example.com",
    "avatar": "https://...",
    "bio": "Hello world"
  }
}
```

---

### 2.2 获取聚合用户资料 — BFF（公开）

```
GET /api/v1/users/:id/full_profile
```

> 🔥 **BFF 聚合端点**：并发调用 User/Tweet/Follow 三个服务，返回完整用户画像。

**成功响应 (200)：**

```json
{
  "user": {
    "id": 1,
    "username": "john",
    "email": "john@example.com",
    "avatar": "https://...",
    "bio": "Hello world"
  },
  "recent_tweets": [
    {
      "id": 101,
      "content": "My first tweet!",
      "created_at": 1704067200
    }
  ],
  "follow_stats": {
    "follower_count": 42,
    "followee_count": 18
  }
}
```

---

### 2.3 获取当前用户信息 🔒

```
GET /api/v1/users/me
```

**Headers：** `Authorization: Bearer <token>`

---

### 2.4 更新当前用户资料 🔒

```
PUT /api/v1/users/me
```

**Headers：** `Authorization: Bearer <token>`

**请求体：**

```json
{
  "avatar": "string (可选)",
  "bio": "string (可选)"
}
```

---

## 3. 推文接口 (Tweets)

### 3.1 获取推文详情（公开）

```
GET /api/v1/tweets/:id
```

**路径参数：**

| 参数   | 类型   | 说明    |
| ------ | ------ | ------- |
| `id` | uint64 | 推文 ID |

**成功响应 (200)：**

```json
{
  "tweet": {
    "id": 101,
    "user_id": 1,
    "content": "Hello Twitter!",
    "media_urls": ["https://..."],
    "type": 0,
    "visible_type": 0,
    "created_at": 1704067200,
    "like_count": 5,
    "comment_count": 2,
    "share_count": 1,
    "is_liked": false
  }
}
```

---

### 3.2 发布推文 🔒

```
POST /api/v1/tweets
```

**Headers：** `Authorization: Bearer <token>`

**请求体：**

```json
{
  "content": "string (必填)",
  "media_urls": ["string"] 
}
```

---

### 3.3 删除推文 🔒

```
DELETE /api/v1/tweets/:id
```

**Headers：** `Authorization: Bearer <token>`

---

### 3.4 点赞推文 🔒

```
POST /api/v1/tweets/:id/like
```

**Headers：** `Authorization: Bearer <token>`

**成功响应 (200)：**

```json
{
  "like_count": 6,
  "is_liked": true
}
```

---

### 3.5 取消点赞 🔒

```
DELETE /api/v1/tweets/:id/like
```

**Headers：** `Authorization: Bearer <token>`

**成功响应 (200)：**

```json
{
  "like_count": 5,
  "is_liked": false
}
```

---

### 3.6 发布评论 🔒

```
POST /api/v1/tweets/:id/comments
```

**Headers：** `Authorization: Bearer <token>`

**Body：**

```json
{
  "content": "This is a comment!",
  "parent_id": 0
}
```

**成功响应 (201)：**

```json
{
  "comment": {
    "id": "1234567890",
    "content": "This is a comment!",
    "user": { ... }
  }
}
```

---

### 3.7 获取推文评论（公开）

```
GET /api/v1/tweets/:id/comments?cursor=0&limit=20
```

**成功响应 (200)：**

```json
{
  "comments": [ ... ],
  "next_cursor": "12345",
  "has_more": true
}
```

---

### 3.8 删除评论 🔒

```
DELETE /api/v1/comments/:id
```

**Headers：** `Authorization: Bearer <token>`

---

### 3.9 获取用户时间线（公开）

```
GET /api/v1/users/:id/timeline
```

---

## 4. 实时通知 (WebSocket)

### 4.1 建立连接

```
GET /api/v1/ws?token=<access_token>
```

**Query Parameters：**

- `token`: JWT Access Token (必填)

**消息通过 WebSocket 推送，格式如下：**

```json
{
  "id": "189...",
  "user_id": "123",
  "actor_id": "456",
  "type": "like", // like, comment, follow
  "target_id": "789",
  "content": "Somebody liked your tweet",
  "created_at": 1678901234567,
  "actor": {
    "username": "alice",
    "nickname": "Alice",
    "avatar_url": "..."
  }
}
```

---

## 5. 搜索 (Search)

### 5.1 搜索推文 (简易)

```
GET /api/v1/search?q=<keyword>
```

**Query Parameters：**

- `q`: 搜索关键词 (必填)
- `cursor`: 游标 (上次返回的 `next_cursor`，默认为 0)
- `limit`: 每页数量 (默认 20)

**成功响应 (200 OK)：**

```json
{
  "tweets": [
    {
      "id": "189...",
      "content": "This is a search result...",
      // ... same as Tweet object
    }
  ],
  "next_cursor": "189...",
  "has_more": true
}
```

### 5.2 热门话题 (Trending Topics)

```
GET /api/v1/trends
```

**Query Parameters：**

- `limit`: 返回数量 (默认 10，最大 50)

**成功响应 (200 OK)：**

```json
{
  "topics": [
    {
      "topic": "golang",
      "score": 0 // 目前暂不返回具体热度分值
    },
    {
      "topic": "k8s",
      "score": 0
    }
  ]
}
```

---

## 6. 用户时间线 (Timeline)

### 6.1 获取用户时间线

```
GET /api/v1/users/:id/timeline
```

**查询参数：**

| 参数       | 类型   | 默认值 | 说明             |
| ---------- | ------ | ------ | ---------------- |
| `cursor` | uint64 | 0      | 游标（分页起点） |
| `limit`  | int32  | 20     | 每页数量         |

**成功响应 (200)：**

```json
{
  "tweets": [...],
  "next_cursor": 95,
  "has_more": true
}
```

---

### 3.5 获取关注 Feed 流 🔒

```
GET /api/v1/feeds
```

**Headers：** `Authorization: Bearer <token>`

**查询参数：** 同上 (`cursor`, `limit`)

### 1.3 获取当前用户信息

```
GET /api/v1/users/me
```

**成功响应 (200 OK)：**

```json
{
  "user": {
    "id": 123,
    "username": "alice",
    "avatar": "http://localhost:8080/uploads/20231010/xxx.jpg",
    "bio": "Hello World",
    "created_at": 1678901234567
  }
}
```

### 1.4 更新用户信息

```
PUT /api/v1/users/me
```

**请求体 (JSON)：**

```json
{
  "avatar": "http://localhost:8080/uploads/20231010/xxx.jpg",
  "bio": "New Bio"
}
```

**成功响应 (200 OK)：**

```json
{
  "user": { ... } // 更新后的用户信息
}
```

---

## 2. 媒体上传 (Media Upload)

```
POST /api/v1/upload
```

**Content-Type**: `multipart/form-data`

**Form Data:**

- `file`: (Binary) 图片或视频文件 (max 10MB)

**成功响应 (200 OK)：**

```json
{
  "url": "http://localhost:9000/twitter-media/20231010/uuid.jpg"
}
```

---

## 3. 关注 (Follow)

### 3.1 关注用户 🔒

```
POST /api/v1/follows
```

**Headers：** `Authorization: Bearer <token>`

**请求体：**

```json
{
  "followee_id": 2
}
```

---

### 4.2 取消关注 🔒

```
DELETE /api/v1/follows/:id
```

**Headers：** `Authorization: Bearer <token>`

**路径参数：** `id` = 被取关用户的 ID

---

### 4.3 检查关注状态 🔒

```
GET /api/v1/follows/:id/status
```

**成功响应 (200)：**

```json
{
  "is_following": true
}
```

---

### 4.4 获取粉丝列表（公开）

```
GET /api/v1/users/:id/followers
```

**查询参数：** `cursor`, `limit`

**成功响应 (200)：**

```json
{
  "follower_ids": [3, 5, 8],
  "next_cursor": 8,
  "has_more": false
}
```

---

### 4.5 获取关注列表（公开）

```
GET /api/v1/users/:id/followees
```

---

### 4.6 获取关注统计（公开）

```
GET /api/v1/users/:id/stats
```

**成功响应 (200)：**

```json
{
  "follower_count": 42,
  "followee_count": 18
}
```

---

## 5. 系统接口

### 5.1 健康检查

```
GET /health
```

**响应：**

```json
{
  "status": "ok"
}
```

---

### 5.2 Prometheus 指标

```
GET /metrics
```

返回 Prometheus 格式的系统指标。

---

## 6. gRPC 内部服务接口

> 以下接口仅供微服务间内部调用，不对外暴露。

### 6.1 UserService (端口 9091)

| 方法               | 请求                                      | 响应              |
| ------------------ | ----------------------------------------- | ----------------- |
| `Register`       | `{username, email, password}`           | `{user}`        |
| `Login`          | `{email, password}`                     | `{token, user}` |
| `GetProfile`     | `{user_id}`                             | `{user}`        |
| `UpdateProfile`  | `{user_id, avatar, bio}`                | `{user}`        |
| `ChangePassword` | `{user_id, old_password, new_password}` | `{message}`     |

### 6.2 TweetService (端口 9092)

| 方法                 | 请求                                             | 响应                                  |
| -------------------- | ------------------------------------------------ | ------------------------------------- |
| `CreateTweet`      | `{user_id, content, media_urls, parent_id, poll_options, poll_duration_minutes, idempotency_key?}` | `{tweet}` |
| `GetTweet`         | `{tweet_id}`                                   | `{tweet}`                           |
| `DeleteTweet`      | `{tweet_id, user_id}`                          | `{message}`                         |
| `GetAuthorPostingStats` | `{author_id, lookback_seconds}` | `{sample_count, latest_created_at, previous_created_at}` |
| `ApplyTweetModeration` | `{tweet_id, author_id, action, reason_code}` | `{applied, timelines_cleaned, cleanup_queued}` |
| `GetUserTimeline`  | `{user_id, cursor, limit}`                     | `{tweets[], next_cursor, has_more}` |
| `GetFeeds`         | `{user_id, cursor, limit}`                     | `{tweets[], next_cursor, has_more}` |
| `BookmarkTweet`    | `{user_id, tweet_id}`                          | `{message}`                         |
| `UnbookmarkTweet`  | `{user_id, tweet_id}`                          | `{message}`                         |
| `GetUserBookmarks` | `{user_id, cursor, limit}`                     | `{tweets[], next_cursor, has_more}` |
| `RetweetTweet`     | `{user_id, tweet_id}`                          | `{retweet_count, is_retweeted}`     |
| `UnretweetTweet`   | `{user_id, tweet_id}`                          | `{retweet_count, is_retweeted}`     |
| `GetUserLikes`     | `{user_id, cursor, limit, requesting_user_id}` | `{tweets[], next_cursor, has_more}` |
| `GetUserReplies`   | `{user_id, cursor, limit, requesting_user_id}` | `{tweets[], next_cursor, has_more}` |
| `GetUserMedia`     | `{user_id, cursor, limit, requesting_user_id}` | `{tweets[], next_cursor, has_more}` |

`CreateTweet.idempotency_key` 是向后兼容的可选字段，建议所有可重试写入链路传入稳定键。该键按用户隔离：相同用户、相同键和相同请求摘要会返回首次提交的推文；相同键但请求内容不同返回 `AlreadyExists`。Tweet、Poll、Outbox Event 与幂等记录在同一 MySQL 事务内提交，避免 Agent 审批恢复或网络重试重复发推。

`GetAuthorPostingStats` 与 `ApplyTweetModeration` 是服务间风控接口，不映射为 Gateway HTTP API。前者只返回最多两条时间戳信号，不泄露推文正文；后者当前仅接受 `SHADOWBAN`。TweetService 在同一 MySQL 事务内幂等更新可见性并写入带去重键的 `TWEET_MODERATED` Outbox Event，随后由 Canal 投递 `tweet.moderated`。Timeline Consumer 使用关注关系 ID 稳定分页全量清理，并在 Redis 保存页游标与完成标记；重复投递只会重放幂等 Lua 清理。`cleanup_queued=true` 表示事务事件已提交；兼容字段 `timelines_cleaned` 在异步模式下固定为零。

### 6.3 FollowService (端口 9093)

| 方法               | 请求                           | 响应                                        |
| ------------------ | ------------------------------ | ------------------------------------------- |
| `Follow`         | `{follower_id, followee_id}` | `{message}`                               |
| `Unfollow`       | `{follower_id, followee_id}` | `{message}`                               |
| `IsFollowing`    | `{follower_id, followee_id}` | `{is_following}`                          |
| `GetFollowers`   | `{user_id, cursor, limit}`   | `{follower_ids[], next_cursor, has_more}` |
| `GetFollowees`   | `{user_id, cursor, limit}`   | `{followee_ids[], next_cursor, has_more}` |
| `GetFollowStats` | `{user_id}`                  | `{follower_count, followee_count}`        |

### 6.4 NotificationService (端口 9095)

| 方法                  | 请求                         | 响应                                         |
| --------------------- | ---------------------------- | -------------------------------------------- |
| `ListNotifications` | `{user_id, cursor, limit}` | `{notifications[], next_cursor, has_more}` |
| `MarkAsRead`        | `{user_id, ids[]}`         | `{message}`                                |
| `MarkAllAsRead`     | `{user_id}`                | `{message}`                                |
| `GetUnreadCount`    | `{user_id}`                | `{count}`                                  |

---

## 7. 错误响应格式

所有错误响应遵循统一格式：

```json
{
  "error": "错误描述信息"
}
```

| HTTP 状态码 | 说明                 |
| ----------- | -------------------- |
| 400         | 请求参数错误         |
| 401         | 未认证 / Token 无效  |
| 403         | 权限不足             |
| 404         | 资源不存在           |
| 429         | 请求过于频繁（限流） |
| 500         | 服务器内部错误       |

---

## 8. 通知接口 (Notification) 🔒

### 8.1 获取通知列表

```
GET /api/v1/notifications?cursor=0&limit=20
```

**成功响应 (200)：**

```json
{
  "notifications": [
    {
      "id": "1234567890",
      "type": "like",
      "target_id": "9876543210",
      "content": "",
      "is_read": false,
      "created_at": 1708000000000,
      "actor": {
        "id": "111",
        "username": "john",
        "avatar": "https://..."
      }
    }
  ],
  "next_cursor": "1234567890",
  "has_more": true
}
```

> type 取值: `like` / `comment` / `follow`

### 8.2 标记通知已读

```
PUT /api/v1/notifications/read
```

**请求体：**

```json
{
  "ids": [1234567890, 1234567891]
}
```

### 8.3 获取未读通知数

```
GET /api/v1/notifications/unread-count
```

**成功响应 (200)：**

```json
{
  "count": 5
}
```

---

## 9. 书签接口 (Bookmark) 🔒

### 9.1 添加书签

```
POST /api/v1/tweets/:id/bookmark
```

### 9.2 取消书签

```
DELETE /api/v1/tweets/:id/bookmark
```

### 9.3 获取书签列表

```
GET /api/v1/bookmarks?cursor=0&limit=20
```

**成功响应 (200)：**

```json
{
  "tweets": [
    {
      "id": "111",
      "content": "...",
      "user": { "id": "222", "username": "..." },
      "bookmarked_at": 1708000000000
    }
  ],
  "next_cursor": "0",
  "has_more": false
}
```

---

## 10. 转发接口 (Retweet)

### 10.1 转发推文  🔒

```
POST /api/v1/tweets/:id/retweet
```

**成功响应：**

```json
{ "retweet_count": 5, "is_retweeted": true }
```

### 10.2 取消转发  🔒

```
DELETE /api/v1/tweets/:id/retweet
```

**成功响应：**

```json
{ "retweet_count": 4, "is_retweeted": false }
```

---

## 11. 用户资料 Tabs 接口

### 11.1 获取用户喜欢的推文

```
GET /api/v1/users/:id/likes?cursor=0&limit=20
```

**成功响应：** 同推文列表格式 (`tweets` / `next_cursor` / `has_more`)

### 11.2 获取用户的回复

```
GET /api/v1/users/:id/replies?cursor=0&limit=20
```

**成功响应：**

```json
{
  "replies": [
    {
      "id": "123",
      "user_id": "456",
      "tweet_id": "789",
      "content": "回复内容",
      "created_at": 1708000000000,
      "user": { "id": "456", "username": "..." },
      "tweet": { "id": "789", "content": "原推文摘要", "user": { ... } }
    }
  ],
  "next_cursor": "0",
  "has_more": false
}
```

### 11.3 获取用户的媒体推文

```
GET /api/v1/users/:id/media?cursor=0&limit=20
```

**成功响应：** 同推文列表格式 (`tweets` / `next_cursor` / `has_more`)

---

## 12. 告警通知接收接口 (AlertManager Webhook)

### 12.1 接收告警通知 🔒

用于接收 Prometheus AlertManager 触发的告警回调。包含 firing 过滤与 groupKey 5分钟防抖去重机制，防止告警风暴 DDoS 攻击大模型 API。

```
POST /alerts
```

**Headers：**

| 请求头                   | 说明                                                 | 必填 |
| ------------------------ | ---------------------------------------------------- | ---- |
| `X-Alertmanager-Token` | 鉴权令牌，固定为`twitter-clone-secret-alert-token` | 是   |

**请求体 (JSON 示例)：**

```json
{
  "status": "firing",
  "groupKey": "redis-error-group"
}
```

**成功响应 (200 OK)：**

1. **正常接收并启动大模型诊断**（首次触发 firing）：

```json
{
  "status": "accepted",
  "msg": "alert accepted, diagnosing root cause..."
}
```

2. **触发防抖去重拦截**（同一个 groupKey 在 5分钟内重复发送）：

```json
{
  "status": "debounced",
  "msg": "alert storm debounced, skip LLM call"
}
```

3. **忽略恢复告警**（status 为 resolved）：

```json
{
  "status": "ignored",
  "msg": "resolved alert ignored"
}
```

---

## 13. 中间件

| 中间件                       | 说明                                          |
| ---------------------------- | --------------------------------------------- |
| **OpenTelemetry**      | 自动注入 TraceID/SpanID                       |
| **Prometheus Metrics** | 记录请求延迟、状态码等指标                    |
| **Rate Limiter**       | Redis 分布式限流 (1000/min per IP)            |
| **Logger**             | 请求日志记录                                  |
| **CORS**               | 跨域资源共享                                  |
| **Recovery**           | Panic 恢复                                    |
| **Error Handler**      | 统一错误处理                                  |
| **JWT Auth**           | 🔒 标记的接口需要认证                         |
| **JWT AuthOptional**   | 可选认证：有 token 就解析 user_id，没有则跳过 |

---

## 14. AI 智能体接口 (AI Agent) 🔒

所有智能体接口都需要 JWT 认证。由于 JavaScript 对 19 位超大 Snowflake ID 的精度截断限制，响应和请求中的 `dialogue_id`、`id`、`tweet_id` 等字段全部统一采用 **String 字符串** 类型传输。

### 14.0 统一 Agent 入口（P8 推荐）

```http
POST /api/v1/agent/run
```

`preferred_capability_ids` 可省略；它只表示用户偏好，不授予工具权限。Catalog 固定包含 `conversation.reply`、`platform.search`、`content.draft`、`web.search`、`connector.mcp` 和 `skill.run`。`web.search`、`connector.mcp` 与 `skill.run` 默认状态为 `planned`，只有各自服务端 Feature Flag 和真实 Adapter 可用时才注册精确执行路由。当前精确组合包括 `platform.search + content.draft`、Provider 可用时的 `web.search + content.draft`、显式选择的单能力 `connector.mcp -> runtime.external_mcp`，以及携带精确 Skill ID/Version 的 `skill.run -> runtime.skill`；草拟路径均不包含发布权限。外部 MCP 运行时还会重新取当前用户、Active Snapshot 和 Tool Policy 的交集，Capability Hint 不能直接授权远程工具。

`conversation.reply` 解析为 `runtime.chat`：它使用版本化 Chat Profile、Message Builder、模型路由、预算、Trace、同一 Dialogue 历史与认知上下文，但不枚举或携带任何 MCP 工具。旧 `/chat` 在迁移期继续兼容，并由 `AGENT_RUNTIME_V2_MODES=chat` 在 Runtime v2 与 Legacy 之间灰度切换。

**请求体：**

```json
{
  "content": "帮我查询关于 Go Agent 的推文",
  "dialogue_id": "0",
  "dialogue_key": "",
  "model_kind_id": "1",
  "preferred_capability_ids": ["platform.search", "content.draft"]
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `content` | string | 是 | 当前用户消息 |
| `dialogue_id` | string | 否 | 旧数字会话 ID 兼容字段 |
| `dialogue_key` | string | 否 | Mongo Dialogue ObjectID；后续消息复用响应值 |
| `model_kind_id` | string | 否 | Chat-capable 模型 ID，省略时使用服务端默认模型 |
| `preferred_capability_ids` | string[] | 否 | Planner 偏好；不是授权或 Tool 白名单 |
| `web_search_provider_config_id` | string | 否 | 当前用户的 `kind=web_search` Provider Config；只用于本次 Run，服务端按 JWT 用户校验所有权 |
| `skill_id` | string | 条件必填 | 版本化 Skill ID；必须与 `skill_version` 同时提供 |
| `skill_version` | string | 条件必填 | 精确不可变版本；服务端不接受 `latest`，也不会按关键词自动选 Skill |

**成功响应 (200 OK)：**

```json
{
  "response": "为你找到了以下站内内容：",
  "dialogue_key": "667f8e03c35b9a1200000001",
  "run_id": "01JZ...",
  "run_status": "completed",
  "execution_profile": "runtime.research_draft",
  "capability_ids": ["platform.search", "content.draft"],
  "execution_strategy_plan": {
    "version": "agent.execution_strategy.v1",
    "template_id": "platform.research_draft.v1",
    "candidate_strategy": "multi_agent",
    "selected_strategy": "single_agent",
    "decision": "fallback",
    "reason_code": "multi_executor_unavailable",
    "complexity_score": 8,
    "complexity_class": "high",
    "complexity_signals": ["capability_composition", "analysis_request", "multiple_outputs", "quality_review"],
    "estimated_latency_millis": 50000,
    "estimated_total_tokens": 24000,
    "estimated_cost_micros": 100000,
    "max_parallel_roles": 1,
    "roles": [
      {
        "role_id": "researcher",
        "capability_ids": ["platform.search"],
        "allowed_tools": ["hybrid_search_tweets"],
        "max_steps": 3,
        "max_total_tokens": 10000,
        "max_estimated_cost_micros": 45000,
        "timeout_millis": 25000
      },
      {
        "role_id": "drafter",
        "capability_ids": ["content.draft"],
        "allowed_tools": [],
        "max_steps": 1,
        "max_total_tokens": 9000,
        "max_estimated_cost_micros": 35000,
        "timeout_millis": 17000
      },
      {
        "role_id": "reviewer",
        "capability_ids": ["content.draft"],
        "allowed_tools": [],
        "max_steps": 1,
        "max_total_tokens": 5000,
        "max_estimated_cost_micros": 20000,
        "timeout_millis": 8000
      }
    ],
    "plan_digest": "sha256..."
  },
  "selected_skill_id": "",
  "selected_skill_version": "",
  "selected_task_template_id": "",
  "selected_task_template_revision": 0,
  "tweet_list": [],
  "publishable_draft": true,
  "tool_activities": [
    {
      "step_index": 1,
      "tool_name": "hybrid_search_tweets",
      "status": "succeeded",
      "result_count": 1
    }
  ],
  "citations": [
    {
      "citation_id": "platform_tweet:2024791560905822208",
      "source_type": "platform_tweet",
      "source_id": "2024791560905822208",
      "url": "/tweet/2024791560905822208",
      "title": "",
      "snippet": "这条推文讨论了 Go Agent Runtime 的工具治理。"
    }
  ],
  "artifacts": [
    {
      "artifact_id": "content.draft:01JZ...",
      "type": "content.draft",
      "status": "ready",
      "content_type": "text/markdown",
      "content": "为你找到并整理了以下站内内容与草稿：",
      "source_run_id": "01JZ...",
      "requires_confirmation": true
    }
  ],
  "approval_state": {
    "status": "not_required",
    "approval_id": "",
    "run_id": "01JZ...",
    "action": "",
    "revision": 0,
    "expires_at": 0,
    "resume_supported": false
  }
}
```

`tool_activities` 只返回脱敏后的步骤、工具名、状态和结果数量，不包含参数、原始 Observation
或内部错误。`citations` 仅由受信任工具的版本化结构结果生成，不从模型最终回答中用文本规则猜测；
`platform_tweet` 表示站内来源；启用 P8.2 Search Provider 后，`web_page` 表示由受信任
`web.search.v1` 或 `web.page.v1` Structured Content 投影的公网来源。公网 Citation 会校验来源
工具、Schema、HTTP(S) URL、私网地址和长度，仍不会信任回答正文中的链接。`page_read`
读取同一 URL 后，可用受限正文摘录替换搜索摘要，但不会产生重复 Citation。

`execution_strategy_plan` 是版本化、可复现的准入证据，不是权限声明。它只包含稳定原因码、
复杂度信号、角色预算和 Tool Scope，不保存原始问题、Prompt、凭据或工具参数。
`AGENT_MULTI_AGENT_PLANNER_ENABLED` 控制准入，`AGENT_MULTI_AGENT_EXECUTION_ENABLED` 独立控制
真实聚合执行并要求 `AGENT_RECOVERABLE_RUNS_ENABLED=true`。执行关闭时，高复杂度候选仍返回
`selected_strategy=single_agent` 和 `reason_code=multi_executor_unavailable`；Planner 默认关闭时
使用 `decision=disabled` 与 `reason_code=multi_feature_disabled`。

当前真实执行只支持 `platform.research_draft.v1` 与 `web.research_draft.v1`，固定顺序为
`researcher -> drafter -> reviewer` 且 `max_parallel_roles=1`。角色 Tool Scope 必须在执行时通过
父 Profile、角色 Profile、当前 Catalog、Tool Policy 和计划范围的交集校验；只有研究角色可调用
只读工具，起草与审校角色无工具。父级共享 Token/成本预算并聚合到同一权威 Run；任一角色失败、
挂起或请求审批都会使父 Run失败，不会在已经产生消耗后自动回退重跑单 Agent。客户端不能根据
计划证据自行授予工具，也不能把当前能力描述为任意角色编排或写工具多 Agent 协作。

`web.search` 使用 `runtime.web_search`，并要求 Run 内存在成功的只读 `web_search` Observation。
联网 Profile 可按需调用只读 `page_read` 核对高价值来源；未配置 Provider 时显式请求该能力
返回不可用，不会退回 Mock 或声称结果正在获取。两种工具的用户/Run 配额身份均由服务端注入，
不会采信模型或 Workflow DSL 提供的计费身份。

`content.draft` 单能力使用 `runtime.draft`。只有草稿消息和来源 Run 均成功持久化时，
`publishable_draft` 才为 `true`，并返回绑定同一 `source_run_id` 的 Artifact。
`requires_confirmation=true` 表示用户仍需通过确认发布接口执行发布，它不等于模型写工具审批。

`skill.run` 只能由请求显式传入完整的 `skill_id + skill_version` 触发；此时
`preferred_capability_ids` 必须省略或精确为 `["skill.run"]`。服务端在规划前和真实工具调用前
都会重新解析当前用户的 Active 发布记录，校验发布 Revision、Workflow Revision、DSL Hash、
Profile/Prompt、预算、单一 Tool 与输出 Schema。成功响应通过
`selected_skill_id/selected_skill_version` 回显实际版本，权威 Agent Run 同样持久化这两个字段。
通过任务模板入口执行时，`selected_task_template_id/selected_task_template_revision` 回显实际
使用的不可变模板版本，并写入权威 Agent Run；普通统一入口不能伪造这两个服务端字段。

`approval_state` 是现有审批事实源的脱敏投影，普通响应永远不返回审批输入、API Key 或一次性
`resume_token`。普通完成请求返回 `status=not_required`；当 Runtime 返回 `ask_human`，或在
`AGENT_RECOVERABLE_RUNS_ENABLED=true`、`AGENT_EXTERNAL_MCP_ENABLED=true` 与
`AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED=true` 下调用需要逐次审批的外部 MCP 工具时，返回
`status=input_required`。工具审批继续复用 14.10 的 Tool Approval 事实源，不创建第二套审批记录。

| 状态码 | 说明 |
|--------|------|
| `400` | 请求、模型、Capability ID 或 Skill 选择参数非法 |
| `404` | 显式 Skill ID/Version 不存在、已停用或版本已因重新发布而失效 |
| `422` | Capability 已知但未启用、当前集合没有精确执行路由，或 Skill 执行绑定在调用前发生漂移 |
| `500` | 模型、检索、持久化或内部执行失败 |

当 Runtime 返回 `ask_human` 时，接口仍返回 `200`，但 `run_status=awaiting_human`，
`response` 是需要用户回答的问题，`approval_state.status=input_required`。工具调用需要审批时，
接口同样返回 `200`，但 `run_status=approval_required`，`approval_state.action=tool_call` 并携带
审批 ID、当前 Run Revision 和过期时间。只有服务端成功加密保存完整 Checkpoint 且对应恢复开关
启用时，`approval_state.resume_supported=true`；调用方必须使用权威 Revision 恢复，不能根据
回答文本或前端状态猜测 Revision。

#### 14.0.1 查询权威 Agent Run

```http
GET /api/v1/agent/runs/:id
```

| 路径参数 | 类型 | 必填 | 说明 |
|---------|------|------|------|
| `id` | string | 是 | `RunAgent` 返回的 `run_id` |

服务端使用 JWT 用户身份做租户隔离，只允许查询当前用户自己的 Run。响应设置
`Cache-Control: no-store`，不返回 Checkpoint 密文、Nonce、Key ID、人工回答、恢复
Attempt ID 或租约。

**成功响应 (200 OK)：**

```json
{
  "run_id": "01JZ...",
  "dialogue_key": "667f8e03c35b9a1200000001",
  "execution_profile": "runtime.web_search",
  "capability_ids": ["web.search"],
  "execution_strategy_plan": {
    "version": "agent.execution_strategy.v1",
    "candidate_strategy": "single_agent",
    "selected_strategy": "single_agent",
    "decision": "selected",
    "reason_code": "single_capability_scope",
    "complexity_score": 0,
    "complexity_class": "low",
    "plan_digest": "sha256..."
  },
  "task_template_id": "",
  "task_template_revision": 0,
  "status": "awaiting_human",
  "revision": 2,
  "resume_supported": true,
  "pending_action_type": "ask_human",
  "pending_action_name": "",
  "pending_action_id": "action_01",
  "approval_id": "",
  "approval_expires_at": 0,
  "step_count": 1,
  "input_tokens": 128,
  "output_tokens": 24,
  "total_tokens": 152,
  "estimated_cost_micros": 0,
  "pricing_version": "",
  "failure_code": "",
  "started_at": 1785200000000,
  "updated_at": 1785200001200,
  "suspended_at": 1785200001200,
  "finished_at": 0
}
```

`status` 可能为 `running`、`completed`、`failed`、`canceled`、`awaiting_human`
或 `approval_required`。`resume_supported=true` 只表示当前记录具备服务端可验证的
人工回答或工具审批恢复条件，不授予 Tool 权限；工具恢复还必须先批准 `approval_id` 并签发
一次性授权。`pending_action_id` 是服务端动作绑定，不应由客户端修改或解释。
正在被某个实例恢复且租约仍有效时返回 `running + resume_supported=false`；如果该实例
崩溃且租约已过期，查询投影会恢复为原来的 `awaiting_human` 或 `approval_required`；只有
Checkpoint、审批和授权条件仍有效时才返回 `resume_supported=true`。响应会给出当前 Revision，
但不会暴露租约、Attempt ID、令牌哈希或 Checkpoint。

| 状态码 | 说明 |
|--------|------|
| `400` | Run ID 为空或请求非法 |
| `404` | Run 不存在，或不属于当前 JWT 用户 |
| `422` | 当前部署未启用权威 Run 仓储 |
| `500` | 状态仓储读取失败 |

#### 14.0.2 恢复 Agent Run

```http
POST /api/v1/agent/runs/:id/resume
```

| 路径参数 | 类型 | 必填 | 说明 |
|---------|------|------|------|
| `id` | string | 是 | 待恢复 Run ID |

一次请求必须且只能选择一种恢复模式。

**人工回答模式请求体：**

```json
{
  "expected_revision": 2,
  "human_response": "只分析当前仓库，不访问外部项目"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `expected_revision` | int64 | 是 | 查询或挂起响应返回的当前 Revision；用于防止重复提交 |
| `human_response` | string | 人工模式必填 | 人工回答，服务端限制为非空且最多 64 KiB |
| `approval_id` | string | 审批模式必填 | 必须等于权威 Run 当前绑定的审批 ID |
| `resume_token` | string | 审批模式必填 | 由 14.0.3 刚签发的一次性短期授权 |

**工具审批模式请求体：**

```json
{
  "expected_revision": 4,
  "approval_id": "66cc...",
  "resume_token": "仅本次签发响应可见的随机授权"
}
```

服务端通过 `status + revision + resume lease + attempt_id` 原子领取执行权。恢复时解密并
校验版本化 Checkpoint，重新解析当前 Profile 与 Prompt 版本，并重新装配当前用户仍获授权的
Tool；Checkpoint 不保存 Tool Definition。人工模式要求待处理动作是 `ask_human`。审批模式还会
校验审批来源为 Runtime、状态已批准且未过期，并精确绑定 User、Run、Action ID、Tool、Category、
输入摘要和稳定幂等键；然后重新读取 Connection、Credential Version、Active Snapshot、Schema
与 Tool Policy。任一条件漂移都 fail-closed。过期租约可以被后续请求重新领取，旧 Attempt 不能
提交结果；一次性授权被成功领取后不能重放。

**成功响应 (200 OK)：** 使用 14.0 的 `RunAgentResponse`。如果模型再次请求人工输入或工具
审批，会返回新的挂起状态与新 Revision；完成时返回 `completed`。恢复不会重放 Checkpoint 中
已成功的旧 Step。审批恢复只保存 assistant 结果，不把审批动作伪造成新的用户消息。

| 状态码 | 说明 |
|--------|------|
| `400` | Run ID、Revision 非法，未选择唯一完整模式，或人工回答非法 |
| `404` | Run 不存在，或不属于当前 JWT 用户 |
| `409` | Revision 已过期、Run 已被其他请求领取，或状态已变化 |
| `422` | Feature Flag/Checkpoint/Runner 不可用，动作不可恢复，审批无效，或重新授权失败 |
| `500` | 解密、模型执行、对话持久化或状态提交失败 |

#### 14.0.3 签发 Agent 工具审批恢复授权

```http
POST /api/v1/agent/tool-approvals/:id/agent-resume-grant
```

调用前必须先通过 14.10.10 把同一审批置为 `approved`。仅当前登录用户可以为自己已批准、未过期、
`source=runtime` 且仍与 `approval_required` Agent Run 精确绑定的审批签发授权。

**请求体：**

```json
{
  "expected_run_revision": 3
}
```

**成功响应 (200 OK)：**

```json
{
  "run": {
    "run_id": "01JZ...",
    "status": "approval_required",
    "revision": 4,
    "resume_supported": true,
    "pending_action_type": "tool_call",
    "pending_action_name": "mcp_01.create_issue",
    "pending_action_id": "action_02",
    "approval_id": "66cc...",
    "approval_expires_at": 1785200300
  },
  "resume_token": "仅本次响应可见的随机授权",
  "expires_at": 1785200300
}
```

签发前会重新校验 Checkpoint 与当前 Profile、Connection、Snapshot、Schema、Policy 和审批绑定。
服务端只保存令牌 SHA-256；每次成功签发按 Run Revision 原子轮换，旧令牌立即失效。响应设置
`Cache-Control: no-store, max-age=0` 与 `Pragma: no-cache`。客户端应把令牌保留在当前调用栈，
立即调用 14.0.2，禁止写入 LocalStorage、SessionStorage、日志或业务数据库。

| 状态码 | 说明 |
|--------|------|
| `400` | 审批 ID 或 `expected_run_revision` 非法 |
| `404` | 审批或 Run 不存在，或不属于当前 JWT 用户 |
| `409` | Revision、Run 状态或审批绑定已变化 |
| `422` | Feature Flag/Checkpoint/审批/当前 Tool 授权不可用 |
| `500` | 状态仓储、加密或授权签发失败 |

#### 14.0.4 显式任务模板（P8.4）

任务模板是用户主动编写的可复用 `RunAgent` 预设，不是 Workflow DAG，也不会自动复制或解析
整段对话。模板必须绑定当前用户一个 `completed` 权威 Agent Run，并固化该 Run 的
`execution_profile`、Capability 集合、精确 Skill 版本（如有）、结果摘要和 Profile/Prompt
版本证据。模板不保存源问题、源回答、Checkpoint、Provider API Key 或 Web/MCP 凭据。

创建和执行由 `AGENT_TASK_TEMPLATES_ENABLED` 独立控制，默认关闭，并要求
`AGENT_RECOVERABLE_RUNS_ENABLED=true`。关闭执行开关不会删除模板；列表和显式归档仍可用。

**从成功 Run 创建：**

```http
POST /api/v1/agent/runs/:id/task-templates
```

```json
{
  "expected_source_run_revision": 2,
  "name": "技术主题研究摘要",
  "description": "复用已验证的研究与归纳能力",
  "instruction_template": "研究以下主题并给出结构化摘要：{{input}}",
  "idempotency_key": "8a311b92-8447-4f58-bb8a-91182b635c42"
}
```

`instruction_template` 必须且只能包含一个字面量 `{{input}}`，不接受其他占位符。名称最多
80 个字符，描述最多 500 个字符，指令最多 12 KiB。`expected_source_run_revision` 防止在
旧 Run 视图上创建；`idempotency_key` 用于安全重试，同一用户和 Key 携带不同内容返回
`409 Conflict`。只有状态为 `completed`、Revision 精确匹配且具有结果摘要的源 Run 才能创建。

成功返回 `201 Created`：

```json
{
  "task_template": {
    "contract_version": "agent.task_template.v1",
    "template_id": "66dd00112233445566778899",
    "name": "技术主题研究摘要",
    "description": "复用已验证的研究与归纳能力",
    "instruction_template": "研究以下主题并给出结构化摘要：{{input}}",
    "status": "active",
    "revision": 1,
    "source_run_id": "01JZ...",
    "source_run_revision": 2,
    "source_result_digest": "sha256...",
    "source_execution_profile": "runtime.web_search",
    "capability_ids": ["web.search"],
    "skill_id": "",
    "skill_version": "",
    "source_model": "qwen-plus",
    "created_at": 1785400000000,
    "updated_at": 1785400000000,
    "archived_at": 0
  }
}
```

**列出当前 Active 模板：**

```http
GET /api/v1/agent/task-templates?limit=20
```

```json
{
  "execution_enabled": true,
  "task_templates": []
}
```

列表按 JWT 用户隔离，`limit` 范围为 `1..100`。`execution_enabled=false` 表示当前部署只允许
查看和归档，不能创建或执行；它不会把已存模板误报为可运行。

**执行一个精确模板版本：**

```http
POST /api/v1/agent/task-templates/:id/run
```

```json
{
  "expected_revision": 1,
  "input": "Go Agent Runtime 的预算与审批设计",
  "dialogue_id": "0",
  "dialogue_key": "",
  "model_kind_id": "1",
  "web_search_provider_config_id": ""
}
```

服务端重新读取模板和源 Run，逐项核验完成状态、Revision、结果摘要、执行 Profile、
Capability 和 Skill 版本，再把用户输入替换到唯一 `{{input}}`。渲染结果最多 20 KiB。
随后仍通过统一 Catalog、Profile、Tool Policy、Budget、Approval 与 Runtime 执行；模板不能
授予新权限。模型和可选 Web Provider Config 由用户在本次请求选择，不会固化到模板。
执行 Profile、Capability 或 Skill 绑定发生漂移时 fail-closed，不降级到普通对话。

成功响应复用 14.0 的 `RunAgentResponse`，并设置
`selected_task_template_id/selected_task_template_revision`。新权威 Run 同样持久化这两个字段。

**归档：**

```http
DELETE /api/v1/agent/task-templates/:id?expected_revision=1
```

归档使用 Revision CAS，把模板移出 Active 列表但不删除源 Run 证据。归档后旧客户端执行返回
`409 Conflict`。

| 状态码 | 说明 |
|--------|------|
| `400` | 模板字段、占位符、输入、ID 或 Revision 非法 |
| `404` | 模板或源 Run 不存在，或不属于当前 JWT 用户 |
| `409` | 模板/源 Run Revision 已变化、模板已归档或幂等键内容冲突 |
| `422` | Feature Flag/权威 Run 仓储不可用、源 Run 未完成、执行路由或 Skill 绑定漂移 |
| `500` | 模板仓储、模型、对话持久化或内部执行失败 |

#### 14.0.5 Agent Run 聚合用量（P8.4）

```http
GET /api/v1/agent/runs/:id/accounting?child_limit=50
```

该接口按 JWT 用户隔离，读取一个权威 Agent Run 以及由它直接调用的 Workflow Run。它返回
父 Agent 自身、直接子 Workflow 和两者合计的 Token/估算成本，并保留各自预算上限及消费量。
这是只读观测视图，不会把父子独立预算改成一个共享准入上限，也不会递归统计更深层级。

`child_limit` 可省略，默认 50，范围 `1..200`。查询使用
`user_id + parent_run_id + started_at + _id` 索引；响应不包含 Prompt、Completion、
Workflow `input_json/output_json`、工具参数、Checkpoint 或恢复凭据。

```json
{
  "run_id": "01JZ...",
  "run_status": "completed",
  "scope": "direct_children.v1",
  "state": "complete",
  "complete": true,
  "truncated": false,
  "child_run_count": 1,
  "included_child_run_count": 1,
  "accounting_version": "execution.accounting.v1",
  "parent_usage": {
    "input_tokens": 1200,
    "output_tokens": 300,
    "total_tokens": 1500,
    "estimated": false,
    "estimated_cost_micros": 4200,
    "cost_estimated": false,
    "pricing_version": "catalog-2026-07"
  },
  "parent_budget": {
    "max_steps": 12,
    "max_total_tokens": 30000,
    "max_estimated_cost_micros": 120000,
    "consumed_steps": 3,
    "consumed_tokens": 1500,
    "consumed_cost_micros": 4200
  },
  "child_usage": {
    "input_tokens": 800,
    "output_tokens": 200,
    "total_tokens": 1000,
    "estimated": true,
    "estimated_cost_micros": 2800,
    "cost_estimated": true,
    "pricing_version": "catalog-2026-07"
  },
  "total_usage": {
    "input_tokens": 2000,
    "output_tokens": 500,
    "total_tokens": 2500,
    "estimated": true,
    "estimated_cost_micros": 7000,
    "cost_estimated": true,
    "pricing_version": "catalog-2026-07"
  },
  "children": [
    {
      "run_id": "66ee...",
      "workflow_id": "66dd...",
      "parent_action_id": "action_02",
      "status": "success",
      "state": "complete",
      "accounting_version": "execution.accounting.v1",
      "usage": {},
      "budget": {},
      "started_at_ms": 1785500000000,
      "suspended_at_ms": 0,
      "finished_at_ms": 1785500002400
    }
  ]
}
```

新运行把精确或估算 Usage、Pricing Version、预算上限和消费快照直接写入运行记录，并标记
`execution.accounting.v1`。旧记录不从业务 JSON 或 Trace 反推：缺少版本标记时返回
`unavailable`；父/子仍在运行或挂起、混有旧记录时返回 `partial`；超过查询上限时
`truncated=true` 且 `complete=false`。不同 Pricing Version 的合计会返回 `mixed`。

| 状态码 | 说明 |
|--------|------|
| `400` | Run ID 或 `child_limit` 非法 |
| `404` | 父 Agent Run 不存在或不属于当前 JWT 用户 |
| `422` | Recoverable Run 或核算只读仓储不可用 |
| `500` | 父 Run 或直接子 Workflow Run 查询失败 |

旧 `/chat`、`/consult`、`/assist`、`/multi` 与 Workflow API 在迁移期继续兼容。

---

### 14.1 直接 AI 对话 (模式一)

```
POST /api/v1/agent/chat
```

**请求体：**

```json
{
  "content": "你好",
  "dialogue_id": "3553550178352795156",
  "model_kind_id": 1
}
```

*注：首次发起新对话时 `dialogue_id` 可传空字符串 `""` 或 `"0"`，后续追加对话需携带首轮返回的 dialogue_id*

**成功响应 (200 OK)：**

```json
{
  "response": "你好！我是你的 AI 助手，有什么可以帮你的吗？"
}
```

---

### 14.2 语义搜索推文和作者 (模式二)

```
POST /api/v1/agent/consult
```

**请求体：**

```json
{
  "content": "帮我搜搜关于 Go 1.25 的推文",
  "dialogue_id": "3553550178352795156",
  "model_kind_id": 1
}
```

**成功响应 (200 OK)：**

```json
{
  "response": "为您找到了以下关于 Go 1.25 的推文：",
  "tweet_list": [
    {
      "tweet_id": "2024791560905822208",
      "url": "/tweet/2024791560905822208",
      "summary": "作者讨论了 Go 1.25 编译器在性能上的提升以及新的 GC 优化。"
    }
  ]
}
```

---

### 14.3 协作构建推文 (模式三 - 阶段一)

```
POST /api/v1/agent/assist
```

**请求体：**

```json
{
  "content": "写一篇关于 K8s Ingress 的推文",
  "dialogue_id": "3553550178352795156",
  "model_kind_id": 1
}
```

**成功响应 (200 OK)：**

```json
{
  "response": "这是为您生成的推文草稿选项：",
  "dialogue_key": "667f8e03c35b9a1200000001",
  "run_id": "run-assist-01JZ...",
  "tweet_list": [
    {
      "id": "2024791560905822210",
      "user_id": "123",
      "content": "K8s Ingress 是管理外部访问的利器，看看怎么配置...",
      "media_urls": null,
      "type": 0,
      "visible_type": 0,
      "created_at": 1708000000,
      "updated_at": 1708000000,
      "like_count": 0,
      "comment_count": 0,
      "share_count": 0,
      "is_liked": false,
      "parent_id": "0"
    }
  ]
}
```

---

### 14.4 确认发布推文 (模式三 - 阶段二)

```
POST /api/v1/agent/confirm
```

**请求体：**

```json
{
  "content": "用户选择并编辑后的推文内容",
  "source_run_id": "run-assist-01JZ..."
}
```

`source_run_id` 应使用阶段一返回的 Assist Run ID。服务端会校验该 Run 属于当前用户、模式为
`assist` 且已完成；旧客户端可暂时省略该字段，但不会形成 Profile 实验产品结果归因。

**成功响应 (200 OK)：**

```json
{
  "response": "发布成功",
  "tweet_id": "2024791560905822209"
}
```

来源 Run 无效、越权或状态不满足时返回 `422 Unprocessable Entity`。相同来源 Run 与相同正文
会使用稳定幂等键调用 TweetService。只有 TweetService 成功后，服务端才尝试旁路记录
`draft_published=true`；归因失败不回滚已经创建的推文。启用 Profile 实验时还会创建默认 7 天的
短期 Tweet/Run 映射，首个外部点赞或评论可按来源 Run 幂等记录 `content_engaged=true`。
自赞、过期互动、未互动和重复消息不会生成新结果；窗口可由
`AGENT_PROFILE_CONTENT_ATTRIBUTION_WINDOW` 配置。该异步事件不改变本接口响应契约。

---

### 14.5 多 Agent 协同深度创作 (模式四)

```
POST /api/v1/agent/multi
```

**请求体：**

```json
{
  "domain": "技术分享",
  "author_user_id": "123",
  "style_ratio": 0.8,
  "reference_tweet_ids": ["2024791560905822208"],
  "content": "补充的主题信息"
}
```

**成功响应 (200 OK)：**

```json
{
  "response": "Markdown 格式的深度研究推文推荐及舆情审查意见"
}
```

---

### 14.6 获取历史对话列表

```
GET /api/v1/agent/dialogues
```

**成功响应 (200 OK)：**

```json
{
  "code": 200,
  "msg": "success",
  "repository_dialogue_list": [
    {
      "id": "667f8e03c35b9a1200000001",
      "dialogue_key": "667f8e03c35b9a1200000001",
      "legacy_id": "3553550178352795156",
      "user_id": "123",
      "title": "关于 K8s Ingress 的讨论"
    }
  ]
}
```

---

### 14.7 获取特定对话消息历史

```
GET /api/v1/agent/dialogues/:id/messages
```

**路径参数：**

| 参数   | 类型   | 说明        |
| ------ | ------ | ----------- |
| `id` | string | 对话会话 ID |

**成功响应 (200 OK)：**

```json
{
  "code": 200,
  "msg": "success",
  "messages": [
    {
      "id": "3553550178352795157",
      "user_id": "123",
      "dialogue_id": "3553550178352795156",
      "dialogue_key": "667f8e03c35b9a1200000001",
      "role": "user",
      "content": "你好",
      "question": "你好",
      "response": ""
    },
    {
      "id": "3553550178352795158",
      "user_id": "123",
      "dialogue_id": "3553550178352795156",
      "dialogue_key": "667f8e03c35b9a1200000001",
      "role": "assistant",
      "content": "你好！我是你的 AI 助手...",
      "question": "",
      "response": "你好！我是你的 AI 助手...",
      "run_id": "",
      "publishable_draft": false
    }
  ]
}
```

客户端应优先读取 `role/content`。`question/response` 是兼容旧客户端的过渡字段。Assist 生成的
assistant 消息会额外返回 `run_id` 与 `publishable_draft=true`，用于恢复“选择草稿、编辑并显式发布”
操作；普通对话和用户消息不会被标记为可发布草稿。

---

### 14.8 显式结束对话 Session

结束对话不会删除历史消息。服务端会停止该会话的空闲摘要 Timer，取消并等待在途摘要任务释放租约，再同步结晶尚未处理的消息。

```
POST /api/v1/agent/dialogues/:id/end
```

**路径参数：**

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | 对话会话 ID |

**成功响应 (200 OK)：**

```json
{
  "code": 200,
  "msg": "success"
}
```

该接口保留对话，不代表删除或清空上下文。重复调用是幂等的；并发调用最多推进一个摘要版本。

---

### 14.9 获取可用模型信息

```
GET /api/v1/agent/models
```

**成功响应 (200 OK)：**

```json
{
  "code": 200,
  "msg": "success",
  "model_kind_list": [
    {
      "id": 1,
      "name": "qwen-plus",
      "description": "阿里云百炼 Qwen 大语言模型，用于对话、推理和推文生成",
      "max_tokens": 32768,
      "file_kind_list": [
        {
          "id": 1,
          "name": "pdf"
        }
      ]
    }
  ]
}
```

该接口只返回支持 Chat Completion 的语言模型。Embedding、Reranker 等基础设施模型不会出现在 AI 助手模型选择器中。

---

### 14.10 智能解析上传文件

```
POST /api/v1/agent/files/analysis
```

**Content-Type**: `multipart/form-data`

**Form Data:**

- `file`: 文件二进制
- `file_kind_id`: 文件类型 ID

**成功响应 (200 OK)：**

```json
{
  "code": 200,
  "msg": "success",
  "parsed_content": "文件解析后的纯文本内容...",
  "file_key": "user_uploads/123/xxx.pdf"
}
```

---

### 14.11 可视化工作流 DSL 接口

所有接口均需要 `Authorization: Bearer <token>`。`workflow_id` 与 `run_id` 使用字符串传输，避免前端数字精度丢失。

#### 14.10.1 创建工作流

```
POST /api/v1/agent/workflows
```

**请求体：**

```json
{
  "name": "高定制化 AI 发推助手",
  "dsl": {
    "name": "高定制化 AI 发推助手",
    "budget": {
      "max_node_executions": 50,
      "max_parallel_nodes": 8,
      "timeout_sec": 300,
      "max_total_tokens": 120000,
      "max_estimated_cost_micros": 0
    },
    "nodes": [
      {
        "id": "start",
        "type": "start",
        "properties": {},
        "timeout_sec": 30
      },
      {
        "id": "node_llm_01",
        "type": "llm",
        "properties": {
          "prompt": "帮我重新润色: {{start.user_input}}"
        },
        "retry": {
          "max_attempts": 3,
          "initial_backoff_ms": 100,
          "max_backoff_ms": 2000,
          "multiplier": 2,
          "jitter": 0.1
        },
        "writes": [
          {
            "path": "shared.drafts",
            "source": "text",
            "reducer": "append"
          }
        ],
        "timeout_sec": 15
      },
      {
        "id": "node_tweet_01",
        "type": "tool",
        "properties": {
          "tool_name": "PublishTweet",
          "content": "{{node_llm_01.text}}"
        },
        "timeout_sec": 10
      }
    ],
    "edges": [
      {
        "id": "e1",
        "source": "start",
        "target": "node_llm_01",
        "source_handle": "output",
        "target_handle": "input"
      }
    ]
  }
}
```

也可传 `dsl_json` 字符串字段，网关会校验其必须是合法 JSON。

节点可通过顶层 `writes` 将本节点输出字段写入两段式全局状态路径。`source` 为空时默认使用 `path` 的字段名；`reducer` 支持 `append`、`sum`、`min`、`max`、`merge`、`first`、`last`。没有依赖关系的多个节点写入同一路径时，所有写入必须声明相同且非空的 Reducer，否则 Compile 阶段拒绝该 DSL。Reducer 由协调器按 DSL 节点声明顺序执行，不受 Goroutine 完成顺序影响；其中数值 Reducer 将 JSON 数值归一为 number，`merge` 仅接受字符串键对象，后声明节点的同名键覆盖先声明节点。

节点顶层 `retry` 的 `max_attempts` 表示总尝试次数，范围为 1-10；未配置 Retry 时只执行一次。启用重试但省略退避字段时，默认初始退避 100ms、最大退避 5s、倍数 2。`jitter` 范围为 0-1，由节点 ID 和失败尝试次数确定性生成，不使用随机数改变 Replay 语义。Scheduler 仅重试显式实现 `IsRetryable()` 的错误或临时网络错误；业务校验错误、挂起、取消和 Deadline 超时不会重试。节点总 `timeout_sec` 同时约束执行尝试与退避等待。运行结果中的节点 Trace 会返回 `attempt`、`max_attempts` 及 `running/retrying/success/failed/skipped/suspended/canceled/timed_out` 状态。

DSL 顶层 `budget` 是整个 Workflow Run 共享的硬预算，不属于单个节点。`max_node_executions` 统计实际节点尝试次数，重试会再次计数，未激活的分支不会计数；`max_parallel_nodes` 限制同时执行的节点处理器；`timeout_sec` 为工作流执行超时；`max_total_tokens` 和 `max_estimated_cost_micros` 由所有 LLM、ReAct 和 Plan-Execute 调用共同消费。并发分支会在网络调用前预留 Token/成本，防止各分支分别通过检查后合计超额。Provider 返回 Usage 时按真实值入账；未返回时使用启发式估算并标记 `estimated`。已经发生的模型调用即使实际用量超过预算也会保留真实消耗，然后以 `budget_exceeded` 终止。

默认值依次为 50 次节点尝试、8 个并发节点、300 秒、120000 Token，成本上限默认 0（关闭成本拦截）。服务端硬上限依次为 1000、64、3600 秒、10000000 Token 和 1000000000000 微单位。成本预算启用后，所选模型必须存在可用 Pricing/Cost Estimator；未知定价会在模型调用前拒绝，不能把未知成本当作 0。最终、失败和挂起 Run 的 `output.budget` 会返回 `node_executions`、累计 `usage` 与当前 `reserved` 快照；持久 Checkpoint 恢复会继续累计，不会重置已消费额度。

Tool 节点可声明顶层 `compensation`。补偿工具必须存在于当前 Tool Registry；`properties` 可引用源节点自身输出或其上游节点输出，不能读取未来节点或无依赖路径的并行节点。补偿的 `timeout_sec` 和 `retry` 独立于正向节点。工作流失败时只为已成功节点生成反向拓扑补偿计划，计划先持久化后执行；补偿工具继续经过统一 Tool Policy、审批、熔断和幂等结果回放。

以下是扩展 Tool Catalog 注册 `ReserveResource/ReleaseResource` 后的补偿片段；它不代表内置社交工具已经提供删除推文能力：

```json
{
  "id": "reserve_resource",
  "type": "tool",
  "properties": {"tool_name": "ReserveResource", "sku": "{{start.sku}}"},
  "compensation": {
    "tool_name": "ReleaseResource",
    "properties": {"reservation_id": "{{reserve_resource.reservation_id}}"},
    "timeout_sec": 15,
    "retry": {"max_attempts": 3, "initial_backoff_ms": 200, "max_backoff_ms": 2000}
  }
}
```

**成功响应：**

```json
{
  "workflow": {
    "workflow_id": "66aa...",
    "user_id": "123",
    "name": "高定制化 AI 发推助手",
    "dsl": {},
    "dsl_json": "{}",
    "current_revision_id": "66ab...",
    "current_revision_number": 1,
    "current_dsl_hash": "sha256-hex",
    "created_at": 1782380000,
    "updated_at": 1782380000
  }
}
```

#### 14.10.2 更新工作流

```
PUT /api/v1/agent/workflows/:id
```

请求体同创建接口。后端会按当前登录用户过滤所有权。每次成功更新都会创建新的不可变 Workflow Revision；`agent_workflows` 只保存当前视图和 Revision 指针，历史 Revision 不会被覆盖。

#### 14.10.3 查询工作流列表

```
GET /api/v1/agent/workflows?page=1&page_size=20
```

**成功响应：**

```json
{
  "workflows": [
    {
      "workflow_id": "66aa...",
      "user_id": "123",
      "name": "高定制化 AI 发推助手",
      "current_revision_id": "66ab...",
      "current_revision_number": 1,
      "current_dsl_hash": "sha256-hex",
      "created_at": 1782380000,
      "updated_at": 1782380000
    }
  ],
  "total": 1
}
```

#### 14.10.4 查询工作流详情

```
GET /api/v1/agent/workflows/:id
```

响应结构同创建接口。

#### 14.10.5 执行工作流

```
POST /api/v1/agent/workflows/:id/run
```

**请求体：**

```json
{
  "input": {
    "user_input": "分析 Go 工作流引擎的设计取舍",
    "dialogue_key": "",
    "persist_dialogue": true
  }
}
```

也可传 `input_json` 字符串字段。非法 JSON 会返回 400。

`persist_dialogue` 默认关闭，工作流编辑器的测试运行不会污染 AI 助手历史。AI 助手的“自定义工作流”模式会显式开启该字段，并通过 `dialogue_key` 续接会话。

**成功响应：**

```json
{
  "dialogue_key": "667f8e03c35b9a1200000001",
  "response": "工作流最终可展示文本",
  "run": {
    "run_id": "66bb...",
    "workflow_id": "66aa...",
    "workflow_revision_id": "66ab...",
    "workflow_revision_number": 1,
    "state_version": 4,
    "user_id": "123",
    "status": "success",
    "input": {},
    "input_json": "{}",
    "output": {},
    "output_json": "{}",
    "error_message": "",
    "started_at": 1782380000,
    "finished_at": 1782380008
  }
}
```

Run 创建时会固定引用当时的 `workflow_revision_id`。之后即使用户继续编辑工作流，已创建 Run 的挂起恢复、审计和输出仍使用原 Revision。`revision` 是 Run 记录自身的并发控制版本；`workflow_revision_number` 是 DSL 版本，两者语义不同。`state_version` 是 Blackboard 已持久化的最后事件序号。

`status` 取值为 `running`、`canceling`、`canceled`、`suspended`、`success`、`failed`、`rejected`、`compensating`、`compensated`、`compensation_failed`。节点执行失败时接口仍会返回运行记录，并在 `status` 与 `error_message` 中呈现失败原因。`compensated` 表示主流程失败或取消后已完成全部补偿，不表示主流程成功；原始失败或取消原因会保留。

`output` 会保留各节点 blackboard 顶层字段，同时新增：

```json
{
  "blackboard": {
    "node_llm_01": {
      "text": "生成结果"
    }
  },
  "traces": [
    {
      "node_id": "node_llm_01",
      "node_type": "llm",
      "status": "success",
      "started_at": 1782380000,
      "finished_at": 1782380002,
      "duration_ms": 2048
    }
  ]
}
```

当前 DSL 支持以下能力：

| 节点类型 | 能力 |
| --- | --- |
| `llm` | 对话、创作、Planner 规划 |
| `agent` | `ReActAgent`、`PlanExecutor`，最多 8 轮，只允许调用只读 MCP 工具 |
| `tool` | `PublishTweet`、`SemanticTweetSearch`、`HybridTweetSearch`、`SearchUsers`、`GetUserTweets`、`GetTweetsByIDs` |
| `router` / `wait` | 条件分支、人工审批与挂起恢复 |

策略节点不会隐式调用发布工具。所有写操作必须以独立 `PublishTweet` 节点呈现，并继续接受认证上下文注入与运行确认。

`traces[].status` 取值为 `pending`、`running`、`success`、`failed`、`skipped`。

#### 14.10.5.1 发布为 Agent 工具（P8.4）

将某个工作流的不可变 Revision 显式发布到当前用户的 Unified Agent 工具目录：

```text
PUT /api/v1/agent/workflows/:id/tool-publication
```

```json
{
  "workflow_revision_id": "66ab...",
  "description": "读取输入并生成结构化摘要。",
  "input_schema": {
    "type": "object",
    "properties": {
      "user_input": {
        "type": "string",
        "minLength": 1,
        "maxLength": 12000
      }
    },
    "required": ["user_input"],
    "additionalProperties": false
  },
  "expected_revision": 0
}
```

`workflow_revision_id` 可省略，此时绑定工作流当前 Revision。创建时 `expected_revision=0`；更新已存在的发布记录时必须传当前 `revision`，冲突返回 `409`。发布成功后，继续保存工作流草稿或生成新 Revision 不会改变 Agent 实际调用的版本，用户必须显式更新发布记录。

```json
{
  "publication": {
    "publication_id": "66ac...",
    "user_id": "123",
    "workflow_id": "66aa...",
    "workflow_revision_id": "66ab...",
    "workflow_revision_number": 3,
    "workflow_dsl_hash": "sha256-hex",
    "tool_name": "workflow_66aa00000000000000000001",
    "display_name": "研究摘要",
    "description": "读取输入并生成结构化摘要。",
    "input_schema": {},
    "input_schema_json": "{}",
    "status": "active",
    "revision": 1,
    "created_at": 1782380000,
    "updated_at": 1782380000
  }
}
```

查询或停用发布记录：

```text
GET    /api/v1/agent/workflows/:id/tool-publication
DELETE /api/v1/agent/workflows/:id/tool-publication?expected_revision=1
```

该能力由 `AGENT_WORKFLOW_AS_TOOL_ENABLED` 独立控制，默认关闭；关闭时新发布请求失败，但仍允许查询和停用已有记录。首个版本只允许无补偿、无 `wait`、无 `agent`、无递归 Workflow Tool，且全部节点工具均为平台确认的只读能力的 DAG。未知、`risky`、`write` 或外部 MCP 非只读节点均在发布和真实调用前 fail-closed。Input Schema 不允许 `$ref`，也不能声明 `user_id`、`run_id`、`parent_run_id` 等平台身份字段。

Unified Agent 只会看到当前用户的 Active 发布记录。调用仍经过统一 `ToolExecutor` 的 Schema、超时、幂等结果、熔断、结果体积、审计与 Trace 治理；子 Workflow Run 记录 `invocation_source=runtime`、`parent_run_id` 和 `parent_action_id`。父 Agent Run 与子 Workflow Run 各自保留预算账本，父工具超时同时约束子运行；当前版本不把两者伪装成一个聚合成本账本。

#### 14.10.5.2 版本化 Skill 目录（P8.4）

Skill 目录是当前用户 Active Workflow-as-Tool 发布记录的只读投影，不新增第二套可漂移的 Skill
存储。每个版本固定绑定说明、单一允许工具、Profile/Prompt 版本、Token/成本/步骤/超时预算、
输出 JSON Schema，以及 Publication/Workflow Revision 和 DSL Hash。

```text
GET /api/v1/agent/skills?limit=20
GET /api/v1/agent/skills/:id?version=v1-<sha256>
```

列表默认 20 条，最大 100 条。按 ID 查询必须传 `version`，服务端没有“取最新版本”的隐式语义。
响应使用 `Cache-Control: no-store`：

```json
{
  "skills": [{
    "id": "workflow.66aa00000000000000000001",
    "version": "v1-7cf9...",
    "name": "研究摘要",
    "description": "读取输入并生成结构化摘要。",
    "instructions": "仅执行已绑定的工作流，并按输出契约返回结果。",
    "allowed_tools": ["workflow_66aa00000000000000000001"],
    "profile": {
      "profile_id": "workflow.skill",
      "profile_version": "v1",
      "prompt_template_id": "workflow.skill.system",
      "prompt_template_version": "v1"
    },
    "budget": {
      "max_steps": 2,
      "max_input_tokens": 12000,
      "max_output_tokens": 4000,
      "max_total_tokens": 16000,
      "max_cost_micros": 200000,
      "timeout_seconds": 75
    },
    "output": {
      "schema_id": "workflow.run.v1",
      "schema": {}
    },
    "workflow": {
      "publication_id": "66ac...",
      "publication_revision": 1,
      "workflow_id": "66aa...",
      "workflow_revision_id": "66ab...",
      "workflow_revision_number": 3,
      "workflow_dsl_hash": "sha256-hex",
      "tool_name": "workflow_66aa00000000000000000001"
    }
  }]
}
```

停用或更新 Workflow 发布记录会立即使旧 Skill 版本不可解析；历史 Agent Run 仍保留原
`skill_id/skill_version` 审计字段，但不能借此重获执行权限。`AGENT_SKILL_CATALOG_ENABLED`
默认关闭且要求 `AGENT_WORKFLOW_AS_TOOL_ENABLED=true`；关闭 Skill 开关即可撤销目录与
`runtime.skill` 路由，不修改发布元数据，也不影响普通对话和现有 Workflow-as-Tool 能力。

#### 14.10.5.3 租户扩展目录（P8.5）

扩展目录在一个只读页面中合并内建 Capability、当前用户精确版本 Skill，以及当前用户或
其 Agent Project 可访问且已经审核并启用的 MCP Tool：

```text
GET /api/v1/agent/extensions
  ?kind=capability|skill|mcp_tool
  &category=general|workflow|read|write|risky
  &scope=platform|user|project
  &status=available|planned
  &search=crm
  &after_cursor=<opaque>
  &page_size=20
```

`page_size` 默认 20，最大 50。`after_cursor` 是不透明稳定 Cursor，并绑定前一页的过滤与
搜索条件；改变条件后必须从第一页重新查询。用户身份只取自 JWT Context，客户端不能用
Query 或 Body 指定其他 `user_id`。成功响应使用 `Cache-Control: no-store`：

```json
{
  "contract_version": "agent.extension.v1",
  "extensions": [{
    "extension_id": "mcp_tool_1d9e...",
    "kind": "mcp_tool",
    "name": "crm.create_record",
    "display_name": "CRM / create_record",
    "description": "Create a CRM record.",
    "version": "snapshot-3-a1b2c3d4e5f6",
    "source": "external_mcp",
    "capability_id": "connector.mcp",
    "category": "write",
    "scope": "project",
    "status": "available",
    "approval_mode": "required",
    "health_status": "healthy",
    "mcp": {
      "connection_id": "mcpconn_...",
      "server_id": "crm",
      "snapshot_id": "mcpsnap_...",
      "qualified_tool_name": "crm.create_record"
    }
  }],
  "sources": [
    {"source": "built_in", "state": "ready", "entry_count": 7},
    {"source": "external_mcp", "state": "ready", "entry_count": 1},
    {"source": "workflow", "state": "disabled", "entry_count": 0}
  ],
  "next_cursor": "eyJ2IjoxLC4uLn0",
  "has_more": true
}
```

Skill 条目携带精确 `skill.skill_id + skill.version`；使用前仍调用精确 Skill 解析并重校验当前
发布绑定。MCP 条目只携带连接、Server、Snapshot 和 Qualified Tool 引用，不返回 Endpoint、
Credential、输入 Schema 或 Tool Result。真实 MCP 调用仍经过成员权限、Snapshot/Policy、
Runtime Budget、ToolExecutor 和 Approval，目录本身不授予执行权限。

该 API 由 `AGENT_EXTENSION_CATALOG_ENABLED` 独立控制，默认关闭；关闭时返回前置条件失败，
不删除任何 Capability、Workflow Publication、Skill 或 MCP 数据。当前只表示“租户已安装
扩展目录”，不表示公共市场、扩展安装或发布者信任已实现。

#### 14.10.5.4 签名公共扩展市场（P8.5）

公共市场当前只提供经过发布者身份和 Ed25519 签名复验的只读版本目录：

```text
GET /api/v1/agent/marketplace/extensions
  ?kind=skill|mcp_server
  &publisher_id=publisher.example
  &search=research
  &after_cursor=<opaque>
  &page_size=20
```

`page_size` 缺省使用服务端配置，范围为 1-50。Cursor 同类型、发布者和搜索条件绑定，
不能跨查询复用。Gateway 只接受 JWT 中的用户身份，忽略客户端伪造的用户字段，并返回
`Cache-Control: no-store`。

```json
{
  "contract_version": "agent.extension_marketplace.v1",
  "releases": [{
    "contract_version": "agent.extension_release.v1",
    "release_id": "extrel_0123456789abcdef0123456789abcdef",
    "package_id": "com.example.research-skill",
    "kind": "skill",
    "version": "1.2.0",
    "display_name": "Research Skill",
    "description": "Structured research workflow",
    "publisher": {
      "publisher_id": "publisher.example",
      "display_name": "Example Publisher",
      "verification": "verified"
    },
    "artifact_digest_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    "signature_key_id": "publisher-key-2026-01",
    "capability_ids": ["web.research_draft.v1"],
    "requested_permissions": ["network", "user_data_read"],
    "published_at_unix_ms": 1785686400000,
    "signature_verified": true
  }],
  "next_cursor": "",
  "has_more": false
}
```

服务端只返回 `stable` 版本，并在每次读取时用当前可信发布者的 Active/Retired 公钥重算规范
Manifest、Release ID 和 Ed25519 签名；发布者被暂停、密钥被撤销、记录被篡改或发布者缺失
都会整页 fail-closed。响应不包含公钥、原始签名、Artifact URL/字节、Endpoint、Credential
或安装授权；摘要和权限声明仅供未来安装审核，当前不授予任何执行权限。

该 API 由 `AGENT_EXTENSION_MARKETPLACE_ENABLED=false` 独立控制，关闭时不影响已经保存的
发布者与版本记录，也不修改租户已安装目录。发布者控制面使用另一开关，可以在公开目录关闭时
先完成版本编排。安装审批、依赖解析、恶意包扫描、Artifact 分发与租户安装仍未实现，因此不能
描述为可安装的开放市场。

#### 14.10.5.5 扩展发布控制面（P8.5）

控制面要求登录 JWT，同时由 Gateway 向 Agent Service 注入独立的 32 字符以上内部令牌。
客户端不能提交 `actor_user_id`；Gateway 始终使用 JWT 用户。平台管理员来自启动配置，发布者
所有者来自 Mongo 发布者记录，两者均由 Agent Service 在每次写操作前重新校验。

```text
GET  /api/v1/agent/marketplace/manage/access
GET  /api/v1/agent/marketplace/manage/publishers?page=1&page_size=20
POST /api/v1/agent/marketplace/manage/publishers
POST /api/v1/agent/marketplace/manage/publishers/:publisher_id/keys/rotate
POST /api/v1/agent/marketplace/manage/publishers/:publisher_id/keys/:key_id/revoke
PUT  /api/v1/agent/marketplace/manage/publishers/:publisher_id/verification

GET  /api/v1/agent/marketplace/manage/releases?publisher_id=&status=&page=1&page_size=20
POST /api/v1/agent/marketplace/manage/releases
POST /api/v1/agent/marketplace/manage/releases/:release_id/withdraw

GET  /api/v1/agent/marketplace/manage/audits?publisher_id=&action=&outcome=&page=1&page_size=20
```

平台注册发布者时提交不可变所有者列表和首个 Ed25519 公钥。普通轮换会把旧 Active Key 标记为
Retired，历史版本继续通过验签，但新版本只能使用当前 Active Key；Revoked Key 会立即使其历史
版本无法通过公开目录复验。所有发布者与版本写入使用 `expected_revision` CAS，版本撤回是不可逆的
`published -> withdrawn` 状态迁移，不删除版本，也不能复用相同 Package/SemVer 重新发布。

发布接口接收规范 Manifest、`signature_key_id` 和在客户端/离线工具生成的 `signature_base64`。
服务端重新规范化 Manifest、计算稳定 Release ID、验证 Active 公钥并生成发布时间。私钥不得发送给
本系统；所有管理响应与审计响应都不会回显原始签名。审计采用 `requested/succeeded/failed`
追加事件，只包含固定 Action、Outcome、Reason/Error Code、对象 ID、Revision 与操作者，不包含
Artifact、凭据、Prompt、正文或签名内容。

控制面由以下配置独立启用：

```text
AGENT_EXTENSION_MARKETPLACE_ADMIN_ENABLED=false
AGENT_EXTENSION_MARKETPLACE_ADMIN_TOKEN=<32+ characters>
AGENT_EXTENSION_MARKETPLACE_ADMIN_USER_IDS=<comma-separated JWT user IDs>
```

关闭控制面只撤销管理 API，不删除已有发布者、密钥、版本或审计。所有者转移、私钥托管、Artifact
上传/下载、扫描、依赖解析、安装审批和租户安装不属于当前契约。

#### 14.10.6 查询运行记录

```text
GET /api/v1/agent/workflow-runs/:id
```

分页查询当前用户的运行摘要：

```text
GET /api/v1/agent/workflow-runs?workflow_id=66aa...&status=failed&page=1&page_size=20
```

`workflow_id` 与 `status` 可省略，`page_size` 最大 100。列表按 `started_at` 与 `_id` 倒序，返回轻量摘要、总数和页码，不返回工作流输入、输出或 Trace 大字段。获取某一 Run 的业务详情、节点 Trace 或 Blackboard 时分别调用对应详情接口。

查询独立、脱敏的执行追踪：

```text
GET /api/v1/agent/workflow-runs/:id/traces
```

响应按 `run`、`steps`、`llm_calls`、`tool_calls` 分组。时间字段使用 Unix 毫秒；LLM 记录包含最终模型/Provider、Token/成本、耗时、Prompt/Completion 的 SHA-256 与长度，以及 `prompt_template_id`、`prompt_template_version`。Tool 记录包含治理状态、尝试次数及参数/结果摘要。超过内联阈值的 Tool Result 还会返回 `output_storage`、`output_reference`、`output_content_type`，其中引用是无凭证的内部 `minio://<private-bucket>/<hashed-key>`，不能当作公开下载 URL。

Prompt/Completion 安全预览采样默认关闭。开启后，`content_sample_policy` 和 `prompt_sample_status`/`completion_sample_status` 会说明样本是关闭、未选中、空、疑似敏感、超长或已采集；只有 `captured` 才返回有界的 `prompt_sample`/`completion_sample`。采样器拒绝疑似密钥、认证头/Cookie、邮箱、手机号、身份证和带 Query/UserInfo 的 URL，且样本只进入租户隔离 Trace，不进入 OTel Attribute、Prometheus Label 或日志。该策略是保守的诊断预览，不替代正式 DLP。

接口在读取 Trace 前按当前用户验证 Run 所有权。关闭采样时不会返回原始 Prompt、Completion、工具参数或工具结果；旧 Run 若没有独立记录，工作流编辑器仍可只读展示详情中的历史 `output.traces`。

Tool Result 在统一 Executor 中先 JSON 编码。超过 `AGENT_TOOL_RESULT_MAX_BYTES`（默认 1 MiB）返回 `result_too_large`；超过 `AGENT_TOOL_RESULT_INLINE_MAX_BYTES`（默认 64 KiB）时必须归档到私有对象桶。关闭 `AGENT_TOOL_RESULT_OBJECT_STORE_ENABLED` 是对象存储回滚开关，但不会放宽硬上限，也不会把大结果降级写入 Mongo。

检索已验证的 Blackboard 快照：

```text
GET /api/v1/agent/workflow-runs/:id/blackboard?state_version=12&path_prefix=writer.&query=draft&page_size=25&after_cursor=<cursor>
```

**路径参数：**

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 是 | 当前用户拥有的 Workflow Run ID |

**查询参数：**

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `state_version` | int64 | 否 | 目标状态版本；`0` 或省略表示最新已持久化版本 |
| `path_prefix` | string | 否 | 按 `node_id.field` 路径前缀过滤，最长 256 字节 |
| `query` | string | 否 | 不区分大小写匹配路径或脱敏后的值预览，最长 128 字节 |
| `page_size` | uint32 | 否 | 每页字段数，默认 25，最大 100 |
| `after_cursor` | string | 否 | 服务端返回的稳定游标；游标绑定首次查询的状态版本和过滤条件 |

```json
{
  "run_id": "66aa00000000000000000002",
  "state_version": 12,
  "base_snapshot_version": 10,
  "base_snapshot_hash": "9e4f...",
  "state_hash": "53b2...",
  "verified": true,
  "entries": [
    {
      "path": "writer.draft",
      "value_json": "\"一段已脱敏的草稿\"",
      "value_type": "string",
      "value_hash": "d98a...",
      "value_length": 34,
      "truncated": false
    }
  ],
  "matched_total": 1,
  "next_cursor": "",
  "has_more": false
}
```

服务端先按 `(user_id, run_id)` 验证所有权，再从目标版本之前最近的完整快照重放有界事件区间，并校验快照哈希、事件哈希、连续序号和最终状态版本。分页游标固定目标版本，因此运行中的 Blackboard 即使继续增长，后续页面也不会漂移。API Key、Authorization、Cookie、Password、Secret、Access/Refresh/Resume Token 等敏感键会递归替换为 `[REDACTED]`；单值脱敏 JSON 超过 16 KiB 时只返回类型、长度和 SHA-256，`value_json` 留空且 `truncated=true`。查询不会调用 Scheduler、LLM 或 Tool，也不修改 Run。

订阅有界、可恢复的执行事件：

```text
GET /api/v1/agent/workflow-runs/:id/events?after_cursor=1710000000000-0
Accept: text/event-stream
Authorization: Bearer <token>
```

**路径参数：**

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 是 | 当前用户拥有的 Workflow Run ID |

**查询参数：**

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `after_cursor` | string | 否 | Redis Stream ID；缺省从当前保留窗口起点读取 |

响应使用 SSE，`id` 是下一次重连应携带的游标，`event` 为 `trace` 或 `control`：

```text
id: 1710000000001-0
event: trace
data: {"cursor":"1710000000001-0","kind":"step","step":{"record_id":"run:step:llm","step_id":"llm","status":"success"}}

event: control
data: {"kind":"control","heartbeat":true,"reason":"heartbeat"}
```

服务端先按当前用户验证 Run 所有权。事件正文只复用脱敏的 Run/Step/LLM/Tool Trace DTO，不包含原始 Prompt、Completion、工具参数、工具结果或 API Key。`reset=true` 表示游标已过期或超前，客户端应重新读取详情与 Trace 快照；`terminal=true` 表示本次运行进入终态或挂起态。单连接达到 `AGENT_WORKFLOW_EVENT_STREAM_WINDOW` 后以 `window_expired` 控制事件结束，客户端使用最后游标重连。Redis Stream 只是有 TTL 和长度上限的投递通道，完整查询继续以 Mongo Trace 为事实源。

请求取消仍在执行的 Run：

```text
POST /api/v1/agent/workflow-runs/:id/cancel
Content-Type: application/json

{"reason":"用户请求停止"}
```

仅 `running` 可接受取消请求；原因最多 500 个字符。服务端通过 Mongo 条件更新把状态原子改为 `canceling`，并由执行实例按 `AGENT_WORKFLOW_CANCEL_POLL_INTERVAL`（默认 `500ms`）读取持久状态后取消 Context。正常完成/挂起与取消并发时，终态提交使用 `status + revision` 条件竞争；先持久化的取消请求优先收敛为 `canceled`。重复请求在 `canceling/canceled` 时幂等返回，其他终态返回 `409 Conflict`。

取消不会直接杀死进程或遗留节点 goroutine；Scheduler 仍等待本波次已启动节点响应 Context 后退出。若取消前已有成功且声明补偿的节点，Run 会继续进入原有持久补偿 Journal，最终可能是 `compensated` 或 `compensation_failed`。审批挂起 Run 应通过审批拒绝处理，不允许取消接口绕过审批状态机。

#### 14.10.7 Provider Config 管理

所有接口均需要 `Authorization: Bearer <token>`。`api_key` 只允许出现在创建或更新请求中，响应不会返回 API Key 或数据库密文。

```text
POST   /api/v1/agent/provider-configs
GET    /api/v1/agent/provider-configs?kind=llm&page=1&page_size=20
GET    /api/v1/agent/provider-configs/:id
PUT    /api/v1/agent/provider-configs/:id
DELETE /api/v1/agent/provider-configs/:id?revision=2
```

创建/更新请求包含 `kind`、`name`、`provider`、`base_url`、`model`、`api_key`；更新时还应携带 `revision`。`kind` 可取 `llm` 或 `web_search`，旧配置缺省时按 `llm` 处理且创建后不可变。`llm` 必须提供模型；首版 `web_search` 仅支持 Brave 且不得填写模型。空 `api_key` 表示更新时保留现有凭据，新值会轮换凭据并递增 `credential_version`。列表可用 `kind` 过滤两类配置。

响应只包含 `provider_config_id`、`kind`、公开连接元数据、`status`、`has_secret`、`credential_version`、`revision` 和时间戳。工作流 LLM/WebSearch 节点仅保存 `provider_config_id`，服务端按认证用户和配置类型校验所有权并解密。AI 助手的搜索配置 ID 由可信 Run 上下文注入内部 MCP 参数，不进入模型可见 Tool Schema。撤销会清除密文，后续引用 fail-closed。

响应结构同执行工作流接口。

#### 14.10.8 外部 MCP 连接与 Schema 发现

所有接口均需要 `Authorization: Bearer <token>`。连接支持个人作用域和 Agent 项目作用域；只支持远程 `streamable_http` 和 `sse`，不接受 stdio。项目能力由默认关闭的 `AGENT_EXTERNAL_MCP_PROJECT_SCOPE_ENABLED` 控制，且必须同时启用 `AGENT_EXTERNAL_MCP_ENABLED`。部署托管凭据还要求默认关闭的 `AGENT_EXTERNAL_MCP_MANAGED_CREDENTIALS_ENABLED`，并且只能用于项目级 Bearer 连接。

##### Agent 项目与成员

```text
POST   /api/v1/agent/projects
GET    /api/v1/agent/projects?page=1&page_size=20
GET    /api/v1/agent/projects/:project_id
PUT    /api/v1/agent/projects/:project_id/members/:user_id
DELETE /api/v1/agent/projects/:project_id/members/:user_id
```

创建项目只接受 `{"name":"Platform Research"}`，JWT 用户自动成为不可移除、不可降级的 `owner`。成员写接口使用 `expected_revision` 做 CAS；新增或更新成员还需传 `role=editor|viewer`。服务端会通过 User Service 验证路径中的目标用户真实存在，User Service 不可用时 fail-closed，不创建悬空成员。Gateway 只使用 JWT 作为操作者身份，忽略请求正文中的任何操作者或目标用户字段。

项目列表的 `page` 必须是正整数，`page_size` 范围为 1-100；非法值在 Gateway 返回 `400 Bad Request`，不会转换为无界 Mongo Skip。Web 管理面以最多三次请求有界聚合当前用户可访问的 256 个项目。

角色权限固定为：`owner` 可管理成员和连接，`editor` 可管理项目连接，`viewer` 只可查看并使用已审核工具。成员列表、连接控制和每次工具解析/调用都会读取当前成员关系；移除成员后，目录和执行权限立即失效。成员更新返回新的 `revision`，旧 Revision 返回 `409 Conflict`。所有用户 ID 在 HTTP JSON 中以字符串返回，避免 JavaScript 整数精度损失。

项目响应示例：

```json
{
  "project": {
    "project_id": "agentproj_0123456789abcdef0123456789abcdef",
    "name": "Platform Research",
    "owner_id": "9007199254740993",
    "current_role": "owner",
    "revision": 2,
    "members": [
      {"user_id":"9007199254740993","role":"owner","added_by":"9007199254740993"},
      {"user_id":"42","role":"viewer","added_by":"9007199254740993"}
    ]
  }
}
```

```text
POST   /api/v1/agent/mcp-connections
GET    /api/v1/agent/mcp-connections?page=1&page_size=20
GET    /api/v1/agent/mcp-connections/:id
PUT    /api/v1/agent/mcp-connections/:id
DELETE /api/v1/agent/mcp-connections/:id
POST   /api/v1/agent/mcp-connections/:id/discover
POST   /api/v1/agent/mcp-connections/:id/snapshots/:snapshot_id/approve
GET    /api/v1/agent/mcp-connections/:id/tools
PUT    /api/v1/agent/mcp-connections/:id/tools/:tool_name/policy
```

创建请求包含 `scope=user|project`、可选 `project_id`、`name`、`transport`、`endpoint`、`auth_type`、可选 `credential_source=user|managed`、可选 `managed_credential_ref` 和可选 `bearer_token`；省略 `scope` 时按个人连接处理，省略 `credential_source` 时按用户凭据处理。项目连接要求当前用户是 `owner/editor`，且 `project_id` 必须存在。作用域和项目归属创建后不可修改。更新、撤销、发现与审核均携带 `expected_revision`；用户凭据更新时空 Bearer Token 保留现有密文。

`credential_source=user` 支持个人或项目连接。Bearer 端点必须使用 HTTPS，Token 只以 AES-256-GCM 密文保存；其他项目成员只能通过受治理调用边界使用，无法读取密文或明文。`credential_source=managed` 只支持项目级 Bearer 连接，必须提供 `managed_credential_ref`，且请求不得同时发送 `bearer_token`。服务端会将引用与部署 Registry 中的 `project_id + endpoint + auth_type` 精确匹配；Registry 只保存元数据和 Secret 文件名，真实 Token 位于专用只读目录，并在 Discovery、健康探测和每次工具调用时重新解析。Connection 只持久化引用和已验证版本，不持久化托管 Token 或其密文。

用户凭据与托管凭据可通过显式更新和 Revision CAS 迁移；切换到用户 Bearer 凭据时必须提供新 Token。端点、认证方式、凭据来源、托管引用或已验证 Registry Version 变化会清空现有 Snapshot 与 Tool Policy，要求重新发现和审核。响应只返回 `has_secret`、`credential_source`、`credential_version`、`managed_credential_ref`、`managed_credential_version`、`scope`、`project_id` 和字符串形式的 `owner_user_id`，永不返回 Secret 文件名、Token、密文或运行时凭据身份。旧记录缺少 `scope` 时按个人连接兼容，缺少 `credential_source` 时按用户凭据兼容；关闭项目作用域开关后，项目连接不会进入列表、目录或运行时，但个人连接保持可用。

部署 Registry 示例：

```json
[
  {
    "reference": "team.research",
    "project_id": "agentproj_0123456789abcdef0123456789abcdef",
    "endpoint": "https://mcp.example.com/mcp",
    "auth_type": "bearer",
    "secret_key": "research-token",
    "version": 1
  }
]
```

Registry 使用严格 JSON 解析，未知字段、重复引用、非法版本、越界 Secret 路径或绑定不一致都会 fail-closed。Kubernetes 通过 `externalMCP.managedCredentials.existingSecret` 将 Secret 只读挂载到专用目录；在 Registry Version 不变时，仅替换挂载文件即可轮换同一身份的 Token，运行时会以单向凭据身份隔离新旧 Session，既有 Schema/Policy 不变。修改 Registry 元数据或 `version` 需要滚动 Agent Service；版本与 Connection 最近验证值不一致时，Discovery、健康和调用都会拒绝，Owner/Editor 必须用 Revision CAS 重新保存连接，随后重新发现和审核。Docker Compose 只透传环境变量，启用时必须额外提供对应只读目录挂载。

Discovery 受 `AGENT_EXTERNAL_MCP_ENABLED`、独立 Host Allowlist、禁重定向、DNS/IP 与响应体边界约束。结果按名称规范化为不可变 Snapshot，并生成 `server_id.tool_name`；首次发现或 Schema Hash 变化只设置 `pending_snapshot_id`，审核后才推进 `active_snapshot_id`。审核仅接受 Schema，工具仍默认禁用。

SDK Adapter 可通过 `AGENT_EXTERNAL_MCP_POOL_ENABLED` 启用有界 Session Pool；全局 Session 数、单连接 Session 数、获取等待和空闲 TTL 均由部署配置限制。池键绑定 Connection、Transport、Endpoint 与单向 Credential Identity；用户凭据使用 Connection Credential Version，托管凭据额外绑定 Registry Version 与当前 Secret 摘要。端点或凭据轮换成功后新调用不会复用旧 Session，Connection 更新或撤销会使该 Connection 的全部凭据身份失效；关闭该开关恢复逐次建连。

`AGENT_EXTERNAL_MCP_HEALTH_CHECK_ENABLED` 独立控制主动健康巡检。多实例通过 Mongo 租约抢占到期连接，并使用批次、并发上限、稳定抖动和指数退避调用 MCP `ping`。健康状态只用于诊断，不改变 `discovery_status`、Active Snapshot 或 Tool Policy，也不代替真实调用时的权限和连接校验。

**工具目录路径参数：**

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 是 | 当前用户拥有的个人连接，或当前仍有权访问的项目 MCP Connection ID |
| `tool_name` | string | 更新策略时是 | URL 编码后的完整 `server_id.tool_name` |

`GET /mcp-connections/:id/tools` 返回 Active Snapshot 与当前策略视图，不返回 Endpoint 凭据、密文或 Bearer Token：

```json
{
  "connection": {
    "connection_id": "mcpconn_01",
    "server_id": "mcp_01",
    "scope": "project",
    "project_id": "agentproj_0123456789abcdef0123456789abcdef",
    "owner_user_id": "9007199254740993",
    "credential_source": "managed",
    "managed_credential_ref": "team.research",
    "managed_credential_version": 1,
    "active_snapshot_id": "mcpsnap_01",
    "discovery_status": "ready",
    "health_status": "healthy",
    "health_error_code": "",
    "health_failure_count": 0,
    "last_health_checked_at": 1785150000,
    "last_healthy_at": 1785150000,
    "next_health_check_at": 1785150120,
    "revision": 4
  },
  "snapshot": {
    "snapshot_id": "mcpsnap_01",
    "schema_hash": "2fe2...",
    "version": 1785150000000000000
  },
  "tools": [
    {
      "schema": {
        "name": "lookup",
        "qualified_name": "mcp_01.lookup",
        "description": "Look up public documentation",
        "input_schema_json": "{\"type\":\"object\"}",
        "output_schema_json": "",
        "declared_read_only": true,
        "declared_idempotent": false,
        "idempotency_key_argument": "",
        "supports_write_idempotency": false
      },
      "policy": {
        "snapshot_id": "mcpsnap_01",
        "tool_name": "lookup",
        "qualified_name": "mcp_01.lookup",
        "category": "read",
        "enabled": true,
        "updated_at": 1785150000
      }
    }
  ]
}
```

`health_status` 可取 `unknown`、`healthy`、`degraded`、`unhealthy`。旧记录缺少字段时返回 `unknown`；单次失败进入 `degraded`，达到部署级连续失败阈值后进入 `unhealthy`，成功探测会清零失败计数。`health_error_code` 只返回固定分类，不包含 Endpoint、凭据或远端原始错误。

`PUT /mcp-connections/:id/tools/:tool_name/policy` 使用 Connection Revision 做 CAS，策略必须绑定当前 Active Snapshot：

```json
{
  "snapshot_id": "mcpsnap_01",
  "category": "read",
  "enabled": true,
  "expected_revision": 4
}
```

只有服务端 Schema Annotation 同时声明 `readOnlyHint=true` 且未声明 destructive 的工具可以用 `category=read, enabled=true`。未声明只读的工具可由用户显式使用 `category=risky, enabled=true`，但只允许在 Workflow 中逐次审批执行。

`category=write` 还要求审核快照同时满足：MCP 标准 Annotation 的 `idempotentHint=true`；工具 `_meta["io.twitter-clone/idempotency-key-argument"]` 是合法属性名；该属性在顶层 Input Schema 中是 `type=string` 且位于 `required`。服务端会返回 `declared_idempotent`、`idempotency_key_argument` 和计算后的 `supports_write_idempotency`。缺少任一条件时启用写策略返回 `412 Precondition Failed`。Schema 变化会立即把 Connection 置为 `review_required`；新 Snapshot 审核后清空旧策略，防止旧授权漂移到新契约。

AI 助手显式选择 `preferred_capability_ids=["connector.mcp"]` 时，服务端每次 Run 都从当前用户、Connection、Active Snapshot 与 Tool Policy 的交集构造目录，并要求至少一个真实成功 Tool Observation 才返回答案。默认路径仍只暴露已启用的 `read` 工具；只有同时启用 `AGENT_RECOVERABLE_RUNS_ENABLED`、`AGENT_EXTERNAL_MCP_ENABLED` 和 `AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED`，且权威 Run、Checkpoint 与审批仓储均可用时，受治理目录才可加入 `risky/write`。后两类工具在远端调用前挂起为 `approval_required`，复用 14.10 的 Tool Approval，并按 14.0.3 与 14.0.2 签发、消费一次性授权。每次授权签发、恢复和真实调用前都重新校验租户、Connection、Credential Version、Snapshot、Schema、Policy 与审批绑定，随后经统一 ToolExecutor 执行身份、超时、重试、熔断、结果体积、审计与 Trace 规则。远端工具描述和结果按不可信第三方数据处理，不投影为平台 Citation；关闭任一相关开关可回退到只读或旧不可恢复路径。

个人与项目连接共享同一 Snapshot、Policy、审批和执行路径。项目连接在列举目录、按 Connection ID 查询、按 `server_id` 解析以及真正远端调用前都会重新验证当前成员角色；不会把创建时权限缓存为长期授权。项目 `viewer` 可调用已启用能力但不能发现、审核、配置或撤销连接，`owner/editor` 可执行连接治理操作，只有 `owner` 可增删成员。

Workflow 可使用动态 Tool 节点引用当前用户已启用的 `read/risky/write` 工具：

```json
{
  "id": "remote_tool",
  "type": "tool",
  "properties": {
    "external_mcp": true,
    "tool_name": "mcp_01.create_issue",
    "mcp_arguments": {
      "title": "{{start.user_input}}"
    }
  },
  "timeout_sec": 20
}
```

`mcp_arguments` 必须是 JSON 对象，内部字段按远端 Active Snapshot 的 Input Schema 校验；`external_mcp`、`tool_name`、编辑器超时字段和平台内部 `user_id` 不会被隐式传给远端。平台身份仅用于租户授权、审批与审计；远端 Schema 若显式声明同名业务字段，则以用户通过 Schema 校验的原始值为准。

`risky` 和 `write` 节点首次执行都会创建持久审批并挂起 Run，批准后使用单次 Resume Grant 重试当前节点；恢复与真实调用前再次校验策略。`risky` 远端调用固定单次且不可自动重试。`write` 使用稳定的工作流执行键，经 SHA-256 域隔离后覆盖注入远端声明的幂等参数；DSL、模型和用户提供的同名值不会生效，同一逻辑执行的有限网络重试使用同一远端键。该机制依赖第三方 Server 遵守其声明，只提供声明式重放安全边界，不承诺跨系统严格 exactly-once。外部 MCP 补偿仍未开放。

#### 14.10.9 查询工具审批请求

```text
GET /api/v1/agent/tool-approvals?status=pending&page=1&page_size=20
```

**路径参数：** 无。

**查询参数：**

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `status` | string | 否 | `pending`、`approved`、`rejected`、`executing`、`consumed`、`expired` |
| `page` | int | 否 | 页码，默认 1 |
| `page_size` | int | 否 | 每页数量，默认 20，最大 100 |

**成功响应：**

```json
{
  "approvals": [
    {
      "approval_id": "66cc...",
      "user_id": "123",
      "run_id": "66bb...",
      "step_id": "node_tweet_01",
      "tool_name": "PublishTweet",
      "source": "workflow",
      "category": "write",
      "status": "pending",
      "redacted_inputs": {"content": "[REDACTED]", "user_id": 123},
      "idempotency_key": "66bb...:node_tweet_01:PublishTweet",
      "revision": 1,
      "created_at": 1782380010,
      "expires_at": 1782380910,
      "decided_at": 0
    }
  ],
  "total": 1
}
```

审批参数只返回脱敏副本，原始正文、Prompt、密钥和 Token 不进入审批记录。

#### 14.10.10 批准或拒绝工具执行

```text
POST /api/v1/agent/tool-approvals/:id/decision
```

**路径参数：**

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 是 | 审批请求 ID |

**请求体：**

```json
{
  "decision": "approved",
  "reason": "内容已人工确认",
  "expected_revision": 1
}
```

`decision` 仅接受 `approved` 或 `rejected`。`expected_revision` 是必填乐观锁版本；重复点击、过期审批或旧版本决策返回 `409 Conflict`。拒绝后不会执行远端工具：`source=workflow` 的挂起 Workflow Run 进入 `rejected`；`source=runtime` 的 Agent Run 进入 `failed`，`failure_code=approval_rejected`，并清除 Checkpoint、恢复授权与租约。

**成功响应：**

```json
{
  "approval": {
    "approval_id": "66cc...",
    "run_id": "66bb...",
    "status": "approved",
    "revision": 2,
    "reason": "内容已人工确认"
  }
}
```

#### 14.10.11 签发短期恢复授权

```text
POST /api/v1/agent/tool-approvals/:id/resume-grant
```

仅当前登录用户可以为自己已批准、未过期且仍绑定 `suspended` Run 的审批签发恢复授权。请求体必须携带刚查询到的 Run 乐观锁版本：

```json
{
  "expected_run_revision": 3
}
```

成功响应：

```json
{
  "run": {
    "run_id": "66bb...",
    "status": "suspended",
    "revision": 4,
    "approval_request_id": "66cc...",
    "resume_grant_issued_at": 1782380100,
    "resume_grant_expires_at": 1782380400
  },
  "resume_token": "仅本次响应可见的随机授权",
  "expires_at": 1782380400
}
```

授权默认有效期为 5 分钟，并且不会超过审批自身的过期时间。每次成功签发都会原子轮换 Run 中保存的 SHA-256 哈希，使旧挂起令牌和此前签发的授权立即失效；明文只在本次响应返回。审批状态、Run 状态、归属、绑定审批或 `expected_run_revision` 发生变化时返回 `409 Conflict`。

该接口及其他可能返回 `resume_token` 的接口均设置 `Cache-Control: no-store, max-age=0` 和 `Pragma: no-cache`。客户端应在内存中立即调用恢复接口，不得把新授权写入 LocalStorage、SessionStorage、日志或业务数据库。

#### 14.10.12 恢复挂起工作流

```text
POST /api/v1/agent/workflow-runs/:id/resume
```

**路径参数：**

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 是 | 挂起的工作流 Run ID |

**请求体：**

```json
{
  "approval_id": "66cc...",
  "resume_token": "本次挂起响应返回的一次性令牌",
  "input": {"approved": true}
}
```

也可使用 `input_json`。对 `suspended` Run，服务端只保存 `resume_token` 的 SHA-256 哈希；令牌与当前用户、Run 和审批请求绑定，并通过 Mongo 条件更新原子领取。重复恢复、错误令牌、跨用户审批或未批准状态返回 `409 Conflict`。

为兼容旧客户端，`compensation_failed` Run 仍可发送空对象 `{}` 显式重试首个失败补偿；新客户端应使用 `POST /workflow-runs/:id/compensations/retry`。此路径不需要 `resume_token`，但仍校验当前用户、Run、Journal 状态与租约，并且不会重放主 DAG。`running`、`success`、`compensated` 等其他状态不能启动补偿。

**成功响应：**

```json
{
  "run": {
    "run_id": "66bb...",
    "status": "success",
    "approval_request_id": "",
    "waiting_node_id": "",
    "revision": 4
  },
  "response": "工作流执行完成",
  "resume_token": ""
}
```

若后续节点再次挂起，响应会返回新的 `resume_token`。查询 Run 接口不会再次返回令牌。

Web 审批收件箱不持久化明文恢复令牌。批准或重试继续运行时，客户端先查询最新 Run revision，再签发短期授权并立即恢复，因此关闭标签页、跨浏览器或跨设备后仍可处理同一审批。签发请求因并发 revision 变化返回 `409` 时，客户端只允许重新查询一次后重试；已批准但首次恢复请求失败的审批可在“已批准”列表再次继续。

#### 14.10.13 Suspended Workflow Checkpoint

工作流支持 `wait` 节点，以及由 Write/Risky 工具自动触发的审批挂起。运行进入 `suspended` 后，CheckPoint 保存 Blackboard、Trace 和恢复模式，但不保存原始恢复令牌：

```json
{
  "resume_token": "仅本次响应可见的随机令牌",
  "run": {
    "status": "suspended",
    "waiting_node_id": "node_tweet_01",
    "approval_request_id": "66cc...",
    "suspended_at": 1782380010,
    "output": {
      "checkpoint": {
        "current_node_id": "node_tweet_01",
        "state_version": 2,
        "retry_current_node": true,
        "reason": "tool approval required"
      }
    }
  }
}
```

普通 `wait` 恢复时把当前 Wait 节点视为完成；审批挂起设置 `retry_current_node=true`，批准后重新执行当前工具节点，防止写工具被错误跳过。

协调器每次应用 Blackboard Delta 时都会生成 append-only StateEvent。事件按 `(run_id, sequence)` 唯一并幂等写入 `agent_workflow_state_events`；同一 sequence 内容不同会作为一致性冲突拒绝。Checkpoint 的 `state_version` 与 Run 的 `state_version` 指向已持久化的最后事件，恢复后的新事件从下一 sequence 继续追加。

#### 14.10.14 Durable Compensation Journal

补偿计划持久化在 `agent_workflow_compensations`，按 `(run_id, sequence)` 唯一。每条记录包含源节点、补偿工具、输入/计划哈希、稳定幂等键、Retry、Timeout、状态、Attempt ID 和领取租约。执行器只领取严格顺序中的首个未成功任务；已成功记录不会重复执行，同一序号计划内容漂移会作为冲突拒绝。

补偿状态为 `planned`、`executing`、`suspended`、`succeeded`、`failed`。写补偿触发审批时，Run 进入现有 `suspended` 状态并返回一次性令牌；批准后继续当前补偿，拒绝后 Journal 与 Run 终止。

后台扫描器按 `AGENT_WORKFLOW_COMPENSATION_RECONCILE_INTERVAL` 周期查找每个 Run 严格首个未完成且租约过期的 `executing` 记录，批量上限由 `AGENT_WORKFLOW_COMPENSATION_RECONCILE_BATCH_SIZE` 控制。多实例只负责发现候选，真正执行仍通过 Journal 原子 Claim、Attempt ID 和稳定幂等键竞争。无需审批的补偿可自动继续；需要审批或工具目录缺失的补偿不会在后台执行，而会转成 `failed/compensation_failed`，由用户显式重试以安全获得一次性 Resume Token。`suspended`、普通 `failed` 和仍有有效租约的记录不会被后台抢占。计划已持久化但尚未进入 `executing` 的崩溃窗口不由扫描器自动启动，可通过下方专用人工控制端点推进。

查询当前用户拥有的 Run 的脱敏 Journal：

```text
GET /api/v1/agent/workflow-runs/:id/compensations
```

```json
{
  "run": {
    "run_id": "66bb...",
    "workflow_id": "66aa...",
    "status": "compensation_failed",
    "error_message": "main workflow failed; compensation step release failed"
  },
  "entries": [
    {
      "sequence": 1,
      "source_node_id": "reserve",
      "step_id": "reserve$compensate",
      "tool_name": "ReleaseResource",
      "input_hash": "sha256-hex",
      "plan_hash": "sha256-hex",
      "status": "failed",
      "attempt": 2,
      "is_next": true
    }
  ],
  "next_sequence": 1,
  "retry_available": true
}
```

该响应不包含工作流输入/输出、补偿输入/输出、幂等键、Attempt ID 或审批参数。`retry_available` 仅在 Run 可恢复且严格首个未完成记录为 `planned`、`failed` 或租约已过期的 `executing` 时为 `true`；有效租约和 `suspended` 记录不能被人工抢占。

显式重试下一条可恢复补偿：

```text
POST /api/v1/agent/workflow-runs/:id/compensations/retry
```

请求体可为空。服务端重新读取用户隔离 Journal 并通过原子 Claim 竞争，绝不重放主 DAG。若写工具需要审批，响应中的 Run 进入 `suspended`，并且只在本次响应返回新的 `resume_token`。

#### 14.10.15 只读运行回放

```text
GET /api/v1/agent/workflow-runs/:id/replay
```

接口只读取当前用户拥有的 Run、固定 Revision 元数据、StateEvent、最近 Snapshot 元数据和 Compensation Journal，不会调用 Scheduler、LLM、MCP 或任何 Tool。补偿原始输入、输出、幂等键和审批敏感参数不会返回。

```json
{
  "run": {"run_id": "66bb...", "status": "compensated", "state_version": 2},
  "revision": {"revision_id": "66ab...", "revision_number": 3, "dsl_hash": "..."},
  "events": [
    {
      "sequence": 1,
      "node_id": "start",
      "delta": {"user_input": "hello"},
      "event_hash": "sha256-hex",
      "applied_at": 1782380000
    }
  ],
  "snapshot": {"state_version": 2, "snapshot_hash": "sha256-hex", "created_at": 1782380002},
  "compensations": [
    {
      "sequence": 1,
      "source_node_id": "reserve",
      "tool_name": "ReleaseResource",
      "input_hash": "sha256-hex",
      "plan_hash": "sha256-hex",
      "status": "succeeded",
      "attempt": 1
    }
  ],
  "integrity": {
    "verified": true,
    "state_version": 2,
    "event_count": 2,
    "last_sequence": 2,
    "snapshot_version": 2
  }
}
```

服务端会校验事件所有权、连续序号、Event Hash、Snapshot Hash 和最终状态版本。任一证据缺失或被篡改时返回 `422 Unprocessable Entity`，不会带着不完整数据声称回放成功。单次最多读取 10,000 个事件，超过上限同样 fail-closed；当前接口提供完整证据视图，不做静默截断。
## 15. Agent Profile 管理接口 🔒

以下接口均要求正常 JWT。Gateway 使用内部 `AGENT_PROFILE_ADMIN_TOKEN` 向 Agent Service
查询当前用户的项目级角色，Agent Service 在实际管理 RPC 内再次校验
`viewer/editor/approver/admin`。角色由环境变量 break-glass 绑定与 Mongo 动态绑定合并，
所有响应设置 `Cache-Control: no-store`，客户端不得提交服务间令牌。`admin` 继承全部角色。

```text
GET  /api/v1/agent/profile-catalog/access
POST /api/v1/agent/profile-catalog/versions
GET  /api/v1/agent/profile-catalog/versions?profile_id=&page=1&page_size=20
GET  /api/v1/agent/profile-catalog/versions/:profile_id/:version
POST /api/v1/agent/profile-catalog/versions/:profile_id/:version/publish-requests
GET  /api/v1/agent/profile-catalog/publish-approvals?profile_id=&status=&page=1&page_size=20
GET  /api/v1/agent/profile-catalog/publish-approvals/:approval_id
POST /api/v1/agent/profile-catalog/publish-approvals/:approval_id/decision
POST /api/v1/agent/profile-catalog/publish-approvals/:approval_id/retry
POST /api/v1/agent/profile-catalog/versions/:profile_id/:version/publish  # break-glass only
GET  /api/v1/agent/profile-catalog/releases/:profile_id
PUT  /api/v1/agent/profile-catalog/releases/:profile_id
GET  /api/v1/agent/profile-catalog/audits?profile_id=&page=1&page_size=20
POST /api/v1/agent/profile-catalog/experiments
GET  /api/v1/agent/profile-catalog/experiments?profile_id=&status=&page=1&page_size=20
GET  /api/v1/agent/profile-catalog/experiments/:experiment_id
POST /api/v1/agent/profile-catalog/experiments/:experiment_id/evaluate
POST /api/v1/agent/profile-catalog/experiments/:experiment_id/stop
POST /api/v1/agent/profile-catalog/experiments/:experiment_id/outcomes
GET  /api/v1/agent/profile-catalog/role-bindings?page=1&page_size=20
PUT  /api/v1/agent/profile-catalog/role-bindings/:user_id
DELETE /api/v1/agent/profile-catalog/role-bindings/:user_id?expected_revision=1
GET  /api/v1/agent/profile-catalog/role-audits?page=1&page_size=20
```

创建草稿请求使用强类型 Profile 字段：`profile_id`、`version`、`prompt_id`、
`prompt_version`、`system_prompt`、Token/成本/超时预算和 `allowed_tools`。编辑者提交发布申请：

```json
{
  "expected_version_revision": 1,
  "quality_evidence": {
    "storage": "minio",
    "bucket": "agent-task-eval-reports",
    "key": "agent-task-eval/.../report-sha256.json",
    "version_id": "immutable-object-version",
    "etag": "optional-etag",
    "report_sha256": "64-char-sha256",
    "length": 16384,
    "content_type": "application/json",
    "retention_mode": "COMPLIANCE",
    "retain_until": 1780000000000,
    "archived_at": 1770000000000,
    "dataset_version": "agent-task-v1",
    "dataset_sha256": "64-char-sha256",
    "execution_config_sha256": "64-char-sha256",
    "integrity_key_id": "eval-signing-v1"
  }
}
```

`quality_evidence` 接受 `agent-task-eval` 归档命令生成的回执字段。默认可省略；当
`AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED=true` 时必须提供。Agent Service 会从 MinIO 按
`version_id` 读取完整报告，验证报告 SHA-256、HMAC Key ID、`runtime_live` 执行类型、目标
Profile ID/Version、质量 Gate、安全指标和仍有效的 `COMPLIANCE` 保留期。审批通过与失败重试前
会再次读取并验证同一对象版本。审批记录和 API 只保存/返回摘要，不保存评测输入、Prompt、回答
正文、Base URL 或 Credential。

当同时设置 `AGENT_PROFILE_EVAL_CONTENT_SIGNOFF_REQUIRED=true` 时，同一请求结构不变，但回执
必须指向 `agent-task-content-qualified-evidence/v1`。此时 `report_sha256` 覆盖整个“签名报告 +
外部人工 Signoff”对象；服务分别验证报告与 Signoff HMAC，并要求 Candidate 为
`external_human/ asserted_external / approved`。Judge 或旧裸报告均被拒绝。该新增开关默认关闭，
因此不要求客户端、Proto 或历史审批记录同步迁移。

审批者必须与申请者不同，且只能决策申请绑定的 revision 与 snapshot hash：

```json
{"decision":"approved","reason":"reviewed","expected_revision":1}
```

审批状态为 `pending -> applying -> applied` 或 `pending -> rejected`；发布失败进入
`apply_failed`。`applying` 带短租约，审批者可在租约过期或失败后调用 `/retry`，服务会先
检查版本是否已经发布，避免崩溃恢复时重复变更。直接发布默认关闭，只有同时配置
`AGENT_PROFILE_DIRECT_PUBLISH_ENABLED=true` 的管理员可以作为应急逃生口使用。

Release 创建时 `expected_revision=0`，更新时必须传当前 revision：

```json
{
  "stable_version": "v1",
  "candidate_version": "v2",
  "candidate_basis_points": 1000,
  "salt": "assist-v2-canary",
  "expected_revision": 2
}
```

版本、审批或 Release CAS 冲突返回 HTTP `409`。持久化已提交但本地激活或跨实例通知失败时
返回 `422`，审计记录会标记 `activation_failed` 或 `propagation_failed`；调用方应先读取
版本、Release 和审计状态，不能盲目重试写操作。

### 15.1 获取当前管理权限

`GET /api/v1/agent/profile-catalog/access`

响应区分静态 break-glass 与动态角色来源：

```json
{
  "enabled": true,
  "roles": ["viewer", "admin"],
  "static_roles": ["admin"],
  "dynamic_roles": ["viewer"],
  "root_admin": true,
  "dynamic_rbac_enabled": true,
  "direct_publish_enabled": false,
  "experiments_enabled": true
}
```

### 15.2 分页查询动态角色绑定

`GET /api/v1/agent/profile-catalog/role-bindings?page=1&page_size=20`

仅 `admin` 可访问。环境变量角色不在此列表中，也不能通过 API 删除。

| Query 参数 | 类型 | 必填 | 说明 |
|---|---:|---:|---|
| `page` | integer | 否 | 默认 1 |
| `page_size` | integer | 否 | 1 到 100，默认 20 |

```json
{
  "role_bindings": [{
    "user_id": "42",
    "roles": ["editor"],
    "revision": 2,
    "created_by": "1",
    "updated_by": "1",
    "created_at": 1784512800000,
    "updated_at": 1784516400000
  }],
  "total": 1
}
```

### 15.3 创建或更新动态角色绑定

`PUT /api/v1/agent/profile-catalog/role-bindings/:user_id`

| Path 参数 | 类型 | 必填 | 说明 |
|---|---:|---:|---|
| `user_id` | uint64 string | 是 | 目标平台用户 ID |

创建时 `expected_revision=0`，更新时传当前 revision：

```json
{"roles":["viewer","editor"],"expected_revision":0}
```

动态管理员可管理 `viewer/editor/approver`；只有由
`AGENT_PROFILE_ADMIN_USER_IDS` 配置的根管理员可授予或撤销 `admin`。未知角色返回 `400`，
revision 冲突返回 `409`。

### 15.4 删除动态角色绑定

`DELETE /api/v1/agent/profile-catalog/role-bindings/:user_id?expected_revision=2`

| 参数 | 位置 | 类型 | 必填 | 说明 |
|---|---|---:|---:|---|
| `user_id` | Path | uint64 string | 是 | 目标平台用户 ID |
| `expected_revision` | Query | integer | 是 | 当前绑定 revision |

成功返回 `204 No Content`。删除动态绑定不会影响同一用户的环境变量角色。

### 15.5 查询角色审计

`GET /api/v1/agent/profile-catalog/role-audits?page=1&page_size=20`

仅 `admin` 可访问。审计为 append-only，不保存 JWT、内部令牌或请求正文。

```json
{
  "audit_events": [{
    "id": "669c00000000000000000001",
    "operation_id": "1b3d...",
    "action": "upsert_profile_role_binding",
    "outcome": "succeeded",
    "actor_user_id": "1",
    "subject_user_id": "42",
    "roles": ["editor"],
    "revision": 1,
    "error_code": "",
    "created_at": 1784516400000
  }],
  "total": 2
}
```

### 15.6 Profile 运行时实验门禁

实验功能仅在 `AGENT_PROFILE_EXPERIMENTS_ENABLED=true` 时可用。启动前必须先配置包含 stable、
candidate 和非零候选流量的 Release；请求绑定当前 Release revision：

```json
{
  "profile_id": "assist.draft",
  "expected_release_revision": 3,
  "policy": {
    "min_samples_per_arm": 50,
    "target_samples_per_arm": 200,
    "max_error_rate_increase_basis_points": 500,
    "max_p95_latency_increase_basis_points": 2000,
    "max_average_cost_increase_basis_points": 2000,
    "outcome_signal": "draft_published",
    "min_outcome_samples_per_arm": 50,
    "max_outcome_rate_decrease_basis_points": 1000
  }
}
```

`min_samples_per_arm` 与 `target_samples_per_arm` 最大为 5000；阈值范围是 `0..10000`
基点。策略字段为 `0` 时使用服务端默认值。`outcome_signal` 省略或为空时保持原运行指标门禁；
启用时只能为 `response_accepted`、`draft_published`、`content_engaged`，并要求每组业务结果
样本达到 `min_outcome_samples_per_arm`。只有 admin 可启动、立即评估、停止或写入结果，viewer 可查询。
同一 Profile 同时只能有一个 `running` 实验，冲突返回 `409`。

立即评估与停止请求都必须携带当前实验 revision：

```json
{"expected_revision": 4}
```

实验响应包含 stable/candidate 样本数、成功/失败数、错误率基点、P95 毫秒、平均微单位成本，
以及已配置业务信号的结果样本、正向样本和结果率基点。
候选越过任一护栏时服务端用 Release CAS 把候选流量设为 0，并将实验标记为 `rolled_back`；
达到目标样本只标记为 `passed`，不会自动提升候选。外部修改过 Release 时进入 `superseded`。
观测记录不包含用户、Prompt 或生成内容，因此业务结果门禁也不能替代固定数据集语义 Eval。

业务系统使用下列接口按已经落库的 Run/Event ID 回填结果。该入口要求 admin 角色和内部管理凭据，
模型与 Prompt 无权调用：

```http
POST /api/v1/agent/profile-catalog/experiments/:experiment_id/outcomes
```

| 路径参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `experiment_id` | string | 是 | 运行中 Profile 实验的 Mongo ObjectID |

```json
{
  "event_id": "agent-run-01K2...",
  "signal": "draft_published",
  "positive": true
}
```

```json
{
  "idempotent_replay": false
}
```

同一 `experiment_id + event_id` 首次写入后不可改值；同值重试返回
`idempotent_replay=true`，冲突值返回 `409`。运行观测尚未异步落库、实验未配置该信号或已经终止时返回
`422`，调用方应仅对“观测未就绪”使用相同请求做有界重试。
