import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'auth_notifier.dart';

class AccountSecurityScreen extends ConsumerStatefulWidget {
  const AccountSecurityScreen({super.key});
  @override
  ConsumerState<AccountSecurityScreen> createState() => _AccountSecurityState();
}

class _AccountSecurityState extends ConsumerState<AccountSecurityScreen> {
  final _form = GlobalKey<FormState>();
  final _old = TextEditingController(),
      _password = TextEditingController(),
      _confirm = TextEditingController();
  bool _busy = false;
  @override
  void dispose() {
    _old.dispose();
    _password.dispose();
    _confirm.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_busy || !_form.currentState!.validate()) return;
    setState(() => _busy = true);
    await ref
        .read(authNotifierProvider.notifier)
        .changePassword(_old.text, _password.text);
    if (!mounted) return;
    _old.clear();
    _password.clear();
    _confirm.clear();
    setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authNotifierProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('账号安全')),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(24),
        child: Form(
          key: _form,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Text('修改密码后当前客户端需要重新登录。其他设备和已签发令牌仍有效至到期，暂不支持全设备撤销。'),
              TextFormField(
                controller: _old,
                obscureText: true,
                autofillHints: const [AutofillHints.password],
                decoration: const InputDecoration(labelText: '当前密码'),
                validator: (v) => v == null || v.isEmpty ? '请输入当前密码' : null,
              ),
              TextFormField(
                controller: _password,
                obscureText: true,
                autofillHints: const [AutofillHints.newPassword],
                decoration: const InputDecoration(labelText: '新密码'),
                validator: (v) {
                  final length = utf8.encode(v ?? '').length;
                  return length < 6 || length > 72
                      ? '新密码须为 6–72 UTF-8 字节'
                      : null;
                },
              ),
              TextFormField(
                controller: _confirm,
                obscureText: true,
                autofillHints: const [AutofillHints.newPassword],
                decoration: const InputDecoration(labelText: '确认新密码'),
                validator: (v) => v != _password.text ? '两次输入须一致' : null,
              ),
              if (auth.errorMessage != null)
                Text(
                  auth.errorMessage!,
                  style: const TextStyle(color: Colors.red),
                ),
              ElevatedButton(
                onPressed: _busy ? null : _submit,
                child: Text(_busy ? '提交中…' : '修改密码并重新登录'),
              ),
              TextButton(
                onPressed: _busy
                    ? null
                    : () async {
                        final accepted = await showDialog<bool>(
                          context: context,
                          builder: (dialog) => AlertDialog(
                            title: const Text('退出当前客户端？'),
                            content: const Text('其他设备和已签发令牌仍有效至到期。'),
                            actions: [
                              TextButton(
                                onPressed: () => Navigator.pop(dialog, false),
                                child: const Text('取消'),
                              ),
                              TextButton(
                                onPressed: () => Navigator.pop(dialog, true),
                                child: const Text('退出'),
                              ),
                            ],
                          ),
                        );
                        if (accepted == true && mounted) {
                          await ref
                              .read(authNotifierProvider.notifier)
                              .logout();
                        }
                      },
                child: const Text('退出当前客户端'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
