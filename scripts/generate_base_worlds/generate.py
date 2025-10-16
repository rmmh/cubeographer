#!/usr/bin/env python3

import json
import os
import shutil
import subprocess

import requests

os.chdir(os.path.dirname(__file__))

def get_forge_dl(ver):
    return f"https://maven.minecraftforge.net/net/minecraftforge/forge/{ver}/forge-{ver}-installer.jar"

def get_chunk_pregen_dl(ver):
    # https://www.curseforge.com/minecraft/mc-mods/chunkpregenerator/files/all
    # [...document.querySelectorAll(".file-card")].map(x=>x.firstChild.textContent + " " + x.href.replace(/.*\//, ''))
    return f"https://www.curseforge.com/api/v1/mods/267193/files/{ver}/download"

def get_carbon_config_dl(ver):
    return f"https://www.curseforge.com/api/v1/mods/898104/files/{ver}/download"

want_versions = [
    ("1.8.9", "11.15.1.2318-1.8.9", 5632149),
    # ("1.9.4", "12.17.0.2317", ),
    ("1.10.2", "12.18.3.2511", 5632150),
    ("1.11.2", "13.20.1.2588", 5632151),
    ("1.12.2", "14.23.5.2860", 5632153),
    # ("1.13.2", "", ),
    ("1.14.4", "28.2.26", 5632155),
    ("1.15.2", "31.2.60", 5632157),
    ("1.16.5", "36.2.42", 5632158),
    # ("1.17.1", "37.1.1", ),
    ("1.18.2", "40.3.11", 5632169),
    ("1.19.4", "45.4.2", 5632181),
    ("1.20.4", "49.2.2", 5632192),  # probably broken -- for 1.20.4
    ("1.21", "51.0.33", 5632196, 7036780),
][::-1]

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

if 0:
    for ver_mc, ver_forge, ver_pregen, ver_cc in want_versions:
        print(ver_mc, ver_forge, ver_pregen, ver_cc)
        forge_url = get_forge_dl(f'{ver_mc}-{ver_forge}')
        forge_jar = 'jars/' + os.path.basename(forge_url)
        dl_to(forge_url, forge_jar)
        pregen_url = get_chunk_pregen_dl(ver_pregen)
        pregen_jar = f'jars/chunk-pregenerator-{ver_mc}-{ver_pregen}.jar'
        dl_to(pregen_url, pregen_jar)
        cc_url = get_carbon_config_dl(ver_cc)
        cc_jar = f'jars/carbon-config-{ver_mc}-{ver_pregen}.jar'
        dl_to(cc_url, cc_jar)
        shutil.rmtree('tmp')
        os.makedirs('tmp', exist_ok=True)
        open('tmp/eula.txt', 'w').write('eula=true')
        subprocess.check_call(f"java -jar {forge_jar} --installServer tmp".split())
        shutil.copyfile(pregen_jar, pregen_jar.replace('jars/', 'tmp/mods/'))
        shutil.copyfile(cc_jar, cc_jar.replace('jars/', 'tmp/mods/'))

if 0:
    for version in want_versions:
        server_jar = f'jars/minecraft-server-{version}.jar'
        if not os.path.exists(server_jar):
            get_manifest()
            version_manifest = requests.get(manifest_versions[version]['url']).json()
            server_url = version_manifest['downloads']['server']['url']
            print(version, server_url)
            dl_to(server_url, server_jar)

BASE_MAP_DIR = '../../maps/base/'

def version_tuple(v):
    return tuple(int(x) for x in v.split('.'))

if 1:
    get_manifest()
    for man in manifest['versions']:
        if man['type'] != 'release':
            continue
        version = man['id']
        if version_tuple(version) <= version_tuple('1.2.4'):
            break
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
        open('tmp/server.properties', 'w').write('level-seed=cubeographer-' + version)
        print("generating world for", version)
        try:
            subprocess.run(['java', '-jar', '../' + server_jar, '-nogui'], cwd='tmp', input=b'stop\n', check=True, capture_output=True, timeout=60)
        except subprocess.TimeoutExpired as e:
            stdout = e.stdout.decode()
            if 'Done' not in stdout:
                print(stdout)
            else:
                print("timed out successfully")
        # print(res.stdout)
        if os.path.exists('tmp/world/region/'):
            shutil.move('tmp/world', version_world_dir)
        else:
            print("something broke")
