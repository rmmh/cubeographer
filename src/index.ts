import * as THREE from "three";
import { AddEquation, InstancedBufferAttribute } from "three";
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';

const Stats = require("stats.js");

let ONDEMAND = true;

let PROD = 1;

const glitterCount = PROD ? 1e6 : 1e4;
const cubeCount = PROD ? 512 * 512 * 2 : 1024;
const space = PROD ? 750 : 64;

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
    const stride = 28; // vec3 pos, vec3 normal, fp16*2  => 6 * 4 => 24B
    const stridef = (stride / 4)|0;
    const tris = 12;  // 6 faces * 2 tris each -- 28B * 6 * 2 * 3 = 1008B each!
    const cubeBuffer = new ArrayBuffer(stride * tris * 3);
    console.log("cubeBuf is", Math.round(cubeBuffer.byteLength / 1024 / 1024), "MiB");

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
    addQuad(FLD, FRD, FRU, FLU, 0);      // F
    addQuad(FLD, FLU, BLU, BLD, 2);  // L
    addQuad(BLU, BRU, BRD, BLD, 4);  // B
    addQuad(BRD, BRU, FRU, FRD, 6);  // R
    addQuad(FLU, FRU, BRU, BLU, 8);  // U
    addQuad(BRD, FRD, FLD, BLD, 10); // D

    const attribBuffer = new Uint32Array(cubes * CUBE_ATTRIB_STRIDE);

    for (let i = 0; i < cubes; i++) {
        // positions
        const x = (Math.random() * n) | 0;
        const y = (Math.random() * n) | 0;
        const z = (Math.random() * n) | 0;

        // colors
        let r = ((x / n) * 255) | 0, g = ((y / n) * 255) | 0, b = ((z / n) * 255) | 0;
        let o = i * CUBE_ATTRIB_STRIDE;
        attribBuffer[o] = (x << 20) | (y << 10) | z;
        attribBuffer[o+1] = (r << 16) | (g << 8) | b;
    }

    const geometry = new THREE.BufferGeometry();

    const ibf32 = new THREE.InterleavedBuffer(bf32, stridef);
    const ibu8 = new THREE.InterleavedBuffer(bu8, stride);
    const attrBuf = new THREE.InstancedBufferAttribute(attribBuffer, CUBE_ATTRIB_STRIDE);

    geometry.setAttribute('position', new THREE.InterleavedBufferAttribute(ibf32, 3, 0, false));
    geometry.setAttribute('normal', new THREE.InterleavedBufferAttribute(ibf32, 3, 3, false));
    geometry.setAttribute('attr', attrBuf);

    geometry.computeBoundingSphere();

    const material: THREE.Material = new THREE.RawShaderMaterial({
        uniforms: {
            time: { value: 1.0 }
        },
        vertexShader: `# version 300 es
			precision mediump float;
			precision highp int;

			uniform mat4 modelViewMatrix; // optional
			uniform mat4 projectionMatrix; // optional

			in vec3 position;
            in vec4 color;
            in vec3 normal;
            in uvec3 attr;
            in uint normb;

			out vec3 vPosition;
            out vec4 vColor;
            out vec3 vNormal;

            vec3 unpackPos(uint p) {
                return vec3(float(p >> 20), float((p >> 10) & 1023u), float(p & 1023u)) - vec3(${space/2});
            }
            vec3 unpackColor(int p) {
                return vec3(p >> 16, (p >> 8) & 0xff, p & 0xff) / 255.0;
            }

			void main()	{
                vColor = vec4(unpackColor(int(attr.y)), 1.0);
                vNormal = normal;
                gl_Position = projectionMatrix * modelViewMatrix * vec4( position + unpackPos(attr.x), 1.0 );
			}
        `,
        fragmentShader: `# version 300 es
            precision mediump float;
			precision highp int;

			uniform float time;

			in vec3 vPosition;
			in vec4 vColor;
            in vec3 vNormal;

            out vec4 outColor;

			void main()	{
				vec4 color = vec4( vColor );
				outColor = vec4(color.rgb * (0.6+.4*dot(vNormal, normalize(vec3(10,5,2)))), 1);
			}
        `,
        side: THREE.FrontSide,
        transparent: true

    });

    const mesh = new THREE.InstancedMesh(geometry, material, 0);
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
