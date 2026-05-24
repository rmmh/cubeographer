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
    occluded: boolean
    query: WebGLQuery
    queryInProgress: boolean
    voxelBitset?: any

    constructor(public gl: WebGL2RenderingContext) {
        this.position = vec3.create();
        this.query = null;
        this.queryInProgress = false;
        this.occluded = false;
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
        this.canvas.height = innerHeight;
        this.canvas.width = width;
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

function sphereCone(sphereCenter: vec3, sphereRadius: number,
    coneOrigin: vec3, coneNormal: vec3,
    sinAngle: number, tanAngleSqPlusOne: number): boolean {
    const diff = vec3.sub(vec3.create(), sphereCenter, coneOrigin);

    // If the cone origin (camera) is inside the sphere, it always intersects the frustum!
    if (vec3.sqrLen(diff) <= sphereRadius * sphereRadius) {
        return true;
    }

    // this code is somehow broken. unfortunate. this approximation helps slightly.
    let cos = Math.sqrt(1 - sinAngle * sinAngle);
    return vec3.dot(coneNormal, vec3.scaleAndAdd(vec3.create(), diff, coneNormal, sphereRadius * sinAngle)) > cos;

    // translated from https://github.com/mosra/magnum/blob/master/src/Magnum/Math/Intersection.h#L539-L565

    /* Point - cone test */
    // if (Math:: dot(diff - sphereRadius * sinAngle * coneNormal, coneNormal) > T(0)) {

    let dot = vec3.dot(coneNormal, vec3.scaleAndAdd(vec3.create(),
        diff, coneNormal, -sphereRadius * sinAngle));
    if (dot > 0) {
        // const Vector3<T>c = sinAngle * diff + coneNormal * sphereRadius;
        const c = vec3.scale(vec3.create(), diff, sinAngle);
        vec3.scaleAndAdd(c, c, coneNormal, sphereRadius);

        // const T lenA = Math:: dot(c, coneNormal);
        const lenA = vec3.dot(c, coneNormal);

        console.log(`cone test, dot=${dot} lenA=${lenA} c=${c}`);

        // return c.dot() <= lenA * lenA * tanAngleSqPlusOne;
        return vec3.sqrLen(c) <= lenA * lenA * tanAngleSqPlusOne;
        // } else return diff.dot() <= sphereRadius * sphereRadius;
    } else {
        console.log("near fallback", dot, coneNormal,
            vec3.scaleAndAdd(vec3.create(),
                diff, coneNormal, -sphereRadius * sinAngle),
            diff, vec3.len(diff), sphereRadius);
        return vec3.sqrLen(diff) <= sphereRadius * sphereRadius;
    }
}

export class Frustum {
    coneOrigin: vec3
    coneNormal: vec3
    coneAngle: number // radians

    constructor(
        public camera: PerspectiveCamera,
    ) {
        this.coneOrigin = vec3.copy(vec3.create(), camera.position);
        this.coneNormal = vec3.sub(vec3.create(), camera.target, camera.position);
        vec3.normalize(this.coneNormal, this.coneNormal);
        const vFovRad = camera.fov * Math.PI / 180;
        const hFovRad = 2 * Math.atan(Math.tan(vFovRad / 2) * camera.aspect);
        this.coneAngle = 2 * Math.atan(Math.sqrt(hFovRad * hFovRad + vFovRad * vFovRad));
    }

    intersects(c: Chunk): boolean {
        // TODO: center this more conservatively based on observed y-height?
        let sphereCenter = vec3.fromValues(128, 128, 128);
        vec3.add(sphereCenter, sphereCenter, c.position);
        let sphereRadius = Math.sqrt(3 * 128 * 128);

        const halfAngle = this.coneAngle * .5;


        const sinAngle = Math.sin(halfAngle);
        const tanAngle = Math.tan(halfAngle);
        const tanAngleSqPlusOne = 1 + tanAngle * tanAngle;

        if (!sphereCone(sphereCenter, sphereRadius, this.coneOrigin, this.coneNormal,
            sinAngle, tanAngleSqPlusOne)) {
            return false;
        }
        return true;

    }

    intersectsRegion(rx: number, rz: number): boolean {
        let sphereCenter = vec3.fromValues(rx * 512 + 256, 160, rz * 512 + 256);
        let sphereRadius = Math.sqrt(256 * 256 + 160 * 160 + 256 * 256);

        const halfAngle = this.coneAngle * .5;
        const sinAngle = Math.sin(halfAngle);
        const tanAngle = Math.tan(halfAngle);
        const tanAngleSqPlusOne = 1 + tanAngle * tanAngle;

        if (!sphereCone(sphereCenter, sphereRadius, this.coneOrigin, this.coneNormal,
            sinAngle, tanAngleSqPlusOne)) {
            return false;
        }
        return true;
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
    chunk: Chunk;
}

export interface ImpostorNode {
    rx: number;
    rz: number;
    status: ImpostorStatus;
    textures: any | null;
    loaded: boolean;
    maxHeight?: number;
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
    maxHighResChunks = 8;
    showBoundaries = false;
    mapMetadata: MapMetadata | null = null;
    lod2GroupSize = 2;
    lod2DistortionThreshold = 15.0;
    lod2UpdateBudget = 4;
    lod2StartDistance = 1536.0;
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
            const chunk = new Chunk(this.context.gl);
            vec3.set(chunk.position, rx * 512 + (off & 1) * 256, 0, rz * 512 + (off & 2) * 128);
            chunk.minY = 0;
            chunk.maxY = 255;
            regionlets.push({
                rx, rz, off,
                status: 'NONE',
                chunk
            });
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

    updateRegionletStatus(rx: number, rz: number, off: number, status: RegionletStatus, chunkDataCallback?: (chunk: Chunk) => void) {
        const region = this.getOrCreateRegion(rx, rz);
        const regionlet = region.regionlets[off];
        regionlet.status = status;
        if (chunkDataCallback) {
            chunkDataCallback(regionlet.chunk);
        }
        this.notify();
    }

    updateImpostorStatus(rx: number, rz: number, status: ImpostorStatus, textures: any = null, maxHeight?: number) {
        const region = this.getOrCreateRegion(rx, rz);
        region.impostor.status = status;
        region.impostor.loaded = (status === 'READY');
        if (textures) {
            region.impostor.textures = textures;
        }
        if (maxHeight !== undefined) {
            region.impostor.maxHeight = maxHeight;
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

        // 1. Gather all visible regionlets across all regions in the frustum (loaded or not)
        const allVisibleRegionlets: { rlet: RegionletNode; distSq: number }[] = [];
        const visibleRegionletSet = new Set<RegionletNode>();

        for (const region of this.regions.values()) {
            if (!frustum.intersectsRegion(region.rx, region.rz)) {
                continue;
            }

            for (const rlet of region.regionlets) {
                if (frustum.intersects(rlet.chunk)) {
                    const rletCenter = vec3.fromValues(128, 128, 128);
                    vec3.add(rletCenter, rletCenter, rlet.chunk.position);
                    const distSq = vec3.sqrDist(camera.position, rletCenter);
                    allVisibleRegionlets.push({ rlet, distSq });
                    visibleRegionletSet.add(rlet);
                }
            }
        }

        // 2. Sort all visible regionlets by distance to camera (closest first)
        allVisibleRegionlets.sort((a, b) => a.distSq - b.distSq);

        // 3. Define the target fetch set (top closest visible chunks, loaded or not)
        const targetRegionlets = allVisibleRegionlets.slice(0, this.maxHighResChunks);
        const targetSet = new Set<RegionletNode>(targetRegionlets.map(x => x.rlet));

        // 4. Identify all visible chunks that are actually LOADED
        const loadedVisible = allVisibleRegionlets.filter(x => x.rlet.status === 'READY' || x.rlet.status === 'STREAM');

        // 5. Determine which chunks will actually be rendered (top closest loaded chunks)
        const renderedChunks = loadedVisible.slice(0, this.maxHighResChunks);
        const renderedChunkSet = new Set<RegionletNode>(renderedChunks.map(x => x.rlet));

        // 6. Decide rendering and fetching per region
        for (const region of this.regions.values()) {
            if (!frustum.intersectsRegion(region.rx, region.rz)) {
                continue;
            }

            // Find all regionlets in this region that intersect the frustum
            const regionVisibleRlets = region.regionlets.filter(rlet =>
                visibleRegionletSet.has(rlet)
            );

            if (regionVisibleRlets.length === 0) {
                continue;
            }

            // A region is fully rendered as chunks if all of its visible regionlets are actually being rendered as chunks
            const allVisibleAreRendered = regionVisibleRlets.every(rlet => renderedChunkSet.has(rlet));

            if (allVisibleAreRendered) {
                // Render them as chunks!
                for (const rlet of regionVisibleRlets) {
                    chunksToRender.push(rlet.chunk);
                }
            } else {
                // Check if this region belongs to a distant LOD2 group
                const G = this.lod2GroupSize;
                const groupX = Math.floor(region.rx / G);
                const groupZ = Math.floor(region.rz / G);

                const groupMinX = groupX * G * 512;
                const groupMaxX = (groupX + 1) * G * 512;
                const groupMinZ = groupZ * G * 512;
                const groupMaxZ = (groupZ + 1) * G * 512;

                const groupCenterX = (groupMinX + groupMaxX) * 0.5;
                const groupCenterZ = (groupMinZ + groupMaxZ) * 0.5;

                const distToGroup = vec3.distance(camera.position, vec3.fromValues(groupCenterX, 160, groupCenterZ));

                if (distToGroup >= this.lod2StartDistance) {
                    // Render as LOD2!
                    const group = this.lod2Manager.getOrCreateGroup(groupX, groupZ, G);
                    lod2GroupsToRender.add(group);
                } else {
                    // Not fully rendered as chunks (some visible regionlets are missing or sliced out):
                    // A) Fetch missing regionlets ONLY if they are in the top 8 closest visible target set
                    for (const rlet of region.regionlets) {
                        if (rlet.status !== 'READY' && rlet.status !== 'STREAM') {
                            if (targetSet.has(rlet) && !rlet.chunk.occluded) {
                                missingRegionlets.push(rlet);
                            }
                        }
                    }

                    // B) Render the region's LOD impostor if loaded
                    if (region.impostor.status === 'READY' && region.impostor.textures) {
                        impostorsToRender.push(region.impostor);
                    } else {
                        // C) Fetch impostor if missing
                        if (region.impostor.status === 'NONE') {
                            missingImpostors.push(region.impostor);
                        }
                        // D) Render any loaded regionlets that are in our renderedChunkSet as fallback
                        for (const rlet of regionVisibleRlets) {
                            if (renderedChunkSet.has(rlet)) {
                                chunksToRender.push(rlet.chunk);
                            }
                        }
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
    lod2Material?: Material
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

    // 1. Process/update occlusion query results from previous frames
    for (const region of sceneGraph.regions.values()) {
        for (const rlet of region.regionlets) {
            const chunk = rlet.chunk;
            if (chunk.query && chunk.queryInProgress && gl.getQueryParameter(chunk.query, gl.QUERY_RESULT_AVAILABLE)) {
                chunk.occluded = !gl.getQueryParameter(chunk.query, gl.QUERY_RESULT);
                chunk.queryInProgress = false;
            }
        }
    }

    // 2. Perform frustum and visibility culling on the Scene Graph
    let culledChunks = cullResults.chunks;

    // Sort renderable chunks by distance to camera
    culledChunks.sort((a, b) => {
        const apos = vec3.fromValues(128, 128, 128);
        const bpos = vec3.fromValues(128, 128, 128);
        vec3.add(apos, apos, a.position);
        vec3.add(bpos, bpos, b.position);
        return vec3.sqrDist(camera.position, apos) - vec3.sqrDist(camera.position, bpos);
    });

    // Limit to rendering top chunks to match original behavior / performance target
    const renderedChunks = culledChunks.slice(0, sceneGraph.maxHighResChunks);

    var activeProgram: WebGLProgram
    function bind(mat: Material, geo: Geometry) {
        if (mat.program != activeProgram) {
            activeProgram = mat.program;
            gl.useProgram(mat.program);
            if (mat.uniformSetters.projectionMatrix)
                mat.uniformSetters.projectionMatrix(projectionMatrix);
            if (mat.uniformSetters.cameraPosition)
                mat.uniformSetters.cameraPosition(camera.position);
            if (mat.uniformSetters.uFogScale)
                mat.uniformSetters.uFogScale(fogScale);
            for (const [key, value] of Object.entries(geo.attributes)) {
                mat.attribSetters[key](value);
            }
        }
    }

    // 3. Occlusion query pass (performed BEFORE rendering standard layers to avoid bind swapping)
    const queryChunk = (chunk: Chunk, minY: number, maxY: number) => {
        if (chunk.query === null) {
            chunk.query = gl.createQuery();
        }
        if (!chunk.queryInProgress) {
            bind(cube.material, cube.geometry);
            cube.material.uniformSetters.modelViewMatrix(camera.getView());
            cube.material.uniformSetters.scale(vec3.fromValues(256, 1 + maxY - minY, 256));

            gl.enable(gl.CULL_FACE);
            gl.colorMask(false, false, false, false);
            gl.depthMask(false);

            gl.beginQuery(gl.ANY_SAMPLES_PASSED_CONSERVATIVE, chunk.query);
            const offset = vec3.fromValues(0, minY, 0);
            vec3.add(offset, offset, chunk.position);
            cube.material.uniformSetters.offset(offset);
            gl.drawArrays(gl.TRIANGLES, 0, 12 * 3);
            gl.endQuery(gl.ANY_SAMPLES_PASSED_CONSERVATIVE);

            gl.colorMask(true, true, true, true);
            gl.depthMask(true);
            gl.disable(gl.CULL_FACE);

            chunk.queryInProgress = true;
        }
    };

    // Trigger occlusion queries for far loaded chunks
    for (let i = 5; i < renderedChunks.length; i++) {
        queryChunk(renderedChunks[i], renderedChunks[i].minY, renderedChunks[i].maxY);
    }

    // Trigger occlusion queries for the closest missing chunks in frustum (culling missing things)
    const sortedMissing = cullResults.missingRegionlets.slice();
    sortedMissing.sort((a, b) => {
        const apos = vec3.fromValues(128, 128, 128);
        const bpos = vec3.fromValues(128, 128, 128);
        vec3.add(apos, apos, a.chunk.position);
        vec3.add(bpos, bpos, b.chunk.position);
        return vec3.sqrDist(camera.position, apos) - vec3.sqrDist(camera.position, bpos);
    });
    for (const rlet of sortedMissing.slice(0, sceneGraph.maxHighResChunks)) {
        queryChunk(rlet.chunk, 0, 255);
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

            if (chunkNum >= 5 && chunk.occluded) {
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

    // 5. Render Region Impostors as fallback
    if (impostorGeometry && impostorMaterial) {
        gl.disable(gl.BLEND);
        gl.enable(gl.DEPTH_TEST);

        for (const lod of cullResults.impostors) {
            if (!lod.loaded || !lod.textures) continue;

            bind(impostorMaterial, impostorGeometry);

            impostorMaterial.uniformSetters.uCameraPosition(camera.position);

            const regionOffset = vec3.fromValues(lod.rx * 512, 0, lod.rz * 512);
            impostorMaterial.uniformSetters.uRegionOffset(regionOffset);

            impostorMaterial.uniformSetters.uMaxHeight(lod.maxHeight ?? 320.0);

            const modelViewMatrix = mat4.translate(mat4.create(), camera.getView(), regionOffset);
            impostorMaterial.uniformSetters.modelViewMatrix(modelViewMatrix);

            // Bind textures
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
