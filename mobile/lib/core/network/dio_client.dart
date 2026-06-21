import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'auth_interceptor.dart';
import 'app_config.dart';

class DioClient {
  late final Dio _dio;

  DioClient({void Function()? onUnauthorized}) {
    _dio = Dio(
      BaseOptions(
        baseUrl: AppConfig.apiBaseUrl,
        connectTimeout: const Duration(seconds: 15),
        receiveTimeout: const Duration(seconds: 15),
        responseType: ResponseType.json,
      ),
    );
    
    // Add interceptors
    _dio.interceptors.add(AuthInterceptor(onUnauthorized: onUnauthorized));
    
    if (kDebugMode) {
      _dio.interceptors.add(LogInterceptor(
        requestHeader: true,
        requestBody: true,
        responseBody: true,
        responseHeader: false,
        error: true,
      ));
    }
  }

  Dio get dio => _dio;

  // Prefix relative media paths with MEDIA_BASE_URL
  static String getMediaUrl(String rawUrl) {
    if (rawUrl.isEmpty) return '';
    
    // If it is a relative path starting with '/', append mediaBaseUrl prefix
    if (rawUrl.startsWith('/')) {
      return '${AppConfig.mediaBaseUrl}$rawUrl';
    }
    
    // Otherwise, return as is (absolute URL)
    return rawUrl;
  }
}
