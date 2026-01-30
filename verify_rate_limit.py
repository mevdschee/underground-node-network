import subprocess
import time
import os
import signal
import re
import sys
import shutil

def run_test(file_size_kb, limit_str, expected_duration):
    print(f"\n--- Testing: {file_size_kb}KB file with {limit_str} limit ---")
    
    # 1. Setup
    test_dir = f"test_env_{file_size_kb}"
    if os.path.exists(test_dir):
        shutil.rmtree(test_dir)
    os.makedirs(f"{test_dir}/room_files", exist_ok=True)
    os.makedirs(f"{test_dir}/users", exist_ok=True)
    
    with open(f"{test_dir}/room_files/test_{file_size_kb}.bin", "wb") as f:
        f.write(os.urandom(file_size_kb * 1024))

    with open("tests/integration/test_user_key.pub", "r") as f:
        pubkey = f.read().strip()
    with open(f"{test_dir}/users/maurits", "w") as f:
        f.write(pubkey)
    with open(f"{test_dir}/users/myroom", "w") as f:
        f.write(pubkey)

    ep_port = 44322 + (file_size_kb % 100)
    client_port = 44323 + (file_size_kb % 100)

    # 2. Start Entrypoint
    ep = subprocess.Popen(["./unn-entrypoint-bin", "-headless", "-port", str(ep_port), "-users", f"{test_dir}/users", 
                          "-hostkey", f"{test_dir}/host_key"], 
                          stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    time.sleep(2)

    # 3. Start Room
    client = subprocess.Popen(["./unn-room-bin", "-port", str(client_port), "-room", "myroom", 
                              "-entrypoint", f"localhost:{ep_port}", "-headless", "-identity", "tests/integration/test_user_key", 
                              "-max-upload", limit_str, "-files", f"./{test_dir}/room_files"], 
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    time.sleep(3)

    # 4. Use unn-client to trigger a download
    ssh_cmd = ["./unn-client-bin", "-batch", "-identity", "tests/integration/test_user_key", f"ssh://maurits@localhost:{ep_port}/myroom"]
    wrapper = subprocess.Popen(ssh_cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    
    time.sleep(3)
    if wrapper.poll() is not None:
        print(f"Wrapper exited early with code {wrapper.returncode}")
        print(wrapper.stdout.read())
        ep.kill()
        client.kill()
        return False

    print("Requesting file...")
    try:
        wrapper.stdin.write(f"/get test_{file_size_kb}.bin\n")
        wrapper.stdin.flush()
    except BrokenPipeError:
        print("Broken pipe when sending command. Wrapper output:")
        print(wrapper.stdout.read())
        ep.kill()
        client.kill()
        return False

    start_transfer = None
    print("DEBUG: Waiting for 'UNN DOWNLOAD READY'...")
    # Wait for completion
    while True:
        line = wrapper.stdout.readline()
        if not line:
            if wrapper.poll() is not None:
                print("DEBUG: Wrapper process terminated.")
                break
            continue
        
        # Print raw data for debugging
        print(f"DEBUG: RECEIVED: {repr(line)}")
        sys.stdout.write(line)
        sys.stdout.flush()
        
        if "UNN DOWNLOAD READY" in line:
            print("DEBUG: FOUND TRIGGER! Transfer started (Instruction block detected)...")
            start_transfer = time.time()
        
    wrapper.wait()
    end_transfer = time.time()
    
    if start_transfer is None:
        print("Failed to trigger download")
        ep.kill()
        client.kill()
        return False

    actual_duration = end_transfer - start_transfer
    speed_kb_s = file_size_kb / actual_duration
    
    print(f"\nTransfer took {actual_duration:.2f} seconds (Expected ~{expected_duration}s)")
    print(f"Average speed: {speed_kb_s:.2f} KB/s")

    # 6. Cleanup
    ep.kill()
    client.kill()
    shutil.rmtree(test_dir)

    # Allow 20% margin
    if actual_duration >= expected_duration * 0.8:
        print("SUCCESS: Rate limiting works!")
        return True
    else:
        print("FAILURE: Rate limiting is not effective.")
        return False

if __name__ == "__main__":
    success = True
    # Test case 1: 50KB file with 10KB limit -> ~5s
    if not run_test(50, "10KB", 5):
        success = False
    
    # Test case 2: 150KB file with 30KB limit -> ~5s 
    if not run_test(150, "30KB", 5):
        success = False

    if success:
        print("\nALL TESTS PASSED")
    else:
        print("\nSOME TESTS FAILED")
        exit(1)
