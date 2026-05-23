import { vec3 } from "gl-matrix";
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

function createDepthTextureWithMipmaps(gl: WebGL2RenderingContext, image: HTMLImageElement | ImageBitmap, isMin: boolean): { texture: WebGLTexture; maxHeight: number } {
    const tex = gl.createTexture();
    if (!tex) throw new Error("Failed to create WebGL texture");
    gl.bindTexture(gl.TEXTURE_2D, tex);

    const width = image.width;
    const height = image.height;

    const canvas = document.createElement('canvas');
    canvas.width = width;
    canvas.height = height;
    const ctx = canvas.getContext('2d');
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

        console.log("YOHOHO");

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

export function makeImpostorGeometry(gl: WebGL2RenderingContext): renderer.Geometry {
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
