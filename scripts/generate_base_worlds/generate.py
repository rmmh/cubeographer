#!/usr/bin/env python3

import argparse
import json
import os
import shutil
import subprocess

import requests

os.chdir(os.path.dirname(__file__))

manifest = None
def get_manifest():
    global manifest, manifest_versions
    if not manifest:
        manifest = requests.get("https://launchermeta.mojang.com/mc/game/version_manifest.json").json()
        manifest_versions = {x['id']: x for x in manifest['versions']}

def dl_to(url, path):
    if os.path.exists(path):
        return
    with requests.get(url, stream=True) as r:
        r.raise_for_status()
        with open(path + '.tmp', 'wb') as f:
            for chunk in r.iter_content(1<<16):
                f.write(chunk)
    os.rename(path + '.tmp', path)
    print(path)

os.makedirs('jars', exist_ok=True)

BASE_MAP_DIR = '../../maps/base/'

def version_tuple(v):
    return tuple(int(x) for x in v.split('.'))

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Generate base Minecraft worlds.')
    parser.add_argument('--seed', type=str, default=None, help='Seed for the generated worlds. If not specified, defaults to "cubeographer-<version>".')
    parser.add_argument('--all', action='store_true', help='Generate for all release versions instead of only the latest release of each line (e.g. only 1.21.11, not 1.21.10).')
    args = parser.parse_args()

    get_manifest()
    last_line = None
    for man in manifest['versions']:
        if man['type'] != 'release':
            continue
        version = man['id']

        try:
            vt = version_tuple(version)
        except ValueError:
            continue
        if vt <= version_tuple('1.2.4'):
            break

        line = vt[:2]
        if not args.all and line == last_line:
            continue
        last_line = line

        version_world_dir = BASE_MAP_DIR + version
        if os.path.exists(version_world_dir):
            continue

        server_jar = f'jars/minecraft-server-{version}.jar'
        if not os.path.exists(server_jar):
            version_manifest = requests.get(manifest_versions[version]['url']).json()
            print(version_manifest)
            server_url = version_manifest['downloads']['server']['url']
            dl_to(server_url, server_jar)

        shutil.rmtree('tmp', ignore_errors=True)
        os.makedirs('tmp', exist_ok=True)
        open('tmp/eula.txt', 'w').write('eula=true')
        seed = args.seed if args.seed is not None else 'cubeographer-' + version
        open('tmp/server.properties', 'w').write(f'level-seed={seed}\n')
        print("generating world for", version)
        try:
            subprocess.run(['java', '-jar', '../' + server_jar, '-nogui'], cwd='tmp', input=b'stop\n', check=True, capture_output=True, timeout=15)
        except subprocess.TimeoutExpired as e:
            stdout = e.stdout.decode()
            if 'Done' not in stdout:
                print(stdout)
            else:
                print("timed out successfully?")
                print(stdout)
        # print(res.stdout)
        if os.path.exists('tmp/world/region/') or os.path.exists('tmp/world/dimensions/minecraft/overworld/region/'):
            shutil.move('tmp/world', version_world_dir)
        else:
            print("something broke")
