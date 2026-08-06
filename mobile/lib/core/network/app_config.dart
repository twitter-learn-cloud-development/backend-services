import 'dart:io' show Platform;
import 'package:flutter/foundation.dart' show kIsWeb;

class AppConfig {
  static String get apiBaseUrl {
    const fromEnv = String.fromEnvironment('API_BASE_URL');
    if (fromEnv.isNotEmpty) {
      return fromEnv;
    }
    return _getDefaultApiUrl();
  }

  static String get mediaBaseUrl {
    const fromEnv = String.fromEnvironment('MEDIA_BASE_URL');
    if (fromEnv.isNotEmpty) {
      return fromEnv;
    }
    return _getDefaultMediaUrl();
  }

  static String _getDefaultApiUrl() {
    if (kIsWeb) {
      return 'http://localhost:9638/api/v1';
    }
    try {
      if (Platform.isAndroid) {
        return 'http://10.103.229.101:9638/api/v1';
      }
    } catch (_) {}
    return 'http://localhost:9638/api/v1';
  }

  static String _getDefaultMediaUrl() {
    if (kIsWeb) {
      return 'http://localhost:9000';
    }
    try {
      if (Platform.isAndroid) {
        return 'http://10.103.229.101:9000';
      }
    } catch (_) {}
    return 'http://localhost:9000';
  }
}
