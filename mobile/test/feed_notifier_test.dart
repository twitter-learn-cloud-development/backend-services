import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/material.dart';
import 'package:mobile/features/auth/presentation/auth_notifier.dart';
import 'package:mobile/features/tweet/data/tweet_repository.dart';
import 'package:mobile/features/tweet/domain/tweet_model.dart';
import 'package:mobile/features/tweet/presentation/feed_notifier.dart';
import 'package:mobile/features/tweet/presentation/home_screen.dart';

class _FixtureAuth extends AuthNotifier {
  @override
  AuthState build() => AuthState(token: 'fixture');
  void switchIdentity() {
    state = AuthState(token: 'changed');
  }
}

class _FixtureRepository extends TweetRepository {
  final Future<Map<String, dynamic>> Function(String cursor) fetch;
  _FixtureRepository(this.fetch) : super(Dio());
  @override
  Future<Map<String, dynamic>> getFeeds({String? cursor, int limit = 20}) =>
      fetch(cursor ?? '0');
}

class _WidgetFeed extends FeedNotifier {
  final FeedState initial;
  int nextCalls = 0;
  int retryCalls = 0;
  _WidgetFeed(this.initial);
  @override
  FeedState build() => initial;
  @override
  Future<void> fetchNextPage() async {
    nextCalls++;
    state = FeedState();
  }

  @override
  Future<void> retry() async {
    retryCalls++;
    state = FeedState();
  }
}

Map<String, dynamic> _page(
  List<String> ids, {
  String cursor = '0',
  bool more = false,
}) => {
  'tweets': ids
      .map(
        (id) => Tweet(
          id: id,
          userId: '2',
          content: id,
          mediaUrls: [],
          createdAt: 1,
          updatedAt: 1,
        ),
      )
      .toList(),
  'next_cursor': cursor,
  'has_more': more,
};

ProviderContainer _container(
  Future<Map<String, dynamic>> Function(String) fetch,
) {
  final container = ProviderContainer(
    overrides: [
      authNotifierProvider.overrideWith(_FixtureAuth.new),
      tweetRepositoryProvider.overrideWithValue(_FixtureRepository(fetch)),
    ],
  );
  addTearDown(container.dispose);
  container.read(feedNotifierProvider);
  return container;
}

Future<void> _settle() async {
  for (var i = 0; i < 8; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

void main() {
  testWidgets(
    'empty continuation renders an explicit button and terminal state',
    (tester) async {
      final feed = _WidgetFeed(FeedState(hasMore: true, continuation: true));
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            authNotifierProvider.overrideWith(_FixtureAuth.new),
            feedNotifierProvider.overrideWith(() => feed),
          ],
          child: const MaterialApp(home: HomeScreen()),
        ),
      );
      expect(find.text('继续加载'), findsOneWidget);
      expect(find.text('你的时间线上还没有推文，去关注一些人吧！'), findsNothing);
      await tester.tap(find.text('继续加载'));
      await tester.pump();
      expect(feed.nextCalls, 1);
      expect(find.text('你的时间线上还没有推文，去关注一些人吧！'), findsOneWidget);
    },
  );

  testWidgets('empty failure has retry instead of an endless spinner', (
    tester,
  ) async {
    final feed = _WidgetFeed(FeedState(errorMessage: '加载失败，请重试'));
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          authNotifierProvider.overrideWith(_FixtureAuth.new),
          feedNotifierProvider.overrideWith(() => feed),
        ],
        child: const MaterialApp(home: HomeScreen()),
      ),
    );
    expect(find.text('重试'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);
    await tester.tap(find.text('重试'));
    await tester.pump();
    expect(feed.retryCalls, 1);
  });

  test('continuous pages deduplicate and keep large string cursor', () async {
    final cursors = <String>[];
    var calls = 0;
    final container = _container((cursor) async {
      cursors.add(cursor);
      return ++calls == 1
          ? _page(
              ['9007199254740999', '9007199254740998'],
              cursor: '9007199254740998',
              more: true,
            )
          : _page(['9007199254740998', '9007199254740997']);
    });
    await _settle();
    await container.read(feedNotifierProvider.notifier).fetchNextPage();
    expect(cursors, ['0', '9007199254740998']);
    expect(
      container.read(feedNotifierProvider).tweets.map((tweet) => tweet.id),
      ['9007199254740999', '9007199254740998', '9007199254740997'],
    );
    expect(container.read(feedNotifierProvider).hasMore, isFalse);
  });

  test(
    'empty continuation pages are bounded and resume at last cursor',
    () async {
      var calls = 0;
      final container = _container(
        (_) async => _page([], cursor: '${100 - calls++}', more: true),
      );
      await _settle();
      expect(calls, 3);
      expect(container.read(feedNotifierProvider).continuation, isTrue);
      expect(container.read(feedNotifierProvider).nextCursor, '98');
      await container.read(feedNotifierProvider.notifier).fetchNextPage();
      expect(calls, 6);
      expect(container.read(feedNotifierProvider).nextCursor, '95');
    },
  );

  test(
    'refresh supersedes pending next page and releases only its loading state',
    () async {
      var calls = 0;
      final pending = Completer<Map<String, dynamic>>();
      final container = _container((_) async {
        calls++;
        if (calls == 1) return _page(['5'], cursor: '5', more: true);
        if (calls == 2) return pending.future;
        return _page(['9']);
      });
      await _settle();
      final notifier = container.read(feedNotifierProvider.notifier);
      final old = notifier.fetchNextPage();
      await notifier.fetchNextPage();
      expect(calls, 2);
      await notifier.refresh();
      pending.complete(_page(['4']));
      await old;
      expect(container.read(feedNotifierProvider).tweets.single.id, '9');
      expect(container.read(feedNotifierProvider).isFetchingMore, isFalse);
    },
  );

  test(
    'failure retains cursor and items and retries exact failed page',
    () async {
      final cursors = <String>[];
      var calls = 0;
      final container = _container((cursor) async {
        cursors.add(cursor);
        if (++calls == 2) throw Exception('private details');
        return calls == 1
            ? _page(['5'], cursor: '5', more: true)
            : _page(['4']);
      });
      await _settle();
      final notifier = container.read(feedNotifierProvider.notifier);
      await notifier.fetchNextPage();
      expect(container.read(feedNotifierProvider).nextCursor, '5');
      expect(container.read(feedNotifierProvider).tweets.single.id, '5');
      expect(container.read(feedNotifierProvider).errorMessage, '加载失败，请重试');
      await notifier.retry();
      expect(cursors, ['0', '5', '5']);
    },
  );

  test(
    'account change resets items and rejects old account response',
    () async {
      final old = Completer<Map<String, dynamic>>();
      var calls = 0;
      final container = _container(
        (_) async => ++calls == 1 ? old.future : _page(['new-account']),
      );
      await _settle();
      (container.read(authNotifierProvider.notifier) as _FixtureAuth)
          .switchIdentity();
      container.read(feedNotifierProvider);
      await _settle();
      old.complete(_page(['old-account']));
      await _settle();
      expect(
        container.read(feedNotifierProvider).tweets.single.id,
        'new-account',
      );
    },
  );

  test(
    'non-advancing cursor is a retryable error; local insert deduplicates',
    () async {
      var calls = 0;
      final container = _container((_) async {
        calls++;
        return _page([], cursor: '0', more: true);
      });
      await _settle();
      expect(calls, 1);
      expect(container.read(feedNotifierProvider).errorMessage, isNotNull);
      final tweet = (_page(['5'])['tweets'] as List<Tweet>).single;
      final notifier = container.read(feedNotifierProvider.notifier);
      notifier.insertNewTweet(tweet);
      notifier.insertNewTweet(tweet);
      expect(container.read(feedNotifierProvider).tweets, hasLength(1));
    },
  );
}
