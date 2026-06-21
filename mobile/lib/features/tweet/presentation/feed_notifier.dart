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

  FeedState({
    this.isLoading = false,
    this.isFetchingMore = false,
    this.tweets = const [],
    this.nextCursor = '0',
    this.hasMore = false,
    this.errorMessage,
  });

  FeedState copyWith({
    bool? isLoading,
    bool? isFetchingMore,
    List<Tweet>? tweets,
    String? nextCursor,
    bool? hasMore,
    String? errorMessage,
  }) {
    return FeedState(
      isLoading: isLoading ?? this.isLoading,
      isFetchingMore: isFetchingMore ?? this.isFetchingMore,
      tweets: tweets ?? this.tweets,
      nextCursor: nextCursor ?? this.nextCursor,
      hasMore: hasMore ?? this.hasMore,
      errorMessage: errorMessage,
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
  late final TweetRepository _repository;

  @override
  FeedState build() {
    _repository = ref.watch(tweetRepositoryProvider);
    Future.microtask(() => refresh());
    return FeedState();
  }

  // Refresh feeds (clear state and load first page)
  Future<void> refresh() async {
    state = state.copyWith(isLoading: true);
    try {
      final result = await _repository.getFeeds(cursor: '0', limit: 20);
      final tweets = result['tweets'] as List<Tweet>;
      final nextCursor = result['next_cursor'] as String;
      final hasMore = result['has_more'] as bool;
      
      state = FeedState(
        tweets: tweets,
        nextCursor: nextCursor,
        hasMore: hasMore,
      );
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        errorMessage: e.toString().replaceAll('Exception: ', ''),
      );
    }
  }

  // Fetch next page of feeds
  Future<void> fetchNextPage() async {
    if (state.isFetchingMore || !state.hasMore) return;
    
    state = state.copyWith(isFetchingMore: true);
    try {
      final result = await _repository.getFeeds(
        cursor: state.nextCursor,
        limit: 20,
      );
      
      final newTweets = result['tweets'] as List<Tweet>;
      final nextCursor = result['next_cursor'] as String;
      final hasMore = result['has_more'] as bool;
      
      state = state.copyWith(
        isFetchingMore: false,
        tweets: [...state.tweets, ...newTweets],
        nextCursor: nextCursor,
        hasMore: hasMore,
      );
    } catch (e) {
      state = state.copyWith(
        isFetchingMore: false,
        errorMessage: e.toString().replaceAll('Exception: ', ''),
      );
    }
  }

  // Optimistic Like / Unlike update
  Future<void> toggleLike(String tweetId) async {
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
      final updatedTweets = [...state.tweets];
      final currentIdx = updatedTweets.indexWhere((t) => t.id == tweetId);
      if (currentIdx != -1) {
        updatedTweets[currentIdx] = updatedTweets[currentIdx].copyWith(
          likeCount: updatedCount,
        );
        state = state.copyWith(tweets: updatedTweets);
      }
    } catch (e) {
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
      final updatedTweets = [...state.tweets];
      final currentIdx = updatedTweets.indexWhere((t) => t.id == tweetId);
      if (currentIdx != -1) {
        updatedTweets[currentIdx] = updatedTweets[currentIdx].copyWith(
          retweetCount: result['retweet_count'] as int,
          isRetweeted: result['is_retweeted'] as bool,
        );
        state = state.copyWith(tweets: updatedTweets);
      }
    } catch (e) {
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
    final oldTweets = [...state.tweets];
    final index = oldTweets.indexWhere((t) => t.id == tweetId);
    if (index == -1) return;

    final tweet = oldTweets[index];
    final bool newIsBookmarked = !tweet.isBookmarked;

    // Apply optimistic update in UI
    oldTweets[index] = tweet.copyWith(
      isBookmarked: newIsBookmarked,
    );
    state = state.copyWith(tweets: oldTweets);

    try {
      if (newIsBookmarked) {
        await _repository.bookmark(tweetId);
      } else {
        await _repository.unbookmark(tweetId);
      }
    } catch (e) {
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
      tweets: [newTweet, ...state.tweets],
    );
  }
}

final feedNotifierProvider = NotifierProvider<FeedNotifier, FeedState>(FeedNotifier.new);
