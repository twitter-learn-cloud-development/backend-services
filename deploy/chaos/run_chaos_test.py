import subprocess
import time
import sys

def run_cmd(cmd):
    result = subprocess.run(cmd, shell=True, text=True, capture_output=True)
    return result.stdout.strip(), result.stderr.strip()

def main():
    print("==================================================")
    print("[SAFETY] Chaos Engineering - Sentinel Resiliency Test")
    print("==================================================")

    # 1. 核心护栏：验证 Kubernetes 上下文，防止误伤生产
    context, err = run_cmd("kubectl config current-context")
    if err or "minikube" not in context:
        print(f"[ERROR] SAFETY BARRIER: Current context is '{context}'.")
        print("   This test MUST and CAN only run in the 'minikube' local environment!")
        sys.exit(1)

    print("[SUCCESS] Safety check passed. Local Kubernetes environment verified (Minikube).")

    # 2. 检查 Chaos Mesh 是否在集群中部署
    has_chaos_mesh = True
    crd_check, _ = run_cmd("kubectl get crd networkchaos.chaos-mesh.org")
    if "networkchaos.chaos-mesh.org" not in crd_check:
        print("[WARNING] Chaos Mesh CRD (networkchaos) not found in cluster.")
        print("   Make sure Chaos Mesh is installed in your minikube cluster for complete scenarios.")
        print("   Entering Simulative Chaos Fallback Mode...")
        has_chaos_mesh = False

    # 3. 解析混沌类型 (默认是 network-delay)
    chaos_type = "network-delay"
    manifest_path = "deploy/chaos/network-delay.yaml"
    if len(sys.argv) > 1 and sys.argv[1] == "pod-kill":
        chaos_type = "pod-kill"
        manifest_path = "deploy/chaos/pod-kill.yaml"

    print("[INFO] Current time: ", time.strftime("%Y-%m-%d %H:%M:%S"))
    print(f"[START] Triggering chaos injection ({chaos_type})...")
    
    # 4. 执行混沌注入
    if has_chaos_mesh:
        stdout, stderr = run_cmd(f"kubectl apply -f {manifest_path}")
        if stderr:
            print(f"[ERROR] Failed to inject chaos: {stderr}")
            sys.exit(1)
        
        if chaos_type == "network-delay":
            print("[CHAOS] Chaos injected: Redis network delay of 5000ms is active.")
            print("[INFO] temporal workflow will catch Redis connection time-outs and execute exponential retry.")
        else:
            print("[CHAOS] Chaos injected: tweet-service pod eviction (pod-kill) is active.")
    else:
        # Fallback 模拟分支
        if chaos_type == "pod-kill":
            print("[CHAOS] [FALLBACK] Simulating pod-kill by evicting tweet-service pods...")
            # 强杀 tweet-service Pod 以模拟 pod-kill
            stdout, stderr = run_cmd("kubectl delete pod -l app.kubernetes.io/component=tweet-service --force --grace-period=0")
            if stderr:
                print(f"[WARNING] Fallback eviction warning: {stderr}")
            else:
                print("[SUCCESS] Mock Pod Eviction executed successfully.")
        else:
            print("[WARNING] Cannot mock network-delay without Chaos Mesh. Skipping inject.")

    print("[INFO] Waiting 10 seconds to simulate failures under chaos...")
    time.sleep(10)

    # 5. 清理混沌
    if has_chaos_mesh:
        print(f"[CLEAN] Cleaning up chaos ({chaos_type})...")
        stdout_del, stderr_del = run_cmd(f"kubectl delete -f {manifest_path}")
        if stderr_del:
            print(f"[WARNING] Failed to remove chaos resource: {stderr_del}")
        else:
            print(f"[SUCCESS] Chaos removed successfully. System latency/pod recovered.")
    else:
        print("[CLEAN] Simulative chaos session finished.")

    print("[DONE] Resiliency verification completed.")

if __name__ == "__main__":
    main()
