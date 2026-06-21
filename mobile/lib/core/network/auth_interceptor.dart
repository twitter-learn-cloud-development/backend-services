import 'package:dio/dio.dart';
import '../utils/storage.dart';

class AuthInterceptor extends Interceptor {
  final void Function()? onUnauthorized;

  AuthInterceptor({this.onUnauthorized});

  @override
  void onRequest(RequestOptions options, RequestInterceptorHandler handler) {
    final token = Storage.getToken();
    if (token != null && token.isNotEmpty) {
      options.headers['Authorization'] = 'Bearer $token';
    }
    super.onRequest(options, handler);
  }

  @override
  void onError(DioException err, ErrorInterceptorHandler handler) {
    if (err.response?.statusCode == 401) {
      // Token is expired or invalid, clear data and notify
      Storage.clearAuthData();
      if (onUnauthorized != null) {
        onUnauthorized!();
      }
    }
    super.onError(err, handler);
  }
}
