import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:cached_network_image/cached_network_image.dart';
import 'package:go_router/go_router.dart';
import '../../auth/presentation/auth_notifier.dart';
import '../domain/tweet_model.dart';
import 'feed_notifier.dart';
import '../../../core/constants/colors.dart';
import '../../../core/utils/date_formatter.dart';
import '../../../core/network/dio_client.dart';

class TweetCard extends ConsumerWidget {
  final Tweet tweet;
  final VoidCallback? onTap;

  const TweetCard({
    super.key,
    required this.tweet,
    this.onTap,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    final user = tweet.user;
    final username = user?.username ?? '未知用户';
    final userHandle = '@${user?.username ?? "unknown"}';
    
    // Resolve avatar URL
    final avatarUrl = user?.avatar != null && user!.avatar.isNotEmpty
        ? DioClient.getMediaUrl(user.avatar)
        : '';

    return InkWell(
      onTap: onTap ?? () => context.push('/tweet/${tweet.id}'),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16.0, vertical: 12.0),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Left: User Avatar
            GestureDetector(
              onTap: () {
                if (user != null) {
                  context.push('/profile/${user.id}');
                }
              },
              child: CircleAvatar(
                radius: 22,
                backgroundColor: isDark ? AppColors.darkBorder : AppColors.lightBorder,
                backgroundImage: avatarUrl.isNotEmpty
                    ? CachedNetworkImageProvider(avatarUrl)
                    : null,
                child: avatarUrl.isEmpty
                    ? const Icon(Icons.person, color: Colors.grey)
                    : null,
              ),
            ),
            const SizedBox(width: 12),
            
            // Right: Content & Metadata
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  // Top Row: User metadata and timestamp
                  Row(
                    children: [
                      Flexible(
                        child: GestureDetector(
                          onTap: () {
                            if (user != null) {
                              context.push('/profile/${user.id}');
                            }
                          },
                          child: Text(
                            username,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: theme.textTheme.bodyLarge?.copyWith(
                              fontWeight: FontWeight.bold,
                            ),
                          ),
                        ),
                      ),
                      const SizedBox(width: 4),
                      Flexible(
                        child: Text(
                          userHandle,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            color: isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary,
                            fontSize: 14,
                          ),
                        ),
                      ),
                      const SizedBox(width: 4),
                      Text(
                        '·',
                        style: TextStyle(
                          color: isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary,
                        ),
                      ),
                      const SizedBox(width: 4),
                      Text(
                        DateFormatter.formatRelative(tweet.createdAt),
                        style: TextStyle(
                          color: isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary,
                          fontSize: 14,
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 4),
                  
                  // Tweet Text
                  Text(
                    tweet.content,
                    style: theme.textTheme.bodyLarge?.copyWith(
                      height: 1.3,
                    ),
                  ),
                  
                  // Media Grid (if any)
                  if (tweet.mediaUrls.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(top: 8.0),
                      child: _buildMediaGrid(context, tweet.mediaUrls),
                    ),
                    
                  const SizedBox(height: 10),
                  
                  // Bottom Row: Actions (Reply, Retweet, Like, Bookmark)
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      // Reply
                      _buildActionButton(
                        icon: Icons.chat_bubble_outline,
                        count: tweet.commentCount,
                        color: isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary,
                        onPressed: () => context.push('/tweet/${tweet.id}'),
                      ),
                      // Retweet
                      _buildActionButton(
                        icon: tweet.isRetweeted ? Icons.repeat : Icons.repeat_outlined,
                        count: tweet.retweetCount,
                        color: tweet.isRetweeted
                            ? AppColors.retweetColor
                            : (isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary),
                        onPressed: () {
                          ref.read(feedNotifierProvider.notifier).toggleRetweet(tweet.id);
                        },
                      ),
                      // Like
                      _buildActionButton(
                        icon: tweet.isLiked ? Icons.favorite : Icons.favorite_border,
                        count: tweet.likeCount,
                        color: tweet.isLiked
                            ? AppColors.likeColor
                            : (isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary),
                        onPressed: () {
                          ref.read(feedNotifierProvider.notifier).toggleLike(tweet.id);
                        },
                      ),
                      // Bookmark
                      _buildActionButton(
                        icon: tweet.isBookmarked ? Icons.bookmark : Icons.bookmark_border,
                        count: 0,
                        color: tweet.isBookmarked
                            ? AppColors.primary
                            : (isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary),
                        onPressed: () {
                          ref.read(feedNotifierProvider.notifier).toggleBookmark(tweet.id);
                        },
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildActionButton({
    required IconData icon,
    required int count,
    required Color color,
    required VoidCallback onPressed,
  }) {
    return GestureDetector(
      onTap: onPressed,
      behavior: HitTestBehavior.opaque,
      child: Padding(
        padding: const EdgeInsets.all(4.0),
        child: Row(
          children: [
            Icon(icon, size: 18, color: color),
            if (count > 0) ...[
              const SizedBox(width: 6),
              Text(
                count.toString(),
                style: TextStyle(
                  fontSize: 12,
                  color: color,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  // Beautiful grid based on Twitter design
  Widget _buildMediaGrid(BuildContext context, List<String> rawUrls) {
    final urls = rawUrls.map((e) => DioClient.getMediaUrl(e)).toList();
    final count = urls.length;
    
    return ClipRRect(
      borderRadius: BorderRadius.circular(12),
      child: AspectRatio(
        aspectRatio: count == 1 ? 16 / 9 : 1.8,
        child: Builder(
          builder: (context) {
            if (count == 1) {
              return _buildImage(urls[0]);
            } else if (count == 2) {
              return Row(
                children: [
                  Expanded(child: _buildImage(urls[0])),
                  const SizedBox(width: 2),
                  Expanded(child: _buildImage(urls[1])),
                ],
              );
            } else if (count == 3) {
              return Row(
                children: [
                  Expanded(child: _buildImage(urls[0])),
                  const SizedBox(width: 2),
                  Expanded(
                    child: Column(
                      children: [
                        Expanded(child: _buildImage(urls[1])),
                        const SizedBox(height: 2),
                        Expanded(child: _buildImage(urls[2])),
                      ],
                    ),
                  ),
                ],
              );
            } else {
              // 4 or more
              return Column(
                children: [
                  Expanded(
                    child: Row(
                      children: [
                        Expanded(child: _buildImage(urls[0])),
                        const SizedBox(width: 2),
                        Expanded(child: _buildImage(urls[1])),
                      ],
                    ),
                  ),
                  const SizedBox(height: 2),
                  Expanded(
                    child: Row(
                      children: [
                        Expanded(child: _buildImage(urls[2])),
                        const SizedBox(width: 2),
                        Expanded(child: _buildImage(urls[3])),
                      ],
                    ),
                  ),
                ],
              );
            }
          },
        ),
      ),
    );
  }

  Widget _buildImage(String url) {
    return SizedBox.expand(
      child: CachedNetworkImage(
        imageUrl: url,
        fit: BoxFit.cover,
        placeholder: (context, url) => Container(
          color: Colors.grey[300],
          child: const Center(
            child: SizedBox(
              width: 24,
              height: 24,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
          ),
        ),
        errorWidget: (context, url, error) => Container(
          color: Colors.grey[800],
          child: const Icon(Icons.broken_image, color: Colors.grey),
        ),
      ),
    );
  }
}
