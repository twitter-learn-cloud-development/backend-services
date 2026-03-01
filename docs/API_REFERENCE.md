# Twitter Clone API 接口文档

## 概述

| 项目 | 说明 |
|------|------|
| **Base URL** | `http://twitter-clone.local/api/v1` (Ingress) 或 `http://<minikube-ip>:30638/api/v1` (NodePort) |
| **认证方式** | Bearer Token (JWT)，通过 `Authorization: Bearer <token>` 请求头传递 |
| **数据格式** | JSON |
| **网关端口** | 9638 |

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
| 参数 | 类型 | 说明 |
|------|------|------|
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
| 参数 | 类型 | 说明 |
|------|------|------|
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
| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `cursor` | uint64 | 0 | 游标（分页起点） |
| `limit` | int32 | 20 | 每页数量 |

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
  "url": "http://localhost:8080/uploads/20231010/uuid.jpg"
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

| 方法 | 请求 | 响应 |
|------|------|------|
| `Register` | `{username, email, password}` | `{user}` |
| `Login` | `{email, password}` | `{token, user}` |
| `GetProfile` | `{user_id}` | `{user}` |
| `UpdateProfile` | `{user_id, avatar, bio}` | `{user}` |
| `ChangePassword` | `{user_id, old_password, new_password}` | `{message}` |

### 6.2 TweetService (端口 9092)

| 方法 | 请求 | 响应 |
|------|------|------|
| `CreateTweet` | `{user_id, content, media_urls}` | `{tweet}` |
| `GetTweet` | `{tweet_id}` | `{tweet}` |
| `DeleteTweet` | `{tweet_id, user_id}` | `{message}` |
| `GetUserTimeline` | `{user_id, cursor, limit}` | `{tweets[], next_cursor, has_more}` |
| `GetFeeds` | `{user_id, cursor, limit}` | `{tweets[], next_cursor, has_more}` |

### 6.3 FollowService (端口 9093)

| 方法 | 请求 | 响应 |
|------|------|------|
| `Follow` | `{follower_id, followee_id}` | `{message}` |
| `Unfollow` | `{follower_id, followee_id}` | `{message}` |
| `IsFollowing` | `{follower_id, followee_id}` | `{is_following}` |
| `GetFollowers` | `{user_id, cursor, limit}` | `{follower_ids[], next_cursor, has_more}` |
| `GetFollowees` | `{user_id, cursor, limit}` | `{followee_ids[], next_cursor, has_more}` |
| `GetFollowStats` | `{user_id}` | `{follower_count, followee_count}` |

---

## 7. 错误响应格式

所有错误响应遵循统一格式：

```json
{
  "error": "错误描述信息"
}
```

| HTTP 状态码 | 说明 |
|------------|------|
| 400 | 请求参数错误 |
| 401 | 未认证 / Token 无效 |
| 403 | 权限不足 |
| 404 | 资源不存在 |
| 429 | 请求过于频繁（限流） |
| 500 | 服务器内部错误 |

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

## 12. 中间件

| 中间件 | 说明 |
|--------|------|
| **OpenTelemetry** | 自动注入 TraceID/SpanID |
| **Prometheus Metrics** | 记录请求延迟、状态码等指标 |
| **Rate Limiter** | Redis 分布式限流 (1000/min per IP) |
| **Logger** | 请求日志记录 |
| **CORS** | 跨域资源共享 |
| **Recovery** | Panic 恢复 |
| **Error Handler** | 统一错误处理 |
| **JWT Auth** | 🔒 标记的接口需要认证 |
| **JWT AuthOptional** | 可选认证：有 token 就解析 user_id，没有则跳过 |
