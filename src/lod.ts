import { mat4, vec3, vec4 } from "gl-matrix";
import * as renderer from './renderer';
import * as twgl from 'twgl.js';
import { HttpError } from "./util";

interface RegionLOD {
    rx: number;
    rz: number;
    textures: {
        texTop: WebGLTexture;
        texNorth: WebGLTexture;
        texSouth: WebGLTexture;
        texEast: WebGLTexture;
        texWest: WebGLTexture;
        texTopColor: WebGLTexture;
        texNorthColor: WebGLTexture;
        texSouthColor: WebGLTexture;
        texEastColor: WebGLTexture;
        texWestColor: WebGLTexture;
    } | null;
    loaded: boolean;
}

function create2DTexture(gl: WebGL2RenderingContext, image: HTMLImageElement | ImageBitmap, isDepth: boolean): WebGLTexture {
    const notUsingSpector = true;
    const kind = notUsingSpector && isDepth ? gl.RED : gl.RGBA;
    const format = notUsingSpector && isDepth ? gl.R8 : gl.RGBA8;
    return twgl.createTexture(gl, {
        target: gl.TEXTURE_2D,
        internalFormat: format,
        format: kind,
        type: gl.UNSIGNED_BYTE,
        src: image,
        minMag: gl.NEAREST,
        wrap: gl.CLAMP_TO_EDGE
    });
}

function createDepthTextureWithMipmaps(gl: WebGL2RenderingContext, image: ImageBitmap, isMin: boolean): { texture: WebGLTexture; maxHeight: number } {
    const tex = gl.createTexture();
    if (!tex) throw new Error("Failed to create WebGL texture");
    gl.bindTexture(gl.TEXTURE_2D, tex);

    const width = image.width;
    const height = image.height;

    const canvas = document.createElement('canvas');
    canvas.width = width;
    canvas.height = height;
    const ctx = canvas.getContext('2d', { willReadFrequently: true });
    if (!ctx) throw new Error("Could not create 2d canvas context");
    ctx.drawImage(image, 0, 0);
    const imgData = ctx.getImageData(0, 0, width, height);
    const srcPixels = imgData.data;

    let maxVal = 0;
    let lastWidth = width;
    let lastHeight = height;
    let lastPixels = new Uint8Array(width * height);
    for (let i = 0; i < width * height; i++) {
        const val = srcPixels[i * 4];
        lastPixels[i] = val;
        if (val < 255 && val > maxVal) {
            maxVal = val;
        }
    }

    const maxHeight = Math.min(320.0, Math.ceil(((maxVal + 2) / 255.0) * 320.0));

    const levels = Math.floor(Math.log2(Math.max(width, height))) + 1;

    gl.pixelStorei(gl.UNPACK_ALIGNMENT, 1);
    gl.texStorage2D(gl.TEXTURE_2D, levels, gl.R8, width, height);
    gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, width, height, gl.RED, gl.UNSIGNED_BYTE, lastPixels);

    for (let level = 1; level < levels; level++) {
        const nextWidth = Math.max(1, lastWidth >> 1);
        const nextHeight = Math.max(1, lastHeight >> 1);
        const nextPixels = new Uint8Array(nextWidth * nextHeight);

        for (let y = 0; y < nextHeight; y++) {
            for (let x = 0; x < nextWidth; x++) {
                const srcX0 = x * 2;
                const srcX1 = Math.min(lastWidth - 1, x * 2 + 1);
                const srcY0 = y * 2;
                const srcY1 = Math.min(lastHeight - 1, y * 2 + 1);

                const val00 = lastPixels[srcX0 + srcY0 * lastWidth];
                const val10 = lastPixels[srcX1 + srcY0 * lastWidth];
                const val01 = lastPixels[srcX0 + srcY1 * lastWidth];
                const val11 = lastPixels[srcX1 + srcY1 * lastWidth];

                const vals = [val00, val10, val01, val11];
                const solidVals = vals.filter(v => v < 255);
                let targetVal: number;
                if (solidVals.length > 0) {
                    if (isMin) {
                        targetVal = Math.min(...solidVals);
                    } else {
                        targetVal = Math.max(...solidVals);
                    }
                } else {
                    targetVal = 255;
                }

                nextPixels[x + y * nextWidth] = targetVal;
            }
        }

        gl.texSubImage2D(gl.TEXTURE_2D, level, 0, 0, nextWidth, nextHeight, gl.RED, gl.UNSIGNED_BYTE, nextPixels);

        lastWidth = nextWidth;
        lastHeight = nextHeight;
        lastPixels = nextPixels;
    }
    gl.pixelStorei(gl.UNPACK_ALIGNMENT, 4);

    twgl.setTextureParameters(gl, tex, {
        wrap: gl.CLAMP_TO_EDGE,
        min: gl.NEAREST_MIPMAP_NEAREST,
        mag: gl.NEAREST
    });

    return { texture: tex, maxHeight };
}

export function fetchRegionLOD(
    rx: number,
    rz: number,
    sceneGraph: renderer.SceneGraph,
    cameraPosition: any,
    render: () => void
) {
    const key = `${rx}.${rz}`;
    const region = sceneGraph.getOrCreateRegion(rx, rz);
    if (region.impostor.status !== 'NONE') {
        return;
    }

    const meta = sceneGraph.mapMetadata;
    if (meta && meta.loaded) {
        const isFull = meta.full_regions.has(key);
        const isLod = meta.lod_regions.has(key);
        const isTile = meta.tile_regions.has(key);

        if (!isFull && !isLod && !isTile) {
            // Not in any active metadata regions, skip completely
            sceneGraph.updateImpostorStatus(rx, rz, 'ERROR');
            return;
        }
    }

    sceneGraph.updateImpostorStatus(rx, rz, 'FETCH');

    // Calculate distance to region center as priority (closer is higher priority, i.e., lower priority number)
    const rCenter = [rx * 512 + 256, 120, rz * 512 + 256];
    const dx = cameraPosition[0] - rCenter[0];
    const dy = cameraPosition[1] - rCenter[1];
    const dz = cameraPosition[2] - rCenter[2];
    const priority = dx * dx + dy * dy + dz * dz;

    sceneGraph.requestManager.enqueue({
        type: 'IMPOSTOR',
        key: `impostor:${key}`,
        priority,
        run: async () => {
            try {
                const meta = sceneGraph.mapMetadata;
                const loadBin = !meta || !meta.loaded || meta.full_regions.has(key) || meta.lod_regions.has(key);

                const topColorImgPromise: Promise<ImageBitmap> = fetch(`map/tiles/r.${rx}.${rz}.png`)
                    .then(async (res: Response) => {
                        if (!res.ok) throw new HttpError(res);
                        const blob = await res.blob();
                        return createImageBitmap(blob);
                    });

                let binPromise: Promise<ArrayBuffer | null>;
                if (loadBin) {
                    binPromise = fetch(`map/lods/r.${rx}.${rz}.bin`)
                        .then(async (res: Response) => {
                            if (!res.ok) throw new HttpError(res);
                            return res.arrayBuffer();
                        });
                } else {
                    binPromise = Promise.resolve(null);
                }

                const [topColorImg, binBuffer] = await Promise.allSettled([topColorImgPromise, binPromise]);
                if (topColorImg.status === 'rejected') {
                    throw topColorImg.reason;
                }
                if (binBuffer.status === 'rejected') {
                    throw binBuffer.reason;
                }

                const sideImgs: ImageBitmap[] = [];
                const typeToImgIndex: { [type: number]: number } = {};

                if (binBuffer.value !== null) {
                    const uint8Array = new Uint8Array(binBuffer.value);
                    const dataView = new DataView(binBuffer.value);

                    let offset = 0;
                    const imgPromises: Promise<ImageBitmap>[] = [];

                    while (offset < uint8Array.length) {
                        if (offset + 5 > uint8Array.length) break;
                        const type = uint8Array[offset];
                        offset += 1;
                        const length = dataView.getUint32(offset, true);
                        offset += 4;
                        if (offset + length > uint8Array.length) break;

                        const value = uint8Array.subarray(offset, offset + length);
                        offset += length;

                        const blob = new Blob([value], { type: 'image/png' });
                        const imgIdx = imgPromises.length;
                        imgPromises.push(createImageBitmap(blob, {
                            colorSpaceConversion: 'none',
                            premultiplyAlpha: 'none'
                        }));
                        typeToImgIndex[type] = imgIdx;
                    }

                    const loadedSideImgs = await Promise.all(imgPromises);
                    sideImgs.push(...loadedSideImgs);
                }

                const context = sceneGraph.context;

                const getTex = (type: number, isDepth: boolean, isMin?: boolean): { texture: WebGLTexture; maxHeight?: number } => {
                    const idx = typeToImgIndex[type];
                    if (idx === undefined) {
                        return {
                            texture: twgl.createTexture(context.gl, {
                                src: [255, 255, 255, 255]
                            }),
                            maxHeight: 0
                        };
                    }
                    const img = sideImgs[idx];
                    if (isDepth) {
                        return createDepthTextureWithMipmaps(context.gl, img, !!isMin);
                    }
                    return {
                        texture: create2DTexture(context.gl, img, isDepth)
                    };
                };

                const texTopResult = getTex(0, true, false);
                const textures = {
                    texTopColor: create2DTexture(context.gl, topColorImg.value, false),
                    texTop: texTopResult.texture,
                    texNorthColor: getTex(1, false).texture,
                    texNorth: getTex(2, true, false).texture,
                    texSouthColor: getTex(3, false).texture,
                    texSouth: getTex(4, true, true).texture,
                    texEastColor: getTex(5, false).texture,
                    texEast: getTex(6, true, false).texture,
                    texWestColor: getTex(7, false).texture,
                    texWest: getTex(8, true, true).texture
                };

                const maxHeight = texTopResult.maxHeight ?? 320.0;

                // Clean up ImageBitmaps from memory immediately after texture upload
                for (const img of sideImgs) {
                    if (img instanceof ImageBitmap) {
                        img.close();
                    }
                }
                if (topColorImg.value instanceof ImageBitmap) {
                    topColorImg.value.close();
                }

                sceneGraph.updateImpostorStatus(rx, rz, 'READY', textures, maxHeight);
                render();
            } catch (e) {
                sceneGraph.updateImpostorStatus(rx, rz, 'ERROR');
                if (e instanceof HttpError && e.status === 404) {
                    return;
                }
                console.warn(`LOD loading failed for region ${rx},${rz}:`, e);
                throw e;
            }
        }
    });
}

export function makeLOD1Geometry(gl: WebGL2RenderingContext): renderer.Geometry {
    const stride = 12;
    const stridef = (stride / 4) | 0;
    const tris = 12;
    const buffer = new ArrayBuffer(stride * tris * 3);
    const bf32 = new Float32Array(buffer);

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

    vec3.set(FLD, 0, 0, 512);
    vec3.set(FLU, 0, 320, 512);
    vec3.set(FRD, 512, 0, 512);
    vec3.set(FRU, 512, 320, 512);
    vec3.set(BLD, 0, 0, 0);
    vec3.set(BLU, 0, 320, 0);
    vec3.set(BRD, 512, 0, 0);
    vec3.set(BRU, 512, 320, 0);

    addQuad(FLU, BLU, BLD, FLD, 0);
    addQuad(FRD, BRD, BRU, FRU, 2);
    addQuad(FRU, FLU, FLD, FRD, 4);
    addQuad(BRD, BLD, BLU, BRU, 6);
    addQuad(FLU, FRU, BRU, BLU, 8);
    addQuad(BLD, BRD, FRD, FLD, 10);

    let geometry = new renderer.Geometry(gl);
    geometry.setAttributes({
        position: { data: bf32, numComponents: 3, offset: 0 },
    });
    geometry.verts = tris * 3;
    return geometry;
}

export class LOD2Group {
    fbo: twgl.FramebufferInfo | null = null;
    initialCameraPos = vec3.create();
    vpMatrix = mat4.create();
    hasTexture = false;
    lastActiveFrame = 0;
    angularDeviation = 0;
    stale = false;

    // Bounds in world space
    minX = 0;
    maxX = 0;
    minZ = 0;
    maxZ = 0;
    center = vec3.create();

    constructor(
        public groupX: number,
        public groupZ: number,
        public groupSize: number
    ) {
        this.minX = groupX * groupSize * 512;
        this.maxX = (groupX + 1) * groupSize * 512;
        this.minZ = groupZ * groupSize * 512;
        this.maxZ = (groupZ + 1) * groupSize * 512;

        vec3.set(this.center, (this.minX + this.maxX) * 0.5, 160.0, (this.minZ + this.maxZ) * 0.5);
    }

    destroy(gl: WebGL2RenderingContext) {
        if (this.fbo) {
            gl.deleteFramebuffer(this.fbo.framebuffer);
            for (const attachment of this.fbo.attachments) {
                if (attachment instanceof WebGLTexture) {
                    gl.deleteTexture(attachment);
                } else if (attachment instanceof WebGLRenderbuffer) {
                    gl.deleteRenderbuffer(attachment);
                }
            }
            this.fbo = null;
        }
        this.hasTexture = false;
    }
}

export class LOD2GroupManager {
    groups = new Map<string, LOD2Group>();

    constructor(public sceneGraph: renderer.SceneGraph) { }

    getOrCreateGroup(groupX: number, groupZ: number, groupSize: number): LOD2Group {
        const key = `${groupX},${groupZ}`;
        if (this.groups.has(key)) {
            const group = this.groups.get(key)!;
            if (group.groupSize !== groupSize) {
                group.destroy(this.sceneGraph.context.gl);
                const newGroup = new LOD2Group(groupX, groupZ, groupSize);
                this.groups.set(key, newGroup);
                return newGroup;
            }
            return group;
        }
        const group = new LOD2Group(groupX, groupZ, groupSize);
        this.groups.set(key, group);
        return group;
    }
}

export function renderGroupToFBO(
    gl: WebGL2RenderingContext,
    group: LOD2Group,
    sceneGraph: renderer.SceneGraph,
    camera: renderer.PerspectiveCamera,
    impostorGeometry: renderer.Geometry,
    impostorMaterial: renderer.Material
) {
    const canvasWidth = gl.canvas.width;
    const canvasHeight = gl.canvas.height;

    const viewMatrix = camera.getView();

    // 1. Calculate corners of the 3D bounding box
    const corners = [
        vec3.fromValues(group.minX, 0, group.minZ),
        vec3.fromValues(group.maxX, 0, group.minZ),
        vec3.fromValues(group.minX, 320, group.minZ),
        vec3.fromValues(group.maxX, 320, group.minZ),
        vec3.fromValues(group.minX, 0, group.maxZ),
        vec3.fromValues(group.maxX, 0, group.maxZ),
        vec3.fromValues(group.minX, 320, group.maxZ),
        vec3.fromValues(group.maxX, 320, group.maxZ)
    ];

    // 2. Determine tight near and far planes
    let maxZ = -Infinity;
    let minZ = Infinity;
    const viewCorners = corners.map(corner => {
        const vc = vec3.transformMat4(vec3.create(), corner, viewMatrix);
        maxZ = Math.max(maxZ, vc[2]);
        minZ = Math.min(minZ, vc[2]);
        return vc;
    });

    const nearPlane = Math.max(camera.near, -maxZ - 10.0);
    const farPlane = -minZ + 10.0;

    // 3. Clip the AABB's 12 edges against the camera's near plane in view space
    const edges = [
        [0, 1], [2, 3], [4, 5], [6, 7], // X edges
        [0, 2], [1, 3], [4, 6], [5, 7], // Y edges
        [0, 4], [1, 5], [2, 6], [3, 7]  // Z edges
    ];

    const clipNearZ = -camera.near;
    const clippedPoints: vec3[] = [];

    // Keep view-space corners that are in front of the camera near plane
    for (const pt of viewCorners) {
        if (pt[2] <= clipNearZ) {
            clippedPoints.push(pt);
        }
    }

    // Intersect edges crossing the camera near plane
    for (const [i, j] of edges) {
        const p1 = viewCorners[i];
        const p2 = viewCorners[j];
        const z1 = p1[2];
        const z2 = p2[2];

        if ((z1 < clipNearZ && z2 > clipNearZ) || (z1 > clipNearZ && z2 < clipNearZ)) {
            const t = (clipNearZ - z1) / (z2 - z1);
            const ix = p1[0] + t * (p2[0] - p1[0]);
            const iy = p1[1] + t * (p2[1] - p1[1]);
            clippedPoints.push(vec3.fromValues(ix, iy, clipNearZ));
        }
    }

    // If all points are behind the camera near plane, the volume is fully clipped
    if (clippedPoints.length === 0) {
        return;
    }

    // 4. Map the clipped points to the main camera's NDC space
    const vFovRad = camera.fov * Math.PI / 180.0;
    const main_top = camera.near * Math.tan(vFovRad / 2.0);
    const main_right = main_top * camera.aspect;

    let minNdcX = Infinity, maxNdcX = -Infinity;
    let minNdcY = Infinity, maxNdcY = -Infinity;

    for (const pt of clippedPoints) {
        // Standard perspective projection formulas
        const x_proj_main = pt[0] * camera.near / -pt[2];
        const y_proj_main = pt[1] * camera.near / -pt[2];

        const ndcX = x_proj_main / main_right;
        const ndcY = y_proj_main / main_top;

        minNdcX = Math.min(minNdcX, ndcX);
        maxNdcX = Math.max(maxNdcX, ndcX);
        minNdcY = Math.min(minNdcY, ndcY);
        maxNdcY = Math.max(maxNdcY, ndcY);
    }

    // 5. Convert NDC to pixels and pad to multiples of 16
    let pixelLeft = Math.floor((minNdcX + 1.0) * 0.5 * canvasWidth);
    let pixelRight = Math.ceil((maxNdcX + 1.0) * 0.5 * canvasWidth);
    let pixelBottom = Math.floor((minNdcY + 1.0) * 0.5 * canvasHeight);
    let pixelTop = Math.ceil((maxNdcY + 1.0) * 0.5 * canvasHeight);

    let desiredWidth = pixelRight - pixelLeft;
    let desiredHeight = pixelTop - pixelBottom;

    if (desiredWidth < 1 || desiredHeight < 1) {
        return;
    }

    const paddedWidth = Math.ceil(desiredWidth / 16) * 16;
    const paddedHeight = Math.ceil(desiredHeight / 16) * 16;

    const padX = paddedWidth - desiredWidth;
    const padY = paddedHeight - desiredHeight;

    pixelLeft = pixelLeft - Math.floor(padX / 2);
    pixelRight = pixelLeft + paddedWidth;

    pixelBottom = pixelBottom - Math.floor(padY / 2);
    pixelTop = pixelBottom + paddedHeight;

    desiredWidth = pixelRight - pixelLeft;
    desiredHeight = pixelTop - pixelBottom;

    desiredWidth = Math.max(16, Math.min(1024, desiredWidth));
    desiredHeight = Math.max(16, Math.min(1024, desiredHeight));

    // Recreate FBO if size changes
    if (group.fbo && (group.fbo.width !== desiredWidth || group.fbo.height !== desiredHeight)) {
        group.destroy(gl);
    }

    if (!group.fbo) {
        const attachments = [
            {
                internalFormat: gl.RGBA8,
                format: gl.RGBA,
                type: gl.UNSIGNED_BYTE,
                minMag: gl.NEAREST,
                wrap: gl.CLAMP_TO_EDGE
            },
            {
                attachmentPoint: gl.DEPTH_ATTACHMENT,
                internalFormat: gl.DEPTH_COMPONENT24,
                format: gl.DEPTH_COMPONENT,
                type: gl.UNSIGNED_INT,
                minMag: gl.NEAREST,
                wrap: gl.CLAMP_TO_EDGE
            }
        ];
        group.fbo = twgl.createFramebufferInfo(gl, attachments, desiredWidth, desiredHeight);
    }

    twgl.bindFramebufferInfo(gl, group.fbo);
    gl.viewport(0, 0, desiredWidth, desiredHeight);

    gl.clearColor(0.0, 0.0, 0.0, 0.0);
    gl.clearDepth(0.0);
    gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);

    gl.disable(gl.SCISSOR_TEST);
    gl.disable(gl.CULL_FACE);
    gl.enable(gl.DEPTH_TEST);
    gl.depthFunc(gl.GREATER);

    // 6. Convert final padded pixel bounds back to NDC coordinates
    const ndcLeft = (pixelLeft / canvasWidth) * 2.0 - 1.0;
    const ndcRight = (pixelRight / canvasWidth) * 2.0 - 1.0;
    const ndcBottom = (pixelBottom / canvasHeight) * 2.0 - 1.0;
    const ndcTop = (pixelTop / canvasHeight) * 2.0 - 1.0;

    // 7. Project NDC back to coordinates at the custom near plane
    const scaleFactor = nearPlane / camera.near;
    const left = ndcLeft * main_right * scaleFactor;
    const right = ndcRight * main_right * scaleFactor;
    const bottom = ndcBottom * main_top * scaleFactor;
    const top = ndcTop * main_top * scaleFactor;

    // 8. Construct the custom projection frustum
    const projectionMatrix = mat4.create();
    mat4.frustum(projectionMatrix, left, right, bottom, top, nearPlane, farPlane);

    // Apply your original inverse-Z depth mapping formulas
    projectionMatrix[10] = 1.0;
    projectionMatrix[14] = 2.0 * nearPlane;

    mat4.multiply(group.vpMatrix, projectionMatrix, viewMatrix);

    const regionsInGroup = Array.from(sceneGraph.regions.values()).filter(r =>
        Math.floor(r.rx / group.groupSize) === group.groupX && Math.floor(r.rz / group.groupSize) === group.groupZ
    );

    let activeProgram: WebGLProgram | null = null;
    function bind(mat: renderer.Material, geo: renderer.Geometry) {
        if (mat.program !== activeProgram) {
            activeProgram = mat.program;
            gl.useProgram(mat.program);
            if (mat.uniformSetters.projectionMatrix) {
                mat.uniformSetters.projectionMatrix(projectionMatrix);
            }
            if (mat.uniformSetters.cameraPosition) {
                mat.uniformSetters.cameraPosition(camera.position);
            }
            if (mat.uniformSetters.uFogScale) {
                mat.uniformSetters.uFogScale(0.0);
            }
            for (const [key, value] of Object.entries(geo.attributes)) {
                if (mat.attribSetters[key]) {
                    mat.attribSetters[key](value);
                }
            }
        }
    }

    for (const region of regionsInGroup) {
        const allRegionletsReady = region.regionlets.every(rlet => rlet.status === 'READY');
        if (!allRegionletsReady && region.impostor.status === 'READY' && region.impostor.textures) {
            bind(impostorMaterial, impostorGeometry);

            if (impostorMaterial.uniformSetters.uCameraPosition) {
                impostorMaterial.uniformSetters.uCameraPosition(camera.position);
            }

            const regionOffset = vec3.fromValues(region.rx * 512, 0, region.rz * 512);
            if (impostorMaterial.uniformSetters.uRegionOffset) {
                impostorMaterial.uniformSetters.uRegionOffset(regionOffset);
            }

            if (impostorMaterial.uniformSetters.uMaxHeight) {
                impostorMaterial.uniformSetters.uMaxHeight(region.impostor.maxHeight ?? 320.0);
            }

            const mvMatrix = mat4.translate(mat4.create(), viewMatrix, regionOffset);
            if (impostorMaterial.uniformSetters.modelViewMatrix) {
                impostorMaterial.uniformSetters.modelViewMatrix(mvMatrix);
            }

            const texs = region.impostor.textures;
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
