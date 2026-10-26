import 'dart:async';
import 'dart:convert';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:mobile/core/router/app_router.dart';
import 'package:mobile/core/utils/storage.dart';
import 'package:mobile/features/auth/data/auth_repository.dart';
import 'package:mobile/features/auth/domain/user_model.dart';
import 'package:mobile/features/auth/presentation/auth_notifier.dart';
import 'package:mobile/main.dart';

class AccountRepository extends AuthRepository {
  AccountRepository() : super(Dio());
  final refresh = Completer<User>();
  final changed = Completer<void>();
  @override
  Future<User> getMe() => refresh.future;
  @override
  Future<void> changePassword(String oldPassword, String newPassword) =>
      changed.future;
}

void main() {
  setUpAll(() async {
    SharedPreferences.setMockInitialValues({});
    await Storage.init();
  });
  testWidgets(
    'password form validates and successful change returns to login',
    (tester) async {
      final user = User(
        id: '42',
        username: 'alice',
        email: 'alice@example.com',
      );
      await Storage.saveToken('cached-jwt');
      await Storage.saveUserProfile(jsonEncode(user.toJson()));
      final repo = AccountRepository();
      final scope = ProviderContainer(
        overrides: [authRepositoryProvider.overrideWithValue(repo)],
      );
      addTearDown(scope.dispose);
      final router = scope.read(appRouterProvider);
      router.go('/account/security');
      await tester.pumpWidget(
        UncontrolledProviderScope(container: scope, child: const MyApp()),
      );
      await tester.pumpAndSettle();
      expect(find.text('账号安全'), findsOneWidget);
      await tester.tap(find.text('修改密码并重新登录'));
      await tester.pumpAndSettle();
      expect(find.text('请输入当前密码'), findsOneWidget);
      final fields = find.byType(TextFormField);
      await tester.enterText(fields.at(0), 'old-secret');
      await tester.enterText(fields.at(1), 'new-secret');
      await tester.enterText(fields.at(2), 'new-secret');
      await tester.tap(find.text('修改密码并重新登录'));
      await tester.pump();
      repo.changed.complete();
      await tester.pumpAndSettle();
      expect(find.text('登录 Twitter Clone'), findsOneWidget);
      expect(scope.read(authNotifierProvider).isAuthenticated, false);
      expect(Storage.getToken(), isNull);
      expect(Storage.getUserProfile(), isNull);
      expect(tester.takeException(), isNull);
    },
  );
}
