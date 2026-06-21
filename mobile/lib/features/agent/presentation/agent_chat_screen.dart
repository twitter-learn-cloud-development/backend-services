import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../data/agent_repository.dart';
import '../../auth/presentation/auth_notifier.dart';
import '../../../core/constants/colors.dart';
import '../../tweet/domain/tweet_model.dart';
import '../../tweet/presentation/feed_notifier.dart';

final agentRepositoryProvider = Provider<AgentRepository>((ref) {
  final dio = ref.watch(dioProvider);
  return AgentRepository(dio);
});

class AgentChatScreen extends ConsumerStatefulWidget {
  const AgentChatScreen({super.key});

  @override
  ConsumerState<AgentChatScreen> createState() => _AgentChatScreenState();
}

class _AgentChatScreenState extends ConsumerState<AgentChatScreen> {
  List<DialogueSession> _sessions = [];
  bool _isLoading = true;

  @override
  void initState() {
    super.initState();
    _fetchSessions();
  }

  Future<void> _fetchSessions() async {
    try {
      final repo = ref.read(agentRepositoryProvider);
      final list = await repo.getDialogues();
      setState(() {
        _sessions = list;
        _isLoading = false;
      });
    } catch (_) {
      setState(() {
        _isLoading = false;
      });
    }
  }

  void _openChat(DialogueSession? session) {
    Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => DialogueChatScreen(session: session),
      ),
    ).then((_) => _fetchSessions()); // Refresh list when returning
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    return Scaffold(
      backgroundColor: isDark ? AppColors.darkBg : AppColors.lightBg,
      appBar: AppBar(
        title: const Text('AI 智能体助手'),
        centerTitle: true,
      ),
      body: _isLoading
          ? const Center(child: CircularProgressIndicator(color: AppColors.primary))
          : Column(
              children: [
                // Top prompt to create a new session
                Padding(
                  padding: const EdgeInsets.all(16.0),
                  child: SizedBox(
                    width: double.infinity,
                    height: 50,
                    child: ElevatedButton.icon(
                      onPressed: () => _openChat(null),
                      icon: const Icon(Icons.add, color: Colors.white),
                      label: const Text('新建 AI 智能对话', style: TextStyle(fontWeight: FontWeight.bold)),
                      style: ElevatedButton.styleFrom(
                        backgroundColor: AppColors.primary,
                        foregroundColor: Colors.white,
                        shape: RoundedRectangleBorder(
                          borderRadius: BorderRadius.circular(25),
                        ),
                      ),
                    ),
                  ),
                ),
                
                // History sessions list
                Expanded(
                  child: _sessions.isEmpty
                      ? const Center(
                          child: Text(
                            '没有历史对话，开启你的第一轮探索吧！',
                            style: TextStyle(color: Colors.grey),
                          ),
                        )
                      : ListView.separated(
                          itemCount: _sessions.length,
                          separatorBuilder: (context, index) => const Divider(height: 1),
                          itemBuilder: (context, index) {
                            final session = _sessions[index];
                            return ListTile(
                              leading: const CircleAvatar(
                                backgroundColor: AppColors.primary,
                                child: Icon(Icons.smart_toy, color: Colors.white),
                              ),
                              title: Text(
                                session.title,
                                style: const TextStyle(fontWeight: FontWeight.bold),
                              ),
                              subtitle: const Text('点击继续研讨对话'),
                              trailing: const Icon(Icons.chevron_right),
                              onTap: () => _openChat(session),
                            );
                          },
                        ),
                ),
              ],
            ),
    );
  }
}

// Sub-screen for active Dialogue Session
class DialogueChatScreen extends ConsumerStatefulWidget {
  final DialogueSession? session;

  const DialogueChatScreen({super.key, this.session});

  @override
  ConsumerState<DialogueChatScreen> createState() => _DialogueChatScreenState();
}

class _DialogueChatScreenState extends ConsumerState<DialogueChatScreen> {
  late String _dialogueId;
  final List<DialogueMessage> _messages = [];
  List<AgentModel> _models = [];
  AgentModel? _selectedModel;
  
  // 0: Chat (Direct), 1: Consult (RAG), 2: Assist (Draft Post)
  int _selectedMode = 0;
  bool _isLoadingMessages = false;
  bool _isSending = false;
  
  final _messageController = TextEditingController();
  final ScrollController _scrollController = ScrollController();

  @override
  void initState() {
    super.initState();
    _dialogueId = widget.session?.id ?? '';
    _fetchModelsAndHistory();
  }

  @override
  void dispose() {
    _messageController.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  Future<void> _fetchModelsAndHistory() async {
    setState(() {
      _isLoadingMessages = true;
    });

    try {
      final repo = ref.read(agentRepositoryProvider);
      
      // Load AI models
      final modelList = await repo.getModels();
      _models = modelList;
      if (_models.isNotEmpty) {
        _selectedModel = _models.first;
      }

      // Load history messages if session is not new
      if (_dialogueId.isNotEmpty) {
        final msgs = await repo.getDialogueMessages(_dialogueId);
        _messages.addAll(msgs);
      }
    } catch (_) {}

    setState(() {
      _isLoadingMessages = false;
    });
  }

  Future<void> _sendMessage() async {
    final query = _messageController.text.trim();
    if (query.isEmpty || _isSending || _selectedModel == null) return;

    _messageController.clear();
    
    // Add user message locally
    final userMsgIdx = _messages.length;
    setState(() {
      _messages.add(DialogueMessage(
        id: 'temp_$userMsgIdx',
        question: query,
        response: '思考中...',
      ));
      _isSending = true;
    });

    // Scroll to bottom
    Future.delayed(const Duration(milliseconds: 100), () {
      if (_scrollController.hasClients) {
        _scrollController.animateTo(
          _scrollController.position.maxScrollExtent,
          duration: const Duration(milliseconds: 300),
          curve: Curves.easeOut,
        );
      }
    });

    try {
      final repo = ref.read(agentRepositoryProvider);
      Map<String, dynamic> result;
      
      // Select endpoint based on Mode
      if (_selectedMode == 1) {
        result = await repo.consultSemantic(
          content: query,
          dialogueId: _dialogueId,
          modelId: _selectedModel!.id,
        );
      } else if (_selectedMode == 2) {
        result = await repo.assistDraft(
          content: query,
          dialogueId: _dialogueId,
          modelId: _selectedModel!.id,
        );
      } else {
        result = await repo.chatDirect(
          content: query,
          dialogueId: _dialogueId,
          modelId: _selectedModel!.id,
        );
      }

      // Parse dialogueId if starting a new session
      if (_dialogueId.isEmpty && result['dialogue_id'] != null) {
        _dialogueId = result['dialogue_id'].toString();
      }

      final aiResponse = result['response']?.toString() ?? '';
      
      // Attach special payload lists (tweets or summaries) if present
      List<dynamic>? attachment;
      if (result['tweet_list'] != null) {
        attachment = result['tweet_list'] as List;
      }

      setState(() {
        _messages[userMsgIdx] = DialogueMessage(
          id: DateTime.now().millisecondsSinceEpoch.toString(),
          question: query,
          response: aiResponse,
          attachedTweets: attachment,
        );
        _isSending = false;
      });
    } catch (e) {
      setState(() {
        _messages[userMsgIdx] = DialogueMessage(
          id: 'error_$userMsgIdx',
          question: query,
          response: 'AI 发生故障，请检查连接: $e',
        );
        _isSending = false;
      });
    }

    Future.delayed(const Duration(milliseconds: 100), () {
      if (_scrollController.hasClients) {
        _scrollController.animateTo(
          _scrollController.position.maxScrollExtent,
          duration: const Duration(milliseconds: 300),
          curve: Curves.easeOut,
        );
      }
    });
  }

  Future<void> _confirmPublishDraft(String draftContent, int msgIdx) async {
    try {
      final repo = ref.read(agentRepositoryProvider);
      final tweetId = await repo.confirmPublish(draftContent);
      
      if (tweetId.isNotEmpty) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('智能发帖成功！可在主页查看。'),
            backgroundColor: AppColors.retweetColor,
          ),
        );
        
        // Refresh feeds in background
        ref.read(feedNotifierProvider.notifier).refresh();

        setState(() {
          // Replace message with success status
          final oldMsg = _messages[msgIdx];
          _messages[msgIdx] = DialogueMessage(
            id: oldMsg.id,
            question: oldMsg.question,
            response: '${oldMsg.response}\n\n✅ 已发布成功！推文 ID: $tweetId',
          );
        });
      }
    } catch (e) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('发布失败: $e'), backgroundColor: AppColors.likeColor),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    return Scaffold(
      backgroundColor: isDark ? AppColors.darkBg : AppColors.lightBg,
      appBar: AppBar(
        title: const Text('AI 专家研讨室'),
        actions: [
          // Model selection dropdown
          if (_models.isNotEmpty)
            Container(
              margin: const EdgeInsets.only(right: 8),
              padding: const EdgeInsets.symmetric(horizontal: 10),
              decoration: BoxDecoration(
                borderRadius: BorderRadius.circular(15),
                border: Border.all(color: Colors.grey.withOpacity(0.5)),
              ),
              child: DropdownButton<AgentModel>(
                value: _selectedModel,
                underline: const SizedBox(),
                items: _models.map((m) {
                  return DropdownMenuItem(
                    value: m,
                    child: Text(m.name, style: const TextStyle(fontSize: 13)),
                  );
                }).toList(),
                onChanged: (val) {
                  setState(() {
                    _selectedModel = val;
                  });
                },
              ),
            ),
        ],
      ),
      body: Column(
        children: [
          // Mode select chips
          _buildModeSelector(isDark),
          
          const Divider(height: 1),
          
          // Chat flow stream
          Expanded(
            child: _isLoadingMessages
                ? const Center(child: CircularProgressIndicator(color: AppColors.primary))
                : _messages.isEmpty
                    ? const Center(child: Text('输入你的研讨问题，AI 将提供专业解答。', style: TextStyle(color: Colors.grey)))
                    : ListView.builder(
                        controller: _scrollController,
                        padding: const EdgeInsets.all(16),
                        itemCount: _messages.length,
                        itemBuilder: (context, index) {
                          final msg = _messages[index];
                          return Column(
                            crossAxisAlignment: CrossAxisAlignment.stretch,
                            children: [
                              // User Question (Right aligned bubble)
                              _buildUserBubble(msg.question, isDark),
                              
                              const SizedBox(height: 12),
                              
                              // AI Answer (Left aligned bubble)
                              _buildAIBubble(msg, index, isDark),
                              
                              const SizedBox(height: 24),
                            ],
                          );
                        },
                      ),
          ),
          
          const Divider(height: 1),
          
          // Input bar
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
            color: isDark ? AppColors.darkBg : AppColors.lightBg,
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _messageController,
                    maxLines: null,
                    decoration: const InputDecoration(
                      hintText: '向 AI 发问...',
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

  Widget _buildModeSelector(bool isDark) {
    return Container(
      height: 50,
      padding: const EdgeInsets.symmetric(horizontal: 16),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceEvenly,
        children: [
          _buildModeChip(0, '直接对话', Icons.chat),
          _buildModeChip(1, '语义搜索', Icons.travel_explore),
          _buildModeChip(2, '智能发帖', Icons.draw),
        ],
      ),
    );
  }

  Widget _buildModeChip(int index, String label, IconData icon) {
    final active = _selectedMode == index;
    return ChoiceChip(
      iconTheme: IconThemeData(color: active ? Colors.white : Colors.grey, size: 16),
      avatar: Icon(icon),
      label: Text(label),
      selected: active,
      selectedColor: AppColors.primary,
      onSelected: (selected) {
        if (selected) {
          setState(() {
            _selectedMode = index;
          });
        }
      },
    );
  }

  Widget _buildUserBubble(String text, bool isDark) {
    return Align(
      alignment: Alignment.centerRight,
      child: Container(
        constraints: BoxConstraints(
          maxWidth: MediaQuery.of(context).size.width * 0.75,
        ),
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        decoration: BoxDecoration(
          color: AppColors.primary,
          borderRadius: const BorderRadius.only(
            topLeft: Radius.circular(16),
            topRight: Radius.circular(16),
            bottomLeft: Radius.circular(16),
          ),
        ),
        child: Text(
          text,
          style: const TextStyle(color: Colors.white, fontSize: 15),
        ),
      ),
    );
  }

  Widget _buildAIBubble(DialogueMessage msg, int msgIdx, bool isDark) {
    final hasAttachments = msg.attachedTweets != null && msg.attachedTweets!.isNotEmpty;
    
    return Align(
      alignment: Alignment.centerLeft,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            constraints: BoxConstraints(
              maxWidth: MediaQuery.of(context).size.width * 0.8,
            ),
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
            decoration: BoxDecoration(
              color: isDark ? AppColors.darkSurface : AppColors.lightSurface,
              borderRadius: const BorderRadius.only(
                topLeft: Radius.circular(16),
                topRight: Radius.circular(16),
                bottomRight: Radius.circular(16),
              ),
            ),
            child: Text(
              msg.response,
              style: TextStyle(
                color: isDark ? Colors.white : Colors.black,
                fontSize: 15,
                height: 1.3,
              ),
            ),
          ),
          
          // RAG / Draft List rendering
          if (hasAttachments) ...[
            const SizedBox(height: 8),
            SizedBox(
              height: 110,
              width: MediaQuery.of(context).size.width * 0.85,
              child: ListView.builder(
                scrollDirection: Axis.horizontal,
                itemCount: msg.attachedTweets!.length,
                itemBuilder: (context, idx) {
                  final item = msg.attachedTweets![idx] as Map<String, dynamic>;
                  final isDraft = item['content'] != null; // Draft Tweet contains full fields

                  if (isDraft) {
                    final draftContent = item['content']?.toString() ?? '';
                    return _buildDraftCard(draftContent, msgIdx, isDark);
                  } else {
                    // RAG consult item (tweet_id, summary, url)
                    final summary = item['summary']?.toString() ?? '';
                    final tweetId = item['tweet_id']?.toString() ?? '';
                    return _buildRAGCard(summary, tweetId, isDark);
                  }
                },
              ),
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildRAGCard(String summary, String tweetId, bool isDark) {
    return Container(
      width: 250,
      margin: const EdgeInsets.only(right: 10),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: isDark ? AppColors.darkSurface.withOpacity(0.5) : AppColors.lightSurface,
        border: Border.all(color: Colors.grey.withOpacity(0.3)),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Row(
            children: [
              Icon(Icons.search, size: 14, color: AppColors.primary),
              SizedBox(width: 4),
              Text('召回匹配推文', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: AppColors.primary)),
            ],
          ),
          const SizedBox(height: 4),
          Expanded(
            child: Text(
              summary,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontSize: 12),
            ),
          ),
          GestureDetector(
            onTap: () => context.push('/tweet/$tweetId'),
            child: const Text(
              '查看原推文 ➔',
              style: TextStyle(color: AppColors.primary, fontSize: 11, fontWeight: FontWeight.bold),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildDraftCard(String draftContent, int msgIdx, bool isDark) {
    return Container(
      width: 280,
      margin: const EdgeInsets.only(right: 10),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: isDark ? AppColors.darkSurface.withOpacity(0.5) : AppColors.lightSurface,
        border: Border.all(color: Colors.grey.withOpacity(0.3)),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              const Row(
                children: [
                  Icon(Icons.edit_note, size: 16, color: AppColors.primary),
                  SizedBox(width: 4),
                  Text('AI 生成推文草稿', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: AppColors.primary)),
                ],
              ),
              ElevatedButton(
                onPressed: () => _confirmPublishDraft(draftContent, msgIdx),
                style: ElevatedButton.styleFrom(
                  backgroundColor: AppColors.primary,
                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                  minimumSize: Size.zero,
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
                ),
                child: const Text('一键发布', style: TextStyle(color: Colors.white, fontSize: 11, fontWeight: FontWeight.bold)),
              ),
            ],
          ),
          const SizedBox(height: 6),
          Expanded(
            child: Text(
              draftContent,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontSize: 12),
            ),
          ),
        ],
      ),
    );
  }
}
