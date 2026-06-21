import 'dart:convert';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:dio/dio.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/utils/storage.dart';
import '../data/auth_repository.dart';
import '../domain/user_model.dart';

// 1. Auth State representation
class AuthState {
  final bool isLoading;
  final User? user;
  final String? token;
  final String? errorMessage;

  AuthState({
    this.isLoading = false,
    this.user,
    this.token,
    this.errorMessage,
  });

  bool get isAuthenticated => token != null && token!.isNotEmpty && user != null;

  AuthState copyWith({
    bool? isLoading,
    User? user,
    String? token,
    String? errorMessage,
  }) {
    return AuthState(
      isLoading: isLoading ?? this.isLoading,
      user: user ?? this.user,
      token: token ?? this.token,
      errorMessage: errorMessage, // We reset error unless provided
    );
  }
}

// 2. Dio provider
final Provider<Dio> dioProvider = Provider<Dio>((ref) {
  // Pass a callback to trigger logout when a 401 occurs
  final client = DioClient(onUnauthorized: () {
    ref.read(authNotifierProvider.notifier).logout();
  });
  return client.dio;
});

// 3. Auth Repository provider
final Provider<AuthRepository> authRepositoryProvider = Provider<AuthRepository>((ref) {
  final dio = ref.watch(dioProvider);
  return AuthRepository(dio);
});

// 4. Auth Notifier provider using Notifier
class AuthNotifier extends Notifier<AuthState> {
  late final AuthRepository _repository;

  @override
  AuthState build() {
    _repository = ref.watch(authRepositoryProvider);
    
    // We bootstrap authentication state asynchronously after initialization
    Future.microtask(() => _bootstrapAuth());
    
    return AuthState();
  }

  // Load persisted session on app startup
  void _bootstrapAuth() {
    final token = Storage.getToken();
    final userJson = Storage.getUserProfile();
    
    if (token != null && token.isNotEmpty && userJson != null) {
      try {
        final user = User.fromJson(jsonDecode(userJson));
        state = AuthState(token: token, user: user);
        
        // Asynchronously check if token is still valid by fetching fresh profile
        _refreshUserProfile();
      } catch (_) {
        logout();
      }
    }
  }

  Future<void> _refreshUserProfile() async {
    try {
      final freshUser = await _repository.getMe();
      state = state.copyWith(user: freshUser);
      await Storage.saveUserProfile(jsonEncode(freshUser.toJson()));
    } catch (_) {
      // If refresh fails, we keep the cached profile unless it is a 401 error (handled by Interceptor)
    }
  }

  // Login action
  Future<void> login(String email, String password) async {
    state = state.copyWith(isLoading: true);
    try {
      final result = await _repository.login(email: email, password: password);
      final token = result['token'] as String;
      final user = result['user'] as User;

      // Persist in local storage
      await Storage.saveToken(token);
      await Storage.saveUserProfile(jsonEncode(user.toJson()));

      state = AuthState(token: token, user: user);
    } catch (e) {
      state = AuthState(errorMessage: e.toString().replaceAll('Exception: ', ''));
    }
  }

  // Register action
  Future<bool> register(String username, String email, String password) async {
    state = state.copyWith(isLoading: true);
    try {
      await _repository.register(username: username, email: email, password: password);
      state = AuthState(); // Reset state, user should now login
      return true;
    } catch (e) {
      state = AuthState(errorMessage: e.toString().replaceAll('Exception: ', ''));
      return false;
    }
  }

  // Logout action
  void logout() {
    Storage.clearAuthData();
    state = AuthState();
  }
}

final NotifierProvider<AuthNotifier, AuthState> authNotifierProvider =
    NotifierProvider<AuthNotifier, AuthState>(AuthNotifier.new);
