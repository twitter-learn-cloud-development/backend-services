class User {
  final String id;
  final String username;
  final String email;
  final String avatar;
  final String bio;
  final String coverUrl;
  final String website;
  final String location;
  final int createdAt;

  User({
    required this.id,
    required this.username,
    required this.email,
    this.avatar = '',
    this.bio = '',
    this.coverUrl = '',
    this.website = '',
    this.location = '',
    this.createdAt = 0,
  });

  factory User.fromJson(Map<String, dynamic> json) {
    // ID might come as integer in some raw endpoints, or string in BFF. Handle both safely.
    final rawId = json['id'];
    final idString = rawId != null ? rawId.toString() : '';

    return User(
      id: idString,
      username: json['username'] ?? '',
      email: json['email'] ?? '',
      avatar: json['avatar'] ?? '',
      bio: json['bio'] ?? '',
      coverUrl: json['cover_url'] ?? '',
      website: json['website'] ?? '',
      location: json['location'] ?? '',
      createdAt: json['created_at'] is int ? json['created_at'] : 0,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'username': username,
      'email': email,
      'avatar': avatar,
      'bio': bio,
      'cover_url': coverUrl,
      'website': website,
      'location': location,
      'created_at': createdAt,
    };
  }

  User copyWith({
    String? id,
    String? username,
    String? email,
    String? avatar,
    String? bio,
    String? coverUrl,
    String? website,
    String? location,
    int? createdAt,
  }) {
    return User(
      id: id ?? this.id,
      username: username ?? this.username,
      email: email ?? this.email,
      avatar: avatar ?? this.avatar,
      bio: bio ?? this.bio,
      coverUrl: coverUrl ?? this.coverUrl,
      website: website ?? this.website,
      location: location ?? this.location,
      createdAt: createdAt ?? this.createdAt,
    );
  }
}
