import { mat4, vec3 } from 'gl-matrix';
import { Context, PerspectiveCamera, SceneGraph, Mesh, Chunk, Geometry } from './renderer';

export function renderBoundaries(
    gl: WebGL2RenderingContext,
    context: Context,
    camera: PerspectiveCamera,
    sceneGraph: SceneGraph,
    cube: Mesh,
    renderedChunks: Chunk[],
    cullResults: { impostors: any[] },
    projectionMatrix: mat4
) {
    if (!context.boundaryGeometry) {
        const FLD = [0, 0, 1], FLU = [0, 1, 1],
              FRD = [1, 0, 1], FRU = [1, 1, 1],
              BLD = [0, 0, 0], BLU = [0, 1, 0],
              BRD = [1, 0, 0], BRU = [1, 1, 0];

        const positions: number[] = [];

        function addQuad(pA: number[], pB: number[], pC: number[], pD: number[]) {
            positions.push(...pA);
            positions.push(...pB);
            positions.push(...pC);
            positions.push(...pC);
            positions.push(...pD);
            positions.push(...pA);
        }

        addQuad(FLU, BLU, BLD, FLD);  // L
        addQuad(FRD, BRD, BRU, FRU);  // R
        addQuad(FRU, FLU, FLD, FRD);  // F
        addQuad(BRD, BLD, BLU, BRU);  // B
        addQuad(FLU, FRU, BRU, BLU);  // U
        addQuad(BLD, BRD, FRD, FLD);  // D

        const bf32 = new Float32Array(positions);
        const geom = context.Geometry();
        geom.setAttributes({
            position: { data: bf32, numComponents: 3, offset: 0 }
        });
        geom.verts = 36;
        context.boundaryGeometry = geom;
    }

    if (!context.boundaryMaterial) {
        context.boundaryMaterial = context.Material(
            `#version 300 es
            in vec3 position;
            uniform vec3 scale;
            uniform vec3 offset;
            uniform mat4 modelViewMatrix;
            uniform mat4 projectionMatrix;
            out vec3 vLocalPos;
            void main(void) {
                vLocalPos = position;
                vec4 pos = projectionMatrix * modelViewMatrix * vec4(position * scale, 1.0);
                // Push boundary slightly closer to the camera to prevent Z-fighting with coplanar terrain faces.
                // In inverse-Z, larger Z is closer to the camera.
                pos.z += 0.0001 * pos.w;
                gl_Position = pos;
            }
            `,
            `#version 300 es
            precision mediump float;
            in vec3 vLocalPos;
            uniform vec3 uScale;
            uniform vec3 uColor;
            uniform float uThickness;
            out vec4 fragColor;
            void main(void) {
                // Screen-space derivative to make thickness adaptive so far away boundaries don't alias/disappear.
                vec3 dy = fwidth(vLocalPos);
                vec3 tWorld = vec3(uThickness) / uScale;
                vec3 tScreen = 1.5 * dy; // Ensure thickness is at least 1.5 pixels wide on screen
                vec3 t = max(tWorld, tScreen);

                vec3 dx = min(vLocalPos, vec3(1.0) - vLocalPos);
                bool borderX = dx.x < t.x;
                bool borderY = dx.y < t.y;
                bool borderZ = dx.z < t.z;
                if ((borderX && borderY) || (borderY && borderZ) || (borderZ && borderX)) {
                    float fogFactor = min(1.0, (1.0 / gl_FragCoord.w) / 3000.0);
                    vec3 fogColor = vec3(.722, .855, 1.0);
                    fragColor = vec4(mix(uColor, fogColor, fogFactor), 1.0);
                } else {
                    discard;
                }
            }
            `
        );
    }

    const mat = context.boundaryMaterial;
    const geo = context.boundaryGeometry;

    const prevDepthFunc = gl.getParameter(gl.DEPTH_FUNC);
    const prevDepthWrite = gl.getParameter(gl.DEPTH_WRITEMASK);
    const prevBlend = gl.isEnabled(gl.BLEND);

    gl.enable(gl.DEPTH_TEST);
    gl.depthFunc(gl.GEQUAL); // allow equal depth for boundaries overlay
    gl.depthMask(false);
    gl.disable(gl.BLEND);

    gl.useProgram(mat.program);
    if (mat.uniformSetters.projectionMatrix) {
        mat.uniformSetters.projectionMatrix(projectionMatrix);
    }
    if (mat.attribSetters.position && geo.attributes.position) {
        mat.attribSetters.position(geo.attributes.position);
    }

    // Gather all boundary items to render back-to-front
    interface BoundaryItem {
        color: vec3;
        scale: vec3;
        offset: vec3;
        distSq: number;
    }

    const items: BoundaryItem[] = [];
    const thickness = 2.0; // Thick line (2 blocks in world coords)

    // A. Regionlet (High-Res Chunk) boundaries
    const chunkColor = vec3.fromValues(0.0, 1.0, 0.8); // Vibrant cyan-green
    let chunkNum = 0;
    for (const chunk of renderedChunks) {
        chunkNum++;
        if (chunkNum >= 5 && chunk.occluded) {
            continue;
        }

        const minY = chunk.minY;
        const maxY = chunk.maxY;
        const sizeY = 1.0 + maxY - minY;
        const scale = vec3.fromValues(256.0, sizeY, 256.0);
        const offset = vec3.fromValues(chunk.position[0], minY, chunk.position[2]);

        const center = vec3.fromValues(offset[0] + 128.0, minY + sizeY * 0.5, offset[2] + 128.0);
        const distSq = vec3.sqrDist(camera.position, center);

        items.push({
            color: chunkColor,
            scale,
            offset,
            distSq
        });
    }

    // B. Impostor boundaries
    const impostorColor = vec3.fromValues(1.0, 0.0, 0.6); // Vibrant magenta
    const impostorScale = vec3.fromValues(512.0, 320.0, 512.0);
    for (const lod of cullResults.impostors) {
        if (!lod.loaded || !lod.textures) continue;

        const regionOffset = vec3.fromValues(lod.rx * 512.0, 0.0, lod.rz * 512.0);
        const center = vec3.fromValues(regionOffset[0] + 256.0, 160.0, regionOffset[2] + 256.0);
        const distSq = vec3.sqrDist(camera.position, center);

        items.push({
            color: impostorColor,
            scale: impostorScale,
            offset: regionOffset,
            distSq
        });
    }

    // Sort items back-to-front (farthest first)
    items.sort((a, b) => b.distSq - a.distSq);

    // Draw all items back-to-front
    for (const item of items) {
        const modelViewMatrix = mat4.translate(mat4.create(), camera.getView(), item.offset);

        if (mat.uniformSetters.modelViewMatrix) {
            mat.uniformSetters.modelViewMatrix(modelViewMatrix);
        }
        if (mat.uniformSetters.scale) {
            mat.uniformSetters.scale(item.scale);
        }
        if (mat.uniformSetters.offset) {
            mat.uniformSetters.offset(item.offset);
        }
        if (mat.uniformSetters.uScale) {
            mat.uniformSetters.uScale(item.scale);
        }
        if (mat.uniformSetters.uColor) {
            mat.uniformSetters.uColor(item.color);
        }
        if (mat.uniformSetters.uThickness) {
            mat.uniformSetters.uThickness(thickness);
        }

        gl.drawArrays(gl.TRIANGLES, 0, geo.verts);
    }

    // Restore WebGL state
    gl.depthFunc(prevDepthFunc);
    gl.depthMask(prevDepthWrite);
    if (prevBlend) {
        gl.enable(gl.BLEND);
    } else {
        gl.disable(gl.BLEND);
    }
}
