import subprocess
import time
import urllib.request
import json
import os

def run_cmd(cmd):
    res = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, encoding="utf-8")
    return res.stdout, res.stderr

def main():
    print("==================================================")
    print("[INFO] Evolution V5.0 - Adaptive Tuning Test")
    print("==================================================")

    # 1. 检查 Redis 状态并注入高危低效的初始缓存配置 (L1 TTL=2s, L2 TTL=10s, Preload=1)
    print("[1] Injecting initial low-performance cache configs into Redis...")
    
    init_config = {
        "l1_cache_ttl_seconds": 2,
        "l2_cache_ttl_seconds": 10,
        "preload_depth": 1
    }
    
    payload_str = json.dumps(init_config)
    print(f"    Payload: {payload_str}")
    
    # 优先通过 kubectl exec 往 K8s 的 redis-master-0 写入
    stdout, stderr = run_cmd([
        "kubectl", "exec", "twitter-clone-redis-master-0", "--", 
        "redis-cli", "SET", "system:cache:dynamic_config", payload_str
    ])
    
    if "OK" not in stdout and "OK" not in stderr:
        print("[WARN] Failed to write to K8s redis, checking local redis...")
        # 尝试本地 redis-cli
        stdout, stderr = run_cmd([
            "redis-cli", "SET", "system:cache:dynamic_config", payload_str
        ])
        if "OK" not in stdout:
            print("[ERROR] Failed to inject initial config. Redis not reachable.")
            print(stderr)
            print("💡 Please ensure Docker Desktop and Minikube are running, then retry.")
            return
            
    print("✅ Initial low-performance cache config injected.")

    # 2. 启动端口转发 (API Gateway)
    print("[2] Spawning temporary port-forward on port 9639...")
    pf_process = subprocess.Popen(
        ["kubectl", "port-forward", "deployment/twitter-clone-gateway", "9639:9638"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL
    )
    time.sleep(3) # 等待端口建立

    # 3. 发送 Firing 告警，注入 CPU 火焰图过载的报错 labels，诱导 LLM Agent 进行自适应调优
    dynamic_group_key = f"TimelineCacheCPUOverload-test-group-{int(time.time())}"
    alert_url = "http://127.0.0.1:9639/alerts"
    alert_payload = {
        "status": "firing",
        "groupKey": dynamic_group_key,
        "alerts": [
            {
                "labels": {
                    "alertname": "TimelineCacheCPUOverload",
                    "severity": "critical",
                    "service": "tweet-service"
                },
                "annotations": {
                    "summary": "High CPU usage detected in timeline cache merge sort"
                }
            }
        ]
    }
    
    req = urllib.request.Request(
        alert_url,
        data=json.dumps(alert_payload).encode("utf-8"),
        headers={
            "Content-Type": "application/json",
            "X-Alertmanager-Token": "twitter-clone-secret-alert-token"
        },
        method="POST"
    )

    print(f"[3] Sending firing alert with GroupKey: {dynamic_group_key}...")
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            print(f"    Gateway Response: {resp.read().decode('utf-8')}")
    except Exception as e:
        print(f"[ERROR] Failed to send alert: {e}")
        pf_process.terminate()
        return

    # 4. 等待 AIOps 自愈决策、Pyroscope 火焰图分析和 TuneCacheConfig 下发执行
    print("[4] Waiting 12 seconds for AIOps async profiling analysis and Redis broadcast...")
    time.sleep(12)

    # 5. 重新获取 Redis 配置，检查配置是否被 AI 成功调优
    print("[5] Fetching updated dynamic config from Redis...")
    stdout, _ = run_cmd([
        "kubectl", "exec", "twitter-clone-redis-master-0", "--", 
        "redis-cli", "GET", "system:cache:dynamic_config"
    ])
    if "l1_cache_ttl_seconds" not in stdout:
        stdout_local, _ = run_cmd([
            "redis-cli", "GET", "system:cache:dynamic_config"
        ])
        if "l1_cache_ttl_seconds" in stdout_local:
            stdout = stdout_local
        
    print("--- Updated Dynamic Config ---")
    print(stdout.strip())
    print("-------------------------------")

    # 6. 检查冷却锁状态 (aiops:cooldown:tune_cache)
    stdout_lock, _ = run_cmd([
        "kubectl", "exec", "twitter-clone-redis-master-0", "--", 
        "redis-cli", "TTL", "aiops:cooldown:tune_cache"
    ])
    if stdout_lock.strip() == "" or "ERR" in stdout_lock:
        stdout_lock_local, _ = run_cmd([
            "redis-cli", "TTL", "aiops:cooldown:tune_cache"
        ])
        if stdout_lock_local.strip() != "":
            stdout_lock = stdout_lock_local
            
    print(f"✅ Cooldown lock TTL: {stdout_lock.strip()} seconds remaining (Prevents Flapping).")

    # 7. 清理端口转发
    pf_process.terminate()
    print("[SUCCESS] Adaptive tuning test script finished.")

if __name__ == "__main__":
    main()
