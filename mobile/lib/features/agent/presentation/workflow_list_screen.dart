import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../data/agent_repository.dart';
import 'agent_chat_screen.dart';
import '../../../core/constants/colors.dart';
import '../../../core/utils/date_formatter.dart';

class WorkflowListScreen extends ConsumerStatefulWidget {
  const WorkflowListScreen({super.key});

  @override
  ConsumerState<WorkflowListScreen> createState() => _WorkflowListScreenState();
}

class _WorkflowListScreenState extends ConsumerState<WorkflowListScreen> {
  List<WorkflowSummary> _workflows = [];
  bool _isLoading = true;

  @override
  void initState() {
    super.initState();
    _fetchWorkflows();
  }

  Future<void> _fetchWorkflows() async {
    try {
      final repo = ref.read(agentRepositoryProvider);
      final list = await repo.listWorkflows(page: 1, pageSize: 50);
      setState(() {
        _workflows = list;
        _isLoading = false;
      });
    } catch (_) {
      setState(() {
        _isLoading = false;
      });
    }
  }

  void _runWorkflow(WorkflowSummary workflow) {
    // Show a dialog to collect input if any
    final inputController = TextEditingController(text: '{}');
    
    showDialog(
      context: context,
      builder: (context) {
        return AlertDialog(
          title: Text('执行工作流: ${workflow.name}'),
          content: TextField(
            controller: inputController,
            maxLines: 4,
            decoration: const InputDecoration(
              labelText: '输入参数 (JSON格式)',
              border: OutlineInputBorder(),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(context).pop(),
              child: const Text('取消'),
            ),
            ElevatedButton(
              onPressed: () async {
                Navigator.of(context).pop();
                
                // Show loading indicator
                showDialog(
                  context: context,
                  barrierDismissible: false,
                  builder: (_) => const Center(child: CircularProgressIndicator()),
                );
                
                try {
                  final repo = ref.read(agentRepositoryProvider);
                  final result = await repo.runWorkflow(workflow.workflowId, inputController.text);
                  
                  // Hide loading
                  Navigator.of(context).pop();
                  
                  // Show result
                  showDialog(
                    context: context,
                    builder: (context) => AlertDialog(
                      title: const Text('执行成功'),
                      content: SingleChildScrollView(
                        child: Text(result['response']?.toString() ?? '无输出'),
                      ),
                      actions: [
                        TextButton(
                          onPressed: () => Navigator.of(context).pop(),
                          child: const Text('关闭'),
                        ),
                      ],
                    ),
                  );
                } catch (e) {
                  // Hide loading
                  Navigator.of(context).pop();
                  
                  ScaffoldMessenger.of(context).showSnackBar(
                    SnackBar(content: Text('执行失败: $e'), backgroundColor: AppColors.likeColor),
                  );
                }
              },
              style: ElevatedButton.styleFrom(backgroundColor: AppColors.primary),
              child: const Text('运行', style: TextStyle(color: Colors.white)),
            ),
          ],
        );
      }
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    return Scaffold(
      backgroundColor: isDark ? AppColors.darkBg : AppColors.lightBg,
      appBar: AppBar(
        title: const Text('自定义工作流 (Workflow)'),
        centerTitle: true,
      ),
      body: _isLoading
          ? const Center(child: CircularProgressIndicator(color: AppColors.primary))
          : _workflows.isEmpty
              ? const Center(
                  child: Text('没有可用的工作流', style: TextStyle(color: Colors.grey)),
                )
              : ListView.builder(
                  padding: const EdgeInsets.all(16),
                  itemCount: _workflows.length,
                  itemBuilder: (context, index) {
                    final workflow = _workflows[index];
                    return Card(
                      color: isDark ? AppColors.darkSurface : AppColors.lightSurface,
                      margin: const EdgeInsets.only(bottom: 12),
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                      elevation: 0,
                      child: Padding(
                        padding: const EdgeInsets.all(16),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              mainAxisAlignment: MainAxisAlignment.spaceBetween,
                              children: [
                                Expanded(
                                  child: Text(
                                    workflow.name,
                                    style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 16),
                                  ),
                                ),
                                IconButton(
                                  icon: const Icon(Icons.play_circle_fill, color: AppColors.primary, size: 32),
                                  onPressed: () => _runWorkflow(workflow),
                                ),
                              ],
                            ),
                            const SizedBox(height: 8),
                            Text(
                              'ID: ${workflow.workflowId}',
                              style: TextStyle(color: Colors.grey[500], fontSize: 12),
                            ),
                            const SizedBox(height: 4),
                            Text(
                              '最近更新: ${DateFormatter.formatRelative(workflow.updatedAt)}',
                              style: TextStyle(color: Colors.grey[500], fontSize: 12),
                            ),
                          ],
                        ),
                      ),
                    );
                  },
                ),
    );
  }
}
