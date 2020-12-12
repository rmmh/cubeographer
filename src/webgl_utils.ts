/*
 * Copyright 2012, Gregg Tavares.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Gregg Tavares. nor the names of his
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */


/**
 * Wrapped logging function.
 * @param {string} msg The message to log.
 */
function error(msg: string) {
    if (console) {
        if (console.error) {
            console.error(msg);
        } else if (console.log) {
            console.log(msg);
        }
    }
}


/**
 * Error Callback
 * @callback ErrorCallback
 * @param {string} msg error message.
 * @memberOf module:webgl-utils
 */


/**
 * Loads a shader.
 * @param gl The WebGLRenderingContext to use.
 * @param shaderSource The shader source.
 * @param shaderType The type of shader.
 * @param {module:webgl-utils.ErrorCallback} opt_errorCallback callback for errors.
 * @return {WebGLShader} The created shader.
 */
function loadShader(gl: WebGLRenderingContext, shaderSource: string, shaderType: number, errorCallback?: (msg: string)=>void) {
    const errFn = errorCallback || error;
    // Create the shader object
    const shader = gl.createShader(shaderType);

    // Load the shader source
    gl.shaderSource(shader, shaderSource);

    // Compile the shader
    gl.compileShader(shader);

    // Check the compile status
    const compiled = gl.getShaderParameter(shader, gl.COMPILE_STATUS);
    if (!compiled) {
        // Something went wrong during compilation; get the error
        const lastError = gl.getShaderInfoLog(shader);
        errFn('*** Error compiling shader \'' + shader + '\':' + lastError + `\n` + shaderSource.split('\n').map((l, i) => `${i + 1}: ${l}`).join('\n'));
        gl.deleteShader(shader);
        return null;
    }

    return shader;
}

/**
 * Creates a program, attaches shaders, binds attrib locations, links the
 * program and calls useProgram.
 * @param shaders The shaders to attach
 * @param [opt_attribs] An array of attribs names. Locations will be assigned by index if not passed in
 * @param [opt_locations] The locations for the. A parallel array to opt_attribs letting you assign locations.
 * @param opt_errorCallback callback for errors. By default it just prints an error to the console
 *        on error. If you want something else pass an callback. It's passed an error message.
 * @memberOf module:webgl-utils
 */
function createProgram(
    gl: WebGLRenderingContext, shaders: WebGLShader[], opt_attribs: string[], opt_locations?: number[], opt_errorCallback?: (msg: string)=>void) {
    const errFn = opt_errorCallback || error;
    const program = gl.createProgram();
    shaders.forEach(function (shader) {
        gl.attachShader(program, shader);
    });
    if (opt_attribs) {
        opt_attribs.forEach(function (attrib, ndx) {
            gl.bindAttribLocation(
                program,
                opt_locations ? opt_locations[ndx] : ndx,
                attrib);
        });
    }
    gl.linkProgram(program);

    // Check the link status
    const linked = gl.getProgramParameter(program, gl.LINK_STATUS);
    if (!linked) {
        // something went wrong with the link
        const lastError = gl.getProgramInfoLog(program);
        errFn('Error in program linking:' + lastError);

        gl.deleteProgram(program);
        return null;
    }
    return program;
}

const defaultShaderType: ('VERTEX_SHADER'|'FRAGMENT_SHADER')[] = [
    'VERTEX_SHADER',
    'FRAGMENT_SHADER',
];

/**
 * Creates a program from 2 sources.
 *
 * @param gl The WebGLRenderingContext to use.
 * @param shaderSourcess Array of sources for the
 *        shaders. The first is assumed to be the vertex shader,
 *        the second the fragment shader.
 * @param [opt_attribs] An array of attribs names. Locations will be assigned by index if not passed in
 * @param [opt_locations] The locations for the. A parallel array to opt_attribs letting you assign locations.
 * @param opt_errorCallback callback for errors. By default it just prints an error to the console
 *        on error. If you want something else pass an callback. It's passed an error message.
 * @return {WebGLProgram} The created program.
 * @memberOf module:webgl-utils
 */
export function createProgramFromSources(
    gl: WebGLRenderingContext, shaderSources: string[], opt_attribs?: string[], opt_locations?: number[], opt_errorCallback?: (msg: string)=>void) {
    const shaders = [];
    for (let ii = 0; ii < shaderSources.length; ++ii) {
        shaders.push(loadShader(
            gl, shaderSources[ii], gl[defaultShaderType[ii]], opt_errorCallback));
    }
    return createProgram(gl, shaders, opt_attribs, opt_locations, opt_errorCallback);
}

/**
 * Returns the corresponding bind point for a given sampler type
 */
function getBindPointForSamplerType(gl: WebGLRenderingContext, type: number) {
    if (type === gl.SAMPLER_2D) return gl.TEXTURE_2D;        // eslint-disable-line
    if (type === gl.SAMPLER_CUBE) return gl.TEXTURE_CUBE_MAP;  // eslint-disable-line
    return undefined;
}

/**
 * @typedef {Object.<string, function>} Setters
 */

/**
 * Creates setter functions for all uniforms of a shader
 * program.
 *
 * @see {@link module:webgl-utils.setUniforms}
 *
 * @param {WebGLProgram} program the program to create setters for.
 * @returns {Object.<string, function>} an object with a setter by name for each uniform
 * @memberOf module:webgl-utils
 */
export function createUniformSetters(gl: WebGLRenderingContext, program: WebGLProgram) {
    let textureUnit = 0;

    /**
     * Creates a setter for a uniform of the given program with it's
     * location embedded in the setter.
     * @param program
     * @param uniformInfo
     * @returns {function} the created setter.
     */
    function createUniformSetter(program: WebGLProgram, uniformInfo: WebGLActiveInfo):
        ((f: Iterable<Number>)=>void)|((f: number)=>void)|((t: Array<WebGLTexture>)=>void) {
        const location = gl.getUniformLocation(program, uniformInfo.name);
        const type = uniformInfo.type;
        // Check if this uniform is an array
        const isArray = (uniformInfo.size > 1 && uniformInfo.name.substr(-3) === '[0]');
        if (type === gl.FLOAT && isArray) {
            return function (v: Float32List) {
                gl.uniform1fv(location, v);
            };
        }
        if (type === gl.FLOAT) {
            return function (v: number) {
                gl.uniform1f(location, v);
            };
        }
        if (type === gl.FLOAT_VEC2) {
            return function (v: Float32List) {
                gl.uniform2fv(location, v);
            };
        }
        if (type === gl.FLOAT_VEC3) {
            return function (v: Float32List) {
                gl.uniform3fv(location, v);
            };
        }
        if (type === gl.FLOAT_VEC4) {
            return function (v: Float32List) {
                gl.uniform4fv(location, v);
            };
        }
        if (type === gl.INT && isArray) {
            return function (v: Int32List) {
                gl.uniform1iv(location, v);
            };
        }
        if (type === gl.INT) {
            return function (v: number) {
                gl.uniform1i(location, v);
            };
        }
        if (type === gl.INT_VEC2) {
            return function (v: Int32List) {
                gl.uniform2iv(location, v);
            };
        }
        if (type === gl.INT_VEC3) {
            return function (v: Int32List) {
                gl.uniform3iv(location, v);
            };
        }
        if (type === gl.INT_VEC4) {
            return function (v: Int32List) {
                gl.uniform4iv(location, v);
            };
        }
        if (type === gl.BOOL) {
            return function (v: Int32List) {
                gl.uniform1iv(location, v);
            };
        }
        if (type === gl.BOOL_VEC2) {
            return function (v: Int32List) {
                gl.uniform2iv(location, v);
            };
        }
        if (type === gl.BOOL_VEC3) {
            return function (v: Int32List) {
                gl.uniform3iv(location, v);
            };
        }
        if (type === gl.BOOL_VEC4) {
            return function (v: Int32List) {
                gl.uniform4iv(location, v);
            };
        }
        if (type === gl.FLOAT_MAT2) {
            return function (v: Float32List) {
                gl.uniformMatrix2fv(location, false, v);
            };
        }
        if (type === gl.FLOAT_MAT3) {
            return function (v: Float32List) {
                gl.uniformMatrix3fv(location, false, v);
            };
        }
        if (type === gl.FLOAT_MAT4) {
            return function (v: Float32List) {
                gl.uniformMatrix4fv(location, false, v);
            };
        }
        if ((type === gl.SAMPLER_2D || type === gl.SAMPLER_CUBE) && isArray) {
            const units = [];
            for (let ii = 0; ii < uniformInfo.size; ++ii) {
                units.push(textureUnit++);
            }
            return function (bindPoint, units) {
                return function (textures: Array<WebGLTexture>) {
                    gl.uniform1iv(location, units);
                    textures.forEach(function (texture, index) {
                        gl.activeTexture(gl.TEXTURE0 + units[index]);
                        gl.bindTexture(bindPoint, texture);
                    });
                };
            }(getBindPointForSamplerType(gl, type), units);
        }
        if (type === gl.SAMPLER_2D || type === gl.SAMPLER_CUBE) {
            return function (bindPoint, unit) {
                return function (texture: WebGLTexture) {
                    gl.uniform1i(location, unit);
                    gl.activeTexture(gl.TEXTURE0 + unit);
                    gl.bindTexture(bindPoint, texture);
                };
            }(getBindPointForSamplerType(gl, type), textureUnit++);
        }
        throw ('unknown type: 0x' + type.toString(16)); // we should never get here.
    }

    const uniformSetters: {[name: string]: any} = {};
    const numUniforms = gl.getProgramParameter(program, gl.ACTIVE_UNIFORMS);

    for (let ii = 0; ii < numUniforms; ++ii) {
        const uniformInfo = gl.getActiveUniform(program, ii);
        if (!uniformInfo) {
            break;
        }
        let name = uniformInfo.name;
        // remove the array suffix.
        if (name.substr(-3) === '[0]') {
            name = name.substr(0, name.length - 3);
        }
        const setter = createUniformSetter(program, uniformInfo);
        uniformSetters[name] = setter;
    }
    return uniformSetters;
}

/**
 * Set uniforms and binds related textures.
 *
 * Example:
 *
 *     let programInfo = createProgramInfo(
 *         gl, ["some-vs", "some-fs"]);
 *
 *     let tex1 = gl.createTexture();
 *     let tex2 = gl.createTexture();
 *
 *     ... assume we setup the textures with data ...
 *
 *     let uniforms = {
 *       u_someSampler: tex1,
 *       u_someOtherSampler: tex2,
 *       u_someColor: [1,0,0,1],
 *       u_somePosition: [0,1,1],
 *       u_someMatrix: [
 *         1,0,0,0,
 *         0,1,0,0,
 *         0,0,1,0,
 *         0,0,0,0,
 *       ],
 *     };
 *
 *     gl.useProgram(program);
 *
 * This will automatically bind the textures AND set the
 * uniforms.
 *
 *     setUniforms(programInfo.uniformSetters, uniforms);
 *
 * For the example above it is equivalent to
 *
 *     let texUnit = 0;
 *     gl.activeTexture(gl.TEXTURE0 + texUnit);
 *     gl.bindTexture(gl.TEXTURE_2D, tex1);
 *     gl.uniform1i(u_someSamplerLocation, texUnit++);
 *     gl.activeTexture(gl.TEXTURE0 + texUnit);
 *     gl.bindTexture(gl.TEXTURE_2D, tex2);
 *     gl.uniform1i(u_someSamplerLocation, texUnit++);
 *     gl.uniform4fv(u_someColorLocation, [1, 0, 0, 1]);
 *     gl.uniform3fv(u_somePositionLocation, [0, 1, 1]);
 *     gl.uniformMatrix4fv(u_someMatrix, false, [
 *         1,0,0,0,
 *         0,1,0,0,
 *         0,0,1,0,
 *         0,0,0,0,
 *       ]);
 *
 * Note it is perfectly reasonable to call `setUniforms` multiple times. For example
 *
 *     let uniforms = {
 *       u_someSampler: tex1,
 *       u_someOtherSampler: tex2,
 *     };
 *
 *     let moreUniforms {
 *       u_someColor: [1,0,0,1],
 *       u_somePosition: [0,1,1],
 *       u_someMatrix: [
 *         1,0,0,0,
 *         0,1,0,0,
 *         0,0,1,0,
 *         0,0,0,0,
 *       ],
 *     };
 *
 *     setUniforms(programInfo.uniformSetters, uniforms);
 *     setUniforms(programInfo.uniformSetters, moreUniforms);
 *
 * @param {Object.<string, function>|module:webgl-utils.ProgramInfo} setters the setters returned from
 *        `createUniformSetters` or a ProgramInfo from {@link module:webgl-utils.createProgramInfo}.
 * @param {Object.<string, value>} an object with values for the
 *        uniforms.
 * @memberOf module:webgl-utils
 */
function setUniforms(setters: any, ...values: any[]) {
    for (const uniforms of values) {
        Object.keys(uniforms).forEach(function (name) {
            const setter = setters[name];
            if (setter) {
                setter(uniforms[name]);
            }
        });
    }
}

/**
 * Creates setter functions for all attributes of a shader
 * program. You can pass this to {@link module:webgl-utils.setBuffersAndAttributes} to set all your buffers and attributes.
 *
 * @see {@link module:webgl-utils.setAttributes} for example
 * @param {WebGLProgram} program the program to create setters for.
 * @return {Object.<string, function>} an object with a setter for each attribute by name.
 * @memberOf module:webgl-utils
 */
export function createAttributeSetters(gl: WebGL2RenderingContext, program: WebGLProgram) {
    const attribSetters: {[name: string]: any} = {
    };

    function createAttribSetter(index: number) {
        return function (b: any) {
            if (b.value) {
                gl.disableVertexAttribArray(index);
                switch (b.value.length) {
                    case 4:
                        gl.vertexAttrib4fv(index, b.value);
                        break;
                    case 3:
                        gl.vertexAttrib3fv(index, b.value);
                        break;
                    case 2:
                        gl.vertexAttrib2fv(index, b.value);
                        break;
                    case 1:
                        gl.vertexAttrib1fv(index, b.value);
                        break;
                    default:
                        throw new Error('the length of a float constant value must be between 1 and 4!');
                }
            } else {
                gl.bindBuffer(gl.ARRAY_BUFFER, b.buffer);
                gl.enableVertexAttribArray(index);
                if (b.type == gl.BYTE || /* b.type == gl.UNSIGNED_BYTE || */ b.type == gl.SHORT ||
                    b.type == gl.UNSIGNED_SHORT || b.type == gl.INT || b.type == gl.UNSIGNED_INT) {
                    gl.vertexAttribIPointer(
                        index, b.numComponents || b.size, b.type, b.stride || 0, b.offset || 0);
                } else {
                    gl.vertexAttribPointer(
                        index, b.numComponents || b.size, b.type || gl.FLOAT, b.normalize || false, b.stride || 0, b.offset || 0);
                }
                if (b.divisor) {
                    gl.vertexAttribDivisor(index, b.divisor);
                }
            }
        };
    }

    const numAttribs = gl.getProgramParameter(program, gl.ACTIVE_ATTRIBUTES);
    for (let ii = 0; ii < numAttribs; ++ii) {
        const attribInfo = gl.getActiveAttrib(program, ii);
        if (!attribInfo) {
            break;
        }
        const index = gl.getAttribLocation(program, attribInfo.name);
        attribSetters[attribInfo.name] = createAttribSetter(index);
    }

    return attribSetters;
}

/**
 * Sets attributes and binds buffers (deprecated... use {@link module:webgl-utils.setBuffersAndAttributes})
 *
 * Example:
 *
 *     let program = createProgramFromScripts(
 *         gl, ["some-vs", "some-fs"]);
 *
 *     let attribSetters = createAttributeSetters(program);
 *
 *     let positionBuffer = gl.createBuffer();
 *     let texcoordBuffer = gl.createBuffer();
 *
 *     let attribs = {
 *       a_position: {buffer: positionBuffer, numComponents: 3},
 *       a_texcoord: {buffer: texcoordBuffer, numComponents: 2},
 *     };
 *
 *     gl.useProgram(program);
 *
 * This will automatically bind the buffers AND set the
 * attributes.
 *
 *     setAttributes(attribSetters, attribs);
 *
 * Properties of attribs. For each attrib you can add
 * properties:
 *
 * *   type: the type of data in the buffer. Default = gl.FLOAT
 * *   normalize: whether or not to normalize the data. Default = false
 * *   stride: the stride. Default = 0
 * *   offset: offset into the buffer. Default = 0
 *
 * For example if you had 3 value float positions, 2 value
 * float texcoord and 4 value uint8 colors you'd setup your
 * attribs like this
 *
 *     let attribs = {
 *       a_position: {buffer: positionBuffer, numComponents: 3},
 *       a_texcoord: {buffer: texcoordBuffer, numComponents: 2},
 *       a_color: {
 *         buffer: colorBuffer,
 *         numComponents: 4,
 *         type: gl.UNSIGNED_BYTE,
 *         normalize: true,
 *       },
 *     };
 *
 * @param {Object.<string, function>|model:webgl-utils.ProgramInfo} setters Attribute setters as returned from createAttributeSetters or a ProgramInfo as returned {@link module:webgl-utils.createProgramInfo}
 * @param {Object.<string, module:webgl-utils.AttribInfo>} attribs AttribInfos mapped by attribute name.
 * @memberOf module:webgl-utils
 * @deprecated use {@link module:webgl-utils.setBuffersAndAttributes}
 */
function setAttributes(setters: {[name: string]: CallableFunction}|ProgramInfo|any, attribs: {[name: string]: AttribInfo}) {
    setters = setters.attribSetters || setters;
    Object.keys(attribs).forEach(function (name) {
        const setter = setters[name];
        if (setter) {
            setter(attribs[name]);
        }
    });
}

/**
 * Creates a vertex array object and then sets the attributes
 * on it
 *
 * @param {WebGLRenderingContext} gl The WebGLRenderingContext
 *        to use.
 * @param {Object.<string, function>} setters Attribute setters as returned from createAttributeSetters
 * @param {Object.<string, module:webgl-utils.AttribInfo>} attribs AttribInfos mapped by attribute name.
 * @param {WebGLBuffer} [indices] an optional ELEMENT_ARRAY_BUFFER of indices
 */
function createVAOAndSetAttributes(gl: WebGL2RenderingContext, setters: any, attribs: any, indices?: WebGLBuffer) {
    const vao = gl.createVertexArray();
    gl.bindVertexArray(vao);
    setAttributes(setters, attribs);
    if (indices) {
        gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, indices);
    }
    // We unbind this because otherwise any change to ELEMENT_ARRAY_BUFFER
    // like when creating buffers for other stuff will mess up this VAO's binding
    gl.bindVertexArray(null);
    return vao;
}

/**
 * Creates a vertex array object and then sets the attributes
 * on it
 *
 * @param {WebGLRenderingContext} gl The WebGLRenderingContext
 *        to use.
 * @param {Object.<string, function>| module:webgl-utils.ProgramInfo} programInfo as returned from createProgramInfo or Attribute setters as returned from createAttributeSetters
 * @param {module:webgl-utils:BufferInfo} bufferInfo BufferInfo as returned from createBufferInfoFromArrays etc...
 * @param {WebGLBuffer} [indices] an optional ELEMENT_ARRAY_BUFFER of indices
 */
function createVAOFromBufferInfo(gl: WebGL2RenderingContext, programInfo: any, bufferInfo: BufferInfo) {
    return createVAOAndSetAttributes(gl, programInfo.attribSetters || programInfo, bufferInfo.attribs, bufferInfo.indices);
}

type ProgramInfo = {
    program: WebGLProgram, // A shader program
    uniformSetters: { [name: string]: CallableFunction }, // object of setters as returned from createUniformSetters
    attribSetters: { [name: string]: CallableFunction }, // object of setters as returned from createAttribSetters
}


/**
 * Sets attributes and buffers including the `ELEMENT_ARRAY_BUFFER` if appropriate
 *
 * Example:
 *
 *     let programInfo = createProgramInfo(
 *         gl, ["some-vs", "some-fs"]);
 *
 *     let arrays = {
 *       position: { numComponents: 3, data: [0, 0, 0, 10, 0, 0, 0, 10, 0, 10, 10, 0], },
 *       texcoord: { numComponents: 2, data: [0, 0, 0, 1, 1, 0, 1, 1],                 },
 *     };
 *
 *     let bufferInfo = createBufferInfoFromArrays(gl, arrays);
 *
 *     gl.useProgram(programInfo.program);
 *
 * This will automatically bind the buffers AND set the
 * attributes.
 *
 *     setBuffersAndAttributes(programInfo.attribSetters, bufferInfo);
 *
 * For the example above it is equivilent to
 *
 *     gl.bindBuffer(gl.ARRAY_BUFFER, positionBuffer);
 *     gl.enableVertexAttribArray(a_positionLocation);
 *     gl.vertexAttribPointer(a_positionLocation, 3, gl.FLOAT, false, 0, 0);
 *     gl.bindBuffer(gl.ARRAY_BUFFER, texcoordBuffer);
 *     gl.enableVertexAttribArray(a_texcoordLocation);
 *     gl.vertexAttribPointer(a_texcoordLocation, 4, gl.FLOAT, false, 0, 0);
 *
 * @param {WebGLRenderingContext} gl A WebGLRenderingContext.
 * @param {Object.<string, function>} setters Attribute setters as returned from `createAttributeSetters`
 * @param {module:webgl-utils.BufferInfo} buffers a BufferInfo as returned from `createBufferInfoFromArrays`.
 * @memberOf module:webgl-utils
 */
export function setBuffersAndAttributes(gl: WebGLRenderingContext, setters: {[name: string]: CallableFunction}, buffers: BufferInfo) {
    setAttributes(setters, buffers.attribs);
    if (buffers.indices) {
        gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, buffers.indices);
    }
}

// Add your prefix here.
const browserPrefixes = [
    '',
    'MOZ_',
    'OP_',
    'WEBKIT_',
];

/**
 * Given an extension name like WEBGL_compressed_texture_s3tc
 * returns the supported version extension, like
 * WEBKIT_WEBGL_compressed_teture_s3tc
 * @param {string} name Name of extension to look for
 * @return {WebGLExtension} The extension or undefined if not
 *     found.
 * @memberOf module:webgl-utils
 */
export function getExtensionWithKnownPrefixes(gl: WebGLRenderingContext, name: string) {
    for (let ii = 0; ii < browserPrefixes.length; ++ii) {
        const prefixedName = browserPrefixes[ii] + name;
        const ext = gl.getExtension(prefixedName);
        if (ext) {
            return ext;
        }
    }
    return undefined;
}

/**
 * Resize a canvas to match the size its displayed.
 * @param {HTMLCanvasElement} canvas The canvas to resize.
 * @param {number} [multiplier] amount to multiply by.
 *    Pass in window.devicePixelRatio for native pixels.
 * @return {boolean} true if the canvas was resized.
 * @memberOf module:webgl-utils
 */
export function resizeCanvasToDisplaySize(canvas: HTMLCanvasElement, multiplier?: number) {
    multiplier = multiplier || 1;
    const width = canvas.clientWidth * multiplier | 0;
    const height = canvas.clientHeight * multiplier | 0;
    if (canvas.width !== width || canvas.height !== height) {
        canvas.width = width;
        canvas.height = height;
        return true;
    }
    return false;
}

// Add `push` to a typed array. It just keeps a 'cursor'
// and allows use to `push` values into the array so we
// don't have to manually compute offsets
function augmentTypedArray(typedArray: any, numComponents: number) {
    let cursor = 0;
    typedArray.push = function () {
        for (let ii = 0; ii < arguments.length; ++ii) {
            const value = arguments[ii];
            if (value instanceof Array || (value.buffer && value.buffer instanceof ArrayBuffer)) {
                for (let jj = 0; jj < value.length; ++jj) {
                    typedArray[cursor++] = value[jj];
                }
            } else {
                typedArray[cursor++] = value;
            }
        }
    };
    typedArray.reset = function (opt_index?: number) {
        cursor = opt_index || 0;
    };
    typedArray.numComponents = numComponents;
    Object.defineProperty(typedArray, 'numElements', {
        get: function () {
            return this.length / this.numComponents | 0;
        },
    });
    return typedArray;
}

/**
 * creates a typed array with a `push` function attached
 * so that you can easily *push* values.
 *
 * `push` can take multiple arguments. If an argument is an array each element
 * of the array will be added to the typed array.
 *
 * Example:
 *
 *     let array = createAugmentedTypedArray(3, 2);  // creates a Float32Array with 6 values
 *     array.push(1, 2, 3);
 *     array.push([4, 5, 6]);
 *     // array now contains [1, 2, 3, 4, 5, 6]
 *
 * Also has `numComponents` and `numElements` properties.
 *
 * @param {number} numComponents number of components
 * @param {number} numElements number of elements. The total size of the array will be `numComponents * numElements`.
 * @param {constructor} opt_type A constructor for the type. Default = `Float32Array`.
 * @return {ArrayBuffer} A typed array.
 * @memberOf module:webgl-utils
 */
function createAugmentedTypedArray(numComponents: number, numElements: number, opt_type?: any) {
    const Type = opt_type || Float32Array;
    return augmentTypedArray(new Type(numComponents * numElements), numComponents);
}

export function createBufferFromTypedArray(gl: WebGLRenderingContext, array: any, type?: number, drawType?: number) {
    type = type || gl.ARRAY_BUFFER;
    const buffer = gl.createBuffer();
    gl.bindBuffer(type, buffer);
    gl.bufferData(type, array, drawType || gl.STATIC_DRAW);
    return buffer;
}

function allButIndices(name: string) {
    return name !== 'indices';
}

function createMapping(obj: {[name: string]: any}) {
    const mapping: {[name: string]: string} = {};
    Object.keys(obj).filter(allButIndices).forEach(function (key) {
        // TODO: maybe revisit this mapping idea, idk
        // mapping['a_' + key] = key;
        mapping[key] = key;
    });
    return mapping;
}

function getGLTypeForTypedArray(gl: WebGLRenderingContext, typedArray: TypedArray) {
    if (typedArray instanceof Int8Array) { return gl.BYTE; }            // eslint-disable-line
    if (typedArray instanceof Uint8Array) { return gl.UNSIGNED_BYTE; }   // eslint-disable-line
    if (typedArray instanceof Int16Array) { return gl.SHORT; }           // eslint-disable-line
    if (typedArray instanceof Uint16Array) { return gl.UNSIGNED_SHORT; }  // eslint-disable-line
    if (typedArray instanceof Int32Array) { return gl.INT; }             // eslint-disable-line
    if (typedArray instanceof Uint32Array) { return gl.UNSIGNED_INT; }    // eslint-disable-line
    if (typedArray instanceof Float32Array) { return gl.FLOAT; }           // eslint-disable-line
    throw 'unsupported typed array type';
}

// This is really just a guess. Though I can't really imagine using
// anything else? Maybe for some compression?
function getNormalizationForTypedArray(typedArray: Int8Array|Uint8Array|any) {
    if (typedArray instanceof Int8Array) { return true; }  // eslint-disable-line
    if (typedArray instanceof Uint8Array) { return true; }  // eslint-disable-line
    return false;
}

function isArrayBuffer(a: any) {
    return a.buffer && a.buffer instanceof ArrayBuffer;
}

function guessNumComponentsFromName(name: string, length: number) {
    let numComponents;
    if (name.indexOf('coord') >= 0) {
        numComponents = 2;
    } else if (name.indexOf('color') >= 0) {
        numComponents = 4;
    } else {
        numComponents = 3;  // position, normals, indices ...
    }

    if (length % numComponents > 0) {
        throw 'can not guess numComponents. You should specify it.';
    }

    return numComponents;
}

type TypedArray = {
    data: ArrayBuffer,
    numComponents: number,
    length?: number,
}

function makeTypedArray(array: any, name: string): TypedArray {
    if (isArrayBuffer(array)) {
        return array;
    }

    if (array.data && isArrayBuffer(array.data)) {
        return array.data;
    }

    if (Array.isArray(array)) {
        array = {
            data: array,
        };
    }

    if (!array.numComponents) {
        array.numComponents = guessNumComponentsFromName(name, array.length);
    }

    let type = array.type;
    if (!type) {
        if (name === 'indices') {
            type = Uint16Array;
        }
    }
    const typedArray = createAugmentedTypedArray(array.numComponents, array.data.length / array.numComponents | 0, type);
    typedArray.push(array.data);
    return typedArray;
}

export type AttribInfo = {
    numComponents?: number, // the number of components for this attribute.
    size?: number, // the number of components for this attribute.
    type?: number, // the type of the attribute (eg. `gl.FLOAT`, `gl.UNSIGNED_BYTE`, etc...) Default = `gl.FLOAT`
    normalize?: boolean, // whether or not to normalize the data. Default = false
    offset?: number, // offset into buffer in bytes. Default = 0
    stride?: number, // the stride in bytes per element. Default = 0
    value?: Float32Array, // the value of the attribute, for a vertex attrib
    buffer?: WebGLBuffer, //the buffer that contains the data for this attribute
    divisor?: number, // for instanced attributes, the divisor
    update?: boolean, // whether it needs to be updated again
    data?: ArrayBufferLike,
};


/**
 * Creates a set of attribute data and WebGLBuffers from set of arrays
 *
 * Given
 *
 *      let arrays = {
 *        position: { numComponents: 3, data: [0, 0, 0, 10, 0, 0, 0, 10, 0, 10, 10, 0], },
 *        texcoord: { numComponents: 2, data: [0, 0, 0, 1, 1, 0, 1, 1],                 },
 *        normal:   { numComponents: 3, data: [0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1],     },
 *        color:    { numComponents: 4, data: [255, 255, 255, 255, 255, 0, 0, 255, 0, 0, 255, 255], type: Uint8Array, },
 *        indices:  { numComponents: 3, data: [0, 1, 2, 1, 2, 3],                       },
 *      };
 *
 * returns something like
 *
 *      let attribs = {
 *        a_position: { numComponents: 3, type: gl.FLOAT,         normalize: false, buffer: WebGLBuffer, },
 *        a_texcoord: { numComponents: 2, type: gl.FLOAT,         normalize: false, buffer: WebGLBuffer, },
 *        a_normal:   { numComponents: 3, type: gl.FLOAT,         normalize: false, buffer: WebGLBuffer, },
 *        a_color:    { numComponents: 4, type: gl.UNSIGNED_BYTE, normalize: true,  buffer: WebGLBuffer, },
 *      };
 *
 * @param {WebGLRenderingContext} gl The webgl rendering context.
 * @param {Object.<string, array|typedarray>} arrays The arrays
 * @param {Object.<string, string>} [opt_mapping] mapping from attribute name to array name.
 *     if not specified defaults to "a_name" -> "name".
 * @return {Object.<string, module:webgl-utils.AttribInfo>} the attribs
 * @memberOf module:webgl-utils
 */
export function createAttribsFromArrays(gl: WebGLRenderingContext, arrays: {[name: string]: any}, opt_mapping?: {[name: string]: string}): {[name: string]: AttribInfo} {
    const mapping = opt_mapping || createMapping(arrays);
    const attribs: {[name: string]: AttribInfo} = {};
    Object.keys(mapping).forEach(function (attribName) {
        const bufferName = mapping[attribName];
        const origArray = arrays[bufferName];
        if (origArray.value) {
            attribs[attribName] = {
                value: origArray.value,
            };
        } else {
            const array = makeTypedArray(origArray, bufferName);
            attribs[attribName] = {
                buffer: createBufferFromTypedArray(gl, array),
                numComponents: origArray.numComponents || array.numComponents || guessNumComponentsFromName(bufferName, array.length),
                type: getGLTypeForTypedArray(gl, array),
                normalize: getNormalizationForTypedArray(array),
                stride: origArray.stride,
                offset: origArray.offset,
            };
            if (origArray.divisor) {
                attribs[attribName].divisor = origArray.divisor;
            }
            if (origArray.retain) {
                attribs[attribName].data = origArray.data.buffer;
            }
        }
    });
    return attribs;
}

function getArray(array: any) {
    return array.length ? array : array.data;
}

const texcoordRE = /coord|texture/i;
const colorRE = /color|colour/i;

function getNumComponents(array: any, arrayName: string) {
    return array.numComponents || array.size || guessNumComponentsFromName(arrayName, getArray(array).length);
}

/**
 * tries to get the number of elements from a set of arrays.
 */
const positionKeys = ['position', 'positions', 'a_position'];
function getNumElementsFromNonIndexedArrays(arrays: any) {
    let key;
    for (const k of positionKeys) {
        if (k in arrays) {
            key = k;
            break;
        }
    }
    key = key || Object.keys(arrays)[0];
    const array = arrays[key];
    const length = getArray(array).length;
    const numComponents = getNumComponents(array, key);
    const numElements = length / numComponents;
    if (length % numComponents > 0) {
        throw new Error(`numComponents ${numComponents} not correct for length ${length}`);
    }
    return numElements;
}

export type BufferInfo = {
    numElements: number, // The number of elements to pass to `gl.drawArrays` or `gl.drawElements`.
    indices?: WebGLBuffer, // The indices `ELEMENT_ARRAY_BUFFER` if any indices exist
    attribs: { [name: string]: AttribInfo }, // The attribs approriate to call `setAttributes`
}

/**
 * Creates a BufferInfo from an object of arrays.
 *
 * This can be passed to {@link module:webgl-utils.setBuffersAndAttributes} and to
 * {@link module:webgl-utils:drawBufferInfo}.
 *
 * Given an object like
 *
 *     let arrays = {
 *       position: { numComponents: 3, data: [0, 0, 0, 10, 0, 0, 0, 10, 0, 10, 10, 0], },
 *       texcoord: { numComponents: 2, data: [0, 0, 0, 1, 1, 0, 1, 1],                 },
 *       normal:   { numComponents: 3, data: [0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1],     },
 *       indices:  { numComponents: 3, data: [0, 1, 2, 1, 2, 3],                       },
 *     };
 *
 *  Creates an BufferInfo like this
 *
 *     bufferInfo = {
 *       numElements: 4,        // or whatever the number of elements is
 *       indices: WebGLBuffer,  // this property will not exist if there are no indices
 *       attribs: {
 *         a_position: { buffer: WebGLBuffer, numComponents: 3, },
 *         a_normal:   { buffer: WebGLBuffer, numComponents: 3, },
 *         a_texcoord: { buffer: WebGLBuffer, numComponents: 2, },
 *       },
 *     };
 *
 *  The properties of arrays can be JavaScript arrays in which case the number of components
 *  will be guessed.
 *
 *     let arrays = {
 *        position: [0, 0, 0, 10, 0, 0, 0, 10, 0, 10, 10, 0],
 *        texcoord: [0, 0, 0, 1, 1, 0, 1, 1],
 *        normal:   [0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1],
 *        indices:  [0, 1, 2, 1, 2, 3],
 *     };
 *
 *  They can also by TypedArrays
 *
 *     let arrays = {
 *        position: new Float32Array([0, 0, 0, 10, 0, 0, 0, 10, 0, 10, 10, 0]),
 *        texcoord: new Float32Array([0, 0, 0, 1, 1, 0, 1, 1]),
 *        normal:   new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1]),
 *        indices:  new Uint16Array([0, 1, 2, 1, 2, 3]),
 *     };
 *
 *  Or augmentedTypedArrays
 *
 *     let positions = createAugmentedTypedArray(3, 4);
 *     let texcoords = createAugmentedTypedArray(2, 4);
 *     let normals   = createAugmentedTypedArray(3, 4);
 *     let indices   = createAugmentedTypedArray(3, 2, Uint16Array);
 *
 *     positions.push([0, 0, 0, 10, 0, 0, 0, 10, 0, 10, 10, 0]);
 *     texcoords.push([0, 0, 0, 1, 1, 0, 1, 1]);
 *     normals.push([0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1]);
 *     indices.push([0, 1, 2, 1, 2, 3]);
 *
 *     let arrays = {
 *        position: positions,
 *        texcoord: texcoords,
 *        normal:   normals,
 *        indices:  indices,
 *     };
 *
 * For the last example it is equivalent to
 *
 *     let bufferInfo = {
 *       attribs: {
 *         a_position: { numComponents: 3, buffer: gl.createBuffer(), },
 *         a_texcoods: { numComponents: 2, buffer: gl.createBuffer(), },
 *         a_normals: { numComponents: 3, buffer: gl.createBuffer(), },
 *       },
 *       indices: gl.createBuffer(),
 *       numElements: 6,
 *     };
 *
 *     gl.bindBuffer(gl.ARRAY_BUFFER, bufferInfo.attribs.a_position.buffer);
 *     gl.bufferData(gl.ARRAY_BUFFER, arrays.position, gl.STATIC_DRAW);
 *     gl.bindBuffer(gl.ARRAY_BUFFER, bufferInfo.attribs.a_texcoord.buffer);
 *     gl.bufferData(gl.ARRAY_BUFFER, arrays.texcoord, gl.STATIC_DRAW);
 *     gl.bindBuffer(gl.ARRAY_BUFFER, bufferInfo.attribs.a_normal.buffer);
 *     gl.bufferData(gl.ARRAY_BUFFER, arrays.normal, gl.STATIC_DRAW);
 *     gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, bufferInfo.indices);
 *     gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, arrays.indices, gl.STATIC_DRAW);
 *
 * @param {WebGLRenderingContext} gl A WebGLRenderingContext
 * @param {Object.<string, array|object|typedarray>} arrays Your data
 * @param {Object.<string, string>} [opt_mapping] an optional mapping of attribute to array name.
 *    If not passed in it's assumed the array names will be mapped to an attribute
 *    of the same name with "a_" prefixed to it. An other words.
 *
 *        let arrays = {
 *           position: ...,
 *           texcoord: ...,
 *           normal:   ...,
 *           indices:  ...,
 *        };
 *
 *        bufferInfo = createBufferInfoFromArrays(gl, arrays);
 *
 *    Is the same as
 *
 *        let arrays = {
 *           position: ...,
 *           texcoord: ...,
 *           normal:   ...,
 *           indices:  ...,
 *        };
 *
 *        let mapping = {
 *          a_position: "position",
 *          a_texcoord: "texcoord",
 *          a_normal:   "normal",
 *        };
 *
 *        bufferInfo = createBufferInfoFromArrays(gl, arrays, mapping);
 *
 * @return {module:webgl-utils.BufferInfo} A BufferInfo
 * @memberOf module:webgl-utils
 */
export function createBufferInfoFromArrays(gl: WebGLRenderingContext, arrays: {[name: string]: any}, opt_mapping?: {[name: string]: string}): BufferInfo {
    const bufferInfo: BufferInfo = {
        attribs: createAttribsFromArrays(gl, arrays, opt_mapping),
        numElements: 0
    };
    let indices = arrays.indices;
    if (indices) {
        indices = makeTypedArray(indices, 'indices');
        bufferInfo.indices = createBufferFromTypedArray(gl, indices, gl.ELEMENT_ARRAY_BUFFER);
        bufferInfo.numElements = indices.length;
    } else {
        bufferInfo.numElements = getNumElementsFromNonIndexedArrays(arrays);
    }

    return bufferInfo;
}

/**
 * Creates buffers from typed arrays
 *
 * Given something like this
 *
 *     let arrays = {
 *        positions: [1, 2, 3],
 *        normals: [0, 0, 1],
 *     }
 *
 * returns something like
 *
 *     buffers = {
 *       positions: WebGLBuffer,
 *       normals: WebGLBuffer,
 *     }
 *
 * If the buffer is named 'indices' it will be made an ELEMENT_ARRAY_BUFFER.
 *
 * @param {WebGLRenderingContext} gl A WebGLRenderingContext.
 * @param {Object<string, array|typedarray>} arrays
 * @return {Object<string, WebGLBuffer>} returns an object with one WebGLBuffer per array
 * @memberOf module:webgl-utils
 */
function createBuffersFromArrays(gl: WebGLRenderingContext, arrays: {[name: string]: any}) {
    const buffers: {
        [name: string]: WebGLBuffer,
    } = {
        numElements: 0,
    };
    Object.keys(arrays).forEach(function (key) {
        const type = key === 'indices' ? gl.ELEMENT_ARRAY_BUFFER : gl.ARRAY_BUFFER;
        const array = makeTypedArray(arrays[key], name);
        buffers[key] = createBufferFromTypedArray(gl, array, type);
    });

    // hrm
    if (arrays.indices) {
        buffers.numElements = arrays.indices.length;
    } else if (arrays.position) {
        buffers.numElements = arrays.position.length / 3;
    }

    return buffers;
}

/**
 * Calls `gl.drawElements` or `gl.drawArrays`, whichever is appropriate
 *
 * normally you'd call `gl.drawElements` or `gl.drawArrays` yourself
 * but calling this means if you switch from indexed data to non-indexed
 * data you don't have to remember to update your draw call.
 *
 * @param {WebGLRenderingContext} gl A WebGLRenderingContext
 * @param {module:webgl-utils.BufferInfo} bufferInfo as returned from createBufferInfoFromArrays
 * @param {enum} [primitiveType] eg (gl.TRIANGLES, gl.LINES, gl.POINTS, gl.TRIANGLE_STRIP, ...)
 * @param {number} [count] An optional count. Defaults to bufferInfo.numElements
 * @param {number} [offset] An optional offset. Defaults to 0.
 * @memberOf module:webgl-utils
 */
function drawBufferInfo(gl: WebGLRenderingContext, bufferInfo: BufferInfo, primitiveType?: number, count?: number, offset?: number) {
    const indices = bufferInfo.indices;
    primitiveType = primitiveType === undefined ? gl.TRIANGLES : primitiveType;
    const numElements = count === undefined ? bufferInfo.numElements : count;
    offset = offset === undefined ? 0 : offset;
    if (indices) {
        gl.drawElements(primitiveType, numElements, gl.UNSIGNED_SHORT, offset);
    } else {
        gl.drawArrays(primitiveType, offset, numElements);
    }
}

 type DrawObject = {
     programInfo: ProgramInfo, // A ProgramInfo as returned from createProgramInfo
     bufferInfo: BufferInfo, // A BufferInfo as returned from createBufferInfoFromArrays
     uniforms: { [name: string]: any }, // The values for the uniforms
 };

/**
 * Draws a list of objects
 * @param {WebGLRenderingContext} gl A WebGLRenderingContext
 * @param {DrawObject[]} objectsToDraw an array of objects to draw.
 * @memberOf module:webgl-utils
 */
function drawObjectList(gl: WebGLRenderingContext, objectsToDraw: DrawObject[]) {
    let lastUsedProgramInfo: WebGLProgram = null;
    let lastUsedBufferInfo: BufferInfo = null;

    objectsToDraw.forEach(function (object) {
        const programInfo = object.programInfo;
        const bufferInfo = object.bufferInfo;
        let bindBuffers = false;

        if (programInfo !== lastUsedProgramInfo) {
            lastUsedProgramInfo = programInfo;
            gl.useProgram(programInfo.program);
            bindBuffers = true;
        }

        // Setup all the needed attributes.
        if (bindBuffers || bufferInfo !== lastUsedBufferInfo) {
            lastUsedBufferInfo = bufferInfo;
            setBuffersAndAttributes(gl, programInfo.attribSetters, bufferInfo);
        }

        // Set the uniforms.
        setUniforms(programInfo.uniformSetters, object.uniforms);

        // Draw
        drawBufferInfo(gl, bufferInfo);
    });
}

function glEnumToString(gl: WebGLRenderingContext, v: any) {
    const results = [];
    for (const [key, value] of Object.entries(gl)) {
        if (value === v) {
            results.push(key);
        }
    }
    return results.length
        ? results.join(' | ')
        : `0x${v.toString(16)}`;
}

// Edge 20+
const isEdge = !!window.StyleMedia;
if (isEdge) {
    // Hack for Edge. Edge's WebGL implmentation is crap still and so they
    // only respond to "experimental-webgl". I don't want to clutter the
    // examples with that so his hack works around it
    HTMLCanvasElement.prototype.getContext = function (origFn) {
        return function () {
            let args = arguments;
            const type = args[0];
            if (type === 'webgl') {
                args = [].slice.call(arguments);
                args[0] = 'experimental-webgl';
            }
            return origFn.apply(this, args);
        };
    }(HTMLCanvasElement.prototype.getContext);
}
