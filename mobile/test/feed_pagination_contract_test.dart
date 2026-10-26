import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile/features/tweet/data/tweet_repository.dart';

class _FeedPageAdapter implements HttpClientAdapter {
  Map<String, dynamic> body;
  int status = 200;
  RequestOptions? request;
  _FeedPageAdapter(this.body);

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
  test('empty filtered page preserves exact continuation cursor', () async {
    final adapter = _FeedPageAdapter({
      'tweets': [],
      'next_cursor': '9007199254740997',
      'has_more': true,
    });
    final dio = Dio(BaseOptions(baseUrl: 'https://fixture.invalid'));
    dio.httpClientAdapter = adapter;
    final repository = TweetRepository(dio);
    final first = await repository.getFeeds(cursor: '9007199254740999');
    expect(adapter.request!.queryParameters['cursor'], '9007199254740999');
    expect(first['tweets'], isEmpty);
    expect(first['has_more'], isTrue);
    expect(first['next_cursor'], '9007199254740997');
    adapter.body = {'tweets': [], 'next_cursor': '0', 'has_more': false};
    final last = await repository.getFeeds(
      cursor: first['next_cursor'] as String,
    );
    expect(adapter.request!.queryParameters['cursor'], '9007199254740997');
    expect(last['has_more'], isFalse);
    expect(last['next_cursor'], '0');
    dio.close();
  });

  test('bad cursor response is an error rather than an empty page', () async {
    final adapter = _FeedPageAdapter({'error': 'failed to read feed'});
    adapter.status = 400;
    final dio = Dio(BaseOptions(baseUrl: 'https://fixture.invalid'));
    dio.httpClientAdapter = adapter;
    await expectLater(
      TweetRepository(dio).getFeeds(cursor: 'bad'),
      throwsException,
    );
    dio.close();
  });
}
