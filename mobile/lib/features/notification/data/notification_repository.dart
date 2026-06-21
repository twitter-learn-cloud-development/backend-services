import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../auth/presentation/auth_notifier.dart';
import '../domain/notification_model.dart';

class NotificationRepository {
  final Dio _dio;

  NotificationRepository(this._dio);

  // Fetch notification list with cursor pagination
  Future<Map<String, dynamic>> getNotifications({String cursor = '0', int limit = 20}) async {
    final Map<String, dynamic> queryParameters = {
      'cursor': cursor,
      'limit': limit,
    };
    final response = await _dio.get('/notifications', queryParameters: queryParameters);
    
    final List<dynamic> dataList = response.data['notifications'] ?? [];
    final notifications = dataList.map((e) => NotificationModel.fromJson(e)).toList();
    
    return {
      'notifications': notifications,
      'next_cursor': response.data['next_cursor']?.toString() ?? '0',
      'has_more': response.data['has_more'] ?? false,
    };
  }

  // Mark specific notification IDs as read
  Future<void> markAsRead(List<String> ids) async {
    // Parse to int list since backend expects IDs as list of uint64
    final intIds = ids.map((id) => int.tryParse(id)).whereType<int>().toList();
    await _dio.put('/notifications/read', data: {'ids': intIds});
  }

  // Mark all notifications as read
  Future<void> markAllAsRead() async {
    await _dio.put('/notifications/read-all');
  }

  // Get unread notifications count
  Future<int> getUnreadCount() async {
    final response = await _dio.get('/notifications/unread-count');
    return response.data['count'] is int ? response.data['count'] : 0;
  }
}

final notificationRepositoryProvider = Provider<NotificationRepository>((ref) {
  final dio = ref.watch(dioProvider);
  return NotificationRepository(dio);
});
