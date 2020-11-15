import * as THREE from "three";
import { AddEquation, MeshPhongMaterial } from "three";
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';

const Stats = require("stats.js");

let ONDEMAND = true;

const glitterCount = 1e6;
const cubeCount = 512 * 512;

document.body.addEventListener('mouseleave', () => { ONDEMAND = true; });
document.body.addEventListener('mouseenter', () => { ONDEMAND = false; });

const scene = new THREE.Scene();
const camera = new THREE.PerspectiveCamera(75, window.innerWidth / window.innerHeight, 0.1, 1000);
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
controls.maxDistance = 500;

// controls.maxPolarAngle = Math.PI / 2;

window.addEventListener('resize', onWindowResize, false);
function onWindowResize() {
    camera.aspect = window.innerWidth / window.innerHeight;
    camera.updateProjectionMatrix();
    renderer.setSize(window.innerWidth, window.innerHeight);
    if (ONDEMAND) render();
}

function makeglitter(particles: number): [THREE.Points, THREE.InterleavedBuffer, THREE.InterleavedBuffer] {
    const geometry = new THREE.BufferGeometry();

    // create a generic buffer of binary data (a single particle has 16 bytes of data)

    const arrayBuffer = new ArrayBuffer(particles * 16);

    // the following typed arrays share the same buffer

    const interleavedFloat32Buffer = new Float32Array(arrayBuffer);
    const interleavedUint8Buffer = new Uint8Array(arrayBuffer);

    const color = new THREE.Color();

    const n = 512, n2 = n / 2; // particles spread in the cube

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

function makeCubes(cubes: number): [THREE.Mesh, THREE.InterleavedBuffer, THREE.InterleavedBuffer] {
    const geometry = new THREE.BufferGeometry();

    // vec3 pos, normal, 24bit color => 6 * 4 + 3 + 1 (pad) => 28B
    // TODO: rewrite vertex shader to unpack 1B normals, 3B position => 8B each?
    const stride = 28;
    const stridef = (stride / 4)|0;
    const tris = 12;  // 6 faces * 2 tris each -- 28B * 6 * 2 * 3 = 1008B each!
    const arrayBuffer = new ArrayBuffer(cubes * stride * tris * 3);
    console.log("cubeBuf is", Math.round(arrayBuffer.byteLength / 1024 / 1024), "MiB");

    // the following typed arrays share the same buffer
    const bf32 = new Float32Array(arrayBuffer);
    const bu8 = new Uint8Array(arrayBuffer);

    const color = new THREE.Color();

    const n = 512, n2 = n / 2;	// cubes spread in the cube
    const d = 1, d2 = d / 2;	// individual triangle size

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
        o += 1;
        bf32[o++] = pB.x;
        bf32[o++] = pB.y;
        bf32[o++] = pB.z;
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;
        o += 1;
        bf32[o++] = pC.x;
        bf32[o++] = pC.y;
        bf32[o++] = pC.z;
        bf32[o++] = nx;
        bf32[o++] = ny;
        bf32[o++] = nz;

        // colors
        const vx = (pA.x / (n + d)) + 0.5;
        const vy = (pA.y / (n + d)) + 0.5;
        const vz = (pA.z / (n + d)) + 0.5;

        let r = (vx * 255) | 0, g = (vy * 255) | 0, b = (vz * 255) | 0;
        for (let o = i * stride * 3 + 6 * 4; o < (i + 1) * stride * 3; o += stride - 3) {
            bu8[o++] = r;
            bu8[o++] = g;
            bu8[o++] = b;
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

    for (let i = 0; i < cubes; i++) {
        // positions
        const x = (Math.random() * n - n2) |0;
        const y = (Math.random() * n - n2) |0;
        const z = (Math.random() * n - n2) |0;

        // cubes have 8 vertices
        // OpenGL/Minecraft: +X = East, +Y = Up, +Z = South
        // Front/Back, Left/Right, Up/Down
        FLD.set(x - d2, y - d2, z + d2);
        FLU.set(x - d2, y + d2, z + d2);
        FRD.set(x + d2, y - d2, z + d2);
        FRU.set(x + d2, y + d2, z + d2);
        BLD.set(x - d2, y - d2, z - d2);
        BLU.set(x - d2, y + d2, z - d2);
        BRD.set(x + d2, y - d2, z - d2);
        BRU.set(x + d2, y + d2, z - d2);

        // Note: "front face" is CCW
        addQuad(FLD, FRD, FRU, FLU, i * 12);      // F
        addQuad(FLD, FLU, BLU, BLD, i * 12 + 2);  // L
        addQuad(BLU, BRU, BRD, BLD, i * 12 + 4);  // B
        addQuad(BRD, BRU, FRU, FRD, i * 12 + 6);  // R
        addQuad(FLU, FRU, BRU, BLU, i * 12 + 8);  // U
        addQuad(BRD, FRD, FLD, BLD, i * 12 + 10); // D
    }

    const ibf32 = new THREE.InterleavedBuffer(bf32, stridef);
    const ibu8 = new THREE.InterleavedBuffer(bu8, stride);

    geometry.setAttribute('position', new THREE.InterleavedBufferAttribute(ibf32, 3, 0, false));
    geometry.setAttribute('normal', new THREE.InterleavedBufferAttribute(ibf32, 3, 3, false));
    geometry.setAttribute('color', new THREE.InterleavedBufferAttribute(ibu8, 3, 24, true));

    geometry.computeBoundingSphere();

    const material = new THREE.MeshPhongMaterial({
        color: 0xaaaaaa, specular: 0xffffff, shininess: 250,
        side: THREE.FrontSide, vertexColors: true
    });

    return [new THREE.Mesh(geometry, material), ibf32, ibu8];
}

const geometry = new THREE.BoxGeometry();
const material = new THREE.MeshLambertMaterial({ color: 0x00ff00 });
const cube = new THREE.Mesh(geometry, material);
scene.add(cube);

let [glitter, glitterPos, glitterColor] = makeglitter(glitterCount);
scene.add(glitter);

let [cubes, cubePos, cubeColor] = makeCubes(cubeCount);
scene.add(cubes);

var dLight = new THREE.DirectionalLight('#fff', 1);
dLight.position.set(-10, 15, 20);

var aLight = new THREE.AmbientLight('#111');

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

    if (scene.children.includes(cubes) && cubePos.array instanceof Float32Array) {
        let paa = cubePos.array;
        let jitters = new Float32Array(128 + (Math.random() * 50) | 0);
        for (let i = 0; i < jitters.length; i++) {
            jitters[i] = ((Math.random() - 0.5) * 2.5)|0;
        }
        const count = 1000;
        const cs = 7 * 6 * 2 * 3;  // cube stride. 252 floats!
        const offset = cs * ((Math.random() * (paa.length/(cs) - count)) | 0);
        for (let i = offset; i < offset + count * cs; i += cs) {
            for (let j = 0; j < cs; j += 7) {
                paa[i + j] += jitters[i % jitters.length];
                paa[i + j + 1] += jitters[i % jitters.length];
                paa[i + j + 2] += jitters[i % jitters.length];
            }
        }

        cubePos.needsUpdate = true;
        cubePos.updateRange = { offset, count: count * cs };
    }

    cube.rotation.x += 0.01;
    cube.rotation.y += 0.01;

    renderer.render(scene, camera);

    stats.end();
};

render();
controls.addEventListener('change', render);
