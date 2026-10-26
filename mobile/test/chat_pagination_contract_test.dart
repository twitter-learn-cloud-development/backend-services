import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile/features/chat/data/chat_repository.dart';
import 'package:mobile/features/chat/domain/message_model.dart';

class _PageAdapter implements HttpClientAdapter {
  final Map<String, dynamic> body;
  final int status;
  RequestOptions? request;
  _PageAdapter(this.body, {this.status = 200});

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    request = options;
    return ResponseBody.fromString(
      jsonEncode(body),
      status,
      headers: {
        Headers.contentTypeHeader: ['application/json'],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  test('message cursor and uint64 IDs remain strings', () async {
    final adapter = _PageAdapter({
      'messages': [
        {
          'id': '9007199254740997',
          'sender_id': '2',
          'receiver_id': '1',
          'content': 'history',
          'created_at': 1000,
          'is_read': true,
        },
      ],
      'next_cursor': '9007199254740997',
      'has_more': true,
    });
    final dio = Dio(BaseOptions(baseUrl: 'https://fixture.invalid'));
    dio.httpClientAdapter = adapter;
    final page = await ChatRepository(
      dio,
    ).getMessages('2', cursor: '9007199254740999', limit: 1);
    expect(adapter.request!.queryParameters['cursor'], '9007199254740999');
    expect(
      (page['messages'] as List<MessageModel>).single.id,
      '9007199254740997',
    );
    expect(page['next_cursor'], '9007199254740997');
    expect(page['has_more'], isTrue);
    dio.close();
  });

  test('conversation cursor is returned unchanged on next request', () async {
    const cursor = 'c1:1:9007199254740999:9007199254740997';
    final adapter = _PageAdapter({
      'conversations': [],
      'next_cursor': cursor,
      'has_more': true,
    });
    final dio = Dio(BaseOptions(baseUrl: 'https://fixture.invalid'));
    dio.httpClientAdapter = adapter;
    final repository = ChatRepository(dio);
    final first = await repository.getConversations();
    expect(adapter.request!.queryParameters.containsKey('cursor'), isFalse);
    await repository.getConversations(cursor: first['next_cursor'] as String);
    expect(adapter.request!.queryParameters['cursor'], cursor);
    expect(first['has_more'], isTrue);
    dio.close();
  });

  test(
    'empty terminal page and invalid cursor failure stay distinct',
    () async {
      final dio = Dio(BaseOptions(baseUrl: 'https://fixture.invalid'));
      dio.httpClientAdapter = _PageAdapter({
        'messages': [],
        'next_cursor': '',
        'has_more': false,
      });
      final repository = ChatRepository(dio);
      final page = await repository.getMessages('2');
      expect(page['messages'], isEmpty);
      expect(page['next_cursor'], '');
      expect(page['has_more'], isFalse);
      dio.httpClientAdapter = _PageAdapter({
        'error': 'invalid cursor',
      }, status: 400);
      await expectLater(
        repository.getMessages('2', cursor: 'bad'),
        throwsException,
      );
      dio.close();
    },
  );
}
