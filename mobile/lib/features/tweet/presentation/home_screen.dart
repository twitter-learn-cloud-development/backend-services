import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:cached_network_image/cached_network_image.dart';
import '../../auth/presentation/auth_notifier.dart';
import 'feed_notifier.dart';
import 'tweet_card.dart';
import '../../../core/constants/colors.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/utils/storage.dart';

class HomeScreen extends ConsumerStatefulWidget {
  const HomeScreen({super.key});

  @override
  ConsumerState<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends ConsumerState<HomeScreen> {
  final ScrollController _scrollController = ScrollController();

  @override
  void initState() {
    super.initState();
    _scrollController.addListener(_onScroll);
  }

  @override
  void dispose() {
    _scrollController.removeListener(_onScroll);
    _scrollController.dispose();
    super.dispose();
  }

  void _onScroll() {
    if (_scrollController.position.pixels >=
        _scrollController.position.maxScrollExtent * 0.9) {
      ref.read(feedNotifierProvider.notifier).fetchNextPage();
    }
  }

  @override
  Widget build(BuildContext context) {
    final authState = ref.watch(authNotifierProvider);
    final feedState = ref.watch(feedNotifierProvider);
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    // Listen to action failures and show error tips
    ref.listen<FeedState>(feedNotifierProvider, (previous, next) {
      if (next.errorMessage != null && next.errorMessage != previous?.errorMessage) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(next.errorMessage!),
            backgroundColor: AppColors.likeColor,
          ),
        );
      }
    });

    // Resolve current user avatar
    final currentUser = authState.user;
    final avatarUrl = currentUser?.avatar != null && currentUser!.avatar.isNotEmpty
        ? DioClient.getMediaUrl(currentUser.avatar)
        : '';

    return Scaffold(
      appBar: AppBar(
        leading: Padding(
          padding: const EdgeInsets.all(8.0),
          child: GestureDetector(
            onTap: () {
              if (currentUser != null) {
                context.push('/profile/${currentUser.id}');
              }
            },
            child: CircleAvatar(
              backgroundColor: isDark ? AppColors.darkBorder : AppColors.lightBorder,
              backgroundImage: avatarUrl.isNotEmpty
                  ? CachedNetworkImageProvider(avatarUrl)
                  : null,
              child: avatarUrl.isEmpty
                  ? const Icon(Icons.person, size: 20)
                  : null,
            ),
          ),
        ),
        title: const Icon(
          Icons.flutter_dash,
          color: AppColors.primary,
          size: 30,
        ),
        centerTitle: true,
        actions: [
          // Theme toggler
          IconButton(
            icon: Icon(isDark ? Icons.light_mode : Icons.dark_mode),
            onPressed: () async {
              final newMode = isDark ? 'light' : 'dark';
              await Storage.saveThemeMode(newMode);
              
              // We restart the app or force a redraw by changing themeMode.
              // For simplicity in this demo system, we can prompt or trigger rebuild
              // In production we would use a ThemeProvider.
              ScaffoldMessenger.of(context).showSnackBar(
                const SnackBar(
                  content: Text('主题设置已保存，请重启应用生效'),
                  duration: Duration(seconds: 1),
                ),
              );
            },
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () => ref.read(feedNotifierProvider.notifier).refresh(),
        color: AppColors.primary,
        child: _buildContent(context, feedState),
      ),
    );
  }

  Widget _buildContent(BuildContext context, FeedState state) {
    if (state.isLoading && state.tweets.isEmpty) {
      return const Center(
        child: CircularProgressIndicator(color: AppColors.primary),
      );
    }

    if (state.errorMessage != null && state.tweets.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 24.0),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Text(
                '加载失败: ${state.errorMessage}',
                textAlign: TextAlign.center,
                style: const TextStyle(fontSize: 16),
              ),
              const SizedBox(height: 16),
              ElevatedButton(
                onPressed: () => ref.read(feedNotifierProvider.notifier).refresh(),
                child: const Text('重试'),
              ),
            ],
          ),
        ),
      );
    }

    if (state.tweets.isEmpty) {
      return const Center(
        child: Text(
          '你的时间线上还没有推文，去关注一些人吧！',
          style: TextStyle(fontSize: 16),
        ),
      );
    }

    return ListView.separated(
      controller: _scrollController,
      itemCount: state.tweets.length + 1,
      separatorBuilder: (context, index) => const Divider(height: 1),
      itemBuilder: (context, index) {
        if (index == state.tweets.length) {
          // Bottom loading spinner for pagination
          return state.hasMore
              ? const Padding(
                  padding: EdgeInsets.symmetric(vertical: 20.0),
                  child: Center(
                    child: CircularProgressIndicator(
                      color: AppColors.primary,
                      strokeWidth: 2,
                    ),
                  ),
                )
              : const SizedBox(height: 50); // Spacer at end of timeline
        }

        final tweet = state.tweets[index];
        return TweetCard(tweet: tweet);
      },
    );
  }
}
