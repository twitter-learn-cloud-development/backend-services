import 'package:dio/dio.dart';
import '../../tweet/domain/tweet_model.dart';

class SearchRepository {
  final Dio _dio;

  SearchRepository(this._dio);

  // Fetch trending topics
  Future<List<Map<String, dynamic>>> getTrendingTopics({int limit = 10}) async {
    try {
      final response = await _dio.get('/trends', queryParameters: {'limit': limit});
      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        final topicsRaw = data['topics'] as List? ?? [];
        return topicsRaw.map((e) => {
          'topic': e['topic']?.toString() ?? '',
          'score': e['score'] is num ? (e['score'] as num).toInt() : 0,
        }).toList();
      }
      return [];
    } catch (_) {
      return [];
    }
  }

  // Search tweets
  Future<Map<String, dynamic>> searchTweets(
    String query, {
    String? cursor,
    int limit = 20,
  }) async {
    try {
      final queryParameters = {
        'q': query,
        'limit': limit,
      };
      if (cursor != null && cursor != '0' && cursor.isNotEmpty) {
        queryParameters['cursor'] = cursor;
      }

      final response = await _dio.get('/search', queryParameters: queryParameters);
      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        final tweetsRaw = data['tweets'] as List? ?? [];
        final tweets = tweetsRaw.map((e) => Tweet.fromJson(e)).toList();
        final nextCursor = data['next_cursor']?.toString() ?? '0';
        final hasMore = data['has_more'] as bool? ?? false;
        
        return {
          'tweets': tweets,
          'next_cursor': nextCursor,
          'has_more': hasMore,
        };
      } else {
        throw Exception(response.data['error'] ?? '搜索失败');
      }
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络错误');
    }
  }
}
