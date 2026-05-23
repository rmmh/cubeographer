#!/usr/bin/env python3
import json
import urllib.request
import subprocess
import zipfile
import tempfile
import os

def get_json(url):
    req = urllib.request.Request(url, headers={"User-Agent": "Mozilla"})
    with urllib.request.urlopen(req) as res:
        return json.loads(res.read().decode())

# Keep absolute path to the project root
project_root = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))

# Resolve URLs before changing directory
print("Resolving Minecraft 1.21.4 server jar URL...")
manifest = get_json("https://launchermeta.mojang.com/mc/game/version_manifest_v2.json")
v_url = next(v["url"] for v in manifest["versions"] if v["id"] == "1.21.4")
server_jar_url = get_json(v_url)["downloads"]["server"]["url"]

with tempfile.TemporaryDirectory() as tmpdir:
    print(f"Working in temporary directory: {tmpdir}")
    os.chdir(tmpdir)

    print("Downloading server jar...")
    urllib.request.urlretrieve(server_jar_url, "minecraft_server.jar")

    print("Running data generator...")
    subprocess.run([
        "java", "-DbundlerMainClass=net.minecraft.data.Main", 
        "-jar", "minecraft_server.jar", "--reports"
    ], check=True)

    # Learn protocol version
    with zipfile.ZipFile("minecraft_server.jar") as z:
        with z.open("version.json") as f:
            protocol_version = json.loads(f.read().decode())["protocol_version"]

    print(f"Protocol version: {protocol_version}")

    # Parse blocks.json
    with open("generated/reports/blocks.json") as f:
        blocks = json.load(f)

    states_by_id = {}
    for name, block in blocks.items():
        short_name = name.replace("minecraft:", "")
        for state in block.get("states", []):
            props = state.get("properties", {})
            if props:
                props_str = ",".join(f"{k}={v}" for k, v in sorted(props.items()))
                states_by_id[state["id"]] = f"{short_name}[{props_str}]"
            else:
                states_by_id[state["id"]] = short_name

    max_id = max(states_by_id.keys())
    names = [states_by_id.get(i, "") for i in range(max_id + 1)]

    # Ensure output directory exists
    out_dir = os.path.join(project_root, "go/zvcr/data")
    os.makedirs(out_dir, exist_ok=True)

    # Generate json file
    json_path = os.path.join(out_dir, f"blockstates{protocol_version}.json")
    with open(json_path, "w") as f:
        json.dump({
            "protocolVersion": protocol_version,
            "entries": [{"id": i, "name": names[i]} for i in range(max_id + 1)]
        }, f, indent=4)

    # Generate and compress txt file
    txt_path = f"blockstates{protocol_version}.txt"
    zstd_path = os.path.join(out_dir, f"blockstates{protocol_version}.txt.zstd")
    with open(txt_path, "w") as f:
        f.write("\n".join(names) + "\n")

    print(f"Compressing to {zstd_path}...")
    subprocess.run(["zstd", "-19", "-f", txt_path, "-o", zstd_path], check=True)

print("Done!")
