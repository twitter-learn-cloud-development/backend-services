import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:cached_network_image/cached_network_image.dart';
import 'notification_notifier.dart';
import '../domain/notification_model.dart';
import '../../../core/constants/colors.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/utils/date_formatter.dart';

class NotificationScreen extends ConsumerStatefulWidget {
  const NotificationScreen({super.key});

  @override
  ConsumerState<NotificationScreen> createState() => _NotificationScreenState();
}

class _NotificationScreenState extends ConsumerState<NotificationScreen> {
  final ScrollController _scrollController = ScrollController();

  @override
  void initState() {
    super.initState();
    _scrollController.addListener(_onScroll);
  }

  @override
  void dispose() {
    _scrollController.removeListener(_onScroll);
    _scrollController.dispose();
    super.dispose();
  }

  void _onScroll() {
    if (_scrollController.position.pixels >=
        _scrollController.position.maxScrollExtent * 0.9) {
      ref.read(notificationNotifierProvider.notifier).fetchNextPage();
    }
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(notificationNotifierProvider);
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    return Scaffold(
      backgroundColor: isDark ? AppColors.darkBg : AppColors.lightBg,
      appBar: AppBar(
        title: const Text('通知'),
        centerTitle: true,
        actions: [
          if (state.notifications.any((n) => !n.isRead))
            IconButton(
              icon: const Icon(Icons.done_all, color: AppColors.primary),
              tooltip: '全部标记为已读',
              onPressed: () {
                ref.read(notificationNotifierProvider.notifier).markAllAsRead();
              },
            ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () => ref.read(notificationNotifierProvider.notifier).refresh(),
        color: AppColors.primary,
        child: state.isLoading && state.notifications.isEmpty
            ? const Center(child: CircularProgressIndicator(color: AppColors.primary))
            : state.notifications.isEmpty
                ? _buildEmptyState(isDark)
                : ListView.separated(
                    controller: _scrollController,
                    itemCount: state.notifications.length + 1,
                    separatorBuilder: (context, index) => const Divider(height: 1),
                    itemBuilder: (context, index) {
                      if (index == state.notifications.length) {
                        return state.hasMore
                            ? const Padding(
                                padding: EdgeInsets.symmetric(vertical: 16.0),
                                child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
                              )
                            : const SizedBox(height: 20);
                      }

                      final notification = state.notifications[index];
                      return _buildNotificationItem(context, notification, isDark, theme);
                    },
                  ),
      ),
    );
  }

  Widget _buildEmptyState(bool isDark) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
            Icons.notifications_none_outlined,
            size: 80,
            color: isDark ? Colors.grey[700] : Colors.grey[300],
          ),
          const SizedBox(height: 16),
          Text(
            '暂无通知',
            style: TextStyle(
              fontSize: 18,
              fontWeight: FontWeight.bold,
              color: isDark ? Colors.white : Colors.black,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            '当有人点赞、评论您的帖子或关注您时，通知会出现在这里。',
            textAlign: TextAlign.center,
            style: TextStyle(
              color: isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary,
              fontSize: 14,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildNotificationItem(
      BuildContext context, NotificationModel notification, bool isDark, ThemeData theme) {
    final actor = notification.actor;
    final avatarUrl = (actor != null && actor.avatar.isNotEmpty)
        ? DioClient.getMediaUrl(actor.avatar)
        : '';

    IconData iconData;
    Color iconColor;
    String actionText = '';

    switch (notification.type) {
      case 'like':
        iconData = Icons.favorite;
        iconColor = AppColors.likeColor;
        actionText = '赞了你的推文';
        break;
      case 'comment':
        iconData = Icons.comment;
        iconColor = AppColors.primary;
        actionText = '回复了你的推文: "${notification.content}"';
        break;
      case 'follow':
        iconData = Icons.person_add;
        iconColor = AppColors.retweetColor;
        actionText = '关注了你';
        break;
      default:
        iconData = Icons.notifications;
        iconColor = Colors.grey;
        actionText = notification.content;
    }

    return InkWell(
      onTap: () {
        // Navigate based on type
        if (notification.type == 'like' || notification.type == 'comment') {
          context.push('/tweet/${notification.targetId}');
        } else if (notification.type == 'follow') {
          context.push('/profile/${actor?.id ?? notification.targetId}');
        }
      },
      child: Container(
        color: notification.isRead
            ? Colors.transparent
            : (isDark
                ? AppColors.primary.withOpacity(0.05)
                : AppColors.primary.withOpacity(0.03)),
        padding: const EdgeInsets.symmetric(horizontal: 16.0, vertical: 12.0),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Unread dot indicator
            if (!notification.isRead)
              Container(
                margin: const EdgeInsets.only(top: 18.0, right: 8.0),
                width: 8,
                height: 8,
                decoration: const BoxDecoration(
                  color: AppColors.primary,
                  shape: BoxShape.circle,
                ),
              )
            else
              const SizedBox(width: 16),

            // Icon of notification type
            Padding(
              padding: const EdgeInsets.only(top: 8.0, right: 12.0),
              child: Icon(iconData, color: iconColor, size: 24),
            ),

            // Actor avatar & Content
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  GestureDetector(
                    onTap: () {
                      if (actor != null) {
                        context.push('/profile/${actor.id}');
                      }
                    },
                    child: CircleAvatar(
                      radius: 18,
                      backgroundColor: isDark ? AppColors.darkBorder : AppColors.lightBorder,
                      backgroundImage: avatarUrl.isNotEmpty
                          ? CachedNetworkImageProvider(avatarUrl)
                          : null,
                      child: avatarUrl.isEmpty
                          ? const Icon(Icons.person, size: 18, color: Colors.grey)
                          : null,
                    ),
                  ),
                  const SizedBox(height: 8),
                  RichText(
                    text: TextSpan(
                      style: TextStyle(
                        fontSize: 15,
                        color: isDark ? Colors.white : Colors.black,
                      ),
                      children: [
                        TextSpan(
                          text: actor?.username ?? '未知用户',
                          style: const TextStyle(fontWeight: FontWeight.bold),
                        ),
                        const TextSpan(text: ' '),
                        TextSpan(
                          text: actionText,
                          style: TextStyle(
                            color: isDark
                                ? AppColors.darkTextSecondary
                                : AppColors.lightTextSecondary,
                          ),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    DateFormatter.formatRelative(notification.createdAt),
                    style: const TextStyle(color: Colors.grey, fontSize: 12),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
