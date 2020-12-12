import { glMatrix, mat4, quat, vec3, vec4 } from 'gl-matrix';

import * as webgl_utils from "./webgl_utils";
import { generateMipmaps } from "./downscale";

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
    layerLengths: Uint32Array
    verts: number

    constructor(public gl: WebGLRenderingContext, attributes?: { [name: string]: webgl_utils.AttribInfo }) {
        this.attributes = attributes;
        this.verts = 3;
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
    ) {}
}

export class Chunk {
    position: vec3
    minY: number
    maxY: number
    layers: { [name: string]: webgl_utils.AttribInfo }
    occluded: boolean
    query: WebGLQuery
    queryInProgress: boolean

    constructor(public gl: WebGLRenderingContext) {
        this.position = vec3.create();
        this.query = null;
        this.queryInProgress = false;
        this.occluded = false;
        this.minY = 0
        this.maxY = 255
    }

    setLayers(arrays: { [name: string]: any }) {
        this.layers = webgl_utils.createAttribsFromArrays(this.gl, arrays);
    }

    addAttribute(name: string, array: any) {
        Object.assign(this.layers, webgl_utils.createAttribsFromArrays(this.gl, { [name]: array }));
    }

    updateAttribute(name: string, array: any, offset?: number) {
        if (!(name in this.layers)) {
            throw new Error("unknown attribute: " + name);
        }
        this.gl.bindBuffer(this.gl.ARRAY_BUFFER, this.layers[name].buffer);
        this.gl.bufferSubData(this.gl.ARRAY_BUFFER, offset || 0, array);
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

    Chunk(): Chunk {
        return new Chunk(this.gl);
    }

    loadTexture(path: string, done?: ()=>void): WebGLTexture {
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
            gl.texParameterf(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
            gl.texParameterf(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);

            gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
            gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);

            // Check if the image is a power of 2 in both dimensions.
            if (isPowerOf2(image.width) && isPowerOf2(image.height)) {
                // Yes, it's a power of 2. Generate mips.
                // try to prevent texture bleeding by limiting mipmaps to up to 16x reduction.
                // https://gamedev.stackexchange.com/a/50777
                generateMipmaps(gl, image, 4);
                // gl.generateMipmap(gl.TEXTURE_2D);
                gl.texParameterf(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST_MIPMAP_LINEAR);
                gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAX_LEVEL, 4);
            } else {
                gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
            }

            var ext = webgl_utils.getExtensionWithKnownPrefixes(gl, "texture_filter_anisotropic");
            if (ext) {
                gl.texParameterf(gl.TEXTURE_2D, ext.TEXTURE_MAX_ANISOTROPY_EXT, 4);
            }

            done();
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
        // TODO: add ortho mode with correct zooming, shaders (flipping is broken), etc
        // const orthoscale = 128;
        // mat4.ortho(this.proj, -orthoscale * this.aspect, orthoscale * this.aspect, -orthoscale, orthoscale, -5000, 5000)
        mat4.lookAt(this.view, this.position, this.target, vec3.fromValues(0, 1, 0));
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

class Frustum {
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
        const hFovRad = 2 * Math.atan(Math.tan(vFovRad/2) * camera.aspect);
        this.coneAngle = 2 * Math.atan(Math.sqrt(hFovRad*hFovRad + vFovRad * vFovRad));
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
}

export function render(context: Context, camera: PerspectiveCamera, scene: Set<Chunk>, layers: InstancedLayer[], cube: Mesh) {
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

    const frustum = new Frustum(camera);

    let culledChunks: Chunk[] = [];

    for (const chunk of scene) {
        if (frustum.intersects(chunk)) {
            culledChunks.push(chunk);
        }
    }

    culledChunks.sort((a, b) => {
        const apos = vec3.fromValues(128, 128, 128);
        const bpos = vec3.fromValues(128, 128, 128);
        vec3.add(apos, apos, a.position);
        vec3.add(bpos, bpos, b.position);
        return vec3.sqrDist(camera.position, apos) - vec3.sqrDist(camera.position, bpos);
    })


    culledChunks = culledChunks.slice(0, 32);

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

    for (const layer of layers) {
        if (!layer) continue;
        let mat = layer.material;

        bind(mat, layer.geometry)

        mat.uniformSetters.atlas(layer.texture);

        let chunkNum = 0;
        for (const chunk of culledChunks) {
            chunkNum++;
            const chunkLayer = chunk.layers[layer.name];
            if (!chunkLayer || chunkLayer.size == 0) {
                continue;
            }

            if (layer.name == 'CUBE' && chunkNum >= 5) {
                // inspired by https://tsherif.github.io/webgl2examples/occlusion.html
                if (chunk.query === null) {
                    chunk.query = gl.createQuery();
                }
                if (chunk.queryInProgress && gl.getQueryParameter(chunk.query, gl.QUERY_RESULT_AVAILABLE)) {
                    chunk.occluded = !gl.getQueryParameter(chunk.query, gl.QUERY_RESULT);
                    chunk.queryInProgress = false;
                }
                if (!chunk.queryInProgress) {
                    let mat = cube.material;
                    bind(cube.material, cube.geometry);
                    mat.uniformSetters.modelViewMatrix(camera.getView());
                    mat.uniformSetters.scale(vec3.fromValues(256, 1 + chunk.maxY - chunk.minY, 256));

                    gl.enable(gl.CULL_FACE);
                    gl.colorMask(false, false, false, false);
                    gl.depthMask(false);

                    gl.beginQuery(gl.ANY_SAMPLES_PASSED_CONSERVATIVE, chunk.query);
                    const offset = vec3.fromValues(0, chunk.minY, 0);
                    vec3.add(offset, offset, chunk.position);
                    mat.uniformSetters.offset(offset);
                    gl.drawArrays(gl.TRIANGLES, 0, 12 * 3);
                    gl.endQuery(gl.ANY_SAMPLES_PASSED_CONSERVATIVE);

                    gl.colorMask(true, true, true, true);
                    gl.depthMask(true);
                    gl.disable(gl.CULL_FACE);

                    chunk.queryInProgress = true;

                    bind(layer.material, layer.geometry);
                    layer.material.uniformSetters.atlas(layer.texture);
                }
            }

            if (chunkNum >= 5 && chunk.occluded) {
                continue;
            }

            mat.attribSetters.attr(chunkLayer);
            if (mat.uniformSetters.offset)
                mat.uniformSetters.offset(chunk.position);

            // TODO: modelViewMatrix & projectionMatrix?
            var matrix = mat4.translate(mat4.create(), camera.getView(), chunk.position);
            if (mat.uniformSetters.modelViewMatrix)
                mat.uniformSetters.modelViewMatrix(matrix);

            gl.drawArraysInstanced(
                gl.TRIANGLES,
                0,           // offset
                layer.geometry.verts,       // num vertices per instance
                chunkLayer.size,  // num instances
            );
        }
    }
}
