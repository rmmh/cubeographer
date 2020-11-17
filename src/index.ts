/*
TODO: open addressing for block insertion/deletion
steal from https://shlegeris.com/2017/01/06/hash-maps.html


*/

import * as THREE from "three";
import { AddEquation, InstancedBufferAttribute, MeshPhongMaterial } from "three";
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';

var vertexShader = require('./cube_vertex.glsl');
var fragmentShader = require('./cube_fragment.glsl');

const Stats = require("stats.js");

let ONDEMAND = true;

let PROD = 1;

const glitterCount = PROD ? 1e6 : 1e4;
const cubeCount = PROD ? 512 * 512 * 2 : 1024;
const space = PROD ? 512 : 64;

document.body.addEventListener('mouseleave', () => { ONDEMAND = true; });
document.body.addEventListener('mouseenter', () => { ONDEMAND = false; });

const scene = new THREE.Scene();
const camera = new THREE.PerspectiveCamera(75, window.innerWidth / window.innerHeight, 0.1, 2000);
const stats = new Stats();
stats.showPanel(0); // 0: fps, 1: ms, 2: mb, 3+: custom
document.body.appendChild(stats.dom);

const renderer = new THREE.WebGLRenderer();
renderer.setSize(window.innerWidth, window.innerHeight);
document.body.appendChild(renderer.domElement);

let controls = new OrbitControls(camera, renderer.domElement);

//controls.addEventListener( 'change', render ); // call this only in static scenes (i.e., if there is no animation loop)

controls.enableDamping = true; // an animation loop is required when either damping or auto-rotation are enabled
controls.dampingFactor = 0.05;
controls.screenSpacePanning = false;
controls.minDistance = 1;
controls.maxDistance = space * 1.2;

// controls.maxPolarAngle = Math.PI / 2;

window.addEventListener('resize', onWindowResize, false);
function onWindowResize() {
    camera.aspect = window.innerWidth / window.innerHeight;
    camera.updateProjectionMatrix();
    renderer.setSize(window.innerWidth, window.innerHeight);
    if (ONDEMAND) render();
}

function makeGlitter(particles: number): [THREE.Points, THREE.InterleavedBuffer, THREE.InterleavedBuffer] {
    const geometry = new THREE.BufferGeometry();

    // create a generic buffer of binary data (a single particle has 16 bytes of data)

    const arrayBuffer = new ArrayBuffer(particles * 16);

    // the following typed arrays share the same buffer

    const interleavedFloat32Buffer = new Float32Array(arrayBuffer);
    const interleavedUint8Buffer = new Uint8Array(arrayBuffer);

    const color = new THREE.Color();

    const n = space, n2 = n / 2; // particles spread in the cube

    for (let i = 0; i < interleavedFloat32Buffer.length; i += 4) {
        // position (first 12 bytes)
        const x = Math.random() * n - n2;
        const y = Math.random() * n - n2;
        const z = Math.random() * n - n2;

        interleavedFloat32Buffer[i + 0] = x;
        interleavedFloat32Buffer[i + 1] = y;
        interleavedFloat32Buffer[i + 2] = z;

        // color (last 4 bytes)

        const vx = (x / n) + 0.5;
        const vy = (y / n) + 0.5;
        const vz = (z / n) + 0.5;

        color.setRGB(vx, vy, vz);

        const j = (i + 3) * 4;

        interleavedUint8Buffer[j + 0] = color.r * 255;
        interleavedUint8Buffer[j + 1] = color.g * 255;
        interleavedUint8Buffer[j + 2] = color.b * 255;
        interleavedUint8Buffer[j + 3] = 0; // not needed
    }

    const interleavedBuffer32 = new THREE.InterleavedBuffer(interleavedFloat32Buffer, 4);
    const interleavedBuffer8 = new THREE.InterleavedBuffer(interleavedUint8Buffer, 16);

    geometry.setAttribute('position', new THREE.InterleavedBufferAttribute(interleavedBuffer32, 3, 0, false));
    geometry.setAttribute('color', new THREE.InterleavedBufferAttribute(interleavedBuffer8, 3, 12, true));

    const material = new THREE.PointsMaterial({ size: .3, vertexColors: true });

    return [new THREE.Points(geometry, material), interleavedBuffer32, interleavedBuffer8];
}

const CUBE_ATTRIB_STRIDE = 2;

function makeCubes(cubes: number): [THREE.Mesh, InstancedBufferAttribute] {
    // vec3 pos, normal, 24bit color => 6 * 4 + 3 + 1 (pad) => 28B
    // TODO: rewrite vertex shader to unpack 1B normals, 3B position => 8B each?
    const stride = 28; // vec3 pos, vec3 normal, fp16*2  => 6 * 4 + 2 * 2 => 24B
    const stridef = (stride / 4)|0;
    const tris = 6;  // 3 faces * 2 tris each (we flip based on camera)
    const cubeBuffer = new ArrayBuffer(stride * tris * 3);

    // the following typed arrays share the same buffer
    const bf32 = new Float32Array(cubeBuffer);
    const bu8 = new Uint8Array(cubeBuffer);

    const n = space;	// cubes spread in the cube

    const cb = new THREE.Vector3();
    const ab = new THREE.Vector3();
    function addTri(pA: THREE.Vector3, pB: THREE.Vector3, pC: THREE.Vector3, i: number) {
        // flat face normals
        cb.subVectors(pC, pB);
        ab.subVectors(pA, pB);
        cb.cross(ab);
        cb.normalize();
        const nx = cb.x;
        const ny = cb.y;
        const nz = cb.z;

        let o = i * stridef * 3;
        bf32[o++] = pA.x;
        bf32[o++] = pA.y;
        bf32[o++] = pA.z;
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o++;
        bf32[o++] = pB.x;
        bf32[o++] = pB.y;
        bf32[o++] = pB.z;
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o++;
        bf32[o++] = pC.x;
        bf32[o++] = pC.y;
        bf32[o++] = pC.z;
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o++;

        o = i * stride * 3 + 24;
        if (i % 2 == 0) {
            bu8[o] = 0;
            bu8[o + 1] = 1;
            o += stride;
            bu8[o] = 1;
            bu8[o + 1] = 1;
            o += stride;
            bu8[o] = 1;
            bu8[o + 1] = 0;
        } else {
            bu8[o] = 1;
            bu8[o + 1] = 0;
            o += stride;
            bu8[o] = 0;
            bu8[o + 1] = 0;
            o += stride;
            bu8[o] = 0;
            bu8[o + 1] = 1;
        }
    }

    function addQuad(pA: THREE.Vector3, pB: THREE.Vector3, pC: THREE.Vector3, pD: THREE.Vector3, i: number) {
        addTri(pA, pB, pC, i);
        addTri(pC, pD, pA, i + 1);
    }

    const FLD = new THREE.Vector3(), FLU = new THREE.Vector3(),
        FRD = new THREE.Vector3(), FRU = new THREE.Vector3(),
        BLD = new THREE.Vector3(), BLU = new THREE.Vector3(),
        BRD = new THREE.Vector3(), BRU = new THREE.Vector3();

    // cubes have 8 vertices
    // OpenGL/Minecraft: +X = East, +Y = Up, +Z = South
    // Front/Back, Left/Right, Up/Down
    FLD.set(0, 0, 1);
    FLU.set(0, 1, 1);
    FRD.set(1, 0, 1);
    FRU.set(1, 1, 1);
    BLD.set(0, 0, 0);
    BLU.set(0, 1, 0);
    BRD.set(1, 0, 0);
    BRU.set(1, 1, 0);

    // Note: "front face" is CCW
    addQuad(FLD, FRD, FRU, FLU, 0);  // F+B
    addQuad(FLD, FLU, BLU, BLD, 2);  // L+R
    addQuad(FLU, FRU, BRU, BLU, 4);  // U+D

    const attr = new Uint32Array(cubes * CUBE_ATTRIB_STRIDE);
    console.log("cube attr buf is", Math.round(attr.byteLength / 1024 / 1024), "MiB");

    for (let i = 0; i < cubes; i++) {
        // positions
        let x = (Math.random() * n) | 0;
        let y = (Math.random() * n) | 0;
        let z = (Math.random() * n) | 0;

        // colors
        let r = ((x / n) * 255) | 0, g = ((y / n) * 255) | 0, b = ((z / n) * 255) | 0;

        if (i < space * space) {
            x = i % space;
            z = i / space;
            y = space / 2 - 10 + Math.sin(x / 30) * 5 + Math.sin(z / 40) * 5;
            //r = ((x / n) * 255) | 0, g = ((y / n) * 255) | 0, b = ((z / n) * 255) | 0;
            //g = Math.min(255, g + 60);
            r = g = b = 255;
        }

        let o = i * CUBE_ATTRIB_STRIDE;
        attr[o] = (x << 20) | (y << 10) | z;
        attr[o+1] = ((Math.random() * 255) << 24) | (r << 16) | (g << 8) | b;
    }

    // shuffle attrs
    for (let i = attr.length / 2; i > 0;) {
        let j = Math.floor(Math.random() * i--);
        let t1 = attr[i*2], t2 = attr[i*2+1];
        attr[i*2] = attr[j*2], attr[i*2+1] = attr[j*2+1];
        attr[j*2] = t1, attr[j*2+1] = t2;
    }

    const geometry = new THREE.BufferGeometry();

    const ibf32 = new THREE.InterleavedBuffer(bf32, stridef);
    const ibu8 = new THREE.InterleavedBuffer(bu8, stride);
    const attrBuf = new THREE.InstancedBufferAttribute(attr, CUBE_ATTRIB_STRIDE);

    geometry.setAttribute('position', new THREE.InterleavedBufferAttribute(ibf32, 3, 0, false));
    geometry.setAttribute('normal', new THREE.InterleavedBufferAttribute(ibf32, 4, 3, false));
    geometry.setAttribute('uv', new THREE.InterleavedBufferAttribute(ibu8, 2, 24, false));
    geometry.setAttribute('attr', attrBuf);

    geometry.computeBoundingSphere();

    const tex = new THREE.TextureLoader().load(PROD ? 'textures/atlas.png' : 'textures/debug.png',
        () => render(),
    );
    tex.magFilter = THREE.NearestFilter;
    tex.flipY = true;

    const material: THREE.Material = new THREE.RawShaderMaterial({
        uniforms: {
            atlas: {value: tex},
            space: {value: space},
        },
        vertexShader,
        fragmentShader,
        side: THREE.DoubleSide,
        transparent: true
    });

    const mesh = new THREE.InstancedMesh(geometry, material, 0);
    // InstancedMesh by default has instanceMatrix (16 floats per instance),
    // creating it with 0 and then fudging the count lets us use our own attributes.
    mesh.count = cubes;

    return [mesh, attrBuf];

}

const geometry = new THREE.BoxGeometry();
const material = new THREE.MeshLambertMaterial({ color: 0x00ff00 });
const cube = new THREE.Mesh(geometry, material);
scene.add(cube);

let [glitter, glitterPos, glitterColor] = makeGlitter(glitterCount);
scene.add(glitter);

let [cubes, cubeAttr] = makeCubes(cubeCount);
scene.add(cubes);

var dLight = new THREE.DirectionalLight('#fff', 1);
dLight.position.set(-10, 15, 20);
var aLight = new THREE.AmbientLight('#111');

if (true) {
    const loader = new THREE.FontLoader();
    loader.load('fonts/helvetiker_regular.typeface.json', function (font) {
        let materials = [
            new THREE.MeshPhongMaterial({ color: 0xffffff, flatShading: true }), // front
            new THREE.MeshPhongMaterial({ color: 0xffffff }) // side
        ];
        for (let pos of [[10, 10, 10], [10, 10, -10], [10, -10, -10]]) {
            const geometry = new THREE.TextGeometry("" + pos, {
                font: font,
                size: 2,
                height: .1,
                curveSegments: 12,
            });
            let mesh = new THREE.Mesh(geometry, materials)
            mesh.position.set(pos[0], pos[1], pos[2]);
            scene.add(mesh);
        }
    });
}

scene.add(dLight);
scene.add(aLight);

camera.position.set(1, 1, 5);  // face northish
camera.lookAt(0, 0, 0);

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

    if (scene.children.includes(glitter) && glitterPos.array instanceof Float32Array) {
        let paa = glitterPos.array;
        let jitters = new Float32Array(128 + (Math.random() * 50)|0);
        for (let i = 0; i < jitters.length; i++) {
            jitters[i] = (Math.random() - 0.5) * 1;
        }
        const count = 10000;
        const offset = (Math.random() * (paa.length - count))|0;
        for (let i = offset; i < offset + count; i++) {
            if (i % 4 == 3) { i++; }
            paa[i] += jitters[i % jitters.length];
        }
        glitterPos.needsUpdate = true;
        glitterPos.updateRange = {offset, count};
    }

    if (scene.children.includes(cubes) && cubeAttr.array instanceof Uint32Array) {
        let paa = cubeAttr.array;
        let jitters = new Int8Array(1 << ((2 + Math.random() * 7) | 0));
        const jmask = jitters.length - 1;
        for (let i = 0; i < jitters.length; i++) {
            jitters[i] = ((Math.random() - 0.5) * 2.5)|0;
        }
        const count = 5000;
        const cs = CUBE_ATTRIB_STRIDE;
        const offset = cs * ((Math.random() * (paa.length/(cs) - count)) | 0);
        for (let i = offset; i < offset + count * cs; i += cs) {
            let x = paa[i] >> 20, y = (paa[i] >> 10) & 1023, z = paa[i] & 1023;
            x = Math.max(0, x + jitters[i & jmask]);
            y = Math.max(0, y + jitters[(i + 1) & jmask]);
            z = Math.max(0, z + jitters[(i + 2) & jmask]);
            paa[i] = (x&1023)<<20 | (y&1023) << 10 | (z&1023);
        }

        cubeAttr.needsUpdate = true;
        cubeAttr.updateRange = { offset, count: count * cs };
    }

    cube.rotation.x += 0.01;
    cube.rotation.y += 0.01;

    renderer.render(scene, camera);

    stats.end();
};

render();
controls.addEventListener('change', render);

/*
declare namespace Cubiomes {
    function _initBiomes(): void;
    function _malloc(bytes: number): number;

    function onRuntimeInitialized(): void;
}

Cubiomes.onRuntimeInitialized = function () {
    console.log("cubiomes intialized");
    Cubiomes._initBiomes();
    let layerStack = Cubiomes._malloc(500);
}

fetch("map/r.-1.-1.cmt").then(
    async response => {
        if (cubeAttr.array instanceof Uint32Array) {
            console.log("loaded", response.url);
            let paa = cubeAttr.array;

            cubeAttr.array = new Uint32Array(await response.arrayBuffer());


            const attrBuf = new THREE.InstancedBufferAttribute(cubeAttr.array, CUBE_ATTRIB_STRIDE);
            if (cubes.geometry instanceof THREE.BufferGeometry)
                cubes.geometry.setAttribute('attr', attrBuf);

            cubeAttr.needsUpdate = true;
            if (cubes instanceof THREE.InstancedMesh) {
                cubes.count = cubeAttr.array.length / CUBE_ATTRIB_STRIDE;
            }
        }
    },
    reason => console.log("rejected", reason)
)
*/
