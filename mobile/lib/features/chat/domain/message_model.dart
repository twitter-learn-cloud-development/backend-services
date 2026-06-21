import '../../auth/domain/user_model.dart';

class MessageModel {
  final String id;
  final String senderId;
  final String receiverId;
  final String content;
  final int createdAt;
  final bool isRead;
  final User? sender;

  MessageModel({
    required this.id,
    required this.senderId,
    required this.receiverId,
    required this.content,
    required this.createdAt,
    required this.isRead,
    this.sender,
  });

  factory MessageModel.fromJson(Map<String, dynamic> json) {
    User? senderUser;
    if (json['sender'] != null) {
      senderUser = User.fromJson(json['sender']);
    }
    
    return MessageModel(
      id: json['id']?.toString() ?? '',
      senderId: json['sender_id']?.toString() ?? '',
      receiverId: json['receiver_id']?.toString() ?? '',
      content: json['content'] ?? '',
      createdAt: json['created_at'] is int ? json['created_at'] : 0,
      isRead: json['is_read'] ?? false,
      sender: senderUser,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'sender_id': senderId,
      'receiver_id': receiverId,
      'content': content,
      'created_at': createdAt,
      'is_read': isRead,
      'sender': sender?.toJson(),
    };
  }
}

class ConversationModel {
  final String peerId;
  final User peer;
  final MessageModel? latestMessage;
  final int unreadCount;

  ConversationModel({
    required this.peerId,
    required this.peer,
    this.latestMessage,
    this.unreadCount = 0,
  });

  factory ConversationModel.fromJson(Map<String, dynamic> json) {
    return ConversationModel(
      peerId: json['peer_id']?.toString() ?? '',
      peer: User.fromJson(json['peer'] ?? {}),
      latestMessage: json['latest_message'] != null
          ? MessageModel.fromJson(json['latest_message'])
          : null,
      unreadCount: json['unread_count'] is int ? json['unread_count'] : 0,
    );
  }
}
