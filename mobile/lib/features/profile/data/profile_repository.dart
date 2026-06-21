import 'package:dio/dio.dart';
import '../../auth/domain/user_model.dart';
import '../../tweet/domain/tweet_model.dart';

class ProfileRepository {
  final Dio _dio;

  ProfileRepository(this._dio);

  // Fetch full aggregated user profile (BFF)
  Future<Map<String, dynamic>> getFullProfile(String userId) async {
    try {
      final response = await _dio.get('/users/$userId/full_profile');
      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        
        final user = User.fromJson(data['user']);
        
        final stats = data['stats'] as Map<String, dynamic>? ?? {};
        final followerCount = stats['followers'] as int? ?? 0;
        final followingCount = stats['following'] as int? ?? 0;
        
        final tweetsRaw = data['recent_tweets'] as List? ?? [];
        final recentTweets = tweetsRaw.map((e) => Tweet.fromJson(e)).toList();

        return {
          'user': user,
          'follower_count': followerCount,
          'following_count': followingCount,
          'recent_tweets': recentTweets,
        };
      } else {
        throw Exception(response.data['error'] ?? '获取资料失败');
      }
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络错误');
    }
  }

  // Check if current user is following target user
  Future<bool> checkFollowingStatus(String targetUserId) async {
    try {
      final response = await _dio.get('/follows/$targetUserId/status');
      if (response.statusCode == 200) {
        return response.data['is_following'] as bool? ?? false;
      }
      return false;
    } on DioException catch (_) {
      return false;
    }
  }

  // Follow a user
  Future<void> followUser(String targetUserId) async {
    try {
      final response = await _dio.post('/follows', data: {
        'followee_id': targetUserId,
      });
      if (response.statusCode != 200) {
        throw Exception(response.data['error'] ?? '关注失败');
      }
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络错误');
    }
  }

  // Unfollow a user
  Future<void> unfollowUser(String targetUserId) async {
    try {
      final response = await _dio.delete('/follows/$targetUserId');
      if (response.statusCode != 200) {
        throw Exception(response.data['error'] ?? '取消关注失败');
      }
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络错误');
    }
  }

  // Fetch tabs: Likes
  Future<List<Tweet>> getUserLikes(String userId) async {
    try {
      final response = await _dio.get('/users/$userId/likes', queryParameters: {'limit': 20});
      if (response.statusCode == 200) {
        final tweetsRaw = response.data['tweets'] as List? ?? [];
        return tweetsRaw.map((e) => Tweet.fromJson(e)).toList();
      }
      return [];
    } catch (_) {
      return [];
    }
  }

  // Fetch tabs: Media
  Future<List<Tweet>> getUserMedia(String userId) async {
    try {
      final response = await _dio.get('/users/$userId/media', queryParameters: {'limit': 20});
      if (response.statusCode == 200) {
        final tweetsRaw = response.data['tweets'] as List? ?? [];
        return tweetsRaw.map((e) => Tweet.fromJson(e)).toList();
      }
      return [];
    } catch (_) {
      return [];
    }
  }
}
