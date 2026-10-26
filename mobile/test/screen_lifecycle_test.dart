import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile/features/auth/presentation/auth_notifier.dart';
import 'package:mobile/features/tweet/data/tweet_repository.dart';
import 'package:mobile/features/tweet/domain/comment_model.dart';
import 'package:mobile/features/tweet/domain/tweet_model.dart';
import 'package:mobile/features/tweet/presentation/feed_notifier.dart';
import 'package:mobile/features/tweet/presentation/tweet_detail_screen.dart';

class _FixtureAuth extends AuthNotifier {
  @override
  AuthState build() => AuthState();
}

class _PendingTweetRepository extends TweetRepository {
  final Completer<Tweet> pending = Completer<Tweet>();
  _PendingTweetRepository() : super(Dio());

  @override
  Future<Tweet> getTweet(String tweetId) => pending.future;

  @override
  Future<Map<String, dynamic>> getComments(
    String tweetId, {
    String? cursor,
    int limit = 20,
  }) async => {
    'comments': <CommentModel>[],
    'next_cursor': '0',
    'has_more': false,
  };
}

void main() {
  for (final fails in [false, true]) {
    testWidgets(
      'Detached tweet details discard late ${fails ? 'failure' : 'success'}',
      (tester) async {
        final repository = _PendingTweetRepository();
        await tester.pumpWidget(
          ProviderScope(
            overrides: [
              authNotifierProvider.overrideWith(_FixtureAuth.new),
              tweetRepositoryProvider.overrideWithValue(repository),
            ],
            child: const MaterialApp(home: TweetDetailScreen(tweetId: '7')),
          ),
        );
        await tester.pump();
        await tester.pumpWidget(const SizedBox.shrink());

        if (fails) {
          repository.pending.completeError(Exception('fixture unavailable'));
        } else {
          repository.pending.complete(Tweet.fromJson({'id': '7'}));
        }
        await tester.pump();
        expect(tester.takeException(), isNull);
      },
    );
  }
}
