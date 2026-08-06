import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:cached_network_image/cached_network_image.dart';
import '../../auth/presentation/auth_notifier.dart';
import '../../tweet/presentation/tweet_card.dart';
import '../../tweet/domain/tweet_model.dart';
import 'profile_notifier.dart';
import '../../../core/constants/colors.dart';
import '../../../core/network/dio_client.dart';

class ProfileScreen extends ConsumerWidget {
  final String userId;

  const ProfileScreen({super.key, required this.userId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final authState = ref.watch(authNotifierProvider);
    
    // Resolve profile owner ID. If 'me' is passed, use currently logged in user ID
    final currentUserId = authState.user?.id ?? '';
    final resolvedId = (userId == 'me' || userId.isEmpty) ? currentUserId : userId;
    
    final isMe = resolvedId == currentUserId;
    final profileState = ref.watch(profileNotifierProvider(resolvedId));

    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    if (profileState.isLoading && profileState.user == null) {
      return const Scaffold(
        body: Center(child: CircularProgressIndicator(color: AppColors.primary)),
      );
    }

    final user = profileState.user;
    if (user == null) {
      return Scaffold(
        appBar: AppBar(title: const Text('个人主页')),
        body: const Center(child: Text('用户未找到')),
      );
    }

    final avatarUrl = user.avatar.isNotEmpty ? DioClient.getMediaUrl(user.avatar) : '';
    final coverUrl = user.coverUrl.isNotEmpty ? DioClient.getMediaUrl(user.coverUrl) : '';

    return Scaffold(
      backgroundColor: isDark ? AppColors.darkBg : AppColors.lightBg,
      body: DefaultTabController(
        length: 3,
        child: NestedScrollView(
          headerSliverBuilder: (context, innerBoxIsScrolled) {
            return [
              // Custom Header Sliver (Cover, Avatar & Profile Metadata)
              SliverAppBar(
                expandedHeight: 180,
                floating: false,
                pinned: true,
                backgroundColor: isDark ? AppColors.darkBg : AppColors.lightBg,
                flexibleSpace: FlexibleSpaceBar(
                  background: Stack(
                    children: [
                      // Cover Banner image
                      Container(
                        height: 130,
                        width: double.infinity,
                        color: AppColors.primary.withOpacity(0.3),
                        child: coverUrl.isNotEmpty
                            ? CachedNetworkImage(imageUrl: coverUrl, fit: BoxFit.cover)
                            : null,
                      ),
                      
                      // Avatar overlapping
                      Positioned(
                        top: 90,
                        left: 16,
                        child: Container(
                          padding: const EdgeInsets.all(4),
                          decoration: BoxDecoration(
                            color: isDark ? AppColors.darkBg : AppColors.lightBg,
                            shape: BoxShape.circle,
                          ),
                          child: CircleAvatar(
                            radius: 36,
                            backgroundColor: isDark ? AppColors.darkBorder : AppColors.lightBorder,
                            backgroundImage: avatarUrl.isNotEmpty
                                ? CachedNetworkImageProvider(avatarUrl)
                                : null,
                            child: avatarUrl.isEmpty
                                ? const Icon(Icons.person, size: 36, color: Colors.grey)
                                : null,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
                actions: [
                  if (isMe)
                    IconButton(
                      icon: const Icon(Icons.logout, color: AppColors.likeColor),
                      onPressed: () {
                        // Confirm logout
                        showDialog(
                          context: context,
                          builder: (context) => AlertDialog(
                            title: const Text('确认退出登录？'),
                            actions: [
                              TextButton(
                                onPressed: () => Navigator.of(context).pop(),
                                child: const Text('取消'),
                              ),
                              TextButton(
                                onPressed: () {
                                  Navigator.of(context).pop();
                                  ref.read(authNotifierProvider.notifier).logout();
                                  context.go('/login');
                                },
                                child: const Text('退出', style: TextStyle(color: AppColors.likeColor)),
                              ),
                            ],
                          ),
                        );
                      },
                    ),
                ],
              ),
              
              // Metadata and Description section
              SliverToBoxAdapter(
                child: Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 16.0),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      // Actions row: Follow button or Edit Profile
                      Row(
                        mainAxisAlignment: MainAxisAlignment.end,
                        children: [
                          if (isMe)
                            OutlinedButton(
                              onPressed: () {
                                ScaffoldMessenger.of(context).showSnackBar(
                                  const SnackBar(content: Text('编辑个人资料功能将在后续迭代中支持')),
                                );
                              },
                              style: OutlinedButton.styleFrom(
                                side: BorderSide(
                                  color: isDark ? AppColors.darkBorder : AppColors.lightBorder,
                                ),
                                shape: RoundedRectangleBorder(
                                  borderRadius: BorderRadius.circular(20),
                                ),
                              ),
                              child: const Text('编辑个人资料', style: TextStyle(fontWeight: FontWeight.bold)),
                            )
                          else
                            Row(
                              children: [
                                IconButton(
                                  icon: const Icon(Icons.message_outlined),
                                  onPressed: () {
                                    context.push('/chat/${user.id}');
                                  },
                                ),
                                const SizedBox(width: 8),
                                ElevatedButton(
                                  onPressed: () {
                                    final notifier = ref.read(profileNotifierProvider(resolvedId).notifier);
                                    if (profileState.isFollowing) {
                                      notifier.unfollow();
                                    } else {
                                      notifier.follow();
                                    }
                                  },
                                  style: ElevatedButton.styleFrom(
                                    backgroundColor: profileState.isFollowing
                                        ? Colors.transparent
                                        : (isDark ? Colors.white : Colors.black),
                                    foregroundColor: profileState.isFollowing
                                        ? (isDark ? Colors.white : Colors.black)
                                        : (isDark ? Colors.black : Colors.white),
                                    side: profileState.isFollowing
                                        ? BorderSide(color: isDark ? AppColors.darkBorder : AppColors.lightBorder)
                                        : BorderSide.none,
                                    shape: RoundedRectangleBorder(
                                      borderRadius: BorderRadius.circular(20),
                                    ),
                                    elevation: 0,
                                  ),
                                  child: Text(
                                    profileState.isFollowing ? '正在关注' : '关注',
                                    style: const TextStyle(fontWeight: FontWeight.bold),
                                  ),
                                ),
                              ],
                            ),
                        ],
                      ),
                      
                      const SizedBox(height: 8),
                      
                      // Username and handle
                      Text(
                        user.username,
                        style: theme.textTheme.titleLarge?.copyWith(
                          fontSize: 22,
                          fontWeight: FontWeight.bold,
                        ),
                      ),
                      Text(
                        '@${user.username}',
                        style: TextStyle(
                          color: isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary,
                        ),
                      ),
                      
                      const SizedBox(height: 12),
                      
                      // Bio description
                      if (user.bio.isNotEmpty) ...[
                        Text(user.bio, style: theme.textTheme.bodyLarge),
                        const SizedBox(height: 12),
                      ],
                      
                      // Details row (location, website, registration date)
                      Wrap(
                        spacing: 12,
                        runSpacing: 6,
                        children: [
                          if (user.location.isNotEmpty)
                            _buildInfoItem(Icons.location_on_outlined, user.location, isDark),
                          if (user.website.isNotEmpty)
                            _buildInfoItem(Icons.link_outlined, user.website, isDark, isLink: true),
                          _buildInfoItem(
                            Icons.calendar_month_outlined,
                            '2026年加入', // Hardcoded for template or parse date
                            isDark,
                          ),
                        ],
                      ),
                      
                      const SizedBox(height: 12),
                      
                      // Follow Stats
                      Row(
                        children: [
                          _buildStatItem(profileState.followingCount, '正在关注', isDark),
                          const SizedBox(width: 20),
                          _buildStatItem(profileState.followerCount, '关注者', isDark),
                        ],
                      ),
                      
                      const SizedBox(height: 16),
                    ],
                  ),
                ),
              ),
              
              // Tabs Header Sliver
              SliverPersistentHeader(
                pinned: true,
                delegate: _SliverAppBarDelegate(
                  TabBar(
                    labelColor: isDark ? Colors.white : Colors.black,
                    unselectedLabelColor: Colors.grey,
                    indicatorColor: AppColors.primary,
                    indicatorSize: TabBarIndicatorSize.label,
                    tabs: const [
                      Tab(text: '帖子'),
                      Tab(text: '媒体'),
                      Tab(text: '喜欢'),
                    ],
                  ),
                  isDark: isDark,
                ),
              ),
            ];
          },
          body: TabBarView(
            children: [
              // Tab 1: Tweets
              _buildTweetsList(profileState.tweets, '暂无推文'),
              
              // Tab 2: Media
              _buildTweetsList(profileState.mediaTweets, '暂无媒体推文'),
              
              // Tab 3: Likes
              _buildTweetsList(profileState.likedTweets, '暂无喜欢的推文'),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildInfoItem(IconData icon, String text, bool isDark, {bool isLink = false}) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 16, color: Colors.grey),
        const SizedBox(width: 4),
        Text(
          text,
          style: TextStyle(
            color: isLink
                ? AppColors.primary
                : (isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary),
            fontSize: 13,
          ),
        ),
      ],
    );
  }

  Widget _buildStatItem(int count, String label, bool isDark) {
    return Row(
      children: [
        Text(
          count.toString(),
          style: TextStyle(
            fontWeight: FontWeight.bold,
            color: isDark ? Colors.white : Colors.black,
            fontSize: 14,
          ),
        ),
        const SizedBox(width: 4),
        Text(
          label,
          style: TextStyle(
            color: isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary,
            fontSize: 14,
          ),
        ),
      ],
    );
  }

  Widget _buildTweetsList(List<Tweet> tweets, String emptyMsg) {
    if (tweets.isEmpty) {
      return Center(
        child: Text(
          emptyMsg,
          style: const TextStyle(color: Colors.grey, fontSize: 15),
        ),
      );
    }
    return ListView.separated(
      padding: EdgeInsets.zero,
      itemCount: tweets.length,
      separatorBuilder: (context, index) => const Divider(height: 1),
      itemBuilder: (context, index) {
        return TweetCard(tweet: tweets[index]);
      },
    );
  }
}

// Custom Delegate to wrap TabBar inside CustomScrollView slivers
class _SliverAppBarDelegate extends SliverPersistentHeaderDelegate {
  final TabBar tabBar;
  final bool isDark;

  _SliverAppBarDelegate(this.tabBar, {required this.isDark});

  @override
  double get minExtent => tabBar.preferredSize.height;
  @override
  double get maxExtent => tabBar.preferredSize.height;

  @override
  Widget build(BuildContext context, double shrinkOffset, bool overlapsContent) {
    return Container(
      color: isDark ? AppColors.darkBg : AppColors.lightBg,
      child: tabBar,
    );
  }

  @override
  bool shouldRebuild(_SliverAppBarDelegate oldDelegate) {
    return false;
  }
}
