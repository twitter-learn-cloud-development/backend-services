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

final chatRepositoryProvider = Provider<ChatRepository>((ref) {
  final dio = ref.watch(dioProvider);
  return ChatRepository(dio);
});

class ChatListScreen extends ConsumerStatefulWidget {
  const ChatListScreen({super.key});

  @override
  ConsumerState<ChatListScreen> createState() => _ChatListScreenState();
}

class _ChatListScreenState extends ConsumerState<ChatListScreen> {
  List<ConversationModel> _conversations = [];
  bool _isLoading = true;

  @override
  void initState() {
    super.initState();
    _fetchConversations();
  }

  Future<void> _fetchConversations() async {
    try {
      final repo = ref.read(chatRepositoryProvider);
      final result = await repo.getConversations();
      setState(() {
        _conversations = result['conversations'] as List<ConversationModel>;
        _isLoading = false;
      });
    } catch (_) {
      setState(() {
        _isLoading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    return Scaffold(
      backgroundColor: isDark ? AppColors.darkBg : AppColors.lightBg,
      appBar: AppBar(
        title: const Text('私信会话'),
        centerTitle: true,
      ),
      body: RefreshIndicator(
        onRefresh: _fetchConversations,
        color: AppColors.primary,
        child: _isLoading
            ? const Center(child: CircularProgressIndicator(color: AppColors.primary))
            : _conversations.isEmpty
                ? const Center(
                    child: Text('暂无私信对话', style: TextStyle(color: Colors.grey, fontSize: 16)),
                  )
                : ListView.separated(
                    itemCount: _conversations.length,
                    separatorBuilder: (context, index) => const Divider(height: 1),
                    itemBuilder: (context, index) {
                      final conv = _conversations[index];
                      final peer = conv.peer;
                      final peerAvatar = peer.avatar.isNotEmpty ? DioClient.getMediaUrl(peer.avatar) : '';
                      final latestMsg = conv.latestMessage;

                      return ListTile(
                        leading: CircleAvatar(
                          radius: 22,
                          backgroundColor: isDark ? AppColors.darkBorder : AppColors.lightBorder,
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
                            color: isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary,
                          ),
                        ),
                        trailing: Column(
                          mainAxisAlignment: MainAxisAlignment.center,
                          crossAxisAlignment: CrossAxisAlignment.end,
                          children: [
                            if (latestMsg != null)
                              Text(
                                DateFormatter.formatRelative(latestMsg.createdAt),
                                style: const TextStyle(color: Colors.grey, fontSize: 12),
                              ),
                            const SizedBox(height: 4),
                            if (conv.unreadCount > 0)
                              Container(
                                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                                decoration: const BoxDecoration(
                                  color: AppColors.primary,
                                  shape: BoxShape.circle,
                                ),
                                child: Text(
                                  conv.unreadCount.toString(),
                                  style: const TextStyle(color: Colors.white, fontSize: 10, fontWeight: FontWeight.bold),
                                ),
                              ),
                          ],
                        ),
                        onTap: () {
                          // Open Chat Room
                          context.push('/chat/${peer.id}').then((_) => _fetchConversations());
                        },
                      );
                    },
                  ),
      ),
    );
  }
}
