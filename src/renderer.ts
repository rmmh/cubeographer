import { glMatrix, mat4, quat, vec3, vec4 } from 'gl-matrix';

import * as twgl from 'twgl.js';
import { generateTextureArrayMipmaps } from "./downscale";
import { safeLookAt } from "./camera";
import { renderBoundaries } from './render_debug';
import { LOD2Group, LOD2GroupManager, renderGroupToFBO } from './lod';

export class Material {
    gl: WebGL2RenderingContext;
    program: WebGLProgram;
    uniformSetters: { [name: string]: any };
    attribSetters: { [name: string]: any };

    constructor(gl: WebGL2RenderingContext, vertexShader: string, fragmentShader: string) {
        this.gl = gl;
        const programInfo = twgl.createProgramInfo(gl, [vertexShader, fragmentShader]);
        this.program = programInfo.program;
        this.uniformSetters = programInfo.uniformSetters;
        this.attribSetters = programInfo.attribSetters;
    }
}

export class Geometry {
    attributes: { [name: string]: twgl.AttribInfo }
    layerLengths: Uint32Array
    verts: number

    constructor(public gl: WebGL2RenderingContext, attributes?: { [name: string]: twgl.AttribInfo }) {
        this.attributes = attributes || {};
        this.verts = 3;
    }

    clone() {
        return new Geometry(this.gl, { ...this.attributes })
    }

    setAttributes(arrays: { [name: string]: any }) {
        this.attributes = twgl.createAttribsFromArrays(this.gl, arrays);
    }

    addAttribute(name: string, array: any) {
        Object.assign(this.attributes, twgl.createAttribsFromArrays(this.gl, { [name]: array }));
    }

    updateAttribute(name: string, array: any, offset?: number) {
        if (!(name in this.attributes)) {
            throw new Error("unknown attribute: " + name);
        }
        twgl.setAttribInfoBufferFromArray(this.gl, this.attributes[name], array, offset || 0);
    }
}


export class Mesh {
    position: vec3

    constructor(
        public geometry: Geometry,
        public material: Material,
    ) {
        this.position = vec3.create();
    }
}

export class InstancedMesh {
    position: vec3

    constructor(
        public geometry: Geometry,
        public material: Material,
        public count: number
    ) {
        this.position = vec3.create();
    }
}

export class InstancedLayer {
    constructor(
        public geometry: Geometry,
        public material: Material,
        public texture: WebGLTexture,
        public name: string,
    ) { }
}

export class Chunk {
    position: vec3
    minY: number
    maxY: number
    layers: { [name: string]: twgl.AttribInfo }
    voxelBitset?: any

    constructor(public gl: WebGL2RenderingContext) {
        this.position = vec3.create();
        this.minY = 0
        this.maxY = 255
        this.layers = {};
    }

    setLayers(arrays: { [name: string]: any }) {
        this.layers = twgl.createAttribsFromArrays(this.gl, arrays);
        for (const [name, arraySpec] of Object.entries(arrays)) {
            if (arraySpec && arraySpec.retain) {
                this.layers[name].data = arraySpec.data.buffer;
            }
        }
    }

    addAttribute(name: string, array: any) {
        const newAttribs = twgl.createAttribsFromArrays(this.gl, { [name]: array });
        if (array && array.retain) {
            newAttribs[name].data = array.data.buffer;
        }
        Object.assign(this.layers, newAttribs);
    }

    updateAttribute(name: string, array: any, offset?: number) {
        if (!(name in this.layers)) {
            throw new Error("unknown attribute: " + name);
        }
        twgl.setAttribInfoBufferFromArray(this.gl, this.layers[name], array, offset || 0);
        if (this.layers[name].data) {
            let buf = new Uint8Array(this.layers[name].data);
            buf.set(array, offset);
        }
    }
}

export class Context {
    canvas: HTMLCanvasElement
    gl: WebGL2RenderingContext
    clearColor: vec4
    cuboidDataTex?: WebGLTexture
    cuboidTextureData?: Uint32Array
    fboInfo: twgl.FramebufferInfo | null = null
    get fbo(): WebGLFramebuffer | null {
        return this.fboInfo ? this.fboInfo.framebuffer : null;
    }
    get fboDepth(): WebGLTexture | null {
        return this.fboInfo ? (this.fboInfo.attachments[1] as WebGLTexture) : null;
    }
    renderToFBO: boolean = false
    scissorBox: { x: number; y: number; width: number; height: number } | null = null
    boundaryMaterial?: Material
    boundaryGeometry?: Geometry

    updateFBO(width: number, height: number) {
        const gl = this.gl;
        const attachments = [
            { format: gl.RGBA8, samples: 1 },
            { attachmentPoint: gl.DEPTH_ATTACHMENT, internalFormat: gl.DEPTH_COMPONENT32F, format: gl.DEPTH_COMPONENT, type: gl.FLOAT, minMag: gl.NEAREST, wrap: gl.CLAMP_TO_EDGE }
        ];
        if (!this.fboInfo) {
            this.fboInfo = twgl.createFramebufferInfo(gl, attachments, width, height);
        } else if (this.fboInfo.width !== width || this.fboInfo.height !== height) {
            twgl.resizeFramebufferInfo(gl, this.fboInfo, attachments, width, height);
        }
    }

    constructor(canvas: HTMLCanvasElement) {
        this.canvas = canvas;
        this.gl = this.canvas.getContext('webgl2', {
            depth: true,
            antialias: true
        });
        this.clearColor = vec4.fromValues(1, 1, 1, 1);
    }

    setSize(width: number, height: number) {
        this.canvas.width = width;
        this.canvas.height = height;
    }

    setClearColor(r: number, g: number, b: number, a?: number) {
        vec4.set(this.clearColor, r / 255.0, g / 255.0, b / 255.0, (a || 255) / 255.0);
    }

    Material(vertexShader: string, fragmentShader: string): Material {
        return new Material(this.gl, vertexShader, fragmentShader);
    }

    Geometry(): Geometry {
        return new Geometry(this.gl);
    }

    Chunk(): Chunk {
        return new Chunk(this.gl);
    }

    loadTexture(path: string, done?: () => void): WebGLTexture {
        const gl = this.gl;
        const self = this;

        // Create a WebGL 2 texture array
        var texture = gl.createTexture();

        // Asynchronously load the image
        var image = new Image();
        image.src = path;
        image.addEventListener('load', function () {
            const w = image.width;
            const h = image.height;
            const tilesPerRow = Math.floor(w / 16);
            const numSlices = Math.floor((w * h) / 256);

            gl.bindTexture(gl.TEXTURE_2D_ARRAY, texture);

            // Pre-allocate WebGL 2 immutable 3D texture storage (with 5 mipmap levels)
            gl.texStorage3D(
                gl.TEXTURE_2D_ARRAY,
                5, // 5 levels (16x16, 8x8, 4x4, 2x2, 1x1)
                gl.RGBA8,
                16,
                16,
                numSlices
            );

            // Draw the loaded image to a temporary canvas to read its pixel data
            const canvas = document.createElement('canvas');
            canvas.width = w;
            canvas.height = h;
            const ctx = canvas.getContext('2d');
            ctx.drawImage(image, 0, 0);

            const imgData = ctx.getImageData(0, 0, w, h);
            const srcPixels = imgData.data;

            // Allocate a buffer to hold the sliced tiles
            const slicedPixels = new Uint8Array(16 * 16 * 4 * numSlices);

            // Copy each 16x16 tile into its respective slice/layer
            for (let slice = 0; slice < numSlices; slice++) {
                const tileX = (slice % tilesPerRow) * 16;
                const tileY = Math.floor(slice / tilesPerRow) * 16;

                for (let y = 0; y < 16; y++) {
                    const srcRowStart = ((tileY + y) * w + tileX) * 4;
                    const destRowStart = (slice * 256 + y * 16) * 4;

                    // Copy 16 pixels (64 bytes)
                    for (let i = 0; i < 64; i++) {
                        slicedPixels[destRowStart + i] = srcPixels[srcRowStart + i];
                    }
                }
            }

            // Upload the sliced pixel data to WebGL
            gl.texSubImage3D(
                gl.TEXTURE_2D_ARRAY,
                0,
                0, 0, 0, // xoffset, yoffset, zoffset
                16, 16, numSlices, // width, height, depth
                gl.RGBA,
                gl.UNSIGNED_BYTE,
                slicedPixels
            );

            // Generate custom gamma-aware and transparency-weighted mipmaps
            generateTextureArrayMipmaps(gl, texture, slicedPixels, 4);

            // Setup texture wrapping and filtering parameters
            twgl.setTextureParameters(gl, texture, {
                target: gl.TEXTURE_2D_ARRAY,
                wrap: gl.CLAMP_TO_EDGE,
                mag: gl.NEAREST,
                min: gl.NEAREST_MIPMAP_NEAREST,
                maxLevel: 4
            });

            if (done) done();
        });
        return texture;
    }
}

interface Camera {
    getProjection(): mat4;
    getView(): mat4;
    update(): void;
}

export class PerspectiveCamera implements Camera {
    proj: mat4;
    view: mat4;
    position: vec3;
    target: vec3;
    quaternion: quat;
    matrix: mat4;

    isPerspectiveCamera = true;

    constructor(
        public fov: number,
        public aspect: number,
        public near: number,
        public far: number) {
        this.proj = mat4.create();
        this.view = mat4.create();
        this.position = vec3.fromValues(0, 0, 1);
        this.target = vec3.create();
        this.quaternion = quat.create();
        this.matrix = mat4.create();
        this.update();
    }

    lookAt(target: vec3) {
        vec3.copy(this.target, target);
        this.update();
    }

    update() {
        mat4.perspective(this.proj, glMatrix.toRadian(this.fov), this.aspect, this.near, this.far);
        // Modify projection matrix for infinite far plane and inverse-Z depth mapping:
        // - Near plane maps to Z_ndc = +1 (window depth 1.0)
        // - Far plane at infinity maps to Z_ndc = -1 (window depth 0.0)
        this.proj[10] = 1.0;
        this.proj[14] = 2.0 * this.near;

        // TODO: add ortho mode with correct zooming, shaders (flipping is broken), etc
        // const orthoscale = 128;
        // mat4.ortho(this.proj, -orthoscale * this.aspect, orthoscale * this.aspect, -orthoscale, orthoscale, -5000, 5000)
        safeLookAt(this.view, this.position, this.target, vec3.fromValues(0, 1, 0));
        mat4.getRotation(this.quaternion, this.view);
    }

    getProjection(): mat4 {
        return this.proj;
    }

    getView(): mat4 {
        return mat4.mul(mat4.create(), this.view, this.matrix);
    }
}

export class Plane {
    normal = vec3.create();
    constant = 0;

    setComponents(x: number, y: number, z: number, w: number) {
        vec3.set(this.normal, x, y, z);
        const length = vec3.len(this.normal);
        if (length > 0) {
            vec3.scale(this.normal, this.normal, 1.0 / length);
            this.constant = w / length;
        } else {
            this.constant = 0;
        }
    }

    distanceToPoint(point: vec3): number {
        return vec3.dot(this.normal, point) + this.constant;
    }
}

export function sqrDistPointToAABB(point: vec3, min: vec3, max: vec3): number {
    let sqDist = 0;
    for (let i = 0; i < 3; i++) {
        const v = point[i];
        if (v < min[i]) {
            sqDist += (min[i] - v) * (min[i] - v);
        } else if (v > max[i]) {
            sqDist += (v - max[i]) * (v - max[i]);
        }
    }
    return sqDist;
}

export class Frustum {
    planes: Plane[];

    constructor(public camera: PerspectiveCamera) {
        this.planes = [
            new Plane(), // Left
            new Plane(), // Right
            new Plane(), // Bottom
            new Plane(), // Top
            new Plane(), // Near
            new Plane()  // Far
        ];
        this.update();
    }

    update() {
        const proj = this.camera.getProjection();
        const view = this.camera.getView();
        const m = mat4.multiply(mat4.create(), proj, view);

        // Extract the 6 frustum planes
        // Right plane
        this.planes[0].setComponents(m[3] - m[0], m[7] - m[4], m[11] - m[8], m[15] - m[12]);
        // Left plane
        this.planes[1].setComponents(m[3] + m[0], m[7] + m[4], m[11] + m[8], m[15] + m[12]);
        // Bottom plane
        this.planes[2].setComponents(m[3] + m[1], m[7] + m[5], m[11] + m[9], m[15] + m[13]);
        // Top plane
        this.planes[3].setComponents(m[3] - m[1], m[7] - m[5], m[11] - m[9], m[15] - m[13]);
        // Far plane
        this.planes[4].setComponents(m[3] - m[2], m[7] - m[6], m[11] - m[10], m[15] - m[14]);
        // Near plane
        this.planes[5].setComponents(m[3] + m[2], m[7] + m[6], m[11] + m[10], m[15] + m[14]);
    }

    intersectsAABB(min: vec3, max: vec3): boolean {
        const p = vec3.create();
        for (let i = 0; i < 6; i++) {
            const plane = this.planes[i];
            const normal = plane.normal;

            // Find the p-vertex (furthest corner in the positive direction of the plane normal)
            p[0] = normal[0] >= 0 ? max[0] : min[0];
            p[1] = normal[1] >= 0 ? max[1] : min[1];
            p[2] = normal[2] >= 0 ? max[2] : min[2];

            // If the p-vertex is on the negative side of the plane, then the entire AABB is outside the frustum.
            if (plane.distanceToPoint(p) < 0) {
                return false;
            }
        }
        return true;
    }

    intersects(c: Chunk): boolean {
        const min = vec3.fromValues(c.position[0], c.minY, c.position[2]);
        const max = vec3.fromValues(c.position[0] + 256, c.maxY, c.position[2] + 256);
        return this.intersectsAABB(min, max);
    }

    intersectsRegion(rx: number, rz: number, maxHeight: number = 320.0, minHeight: number = -64): boolean {
        const min = vec3.fromValues(rx * 512, minHeight, rz * 512);
        const max = vec3.fromValues(rx * 512 + 512, maxHeight, rz * 512 + 512);
        return this.intersectsAABB(min, max);
    }
}

// --- Scene Graph & Request Queue Types ---
export type RegionletStatus = 'NONE' | 'FETCH' | 'STREAM' | 'READY' | 'ERROR';
export type ImpostorStatus = 'NONE' | 'FETCH' | 'READY' | 'ERROR';
export type RequestType = 'REGIONLET' | 'IMPOSTOR';

export interface MapMetadata {
    full_regions: Set<string>;
    lod_regions: Set<string>;
    tile_regions: Set<string>;
    loaded: boolean;
}

export interface RegionletNode {
    rx: number;
    rz: number;
    off: number;
    status: RegionletStatus;
    chunks: Chunk[]; // one per y-slice, populated when CMT is loaded
}

export interface ImpostorNode {
    rx: number;
    rz: number;
    status: ImpostorStatus;
    textures: any | null;
    loaded: boolean;
    maxHeight?: number;
    heightmaps?: {
        top: Uint8Array;
        north: Uint8Array;
        south: Uint8Array;
        east: Uint8Array;
        west: Uint8Array;
    } | null;
    reconstructedGeometry?: Geometry | null;
}

export interface RegionNode {
    rx: number;
    rz: number;
    impostor: ImpostorNode;
    regionlets: [RegionletNode, RegionletNode, RegionletNode, RegionletNode];
}

export interface QueuedRequest {
    type: RequestType;
    key: string;
    priority: number;
    run: () => Promise<void>;
}

export class RequestManager {
    private activeRequests = new Map<string, QueuedRequest>();
    private queue: QueuedRequest[] = [];

    public maxConcurrent = {
        REGIONLET: 4,
        IMPOSTOR: 2
    };

    public inProgress = {
        REGIONLET: 0,
        IMPOSTOR: 0
    };

    enqueue(req: QueuedRequest) {
        if (this.activeRequests.has(req.key) || this.queue.some(r => r.key === req.key)) {
            return;
        }
        this.queue.push(req);
        this.sortQueue();
        this.tick();
    }

    private sortQueue() {
        this.queue.sort((a, b) => a.priority - b.priority);
    }

    private tick() {
        while (true) {
            let startedAny = false;
            for (let i = 0; i < this.queue.length; i++) {
                const req = this.queue[i];
                if (this.inProgress[req.type] < this.maxConcurrent[req.type]) {
                    this.queue.splice(i, 1);
                    this.activeRequests.set(req.key, req);
                    this.inProgress[req.type]++;
                    req.run().then(
                        () => {
                            this.activeRequests.delete(req.key);
                            this.inProgress[req.type]--;
                            this.tick();
                        },
                        (err) => {
                            console.error(`Request ${req.key} failed:`, err);
                            this.activeRequests.delete(req.key);
                            this.inProgress[req.type]--;
                            this.tick();
                        }
                    );
                    startedAny = true;
                    break;
                }
            }
            if (!startedAny) break;
        }
    }

    getInProgressCount(type: RequestType): number {
        return this.inProgress[type];
    }
}

export interface CullResults {
    chunks: Chunk[];
    impostors: ImpostorNode[];
    missingRegionlets: RegionletNode[];
    missingImpostors: ImpostorNode[];
    lod2Groups: LOD2Group[];
}

export class SceneGraph {
    regions = new Map<string, RegionNode>();
    requestManager = new RequestManager();
    showBoundaries = false;
    mapMetadata: MapMetadata | null = null;
    lod0Dist = 128.0;
    lod1Dist = 1920.0;
    lod2Dist = 15360.0;
    lod1Reconstruction = false;
    showStreamingLOD0 = false;
    lod2GroupSize = 2;
    lod2DistortionThreshold = 15.0;
    lod2UpdateBudget = 4;
    lod2Manager: LOD2GroupManager;
    lastCullResults: CullResults | null = null;
    private listeners = new Set<() => void>();

    constructor(public context: Context) {
        this.lod2Manager = new LOD2GroupManager(this);
    }

    subscribe(listener: () => void): () => void {
        this.listeners.add(listener);
        return () => {
            this.listeners.delete(listener);
        };
    }

    notify() {
        for (const listener of this.listeners) {
            listener();
        }
    }

    getOrCreateRegion(rx: number, rz: number): RegionNode {
        const key = `${rx},${rz}`;
        if (this.regions.has(key)) {
            return this.regions.get(key)!;
        }

        const regionlets: RegionletNode[] = [];
        for (let off = 0; off < 4; off++) {
            regionlets.push({ rx, rz, off, status: 'NONE', chunks: [] });
        }

        const node: RegionNode = {
            rx, rz,
            impostor: {
                rx, rz,
                status: 'NONE',
                textures: null,
                loaded: false,
                maxHeight: 320.0
            },
            regionlets: regionlets as [RegionletNode, RegionletNode, RegionletNode, RegionletNode]
        };

        this.regions.set(key, node);
        return node;
    }

    updateRegionletStatus(rx: number, rz: number, off: number, status: RegionletStatus) {
        const region = this.getOrCreateRegion(rx, rz);
        const regionlet = region.regionlets[off];
        regionlet.status = status;
        this.notify();
    }

    updateImpostorStatus(rx: number, rz: number, status: ImpostorStatus, textures: any = null, maxHeight?: number, heightmaps: any = null) {
        const region = this.getOrCreateRegion(rx, rz);
        region.impostor.status = status;
        region.impostor.loaded = (status === 'READY');
        if (textures) {
            region.impostor.textures = textures;
        }
        if (maxHeight !== undefined) {
            region.impostor.maxHeight = maxHeight;
        }
        if (heightmaps) {
            region.impostor.heightmaps = heightmaps;
        }
        if (status === 'READY') {
            const G = this.lod2GroupSize;
            const groupX = Math.floor(rx / G);
            const groupZ = Math.floor(rz / G);
            const group = this.lod2Manager.groups.get(`${groupX},${groupZ}`);
            if (group) {
                group.stale = true;
            }
        }
        this.notify();
    }

    cull(camera: PerspectiveCamera): CullResults {
        const frustum = new Frustum(camera);
        const chunksToRender: Chunk[] = [];
        const impostorsToRender: ImpostorNode[] = [];
        const missingRegionlets: RegionletNode[] = [];
        const missingImpostors: ImpostorNode[] = [];
        const lod2GroupsToRender = new Set<LOD2Group>();

        const getDistance = (cameraPos: vec3, min: vec3, max: vec3, minDistanceSpecified: number): number => {
            if (minDistanceSpecified <= 256) {
                return Math.sqrt(sqrDistPointToAABB(cameraPos, min, max));
            } else {
                const center = vec3.fromValues(
                    (min[0] + max[0]) * 0.5,
                    (min[1] + max[1]) * 0.5,
                    (min[2] + max[2]) * 0.5
                );
                const diagonal = vec3.fromValues(
                    max[0] - min[0],
                    max[1] - min[1],
                    max[2] - min[2]
                );
                const radius = vec3.length(diagonal) * 0.5;
                return Math.max(0.0, vec3.distance(cameraPos, center) - radius);
            }
        };

        const G = this.lod2GroupSize;

        for (const region of this.regions.values()) {
            if (!frustum.intersectsRegion(region.rx, region.rz, region.impostor.maxHeight ?? 320.0)) {
                continue;
            }

            // 1. Evaluate LOD2 Group distance
            const groupX = Math.floor(region.rx / G);
            const groupZ = Math.floor(region.rz / G);
            const groupMinX = groupX * G * 512;
            const groupMaxX = (groupX + 1) * G * 512;
            const groupMinZ = groupZ * G * 512;
            const groupMaxZ = (groupZ + 1) * G * 512;

            const groupMin = vec3.fromValues(groupMinX, -64, groupMinZ);
            const groupMax = vec3.fromValues(groupMaxX, 320.0, groupMaxZ);
            // Implicit minimum distance for LOD2 is Math.max(lod1Dist, lod0Dist)
            const lod2MinDist = Math.max(this.lod1Dist, this.lod0Dist);
            const distToGroup = getDistance(camera.position, groupMin, groupMax, lod2MinDist);

            if (distToGroup > lod2MinDist && distToGroup <= this.lod2Dist) {
                // Render as LOD2 Group
                const group = this.lod2Manager.getOrCreateGroup(groupX, groupZ, G);
                lod2GroupsToRender.add(group);

                // Fetch region impostor if missing since LOD2 FBO rendering needs it
                if (region.impostor.status === 'NONE') {
                    missingImpostors.push(region.impostor);
                }
            } else if (distToGroup <= lod2MinDist) {
                const regionMin = vec3.fromValues(region.rx * 512, -64, region.rz * 512);
                const regionMax = vec3.fromValues(region.rx * 512 + 512, region.impostor.maxHeight ?? 320.0, region.rz * 512 + 512);
                const distToRegion = getDistance(camera.position, regionMin, regionMax, this.lod0Dist);

                let hasLOD1Regionlet = false;

                // Evaluate each regionlet for LOD0
                for (const rlet of region.regionlets) {
                    const xBase = region.rx * 512 + (rlet.off & 1) * 256;
                    const zBase = region.rz * 512 + (rlet.off & 2) * 128;
                    let minY: number, maxY: number;
                    if (rlet.chunks.length > 0) {
                        minY = Math.min(...rlet.chunks.map(c => c.minY));
                        maxY = Math.max(...rlet.chunks.map(c => c.maxY));
                    } else {
                        minY = -64; maxY = 320;
                    }
                    const minRlet = vec3.fromValues(xBase, minY, zBase);
                    const maxRlet = vec3.fromValues(xBase + 256, maxY, zBase + 256);

                    if (frustum.intersectsAABB(minRlet, maxRlet)) {
                        const distToRegionlet = getDistance(camera.position, minRlet, maxRlet, 0);
                        if (this.lod0Dist > 0 && distToRegionlet <= this.lod0Dist) {
                            const isStreaming = rlet.status === 'STREAM';
                            const hasLOD1 = region.impostor.status === 'READY' && region.impostor.textures;
                            const showLOD0 = rlet.status === 'READY' || (isStreaming && (this.showStreamingLOD0 || !hasLOD1));

                            if (showLOD0) {
                                for (const chunk of rlet.chunks) {
                                    chunksToRender.push(chunk);
                                }
                            } else {
                                if (rlet.status === 'NONE') {
                                    missingRegionlets.push(rlet);
                                }
                                hasLOD1Regionlet = true;
                            }
                        } else {
                            hasLOD1Regionlet = true;
                        }
                    }
                }

                // Render LOD1 region impostor if in range and any visible portion needs LOD1 fallback (LOD1 fills the gap when group is not rendered as LOD2)
                const maxLodDist = Math.max(this.lod1Dist, this.lod2Dist);
                const inLod1Range = (maxLodDist >= this.lod0Dist) && (distToRegion <= maxLodDist);
                if (inLod1Range && hasLOD1Regionlet) {
                    if (region.impostor.status === 'READY' && region.impostor.textures) {
                        impostorsToRender.push(region.impostor);
                    } else if (region.impostor.status === 'NONE') {
                        missingImpostors.push(region.impostor);
                    }
                }
            }
        }

        this.lastCullResults = {
            chunks: chunksToRender,
            impostors: impostorsToRender,
            missingRegionlets,
            missingImpostors,
            lod2Groups: Array.from(lod2GroupsToRender)
        };
        return this.lastCullResults;
    }
}

export function render(
    context: Context,
    camera: PerspectiveCamera,
    sceneGraph: SceneGraph,
    layers: InstancedLayer[],
    cube: Mesh,
    impostorGeometry?: Geometry,
    impostorMaterial?: Material,
    lod2Geometry?: Geometry,
    lod2Material?: Material,
    lod1ReconstructMaterial?: Material
): boolean {
    const gl = context.gl;

    if (!(gl.canvas instanceof HTMLCanvasElement))
        return false;

    // Perform frustum and visibility culling early
    const cullResults = sceneGraph.cull(camera);

    // Calculate height-based fog scale to fade out fog at high altitudes
    const height = camera.position[1];
    const minFogHeight = 400.0;
    const maxFogHeight = 4000.0;
    const fogScale = Math.max(0.0, Math.min(1.0, 1.0 - (height - minFogHeight) / (maxFogHeight - minFogHeight)));

    // Update LOD2 group textures under the per-frame budget
    if (impostorGeometry && impostorMaterial) {
        const candidates: { group: LOD2Group; deviation: number }[] = [];

        for (const group of cullResults.lod2Groups) {
            let deviation = 0;
            if (!group.hasTexture || group.stale) {
                deviation = Infinity;
                group.angularDeviation = 999; // Represent infinite deviation for display
            } else {
                const dirInitial = vec3.create();
                vec3.sub(dirInitial, group.initialCameraPos, group.center);
                vec3.normalize(dirInitial, dirInitial);

                const dirCurrent = vec3.create();
                vec3.sub(dirCurrent, camera.position, group.center);
                vec3.normalize(dirCurrent, dirCurrent);

                const cosTheta = vec3.dot(dirInitial, dirCurrent);
                const angleDev = Math.acos(Math.max(-1.0, Math.min(1.0, cosTheta))) * 180 / Math.PI;

                group.angularDeviation = angleDev;
                deviation = angleDev;
            }

            if (deviation > 0) {
                candidates.push({ group, deviation });
            }
        }

        // Sort descending by deviation (highest deviation first)
        // If deviations are equal, sort by distance to camera (closest first)
        candidates.sort((a, b) => {
            if (b.deviation !== a.deviation) {
                return b.deviation - a.deviation;
            }
            const distSqA = vec3.sqrDist(camera.position, a.group.center);
            const distSqB = vec3.sqrDist(camera.position, b.group.center);
            return distSqA - distSqB;
        });

        const updateSlice = candidates.slice(0, sceneGraph.lod2UpdateBudget);
        for (const item of updateSlice) {
            const group = item.group;
            renderGroupToFBO(gl, group, sceneGraph, camera, impostorGeometry, impostorMaterial);

            vec3.copy(group.initialCameraPos, camera.position);
            group.hasTexture = true;
            group.angularDeviation = 0;
            group.stale = false;
        }
    }

    if (context.renderToFBO) {
        context.updateFBO(gl.canvas.width, gl.canvas.height);
        twgl.bindFramebufferInfo(gl, context.fboInfo);
    } else {
        twgl.bindFramebufferInfo(gl, null);
    }
    gl.viewport(0, 0, gl.canvas.width, gl.canvas.height);

    const useScissor = context.renderToFBO && context.scissorBox;
    if (useScissor) {
        gl.enable(gl.SCISSOR_TEST);
        gl.scissor(
            context.scissorBox.x,
            context.scissorBox.y,
            context.scissorBox.width,
            context.scissorBox.height
        );
    }

    gl.clearColor(context.clearColor[0], context.clearColor[1],
        context.clearColor[2], context.clearColor[3]);

    gl.disable(gl.CULL_FACE);
    gl.enable(gl.DEPTH_TEST);
    gl.depthFunc(gl.GREATER); // Use GREATER depth function for inverse-Z
    gl.clearDepth(0.0); // Clear depth to 0.0 (far plane) for inverse-Z

    // Clear the canvas AND the depth buffer.
    gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);

    // Enable alpha blending
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA);

    // Compute the projection matrix
    var projectionMatrix = camera.getProjection();

    // 2. Perform frustum and visibility culling on the Scene Graph
    let renderedChunks = cullResults.chunks;

    // Sort renderable chunks by distance to camera
    renderedChunks.sort((a, b) => {
        const amin = vec3.fromValues(a.position[0], a.minY, a.position[2]);
        const amax = vec3.fromValues(a.position[0] + 256, a.maxY, a.position[2] + 256);
        const bmin = vec3.fromValues(b.position[0], b.minY, b.position[2]);
        const bmax = vec3.fromValues(b.position[0] + 256, b.maxY, b.position[2] + 256);
        return sqrDistPointToAABB(camera.position, amin, amax) - sqrDistPointToAABB(camera.position, bmin, bmax);
    });

    var activeProgram: WebGLProgram
    function bind(mat: Material, geo: Geometry) {
        if (mat.program != activeProgram) {
            activeProgram = mat.program;
            gl.useProgram(mat.program);
            mat.uniformSetters.projectionMatrix(projectionMatrix);
            if (mat.uniformSetters.cameraPosition)
                mat.uniformSetters.cameraPosition(camera.position);
            if (mat.uniformSetters.uFogScale)
                mat.uniformSetters.uFogScale(fogScale);
            for (const [key, value] of Object.entries(geo.attributes)) {
                if (mat.attribSetters[key]) {
                    mat.attribSetters[key](value);
                }
            }
        }
    }

    // 4. Render main geometry layers
    for (const layer of layers) {
        if (!layer) continue;
        let mat = layer.material;

        bind(mat, layer.geometry)

        mat.uniformSetters.atlas(layer.texture);

        if (mat.uniformSetters.cuboidDataTex && context.cuboidDataTex) {
            mat.uniformSetters.cuboidDataTex(context.cuboidDataTex);
        }

        // Selectively configure blending and alpha cutout per layer:
        // CUBE_FALLBACK (contains water/translucent elements) uses alpha blending.
        // Other layers (CUBE, VOXEL, CROSS, CROP, CUBOID) use efficient alpha cutouts.
        const isCutout = layer.name !== "CUBE_FALLBACK";
        if (isCutout) {
            gl.disable(gl.BLEND);
        } else {
            gl.enable(gl.BLEND);
            gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA);
        }

        // Configure face culling per layer:
        // Plant/crop sprites (CROSS, CROP) are double-sided and should not cull back-faces.
        // Other voxel layers (CUBE, VOXEL, CUBOID, CUBE_FALLBACK) have correct CCW front-faces and benefit from back-face culling.
        const isPlant = layer.name === "CROSS" || layer.name === "CROP";
        if (isPlant) {
            gl.disable(gl.CULL_FACE);
        } else {
            gl.enable(gl.CULL_FACE);
            gl.cullFace(gl.BACK);
        }

        if (mat.uniformSetters.alphaCutoutThreshold) {
            mat.uniformSetters.alphaCutoutThreshold(isCutout ? 0.5 : 0.05);
        }

        let chunkNum = 0;
        for (const chunk of renderedChunks) {
            chunkNum++;
            const chunkLayer = chunk.layers[layer.name];
            if (!chunkLayer || chunkLayer.size == 0) {
                continue;
            }

            mat.attribSetters.attr(chunkLayer);
            if (mat.uniformSetters.offset) {
                mat.uniformSetters.offset(chunk.position);
            }

            var matrix = mat4.translate(mat4.create(), camera.getView(), chunk.position);
            mat.uniformSetters.modelViewMatrix(matrix);

            gl.drawArraysInstanced(
                gl.TRIANGLES,
                0,
                layer.geometry.verts,
                chunkLayer.size,
            );
        }
    }

    // Disable face culling for other rendering phases (boundaries, impostors, LOD2)
    gl.disable(gl.CULL_FACE);

    // 5. Render Region Impostors as fallback
    if (impostorGeometry && impostorMaterial) {
        gl.disable(gl.BLEND);
        gl.enable(gl.DEPTH_TEST);

        const renderedChunkSet = new Set<Chunk>(renderedChunks);

        for (const lod of cullResults.impostors) {
            if (!lod.loaded || !lod.textures) continue;

            const regionOffset = vec3.fromValues(lod.rx * 512, 0, lod.rz * 512);
            const chunkMaxY = new Float32Array([-1.0, -1.0, -1.0, -1.0]);
            const region = sceneGraph.getOrCreateRegion(lod.rx, lod.rz);
            for (let off = 0; off < 4; off++) {
                const rlet = region.regionlets[off];
                const renderedFromRlet = rlet.chunks.filter(c => renderedChunkSet.has(c));
                if (renderedFromRlet.length > 0) {
                    if (rlet.status === 'READY') {
                        chunkMaxY[off] = 320.0;
                    } else if (rlet.status === 'STREAM') {
                        chunkMaxY[off] = Math.max(...renderedFromRlet.map(c => c.maxY));
                    }
                }
            }

            if (sceneGraph.lod1Reconstruction && lod1ReconstructMaterial) {
                // Lazy reconstructed geometry generation
                if (!lod.reconstructedGeometry && lod.heightmaps) {
                    const { reconstructLOD1Geometry } = require('./lod1_reconstruction');
                    lod.reconstructedGeometry = reconstructLOD1Geometry(gl, lod);
                }

                if (lod.reconstructedGeometry && lod.reconstructedGeometry.verts > 0) {
                    bind(lod1ReconstructMaterial, lod.reconstructedGeometry);

                    // Rebind attributes since geometry changes per region
                    for (const [key, value] of Object.entries(lod.reconstructedGeometry.attributes)) {
                        if (lod1ReconstructMaterial.attribSetters[key]) {
                            lod1ReconstructMaterial.attribSetters[key](value);
                        }
                    }

                    if (lod1ReconstructMaterial.uniformSetters.uCameraPosition) {
                        lod1ReconstructMaterial.uniformSetters.uCameraPosition(camera.position);
                    }
                    if (lod1ReconstructMaterial.uniformSetters.uRegionOffset) {
                        lod1ReconstructMaterial.uniformSetters.uRegionOffset(regionOffset);
                    }
                    if (lod1ReconstructMaterial.uniformSetters.uMaxHeight) {
                        lod1ReconstructMaterial.uniformSetters.uMaxHeight(lod.maxHeight ?? 320.0);
                    }

                    if (lod1ReconstructMaterial.uniformSetters.uChunkMaxY) {
                        lod1ReconstructMaterial.uniformSetters.uChunkMaxY(chunkMaxY);
                    }

                    const modelViewMatrix = mat4.translate(mat4.create(), camera.getView(), regionOffset);
                    if (lod1ReconstructMaterial.uniformSetters.modelViewMatrix) {
                        lod1ReconstructMaterial.uniformSetters.modelViewMatrix(modelViewMatrix);
                    }

                    // Bind color textures only (no depth maps required for shading)
                    const texs = lod.textures;
                    if (lod1ReconstructMaterial.uniformSetters.texTopColor) {
                        lod1ReconstructMaterial.uniformSetters.texTopColor(texs.texTopColor);
                    }
                    if (lod1ReconstructMaterial.uniformSetters.texNorthColor) {
                        lod1ReconstructMaterial.uniformSetters.texNorthColor(texs.texNorthColor);
                    }
                    if (lod1ReconstructMaterial.uniformSetters.texSouthColor) {
                        lod1ReconstructMaterial.uniformSetters.texSouthColor(texs.texSouthColor);
                    }
                    if (lod1ReconstructMaterial.uniformSetters.texEastColor) {
                        lod1ReconstructMaterial.uniformSetters.texEastColor(texs.texEastColor);
                    }
                    if (lod1ReconstructMaterial.uniformSetters.texWestColor) {
                        lod1ReconstructMaterial.uniformSetters.texWestColor(texs.texWestColor);
                    }

                    gl.drawArrays(gl.TRIANGLES, 0, lod.reconstructedGeometry.verts);
                }
            } else {
                // Original raymarching shader fallback
                bind(impostorMaterial, impostorGeometry);

                impostorMaterial.uniformSetters.uCameraPosition(camera.position);
                impostorMaterial.uniformSetters.uRegionOffset(regionOffset);
                impostorMaterial.uniformSetters.uMaxHeight(lod.maxHeight ?? 320.0);

                if (impostorMaterial.uniformSetters.uChunkMaxY) {
                    impostorMaterial.uniformSetters.uChunkMaxY(chunkMaxY);
                }

                const modelViewMatrix = mat4.translate(mat4.create(), camera.getView(), regionOffset);
                impostorMaterial.uniformSetters.modelViewMatrix(modelViewMatrix);

                // Bind both color and depth textures for DDA raymarching
                const texs = lod.textures;
                impostorMaterial.uniformSetters.texTop(texs.texTop);
                impostorMaterial.uniformSetters.texNorth(texs.texNorth);
                impostorMaterial.uniformSetters.texSouth(texs.texSouth);
                impostorMaterial.uniformSetters.texEast(texs.texEast);
                impostorMaterial.uniformSetters.texWest(texs.texWest);

                impostorMaterial.uniformSetters.texTopColor(texs.texTopColor);
                impostorMaterial.uniformSetters.texNorthColor(texs.texNorthColor);
                impostorMaterial.uniformSetters.texSouthColor(texs.texSouthColor);
                impostorMaterial.uniformSetters.texEastColor(texs.texEastColor);
                impostorMaterial.uniformSetters.texWestColor(texs.texWestColor);

                gl.drawArrays(gl.TRIANGLES, 0, impostorGeometry.verts);
            }
        }
    }

    // 5.5 Render LOD2 Groups
    if (lod2Geometry && lod2Material) {
        gl.disable(gl.BLEND);
        gl.enable(gl.DEPTH_TEST);

        const vpCurrent = mat4.multiply(mat4.create(), projectionMatrix, camera.getView());

        for (const group of cullResults.lod2Groups) {
            if (!group.hasTexture || !group.fbo) continue;

            bind(lod2Material, lod2Geometry);

            lod2Material.uniformSetters.uCameraPosition(camera.position);
            lod2Material.uniformSetters.uVPCurrent(vpCurrent);

            const groupOffset = vec3.fromValues(group.groupX * group.groupSize * 512, 0, group.groupZ * group.groupSize * 512);
            lod2Material.uniformSetters.uGroupOffset(groupOffset);
            lod2Material.uniformSetters.uOffset(groupOffset);

            const size = group.groupSize;
            lod2Material.uniformSetters.uGroupSize(size);

            const scale = vec3.fromValues(size * 512.0, 320.0, size * 512.0);
            lod2Material.uniformSetters.uScale(scale);

            const modelViewMatrix = mat4.translate(mat4.create(), camera.getView(), groupOffset);
            lod2Material.uniformSetters.modelViewMatrix(modelViewMatrix);

            lod2Material.uniformSetters.uVPInit(group.vpMatrix);

            // Bind color and depth textures
            lod2Material.uniformSetters.uColorTex(group.fbo.attachments[0]);
            lod2Material.uniformSetters.uDepthTex(group.fbo.attachments[1]);
            lod2Material.uniformSetters.uFogScale(fogScale);

            gl.drawArrays(gl.TRIANGLES, 0, lod2Geometry.verts);
        }
    }

    // 6. Draw Boundary Boxes (Wireframes)
    if (sceneGraph.showBoundaries) {
        renderBoundaries(gl, context, camera, sceneGraph, cube, renderedChunks, cullResults, projectionMatrix);
    }

    if (useScissor) {
        gl.disable(gl.SCISSOR_TEST);
    }

    if (context.renderToFBO) {
        gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    }

    let hasPendingLOD2Updates = false;
    if (impostorGeometry && impostorMaterial && lod2Geometry && lod2Material) {
        hasPendingLOD2Updates = cullResults.lod2Groups.some(group => !group.hasTexture || group.stale || group.angularDeviation > 0.05);
    }
    return hasPendingLOD2Updates;
}
