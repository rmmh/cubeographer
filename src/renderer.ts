import { glMatrix, mat4, quat, vec3, vec4 } from 'gl-matrix';

import * as twgl from 'twgl.js';
import { generateTextureArrayMipmaps } from "./downscale";
import { safeLookAt } from "./camera";
import { renderBoundaries } from './render_debug';

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
        gl.bindTexture(gl.TEXTURE_2D_ARRAY, texture);

        // Pre-allocate WebGL 2 immutable 3D texture storage (with 4 mipmap levels)
        gl.texStorage3D(
            gl.TEXTURE_2D_ARRAY,
            5, // 5 levels (16x16, 8x8, 4x4, 2x2, 1x1)
            gl.RGBA8,
            16,
            16,
            1024
        );

        // Asynchronously load the image
        var image = new Image();
        image.src = path;
        image.addEventListener('load', function () {
            // Draw the loaded image to a temporary canvas to read its pixel data
            const canvas = document.createElement('canvas');
            canvas.width = 512;
            canvas.height = 512;
            const ctx = canvas.getContext('2d');
            ctx.drawImage(image, 0, 0);

            // Slices are always extracted from the top 512x512 portion
            const imgData = ctx.getImageData(0, 0, 512, 512);
            const srcPixels = imgData.data;

            // Allocate a buffer to hold the sliced 1024 tiles
            const slicedPixels = new Uint8Array(16 * 16 * 4 * 1024);

            // Copy each 16x16 tile into its respective slice/layer
            for (let slice = 0; slice < 1024; slice++) {
                const tileX = (slice % 32) * 16;
                const tileY = Math.floor(slice / 32) * 16;

                for (let y = 0; y < 16; y++) {
                    const srcRowStart = ((tileY + y) * 512 + tileX) * 4;
                    const destRowStart = (slice * 16 * 16 + y * 16) * 4;

                    // Copy 16 pixels (64 bytes)
                    for (let i = 0; i < 64; i++) {
                        slicedPixels[destRowStart + i] = srcPixels[srcRowStart + i];
                    }
                }
            }

            // Upload the sliced pixel data to WebGL
            gl.bindTexture(gl.TEXTURE_2D_ARRAY, texture);
            gl.texSubImage3D(
                gl.TEXTURE_2D_ARRAY,
                0,
                0, 0, 0, // xoffset, yoffset, zoffset
                16, 16, 1024, // width, height, depth
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

export class SceneGraph {
    regions = new Map<string, RegionNode>();
    requestManager = new RequestManager();
    maxHighResChunks = 8;
    showBoundaries = false;
    private listeners = new Set<() => void>();

    constructor(public context: Context) { }

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
        this.notify();
    }

    cull(camera: PerspectiveCamera): {
        chunks: Chunk[];
        impostors: ImpostorNode[];
        missingRegionlets: RegionletNode[];
        missingImpostors: ImpostorNode[];
    } {
        const frustum = new Frustum(camera);
        const chunksToRender: Chunk[] = [];
        const impostorsToRender: ImpostorNode[] = [];
        const missingRegionlets: RegionletNode[] = [];
        const missingImpostors: ImpostorNode[] = [];

        // 1. Gather all visible regionlets across all regions in the frustum (loaded or not)
        const allVisibleRegionlets: { rlet: RegionletNode; distSq: number }[] = [];

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
                allVisibleRegionlets.some(x => x.rlet === rlet)
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

        return {
            chunks: chunksToRender,
            impostors: impostorsToRender,
            missingRegionlets,
            missingImpostors
        };
    }
}

export function render(
    context: Context,
    camera: PerspectiveCamera,
    sceneGraph: SceneGraph,
    layers: InstancedLayer[],
    cube: Mesh,
    impostorGeometry?: Geometry,
    impostorMaterial?: Material
) {
    const gl = context.gl;

    if (!(gl.canvas instanceof HTMLCanvasElement))
        return;

    if (context.renderToFBO) {
        context.updateFBO(gl.canvas.width, gl.canvas.height);
        twgl.bindFramebufferInfo(gl, context.fboInfo);
    } else {
        twgl.bindFramebufferInfo(gl, null);
    }

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
    const cullResults = sceneGraph.cull(camera);
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
            if (mat.uniformSetters.offset)
                mat.uniformSetters.offset(chunk.position);

            var matrix = mat4.translate(mat4.create(), camera.getView(), chunk.position);
            if (mat.uniformSetters.modelViewMatrix)
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

            if (impostorMaterial.uniformSetters.uCameraPosition) {
                impostorMaterial.uniformSetters.uCameraPosition(camera.position);
            }

            const regionOffset = vec3.fromValues(lod.rx * 512, 0, lod.rz * 512);
            if (impostorMaterial.uniformSetters.uRegionOffset) {
                impostorMaterial.uniformSetters.uRegionOffset(regionOffset);
            }

            if (impostorMaterial.uniformSetters.uMaxHeight) {
                impostorMaterial.uniformSetters.uMaxHeight(lod.maxHeight ?? 320.0);
            }

            const modelViewMatrix = mat4.translate(mat4.create(), camera.getView(), regionOffset);
            if (impostorMaterial.uniformSetters.modelViewMatrix) {
                impostorMaterial.uniformSetters.modelViewMatrix(modelViewMatrix);
            }

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
}
