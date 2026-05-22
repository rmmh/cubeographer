import { mat4, vec3, vec4 } from 'gl-matrix';

function readDepthAtPixel(context: any, ndcX: number, ndcY: number): number {
    const gl = context.gl as WebGL2RenderingContext;
    if (!context.fboDepth) return 1.0;

    // Create resources for depth reading on-demand (only once)
    if (!context._depthReadFBO) {
        context._depthReadFBO = gl.createFramebuffer();
        context._depthReadTex = gl.createTexture();
        gl.bindTexture(gl.TEXTURE_2D, context._depthReadTex);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
        gl.bindFramebuffer(gl.FRAMEBUFFER, context._depthReadFBO);
        gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, context._depthReadTex, 0);

        // Create shader program
        const vs = `#version 300 es
            void main() {
                gl_Position = vec4(
                    gl_VertexID == 0 ? -1.0 : gl_VertexID == 2 ? 3.0 : -1.0,
                    gl_VertexID == 2 ? -1.0 : gl_VertexID == 1 ? 3.0 : -1.0,
                    0.0, 1.0
                );
            }
        `;
        const fs = `#version 300 es
            precision highp float;
            uniform sampler2D uDepthTex;
            uniform vec2 uTexCoord;
            out vec4 fragColor;
            
            void main() {
                float depth = texture(uDepthTex, uTexCoord).r;
                uint u = floatBitsToUint(depth);
                fragColor = vec4(
                    float((u >> 24u) & 0xFFu) / 255.0,
                    float((u >> 16u) & 0xFFu) / 255.0,
                    float((u >> 8u) & 0xFFu) / 255.0,
                    float(u & 0xFFu) / 255.0
                );
            }
        `;

        const vsShader = gl.createShader(gl.VERTEX_SHADER)!;
        gl.shaderSource(vsShader, vs);
        gl.compileShader(vsShader);

        const fsShader = gl.createShader(gl.FRAGMENT_SHADER)!;
        gl.shaderSource(fsShader, fs);
        gl.compileShader(fsShader);

        const program = gl.createProgram()!;
        gl.attachShader(program, vsShader);
        gl.attachShader(program, fsShader);
        gl.linkProgram(program);

        context._depthReadProgram = program;
        context._depthReadUniformDepthTex = gl.getUniformLocation(program, "uDepthTex");
        context._depthReadUniformTexCoord = gl.getUniformLocation(program, "uTexCoord");
    }

    // Now, perform the 1x1 render pass to read depth
    const prevFBO = gl.getParameter(gl.FRAMEBUFFER_BINDING);
    const prevViewport = gl.getParameter(gl.VIEWPORT);

    gl.bindFramebuffer(gl.FRAMEBUFFER, context._depthReadFBO);
    gl.viewport(0, 0, 1, 1);

    gl.useProgram(context._depthReadProgram);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, context.fboDepth);
    gl.uniform1i(context._depthReadUniformDepthTex, 0);

    // ndcX and ndcY are in [-1, 1], map to [0, 1]
    const u = ndcX * 0.5 + 0.5;
    const v = ndcY * 0.5 + 0.5;
    gl.uniform2f(context._depthReadUniformTexCoord, u, v);

    // Disable state that might affect drawing a simple quad
    gl.disable(gl.DEPTH_TEST);
    gl.disable(gl.BLEND);
    gl.disable(gl.CULL_FACE);

    // Draw full-screen triangle
    gl.drawArrays(gl.TRIANGLES, 0, 3);

    // Read the encoded pixel
    const pixel = new Uint8Array(4);
    gl.readPixels(0, 0, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, pixel);

    // Restore WebGL state
    gl.bindFramebuffer(gl.FRAMEBUFFER, prevFBO);
    gl.viewport(prevViewport[0], prevViewport[1], prevViewport[2], prevViewport[3]);
    gl.enable(gl.DEPTH_TEST);
    gl.enable(gl.BLEND);

    // Decode depth value
    const view = new DataView(new ArrayBuffer(4));
    view.setUint8(0, pixel[0]);
    view.setUint8(1, pixel[1]);
    view.setUint8(2, pixel[2]);
    view.setUint8(3, pixel[3]);
    const depth = view.getFloat32(0, false); // Big-endian

    return depth;
}

export function createOrbitTargetFinder(
    context: any,
    camera: any,
    sceneGraph: any,
    renderSceneToFBO: () => void
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

        // Call the on-demand FBO render pass to fill fboDepth
        renderSceneToFBO();

        // Read the depth value at this pixel from our FBO depth texture
        const depth = readDepthAtPixel(context, ndcX, ndcY);

        // If it's very close to 0.0 (the cleared value), we clicked the sky/background
        if (depth <= 0.000001) {
            return null;
        }

        // Map depth from [0, 1] to NDC range [-1, 1]
        const clipZ = depth * 2 - 1;

        // Unproject NDC coordinate back to world space
        const worldPos4 = vec4.transformMat4(vec4.create(), vec4.fromValues(ndcX, ndcY, clipZ, 1), invPVM);
        if (Math.abs(worldPos4[3]) < 1e-8) return null;

        const worldPos = vec3.fromValues(
            worldPos4[0] / worldPos4[3],
            worldPos4[1] / worldPos4[3],
            worldPos4[2] / worldPos4[3]
        );

        return worldPos;
    };
}
