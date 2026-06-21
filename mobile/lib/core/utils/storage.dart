import 'package:shared_preferences/shared_preferences.dart';

class StorageKeys {
  static const String token = 'auth_token';
  static const String userProfile = 'user_profile';
  static const String themeMode = 'theme_mode';
}

class Storage {
  static late final SharedPreferences _prefs;

  // Initialize SharedPreferences
  static Future<void> init() async {
    _prefs = await SharedPreferences.getInstance();
  }

  // Token
  static Future<bool> saveToken(String token) async {
    return await _prefs.setString(StorageKeys.token, token);
  }

  static String? getToken() {
    return _prefs.getString(StorageKeys.token);
  }

  static Future<bool> removeToken() async {
    return await _prefs.remove(StorageKeys.token);
  }

  // User JSON Profile
  static Future<bool> saveUserProfile(String userJson) async {
    return await _prefs.setString(StorageKeys.userProfile, userJson);
  }

  static String? getUserProfile() {
    return _prefs.getString(StorageKeys.userProfile);
  }

  static Future<bool> removeUserProfile() async {
    return await _prefs.remove(StorageKeys.userProfile);
  }

  // Theme preference (light/dark/system)
  static Future<bool> saveThemeMode(String mode) async {
    return await _prefs.setString(StorageKeys.themeMode, mode);
  }

  static String? getThemeMode() {
    return _prefs.getString(StorageKeys.themeMode);
  }

  // Clear all auth data
  static Future<void> clearAuthData() async {
    await removeToken();
    await removeUserProfile();
  }
}
