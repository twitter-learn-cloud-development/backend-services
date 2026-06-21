import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';
import 'package:cached_network_image/cached_network_image.dart';
import 'feed_notifier.dart';
import '../../auth/presentation/auth_notifier.dart';
import '../../../core/constants/colors.dart';
import '../../../core/network/dio_client.dart';

class ComposeScreen extends ConsumerStatefulWidget {
  const ComposeScreen({super.key});

  @override
  ConsumerState<ComposeScreen> createState() => _ComposeScreenState();
}

class _ComposeScreenState extends ConsumerState<ComposeScreen> {
  final _textController = TextEditingController();
  final _picker = ImagePicker();
  final List<String> _imagePaths = [];
  bool _isPublishing = false;

  @override
  void dispose() {
    _textController.dispose();
    super.dispose();
  }

  Future<void> _pickImages() async {
    try {
      final pickedFiles = await _picker.pickMultiImage(
        maxWidth: 1080,
        maxHeight: 1080,
        imageQuality: 85,
      );
      if (pickedFiles.isNotEmpty) {
        setState(() {
          _imagePaths.addAll(pickedFiles.map((e) => e.path));
        });
      }
    } catch (e) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('选择图片失败: $e')),
      );
    }
  }

  void _removeImage(int index) {
    setState(() {
      _imagePaths.removeAt(index);
    });
  }

  Future<void> _publish() async {
    final content = _textController.text.trim();
    if (content.isEmpty && _imagePaths.isEmpty) return;

    setState(() {
      _isPublishing = true;
    });

    try {
      final repo = ref.read(tweetRepositoryProvider);
      final List<String> uploadedUrls = [];

      // 1. Upload images sequentially if any
      for (var path in _imagePaths) {
        final url = await repo.uploadMedia(path);
        uploadedUrls.add(url);
      }

      // 2. Submit tweet
      final newTweet = await repo.createTweet(content, mediaUrls: uploadedUrls);
      
      // 3. Update timeline in memory immediately
      ref.read(feedNotifierProvider.notifier).insertNewTweet(newTweet);

      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('推文发布成功！'),
            backgroundColor: AppColors.retweetColor,
          ),
        );
        Navigator.of(context).pop();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('发布失败: ${e.toString().replaceAll('Exception: ', '')}'),
            backgroundColor: AppColors.likeColor,
          ),
        );
      }
    } finally {
      if (mounted) {
        setState(() {
          _isPublishing = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final authState = ref.watch(authNotifierProvider);
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    final currentUser = authState.user;
    final avatarUrl = currentUser?.avatar != null && currentUser!.avatar.isNotEmpty
        ? DioClient.getMediaUrl(currentUser.avatar)
        : '';

    final textLength = _textController.text.length;
    final canPublish = (textLength > 0 || _imagePaths.isNotEmpty) && !_isPublishing;

    return Scaffold(
      backgroundColor: isDark ? AppColors.darkBg : AppColors.lightBg,
      appBar: AppBar(
        leading: TextButton(
          onPressed: _isPublishing ? null : () => Navigator.of(context).pop(),
          child: Text(
            '取消',
            style: TextStyle(
              color: isDark ? Colors.white : Colors.black,
              fontSize: 16,
            ),
          ),
        ),
        leadingWidth: 70,
        actions: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16.0, vertical: 8.0),
            child: ElevatedButton(
              onPressed: canPublish ? _publish : null,
              style: ElevatedButton.styleFrom(
                backgroundColor: AppColors.primary,
                foregroundColor: Colors.white,
                disabledBackgroundColor: AppColors.primary.withOpacity(0.5),
                disabledForegroundColor: Colors.white.withOpacity(0.8),
                shape: RoundedRectangleBorder(
                  borderRadius: BorderRadius.circular(20),
                ),
                elevation: 0,
                padding: const EdgeInsets.symmetric(horizontal: 20),
              ),
              child: _isPublishing
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(
                        color: Colors.white,
                        strokeWidth: 2,
                      ),
                    )
                  : const Text(
                      '发帖',
                      style: TextStyle(
                        fontWeight: FontWeight.bold,
                        fontSize: 15,
                      ),
                    ),
            ),
          ),
        ],
      ),
      body: SafeArea(
        child: Column(
          children: [
            Expanded(
              child: SingleChildScrollView(
                padding: const EdgeInsets.all(16.0),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    // User avatar
                    CircleAvatar(
                      radius: 20,
                      backgroundColor: isDark ? AppColors.darkBorder : AppColors.lightBorder,
                      backgroundImage: avatarUrl.isNotEmpty
                          ? CachedNetworkImageProvider(avatarUrl)
                          : null,
                      child: avatarUrl.isEmpty
                          ? const Icon(Icons.person, size: 20)
                          : null,
                    ),
                    const SizedBox(width: 12),
                    
                    // Input content area
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          TextField(
                            controller: _textController,
                            maxLines: null,
                            maxLength: 280,
                            autofocus: true,
                            enabled: !_isPublishing,
                            style: TextStyle(
                              color: isDark ? Colors.white : Colors.black,
                              fontSize: 18,
                            ),
                            decoration: const InputDecoration(
                              hintText: '有什么新鲜事？',
                              border: InputBorder.none,
                              enabledBorder: InputBorder.none,
                              focusedBorder: InputBorder.none,
                              counterText: '',
                              contentPadding: EdgeInsets.zero,
                            ),
                            onChanged: (_) => setState(() {}),
                          ),
                          
                          const SizedBox(height: 20),
                          
                          // Previews of selected images
                          if (_imagePaths.isNotEmpty)
                            SizedBox(
                              height: 150,
                              child: ListView.builder(
                                scrollDirection: Axis.horizontal,
                                itemCount: _imagePaths.length,
                                itemBuilder: (context, index) {
                                  return Padding(
                                    padding: const EdgeInsets.only(right: 10.0),
                                    child: Stack(
                                      children: [
                                        ClipRRect(
                                          borderRadius: BorderRadius.circular(12),
                                          child: Image.file(
                                            File(_imagePaths[index]),
                                            width: 150,
                                            height: 150,
                                            fit: BoxFit.cover,
                                          ),
                                        ),
                                        Positioned(
                                          top: 5,
                                          right: 5,
                                          child: GestureDetector(
                                            onTap: () => _removeImage(index),
                                            child: Container(
                                              padding: const EdgeInsets.all(4),
                                              decoration: BoxDecoration(
                                                color: Colors.black.withOpacity(0.7),
                                                shape: BoxShape.circle,
                                              ),
                                              child: const Icon(
                                                Icons.close,
                                                color: Colors.white,
                                                size: 16,
                                              ),
                                            ),
                                          ),
                                        ),
                                      ],
                                    ),
                                  );
                                },
                              ),
                            ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ),
            
            // Bottom Action bar: Gallery button & word counter
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
              decoration: BoxDecoration(
                border: Border(
                  top: BorderSide(
                    color: isDark ? AppColors.darkBorder : AppColors.lightBorder,
                    width: 0.5,
                  ),
                ),
              ),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  // Photo picker button
                  IconButton(
                    icon: const Icon(
                      Icons.image_outlined,
                      color: AppColors.primary,
                      size: 26,
                    ),
                    onPressed: _isPublishing ? null : _pickImages,
                  ),
                  
                  // Word counter circle
                  Row(
                    children: [
                      Text(
                        '${280 - textLength}',
                        style: TextStyle(
                          color: textLength > 250
                              ? AppColors.likeColor
                              : (isDark ? AppColors.darkTextSecondary : AppColors.lightTextSecondary),
                          fontWeight: FontWeight.bold,
                        ),
                      ),
                      const SizedBox(width: 10),
                      SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(
                          value: textLength / 280,
                          backgroundColor: isDark ? AppColors.darkBorder : AppColors.lightBorder,
                          color: textLength > 250 ? AppColors.likeColor : AppColors.primary,
                          strokeWidth: 2,
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
