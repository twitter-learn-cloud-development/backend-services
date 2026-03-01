# 🎨 Twitter Clone Frontend Design & Implementation Plan

## 1. 技术栈选型 (Technology Stack)

基于 **Vue 3** 生态的现代化前端架构，注重开发效率与用户体验（类似 Twitter 原生体验）。

| 模块 | 技术选型 | 理由 |
| :--- | :--- | :--- |
| **框架** | **Vue 3 (Composition API)** | 灵活、高性能，`<script setup>` 语法简洁。 |
| **构建工具** | **Vite** | 极速冷启动和热更新 (HMR)，开发体验远超 Webpack。 |
| **语言** | **TypeScript** | 强类型约束，完美对接后端 Go 结构体定义，减少接口联调错误。 |
| **状态管理** | **Pinia** | Vuex 的继任者，轻量级，TypeScript 支持更友好。 |
| **路由** | **Vue Router 4** | 官方路由管理。 |
| **UI 样式** | **Tailwind CSS** | **关键决策**。不使用 Element Plus 等笨重的组件库，而是用原子化 CSS 快速构建类似 Twitter 的高度定制界面 (响应式、Dark Mode)。 |
| **网络请求** | **Axios** | 标准 HTTP 客户端，配合拦截器处理 JWT Token。 |
| **图标库** | **Heroicons (Vue)** | 风格简洁现代，由 Tailwind CSS 团队维护。 |
| **时间处理** | **Day.js** | 轻量级 (2KB) 替代 Moment.js，用于“3分钟前”、“刚刚”等显示。 |

---

## 2. 项目结构规划 (Project Structure)

```
web/
├── public/              # 静态资源 (favicon, robots.txt)
├── src/
│   ├── api/             # API 接口层 (对应后端 Controller)
│   │   ├── auth.ts      # 登录/注册
│   │   ├── tweet.ts     # 推文/点赞/评论
│   │   ├── user.ts      # 用户资料/关注
│   │   └── upload.ts    # 文件上传
│   ├── assets/          # 图片、全局样式
│   ├── components/      # 公共组件
│   │   ├── TweetCard.vue    # 推文卡片 (核心)
│   │   ├── NavBar.vue       # 侧边导航栏
│   │   ├── ComposeBox.vue   # 发推框
│   │   └── AuthModal.vue    # 登录/注册弹窗
│   ├── layout/
│   │   └── MainLayout.vue   # 主布局 (左侧导航 + 中间内容 + 右侧推荐)
│   ├── router/          # 路由配置
│   ├── stores/          # Pinia 状态仓库 (UserStore, FeedStore)
│   ├── utils/           # 工具函数 (request.ts, date.ts)
│   ├── views/           # 页面视图 (Page)
│   │   ├── Home.vue         # 首页信息流
│   │   ├── Profile.vue      # 个人主页
│   │   ├── TweetDetail.vue  # 推文详情
│   │   ├── Explore.vue      # 探索/热搜
│   │   └── Login.vue        # 登录页
│   ├── App.vue          # 根组件
│   └── main.ts          # 入口文件
├── env.d.ts             # 类型声明
├── index.html           # HTML 模板
├── package.json         # 依赖管理
├── tailwind.config.js   # Tailwind 配置
├── tsconfig.json        # TS 配置
└── vite.config.ts       # Vite 配置
```

---

## 3. 开发阶段规划 (Development Phases)

### ✅ Phase 1: 基础设施搭建 (Infrastructure)
*   **目标**: 项目初始化，配置好基础工具链。
*   **任务**:
    *   `npm create vite@latest` 初始化项目。
    *   安装 Tailwind CSS, Axios, Pinia, Vue Router。
    *   封装 `request.ts` (Axios 拦截器：请求带 Token，响应统一处理 Error)。

### 🚀 Phase 2: 认证模块 (Authentication)
*   **目标**: 用户能注册、登录并保持状态。
*   **对应后端接口**: `/auth/register`, `/auth/login`, `/users/me`。
*   **页面**: Login/Register Page。
*   **逻辑**: JWT 存储 (LocalStorage), Pinia UserStore 状态同步。

### 📰 Phase 3: 核心信息流 (Timeline & Compose)
*   **目标**: 用户能发推、看推。
*   **对应后端接口**: `/tweets` (POST), `/feeds`, `/upload`。
*   **组件**: `TweetCard` (展示头像、内容、图片、时间), `ComposeBox` (发推)。

### ❤️ Phase 4: 互动功能 (Interactions)
*   **目标**: 点赞、评论、查看详情。
*   **对应后端接口**: `/tweets/:id`, `/tweets/:id/like`, `/comments`。
*   **页面**: `TweetDetail` (展示推文 + 评论列表)。

### 👤 Phase 5: 用户系统 (Profile & Social)
*   **目标**: 查看个人主页，关注/取关。
*   **对应后端接口**: `/users/:id`, `/users/:id/timeline`, `/follow`, `/followers`。
*   **页面**: `Profile` (头部展示统计信息，下部展示 Timeline)。

---

## 4. 关键交互流程 (Key Interactions)

1.  **JWT 自动续期/失效处理**:
    *   Axios 响应拦截器监听 `401 Unauthorized` -> 清除 Token -> 跳转登录页。

2.  **无限滚动 (Infinite Scroll)**:
    *   **后端**: 接受 `cursor` 参数，返回 `next_cursor`。
    *   **前端**: 使用 `IntersectionObserver` 监听底部元素，触底时自动请求下一页数据并 `concat` 到当前列表。

3.  **图片上传预览**:
    *   用户选择图片 -> `URL.createObjectURL` 本地预览 -> 点击“发布” -> 上传 API -> 获取 URL -> 提交推文 API。

4.  **全局 Loading 与 错误提示**:
    *   封装统一的 Toast 组件 (或使用 Vant/ElementPlus 提供的)，在 API 错误时自动弹出。
