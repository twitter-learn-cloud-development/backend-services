import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../domain/message_model.dart';
import 'chat_list_screen.dart';
import '../../auth/presentation/auth_notifier.dart';
import '../../../core/constants/colors.dart';
import '../../../core/network/websocket_service.dart';
import '../../../core/utils/date_formatter.dart';
import 'message_history.dart';

class ChatRoomScreen extends ConsumerStatefulWidget {
  final String userId;

  const ChatRoomScreen({super.key, required this.userId});

  @override
  ConsumerState<ChatRoomScreen> createState() => _ChatRoomScreenState();
}

class _ChatRoomScreenState extends ConsumerState<ChatRoomScreen>
    with WidgetsBindingObserver {
  late MessageHistory _history;
  List<MessageModel> get _messages => _history.items;
  bool get _isLoading => _history.busy;
  bool _isSending = false;
  bool get _hasMore => _history.hasMore;

  final _messageController = TextEditingController();
  final ScrollController _scrollController = ScrollController();
  StreamSubscription? _messageSubscription;
  StreamSubscription? _readySubscription;
  Timer? _syncTimer, _pollTimer;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _openHistory();
    _scrollController.addListener(_onScroll);
    _pollTimer = Timer.periodic(
      const Duration(seconds: 30),
      (_) => _scheduleSync(),
    );
    final socket = ref.read(websocketServiceProvider);
    _readySubscription = socket.readyStream.listen((_) => _scheduleSync());
    socket.connect();

    // Listen to real-time messages via WebSocket
    _messageSubscription = ref
        .read(websocketServiceProvider)
        .messageStream
        .listen((event) {
          if (!mounted || event['data'] is! Map<String, dynamic>) return;
          final msgData = event['data'] as Map<String, dynamic>;
          final msg = MessageModel.fromJson(msgData);

          final authState = ref.read(authNotifierProvider);
          final currentUserId = authState.user?.id ?? '';

          // Determine if message belongs to this conversation
          final isRelevant =
              (msg.senderId == widget.userId &&
                  msg.receiverId == currentUserId) ||
              (msg.senderId == currentUserId &&
                  msg.receiverId == widget.userId);

          if (isRelevant) {
            try {
              _history.push(msg);
            } catch (_) {
              /* Invalid hint is recovered via HTTP. */
            }
            _scheduleSync();
          }
        });
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _history.dispose();
    _readySubscription?.cancel();
    _syncTimer?.cancel();
    _pollTimer?.cancel();
    _messageSubscription?.cancel();
    _messageController.dispose();
    _scrollController.removeListener(_onScroll);
    _scrollController.dispose();
    super.dispose();
  }

  @override
  void didUpdateWidget(covariant ChatRoomScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.userId != widget.userId) {
      _history.dispose();
      _isSending = false;
      _messageController.clear();
      _openHistory();
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) _scheduleSync();
  }

  void _scheduleSync() {
    if (!mounted ||
        (WidgetsBinding.instance.lifecycleState != null &&
            WidgetsBinding.instance.lifecycleState !=
                AppLifecycleState.resumed)) {
      return;
    }
    _syncTimer ??= Timer(const Duration(milliseconds: 250), () {
      _syncTimer = null;
      if (mounted) _history.recover();
    });
  }

  void _openHistory() {
    final peer = widget.userId, user = ref.read(authNotifierProvider).user?.id;
    final epoch = ref.read(authNotifierProvider.notifier).sessionRevision;
    final repo = ref.read(chatRepositoryProvider);
    _history = MessageHistory(
      fetch: (cursor) async {
        final page = await repo.getMessages(peer, cursor: cursor);
        if (!mounted ||
            epoch != ref.read(authNotifierProvider.notifier).sessionRevision) {
          throw StateError('stale identity');
        }
        return page;
      },
      accepts: (row) =>
          (row.senderId == peer && row.receiverId == user) ||
          (row.senderId == user && row.receiverId == peer),
      notify: () {
        if (mounted) setState(() {});
      },
    );
    _history.older();
  }

  void _onScroll() {
    if (_scrollController.position.pixels >=
        _scrollController.position.maxScrollExtent * 0.9) {
      _fetchNextPage();
    }
  }

  Future<void> _fetchNextPage() async {
    await _history.older();
  }

  Future<void> _sendMessage() async {
    final text = _messageController.text.trim();
    if (text.isEmpty || _isSending) return;

    final history = _history,
        epoch = ref.read(authNotifierProvider.notifier).sessionRevision;
    setState(() {
      _isSending = true;
    });

    try {
      final repo = ref.read(chatRepositoryProvider);
      final newMsg = await repo.sendMessage(widget.userId, text);
      if (!mounted ||
          history != _history ||
          epoch != ref.read(authNotifierProvider.notifier).sessionRevision) {
        return;
      }
      history.push(newMsg);
      if (_messageController.text.trim() == text) _messageController.clear();
      setState(() {
        _isSending = false;
      });
    } catch (e) {
      if (!mounted ||
          history != _history ||
          epoch != ref.read(authNotifierProvider.notifier).sessionRevision) {
        return;
      }
      setState(() {
        _isSending = false;
      });
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('发送结果未确定，草稿已保留。先同步历史再决定是否重发，重发可能重复。')),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final authState = ref.watch(authNotifierProvider);
    final currentUserId = authState.user?.id ?? '';

    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    return Scaffold(
      backgroundColor: isDark ? AppColors.darkBg : AppColors.lightBg,
      appBar: AppBar(title: const Text('聊天室')),
      body: Column(
        children: [
          if (_history.error != null)
            Text(_history.error!, style: const TextStyle(color: Colors.red)),
          TextButton(
            onPressed: _history.syncing || _history.busy
                ? null
                : _history.recover,
            child: Text(
              _history.syncing
                  ? '同步中…'
                  : _history.continuation
                  ? '继续同步未读区间'
                  : '同步最新消息',
            ),
          ),
          Expanded(
            child: _isLoading && _messages.isEmpty
                ? const Center(
                    child: CircularProgressIndicator(color: AppColors.primary),
                  )
                : _messages.isEmpty
                ? const Center(
                    child: Text(
                      '在此开始你们的私密对话。',
                      style: TextStyle(color: Colors.grey, fontSize: 15),
                    ),
                  )
                : ListView.builder(
                    controller: _scrollController,
                    reverse: true, // Show latest messages at the bottom
                    padding: const EdgeInsets.symmetric(
                      horizontal: 16,
                      vertical: 12,
                    ),
                    itemCount: _messages.length + 1,
                    itemBuilder: (context, index) {
                      if (index == _messages.length) {
                        return _hasMore
                            ? TextButton(
                                onPressed: _fetchNextPage,
                                child: const Text('加载更早消息'),
                              )
                            : const SizedBox(height: 20);
                      }

                      final msg = _messages[index];
                      final isMe = msg.senderId == currentUserId;

                      return _buildMessageBubble(msg, isMe, isDark, theme);
                    },
                  ),
          ),

          // Send Input Area
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
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
                Expanded(
                  child: TextField(
                    controller: _messageController,
                    maxLines: null,
                    decoration: const InputDecoration(
                      hintText: '发送私信...',
                      hintStyle: TextStyle(fontSize: 14),
                      contentPadding: EdgeInsets.symmetric(
                        horizontal: 16,
                        vertical: 10,
                      ),
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                IconButton(
                  icon: const Icon(Icons.send, color: AppColors.primary),
                  onPressed: _sendMessage,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildMessageBubble(
    MessageModel msg,
    bool isMe,
    bool isDark,
    ThemeData theme,
  ) {
    return Align(
      alignment: isMe ? Alignment.centerRight : Alignment.centerLeft,
      child: Container(
        margin: const EdgeInsets.symmetric(vertical: 4),
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
        constraints: BoxConstraints(
          maxWidth: MediaQuery.of(context).size.width * 0.7,
        ),
        decoration: BoxDecoration(
          color: isMe
              ? AppColors.primary
              : (isDark ? AppColors.darkSurface : AppColors.lightSurface),
          borderRadius: BorderRadius.only(
            topLeft: const Radius.circular(16),
            topRight: const Radius.circular(16),
            bottomLeft: isMe ? const Radius.circular(16) : Radius.zero,
            bottomRight: isMe ? Radius.zero : const Radius.circular(16),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              msg.content,
              style: TextStyle(
                color: isMe
                    ? Colors.white
                    : (isDark ? Colors.white : Colors.black),
                fontSize: 15,
              ),
            ),
            const SizedBox(height: 4),
            Align(
              alignment: Alignment.bottomRight,
              child: Text(
                DateFormatter.formatRelative(msg.createdAt),
                style: TextStyle(
                  color: isMe
                      ? Colors.white.withValues(alpha: 0.7)
                      : Colors.grey,
                  fontSize: 10,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
