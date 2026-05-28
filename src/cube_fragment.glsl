# version 300 es
precision mediump float;
precision highp int;

uniform mediump sampler2DArray atlas;
uniform float alphaCutoutThreshold;
uniform float uFogScale;

in vec3 vPosition;
in vec4 vColor;
in vec2 vTexCoord;
flat in int vTexLayer;

out vec4 outColor;

void main()	{
    vec4 color = vec4( vColor ) * texture(atlas, vec3(vTexCoord, float(vTexLayer)));
    if (color.a < alphaCutoutThreshold) discard;
    outColor = mix(
        color,
        vec4(.722, .855, 1.0, 1.0),
        min(1.0, (1.0 / gl_FragCoord.w) / 3000.0) * uFogScale);
}
