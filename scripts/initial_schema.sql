-- Twitter Clone 数据库初始化脚本 (Manual Setup)
-- 注意：本项目后端使用 GORM AutoMigrate，服务启动时会自动创建表逻辑。
-- 如果你希望手动建表，可以使用以下 DDL。

CREATE DATABASE IF NOT EXISTS `twitter` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `twitter`;

-- 1. 用户表
CREATE TABLE IF NOT EXISTS `users` (
    `id` BIGINT UNSIGNED PRIMARY KEY COMMENT 'Snowflake ID',
    `username` VARCHAR(32) NOT NULL COMMENT '用户名',
    `password_hash` VARCHAR(255) NOT NULL COMMENT '密码哈希',
    `email` VARCHAR(128) NOT NULL COMMENT '邮箱',
    `avatar` VARCHAR(255) DEFAULT '' COMMENT '头像URL',
    `bio` VARCHAR(255) DEFAULT '' COMMENT '简介',
    `cover_url` VARCHAR(255) DEFAULT '' COMMENT '封面图URL',
    `website` VARCHAR(255) DEFAULT '' COMMENT '个人网站',
    `location` VARCHAR(100) DEFAULT '' COMMENT '地理位置',
    `created_at` BIGINT NOT NULL COMMENT '创建时间戳',
    `updated_at` BIGINT NOT NULL COMMENT '更新时间戳',
    `deleted_at` BIGINT DEFAULT 0 COMMENT '删除时间戳',
    UNIQUE INDEX `uk_username` (`username`),
    UNIQUE INDEX `uk_email` (`email`)
) ENGINE=InnoDB;

-- 2. 推文表
CREATE TABLE IF NOT EXISTS `tweets` (
    `id` BIGINT UNSIGNED PRIMARY KEY COMMENT 'Snowflake ID',
    `user_id` BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
    `parent_id` BIGINT UNSIGNED DEFAULT 0 COMMENT '父推文ID',
    `content` TEXT COMMENT '内容',
    `media_urls` JSON COMMENT '媒体地址',
    `type` TINYINT DEFAULT 0 COMMENT '类型(0文1图2视)',
    `visible_type` TINYINT DEFAULT 0 COMMENT '可见性',
    `created_at` BIGINT NOT NULL COMMENT '创建时间',
    `updated_at` BIGINT NOT NULL COMMENT '更新时间',
    `deleted_at` BIGINT DEFAULT 0 COMMENT '删除时间',
    INDEX `idx_user_created` (`user_id`, `created_at`),
    INDEX `idx_parent` (`parent_id`)
) ENGINE=InnoDB;

-- 3. 关注关系表
CREATE TABLE IF NOT EXISTS `follows` (
    `id` BIGINT UNSIGNED PRIMARY KEY,
    `follower_id` BIGINT UNSIGNED NOT NULL,
    `followee_id` BIGINT UNSIGNED NOT NULL,
    `created_at` BIGINT NOT NULL,
    `deleted_at` BIGINT DEFAULT 0,
    INDEX `idx_follower` (`follower_id`),
    INDEX `idx_followee` (`followee_id`)
) ENGINE=InnoDB;

-- 4. 点赞表
CREATE TABLE IF NOT EXISTS `likes` (
    `id` BIGINT UNSIGNED PRIMARY KEY,
    `user_id` BIGINT UNSIGNED NOT NULL,
    `tweet_id` BIGINT UNSIGNED NOT NULL,
    `created_at` BIGINT NOT NULL,
    UNIQUE INDEX `uk_user_tweet` (`user_id`, `tweet_id`),
    INDEX `idx_tweet` (`tweet_id`)
) ENGINE=InnoDB;

-- 5. 评论表
CREATE TABLE IF NOT EXISTS `comments` (
    `id` BIGINT UNSIGNED PRIMARY KEY,
    `user_id` BIGINT UNSIGNED NOT NULL,
    `tweet_id` BIGINT UNSIGNED NOT NULL,
    `parent_id` BIGINT UNSIGNED DEFAULT 0,
    `content` TEXT NOT NULL,
    `created_at` BIGINT NOT NULL,
    `deleted_at` BIGINT DEFAULT 0,
    INDEX `idx_user` (`user_id`),
    INDEX `idx_tweet` (`tweet_id`),
    INDEX `idx_parent` (`parent_id`)
) ENGINE=InnoDB;

-- 6. 投票相关表
CREATE TABLE IF NOT EXISTS `polls` (
    `id` BIGINT UNSIGNED PRIMARY KEY,
    `tweet_id` BIGINT UNSIGNED NOT NULL,
    `question` VARCHAR(255) NOT NULL,
    `end_time` BIGINT NOT NULL,
    `created_at` BIGINT NOT NULL,
    INDEX `idx_tweet` (`tweet_id`)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS `poll_options` (
    `id` BIGINT UNSIGNED PRIMARY KEY,
    `poll_id` BIGINT UNSIGNED NOT NULL,
    `text` VARCHAR(255) NOT NULL,
    `vote_count` INT DEFAULT 0,
    INDEX `idx_poll` (`poll_id`)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS `poll_votes` (
    `id` BIGINT UNSIGNED PRIMARY KEY,
    `poll_id` BIGINT UNSIGNED NOT NULL,
    `option_id` BIGINT UNSIGNED NOT NULL,
    `user_id` BIGINT UNSIGNED NOT NULL,
    `created_at` BIGINT NOT NULL,
    UNIQUE INDEX `uk_poll_user` (`poll_id`, `user_id`)
) ENGINE=InnoDB;

-- 7. 书签与转发
CREATE TABLE IF NOT EXISTS `bookmarks` (
    `id` BIGINT UNSIGNED PRIMARY KEY,
    `user_id` BIGINT UNSIGNED NOT NULL,
    `tweet_id` BIGINT UNSIGNED NOT NULL,
    `created_at` BIGINT NOT NULL,
    INDEX `idx_user_bookmark` (`user_id`, `tweet_id`)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS `retweets` (
    `id` BIGINT UNSIGNED PRIMARY KEY,
    `user_id` BIGINT UNSIGNED NOT NULL,
    `tweet_id` BIGINT UNSIGNED NOT NULL,
    `created_at` BIGINT NOT NULL,
    UNIQUE INDEX `uk_user_tweet` (`user_id`, `tweet_id`),
    INDEX `idx_tweet` (`tweet_id`)
) ENGINE=InnoDB;

-- 8. 私信消息
CREATE TABLE IF NOT EXISTS `messages` (
    `id` BIGINT UNSIGNED PRIMARY KEY,
    `conversation_id` VARCHAR(64) NOT NULL,
    `sender_id` BIGINT UNSIGNED NOT NULL,
    `receiver_id` BIGINT UNSIGNED NOT NULL,
    `content` TEXT,
    `is_read` BOOLEAN DEFAULT FALSE,
    `created_at` BIGINT NOT NULL,
    INDEX `idx_conversation_time` (`conversation_id`, `created_at`),
    INDEX `idx_sender` (`sender_id`),
    INDEX `idx_receiver` (`receiver_id`)
) ENGINE=InnoDB;
