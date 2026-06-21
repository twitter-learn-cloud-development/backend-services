import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../auth/presentation/auth_notifier.dart';
import '../../tweet/domain/tweet_model.dart';
import '../data/profile_repository.dart';
import '../../auth/domain/user_model.dart';

// 1. Profile State
class ProfileState {
  final bool isLoading;
  final User? user;
  final int followerCount;
  final int followingCount;
  final bool isFollowing;
  final List<Tweet> tweets;
  final List<Tweet> likedTweets;
  final List<Tweet> mediaTweets;
  final String? errorMessage;

  ProfileState({
    this.isLoading = false,
    this.user,
    this.followerCount = 0,
    this.followingCount = 0,
    this.isFollowing = false,
    this.tweets = const [],
    this.likedTweets = const [],
    this.mediaTweets = const [],
    this.errorMessage,
  });

  ProfileState copyWith({
    bool? isLoading,
    User? user,
    int? followerCount,
    int? followingCount,
    bool? isFollowing,
    List<Tweet>? tweets,
    List<Tweet>? likedTweets,
    List<Tweet>? mediaTweets,
    String? errorMessage,
  }) {
    return ProfileState(
      isLoading: isLoading ?? this.isLoading,
      user: user ?? this.user,
      followerCount: followerCount ?? this.followerCount,
      followingCount: followingCount ?? this.followingCount,
      isFollowing: isFollowing ?? this.isFollowing,
      tweets: tweets ?? this.tweets,
      likedTweets: likedTweets ?? this.likedTweets,
      mediaTweets: mediaTweets ?? this.mediaTweets,
      errorMessage: errorMessage,
    );
  }
}

// 2. Repository Provider
final profileRepositoryProvider = Provider<ProfileRepository>((ref) {
  final dio = ref.watch(dioProvider);
  return ProfileRepository(dio);
});

// 3. Profile Notifier Family Provider (needs userId parameter)
// 3. Profile Notifier Family Provider (needs userId parameter)
class ProfileNotifier extends Notifier<ProfileState> {
  final String userId;
  ProfileNotifier(this.userId);

  late final ProfileRepository _repository;

  @override
  ProfileState build() {
    _repository = ref.watch(profileRepositoryProvider);
    Future.microtask(() => loadProfile());
    return ProfileState();
  }

  // Load user profile details and status
  Future<void> loadProfile() async {
    state = state.copyWith(isLoading: true);
    try {
      // 1. Fetch full profile via BFF
      final fullProfile = await _repository.getFullProfile(userId);
      final user = fullProfile['user'] as User;
      final follower = fullProfile['follower_count'] as int;
      final following = fullProfile['following_count'] as int;
      final recentTweets = fullProfile['recent_tweets'] as List<Tweet>;

      // 2. Fetch follow status
      bool isFollowingUser = false;
      isFollowingUser = await _repository.checkFollowingStatus(userId);

      state = ProfileState(
        user: user,
        followerCount: follower,
        followingCount: following,
        isFollowing: isFollowingUser,
        tweets: recentTweets,
      );

      // Async fetch liked and media tabs
      _loadLikedAndMedia();
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        errorMessage: e.toString().replaceAll('Exception: ', ''),
      );
    }
  }

  Future<void> _loadLikedAndMedia() async {
    try {
      final likes = await _repository.getUserLikes(userId);
      final media = await _repository.getUserMedia(userId);
      state = state.copyWith(
        likedTweets: likes,
        mediaTweets: media,
      );
    } catch (_) {}
  }

  // Follow Action
  Future<void> follow() async {
    final oldState = state;
    // Optimistic Update
    state = state.copyWith(
      isFollowing: true,
      followerCount: state.followerCount + 1,
    );

    try {
      await _repository.followUser(userId);
    } catch (_) {
      // Revert on error
      state = oldState;
    }
  }

  // Unfollow Action
  Future<void> unfollow() async {
    final oldState = state;
    // Optimistic Update
    state = state.copyWith(
      isFollowing: false,
      followerCount: state.followerCount - 1,
    );

    try {
      await _repository.unfollowUser(userId);
    } catch (_) {
      // Revert on error
      state = oldState;
    }
  }
}

final profileNotifierProvider =
    NotifierProvider.family<ProfileNotifier, ProfileState, String>((userId) {
  return ProfileNotifier(userId);
});
