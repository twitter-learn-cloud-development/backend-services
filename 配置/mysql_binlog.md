
# 🐬 MySQL 开启 Binlog + GTID + Canal 监听配置（Docker Compose 方案）

本文用于在 Docker MySQL 环境中开启  **binlog + GTID** ，并创建用于 **Canal / CDC 数据订阅** 的账号。

---

# 🚀 一、核心目标

完成以下能力：

* ✅ 开启 binlog（数据变更日志）
* ✅ 使用 ROW 格式（推荐）
* ✅ 启用 GTID（全局事务 ID）
* ✅ 创建 CDC 监听账号（canal_worker）
* ✅ 支持 Go / Canal / Kafka 数据订阅

---

# ⚙️ 二、修改 docker-compose.yml（推荐方式）

## 📌 在 mysql 服务中添加 command 配置

<pre class="overflow-visible! px-0!" data-start="461" data-end="770"><div class="relative w-full mt-4 mb-1"><div class=""><div class="contents"><div class="border border-token-border-light border-radius-3xl corner-superellipse/1.1 rounded-3xl"><div class="relative h-full w-full border-radius-3xl bg-token-bg-elevated-secondary corner-superellipse/1.1 overflow-clip rounded-3xl lxnfua_clipPathFallback"><div class="pointer-events-none absolute inset-x-4 top-12 bottom-4"><div class="pointer-events-none sticky z-40 shrink-0 z-1!"><div class="sticky bg-token-border-light"></div></div></div><div class="relative"><div class="h-full min-h-0 min-w-0"><div class="h-full min-h-0 min-w-0"><div class=""><div class="relative"><div class=""><div class="relative z-0 flex max-w-full"><div id="code-block-viewer" dir="ltr" class="q9tKkq_viewer cm-editor z-10 light:cm-light dark:cm-light flex h-full w-full flex-col items-stretch ͼd ͼr"><div class="cm-scroller"><pre class="cm-content q9tKkq_readonly m-0"><code><span>services:</span><br/><span>  mysql:</span><br/><span>    image: mysql:8.0</span><br/><span>    container_name: mysql</span><br/><span>    ports:</span><br/><span>      - </span><span class="ͼk">"3306:3306"</span><br/><span>    environment:</span><br/><span>      MYSQL_ROOT_PASSWORD: root</span><br/><span>    command:</span><br/><span>      - --log-bin=mysql-bin</span><br/><span>      - --binlog-format=ROW</span><br/><span>      - --server-id=1</span><br/><span>      - --gtid-mode=ON</span><br/><span>      - --enforce-gtid-consistency=ON</span></code></pre></div></div></div></div></div></div></div></div><div class=""><div class=""></div></div></div></div></div></div></div></div></pre>

---

## 🧠 参数说明

| 参数                     | 作用                       |
| ------------------------ | -------------------------- |
| log-bin=mysql-bin        | 开启 binlog（二进制日志）  |
| binlog-format=ROW        | 记录“行级变化”（推荐）   |
| server-id=1              | MySQL 唯一标识（主从必须） |
| gtid-mode=ON             | 开启 GTID 全局事务 ID      |
| enforce-gtid-consistency | 强制 GTID 安全一致性       |

---

## 🔁 重启容器

<pre class="overflow-visible! px-0!" data-start="1028" data-end="1080"><div class="relative w-full mt-4 mb-1"><div class=""><div class="contents"><div class="border border-token-border-light border-radius-3xl corner-superellipse/1.1 rounded-3xl"><div class="relative h-full w-full border-radius-3xl bg-token-bg-elevated-secondary corner-superellipse/1.1 overflow-clip rounded-3xl lxnfua_clipPathFallback"><div class="pointer-events-none absolute inset-x-4 top-12 bottom-4"><div class="pointer-events-none sticky z-40 shrink-0 z-1!"><div class="sticky bg-token-border-light"></div></div></div><div class="relative"><div class="h-full min-h-0 min-w-0"><div class="h-full min-h-0 min-w-0"><div class=""><div class="relative"><div class=""><div class="relative z-0 flex max-w-full"><div id="code-block-viewer" dir="ltr" class="q9tKkq_viewer cm-editor z-10 light:cm-light dark:cm-light flex h-full w-full flex-col items-stretch ͼd ͼr"><div class="cm-scroller"><pre class="cm-content q9tKkq_readonly m-0"><code><span>docker-compose down</span><br/><span>docker-compose up </span><span class="ͼn">-d</span></code></pre></div></div></div></div></div></div></div></div><div class=""><div class=""></div></div></div></div></div></div></div></div></pre>

---

# 🔍 三、验证配置是否生效

进入 MySQL 容器：

<pre class="overflow-visible! px-0!" data-start="1118" data-end="1167"><div class="relative w-full mt-4 mb-1"><div class=""><div class="contents"><div class="border border-token-border-light border-radius-3xl corner-superellipse/1.1 rounded-3xl"><div class="relative h-full w-full border-radius-3xl bg-token-bg-elevated-secondary corner-superellipse/1.1 overflow-clip rounded-3xl lxnfua_clipPathFallback"><div class="pointer-events-none absolute inset-x-4 top-12 bottom-4"><div class="pointer-events-none sticky z-40 shrink-0 z-1!"><div class="sticky bg-token-border-light"></div></div></div><div class="relative"><div class="h-full min-h-0 min-w-0"><div class="h-full min-h-0 min-w-0"><div class=""><div class="relative"><div class=""><div class="relative z-0 flex max-w-full"><div id="code-block-viewer" dir="ltr" class="q9tKkq_viewer cm-editor z-10 light:cm-light dark:cm-light flex h-full w-full flex-col items-stretch ͼd ͼr"><div class="cm-scroller"><pre class="cm-content q9tKkq_readonly m-0"><code><span>docker exec </span><span class="ͼn">-it</span><span> mysql mysql </span><span class="ͼn">-uroot</span><span></span><span class="ͼn">-p</span></code></pre></div></div></div></div></div></div></div></div><div class=""><div class=""></div></div></div></div></div></div></div></div></pre>

执行：

<pre class="overflow-visible! px-0!" data-start="1174" data-end="1285"><div class="relative w-full mt-4 mb-1"><div class=""><div class="contents"><div class="border border-token-border-light border-radius-3xl corner-superellipse/1.1 rounded-3xl"><div class="relative h-full w-full border-radius-3xl bg-token-bg-elevated-secondary corner-superellipse/1.1 overflow-clip rounded-3xl lxnfua_clipPathFallback"><div class="pointer-events-none absolute inset-x-4 top-12 bottom-4"><div class="pointer-events-none sticky z-40 shrink-0 z-1!"><div class="sticky bg-token-border-light"></div></div></div><div class="relative"><div class="h-full min-h-0 min-w-0"><div class="h-full min-h-0 min-w-0"><div class=""><div class="relative"><div class=""><div class="relative z-0 flex max-w-full"><div id="code-block-viewer" dir="ltr" class="q9tKkq_viewer cm-editor z-10 light:cm-light dark:cm-light flex h-full w-full flex-col items-stretch ͼd ͼr"><div class="cm-scroller"><pre class="cm-content q9tKkq_readonly m-0"><code><span>SHOW VARIABLES </span><span class="ͼg">LIKE</span><span></span><span class="ͼk">'log_bin'</span><span>;</span><br/><span>SHOW VARIABLES </span><span class="ͼg">LIKE</span><span></span><span class="ͼk">'binlog_format'</span><span>;</span><br/><span>SHOW VARIABLES </span><span class="ͼg">LIKE</span><span></span><span class="ͼk">'server_id'</span><span>;</span></code></pre></div></div></div></div></div></div></div></div><div class=""><div class=""></div></div></div></div></div></div></div></div></pre>

---

## ✔ 期望结果

| 变量          | 期望值 |
| ------------- | ------ |
| log_bin       | ON     |
| binlog_format | ROW    |
| server_id     | 1      |

---

# 👤 四、创建 Canal / CDC 监听账号

用于数据订阅（如 Canal、Kafka、Go binlog 解析）

---

## 1️⃣ 创建用户

<pre class="overflow-visible! px-0!" data-start="1481" data-end="1552"><div class="relative w-full mt-4 mb-1"><div class=""><div class="contents"><div class="border border-token-border-light border-radius-3xl corner-superellipse/1.1 rounded-3xl"><div class="relative h-full w-full border-radius-3xl bg-token-bg-elevated-secondary corner-superellipse/1.1 overflow-clip rounded-3xl lxnfua_clipPathFallback"><div class="pointer-events-none absolute inset-x-4 top-12 bottom-4"><div class="pointer-events-none sticky z-40 shrink-0 z-1!"><div class="sticky bg-token-border-light"></div></div></div><div class="relative"><div class="h-full min-h-0 min-w-0"><div class="h-full min-h-0 min-w-0"><div class=""><div class="relative"><div class=""><div class="relative z-0 flex max-w-full"><div id="code-block-viewer" dir="ltr" class="q9tKkq_viewer cm-editor z-10 light:cm-light dark:cm-light flex h-full w-full flex-col items-stretch ͼd ͼr"><div class="cm-scroller"><pre class="cm-content q9tKkq_readonly m-0"><code><span class="ͼg">CREATE</span><span></span><span class="ͼg">USER</span><span></span><span class="ͼk">'canal_worker'</span><span>@</span><span class="ͼk">'%'</span><span> IDENTIFIED </span><span class="ͼg">BY</span><span></span><span class="ͼk">'Canal@123456'</span><span>;</span></code></pre></div></div></div></div></div></div></div></div><div class=""><div class=""></div></div></div></div></div></div></div></div></pre>

---

## 2️⃣ 授权权限

<pre class="overflow-visible! px-0!" data-start="1572" data-end="1664"><div class="relative w-full mt-4 mb-1"><div class=""><div class="contents"><div class="border border-token-border-light border-radius-3xl corner-superellipse/1.1 rounded-3xl"><div class="relative h-full w-full border-radius-3xl bg-token-bg-elevated-secondary corner-superellipse/1.1 overflow-clip rounded-3xl lxnfua_clipPathFallback"><div class="pointer-events-none absolute inset-x-4 top-12 bottom-4"><div class="pointer-events-none sticky z-40 shrink-0 z-1!"><div class="sticky bg-token-border-light"></div></div></div><div class="relative"><div class="h-full min-h-0 min-w-0"><div class="h-full min-h-0 min-w-0"><div class=""><div class="relative"><div class=""><div class="relative z-0 flex max-w-full"><div id="code-block-viewer" dir="ltr" class="q9tKkq_viewer cm-editor z-10 light:cm-light dark:cm-light flex h-full w-full flex-col items-stretch ͼd ͼr"><div class="cm-scroller"><pre class="cm-content q9tKkq_readonly m-0"><code><span class="ͼg">GRANT</span><span></span><span class="ͼg">SELECT</span><span>, REPLICATION SLAVE, REPLICATION CLIENT </span><span class="ͼg">ON</span><span></span><span class="ͼg">*</span><span>.</span><span class="ͼg">*</span><span></span><span class="ͼg">TO</span><span></span><span class="ͼk">'canal_worker'</span><span>@</span><span class="ͼk">'%'</span><span>;</span></code></pre></div></div></div></div></div></div></div></div><div class=""><div class=""></div></div></div></div></div></div></div></div></pre>

---

## 3️⃣ 刷新权限

<pre class="overflow-visible! px-0!" data-start="1684" data-end="1712"><div class="relative w-full mt-4 mb-1"><div class=""><div class="contents"><div class="border border-token-border-light border-radius-3xl corner-superellipse/1.1 rounded-3xl"><div class="relative h-full w-full border-radius-3xl bg-token-bg-elevated-secondary corner-superellipse/1.1 overflow-clip rounded-3xl lxnfua_clipPathFallback"><div class="pointer-events-none absolute inset-x-4 top-12 bottom-4"><div class="pointer-events-none sticky z-40 shrink-0 z-1!"><div class="sticky bg-token-border-light"></div></div></div><div class="relative"><div class="h-full min-h-0 min-w-0"><div class="h-full min-h-0 min-w-0"><div class=""><div class="relative"><div class=""><div class="relative z-0 flex max-w-full"><div id="code-block-viewer" dir="ltr" class="q9tKkq_viewer cm-editor z-10 light:cm-light dark:cm-light flex h-full w-full flex-col items-stretch ͼd ͼr"><div class="cm-scroller"><pre class="cm-content q9tKkq_readonly m-0"><code><span>FLUSH </span><span class="ͼg">PRIVILEGES</span><span>;</span></code></pre></div></div></div></div></div></div></div></div><div class=""><div class=""></div></div></div></div></div></div></div></div></pre>

---

# 🔐 五、权限说明

| 权限               | 作用                |
| ------------------ | ------------------- |
| SELECT             | 读取数据            |
| REPLICATION SLAVE  | 读取 binlog（核心） |
| REPLICATION CLIENT | 查看主从状态        |

---

# 🧬 六、整体数据链路（理解用）

<pre class="overflow-visible! px-0!" data-start="1874" data-end="2043"><div class="relative w-full mt-4 mb-1"><div class=""><div class="contents"><div class="relative"><div class="h-full min-h-0 min-w-0"><div class="h-full min-h-0 min-w-0"><div class="border border-token-border-light border-radius-3xl corner-superellipse/1.1 rounded-3xl"><div class="h-full w-full border-radius-3xl bg-token-bg-elevated-secondary corner-superellipse/1.1 overflow-clip rounded-3xl lxnfua_clipPathFallback"><div class="pointer-events-none absolute end-1.5 top-1 z-2 md:end-2 md:top-1"></div><div class="relative"><div class="pe-11 pt-3"><div class="relative z-0 flex max-w-full"><div id="code-block-viewer" dir="ltr" class="q9tKkq_viewer cm-editor z-10 light:cm-light dark:cm-light flex h-full w-full flex-col items-stretch ͼd ͼr"><div class="cm-scroller"><pre class="cm-content q9tKkq_readonly m-0"><code><span>MySQL 执行 SQL</span><br/><span>      ↓</span><br/><span>生成事务（Transaction）</span><br/><span>      ↓</span><br/><span>写入 binlog（ROW 模式）</span><br/><span>      ↓</span><br/><span>生成 GTID</span><br/><span>      ↓</span><br/><span>canal_worker 读取 binlog</span><br/><span>      ↓</span><br/><span>解析数据变更</span><br/><span>      ↓</span><br/><span>推送到 Kafka / Redis / ES</span></code></pre></div></div></div></div></div></div></div></div></div><div class=""><div class=""></div></div></div></div></div></div></pre>

---

# 🎯 七、最终结果

完成后，你的 MySQL 将具备：

* 📡 可被订阅的数据变更流（CDC）
* 🔁 GTID 可追踪事务系统
* 🧠 稳定的主从复制基础
* 🚀 可对接 Canal / Kafka / Go binlog 解析
