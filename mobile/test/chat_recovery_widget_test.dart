import 'dart:async';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile/core/network/websocket_service.dart';
import 'package:mobile/features/auth/domain/user_model.dart';
import 'package:mobile/features/auth/presentation/auth_notifier.dart';
import 'package:mobile/features/chat/data/chat_repository.dart';
import 'package:mobile/features/chat/domain/message_model.dart';
import 'package:mobile/features/chat/presentation/chat_list_screen.dart';
import 'package:mobile/features/chat/presentation/chat_room_screen.dart';

class FixtureAuth extends AuthNotifier {
  @override
  AuthState build() => AuthState(
    token: 'fixture',
    user: User(id: '1', username: 'self', email: 'fixture@example.invalid'),
  );
}

class FixtureSocket extends WebSocketService {
  FixtureSocket(super.ref);
  final messages = StreamController<Map<String, dynamic>>.broadcast(),
      ready = StreamController<void>.broadcast();
  @override
  Stream<Map<String, dynamic>> get messageStream => messages.stream;
  @override
  Stream<void> get readyStream => ready.stream;
  @override
  void connect() {}
  @override
  void dispose() {
    messages.close();
    ready.close();
  }
}

class FixtureChat extends ChatRepository {
  FixtureChat() : super(Dio());
  final first = Completer<Map<String, dynamic>>();
  int reads = 0;
  MessageModel row(String id) => MessageModel(
    id: id,
    senderId: '1',
    receiverId: '2',
    content: 'same-user-other-device-$id',
    createdAt: 1,
    isRead: false,
  );
  @override
  Future<Map<String, dynamic>> getMessages(
    String peerId, {
    String? cursor,
    int limit = 20,
  }) {
    reads++;
    if (reads == 1) return first.future;
    return Future.value({
      'messages': [row('11'), row('10')],
      'next_cursor': '',
      'has_more': false,
    });
  }

  @override
  Future<MessageModel> sendMessage(String receiverId, String content) async =>
      throw StateError('uncertain commit');
}

void main() {
  testWidgets(
    'ready recovers same-user messages and failed sends retain draft',
    (tester) async {
      final repo = FixtureChat();
      late FixtureSocket socket;
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            authNotifierProvider.overrideWith(FixtureAuth.new),
            chatRepositoryProvider.overrideWithValue(repo),
            websocketServiceProvider.overrideWith((ref) {
              socket = FixtureSocket(ref);
              ref.onDispose(socket.dispose);
              return socket;
            }),
          ],
          child: const MaterialApp(home: ChatRoomScreen(userId: '2')),
        ),
      );
      repo.first.complete({
        'messages': [repo.row('10')],
        'next_cursor': '',
        'has_more': false,
      });
      await tester.pumpAndSettle();
      socket.ready.add(null);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));
      await tester.pumpAndSettle();
      expect(find.text('same-user-other-device-11'), findsOneWidget);
      socket.messages.add({'type': 'message', 'data': repo.row('11').toJson()});
      await tester.pump();
      expect(find.text('same-user-other-device-11'), findsOneWidget);
      await tester.enterText(find.byType(TextField), 'keep this draft');
      await tester.tap(find.byIcon(Icons.send));
      await tester.pumpAndSettle();
      expect(find.text('keep this draft'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets('unmounted chat ignores late history success', (tester) async {
    final repo = FixtureChat();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          authNotifierProvider.overrideWith(FixtureAuth.new),
          chatRepositoryProvider.overrideWithValue(repo),
          websocketServiceProvider.overrideWith((ref) {
            final s = FixtureSocket(ref);
            ref.onDispose(s.dispose);
            return s;
          }),
        ],
        child: const MaterialApp(home: ChatRoomScreen(userId: '2')),
      ),
    );
    await tester.pumpWidget(const SizedBox());
    repo.first.complete({
      'messages': [repo.row('10')],
      'next_cursor': '',
      'has_more': false,
    });
    await tester.pump();
    expect(tester.takeException(), isNull);
  });
}
