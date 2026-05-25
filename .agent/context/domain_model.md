# Domain Model (领域模型与数据流向)

本项目涵盖了社交平台的核心业务场景，主要领域实体均定义在 [internal/domain/](file:///e:/GOProject/云原生/twitter-clone/internal/domain) 目录中。

---

## 1. 核心领域实体

* **User (用户)**：
  * **定义位置**：[user.go](file:///e:/GOProject/云原生/twitter-clone/internal/domain/user.go)
  * **核心属性**：ID, Username, Email, Password, Bio, AvatarURL, CreatedAt, UpdatedAt 等。
* **Follow (关注关系)**：
  * **定义位置**：[follow.go](file:///e:/GOProject/云原生/twitter-clone/internal/domain/follow.go)
  * **核心属性**：ID, FollowerID (关注者), FollowingID (被关注者), CreatedAt。
* **Tweet (推文)**：
  * **定义位置**：[tweet.go](file:///e:/GOProject/云原生/twitter-clone/internal/domain/tweet.go)
  * **核心属性**：ID, UserID (发布者), Content, ImageURLs, VideoURL, RetweetFrom (转推源ID), IsComment (是否是评论), CommentTo (主推文ID), LikeCount, CommentCount, RetweetCount, CreatedAt。
  * **附属实体**：
    * `Poll` ([poll.go](file:///e:/GOProject/云原生/twitter-clone/internal/domain/poll.go))：推文内嵌的投票。
    * `Like` ([like.go](file:///e:/GOProject/云原生/twitter-clone/internal/domain/like.go))：点赞记录。
    * `Bookmark` ([bookmark.go](file:///e:/GOProject/云原生/twitter-clone/internal/domain/bookmark.go))：推文书签。
* **Message (私信)**：
  * **定义位置**：[message.go](file:///e:/GOProject/云原生/twitter-clone/internal/domain/message.go)
  * **核心属性**：ID, SenderID, ReceiverID, Content, MessageType (text/image/video), IsRead, CreatedAt。
* **Notification (通知)**：
  * **定义位置**：[notification.go](file:///e:/GOProject/云原生/twitter-clone/internal/domain/notification.go)
  * **核心属性**：ID, UserID (接收人), SenderID (触发人), Type (like/follow/retweet/reply/mention), SourceID (源实体如推文ID), IsRead, CreatedAt。

---

## 2. 关键业务数据流向

### A. 推文发布与 Timeline 写扩散（Write Path - Fan-out on Write）

系统默认采用**写扩散（Push）**机制将推文分发给关注者，从而提供极快的读性能。

```
[Client] 
   │ (HTTP POST /api/v1/tweets)
   ▼
[Gateway] 
   │ (gRPC CreateTweet)
   ▼
[Tweet-Service] ──► 1. 写入 MySQL (GORM)
   │ 
   ▼ 2. 投递 TweetPublishedEvent 至 RabbitMQ (tweet.exchange)
[RabbitMQ]
   │
   ▼ 3. 异步消费
[Timeline-Consumer]
   ├──► A. 获取发布者的所有 Followers 列表 (gRPC 调 Follow-Service)
   ├──► B. 循环将 TweetID 写入每个 Follower 在 Redis 的 ZSet 中 (key: user:timeline:user_id)
   └──► C. 调用 Agent/ES 同步接口，对推文进行向量化嵌入 (Embedding) 并写入 Elasticsearch
```

### B. 用户读取时间线（Read Path）

```
[Client]
   │ (HTTP GET /api/v1/timeline)
   ▼
[Gateway]
   │ (gRPC GetTimeline)
   ▼
[Tweet-Service]
   │
   ├──► 1. 优先读取 Redis ZSet (key: user:timeline:user_id) 获得 TweetID 列表 (支持分时段分页)
   │       │
   │       ├─── [缓存命中] ──► 2. 根据 TweetID 批量从 Redis 缓存 / MySQL 获取推文详情
   │       │
   │       └─── [缓存缺失] ──► 2. 回源查询 MySQL (聚合关注列表的所有最新推文)，并回写 Redis
   │
   ▼ 3. 并行调用 User-Service 补全推文作者的 Profile 详情
[Client 返回响应]
```

### C. 智能体问答与 RAG 混合检索流程 (RAG Flow)

```
[Client] ──► [Gateway] ──► [Agent-Service]
                               │
                               ├──► 1. 用户提问进行语义向量化 (本地 LM Studio Embedding)
                               ├──► 2. 在 Elasticsearch 进行 HNSW 向量检索 + 传统 BM25 全文检索 (混合检索)
                               ├──► 3. 提取最相关的推文/知识文本作为 Context
                               ├──► 4. 组装 Prompt，向阿里云百炼 (Qwen 模型) 发起流式对话
                               ▼
                        [流式返回回答内容]
```
