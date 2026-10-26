import 'dart:async';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:mobile/core/network/websocket_service.dart';
import 'package:mobile/core/utils/storage.dart';

class TestSocketSink implements WebSocketSink {
  bool closed = false;
  @override
  Future<void> close([int? closeCode, String? closeReason]) async {
    closed = true;
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class TestSocket implements WebSocketChannel {
  @override
  Future<void> get ready => Future.value();
  final events = StreamController<dynamic>();
  @override
  final TestSocketSink sink = TestSocketSink();
  @override
  Stream<dynamic> get stream => events.stream;
  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

void main() {
  setUpAll(() async {
    SharedPreferences.setMockInitialValues({});
    await Storage.init();
  });
  testWidgets(
    'disconnect cancels scheduled reconnect and disposal prevents new sockets',
    (tester) async {
      await Storage.saveToken('fixture-token');
      final sockets = <TestSocket>[];
      final fixture = Provider<WebSocketService>((ref) {
        final service = WebSocketService(
          ref,
          channelFactory: (_) {
            final socket = TestSocket();
            sockets.add(socket);
            return socket;
          },
        );
        ref.onDispose(service.dispose);
        return service;
      });
      final scope = ProviderContainer();
      final service = scope.read(fixture);
      service.connect();
      expect(sockets, hasLength(1));
      sockets.single.events.close();
      await tester.pump();
      service.disconnect();
      await tester.pump(const Duration(seconds: 10));
      expect(sockets, hasLength(1));
      expect(sockets.single.sink.closed, true);
      scope.dispose();
      service.connect();
      await tester.pump(const Duration(seconds: 10));
      expect(sockets, hasLength(1));
      await Storage.clearAuthData();
    },
  );
}
