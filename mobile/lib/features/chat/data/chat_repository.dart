import 'package:dio/dio.dart';
import '../domain/message_model.dart';

class ChatRepository {
  final Dio _dio;

  ChatRepository(this._dio);

  // Fetch recent conversation list
  Future<Map<String, dynamic>> getConversations({
    String? cursor,
    int limit = 20,
  }) async {
    try {
      final Map<String, dynamic> queryParams = {'limit': limit};
      if (cursor != null && cursor.isNotEmpty) {
        queryParams['cursor'] = cursor;
      }
      
      final response = await _dio.get('/messenger/conversations', queryParameters: queryParams);
      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        final convsRaw = data['conversations'] as List? ?? [];
        final conversations = convsRaw.map((e) => ConversationModel.fromJson(e)).toList();
        final nextCursor = data['next_cursor']?.toString() ?? '';
        final hasMore = data['has_more'] as bool? ?? false;
        
        return {
          'conversations': conversations,
          'next_cursor': nextCursor,
          'has_more': hasMore,
        };
      } else {
        throw Exception(response.data['error'] ?? '获取会话列表失败');
      }
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络异常');
    }
  }

  // Fetch messages with a peer
  Future<Map<String, dynamic>> getMessages(
    String peerId, {
    String? cursor,
    int limit = 20,
  }) async {
    try {
      final Map<String, dynamic> queryParams = {'limit': limit};
      if (cursor != null && cursor.isNotEmpty) {
        queryParams['cursor'] = cursor;
      }
      
      final response = await _dio.get('/messenger/conversations/$peerId/messages', queryParameters: queryParams);
      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        final msgsRaw = data['messages'] as List? ?? [];
        final messages = msgsRaw.map((e) => MessageModel.fromJson(e)).toList();
        final nextCursor = data['next_cursor']?.toString() ?? '';
        final hasMore = data['has_more'] as bool? ?? false;
        
        return {
          'messages': messages,
          'next_cursor': nextCursor,
          'has_more': hasMore,
        };
      } else {
        throw Exception(response.data['error'] ?? '获取消息记录失败');
      }
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络异常');
    }
  }

  // Send message
  Future<MessageModel> sendMessage(String receiverId, String content) async {
    try {
      final response = await _dio.post('/messenger/messages', data: {
        'receiver_id': receiverId,
        'content': content,
      });
      if (response.statusCode == 200) {
        return MessageModel.fromJson(response.data);
      } else {
        throw Exception(response.data['error'] ?? '发送失败');
      }
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络异常');
    }
  }
}
