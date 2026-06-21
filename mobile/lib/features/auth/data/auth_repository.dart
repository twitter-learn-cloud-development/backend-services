import 'package:dio/dio.dart';
import '../domain/user_model.dart';

class AuthRepository {
  final Dio _dio;

  AuthRepository(this._dio);

  // Login
  Future<Map<String, dynamic>> login({
    required String email,
    required String password,
  }) async {
    try {
      final response = await _dio.post('/auth/login', data: {
        'email': email,
        'password': password,
      });

      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        final token = data['token'] as String;
        final user = User.fromJson(data['user']);
        return {
          'token': token,
          'user': user,
        };
      } else {
        throw Exception(response.data['error'] ?? '登录失败');
      }
    } on DioException catch (e) {
      final errorMsg = e.response?.data?['error'] ?? e.message ?? '网络连接错误';
      throw Exception(errorMsg);
    }
  }

  // Register
  Future<User> register({
    required String username,
    required String email,
    required String password,
  }) async {
    try {
      final response = await _dio.post('/auth/register', data: {
        'username': username,
        'email': email,
        'password': password,
      });

      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        return User.fromJson(data['user']);
      } else {
        throw Exception(response.data['error'] ?? '注册失败');
      }
    } on DioException catch (e) {
      final errorMsg = e.response?.data?['error'] ?? e.message ?? '网络连接错误';
      throw Exception(errorMsg);
    }
  }

  // Get current user profile (me)
  Future<User> getMe() async {
    try {
      final response = await _dio.get('/users/me');
      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        return User.fromJson(data['user']);
      } else {
        throw Exception(response.data['error'] ?? '获取用户信息失败');
      }
    } on DioException catch (e) {
      final errorMsg = e.response?.data?['error'] ?? e.message ?? '网络连接错误';
      throw Exception(errorMsg);
    }
  }
}
