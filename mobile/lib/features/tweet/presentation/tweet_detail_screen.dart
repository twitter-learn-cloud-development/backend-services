import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:cached_network_image/cached_network_image.dart';
import '../domain/tweet_model.dart';
import '../domain/comment_model.dart';
import 'feed_notifier.dart';
import 'tweet_card.dart';
import '../../auth/presentation/auth_notifier.dart';
import '../../../core/constants/colors.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/utils/date_formatter.dart';

class TweetDetailScreen extends ConsumerStatefulWidget {
  final String tweetId;

  const TweetDetailScreen({super.key, required this.tweetId});

  @override
  ConsumerState<TweetDetailScreen> createState() => _TweetDetailScreenState();
}

class _HomeScreenScrollOffset {} // Just an empty state marker

class _TweetDetailScreenState extends ConsumerState<TweetDetailScreen> {
  Tweet? _tweet;
  List<CommentModel> _comments = [];
  bool _isLoadingTweet = true;
  bool _isLoadingComments = true;
  bool _isSubmittingComment = false;
  String _commentsCursor = '0';
  bool _commentsHasMore = false;
  final _commentController = TextEditingController();
  final ScrollController _scrollController = ScrollController();

  @override
  void initState() {
    super.initState();
    _fetchTweetDetails();
    _fetchComments();
    _scrollController.addListener(_onScroll);
  }

  @override
  void dispose() {
    _commentController.dispose();
    _scrollController.removeListener(_onScroll);
    _scrollController.dispose();
    super.dispose();
  }

  void _onScroll() {
    if (_scrollController.position.pixels >=
        _scrollController.position.maxScrollExtent * 0.9) {
      _fetchNextCommentsPage();
    }
  }

  Future<void> _fetchTweetDetails() async {
    try {
      final repo = ref.read(tweetRepositoryProvider);
      final tweet = await repo.getTweet(widget.tweetId);
      setState(() {
        _tweet = tweet;
        _isLoadingTweet = false;
      });
    } catch (e) {
      setState(() {
        _isLoadingTweet = false;
      });
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('加载推文失败: $e')),
      );
    }
  }

  Future<void> _fetchComments() async {
    try {
      final repo = ref.read(tweetRepositoryProvider);
      final result = await repo.getComments(widget.tweetId, cursor: '0');
      setState(() {
        _comments = result['comments'] as List<CommentModel>;
        _commentsCursor = result['next_cursor'] as String;
        _commentsHasMore = result['has_more'] as bool;
        _isLoadingComments = false;
      });
    } catch (_) {
      setState(() {
        _isLoadingComments = false;
      });
    }
  }

  Future<void> _fetchNextCommentsPage() async {
    if (!_commentsHasMore || _isLoadingComments) return;
    
    try {
      final repo = ref.read(tweetRepositoryProvider);
      final result = await repo.getComments(widget.tweetId, cursor: _commentsCursor);
      setState(() {
        _comments.addAll(result['comments'] as List<CommentModel>);
        _commentsCursor = result['next_cursor'] as String;
        _commentsHasMore = result['has_more'] as bool;
      });
    } catch (_) {}
  }

  Future<void> _submitComment() async {
    final content = _commentController.text.trim();
    if (content.isEmpty || _isSubmittingComment) return;

    setState(() {
      _isSubmittingComment = true;
    });

    try {
      final repo = ref.read(tweetRepositoryProvider);
      final newComment = await repo.createComment(widget.tweetId, content);
      
      setState(() {
        _comments.insert(0, newComment);
        _commentController.clear();
        // Increment count locally
        if (_tweet != null) {
          _tweet = _tweet!.copyWith(commentCount: _tweet!.commentCount + 1);
        }
      });
      
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('回复成功'),
          backgroundColor: AppColors.retweetColor,
          duration: Duration(seconds: 1),
        ),
      );
    } catch (e) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('回复失败: $e')),
      );
    } finally {
      setState(() {
        _isSubmittingComment = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final authState = ref.watch(authNotifierProvider);
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    final currentUser = authState.user;
    final avatarUrl = currentUser?.avatar != null && currentUser!.avatar.isNotEmpty
        ? DioClient.getMediaUrl(currentUser.avatar)
        : '';

    return Scaffold(
      backgroundColor: isDark ? AppColors.darkBg : AppColors.lightBg,
      appBar: AppBar(
        title: const Text('帖子'),
      ),
      body: _isLoadingTweet
          ? const Center(child: CircularProgressIndicator(color: AppColors.primary))
          : Column(
              children: [
                Expanded(
                  child: CustomScrollView(
                    controller: _scrollController,
                    slivers: [
                      // 1. Tweet Details
                      SliverToBoxAdapter(
                        child: Column(
                          children: [
                            if (_tweet != null)
                              TweetCard(
                                tweet: _tweet!,
                                onTap: () {}, // Prevent navigating to itself
                              ),
                            const Divider(height: 1),
                          ],
                        ),
                      ),
                      
                      // 2. Comments List Header
                      SliverToBoxAdapter(
                        child: Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 16.0, vertical: 12.0),
                          child: Text(
                            '回复',
                            style: theme.textTheme.titleMedium?.copyWith(
                              fontWeight: FontWeight.bold,
                            ),
                          ),
                        ),
                      ),
                      
                      // 3. Comments Stream
                      _buildCommentsSection(isDark),
                    ],
                  ),
                ),
                
                // Reply Input Bar
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                  decoration: BoxDecoration(
                    color: isDark ? AppColors.darkBg : AppColors.lightBg,
                    border: Border(
                      top: BorderSide(
                        color: isDark ? AppColors.darkBorder : AppColors.lightBorder,
                        width: 0.5,
                      ),
                    ),
                  ),
                  child: Row(
                    children: [
                      CircleAvatar(
                        radius: 18,
                        backgroundColor: isDark ? AppColors.darkBorder : AppColors.lightBorder,
                        backgroundImage: avatarUrl.isNotEmpty
                            ? CachedNetworkImageProvider(avatarUrl)
                            : null,
                        child: avatarUrl.isEmpty
                            ? const Icon(Icons.person, size: 18)
                            : null,
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: TextField(
                          controller: _commentController,
                          enabled: !_isSubmittingComment,
                          style: TextStyle(color: isDark ? Colors.white : Colors.black),
                          maxLines: null,
                          decoration: const InputDecoration(
                            hintText: '发布你的回复',
                            hintStyle: TextStyle(fontSize: 14),
                            border: InputBorder.none,
                            enabledBorder: InputBorder.none,
                            focusedBorder: InputBorder.none,
                            contentPadding: EdgeInsets.zero,
                          ),
                        ),
                      ),
                      const SizedBox(width: 8),
                      TextButton(
                        onPressed: _commentController.text.trim().isEmpty || _isSubmittingComment
                            ? null
                            : _submitComment,
                        style: TextButton.styleFrom(
                          foregroundColor: AppColors.primary,
                          disabledForegroundColor: Colors.grey,
                        ),
                        child: _isSubmittingComment
                            ? const SizedBox(
                                width: 14,
                                height: 14,
                                child: CircularProgressIndicator(strokeWidth: 2),
                              )
                            : const Text(
                                '回复',
                                style: TextStyle(
                                  fontWeight: FontWeight.bold,
                                  fontSize: 15,
                                ),
                              ),
                      ),
                    ],
                  ),
                ),
              ],
            ),
    );
  }

  Widget _buildCommentsSection(bool isDark) {
    if (_isLoadingComments && _comments.isEmpty) {
      return const SliverToBoxAdapter(
        child: Padding(
          padding: EdgeInsets.all(20.0),
          child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
        ),
      );
    }

    if (_comments.isEmpty) {
      return const SliverToBoxAdapter(
        child: Padding(
          padding: EdgeInsets.symmetric(vertical: 40.0),
          child: Center(
            child: Text(
              '还没有人回复。抢个沙发吧！',
              style: TextStyle(color: Colors.grey),
            ),
          ),
        ),
      );
    }

    return SliverList(
      delegate: SliverChildBuilderDelegate(
        (context, index) {
          final comment = _comments[index];
          final commentAvatar = DioClient.getMediaUrl(comment.avatarUrl);
          
          return Container(
            padding: const EdgeInsets.symmetric(horizontal: 16.0, vertical: 12.0),
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(
                  color: isDark ? AppColors.darkBorder : AppColors.lightBorder,
                  width: 0.5,
                ),
              ),
            ),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                CircleAvatar(
                  radius: 18,
                  backgroundColor: isDark ? AppColors.darkBorder : AppColors.lightBorder,
                  backgroundImage: commentAvatar.isNotEmpty
                      ? CachedNetworkImageProvider(commentAvatar)
                      : null,
                  child: commentAvatar.isEmpty
                      ? const Icon(Icons.person, size: 18)
                      : null,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Text(
                            comment.username,
                            style: const TextStyle(
                              fontWeight: FontWeight.bold,
                              fontSize: 14,
                            ),
                          ),
                          const SizedBox(width: 6),
                          Text(
                            '@${comment.username}',
                            style: const TextStyle(
                              color: Colors.grey,
                              fontSize: 12,
                            ),
                          ),
                          const SizedBox(width: 4),
                          const Text('·', style: TextStyle(color: Colors.grey)),
                          const SizedBox(width: 4),
                          Text(
                            DateFormatter.formatRelative(comment.createdAt),
                            style: const TextStyle(
                              color: Colors.grey,
                              fontSize: 12,
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 4),
                      Text(
                        comment.content,
                        style: const TextStyle(fontSize: 14),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          );
        },
        childCount: _comments.length,
      ),
    );
  }
}
