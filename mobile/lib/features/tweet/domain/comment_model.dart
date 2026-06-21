class CommentModel {
  final String id;
  final String userId;
  final String tweetId;
  final String content;
  final int createdAt;
  final String username;
  final String avatarUrl;

  CommentModel({
    required this.id,
    required this.userId,
    required this.tweetId,
    required this.content,
    required this.createdAt,
    required this.username,
    required this.avatarUrl,
  });

  factory CommentModel.fromJson(Map<String, dynamic> json) {
    // Check nested user info if present
    final userJson = json['user'] as Map<String, dynamic>?;
    final usernameVal = userJson != null ? (userJson['username'] ?? userJson['nickname'] ?? '') : (json['username'] ?? '');
    final avatarVal = userJson != null ? (userJson['avatar_url'] ?? userJson['avatar'] ?? '') : (json['avatar_url'] ?? '');

    return CommentModel(
      id: json['id']?.toString() ?? '',
      userId: json['user_id']?.toString() ?? '',
      tweetId: json['tweet_id']?.toString() ?? '',
      content: json['content'] ?? '',
      createdAt: json['created_at'] is int ? json['created_at'] : 0,
      username: usernameVal,
      avatarUrl: avatarVal,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'user_id': userId,
      'tweet_id': tweetId,
      'content': content,
      'created_at': createdAt,
      'user': {
        'username': username,
        'nickname': username,
        'avatar_url': avatarUrl,
      }
    };
  }
}
