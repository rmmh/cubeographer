/**
 * Generates custom, transparency-weighted mipmaps for a WebGL 2 TEXTURE_2D_ARRAY in sRGB space.
 * It weights colors by their alpha value to avoid bright/dark edge bleeding.
 * For binary alpha cutouts, it strictly preserves binary alpha (0 or 255) using a 50% coverage
 * threshold to keep cutout edges perfectly crisp, while falling back to standard box-filtered
 * averaging if the input neighborhood contains intermediate transparency.
 */
export function generateTextureArrayMipmaps(
    gl: WebGL2RenderingContext,
    texture: WebGLTexture,
    basePixels: Uint8Array, // level 0 pixels, size: 16 * 16 * 4 * 1024
    levels: number
) {
    gl.bindTexture(gl.TEXTURE_2D_ARRAY, texture);

    let lastDim = 16;
    let lastPixels = basePixels;

    for (let level = 1; level <= levels; level++) {
        const dim = lastDim >> 1;
        const slicedPixels = new Uint8Array(dim * dim * 4 * 1024);
        const ls = lastDim * 4; // last row stride in bytes

        for (let slice = 0; slice < 1024; slice++) {
            const lastSliceStart = slice * lastDim * lastDim * 4;
            const sliceStart = slice * dim * dim * 4;

            for (let y = 0; y < dim; y++) {
                for (let x = 0; x < dim; x++) {
                    const destOffset = sliceStart + (x + y * dim) * 4;
                    const srcOffset = lastSliceStart + (x * 2 + y * 2 * lastDim) * 4;

                    // The 4 source pixel offsets in the 2x2 box
                    const idxs = [
                        srcOffset,
                        srcOffset + 4,
                        srcOffset + ls,
                        srcOffset + ls + 4
                    ];

                    let sumR = 0;
                    let sumG = 0;
                    let sumB = 0;
                    let sumA = 0;
                    let totalWeight = 0;

                    let hasIntermediate = false;
                    let opaqueCount = 0;

                    for (let i = 0; i < 4; i++) {
                        const idx = idxs[i];
                        const a = lastPixels[idx + 3];
                        sumA += a;

                        if (a > 0 && a < 255) {
                            hasIntermediate = true;
                        }
                        if (a === 255) {
                            opaqueCount++;
                        }

                        if (a > 0) {
                            const weight = a / 255.0;
                            sumR += lastPixels[idx] * weight;
                            sumG += lastPixels[idx + 1] * weight;
                            sumB += lastPixels[idx + 2] * weight;
                            totalWeight += weight;
                        }
                    }

                    if (totalWeight > 0) {
                        slicedPixels[destOffset] = Math.max(0, Math.min(255, Math.round(sumR / totalWeight)));
                        slicedPixels[destOffset + 1] = Math.max(0, Math.min(255, Math.round(sumG / totalWeight)));
                        slicedPixels[destOffset + 2] = Math.max(0, Math.min(255, Math.round(sumB / totalWeight)));

                        if (hasIntermediate) {
                            // If input had intermediate alphas, standard box-filter average
                            slicedPixels[destOffset + 3] = Math.max(0, Math.min(255, Math.round(sumA / 4.0)));
                        } else {
                            // Otherwise, purely binary 0 or 255 (requires at least 2 pixels to be opaque to stay opaque)
                            slicedPixels[destOffset + 3] = (opaqueCount >= 2) ? 255 : 0;
                        }
                    } else {
                        // All 4 source pixels are fully transparent
                        slicedPixels[destOffset] = 0;
                        slicedPixels[destOffset + 1] = 0;
                        slicedPixels[destOffset + 2] = 0;
                        slicedPixels[destOffset + 3] = 0;
                    }
                }
            }
        }

        // Upload the entire mipmap level at once
        gl.texSubImage3D(
            gl.TEXTURE_2D_ARRAY,
            level,
            0, 0, 0, // xoffset, yoffset, zoffset
            dim, dim, 1024, // width, height, depth
            gl.RGBA,
            gl.UNSIGNED_BYTE,
            slicedPixels
        );

        lastDim = dim;
        lastPixels = slicedPixels;
    }
}
