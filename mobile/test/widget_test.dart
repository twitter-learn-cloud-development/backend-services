import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:mobile/core/utils/storage.dart';
import 'package:mobile/features/auth/presentation/auth_notifier.dart';
import 'package:mobile/main.dart';

void main() {
  setUpAll(() async {
    SharedPreferences.setMockInitialValues({StorageKeys.themeMode: 'light'});
    await Storage.init();
  });

  testWidgets(
    'Unauthenticated boot shows login and registration without API calls',
    (WidgetTester tester) async {
      var requests = 0;
      final dio = Dio();
      dio.interceptors.add(
        InterceptorsWrapper(
          onRequest: (options, handler) {
            requests++;
            handler.reject(
              DioException(
                requestOptions: options,
                error: 'Network is forbidden in this fixture',
              ),
            );
          },
        ),
      );
      addTearDown(() => dio.close(force: true));

      await tester.pumpWidget(
        ProviderScope(
          overrides: [dioProvider.overrideWithValue(dio)],
          child: const MyApp(),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('登录 Twitter Clone'), findsOneWidget);

      await tester.tap(find.text('登 录'));
      await tester.pumpAndSettle();
      expect(find.text('请输入邮箱'), findsOneWidget);
      expect(find.text('请输入密码'), findsOneWidget);

      await tester.ensureVisible(find.text('立即注册'));
      await tester.tap(find.text('立即注册'));
      await tester.pumpAndSettle();
      expect(find.text('创建您的账号'), findsOneWidget);
      expect(requests, 0);
      expect(tester.takeException(), isNull);
    },
  );
}
