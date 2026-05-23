/*
TODO: open addressing for block insertion/deletion
steal from https://shlegeris.com/2017/01/06/hash-maps.html
*/

import './index.css';

import * as twgl from 'twgl.js';
import { mat4, vec3, vec4 } from 'gl-matrix';

const vertexShader = require('./cube_vertex.glsl');
const fragmentShader = require('./cube_fragment.glsl');
const impostorVertexShader = require('./impostor_vertex.glsl');
const impostorFragmentShader = require('./impostor_fragment.glsl');
import { Gunzip, gunzipSync } from 'fflate';

import * as renderer from './renderer';
import { OrbitControls } from './camera';
import { createOrbitTargetFinder } from './voxel';
import { fetchRegionLOD, makeImpostorGeometry } from './lod';
import { setupGUI } from './gui';

DEBUG && new EventSource('/esbuild').addEventListener('change', () => location.reload());

const context = new renderer.Context(document.querySelector('#canvas'));
context.setSize(window.innerWidth, window.innerHeight);

const aspect = window.innerWidth / window.innerHeight;
const camera = new renderer.PerspectiveCamera(75, aspect, 0.1, 3000);

vec3.set(camera.position, 100, 40, 100);  // face northish
vec3.set(camera.target, 0, 0, 0);
camera.update();

window.addEventListener('resize', onWindowResize, false);
function onWindowResize() {
    camera.aspect = window.innerWidth / window.innerHeight;
    context.setSize(window.innerWidth, window.innerHeight);
    camera.update();

    if (ONDEMAND) render();
}

const Stats = require("stats.js");

let ONDEMAND = true;

let PROD = 1;

const space = PROD ? 512 : 64;

const sceneGraph = new renderer.SceneGraph(context);

let metadataLoadingPromise: Promise<void>;

async function loadMetadata() {
    try {
        const response = await fetch('map/metadata.json');
        if (!response.ok) {
            throw new Error(`Failed to fetch map/metadata.json: ${response.statusText}`);
        }
        const data = await response.json();

        const parseSet = (str: string) => {
            if (!str) return new Set<string>();
            return new Set<string>(str.trim().split(/\s+/));
        };

        sceneGraph.mapMetadata = {
            full_regions: parseSet(data.full_regions),
            lod_regions: parseSet(data.lod_regions),
            tile_regions: parseSet(data.tile_regions),
            loaded: true
        };
        console.debug("Loaded map/metadata.json:", sceneGraph.mapMetadata);
    } catch (e) {
        console.warn("Could not load map/metadata.json, falling back to eager loading:", e);
        sceneGraph.mapMetadata = {
            full_regions: new Set<string>(),
            lod_regions: new Set<string>(),
            tile_regions: new Set<string>(),
            loaded: false
        };
    }
}

metadataLoadingPromise = loadMetadata();

const stats = new Stats();
stats.showPanel(0); // 0: fps, 1: ms, 2: mb, 3+: custom
document.body.appendChild(stats.dom);

context.setClearColor(0x7e, 0xab, 0xff);

let urlTimer = 0;

// https://stackoverflow.com/a/13419367/3694
function parseQuery(queryString: string) {
    const query: { [name: string]: string } = {};
    const pairs = (queryString[0] === '?' ? queryString.substr(1) : queryString).split('&');
    for (let i = 0; i < pairs.length; i++) {
        const pair = pairs[i].split('=');
        query[decodeURIComponent(pair[0])] = decodeURIComponent(pair[1] || '');
    }
    return query;
}

function maybeSetCameraFromLocstring() {
    let q = parseQuery(document.location.search);
    if (q['L']) {
        cameraFromLocstring(q['L']);
        camera.update();
    }
}

function cameraFromLocstring(loc: string) {
    const m = /(-?\d+)\.(-?\d+):(-?\d+),(-?\d+),(-?\d+):(-?\d+),(-?\d+),(-?\d+)/.exec(loc);
    if (!m)
        return;
    const [_, rx, rz, lx, y, lz, ox, oy, oz] = m.map(x => +x);

    let newPos = vec3.fromValues(rx * 512 + lx, y, rz * 512 + lz);
    let newTarget = vec3.add(vec3.create(), newPos, vec3.fromValues(ox, oy, oz));

    vec3.copy(camera.position, newPos);
    vec3.copy(controls.target, newTarget);
}

function cameraMove() {
    if (history) {
        if (urlTimer) clearTimeout(urlTimer)
        let pos = camera.position;
        let off = vec3.sub(vec3.create(), controls.target, pos);
        off[0] = Math.round(off[0]),
            off[1] = Math.round(off[1]),
            off[2] = Math.round(off[2]);
        let rx = pos[0] >> 9;
        let rz = pos[2] >> 9;
        let y = pos[1] | 0;
        let lx = (pos[0] | 0) - rx * 512;
        let lz = (pos[2] | 0) - rz * 512;

        const locString = `${rx}.${rz}:${lx},${y},${lz}:${off[0]},${off[1]},${off[2]}`;

        urlTimer = window.setTimeout(() => {
            history.replaceState(null, null, `?L=${locString}`);
            // for debugging:
            // cameraFromLocstring(locString); render();
        }, 100);
    }
    render();
}

var x: boolean;

// TODO: replace these controls with block-based ones,
// i.e. rotate around the click target
let controls = new OrbitControls(camera, context.canvas);
controls.addEventListener('change', cameraMove); // call this only in static scenes (i.e., if there is no animation loop)
controls.screenSpacePanning = true;
controls.minDistance = 1;
controls.maxDistance = space * 2;

controls.getOrbitTarget = createOrbitTargetFinder(context, camera, sceneGraph, () => {
    context.renderToFBO = true;
    renderer.render(context, camera, sceneGraph, layers, cube, impostorGeometry, impostorMaterial);
    context.renderToFBO = false;
});

const CUBE_ATTRIB_STRIDE = 2;

function makeMaterial(defines?: { [name: string]: any }) {
    let defs = '';
    if (defines)
        for (let [def, value] of Object.entries(defines))
            defs += `#define ${def} ${value}\n`

    let material = context.Material(
        vertexShader.replace('//DEFINESBLOCK', defs),
        fragmentShader.replace('//DEFINESBLOCK', defs));

    return material;
}

function makeCubeLayer(name: string, texturePath: string, defines?: { [name: string]: any }) {
    const stride = 28; // vec3 pos, vec3 normal, fp16*2  => 6 * 4 + 2 * 2 => 24B
    const stridef = (stride / 4) | 0;
    const tris = 6;  // 3 faces * 2 tris each (we flip based on camera)
    const cubeBuffer = new ArrayBuffer(stride * tris * 3);

    // the following typed arrays share the same buffer
    const bf32 = new Float32Array(cubeBuffer);
    const bu8 = new Uint8Array(cubeBuffer);

    const cb = vec3.create();
    const ab = vec3.create();
    function addTri(pA: vec3, pB: vec3, pC: vec3, i: number) {
        // flat face normals
        vec3.sub(cb, pC, pB);
        vec3.sub(ab, pA, pB);
        vec3.cross(cb, cb, ab);
        vec3.normalize(cb, cb);
        const nx = cb[0];
        const ny = cb[1];
        const nz = cb[2];

        let o = i * stridef * 3;
        bf32[o++] = pA[0];
        bf32[o++] = pA[1];
        bf32[o++] = pA[2];
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o++;
        bf32[o++] = pB[0];
        bf32[o++] = pB[1];
        bf32[o++] = pB[2];
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o++;
        bf32[o++] = pC[0];
        bf32[o++] = pC[1];
        bf32[o++] = pC[2];
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o++;

        o = i * stride * 3 + 24;
        if (i % 2 == 0) {
            bu8[o] = 0;
            bu8[o + 1] = 255;
            o += stride;
            bu8[o] = 255;
            bu8[o + 1] = 255;
            o += stride;
            bu8[o] = 255;
            bu8[o + 1] = 0;
        } else {
            bu8[o] = 255;
            bu8[o + 1] = 0;
            o += stride;
            bu8[o] = 0;
            bu8[o + 1] = 0;
            o += stride;
            bu8[o] = 0;
            bu8[o + 1] = 255;
        }
    }

    function addQuad(pA: vec3, pB: vec3, pC: vec3, pD: vec3, i: number) {
        addTri(pA, pB, pC, i);
        addTri(pC, pD, pA, i + 1);
    }

    const FLD = vec3.create(), FLU = vec3.create(),
        FRD = vec3.create(), FRU = vec3.create(),
        BLD = vec3.create(), BLU = vec3.create(),
        BRD = vec3.create(), BRU = vec3.create();

    // cubes have 8 vertices
    // OpenGL/Minecraft: +X = East, +Y = Up, +Z = South
    // Front/Back, Left/Right, Up/Down
    vec3.set(FLD, 0, 0, 1);
    vec3.set(FLU, 0, 1, 1);
    vec3.set(FRD, 1, 0, 1);
    vec3.set(FRU, 1, 1, 1);
    vec3.set(BLD, 0, 0, 0);
    vec3.set(BLU, 0, 1, 0);
    vec3.set(BRD, 1, 0, 0);
    vec3.set(BRU, 1, 1, 0);

    // Note: "front face" is CCW
    addQuad(FLU, BLU, BLD, FLD, 0);  // L+R
    addQuad(FRU, FLU, FLD, FRD, 2);  // F+B
    addQuad(FLU, FRU, BRU, BLU, 4);  // U+D

    let geometry = context.Geometry();

    geometry.setAttributes({
        position: { data: bf32, numComponents: 3, stride: stride, offset: 0 },
        normal: { data: bf32, numComponents: 3, stride: stride, offset: 12 },
        uv: { data: bu8, numComponents: 2, stride: stride, offset: 24 },
    });

    geometry.verts = tris * 3;

    let material = makeMaterial(defines);

    let texture = context.loadTexture(texturePath, render);

    return new renderer.InstancedLayer(geometry, material, texture, name);
}

function makeCrossLayer(name: string, texturePath: string, defines?: { [name: string]: any }) {
    const stride = 28; // vec3 pos, vec3 normal, fp16*2  => 6 * 4 + 2 * 2 => 24B
    const stridef = (stride / 4) | 0;
    const tris = 4;  // 2 faces * 2 tris each (double-sided)
    const cubeBuffer = new ArrayBuffer(stride * tris * 3);

    // the following typed arrays share the same buffer
    const bf32 = new Float32Array(cubeBuffer);
    const bu8 = new Uint8Array(cubeBuffer);

    const cb = vec3.create();
    const ab = vec3.create();
    function addTri(pA: vec3, pB: vec3, pC: vec3, i: number) {
        // flat face normals
        vec3.sub(cb, pC, pB);
        vec3.sub(ab, pA, pB);
        vec3.cross(cb, cb, ab);
        vec3.normalize(cb, cb);
        const nx = cb[0];
        const ny = cb[1];
        const nz = cb[2];

        let o = i * stridef * 3;
        bf32[o++] = pA[0];
        bf32[o++] = pA[1];
        bf32[o++] = pA[2];
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o++;
        bf32[o++] = pB[0];
        bf32[o++] = pB[1];
        bf32[o++] = pB[2];
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o++;
        bf32[o++] = pC[0];
        bf32[o++] = pC[1];
        bf32[o++] = pC[2];
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o++;

        o = i * stride * 3 + 24;
        if (i % 2 == 0) {
            bu8[o] = 0;
            bu8[o + 1] = 255;
            o += stride;
            bu8[o] = 255;
            bu8[o + 1] = 255;
            o += stride;
            bu8[o] = 255;
            bu8[o + 1] = 0;
        } else {
            bu8[o] = 255;
            bu8[o + 1] = 0;
            o += stride;
            bu8[o] = 0;
            bu8[o + 1] = 0;
            o += stride;
            bu8[o] = 0;
            bu8[o + 1] = 255;
        }
    }

    function addQuad(pA: vec3, pB: vec3, pC: vec3, pD: vec3, i: number) {
        addTri(pA, pB, pC, i);
        addTri(pC, pD, pA, i + 1);
    }

    const FLD = vec3.create(), FLU = vec3.create(),
        FRD = vec3.create(), FRU = vec3.create(),
        BLD = vec3.create(), BLU = vec3.create(),
        BRD = vec3.create(), BRU = vec3.create();

    // cubes have 8 vertices
    // OpenGL/Minecraft: +X = East, +Y = Up, +Z = South
    // Front/Back, Left/Right, Up/Down
    vec3.set(FLD, 0, 0, 1);
    vec3.set(FLU, 0, 1, 1);
    vec3.set(FRD, 1, 0, 1);
    vec3.set(FRU, 1, 1, 1);
    vec3.set(BLD, 0, 0, 0);
    vec3.set(BLU, 0, 1, 0);
    vec3.set(BRD, 1, 0, 0);
    vec3.set(BRU, 1, 1, 0);

    // Note: "front face" is CCW
    addQuad(FRU, BLU, BLD, FRD, 0);  // L+R
    addQuad(BRU, FLU, FLD, BRD, 2);  // F+B

    let geometry = context.Geometry();

    geometry.setAttributes({
        position: { data: bf32, numComponents: 3, stride: stride, offset: 0 },
        normal: { data: bf32, numComponents: 3, stride: stride, offset: 12 },
        uv: { data: bu8, numComponents: 2, stride: stride, offset: 24 },
    });

    geometry.verts = tris * 3;

    let material = makeMaterial(defines);

    let texture = context.loadTexture(texturePath, render);

    return new renderer.InstancedLayer(geometry, material, texture, name);
}

function makeCropLayer(name: string, texturePath: string, defines?: { [name: string]: any }) {
    const stride = 28; // vec3 pos, vec3 normal, fp16*2  => 6 * 4 + 2 * 2 => 24B
    const stridef = (stride / 4) | 0;
    const tris = 8;  // 2 faces * 2 tris each (double-sided)
    const cubeBuffer = new ArrayBuffer(stride * tris * 3);

    // the following typed arrays share the same buffer
    const bf32 = new Float32Array(cubeBuffer);
    const bu8 = new Uint8Array(cubeBuffer);

    const cb = vec3.create();
    const ab = vec3.create();
    function addTri(pA: vec3, pB: vec3, pC: vec3, i: number) {
        // flat face normals
        vec3.sub(cb, pC, pB);
        vec3.sub(ab, pA, pB);
        vec3.cross(cb, cb, ab);
        vec3.normalize(cb, cb);
        const nx = cb[0];
        const ny = cb[1];
        const nz = cb[2];

        let o = i * stridef * 3;
        bf32[o++] = pA[0];
        bf32[o++] = pA[1];
        bf32[o++] = pA[2];
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o++;
        bf32[o++] = pB[0];
        bf32[o++] = pB[1];
        bf32[o++] = pB[2];
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o++;
        bf32[o++] = pC[0];
        bf32[o++] = pC[1];
        bf32[o++] = pC[2];
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o++;

        o = i * stride * 3 + 24;
        if (i % 2 == 0) {
            bu8[o] = 0;
            bu8[o + 1] = 255;
            o += stride;
            bu8[o] = 255;
            bu8[o + 1] = 255;
            o += stride;
            bu8[o] = 255;
            bu8[o + 1] = 0;
        } else {
            bu8[o] = 255;
            bu8[o + 1] = 0;
            o += stride;
            bu8[o] = 0;
            bu8[o + 1] = 0;
            o += stride;
            bu8[o] = 0;
            bu8[o + 1] = 255;
        }
    }

    function addQuad(pA: vec3, pB: vec3, pC: vec3, pD: vec3, i: number) {
        addTri(pA, pB, pC, i);
        addTri(pC, pD, pA, i + 1);
    }

    function v(x: number, y: number, z: number) {
        return vec3.fromValues(x / 16, y / 16, z / 16);
    }

    // Note: "front face" is CCW
    addQuad(v(4, 15, 16), v(4, 15, 0), v(4, -1, 0), v(4, -1, 16), 0);
    addQuad(v(12, 15, 16), v(12, 15, 0), v(12, -1, 0), v(12, -1, 16), 2);
    addQuad(v(16, 15, 4), v(0, 15, 4), v(0, -1, 4), v(16, -1, 4), 4);
    addQuad(v(16, 15, 12), v(0, 15, 12), v(0, -1, 12), v(16, -1, 12), 6);

    let geometry = context.Geometry();

    geometry.setAttributes({
        position: { data: bf32, numComponents: 3, stride: stride, offset: 0 },
        normal: { data: bf32, numComponents: 3, stride: stride, offset: 12 },
        uv: { data: bu8, numComponents: 2, stride: stride, offset: 24 },
    });

    geometry.verts = tris * 3;

    let material = makeMaterial(defines);

    let texture = context.loadTexture(texturePath, render);

    return new renderer.InstancedLayer(geometry, material, texture, name);
}

function makeCube() {
    const stride = 12;
    const stridef = (stride / 4) | 0;
    const tris = 12;  // 6 faces * 2 tris each
    const cubeBuffer = new ArrayBuffer(stride * tris * 3);

    // the following typed arrays share the same buffer
    const bf32 = new Float32Array(cubeBuffer);

    function addTri(pA: vec3, pB: vec3, pC: vec3, i: number) {
        let o = i * stridef * 3;
        bf32[o++] = pA[0];
        bf32[o++] = pA[1];
        bf32[o++] = pA[2];
        bf32[o++] = pB[0];
        bf32[o++] = pB[1];
        bf32[o++] = pB[2];
        bf32[o++] = pC[0];
        bf32[o++] = pC[1];
        bf32[o++] = pC[2];
    }

    function addQuad(pA: vec3, pB: vec3, pC: vec3, pD: vec3, i: number) {
        addTri(pA, pB, pC, i);
        addTri(pC, pD, pA, i + 1);
    }

    const FLD = vec3.create(), FLU = vec3.create(),
        FRD = vec3.create(), FRU = vec3.create(),
        BLD = vec3.create(), BLU = vec3.create(),
        BRD = vec3.create(), BRU = vec3.create();

    // cubes have 8 vertices
    // OpenGL/Minecraft: +X = East, +Y = Up, +Z = South
    // Front/Back, Left/Right, Up/Down
    vec3.set(FLD, 0, 0, 1);
    vec3.set(FLU, 0, 1, 1);
    vec3.set(FRD, 1, 0, 1);
    vec3.set(FRU, 1, 1, 1);
    vec3.set(BLD, 0, 0, 0);
    vec3.set(BLU, 0, 1, 0);
    vec3.set(BRD, 1, 0, 0);
    vec3.set(BRU, 1, 1, 0);

    // Note: "front face" is CCW
    addQuad(FLU, BLU, BLD, FLD, 0);  // L
    addQuad(FRD, BRD, BRU, FRU, 2);  // R
    addQuad(FRU, FLU, FLD, FRD, 4);  // F
    addQuad(BRD, BLD, BLU, BRU, 6);  // B
    addQuad(FLU, FRU, BRU, BLU, 8);  // U
    addQuad(BLD, BRD, FRD, FLD, 10); // D

    let geometry = context.Geometry();

    geometry.setAttributes({
        position: { data: bf32, numComponents: 3, offset: 0 },
    });

    let material = context.Material(
        `
    attribute vec3 position;
    uniform vec3 scale;
    uniform vec3 offset;
    uniform mat4 modelViewMatrix;
    uniform mat4 projectionMatrix;
    varying lowp vec4 vColor;
    void main(void) {
      gl_Position = projectionMatrix * modelViewMatrix * vec4(position * scale + offset, 1);
      vColor = vec4(1.0);
    }
  `,
        `
    varying lowp vec4 vColor;
    void main(void) {
      gl_FragColor = vColor;
    }
  `
    );

    return new renderer.Mesh(geometry, material);

}

let cube = makeCube();


const layerNames = ["CUBE", "VOXEL", "CROSS", "CROP", "CUBOID", "CUBE_FALLBACK"]
let layers = [
    makeCubeLayer("CUBE", "textures/atlas0.png"),
    makeCubeLayer("VOXEL", "textures/atlas1.png", { VOXEL: 1 }),
    makeCrossLayer("CROSS", "textures/atlas2.png", { CROSS: 1 }),
    makeCropLayer("CROP", "textures/atlas3.png", { CROSS: 1 }),
    makeCubeLayer("CUBOID", "textures/atlas4.png", { CUBOID: 1 }),
    makeCubeLayer("CUBE_FALLBACK", "textures/atlas5.png", { WATER_ID: 1, FALLBACK: 1 })
];

let willRender = false;

function render() {
    // https://threejsfundamentals.org/threejs/lessons/threejs-rendering-on-demand.html
    if (!willRender) {
        willRender = true;
        requestAnimationFrame(renderFrame);
    }
}


let lastView = mat4.create();
let rerenderTimer: number;
function renderFrame() {
    willRender = false;
    if (!ONDEMAND) render();
    stats.begin();

    controls.update();

    if (!mat4.exactEquals(lastView, camera.view)) {
        // trigger a rerender for trailing occlusion queries
        // when the camera has moved
        if (rerenderTimer)
            clearTimeout(rerenderTimer);
        rerenderTimer = window.setTimeout(render, 100);
    }
    mat4.copy(lastView, camera.view);


    renderer.render(context, camera, sceneGraph, layers, cube, impostorGeometry, impostorMaterial);

    // Dynamic loading pass: trigger loads for any missing visible elements
    updateDynamicLoading();

    stats.end();
};

function updateDynamicLoading() {
    if (!sceneGraph.mapMetadata) {
        return; // wait for metadata to load
    }
    const cullResults = sceneGraph.cull(camera);

    // Trigger regionlet fetches (network queue handles limits)
    for (const rlet of cullResults.missingRegionlets) {
        fetchRegion(rlet.rx, rlet.rz, rlet.off);
    }

    // Trigger impostor fetches (network queue handles limits)
    for (const lod of cullResults.missingImpostors) {
        fetchRegionLOD(lod.rx, lod.rz, sceneGraph, camera.position, render);
    }
}

render();

function sleep(ms: number) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

async function* asyncIterableFromStream(stream: ReadableStream<Uint8Array>): AsyncIterator<Uint8Array, Uint8Array> {
    const reader = stream.getReader();

    // transparently decompress GZIP by checking the magic bytes first
    {
        let { done, value } = await reader.read();
        if (value[0] == 0x1f && value[1] == 0x8b) {
            // GZIP-compressed
            let decomp = new Gunzip();
            let chunks: Uint8Array[] = [];
            decomp.ondata = (chunk: Uint8Array) => chunks.push(chunk);
            decomp.push(value, done);

            yield* chunks;
            chunks = [];

            while (!done) {
                ({ done, value } = await reader.read());
                if (value) {
                    decomp.push(value, done);
                }
                yield* chunks;
                chunks = [];
            }
            return;
        }
        yield value;
    }
    while (true) {
        const { done, value } = await reader.read();
        if (done) {
            return;
        }
        yield value;
    }
}

const impostorGeometry = makeImpostorGeometry(context.gl);
const impostorMaterial = new renderer.Material(context.gl, impostorVertexShader, impostorFragmentShader);

function fetchRegion(x: number, z: number, off: number) {
    const key = `${x},${z},${off}`;
    const region = sceneGraph.getOrCreateRegion(x, z);
    const regionlet = region.regionlets[off];
    if (regionlet.status !== 'NONE') {
        return;
    }

    const meta = sceneGraph.mapMetadata;
    if (meta && meta.loaded && !meta.full_regions.has(`${x}.${z}`)) {
        sceneGraph.updateRegionletStatus(x, z, off, 'ERROR');
        return;
    }

    sceneGraph.updateRegionletStatus(x, z, off, 'FETCH');

    // Calculate priority based on distance to the regionlet center
    const rletCenter = vec3.fromValues(
        x * 512 + (off & 1) * 256 + 128,
        120,
        z * 512 + (off & 2) * 128 + 128
    );
    const priority = vec3.sqrDist(camera.position, rletCenter);

    sceneGraph.requestManager.enqueue({
        type: 'REGIONLET',
        key: `regionlet:${key}`,
        priority,
        run: async () => {
            const controller = new AbortController();
            const { signal } = controller;
            try {
                const response = await fetch(`map/r.${x}.${z}.${off}.cmt`, { signal });
                if (!response.ok) {
                    if (response.status == 404) {
                        throw new Error("404");
                    }
                    throw new Error(`failed to fetch cmt: ${response.statusText}`);
                }

                sceneGraph.updateRegionletStatus(x, z, off, 'STREAM');

                const stream = asyncIterableFromStream(response.body);
                const headerObj = await stream.next();
                if (headerObj.done || !headerObj.value) {
                    throw new Error("Empty stream or missing header");
                }
                const header = headerObj.value;
                const magic = new TextDecoder("utf-8").decode(header.subarray(0, 8));
                if (magic != "COMTE00\n") {
                    controller.abort();
                    throw new Error(`invalid comte data file magic: ${magic}`);
                }
                const headerLength = new Uint32Array(header.slice(8, 8 + 4).buffer)[0];
                let meta = JSON.parse(new TextDecoder("utf-8").decode(header.subarray(12, 12 + headerLength)));

                let sectionLengths: Array<number> = meta.layers.map((l: { length: number }) => l.length);
                let length = sectionLengths.reduce((a, b) => a + b);

                let value = header.subarray(12 + headerLength);
                let done = false;

                console.debug("streaming", response.url, (length / 1024) | 0, "KiB, sections", meta, sectionLengths);

                const chunk = regionlet.chunk;

                let layerSpecs: any = {};
                for (const layer of meta.layers) {
                    layerSpecs[layer.name] = {
                        data: new Uint32Array(layer.length / 4), retain: true,
                        numComponents: CUBE_ATTRIB_STRIDE, stride: CUBE_ATTRIB_STRIDE * 4, divisor: 1
                    };
                }

                chunk.setLayers(layerSpecs);



                let offset = 0;
                let layerNumber = 0;

                while (!done) {
                    if (value.length == 0) {
                        ({ value, done } = await stream.next());
                        continue;
                    }

                    let wanted = Math.min(value.length, sectionLengths[layerNumber] - offset);
                    let tail = value.subarray(wanted);
                    value = value.subarray(0, wanted);

                    let layerName = meta.layers[layerNumber].name;
                    chunk.updateAttribute(layerName, value, offset);
                    offset += value.length;
                    chunk.layers[layerName].size = Math.floor(offset / (CUBE_ATTRIB_STRIDE * 4));

                    value = tail;

                    while (offset === sectionLengths[layerNumber]) {
                        offset = 0;
                        layerNumber++;
                    }
                    render();
                    sceneGraph.notify();
                }

                let minY = 255, maxY = 0;
                for (const [name, value] of Object.entries(chunk.layers)) {
                    if (value.data) {
                        const buf = new Uint8Array(value.data);
                        for (let o = 0; o < buf.length; o += value.stride) {
                            let y = buf[o];
                            minY = Math.min(minY, y);
                            maxY = Math.max(maxY, y);
                        }
                        value.data = null;
                    }
                }
                chunk.minY = minY;
                chunk.maxY = maxY;

                sceneGraph.updateRegionletStatus(x, z, off, 'READY');
                console.debug("done streaming", response.url, minY, maxY);
                render();
            } catch (e) {
                sceneGraph.updateRegionletStatus(x, z, off, 'ERROR');
                if ((e as Error).message == "404") {
                    return;
                }
                console.warn(`CMT loading failed for regionlet ${x},${z},${off}:`, e);
                throw e;
            }
        }
    });
}

// interesting coords:
// Novigrad: r.{0..3}.{0..3} -1.5, -2.8]

// fetchRegion(1,1,0,-1.2,-1.2);

function fetchRange(xs: number, xe: number, zs: number, ze: number, angle: number, xo: number, zo: number, pad: number = 4) {
    for (let o = 0; o < 4; o++) {
        for (let x = xs; x <= xe; x++) {
            for (let z = zs; z <= ze; z++) {
                fetchRegion(x, z, o);
            }
        }
    }
    // Fetch region LODs for a grid centered on the high-res area
    for (let x = xs - pad; x <= xe + pad; x++) {
        for (let z = zs - pad; z <= ze + pad; z++) {
            fetchRegionLOD(x, z, sceneGraph, camera.position, render);
        }
    }
    vec3.set(camera.position, xo * 512, 120, zo * 512);
    vec3.add(controls.target, camera.position, vec3.rotateY(vec3.create(), vec3.fromValues(256, -40, 0), vec3.create(), angle * Math.PI / 180));
    controls.update();
}

const cuboidTextureData = new Uint32Array(512 * 512 * 4);

const gl = context.gl;
const cuboidDataTexture = twgl.createTexture(gl, {
    target: gl.TEXTURE_2D,
    internalFormat: gl.RGBA32UI,
    width: 512,
    height: 512,
    format: gl.RGBA_INTEGER,
    type: gl.UNSIGNED_INT,
    src: cuboidTextureData,
    minMag: gl.NEAREST,
    wrap: gl.CLAMP_TO_EDGE,
});

context.cuboidDataTex = cuboidDataTexture;
context.cuboidTextureData = cuboidTextureData;

fetch("textures/cuboid_metadata.bin.gz")
    .then(r => r.arrayBuffer())
    .then(arrayBuffer => {
        const decompressed = gunzipSync(new Uint8Array(arrayBuffer));
        const metadataUint32 = new Uint32Array(
            decompressed.buffer,
            decompressed.byteOffset,
            decompressed.byteLength / 4
        );
        const copyLength = Math.min(metadataUint32.length, cuboidTextureData.length);
        cuboidTextureData.set(metadataUint32.subarray(0, copyLength));

        twgl.setTextureFromArray(gl, cuboidDataTexture, cuboidTextureData, {
            target: gl.TEXTURE_2D,
            internalFormat: gl.RGBA32UI,
            format: gl.RGBA_INTEGER,
            type: gl.UNSIGNED_INT,
            width: 512,
            height: 512,
        });
        render();
    })
    .catch(err => {
        console.error("failed to load cuboid_metadata.bin.gz", err);
    });

metadataLoadingPromise.then(() => {
    const choice: string = 'greenfield';
    switch (choice) {
        case '2b2t': fetchRange(-129, -129, -11, -11, 0, -129, -11, 5); break;
        case 'novitest': fetchRange(1, 1, 1, 1, 130, 1.3, 1.4); break;
        case 'novigrad': fetchRange(0, 3, 0, 3, 130, 2.3, 3.4); break;
        case 'greenfield': fetchRange(2, 2, 2, 2, 0, 0, 0, 15); break;
        case 'hermit': fetchRange(-1, -1, -1, -1, 0, 0, 0); break;
        case 'test': fetchRange(0, 0, 0, 0, 0, 0, 0); break;
        default: case 'center': fetchRange(-1, 1, -1, 1, 90, 0, 0); break;
    }

    maybeSetCameraFromLocstring();
    updateDynamicLoading();
});

// --- DEBUG MENU & REGION INSPECTOR SETUP ---
setupGUI(sceneGraph, controls, context, render);

