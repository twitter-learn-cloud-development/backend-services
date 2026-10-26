import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter/foundation.dart';
import '../../auth/presentation/auth_notifier.dart';
import '../data/tweet_repository.dart';
import '../domain/tweet_model.dart';

// 1. Feed State representation
class FeedState {
  final bool isLoading;
  final bool isFetchingMore;
  final List<Tweet> tweets;
  final String nextCursor;
  final bool hasMore;
  final String? errorMessage;
  final bool continuation;

  FeedState({
    this.isLoading = false,
    this.isFetchingMore = false,
    this.tweets = const [],
    this.nextCursor = '0',
    this.hasMore = false,
    this.errorMessage,
    this.continuation = false,
  });

  FeedState copyWith({
    bool? isLoading,
    bool? isFetchingMore,
    List<Tweet>? tweets,
    String? nextCursor,
    bool? hasMore,
    String? errorMessage,
    bool? continuation,
  }) {
    return FeedState(
      isLoading: isLoading ?? this.isLoading,
      isFetchingMore: isFetchingMore ?? this.isFetchingMore,
      tweets: tweets ?? this.tweets,
      nextCursor: nextCursor ?? this.nextCursor,
      hasMore: hasMore ?? this.hasMore,
      errorMessage: errorMessage,
      continuation: continuation ?? this.continuation,
    );
  }
}

// 2. Repository Provider
final tweetRepositoryProvider = Provider<TweetRepository>((ref) {
  final dio = ref.watch(dioProvider);
  return TweetRepository(dio);
});

// 3. Feed Notifier Provider
class FeedNotifier extends Notifier<FeedState> {
  late TweetRepository _repository;

  @override
  FeedState build() {
    _repository = ref.watch(tweetRepositoryProvider);
    ref.watch(authNotifierProvider.select((auth) => auth.token));
    final generation = ++_generation;
    ref.onDispose(() {
      _generation++;
    });
    Future.microtask(() {
      if (ref.mounted && generation == _generation) refresh();
    });
    return FeedState();
  }

  int _generation = 0;
  bool _retryRefresh = false;

  Future<void> refresh() async {
    final generation = ++_generation;
    _retryRefresh = true;
    state = state.copyWith(
      isLoading: true,
      isFetchingMore: false,
      nextCursor: '0',
      hasMore: true,
      continuation: false,
    );
    await _loadPage(generation, replace: true);
  }

  Future<void> fetchNextPage() async {
    if (state.isLoading || state.isFetchingMore || !state.hasMore) return;
    state = state.copyWith(isFetchingMore: true, continuation: false);
    await _loadPage(_generation, replace: false);
  }

  Future<void> retry() => _retryRefresh ? refresh() : fetchNextPage();

  Future<void> _loadPage(int generation, {required bool replace}) async {
    var replacing = replace;
    try {
      for (var attempt = 0; attempt < 3; attempt++) {
        final requested = state.nextCursor;
        final result = await _repository.getFeeds(cursor: requested, limit: 20);
        if (!ref.mounted || generation != _generation) return;
        final rows = result['tweets'] as List<Tweet>;
        final next = result['next_cursor'] as String;
        final more = result['has_more'] as bool;
        if (more && (next.isEmpty || next == '0' || next == requested)) {
          throw Exception('分页结果无效，请刷新重试');
        }
        final previous = replacing ? <Tweet>[] : state.tweets;
        final ids = previous.map((tweet) => tweet.id).toSet();
        final added = <Tweet>[];
        for (final tweet in rows) {
          if (tweet.id.isEmpty) throw Exception('推文数据无效，请刷新重试');
          if (ids.add(tweet.id)) added.add(tweet);
        }
        state = state.copyWith(
          tweets: [...previous, ...added],
          nextCursor: next,
          hasMore: more,
        );
        _retryRefresh = false;
        replacing = false;
        if (added.isNotEmpty || !more) return;
      }
      state = state.copyWith(continuation: true);
    } catch (_) {
      if (ref.mounted && generation == _generation) {
        state = state.copyWith(errorMessage: '加载失败，请重试');
      }
    } finally {
      if (ref.mounted && generation == _generation) {
        state = state.copyWith(
          isLoading: false,
          isFetchingMore: false,
          errorMessage: state.errorMessage,
        );
      }
    }
  }

  // Optimistic Like / Unlike update
  Future<void> toggleLike(String tweetId) async {
    final generation = _generation;
    final oldTweets = [...state.tweets];
    final index = oldTweets.indexWhere((t) => t.id == tweetId);
    if (index == -1) return;

    final tweet = oldTweets[index];
    final bool newIsLiked = !tweet.isLiked;
    final int newLikeCount = tweet.likeCount + (newIsLiked ? 1 : -1);

    // Apply optimistic update in UI
    oldTweets[index] = tweet.copyWith(
      isLiked: newIsLiked,
      likeCount: newLikeCount,
    );
    state = state.copyWith(tweets: oldTweets);

    try {
      int updatedCount;
      if (newIsLiked) {
        updatedCount = await _repository.likeTweet(tweetId);
      } else {
        updatedCount = await _repository.unlikeTweet(tweetId);
      }

      // Confirm final server count
      if (!ref.mounted || generation != _generation) return;
      final updatedTweets = [...state.tweets];
      final currentIdx = updatedTweets.indexWhere((t) => t.id == tweetId);
      if (currentIdx != -1) {
        updatedTweets[currentIdx] = updatedTweets[currentIdx].copyWith(
          likeCount: updatedCount,
        );
        state = state.copyWith(tweets: updatedTweets);
      }
    } catch (e) {
      if (!ref.mounted || generation != _generation) return;
      if (kDebugMode) {
        print('❌ [FeedNotifier] toggleLike Error: $e');
      }
      // Revert on error
      final revertedTweets = [...state.tweets];
      final currentIdx = revertedTweets.indexWhere((t) => t.id == tweetId);
      if (currentIdx != -1) {
        revertedTweets[currentIdx] = tweet; // original tweet
        state = state.copyWith(
          tweets: revertedTweets,
          errorMessage: e.toString().replaceAll('Exception: ', ''),
        );
      }
    }
  }

  // Optimistic Retweet update
  Future<void> toggleRetweet(String tweetId) async {
    final generation = _generation;
    final oldTweets = [...state.tweets];
    final index = oldTweets.indexWhere((t) => t.id == tweetId);
    if (index == -1) return;

    final tweet = oldTweets[index];
    final bool newIsRetweeted = !tweet.isRetweeted;
    final int newRetweetCount = tweet.retweetCount + (newIsRetweeted ? 1 : -1);

    // Apply optimistic update in UI
    oldTweets[index] = tweet.copyWith(
      isRetweeted: newIsRetweeted,
      retweetCount: newRetweetCount,
    );
    state = state.copyWith(tweets: oldTweets);

    try {
      Map<String, dynamic> result;
      if (newIsRetweeted) {
        result = await _repository.retweet(tweetId);
      } else {
        result = await _repository.unretweet(tweetId);
      }

      // Confirm final server states
      if (!ref.mounted || generation != _generation) return;
      final updatedTweets = [...state.tweets];
      final currentIdx = updatedTweets.indexWhere((t) => t.id == tweetId);
      if (currentIdx != -1) {
        updatedTweets[currentIdx] = updatedTweets[currentIdx].copyWith(
          retweetCount: result['retweet_count'] as int,
          isRetweeted: result['is_retweeted'] as bool,
        );
        state = state.copyWith(tweets: updatedTweets);
      }

      // If we just retweeted, refresh the feed after a short delay so the new tweet appears
      if (newIsRetweeted) {
        Future.delayed(const Duration(milliseconds: 300), () {
          if (ref.mounted && generation == _generation) refresh();
        });
      }
    } catch (e) {
      if (!ref.mounted || generation != _generation) return;
      if (kDebugMode) {
        print('❌ [FeedNotifier] toggleRetweet Error: $e');
      }
      // Revert on error
      final revertedTweets = [...state.tweets];
      final currentIdx = revertedTweets.indexWhere((t) => t.id == tweetId);
      if (currentIdx != -1) {
        revertedTweets[currentIdx] = tweet; // original tweet
        state = state.copyWith(
          tweets: revertedTweets,
          errorMessage: e.toString().replaceAll('Exception: ', ''),
        );
      }
    }
  }

  // Optimistic Bookmark update
  Future<void> toggleBookmark(String tweetId) async {
    final generation = _generation;
    final oldTweets = [...state.tweets];
    final index = oldTweets.indexWhere((t) => t.id == tweetId);
    if (index == -1) return;

    final tweet = oldTweets[index];
    final bool newIsBookmarked = !tweet.isBookmarked;

    // Apply optimistic update in UI
    oldTweets[index] = tweet.copyWith(isBookmarked: newIsBookmarked);
    state = state.copyWith(tweets: oldTweets);

    try {
      if (newIsBookmarked) {
        await _repository.bookmark(tweetId);
      } else {
        await _repository.unbookmark(tweetId);
      }
    } catch (e) {
      if (!ref.mounted || generation != _generation) return;
      if (kDebugMode) {
        print('❌ [FeedNotifier] toggleBookmark Error: $e');
      }
      // Revert on error
      final revertedTweets = [...state.tweets];
      final currentIdx = revertedTweets.indexWhere((t) => t.id == tweetId);
      if (currentIdx != -1) {
        revertedTweets[currentIdx] = tweet; // original tweet
        state = state.copyWith(
          tweets: revertedTweets,
          errorMessage: e.toString().replaceAll('Exception: ', ''),
        );
      }
    }
  }

  // Append a newly created tweet to the top of the feed list
  void insertNewTweet(Tweet newTweet) {
    state = state.copyWith(
      tweets: [
        newTweet,
        ...state.tweets.where((tweet) => tweet.id != newTweet.id),
      ],
    );
  }
}

final feedNotifierProvider = NotifierProvider<FeedNotifier, FeedState>(
  FeedNotifier.new,
);
