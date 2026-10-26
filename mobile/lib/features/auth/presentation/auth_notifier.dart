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

  AuthState({this.isLoading = false, this.user, this.token, this.errorMessage});

  bool get isAuthenticated =>
      token != null && token!.isNotEmpty && user != null;

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
  final client = DioClient(
    onUnauthorized: () {
      ref.read(authNotifierProvider.notifier).logout();
    },
    sessionToken: () => ref.read(authNotifierProvider).token,
    sessionRevision: () =>
        ref.read(authNotifierProvider.notifier).sessionRevision,
  );
  ref.onDispose(() => client.dio.close(force: true));
  return client.dio;
});

// 3. Auth Repository provider
final Provider<AuthRepository> authRepositoryProvider =
    Provider<AuthRepository>((ref) {
      final dio = ref.watch(dioProvider);
      return AuthRepository(dio);
    });

// 4. Auth Notifier provider using Notifier
class AuthNotifier extends Notifier<AuthState> {
  late AuthRepository _repository;
  int _epoch = 0;
  bool _disposed = false;
  Future<void> _writes = Future.value();
  int get sessionRevision => _epoch;
  bool _current(int epoch) => !_disposed && epoch == _epoch;

  Future<void> _persist(int epoch, String? token, User? user) {
    final operation = _writes.catchError((Object _) {}).then((_) async {
      // Queued logout clears the old identity even if another sign-in starts.
      // Any later valid commit follows this cleanup in the same write queue.
      if (token != null && !_current(epoch)) return;
      if (token == null || user == null) {
        await Storage.clearAuthData();
      } else {
        if (!await Storage.saveToken(token) ||
            !await Storage.saveUserProfile(jsonEncode(user.toJson()))) {
          throw StateError('无法保存登录状态');
        }
      }
    });
    _writes = operation;
    return operation;
  }

  @override
  AuthState build() {
    _repository = ref.watch(authRepositoryProvider);
    _disposed = false;
    final epoch = ++_epoch;
    ref.onDispose(() {
      _disposed = true;
      ++_epoch;
    });
    final cached = _bootstrapAuth();
    if (cached.isAuthenticated) {
      Future.microtask(() {
        if (_current(epoch)) _refreshUserProfile(epoch);
      });
    }
    return cached;
  }

  // Load persisted session on app startup
  AuthState _bootstrapAuth() {
    final token = Storage.getToken();
    final userJson = Storage.getUserProfile();

    if (token != null && token.isNotEmpty && userJson != null) {
      try {
        final user = User.fromJson(jsonDecode(userJson));
        if (user.id.isEmpty) throw const FormatException('missing user id');
        return AuthState(token: token, user: user);
      } catch (_) {
        // Invalid persisted profiles must never authorize navigation.
      }
    }
    final epoch = _epoch;
    _persist(epoch, null, null).catchError((Object _) {
      if (_current(epoch)) state = AuthState(errorMessage: '无法清除本机登录信息，请重试');
    });
    return AuthState();
  }

  Future<void> _refreshUserProfile(int epoch) async {
    try {
      final freshUser = await _repository.getMe();
      if (!_current(epoch)) return;
      state = state.copyWith(user: freshUser);
      await _persist(epoch, state.token, freshUser);
    } catch (_) {
      // If refresh fails, we keep the cached profile unless it is a 401 error (handled by Interceptor)
    }
  }

  // Login action
  Future<void> login(String email, String password) async {
    final epoch = ++_epoch;
    state = AuthState(isLoading: true);
    try {
      final result = await _repository.login(email: email, password: password);
      final token = result['token'] as String;
      final user = result['user'] as User;
      if (!_current(epoch)) return;
      if (token.isEmpty || user.id.isEmpty) throw StateError('登录响应不完整');

      // Persist in local storage
      await _persist(epoch, token, user);
      if (!_current(epoch)) return;

      state = AuthState(token: token, user: user);
    } catch (e) {
      if (!_current(epoch)) return;
      try {
        await _persist(epoch, null, null);
      } catch (_) {
        /* Remain signed out. */
      }
      if (!_current(epoch)) return;
      state = AuthState(
        errorMessage: e.toString().replaceAll('Exception: ', ''),
      );
    }
  }

  // Register action
  Future<bool> register(String username, String email, String password) async {
    final epoch = ++_epoch;
    state = state.copyWith(isLoading: true);
    try {
      await _repository.register(
        username: username,
        email: email,
        password: password,
      );
      if (!_current(epoch)) return false;
      state = AuthState(); // Reset state, user should now login
      return true;
    } catch (e) {
      if (!_current(epoch)) return false;
      state = AuthState(
        errorMessage: e.toString().replaceAll('Exception: ', ''),
      );
      return false;
    }
  }

  // Logout action
  Future<void> logout() async {
    final epoch = ++_epoch;
    state = AuthState();
    try {
      await _persist(epoch, null, null);
    } catch (_) {
      if (_current(epoch)) state = AuthState(errorMessage: '无法清除本机登录信息，请重试');
    }
  }

  Future<bool> changePassword(String oldPassword, String newPassword) async {
    if (!state.isAuthenticated || state.isLoading) return false;
    final epoch = _epoch;
    state = state.copyWith(isLoading: true);
    try {
      await _repository.changePassword(oldPassword, newPassword);
      if (!_current(epoch)) return false;
      await logout();
      return true;
    } catch (e) {
      if (_current(epoch)) {
        state = state.copyWith(
          isLoading: false,
          errorMessage: e.toString().replaceAll('Exception: ', ''),
        );
      }
      return false;
    }
  }
}

final NotifierProvider<AuthNotifier, AuthState> authNotifierProvider =
    NotifierProvider<AuthNotifier, AuthState>(AuthNotifier.new);
