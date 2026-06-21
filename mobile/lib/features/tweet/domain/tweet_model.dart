import '../../auth/domain/user_model.dart';

class Tweet {
  final String id;
  final String userId;
  final String content;
  final List<String> mediaUrls;
  final int type;
  final int visibleType;
  final int likeCount;
  final int commentCount;
  final int retweetCount;
  final bool isLiked;
  final bool isRetweeted;
  final bool isBookmarked;
  final int createdAt;
  final int updatedAt;
  final User? user; // Author information

  Tweet({
    required this.id,
    required this.userId,
    required this.content,
    required this.mediaUrls,
    this.type = 0,
    this.visibleType = 0,
    this.likeCount = 0,
    this.commentCount = 0,
    this.retweetCount = 0,
    this.isLiked = false,
    this.isRetweeted = false,
    this.isBookmarked = false,
    required this.createdAt,
    required this.updatedAt,
    this.user,
  });

  factory Tweet.fromJson(Map<String, dynamic> json) {
    // Media URLs list conversion
    final List<String> urls = [];
    if (json['media_urls'] != null) {
      for (var url in json['media_urls']) {
        urls.add(url.toString());
      }
    }

    // Embed author user safely
    User? embeddedUser;
    if (json['user'] != null) {
      embeddedUser = User.fromJson(json['user']);
    }

    return Tweet(
      id: json['id']?.toString() ?? '',
      userId: json['user_id']?.toString() ?? '',
      content: json['content'] ?? '',
      mediaUrls: urls,
      type: json['type'] is int ? json['type'] : 0,
      visibleType: json['visible_type'] is int ? json['visible_type'] : 0,
      likeCount: json['like_count'] is int ? json['like_count'] : 0,
      commentCount: json['comment_count'] is int ? json['comment_count'] : 0,
      retweetCount: json['retweet_count'] is int 
          ? json['retweet_count'] 
          : (json['share_count'] is int ? json['share_count'] : 0),
      isLiked: json['is_liked'] ?? false,
      isRetweeted: json['is_retweeted'] ?? false,
      isBookmarked: json['is_bookmarked'] ?? false,
      createdAt: json['created_at'] is int ? json['created_at'] : 0,
      updatedAt: json['updated_at'] is int ? json['updated_at'] : 0,
      user: embeddedUser,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'user_id': userId,
      'content': content,
      'media_urls': mediaUrls,
      'type': type,
      'visible_type': visibleType,
      'like_count': likeCount,
      'comment_count': commentCount,
      'retweet_count': retweetCount,
      'is_liked': isLiked,
      'is_retweeted': isRetweeted,
      'is_bookmarked': isBookmarked,
      'created_at': createdAt,
      'updated_at': updatedAt,
      'user': user?.toJson(),
    };
  }

  Tweet copyWith({
    String? id,
    String? userId,
    String? content,
    List<String>? mediaUrls,
    int? type,
    int? visibleType,
    int? likeCount,
    int? commentCount,
    int? retweetCount,
    bool? isLiked,
    bool? isRetweeted,
    bool? isBookmarked,
    int? createdAt,
    int? updatedAt,
    User? user,
  }) {
    return Tweet(
      id: id ?? this.id,
      userId: userId ?? this.userId,
      content: content ?? this.content,
      mediaUrls: mediaUrls ?? this.mediaUrls,
      type: type ?? this.type,
      visibleType: visibleType ?? this.visibleType,
      likeCount: likeCount ?? this.likeCount,
      commentCount: commentCount ?? this.commentCount,
      retweetCount: retweetCount ?? this.retweetCount,
      isLiked: isLiked ?? this.isLiked,
      isRetweeted: isRetweeted ?? this.isRetweeted,
      isBookmarked: isBookmarked ?? this.isBookmarked,
      createdAt: createdAt ?? this.createdAt,
      updatedAt: updatedAt ?? this.updatedAt,
      user: user ?? this.user,
    );
  }
}
