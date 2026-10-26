import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:cached_network_image/cached_network_image.dart';
import '../domain/message_model.dart';
import '../data/chat_repository.dart';
import '../../auth/presentation/auth_notifier.dart';
import '../../../core/constants/colors.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/utils/date_formatter.dart';
import '../../../core/network/websocket_service.dart';
import 'message_history.dart';

final chatRepositoryProvider = Provider<ChatRepository>((ref) {
  final dio = ref.watch(dioProvider);
  return ChatRepository(dio);
});

class ChatListScreen extends ConsumerStatefulWidget {
  const ChatListScreen({super.key});

  @override
  ConsumerState<ChatListScreen> createState() => _ChatListScreenState();
}

class _ChatListScreenState extends ConsumerState<ChatListScreen>
    with WidgetsBindingObserver {
  List<ConversationModel> _conversations = [];
  bool _isLoading = true;
  bool _hasMore = false;
  String _cursor = '0';
  String? _error;
  int _generation = 0;
  StreamSubscription? _readySubscription, _messageSubscription;
  Timer? _refreshTimer, _pollTimer;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _fetchConversations();
    final socket = ref.read(websocketServiceProvider);
    _readySubscription = socket.readyStream.listen((_) => _scheduleRefresh());
    _messageSubscription = socket.messageStream.listen(
      (_) => _scheduleRefresh(),
    );
    _pollTimer = Timer.periodic(
      const Duration(seconds: 30),
      (_) => _scheduleRefresh(),
    );
  }

  @override
  void dispose() {
    ++_generation;
    WidgetsBinding.instance.removeObserver(this);
    _readySubscription?.cancel();
    _messageSubscription?.cancel();
    _refreshTimer?.cancel();
    _pollTimer?.cancel();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) _scheduleRefresh();
  }

  void _scheduleRefresh() {
    if (!mounted ||
        (WidgetsBinding.instance.lifecycleState != null &&
            WidgetsBinding.instance.lifecycleState !=
                AppLifecycleState.resumed)) {
      return;
    }
    _refreshTimer ??= Timer(const Duration(milliseconds: 250), () {
      _refreshTimer = null;
      if (mounted && !_isLoading) _fetchConversations();
    });
  }

  Future<void> _fetchConversations({bool more = false}) async {
    if (!mounted || (more && (_isLoading || !_hasMore))) return;
    final generation = more ? _generation : ++_generation;
    final epoch = ref.read(authNotifierProvider.notifier).sessionRevision;
    final cursor = more ? _cursor : '0';
    setState(() {
      _isLoading = true;
      _error = null;
    });
    try {
      final repo = ref.read(chatRepositoryProvider);
      final result = await repo.getConversations(cursor: cursor);
      if (!mounted ||
          generation != _generation ||
          epoch != ref.read(authNotifierProvider.notifier).sessionRevision) {
        return;
      }
      if (result['has_more'] == true &&
          (result['next_cursor'] == '' || result['next_cursor'] == cursor)) {
        throw const FormatException('invalid cursor');
      }
      setState(() {
        final rows = {
          for (final conv in more ? _conversations : <ConversationModel>[])
            conv.peerId: conv,
        };
        for (final conv in result['conversations'] as List<ConversationModel>) {
          rows[conv.peerId] = conv;
        }
        _conversations = rows.values.toList()
          ..sort(
            (a, b) => compareMessageIds(
              b.latestMessage?.id ?? '0',
              a.latestMessage?.id ?? '0',
            ),
          );
        _cursor = result['next_cursor'];
        _hasMore = result['has_more'];
        _isLoading = false;
      });
    } catch (_) {
      if (!mounted ||
          generation != _generation ||
          epoch != ref.read(authNotifierProvider.notifier).sessionRevision) {
        return;
      }
      setState(() {
        _isLoading = false;
        _error = '会话加载失败，请重试';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    return Scaffold(
      backgroundColor: isDark ? AppColors.darkBg : AppColors.lightBg,
      appBar: AppBar(title: const Text('私信会话'), centerTitle: true),
      body: Column(
        children: [
          if (_error != null)
            TextButton(
              onPressed: () => _fetchConversations(),
              child: Text(_error!),
            ),
          if (_hasMore)
            TextButton(
              onPressed: _isLoading
                  ? null
                  : () => _fetchConversations(more: true),
              child: const Text('加载更多会话'),
            ),
          Expanded(
            child: RefreshIndicator(
              onRefresh: () => _fetchConversations(),
              color: AppColors.primary,
              child: _isLoading
                  ? const Center(
                      child: CircularProgressIndicator(
                        color: AppColors.primary,
                      ),
                    )
                  : _conversations.isEmpty
                  ? const Center(
                      child: Text(
                        '暂无私信对话',
                        style: TextStyle(color: Colors.grey, fontSize: 16),
                      ),
                    )
                  : ListView.separated(
                      itemCount: _conversations.length,
                      separatorBuilder: (context, index) =>
                          const Divider(height: 1),
                      itemBuilder: (context, index) {
                        final conv = _conversations[index];
                        final peer = conv.peer;
                        final peerAvatar = peer.avatar.isNotEmpty
                            ? DioClient.getMediaUrl(peer.avatar)
                            : '';
                        final latestMsg = conv.latestMessage;

                        return ListTile(
                          leading: CircleAvatar(
                            radius: 22,
                            backgroundColor: isDark
                                ? AppColors.darkBorder
                                : AppColors.lightBorder,
                            backgroundImage: peerAvatar.isNotEmpty
                                ? CachedNetworkImageProvider(peerAvatar)
                                : null,
                            child: peerAvatar.isEmpty
                                ? const Icon(Icons.person, color: Colors.grey)
                                : null,
                          ),
                          title: Text(
                            peer.username,
                            style: const TextStyle(fontWeight: FontWeight.bold),
                          ),
                          subtitle: Text(
                            latestMsg?.content ?? '没有新消息',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              color: isDark
                                  ? AppColors.darkTextSecondary
                                  : AppColors.lightTextSecondary,
                            ),
                          ),
                          trailing: Column(
                            mainAxisAlignment: MainAxisAlignment.center,
                            crossAxisAlignment: CrossAxisAlignment.end,
                            children: [
                              if (latestMsg != null)
                                Text(
                                  DateFormatter.formatRelative(
                                    latestMsg.createdAt,
                                  ),
                                  style: const TextStyle(
                                    color: Colors.grey,
                                    fontSize: 12,
                                  ),
                                ),
                              const SizedBox(height: 4),
                              if (conv.unreadCount > 0)
                                Container(
                                  padding: const EdgeInsets.symmetric(
                                    horizontal: 6,
                                    vertical: 2,
                                  ),
                                  decoration: const BoxDecoration(
                                    color: AppColors.primary,
                                    shape: BoxShape.circle,
                                  ),
                                  child: Text(
                                    conv.unreadCount.toString(),
                                    style: const TextStyle(
                                      color: Colors.white,
                                      fontSize: 10,
                                      fontWeight: FontWeight.bold,
                                    ),
                                  ),
                                ),
                            ],
                          ),
                          onTap: () {
                            // Open Chat Room
                            context.push('/chat/${conv.peerId}').then((_) {
                              if (mounted) _fetchConversations();
                            });
                          },
                        );
                      },
                    ),
            ),
          ),
        ],
      ),
    );
  }
}
