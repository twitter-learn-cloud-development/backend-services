import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:cached_network_image/cached_network_image.dart';
import '../domain/message_model.dart';
import '../data/chat_repository.dart';
import 'chat_list_screen.dart';
import '../../auth/presentation/auth_notifier.dart';
import '../../../core/constants/colors.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/websocket_service.dart';
import '../../../core/utils/date_formatter.dart';

class ChatRoomScreen extends ConsumerStatefulWidget {
  final String userId;

  const ChatRoomScreen({super.key, required this.userId});

  @override
  ConsumerState<ChatRoomScreen> createState() => _ChatRoomScreenState();
}

class _ChatRoomScreenState extends ConsumerState<ChatRoomScreen> {
  final List<MessageModel> _messages = [];
  bool _isLoading = true;
  bool _isSending = false;
  String _cursor = '';
  bool _hasMore = false;
  
  final _messageController = TextEditingController();
  final ScrollController _scrollController = ScrollController();
  StreamSubscription? _messageSubscription;

  @override
  void initState() {
    super.initState();
    _fetchHistory();
    _scrollController.addListener(_onScroll);
    
    // Listen to real-time messages via WebSocket
    _messageSubscription = ref.read(websocketServiceProvider).messageStream.listen((event) {
      final msgData = event['data'] as Map<String, dynamic>;
      final msg = MessageModel.fromJson(msgData);
      
      final authState = ref.read(authNotifierProvider);
      final currentUserId = authState.user?.id ?? '';
      
      // Determine if message belongs to this conversation
      final isRelevant = (msg.senderId == widget.userId && msg.receiverId == currentUserId) ||
                         (msg.senderId == currentUserId && msg.receiverId == widget.userId);
                         
      if (isRelevant) {
        setState(() {
          // Avoid duplicate entry if we sent the message and it was already inserted locally
          if (!_messages.any((m) => m.id == msg.id)) {
            _messages.insert(0, msg);
          }
        });
      }
    });
  }

  @override
  void dispose() {
    _messageSubscription?.cancel();
    _messageController.dispose();
    _scrollController.removeListener(_onScroll);
    _scrollController.dispose();
    super.dispose();
  }

  void _onScroll() {
    if (_scrollController.position.pixels >=
        _scrollController.position.maxScrollExtent * 0.9) {
      _fetchNextPage();
    }
  }

  Future<void> _fetchHistory() async {
    try {
      final repo = ref.read(chatRepositoryProvider);
      final result = await repo.getMessages(widget.userId);
      setState(() {
        _messages.addAll(result['messages'] as List<MessageModel>);
        _cursor = result['next_cursor'] as String;
        _hasMore = result['has_more'] as bool;
        _isLoading = false;
      });
    } catch (_) {
      setState(() {
        _isLoading = false;
      });
    }
  }

  Future<void> _fetchNextPage() async {
    if (!_hasMore || _isLoading) return;

    try {
      final repo = ref.read(chatRepositoryProvider);
      final result = await repo.getMessages(widget.userId, cursor: _cursor);
      setState(() {
        _messages.addAll(result['messages'] as List<MessageModel>);
        _cursor = result['next_cursor'] as String;
        _hasMore = result['has_more'] as bool;
      });
    } catch (_) {}
  }

  Future<void> _sendMessage() async {
    final text = _messageController.text.trim();
    if (text.isEmpty || _isSending) return;

    _messageController.clear();
    setState(() {
      _isSending = true;
    });

    try {
      final repo = ref.read(chatRepositoryProvider);
      final newMsg = await repo.sendMessage(widget.userId, text);
      setState(() {
        // Since we render from bottom, we insert at the beginning
        _messages.insert(0, newMsg);
        _isSending = false;
      });
    } catch (e) {
      setState(() {
        _isSending = false;
      });
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('发送失败: $e')),
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
      appBar: AppBar(
        title: const Text('聊天室'),
      ),
      body: Column(
        children: [
          Expanded(
            child: _isLoading
                ? const Center(child: CircularProgressIndicator(color: AppColors.primary))
                : _messages.isEmpty
                    ? const Center(
                        child: Text('在此开始你们的私密对话。', style: TextStyle(color: Colors.grey, fontSize: 15)),
                      )
                    : ListView.builder(
                        controller: _scrollController,
                        reverse: true, // Show latest messages at the bottom
                        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                        itemCount: _messages.length + 1,
                        itemBuilder: (context, index) {
                          if (index == _messages.length) {
                            return _hasMore
                                ? const Padding(
                                    padding: EdgeInsets.symmetric(vertical: 10),
                                    child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
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
                      contentPadding: EdgeInsets.symmetric(horizontal: 16, vertical: 10),
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

  Widget _buildMessageBubble(MessageModel msg, bool isMe, bool isDark, ThemeData theme) {
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
                color: isMe ? Colors.white : (isDark ? Colors.white : Colors.black),
                fontSize: 15,
              ),
            ),
            const SizedBox(height: 4),
            Align(
              alignment: Alignment.bottomRight,
              child: Text(
                DateFormatter.formatRelative(msg.createdAt),
                style: TextStyle(
                  color: isMe ? Colors.white.withOpacity(0.7) : Colors.grey,
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
