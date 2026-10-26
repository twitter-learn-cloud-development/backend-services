import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:mobile/core/network/auth_interceptor.dart';
import 'package:mobile/core/utils/storage.dart';
import 'package:mobile/features/auth/data/auth_repository.dart';
import 'package:mobile/features/auth/domain/user_model.dart';
import 'package:mobile/features/auth/presentation/auth_notifier.dart';

final alice = User(
  id: '9007199254740999',
  username: 'alice',
  email: 'alice@example.com',
);
Map<String, dynamic> session(String token) => {'token': token, 'user': alice};
Future<void> settle() async {
  for (var i = 0; i < 8; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

class FakeAuthRepository extends AuthRepository {
  FakeAuthRepository() : super(Dio());
  final logins = <Completer<Map<String, dynamic>>>[];
  final profile = Completer<User>();
  bool rejectPassword = false;
  @override
  Future<Map<String, dynamic>> login({
    required String email,
    required String password,
  }) {
    final pending = Completer<Map<String, dynamic>>();
    logins.add(pending);
    return pending.future;
  }

  @override
  Future<User> getMe() => profile.future;
  @override
  Future<void> changePassword(String oldPassword, String newPassword) async {
    if (rejectPassword) throw Exception('incorrect current password');
  }
}

class ControlledAdapter implements HttpClientAdapter {
  final pending = <Completer<ResponseBody>>[];
  final requests = <RequestOptions>[];
  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? body,
    Future<void>? cancelFuture,
  ) {
    requests.add(options);
    final next = Completer<ResponseBody>();
    pending.add(next);
    return next.future;
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  setUpAll(() async {
    SharedPreferences.setMockInitialValues({});
    await Storage.init();
  });
  setUp(() async {
    await Storage.clearAuthData();
  });

  ProviderContainer container(FakeAuthRepository repo) {
    final value = ProviderContainer(
      overrides: [authRepositoryProvider.overrideWithValue(repo)],
    );
    addTearDown(value.dispose);
    value.read(authNotifierProvider);
    return value;
  }

  test(
    'password repository handles non-JSON errors during rolling upgrades',
    () async {
      final dio = Dio(BaseOptions(baseUrl: 'https://fixture.invalid'));
      final adapter = ControlledAdapter();
      dio.httpClientAdapter = adapter;
      addTearDown(() => dio.close(force: true));
      final pending = AuthRepository(
        dio,
      ).changePassword('old-secret', 'new-secret');
      final expectation = expectLater(
        pending,
        throwsA(
          isA<Exception>().having(
            (e) => e.toString(),
            'message',
            contains('请求结果可能未确定'),
          ),
        ),
      );
      await settle();
      adapter.pending.single.complete(
        ResponseBody.fromString('route not found', 404),
      );
      await expectation;
    },
  );

  test(
    'logout discards late login and clears persisted identity, preserving theme',
    () async {
      await Storage.saveThemeMode('dark');
      final repo = FakeAuthRepository(), scope = container(repo);
      final notifier = scope.read(authNotifierProvider.notifier);
      final pending = notifier.login('alice@example.com', 'password');
      await notifier.logout();
      repo.logins.single.complete(session('old'));
      await pending;
      expect(scope.read(authNotifierProvider).isAuthenticated, false);
      expect(Storage.getToken(), isNull);
      expect(Storage.getUserProfile(), isNull);
      expect(Storage.getThemeMode(), 'dark');
    },
  );

  test(
    'latest login wins and old completion cannot overwrite persisted credentials',
    () async {
      final repo = FakeAuthRepository(),
          scope = container(repo),
          notifier = scope.read(authNotifierProvider.notifier);
      final first = notifier.login('one@example.com', 'password');
      final next = notifier.login('two@example.com', 'password');
      repo.logins[1].complete(session('new'));
      await next;
      repo.logins[0].complete(session('old'));
      await first;
      expect(scope.read(authNotifierProvider).token, 'new');
      expect(Storage.getToken(), 'new');
    },
  );

  test('new pending login cannot skip a queued logout cleanup', () async {
    final repo = FakeAuthRepository(),
        scope = container(repo),
        notifier = scope.read(authNotifierProvider.notifier);
    final first = notifier.login('alice@example.com', 'password');
    repo.logins[0].complete(session('old'));
    await first;
    final clearing = notifier.logout();
    final next = notifier.login('alice@example.com', 'password');
    await clearing;
    expect(Storage.getToken(), isNull);
    expect(Storage.getUserProfile(), isNull);
    repo.logins[1].complete(session('new'));
    await next;
    expect(Storage.getToken(), 'new');
  });

  test('late profile refresh cannot repopulate a logged-out profile', () async {
    await Storage.saveToken('cached');
    await Storage.saveUserProfile(jsonEncode(alice.toJson()));
    final repo = FakeAuthRepository(), scope = container(repo);
    expect(scope.read(authNotifierProvider).isAuthenticated, true);
    await settle();
    await scope.read(authNotifierProvider.notifier).logout();
    repo.profile.complete(alice);
    await settle();
    expect(Storage.getToken(), isNull);
    expect(Storage.getUserProfile(), isNull);
    expect(scope.read(authNotifierProvider).user, isNull);
  });

  test(
    'password failure retains session; success requires sign-in and clears persistence',
    () async {
      final repo = FakeAuthRepository(),
          scope = container(repo),
          notifier = scope.read(authNotifierProvider.notifier);
      final login = notifier.login('alice@example.com', 'password');
      repo.logins.single.complete(session('jwt'));
      await login;
      repo.rejectPassword = true;
      expect(await notifier.changePassword('wrong', 'new-secret'), false);
      expect(scope.read(authNotifierProvider).isAuthenticated, true);
      repo.rejectPassword = false;
      expect(await notifier.changePassword('old-secret', 'new-secret'), true);
      expect(scope.read(authNotifierProvider).isAuthenticated, false);
      expect(Storage.getToken(), isNull);
    },
  );

  test(
    '401 is scoped to request epoch and bearer, including identical JWT strings',
    () async {
      var epoch = 1, expired = 0;
      const token = 'same-jwt';
      final dio = Dio(BaseOptions(baseUrl: 'https://fixture.invalid'));
      final adapter = ControlledAdapter();
      dio.httpClientAdapter = adapter;
      dio.interceptors.add(
        AuthInterceptor(
          sessionToken: () => token,
          sessionRevision: () => epoch,
          onUnauthorized: () {
            expired++;
          },
        ),
      );
      addTearDown(() => dio.close(force: true));
      Future<void> request(String path) async {
        try {
          await dio.get(path);
        } on DioException catch (_) {}
      }

      final old = request('/users/me');
      await settle();
      epoch++;
      adapter.pending[0].complete(ResponseBody.fromString('{}', 401));
      await old;
      expect(expired, 0);
      final current = request('/users/me');
      await settle();
      adapter.pending[1].complete(ResponseBody.fromString('{}', 401));
      await current;
      expect(expired, 1);
      final login = request('/auth/login');
      await settle();
      expect(adapter.requests.last.headers.containsKey('Authorization'), false);
      adapter.pending[2].complete(ResponseBody.fromString('{}', 401));
      await login;
      expect(expired, 1);
    },
  );
}
