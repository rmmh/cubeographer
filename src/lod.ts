import { vec3 } from "gl-matrix";
import * as renderer from './renderer';

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

export const regionLODs = new Map<string, RegionLOD>();

function create2DTexture(gl: WebGL2RenderingContext, image: HTMLImageElement, isDepth: boolean): WebGLTexture {
    const tex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, tex);
    const notUsingSpector = true;
    const kind = notUsingSpector && isDepth ? gl.RED : gl.RGBA;
    const format = notUsingSpector && isDepth ? gl.R8 : gl.RGBA8;
    gl.texImage2D(gl.TEXTURE_2D, 0, format, image.width, image.height, 0, kind, gl.UNSIGNED_BYTE, image);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    return tex;
}

function createDepthTextureWithMipmaps(gl: WebGL2RenderingContext, image: HTMLImageElement, isMin: boolean): WebGLTexture {
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

    let lastWidth = width;
    let lastHeight = height;
    let lastPixels = new Uint8Array(width * height);
    for (let i = 0; i < width * height; i++) {
        lastPixels[i] = srcPixels[i * 4];
    }

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

    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST_MIPMAP_NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);

    return tex;
}

function fetchImage(url: string): Promise<HTMLImageElement> {
    return new Promise((resolve, reject) => {
        const img = new Image();
        img.onload = () => resolve(img);
        img.onerror = (err) => reject(err);
        img.src = url;
    });
}

export async function fetchRegionLOD(rx: number, rz: number, context: renderer.Context, render: () => void): Promise<RegionLOD | null> {
    const key = `${rx},${rz}`;
    if (regionLODs.has(key)) {
        return regionLODs.get(key);
    }

    const lod: RegionLOD = {
        rx,
        rz,
        textures: null,
        loaded: false
    };
    regionLODs.set(key, lod);

    try {
        const topColorImgPromise = fetchImage(`map/tiles/r.${rx}.${rz}.png`);

        const binResponse = await fetch(`map/lods/r.${rx}.${rz}.bin`);
        if (!binResponse.ok) {
            throw new Error(`failed to fetch LOD bin for region ${rx},${rz}`);
        }
        const binBuffer = await binResponse.arrayBuffer();
        const uint8Array = new Uint8Array(binBuffer);
        const dataView = new DataView(binBuffer);

        let offset = 0;
        const imgPromises: Promise<HTMLImageElement>[] = [];
        const typeToImgIndex: { [type: number]: number } = {};

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
            const url = URL.createObjectURL(blob);

            const imgIdx = imgPromises.length;
            imgPromises.push(fetchImage(url).then(img => {
                URL.revokeObjectURL(url);
                return img;
            }));
            typeToImgIndex[type] = imgIdx;
        }

        const [topColorImg, ...sideImgs] = await Promise.all([
            topColorImgPromise,
            ...imgPromises
        ]);

        const gl2 = context.gl as WebGL2RenderingContext;

        const getTex = (type: number, isDepth: boolean, isMin?: boolean) => {
            const idx = typeToImgIndex[type];
            if (idx === undefined) {
                const tex = gl2.createTexture();
                gl2.bindTexture(gl2.TEXTURE_2D, tex);
                gl2.texImage2D(gl2.TEXTURE_2D, 0, gl2.RGBA, 1, 1, 0, gl2.RGBA, gl2.UNSIGNED_BYTE, new Uint8Array([255, 255, 255, 255]));
                return tex;
            }
            const img = sideImgs[idx];
            if (isDepth) {
                return createDepthTextureWithMipmaps(gl2, img, !!isMin);
            }
            return create2DTexture(gl2, img, isDepth);
        };

        lod.textures = {
            texTopColor: create2DTexture(gl2, topColorImg, false),
            texTop: getTex(0, true, false),
            texNorthColor: getTex(1, false),
            texNorth: getTex(2, true, false),
            texSouthColor: getTex(3, false),
            texSouth: getTex(4, true, true),
            texEastColor: getTex(5, false),
            texEast: getTex(6, true, false),
            texWestColor: getTex(7, false),
            texWest: getTex(8, true, true)
        };
        lod.loaded = true;
        render();
        return lod;
    } catch (e) {
        console.warn(`LOD loading failed for region ${rx},${rz}:`, e);
        return null;
    }
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
