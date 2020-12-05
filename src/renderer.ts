import { glMatrix, mat4, quat, vec3, vec4 } from 'gl-matrix';

import * as webgl_utils from "./webgl_utils";

export class Material {
    gl: WebGLRenderingContext;
    program: WebGLProgram;
    uniformSetters: { [name: string]: any };
    attribSetters: { [name: string]: any };
    bufferInfo: webgl_utils.BufferInfo

    constructor(gl: WebGL2RenderingContext, vertexShader: string, fragmentShader: string) {
        this.gl = gl;
        this.program = webgl_utils.createProgramFromSources(gl, [vertexShader, fragmentShader]);
        this.uniformSetters = webgl_utils.createUniformSetters(gl, this.program);
        this.attribSetters = webgl_utils.createAttributeSetters(gl, this.program);
    }
}

export class Geometry {
    attributes: { [name: string]: webgl_utils.AttribInfo }

    constructor(public gl: WebGLRenderingContext, attributes?: { [name: string]: webgl_utils.AttribInfo }) {
        this.attributes = attributes;
    }

    clone() {
        return new Geometry(this.gl, { ...this.attributes })
    }

    setAttributes(arrays: { [name: string]: any }) {
        this.attributes = webgl_utils.createAttribsFromArrays(this.gl, arrays);
    }

    addAttribute(name: string, array: any) {
        Object.assign(this.attributes, webgl_utils.createAttribsFromArrays(this.gl, { [name]: array }));
    }

    updateAttribute(name: string, array: any, offset?: number) {
        if (!(name in this.attributes)) {
            throw new Error("unknown attribute: " + name);
        }
        this.gl.bindBuffer(this.gl.ARRAY_BUFFER, this.attributes[name].buffer);
        this.gl.bufferSubData(this.gl.ARRAY_BUFFER, offset || 0, array);
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

export class Context {
    canvas: HTMLCanvasElement
    gl: WebGL2RenderingContext
    clearColor: vec4

    constructor(canvas: HTMLCanvasElement) {
        this.canvas = canvas;
        this.gl = this.canvas.getContext('webgl2');
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

    loadTexture(path: string): WebGLTexture {
        function isPowerOf2(value: number) {
            return (value & (value - 1)) === 0;
        }

        const gl = this.gl;

        // Create a texture.
        var texture = gl.createTexture();
        gl.bindTexture(gl.TEXTURE_2D, texture);
        // Fill the texture with a 1x1 grey pixel.
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE,
            new Uint8Array([120, 120, 120, 255]));

        // Asynchronously load an image
        var image = new Image();
        image.src = path;
        image.addEventListener('load', function () {
            // Now that the image has loaded make copy it to the texture.
            gl.bindTexture(gl.TEXTURE_2D, texture);
            gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, image);

            // Check if the image is a power of 2 in both dimensions.
            if (isPowerOf2(image.width) && isPowerOf2(image.height)) {
                // Yes, it's a power of 2. Generate mips.
                gl.generateMipmap(gl.TEXTURE_2D);
            } else {
                // No, it's not a power of 2. Turn of mips and set wrapping to clamp to edge
                gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
                gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
                gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
            }

            gl.texParameterf(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
            gl.texParameterf(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);

            var ext = webgl_utils.getExtensionWithKnownPrefixes(gl, "texture_filter_anisotropic");
            if (ext) {
                gl.texParameterf(gl.TEXTURE_2D, ext.TEXTURE_MAX_ANISOTROPY_EXT, 4);
            }

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
    }

    update() {
        mat4.perspective(this.proj, glMatrix.toRadian(this.fov), this.aspect, this.near, this.far);
        mat4.lookAt(this.view, this.position, this.target, vec3.fromValues(0, 1, 0));
        // this.matrix.compose(this.position, this.quaternion, this.scale);
    }

    getProjection(): mat4 {
        return this.proj;
    }

    getView(): mat4 {
        return mat4.mul(mat4.create(), this.view, this.matrix);
    }
}

export function render(context: Context, camera: PerspectiveCamera, scene: Set<InstancedMesh>, tex: WebGLTexture) {
    const gl = context.gl;

    if (!(gl.canvas instanceof HTMLCanvasElement))
        return;

    gl.clearColor(context.clearColor[0], context.clearColor[1],
        context.clearColor[2], context.clearColor[3]);

    gl.disable(gl.CULL_FACE);
    gl.enable(gl.DEPTH_TEST);

    // Tell WebGL how to convert from clip space to pixels
    gl.viewport(0, 0, gl.canvas.width, gl.canvas.height);

    // Clear the canvas AND the depth buffer.
    gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);

    // Enable alpha blending
    // TODO: disable for purely solid passes
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA);

    // Compute the projection matrix
    var projectionMatrix = camera.getProjection();

    // TODO:
    // * sort meshes
    // * frustum cull meshes
    let init = 1;
    var activeProgram: WebGLProgram
    for (const mesh of scene) {
        let mat = mesh.material;

        if (mat.program != activeProgram) {
            activeProgram = mat.program;
            gl.useProgram(mat.program);
            mat.uniformSetters.projectionMatrix(projectionMatrix);
            if (mat.uniformSetters.cameraPosition)
                mat.uniformSetters.cameraPosition(camera.position);
            mat.uniformSetters.atlas(tex);
            for (const [key, value] of Object.entries(mesh.geometry.attributes)) {
                mat.attribSetters[key](value);
            }
        } else {
            mat.attribSetters.attr(mesh.geometry.attributes.attr);
        }
        if (mat.uniformSetters.offset)
            mat.uniformSetters.offset(mesh.position);

        // TODO: modelViewMatrix & projectionMatrix?
        var matrix = mat4.translate(mat4.create(), camera.getView(), mesh.position);
        if (mat.uniformSetters.modelViewMatrix)
            mat.uniformSetters.modelViewMatrix(matrix);

        gl.drawArraysInstanced(
            gl.TRIANGLES,
            0,           // offset
            3 * 6,       // num vertices per instance
            mesh.count,  // num instances
        );
    }
}
