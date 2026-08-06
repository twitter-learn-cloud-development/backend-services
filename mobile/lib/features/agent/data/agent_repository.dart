import 'package:dio/dio.dart';
import '../../tweet/domain/tweet_model.dart';

class AgentModel {
  final int id;
  final String name;
  final String description;

  AgentModel({required this.id, required this.name, required this.description});

  factory AgentModel.fromJson(Map<String, dynamic> json) {
    return AgentModel(
      id: json['id'] is int ? json['id'] : 0,
      name: json['name']?.toString() ?? '',
      description: json['description']?.toString() ?? '',
    );
  }
}

class DialogueSession {
  final String id;
  final String title;

  DialogueSession({required this.id, required this.title});

  factory DialogueSession.fromJson(Map<String, dynamic> json) {
    return DialogueSession(
      id: json['id']?.toString() ?? '',
      title: json['title']?.toString() ?? '',
    );
  }
}

class DialogueMessage {
  final String id;
  final String question;
  final String response;
  final List<dynamic>? attachedTweets; // Can contain RAG summaries or draft Tweet objects

  DialogueMessage({
    required this.id,
    required this.question,
    required this.response,
    this.attachedTweets,
  });

  factory DialogueMessage.fromJson(Map<String, dynamic> json) {
    return DialogueMessage(
      id: json['id']?.toString() ?? '',
      question: json['question'] ?? '',
      response: json['response'] ?? '',
    );
  }
}

class WorkflowSummary {
  final String workflowId;
  final String userId;
  final String name;
  final int createdAt;
  final int updatedAt;

  WorkflowSummary({
    required this.workflowId,
    required this.userId,
    required this.name,
    required this.createdAt,
    required this.updatedAt,
  });

  factory WorkflowSummary.fromJson(Map<String, dynamic> json) {
    return WorkflowSummary(
      workflowId: json['workflow_id']?.toString() ?? '',
      userId: json['user_id']?.toString() ?? '',
      name: json['name']?.toString() ?? '',
      createdAt: json['created_at'] is int ? json['created_at'] : 0,
      updatedAt: json['updated_at'] is int ? json['updated_at'] : 0,
    );
  }
}

class AgentRepository {
  final Dio _dio;

  AgentRepository(this._dio);

  // Fetch available AI models
  Future<List<AgentModel>> getModels() async {
    try {
      final response = await _dio.get('/agent/models');
      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        final list = data['model_kind_list'] as List? ?? [];
        return list.map((e) => AgentModel.fromJson(e)).toList();
      }
      return [];
    } catch (_) {
      return [];
    }
  }

  // Fetch dialogue history list
  Future<List<DialogueSession>> getDialogues() async {
    try {
      final response = await _dio.get('/agent/dialogues');
      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        final list = data['repository_dialogue_list'] as List? ?? [];
        return list.map((e) => DialogueSession.fromJson(e)).toList();
      }
      return [];
    } catch (_) {
      return [];
    }
  }

  // Fetch dialogue messages
  Future<List<DialogueMessage>> getDialogueMessages(String id) async {
    try {
      final response = await _dio.get('/agent/dialogues/$id/messages');
      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        final list = data['messages'] as List? ?? [];
        return list.map((e) => DialogueMessage.fromJson(e)).toList();
      }
      return [];
    } catch (_) {
      return [];
    }
  }

  // Mode 1: Direct Chat
  Future<Map<String, dynamic>> chatDirect({
    required String content,
    required String dialogueId,
    required int modelId,
  }) async {
    try {
      final response = await _dio.post('/agent/chat', data: {
        'content': content,
        'dialogue_id': dialogueId,
        'model_kind_id': modelId,
      });
      if (response.statusCode == 200) {
        return response.data as Map<String, dynamic>;
      }
      throw Exception('发送失败');
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络异常');
    }
  }

  // Mode 2: Semantic search (Consult)
  Future<Map<String, dynamic>> consultSemantic({
    required String content,
    required String dialogueId,
    required int modelId,
  }) async {
    try {
      final response = await _dio.post('/agent/consult', data: {
        'content': content,
        'dialogue_id': dialogueId,
        'model_kind_id': modelId,
      });
      if (response.statusCode == 200) {
        return response.data as Map<String, dynamic>;
      }
      throw Exception('发送失败');
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络异常');
    }
  }

  // Mode 3: Collaborative Draft assist
  Future<Map<String, dynamic>> assistDraft({
    required String content,
    required String dialogueId,
    required int modelId,
  }) async {
    try {
      final response = await _dio.post('/agent/assist', data: {
        'content': content,
        'dialogue_id': dialogueId,
        'model_kind_id': modelId,
      });
      if (response.statusCode == 200) {
        return response.data as Map<String, dynamic>;
      }
      throw Exception('发送失败');
    } on DioException catch (e) {
      throw Exception(e.response?.data?['error'] ?? '网络异常');
    }
  }

  // Mode 3 - Phase 2: Confirm publish
  Future<String> confirmPublish(String content) async {
    try {
      final response = await _dio.post('/agent/confirm', data: {
        'content': content,
      });
      if (response.statusCode == 200) {
        final respText = response.data['response']?.toString() ?? '';
        if (respText.contains('失败') || respText.contains('error') || respText.contains('Error')) {
          throw Exception(respText);
        }
        return respText;
      }
      throw Exception('确认发布失败');
    } on DioException catch (e) {
      final errorMsg = e.response?.data?['error'] ?? e.message ?? '网络连接错误';
      throw Exception(errorMsg);
    } catch (e) {
      rethrow;
    }
  }

  // Workflows: List workflows
  Future<List<WorkflowSummary>> listWorkflows({int page = 1, int pageSize = 20}) async {
    try {
      final response = await _dio.get('/agent/workflows', queryParameters: {
        'page': page,
        'page_size': pageSize,
      });
      if (response.statusCode == 200) {
        final data = response.data as Map<String, dynamic>;
        final list = data['workflows'] as List? ?? [];
        return list.map((e) => WorkflowSummary.fromJson(e)).toList();
      }
      return [];
    } catch (_) {
      return [];
    }
  }

  // Workflows: Run a workflow
  Future<Map<String, dynamic>> runWorkflow(String workflowId, String inputJson) async {
    try {
      final response = await _dio.post('/agent/workflows/$workflowId/run', data: {
        'input_json': inputJson,
      });
      if (response.statusCode == 200) {
        return response.data as Map<String, dynamic>;
      }
      throw Exception('执行工作流失败');
    } on DioException catch (e) {
      final errorMsg = e.response?.data?['error'] ?? e.message ?? '网络连接错误';
      throw Exception(errorMsg);
    } catch (e) {
      rethrow;
    }
  }
}
