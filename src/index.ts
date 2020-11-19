/*
TODO: open addressing for block insertion/deletion
steal from https://shlegeris.com/2017/01/06/hash-maps.html


*/

import * as THREE from "three";
import { InstancedBufferAttribute, Sphere } from "three";
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';

var vertexShader = require('./cube_vertex.glsl');
var fragmentShader = require('./cube_fragment.glsl');

const Stats = require("stats.js");

let ONDEMAND = true;

let PROD = 1;

const space = PROD ? 512 : 64;

const scene = new THREE.Scene();
const camera = new THREE.PerspectiveCamera(75, window.innerWidth / window.innerHeight, 0.1, 3000);
const stats = new Stats();
stats.showPanel(0); // 0: fps, 1: ms, 2: mb, 3+: custom
document.body.appendChild(stats.dom);

const renderer = new THREE.WebGLRenderer();
renderer.setSize(window.innerWidth, window.innerHeight);
document.body.appendChild(renderer.domElement);

renderer.setClearColor('#85a7ff');

// TODO: replace these controls with block-based ones,
// i.e. rotate around the click target
let controls = new OrbitControls(camera, renderer.domElement);
controls.addEventListener( 'change', render ); // call this only in static scenes (i.e., if there is no animation loop)
controls.screenSpacePanning = true;
controls.minDistance = 1;
controls.maxDistance = space * 2;

window.addEventListener('resize', onWindowResize, false);
function onWindowResize() {
    camera.aspect = window.innerWidth / window.innerHeight;
    camera.updateProjectionMatrix();
    renderer.setSize(window.innerWidth, window.innerHeight);
    if (ONDEMAND) render();
}

const CUBE_ATTRIB_STRIDE = 2;

class CubeFactory {
    geometry: THREE.BufferGeometry;
    material: THREE.RawShaderMaterial;;

    constructor() {
        const stride = 28; // vec3 pos, vec3 normal, fp16*2  => 6 * 4 + 2 * 2 => 24B
        const stridef = (stride / 4) | 0;
        const tris = 6;  // 3 faces * 2 tris each (we flip based on camera)
        const cubeBuffer = new ArrayBuffer(stride * tris * 3);

        // the following typed arrays share the same buffer
        const bf32 = new Float32Array(cubeBuffer);
        const bu8 = new Uint8Array(cubeBuffer);

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
        addQuad(FLD, FLU, BLU, BLD, 0);  // L+R
        addQuad(FLD, FRD, FRU, FLU, 2);  // F+B
        addQuad(FLU, FRU, BRU, BLU, 4);  // U+D

        this.geometry = new THREE.BufferGeometry();

        const ibf32 = new THREE.InterleavedBuffer(bf32, stridef);
        const ibu8 = new THREE.InterleavedBuffer(bu8, stride);

        this.geometry.setAttribute('position', new THREE.InterleavedBufferAttribute(ibf32, 3, 0, false));
        this.geometry.setAttribute('normal', new THREE.InterleavedBufferAttribute(ibf32, 4, 3, false));
        this.geometry.setAttribute('uv', new THREE.InterleavedBufferAttribute(ibu8, 2, 24, false));

        const tex = new THREE.TextureLoader().load(PROD ? 'textures/atlas.png' : 'textures/debug.png',
            () => render(),
        );
        tex.magFilter = THREE.NearestFilter;
        tex.minFilter = THREE.NearestFilter;
        tex.flipY = true;

        this.material = new THREE.RawShaderMaterial({
            uniforms: {
                atlas: { value: tex },
            },
            vertexShader,
            fragmentShader,
            side: THREE.DoubleSide,
            transparent: true
        });;
    }

    make(attr: Uint32Array): [THREE.InstancedMesh, InstancedBufferAttribute] {
        const geometry = this.geometry.clone();
        const mat = this.material.clone();
        mat.uniforms = {...this.material.uniforms};
        const attrBuf = new THREE.InstancedBufferAttribute(attr, CUBE_ATTRIB_STRIDE);
        geometry.setAttribute('attr', attrBuf);
        const mesh = new THREE.InstancedMesh(geometry, mat, 0);
        // InstancedMesh by default has instanceMatrix (16 floats per instance),
        // creating it with 0 and then fudging the count lets us use our own attributes.
        mesh.count = attr.length / CUBE_ATTRIB_STRIDE;
        return [mesh, attrBuf];
    }
}

const cubeFactory = new CubeFactory();

function makeCube() {
    const geometry = new THREE.BoxGeometry();
    const material = new THREE.MeshLambertMaterial({ color: 0x00ff00 });
    return new THREE.Mesh(geometry, material);
}

const cube = makeCube();
scene.add(cube);

if (false) {
    const loader = new THREE.FontLoader();
    loader.load('fonts/helvetiker_regular.typeface.json', function (font) {
        let materials = [
            new THREE.MeshPhongMaterial({ color: 0xffffff, flatShading: true }), // front
            new THREE.MeshPhongMaterial({ color: 0xffffff }) // side
        ];
        for (let pos of [[10, 10, 10], [10, 10, -10], [10, -10, -10], [10,-10,10],
                         [-10, 10, 10], [-10, 10, -10], [-10, -10, -10], [-10, -10, 10] ]) {
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

var dLight = new THREE.DirectionalLight('#fff', 1);
dLight.position.set(-10, 15, 20);
var aLight = new THREE.AmbientLight('#111');

scene.add(dLight);
scene.add(aLight);

camera.position.set(100, 40, 100);  // face northish
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

    renderer.render(scene, camera);

    stats.end();
};

render();

function sleep(ms: number) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

function fetchRegion(x: number, z: number, xo: number, zo: number) {
    fetch(`map/r.${x}.${z}.cmt`).then(
        async response => {
            if (!response.ok) {
                return;
            }
            const size = response.headers.get("Content-Length");
            let array: Uint32Array;
            if (size) {
                array = new Uint32Array(+size / Uint32Array.BYTES_PER_ELEMENT);
            } else {
                array = new Uint32Array(await response.arrayBuffer());
            }
            console.log(size ? "streaming" : "loaded", response.url, (array.length / 1024) | 0, "KiB");

            let [mesh, attrArr] = cubeFactory.make(array);
            mesh.position.set((x + xo) * 512, 0, (z + zo) * 512);
            if (mesh.material instanceof THREE.RawShaderMaterial) {
                mesh.material.uniforms.offset = { value: mesh.position };
            }
            let center = new THREE.Vector3(256, 128, 256);
            let dist = center.distanceTo(new THREE.Vector3(0,0,0));
            mesh.geometry.boundingSphere = new THREE.Sphere(center, dist);
            mesh.frustumCulled = true;
            let sph = new THREE.SphereGeometry(mesh.geometry.boundingSphere.radius, 10, 10);
            let smesh = new THREE.Mesh(sph, new THREE.MeshBasicMaterial({wireframe: true}));
            smesh.position.add(center);
            smesh.position.add(mesh.position);
            // scene.add(smesh);
            scene.add(mesh);

            if (size) {
                let byteArray = new Uint8Array(array.buffer);
                const reader = response.body.getReader();
                let offset = 0;
                while (true) {
                    const { value, done } = await reader.read();
                    if (done) break;
                    byteArray.set(value, offset);
                    attrArr.needsUpdate = true;
                    offset += value.length;
                    mesh.count = offset / array.BYTES_PER_ELEMENT;
                    render();
                }
                console.log("done streaming", response.url);
            } else {
                render();
            }

        },
        reason => console.log("rejected", reason)
    );
}

// interesting coords:
// Novigrad: [0, 2, 2, 3, -1.5, -2.8]

for (let x = 0; x <= 2; x++) {
    for (let z = 2; z <= 3; z++) {
        fetchRegion(x, z, -1.5, -2.8)
        // camera.position.set(-200, 1, 250);
        // camera.lookAt(-250, 0, 250);
        // controls.center = new THREE.Vector3(-250, 0, 250);
    }
}

