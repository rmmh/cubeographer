import * as renderer from './renderer';

export function reconstructLOD1Geometry(gl: WebGL2RenderingContext, impostor: any): renderer.Geometry {
    const heightmaps = impostor.heightmaps;
    if (!heightmaps) {
        throw new Error("Missing heightmaps for LOD1 reconstruction");
    }

    const top = heightmaps.top;
    const north = heightmaps.north;
    const south = heightmaps.south;
    const east = heightmaps.east;
    const west = heightmaps.west;

    // Use a dynamic buffer that grows as we find faces
    let capacity = 100000;
    let vertexBuffer = new Uint16Array(capacity * 4); // 4 components per vertex
    let vertexCount = 0;

    function addVertex(x: number, y: number, z: number, normIndex: number) {
        if (vertexCount * 4 >= vertexBuffer.length) {
            // Resize buffer
            const newBuffer = new Uint16Array(vertexBuffer.length * 2);
            newBuffer.set(vertexBuffer);
            vertexBuffer = newBuffer;
        }
        const o = vertexCount * 4;
        vertexBuffer[o] = x;
        vertexBuffer[o + 1] = y;
        vertexBuffer[o + 2] = z;
        vertexBuffer[o + 3] = normIndex;
        vertexCount++;
    }

    function addQuad(
        ax: number, ay: number, az: number,
        bx: number, by: number, bz: number,
        cx: number, cy: number, cz: number,
        dx: number, dy: number, dz: number,
        normIndex: number
    ) {
        // Triangle 1
        addVertex(ax, ay, az, normIndex);
        addVertex(bx, by, bz, normIndex);
        addVertex(cx, cy, cz, normIndex);
        // Triangle 2
        addVertex(cx, cy, cz, normIndex);
        addVertex(dx, dy, dz, normIndex);
        addVertex(ax, ay, az, normIndex);
    }

    // Helper to check if a voxel at (x, y, z) is solid
    // x in 0..255, y in 0..159, z in 0..255
    function isSolid(x: number, y: number, z: number): boolean {
        if (x < 0 || x > 255 || y < 0 || y > 159 || z < 0 || z > 255) {
            return false;
        }

        const topVal = top[x + z * 256];
        if (topVal === 255) return false;
        // Y-axis height check: scale base comparison and apply DEPTH_BIAS (0.63 in voxel space)
        if (y > topVal * 160.0 / 255.0 + 0.63) return false;

        const northVal = north[x + (159 - y) * 256];
        if (northVal === 255) return false;
        // Z-axis North/South check: apply DEPTH_BIAS (1.004 in voxel space)
        if (z > northVal * 256.0 / 255.0 + 1.004) return false;

        const southVal = south[x + (159 - y) * 256];
        if (southVal === 255) return false;
        if (z < southVal * 256.0 / 255.0 - 1.004) return false;

        const eastVal = east[z + (159 - y) * 256];
        if (eastVal === 255) return false;
        // X-axis East/West check: apply DEPTH_BIAS (1.004 in voxel space)
        if (x > eastVal * 256.0 / 255.0 + 1.004) return false;

        const westVal = west[z + (159 - y) * 256];
        if (westVal === 255) return false;
        if (x < westVal * 256.0 / 255.0 - 1.004) return false;

        return true;
    }

    // Walk the implied voxels
    for (let z = 0; z < 256; z++) {
        for (let x = 0; x < 256; x++) {
            const topVal = top[x + z * 256];
            if (topVal === 255) continue; // Completely empty column

            // Maximum y to check: topVal / 255 * 160
            const yMax = Math.min(159, Math.floor((topVal / 255.0) * 160.0) + 1);

            for (let y = 0; y <= yMax; y++) {
                if (isSolid(x, y, z)) {
                    // Check 6 neighbors to generate faces

                    // Top neighbor (+Y)
                    if (!isSolid(x, y + 1, z)) {
                        addQuad(
                            x, y + 1, z + 1,
                            x + 1, y + 1, z + 1,
                            x + 1, y + 1, z,
                            x, y + 1, z,
                            0 // Top normal index
                        );
                    }

                    // Bottom neighbor (-Y)
                    if (!isSolid(x, y - 1, z)) {
                        addQuad(
                            x, y, z,
                            x + 1, y, z,
                            x + 1, y, z + 1,
                            x, y, z + 1,
                            1 // Bottom normal index
                        );
                    }

                    // South neighbor (+Z, normal points +Z)
                    if (!isSolid(x, y, z + 1)) {
                        addQuad(
                            x + 1, y + 1, z + 1,
                            x, y + 1, z + 1,
                            x, y, z + 1,
                            x + 1, y, z + 1,
                            2 // South normal index
                        );
                    }

                    // North neighbor (-Z, normal points -Z)
                    if (!isSolid(x, y, z - 1)) {
                        addQuad(
                            x, y + 1, z,
                            x + 1, y + 1, z,
                            x + 1, y, z,
                            x, y, z,
                            3 // North normal index
                        );
                    }

                    // East neighbor (+X, normal points +X)
                    if (!isSolid(x + 1, y, z)) {
                        addQuad(
                            x + 1, y + 1, z,
                            x + 1, y + 1, z + 1,
                            x + 1, y, z + 1,
                            x + 1, y, z,
                            4 // East normal index
                        );
                    }

                    // West neighbor (-X, normal points -X)
                    if (!isSolid(x - 1, y, z)) {
                        addQuad(
                            x, y + 1, z + 1,
                            x, y + 1, z,
                            x, y, z,
                            x, y, z + 1,
                            5 // West normal index
                        );
                    }
                }
            }
        }
    }

    const finalData = vertexBuffer.slice(0, vertexCount * 4);
    const geometry = new renderer.Geometry(gl);
    geometry.setAttributes({
        a_vertexData: {
            data: finalData,
            numComponents: 4,
            type: gl.UNSIGNED_SHORT,
            normalize: false
        }
    });
    geometry.verts = vertexCount;
    return geometry;
}
