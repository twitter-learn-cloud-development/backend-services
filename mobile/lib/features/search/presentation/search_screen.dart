import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../tweet/domain/tweet_model.dart';
import '../../tweet/presentation/tweet_card.dart';
import '../../auth/presentation/auth_notifier.dart';
import '../data/search_repository.dart';
import '../../../core/constants/colors.dart';

final searchRepositoryProvider = Provider<SearchRepository>((ref) {
  final dio = ref.watch(dioProvider);
  return SearchRepository(dio);
});

class SearchScreen extends ConsumerStatefulWidget {
  const SearchScreen({super.key});

  @override
  ConsumerState<SearchScreen> createState() => _SearchScreenState();
}

class _SearchScreenState extends ConsumerState<SearchScreen> {
  final _searchController = TextEditingController();
  List<Map<String, dynamic>> _trends = [];
  List<Tweet> _searchResults = [];
  bool _isLoadingTrends = true;
  bool _isLoadingSearch = false;
  String _searchCursor = '0';
  bool _searchHasMore = false;
  bool _hasSearched = false;
  final ScrollController _scrollController = ScrollController();

  @override
  void initState() {
    super.initState();
    _fetchTrends();
    _scrollController.addListener(_onScroll);
  }

  @override
  void dispose() {
    _searchController.dispose();
    _scrollController.removeListener(_onScroll);
    _scrollController.dispose();
    super.dispose();
  }

  void _onScroll() {
    if (_scrollController.position.pixels >=
        _scrollController.position.maxScrollExtent * 0.9) {
      _fetchNextSearchPage();
    }
  }

  Future<void> _fetchTrends() async {
    try {
      final repo = ref.read(searchRepositoryProvider);
      final topics = await repo.getTrendingTopics();
      setState(() {
        _trends = topics;
        _isLoadingTrends = false;
      });
    } catch (_) {
      setState(() {
        _isLoadingTrends = false;
      });
    }
  }

  Future<void> _performSearch(String query) async {
    if (query.trim().isEmpty) {
      setState(() {
        _hasSearched = false;
        _searchResults.clear();
      });
      return;
    }

    setState(() {
      _isLoadingSearch = true;
      _hasSearched = true;
    });

    try {
      final repo = ref.read(searchRepositoryProvider);
      final result = await repo.searchTweets(query, cursor: '0');
      setState(() {
        _searchResults = result['tweets'] as List<Tweet>;
        _searchCursor = result['next_cursor'] as String;
        _searchHasMore = result['has_more'] as bool;
        _isLoadingSearch = false;
      });
    } catch (e) {
      setState(() {
        _isLoadingSearch = false;
      });
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('搜索失败: $e')),
      );
    }
  }

  Future<void> _fetchNextSearchPage() async {
    if (!_searchHasMore || _isLoadingSearch) return;

    try {
      final repo = ref.read(searchRepositoryProvider);
      final result = await repo.searchTweets(_searchController.text, cursor: _searchCursor);
      setState(() {
        _searchResults.addAll(result['tweets'] as List<Tweet>);
        _searchCursor = result['next_cursor'] as String;
        _searchHasMore = result['has_more'] as bool;
      });
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    return Scaffold(
      backgroundColor: isDark ? AppColors.darkBg : AppColors.lightBg,
      appBar: AppBar(
        title: Container(
          height: 40,
          decoration: BoxDecoration(
            color: isDark ? AppColors.darkSurface : AppColors.lightSurface,
            borderRadius: BorderRadius.circular(20),
          ),
          child: TextField(
            controller: _searchController,
            style: TextStyle(color: isDark ? Colors.white : Colors.black),
            decoration: InputDecoration(
              hintText: '搜索 Twitter',
              hintStyle: const TextStyle(fontSize: 14),
              prefixIcon: const Icon(Icons.search, size: 20),
              suffixIcon: _searchController.text.isNotEmpty
                  ? IconButton(
                      icon: const Icon(Icons.clear, size: 18),
                      onPressed: () {
                        _searchController.clear();
                        setState(() {
                          _hasSearched = false;
                          _searchResults.clear();
                        });
                      },
                    )
                  : null,
              contentPadding: const EdgeInsets.symmetric(vertical: 8),
              border: InputBorder.none,
              enabledBorder: InputBorder.none,
              focusedBorder: InputBorder.none,
            ),
            onSubmitted: _performSearch,
          ),
        ),
      ),
      body: _hasSearched ? _buildSearchResults(isDark) : _buildTrends(theme, isDark),
    );
  }

  Widget _buildTrends(ThemeData theme, bool isDark) {
    if (_isLoadingTrends) {
      return const Center(child: CircularProgressIndicator(color: AppColors.primary));
    }

    if (_trends.isEmpty) {
      return const Center(
        child: Text('暂无趋势话题', style: TextStyle(color: Colors.grey)),
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.symmetric(vertical: 10),
      itemCount: _trends.length + 1,
      itemBuilder: (context, index) {
        if (index == 0) {
          return Padding(
            padding: const EdgeInsets.all(16.0),
            child: Text(
              '热门趋势',
              style: theme.textTheme.titleLarge?.copyWith(
                fontWeight: FontWeight.bold,
              ),
            ),
          );
        }

        final trend = _trends[index - 1];
        final rank = index;
        return ListTile(
          onTap: () {
            _searchController.text = '#${trend['topic']}';
            _performSearch('#${trend['topic']}');
          },
          leading: Text(
            rank.toString(),
            style: const TextStyle(
              fontSize: 16,
              fontWeight: FontWeight.bold,
              color: Colors.grey,
            ),
          ),
          title: Text(
            '#${trend['topic']}',
            style: const TextStyle(
              fontWeight: FontWeight.bold,
              fontSize: 16,
            ),
          ),
          subtitle: Text(
            '${trend['score']} 关联热度',
            style: TextStyle(
              color: isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary,
              fontSize: 12,
            ),
          ),
          trailing: const Icon(Icons.trending_up, color: AppColors.primary, size: 20),
        );
      },
    );
  }

  Widget _buildSearchResults(bool isDark) {
    if (_isLoadingSearch && _searchResults.isEmpty) {
      return const Center(child: CircularProgressIndicator(color: AppColors.primary));
    }

    if (_searchResults.isEmpty) {
      return const Center(
        child: Text('未找到匹配的推文', style: TextStyle(color: Colors.grey, fontSize: 16)),
      );
    }

    return ListView.separated(
      controller: _scrollController,
      itemCount: _searchResults.length + 1,
      separatorBuilder: (context, index) => const Divider(height: 1),
      itemBuilder: (context, index) {
        if (index == _searchResults.length) {
          return _searchHasMore
              ? const Padding(
                  padding: EdgeInsets.symmetric(vertical: 16),
                  child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
                )
              : const SizedBox(height: 50);
        }
        return TweetCard(tweet: _searchResults[index]);
      },
    );
  }
}
