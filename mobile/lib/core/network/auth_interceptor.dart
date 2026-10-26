import 'package:dio/dio.dart';
import '../utils/storage.dart';

class AuthInterceptor extends Interceptor {
  final void Function()? onUnauthorized;
  final String? Function() sessionToken;
  final int Function() sessionRevision;

  AuthInterceptor({
    this.onUnauthorized,
    String? Function()? sessionToken,
    int Function()? sessionRevision,
  }) : sessionToken = sessionToken ?? Storage.getToken,
       sessionRevision = sessionRevision ?? (() => 0);

  @override
  void onRequest(RequestOptions options, RequestInterceptorHandler handler) {
    final token = sessionToken();
    options.extra['authRevision'] = sessionRevision();
    if (token != null &&
        token.isNotEmpty &&
        !['/auth/login', '/auth/register'].contains(options.path)) {
      options.headers['Authorization'] = 'Bearer $token';
    }
    super.onRequest(options, handler);
  }

  @override
  void onError(DioException err, ErrorInterceptorHandler handler) {
    final token = sessionToken();
    if (err.response?.statusCode == 401 &&
        token != null &&
        token.isNotEmpty &&
        err.requestOptions.extra['authRevision'] == sessionRevision() &&
        err.requestOptions.headers['Authorization'] == 'Bearer $token') {
      if (onUnauthorized != null) {
        onUnauthorized!();
      } else {
        Storage.clearAuthData();
      }
    }
    super.onError(err, handler);
  }
}
