import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/network/websocket_service.dart';
import '../data/notification_repository.dart';
import '../domain/notification_model.dart';

class NotificationState {
  final bool isLoading;
  final bool isFetchingMore;
  final List<NotificationModel> notifications;
  final String nextCursor;
  final bool hasMore;
  final String? errorMessage;

  NotificationState({
    this.isLoading = false,
    this.isFetchingMore = false,
    this.notifications = const [],
    this.nextCursor = '0',
    this.hasMore = false,
    this.errorMessage,
  });

  NotificationState copyWith({
    bool? isLoading,
    bool? isFetchingMore,
    List<NotificationModel>? notifications,
    String? nextCursor,
    bool? hasMore,
    String? errorMessage,
  }) {
    return NotificationState(
      isLoading: isLoading ?? this.isLoading,
      isFetchingMore: isFetchingMore ?? this.isFetchingMore,
      notifications: notifications ?? this.notifications,
      nextCursor: nextCursor ?? this.nextCursor,
      hasMore: hasMore ?? this.hasMore,
      errorMessage: errorMessage,
    );
  }
}

class NotificationNotifier extends Notifier<NotificationState> {
  late final NotificationRepository _repository;

  @override
  NotificationState build() {
    _repository = ref.watch(notificationRepositoryProvider);
    Future.microtask(() => refresh());
    return NotificationState();
  }

  Future<void> refresh() async {
    state = state.copyWith(isLoading: true);
    try {
      final result = await _repository.getNotifications(cursor: '0', limit: 20);
      final list = result['notifications'] as List<NotificationModel>;
      final nextCursor = result['next_cursor'] as String;
      final hasMore = result['has_more'] as bool;

      state = NotificationState(
        notifications: list,
        nextCursor: nextCursor,
        hasMore: hasMore,
      );

      // Auto clear badging/unread counts since we are opening the list
      clearUnreadBadge();
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        errorMessage: e.toString().replaceAll('Exception: ', ''),
      );
    }
  }

  Future<void> fetchNextPage() async {
    if (state.isFetchingMore || !state.hasMore) return;

    state = state.copyWith(isFetchingMore: true);
    try {
      final result = await _repository.getNotifications(
        cursor: state.nextCursor,
        limit: 20,
      );
      final list = result['notifications'] as List<NotificationModel>;
      final nextCursor = result['next_cursor'] as String;
      final hasMore = result['has_more'] as bool;

      state = state.copyWith(
        isFetchingMore: false,
        notifications: [...state.notifications, ...list],
        nextCursor: nextCursor,
        hasMore: hasMore,
      );
    } catch (e) {
      state = state.copyWith(
        isFetchingMore: false,
        errorMessage: e.toString().replaceAll('Exception: ', ''),
      );
    }
  }

  Future<void> markAllAsRead() async {
    try {
      await _repository.markAllAsRead();
      final list = state.notifications.map((n) {
        if (n.isRead) return n;
        return NotificationModel(
          id: n.id,
          type: n.type,
          targetId: n.targetId,
          content: n.content,
          isRead: true,
          createdAt: n.createdAt,
          actor: n.actor,
        );
      }).toList();
      state = state.copyWith(notifications: list);
    } catch (_) {}
  }

  void clearUnreadBadge() {
    ref.read(unreadNotificationProvider.notifier).state = 0;
  }
}

final notificationNotifierProvider = NotifierProvider<NotificationNotifier, NotificationState>(NotificationNotifier.new);
