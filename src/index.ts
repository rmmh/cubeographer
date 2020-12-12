/*
TODO: open addressing for block insertion/deletion
steal from https://shlegeris.com/2017/01/06/hash-maps.html


*/

import { mat4, vec3 } from 'gl-matrix';

var vertexShader = require('./cube_vertex.glsl');
var fragmentShader = require('./cube_fragment.glsl');

import * as renderer from './renderer';
import { OrbitControls } from './camera';

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

const scene: Set<renderer.Chunk> = new Set();

const stats = new Stats();
stats.showPanel(0); // 0: fps, 1: ms, 2: mb, 3+: custom
document.body.appendChild(stats.dom);

context.setClearColor(0x7e, 0xab, 0xff);

// TODO: replace these controls with block-based ones,
// i.e. rotate around the click target
let controls = new OrbitControls(camera, context.canvas);
controls.addEventListener('change', render); // call this only in static scenes (i.e., if there is no animation loop)
controls.screenSpacePanning = true;
controls.minDistance = 1;
controls.maxDistance = space * 2;

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
        position: {data: bf32, numComponents: 3, stride: stride, offset: 0},
        normal: {data: bf32, numComponents: 3, stride: stride, offset: 12},
        uv: {data: bu8, numComponents: 2, stride: stride, offset: 24},
    });

    geometry.verts = tris * 3;

    let material = makeMaterial(defines);

    let texture = context.loadTexture(texturePath, render);

    return new renderer.InstancedLayer(geometry, material, texture, name);
}

function makeCrossLayer(name: string, texturePath: string, defines?: {[name: string]: any}) {
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


const layerNames = ["CUBE", "CROSS", "CUBE_FALLBACK"]
let layers = [
    makeCubeLayer("CUBE", "textures/atlas0.png"),
    makeCrossLayer("CROSS", "textures/atlas1.png", {CROSS: 1}),
    makeCubeLayer("CUBE_FALLBACK", "textures/atlas2.png", {WATER_ID: 2, FALLBACK: 1})
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


    renderer.render(context, camera, scene, layers, cube);

    stats.end();
};

render();

function sleep(ms: number) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

async function* asyncIterableFromStream(stream: ReadableStream<Uint8Array>): AsyncIterator<Uint8Array, Uint8Array> {
    const reader = stream.getReader();

    // assumption: the header is in the first chunk returned here
    {
        const { done, value } = await reader.read();
        const headerLen = 8 + 4 * 3;
        yield value.subarray(0, headerLen);
        yield value.subarray(headerLen);
    }
    while (true) {
        const { done, value } = await reader.read();
        if (done) {
            return;
        }
        yield value;
    }
}

function fetchRegion(x: number, z: number, off: number) {
    const controller = new AbortController();
    const { signal } = controller;
    fetch(`map/r.${x}.${z}.${off}.cmt`, { signal }).then(
        async response => {
            if (!response.ok) {
                return;
            }

            const stream = asyncIterableFromStream(response.body);
            const header = (await stream.next()).value;
            const magic = new TextDecoder("utf-8").decode(header.subarray(0, 8));
            if (magic != "COMTE00\n") {
                console.error(`invalid comte data file (expected magic "COMTE00\\n", got "${magic}"`);
                controller.abort();
                return;
            }
            const sectionLengths = new Uint32Array(header.slice(8).buffer);
            let length = 0;
            for (const sectionLength of sectionLengths) {
                length += sectionLength;
            }

            console.debug("streaming", response.url, (length / 1024) | 0, "KiB, sections", Array.from(sectionLengths).toString());

            let chunk = context.Chunk();

            vec3.set(chunk.position, x * 512 + (off&1) * 256, 0, z * 512 + (off&2) * 128);

            chunk.setLayers({
                CUBE: { data: new Uint32Array(sectionLengths[0]/4), retain: true,
                    numComponents: CUBE_ATTRIB_STRIDE, stride: CUBE_ATTRIB_STRIDE * 4, divisor: 1 },
                CROSS: { data: new Uint32Array(sectionLengths[1]/4), retain: true,
                    numComponents: CUBE_ATTRIB_STRIDE, stride: CUBE_ATTRIB_STRIDE * 4, divisor: 1 },
                CUBE_FALLBACK: { data: new Uint32Array(sectionLengths[2]/4), retain: true,
                    numComponents: CUBE_ATTRIB_STRIDE, stride: CUBE_ATTRIB_STRIDE * 4, divisor: 1 },
            });

            scene.add(chunk);

            let offset = 0;
            let layerNumber = 0;

            let { value, done } = await stream.next();
            while (true) {
                if (done) break;
                if (value.length == 0) {
                    ({value, done} = await stream.next());
                    continue;
                }

                let wanted = Math.min(value.length, sectionLengths[layerNumber] - offset);
                let tail = value.subarray(wanted);
                value = value.subarray(0, wanted);

                chunk.updateAttribute(layerNames[layerNumber], value, offset);
                offset += value.length;
                chunk.layers[layerNames[layerNumber]].size = Math.floor(offset / (CUBE_ATTRIB_STRIDE * 4));

                value = tail;

                if (offset == sectionLengths[layerNumber]) {
                    offset = 0;
                    layerNumber++;
                }
                render();
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
            console.debug("done streaming", response.url, minY, maxY);
        },
        reason => console.log("rejected", reason)
    );
}

// interesting coords:
// Novigrad: r.{0..3}.{0..3} -1.5, -2.8]

// fetchRegion(1,1,0,-1.2,-1.2);

function fetchRange(xs: number, xe: number, zs: number, ze: number, angle: number, xo: number, zo: number) {
    for (let x = xs; x <= xe; x++) {
        for (let z = zs; z <= ze; z++) {
            for (let o = 0; o < 4; o++) {
                fetchRegion(x, z, o);
            }
        }
    }
    vec3.copy(camera.position, vec3.fromValues(xo * 512, 120, zo * 512));
    vec3.add(controls.target, camera.position, vec3.rotateY(vec3.create(), vec3.fromValues(256, -40, 0), vec3.create(), angle * Math.PI / 180));
    controls.update();

}

if (0)
fetchRange(-1, 1, -1, 1, 90, 0, 0);

if (1) // novigrad (FULL)
fetchRange(0, 3, 0, 3, 130, 2.3, 3.4);
else if (0)
fetchRange(1, 1, 1, 1, 130, 2.3, 3.4);
else
if (1) // novigrad (TEST)
fetchRange(1, 2, 1, 3, 130, 2.3, 3.4);
