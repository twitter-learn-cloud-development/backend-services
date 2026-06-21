import '../../auth/domain/user_model.dart';

class NotificationModel {
  final String id;
  final String type; // like, comment, follow
  final String targetId;
  final String content;
  final bool isRead;
  final int createdAt;
  final User? actor;

  NotificationModel({
    required this.id,
    required this.type,
    required this.targetId,
    required this.content,
    required this.isRead,
    required this.createdAt,
    this.actor,
  });

  factory NotificationModel.fromJson(Map<String, dynamic> json) {
    return NotificationModel(
      id: json['id']?.toString() ?? '',
      type: json['type'] ?? '',
      targetId: json['target_id']?.toString() ?? '',
      content: json['content'] ?? '',
      isRead: json['is_read'] ?? false,
      createdAt: json['created_at'] is int ? json['created_at'] : 0,
      actor: json['actor'] != null ? User.fromJson(json['actor']) : null,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'type': type,
      'target_id': targetId,
      'content': content,
      'is_read': isRead,
      'created_at': createdAt,
      'actor': actor?.toJson(),
    };
  }
}
