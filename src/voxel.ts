import { mat4, vec3, vec4 } from 'gl-matrix';

export class VoxelBitset {
    private blocks: { [blockIdx: number]: Uint32Array } = {};

    add(x: number, y: number, z: number) {
        const bx = x >> 3;
        const by = y >> 3;
        const bz = z >> 3;
        const blockIdx = (bx << 10) | (by << 5) | bz;

        let block = this.blocks[blockIdx];
        if (!block) {
            block = new Uint32Array(16);
            this.blocks[blockIdx] = block;
        }

        const lx = x & 7;
        const ly = y & 7;
        const lz = z & 7;
        const bitIdx = (lx << 6) | (ly << 3) | lz;
        const wordIdx = bitIdx >> 5;
        const bitOffset = bitIdx & 31;
        block[wordIdx] |= (1 << bitOffset);
    }

    has(x: number, y: number, z: number): boolean {
        const bx = x >> 3;
        const by = y >> 3;
        const bz = z >> 3;
        const blockIdx = (bx << 10) | (by << 5) | bz;

        const block = this.blocks[blockIdx];
        if (!block) return false;

        const lx = x & 7;
        const ly = y & 7;
        const lz = z & 7;
        const bitIdx = (lx << 6) | (ly << 3) | lz;
        const wordIdx = bitIdx >> 5;
        const bitOffset = bitIdx & 31;
        return (block[wordIdx] & (1 << bitOffset)) !== 0;
    }

    forEach(callback: (x: number, y: number, z: number) => void) {
        for (const keyStr of Object.keys(this.blocks)) {
            const blockIdx = Number(keyStr);
            const block = this.blocks[blockIdx];
            const bx = (blockIdx >> 10) & 31;
            const by = (blockIdx >> 5) & 31;
            const bz = blockIdx & 31;

            const baseX = bx << 3;
            const baseY = by << 3;
            const baseZ = bz << 3;

            for (let wordIdx = 0; wordIdx < 16; wordIdx++) {
                const word = block[wordIdx];
                if (word === 0) continue;
                for (let bitOffset = 0; bitOffset < 32; bitOffset++) {
                    if ((word & (1 << bitOffset)) !== 0) {
                        const bitIdx = (wordIdx << 5) | bitOffset;
                        const lx = (bitIdx >> 6) & 7;
                        const ly = (bitIdx >> 3) & 7;
                        const lz = bitIdx & 7;
                        callback(baseX + lx, baseY + ly, baseZ + lz);
                    }
                }
            }
        }
    }
}

function intersectRayAABB(
    rayOrig: vec3,
    rayDir: vec3,
    boxMin: vec3,
    boxMax: vec3
): number | null {
    let tmin = -Infinity;
    let tmax = Infinity;

    for (let i = 0; i < 3; i++) {
        if (Math.abs(rayDir[i]) < 1e-8) {
            if (rayOrig[i] < boxMin[i] || rayOrig[i] > boxMax[i]) {
                return null;
            }
        } else {
            let invD = 1.0 / rayDir[i];
            let t1 = (boxMin[i] - rayOrig[i]) * invD;
            let t2 = (boxMax[i] - rayOrig[i]) * invD;

            if (t1 > t2) {
                let temp = t1;
                t1 = t2;
                t2 = temp;
            }

            tmin = Math.max(tmin, t1);
            tmax = Math.min(tmax, t2);

            if (tmin > tmax) {
                return null;
            }
        }
    }

    if (tmax < 0) {
        return null;
    }

    return tmin < 0 ? 0 : tmin;
}

export function createOrbitTargetFinder(
    context: any,
    camera: any,
    scene: any[]
): (clientX: number, clientY: number) => vec3 | null {
    return function getOrbitTarget(clientX: number, clientY: number): vec3 | null {
        const rect = context.canvas.getBoundingClientRect();
        const x = clientX - rect.left;
        const y = clientY - rect.top;

        // Convert to NDC (Normalized Device Coordinates)
        const ndcX = (x / context.canvas.width) * 2 - 1;
        const ndcY = 1 - (y / context.canvas.height) * 2;

        // Get view and projection matrices
        const pm = camera.getProjection();
        const vm = camera.getView();
        const pvm = mat4.multiply(mat4.create(), pm, vm);

        // Invert the view-projection matrix
        const invPVM = mat4.invert(mat4.create(), pvm);
        if (!invPVM) return null;

        // Get near and far world coordinates
        const nearWorld4 = vec4.transformMat4(vec4.create(), vec4.fromValues(ndcX, ndcY, -1, 1), invPVM);
        const farWorld4 = vec4.transformMat4(vec4.create(), vec4.fromValues(ndcX, ndcY, 1, 1), invPVM);

        const nearWorld = vec3.fromValues(
            nearWorld4[0] / nearWorld4[3],
            nearWorld4[1] / nearWorld4[3],
            nearWorld4[2] / nearWorld4[3]
        );
        const farWorld = vec3.fromValues(
            farWorld4[0] / farWorld4[3],
            farWorld4[1] / farWorld4[3],
            farWorld4[2] / farWorld4[3]
        );

        // Ray origin and direction
        const rayOrig = vec3.copy(vec3.create(), camera.position);
        const rayDir = vec3.sub(vec3.create(), farWorld, nearWorld);
        vec3.normalize(rayDir, rayDir);

        let closestVoxelPos: vec3 | null = null;
        let minT = Infinity;

        // Check for direct ray-voxel intersections in chunks that intersect the ray
        for (const chunk of scene) {
            if (!chunk.voxelBitset) continue;

            const chunkMin = vec3.fromValues(chunk.position[0], chunk.minY, chunk.position[2]);
            const chunkMax = vec3.fromValues(chunk.position[0] + 256, chunk.maxY + 1, chunk.position[2] + 256);

            // Check if the ray intersects this chunk's bounding box
            if (intersectRayAABB(rayOrig, rayDir, chunkMin, chunkMax) !== null) {
                chunk.voxelBitset.forEach((vxLocal: number, vyLocal: number, vzLocal: number) => {
                    const vx = chunk.position[0] + vxLocal;
                    const vy = chunk.position[1] + vyLocal;
                    const vz = chunk.position[2] + vzLocal;

                    const boxMin = vec3.fromValues(vx, vy, vz);
                    const boxMax = vec3.fromValues(vx + 1, vy + 1, vz + 1);

                    const t = intersectRayAABB(rayOrig, rayDir, boxMin, boxMax);
                    if (t !== null && t < minT) {
                        minT = t;
                        closestVoxelPos = vec3.fromValues(vx + 0.5, vy + 0.5, vz + 0.5);
                    }
                });
            }
        }

        return closestVoxelPos;
    };
}
