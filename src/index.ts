/*
TODO: open addressing for block insertion/deletion
steal from https://shlegeris.com/2017/01/06/hash-maps.html


*/

import { vec3 } from 'gl-matrix';

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
controls.addEventListener( 'change', render ); // call this only in static scenes (i.e., if there is no animation loop)
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


const layerNames = ["CUBE", "CROSS", "CUBE_FALLBACK"]
let layers = [
    makeCubeLayer("CUBE", "textures/atlas0.png"),
    makeCrossLayer("CROSS", "textures/atlas1.png", {CROSS: 1}),
    makeCubeLayer("CUBE_FALLBACK", "textures/atlas2.png", {WATER_ID: 2})
];

let willRender = false;

function render() {
    // https://threejsfundamentals.org/threejs/lessons/threejs-rendering-on-demand.html
    if (!willRender) {
        willRender = true;
        requestAnimationFrame(renderFrame);
    }
}


function renderFrame() {
    willRender = false;
    if (!ONDEMAND) render();

    stats.begin();

    controls.update();

    renderer.render(context, camera, scene, layers);

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
        yield value.slice(0, headerLen);
        yield value.slice(headerLen);
    }
    while (true) {
        const { done, value } = await reader.read();
        if (done) {
            return;
        }
        yield value;
    }
}

function fetchRegion(x: number, z: number, off: number, xo: number, zo: number) {
    const controller = new AbortController();
    const { signal } = controller;
    fetch(`map/r.${x}.${z}.${off}.cmt`, { signal }).then(
        async response => {
            if (!response.ok) {
                return;
            }

            const stream = asyncIterableFromStream(response.body);
            const header = (await stream.next()).value;
            const magic = new TextDecoder("utf-8").decode(header.slice(0, 8));
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

            vec3.set(chunk.position, (x + xo) * 512 + (off&1) * 256, 0, (z + zo) * 512 + (off&2) * 128);

            chunk.setLayers({
                CUBE: { data: new Uint32Array(sectionLengths[0]), numComponents: CUBE_ATTRIB_STRIDE, stride: CUBE_ATTRIB_STRIDE * 4, divisor: 1 },
                CROSS: { data: new Uint32Array(sectionLengths[1]), numComponents: CUBE_ATTRIB_STRIDE, stride: CUBE_ATTRIB_STRIDE * 4, divisor: 1 },
                CUBE_FALLBACK: { data: new Uint32Array(sectionLengths[2]), numComponents: CUBE_ATTRIB_STRIDE, stride: CUBE_ATTRIB_STRIDE * 4, divisor: 1 },
            });

            /*
            // TODO: center this more conservatively based on observed y-height?
            let center = new THREE.Vector3(128, 128, 128);
            let dist = center.distanceTo(new THREE.Vector3(0,0,0));
            mesh.geometry.boundingSphere = new THREE.Sphere(center, dist);
            mesh.frustumCulled = true;
            let sph = new THREE.SphereGeometry(mesh.geometry.boundingSphere.radius, 10, 10);
            let smesh = new THREE.Mesh(sph, new THREE.MeshBasicMaterial({wireframe: true}));
            smesh.position.add(center);
            smesh.position.add(mesh.position);
            // scene.add(smesh);
            */
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
                let tail = value.slice(wanted);
                value = value.slice(0, wanted);

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
            console.debug("done streaming", response.url, chunk.layers);
        },
        reason => console.log("rejected", reason)
    );
}

// interesting coords:
// Novigrad: r.{0..3}.{0..3} -1.5, -2.8]

// fetchRegion(1,1,0,-1.2,-1.2);

if (0)
for (let x = -1; x <= 1; x++) {
    for (let z = -1; z <= 1; z++) {
        for (let o = 0; o < 4; o++)
            fetchRegion(x, z, o, 0, 0)
    }
}


if (1) // novigrad (TEST)
for (let x = 1; x <= 2; x++) {
    for (let z = 1; z <= 2; z++) {
        for (let o = 0; o < 4; o++)
            fetchRegion(x, z, o, -1.9, -2.8)
    }
}


if (0)
for (let x = 4; x <= 7; x++) {
    for (let z = 24; z <= 26; z++) {
        for (let o = 0; o < 4; o++)
            fetchRegion(x, z, o, -5, -25)
    }
}

if(0)
for (let x = 0; x <= 3; x++) {
    for (let z = 0; z <= 3; z++) {
        for (let o = 0; o < 4; o++)
            fetchRegion(x, z, o, -1.9, -3.1)
    }
}

if(0)
for (let x = -2; x <= 1; x++) {
    for (let z = -2; z <= 1; z++) {
        for (let o = 0; o < 4; o++)
            fetchRegion(x, z, o, 0, 0)
    }
}
