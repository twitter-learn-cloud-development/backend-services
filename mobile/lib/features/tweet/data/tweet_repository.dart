import 'dart:io';
import 'package:dio/dio.dart';
import '../domain/tweet_model.dart';
import '../domain/comment_model.dart';
import '../../auth/domain/user_model.dart';

class TweetRepository {
  final Dio _dio;

  TweetRepository(this._dio);

  // Fetch following feeds (Timeline)
  Future<Map<String, dynamic>> getFeeds({
    String? cursor,
    int limit = 20,
  }) async {
    try {
      final Map<String, dynamic> queryParameters = {
        'limit': limit,
      };
      if (cursor != null && cursor != '0' && cursor.isNotEmpty) {
        queryParameters['cursor'] = cursor;
      }

      final response = await _dio.get('/feeds', queryParameters: queryParameters);
      
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
        throw Exception(response.data['error'] ?? '获取推文失败');
      }
    } on DioException catch (e) {
      final errorMsg = e.response?.data?['error'] ?? e.message ?? '网络连接错误';
      throw Exception(errorMsg);
    }
  }

  // Fetch user timeline
  Future<Map<String, dynamic>> getUserTimeline(
    String userId, {
    String? cursor,
    int limit = 20,
  }) async {
    try {
      final Map<String, dynamic> queryParameters = {
        'limit': limit,
      };
      if (cursor != null && cursor != '0' && cursor.isNotEmpty) {
        queryParameters['cursor'] = cursor;
      }

      final response = await _dio.get('/users/$userId/timeline', queryParameters: queryParameters);
      
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
        throw Exception(response.data['error'] ?? '获取用户推文失败');
      }
    } on DioException catch (e) {
      final errorMsg = e.response?.data?['error'] ?? e.message ?? '网络连接错误';
      throw Exception(errorMsg);
    }
  }

  // Create Tweet
  Future<Tweet> createTweet(String content, {List<String>? mediaUrls}) async {
    try {
      final response = await _dio.post('/tweets', data: {
        'content': content,
        'media_urls': mediaUrls ?? [],
      });

      if (response.statusCode == 201 || response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        return Tweet.fromJson(data['tweet']);
      } else {
        throw Exception(response.data['error'] ?? '发布推文失败');
      }
    } on DioException catch (e) {
      final errorMsg = e.response?.data?['error'] ?? e.message ?? '网络连接错误';
      throw Exception(errorMsg);
    }
  }

  // Upload Media (Image)
  Future<String> uploadMedia(String filePath) async {
    try {
      final file = File(filePath);
      final filename = filePath.split('/').last;
      
      final formData = FormData.fromMap({
        'file': await MultipartFile.fromFile(
          file.path,
          filename: filename,
        ),
      });

      final response = await _dio.post('/upload', data: formData);

      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        return data['url'] as String;
      } else {
        throw Exception(response.data['error'] ?? '图片上传失败');
      }
    } on DioException catch (e) {
      final errorMsg = e.response?.data?['error'] ?? e.message ?? '图片上传错误';
      throw Exception(errorMsg);
    }
  }

  // Like / Unlike Tweet
  Future<int> likeTweet(String tweetId) async {
    try {
      final response = await _dio.post('/tweets/$tweetId/like');
      if (response.statusCode == 200) {
        return response.data['like_count'] as int? ?? 0;
      }
      throw Exception(response.data['error'] ?? '操作失败');
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络异常');
    }
  }

  Future<int> unlikeTweet(String tweetId) async {
    try {
      final response = await _dio.delete('/tweets/$tweetId/like');
      if (response.statusCode == 200) {
        return response.data['like_count'] as int? ?? 0;
      }
      throw Exception(response.data['error'] ?? '操作失败');
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络异常');
    }
  }

  // Retweet / Unretweet
  Future<Map<String, dynamic>> retweet(String tweetId) async {
    try {
      final response = await _dio.post('/tweets/$tweetId/retweet');
      if (response.statusCode == 200) {
        return {
          'retweet_count': response.data['retweet_count'] as int? ?? 0,
          'is_retweeted': response.data['is_retweeted'] as bool? ?? true,
        };
      }
      throw Exception(response.data['error'] ?? '操作失败');
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络异常');
    }
  }

  Future<Map<String, dynamic>> unretweet(String tweetId) async {
    try {
      final response = await _dio.delete('/tweets/$tweetId/retweet');
      if (response.statusCode == 200) {
        return {
          'retweet_count': response.data['retweet_count'] as int? ?? 0,
          'is_retweeted': response.data['is_retweeted'] as bool? ?? false,
        };
      }
      throw Exception(response.data['error'] ?? '操作失败');
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络异常');
    }
  }

  // Bookmark / Unbookmark
  Future<void> bookmark(String tweetId) async {
    try {
      final response = await _dio.post('/tweets/$tweetId/bookmark');
      if (response.statusCode != 200) {
        throw Exception(response.data['error'] ?? '添加书签失败');
      }
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络异常');
    }
  }

  Future<void> unbookmark(String tweetId) async {
    try {
      final response = await _dio.delete('/tweets/$tweetId/bookmark');
      if (response.statusCode != 200) {
        throw Exception(response.data['error'] ?? '取消书签失败');
      }
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络异常');
    }
  }

  // Fetch single tweet details
  Future<Tweet> getTweet(String tweetId) async {
    try {
      final response = await _dio.get('/tweets/$tweetId');
      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        return Tweet.fromJson(data['tweet']);
      } else {
        throw Exception(response.data['error'] ?? '获取推文失败');
      }
    } on DioException catch (e) {
      final errorMsg = e.response?.data?['error'] ?? e.message ?? '网络连接错误';
      throw Exception(errorMsg);
    }
  }

  // Fetch comments for a tweet
  Future<Map<String, dynamic>> getComments(
    String tweetId, {
    String? cursor,
    int limit = 20,
  }) async {
    try {
      final Map<String, dynamic> queryParameters = {
        'limit': limit,
      };
      if (cursor != null && cursor != '0' && cursor.isNotEmpty) {
        queryParameters['cursor'] = cursor;
      }

      final response = await _dio.get('/tweets/$tweetId/comments', queryParameters: queryParameters);
      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        final commentsRaw = data['comments'] as List? ?? [];
        // Import domain model
        final comments = commentsRaw.map((e) => CommentModel.fromJson(e)).toList();
        final nextCursor = data['next_cursor']?.toString() ?? '0';
        final hasMore = data['has_more'] as bool? ?? false;
        
        return {
          'comments': comments,
          'next_cursor': nextCursor,
          'has_more': hasMore,
        };
      } else {
        throw Exception(response.data['error'] ?? '获取评论失败');
      }
    } on DioException catch (e) {
      final errorMsg = e.response?.data?['error'] ?? e.message ?? '网络连接错误';
      throw Exception(errorMsg);
    }
  }

  // Create comment
  Future<CommentModel> createComment(
    String tweetId,
    String content, {
    String? parentId,
  }) async {
    try {
      final data = {
        'content': content,
      };
      if (parentId != null && parentId.isNotEmpty) {
        data['parent_id'] = parentId;
      }

      final response = await _dio.post('/tweets/$tweetId/comments', data: data);
      if (response.statusCode == 201 || response.statusCode == 200) {
        final respData = response.data as Map<String, dynamic>;
        return CommentModel.fromJson(respData['comment']);
      } else {
        throw Exception(response.data['error'] ?? '发布评论失败');
      }
    } on DioException catch (e) {
      final errorMsg = e.response?.data?['error'] ?? e.message ?? '网络连接错误';
      throw Exception(errorMsg);
    }
  }
}
