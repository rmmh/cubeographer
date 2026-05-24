# version 300 es
precision mediump float;
precision highp int;

uniform mediump sampler2DArray atlas;
uniform float alphaCutoutThreshold;
uniform float uFogScale;

in vec3 vPosition;
in vec4 vColor;
in vec3 vNormal;
in vec2 vTexCoord;
flat in int vTexLayer;

out vec4 outColor;

void main()	{
    vec4 color = vec4( vColor ) * texture(atlas, vec3(vTexCoord, float(vTexLayer)));
    if (color.a < alphaCutoutThreshold) discard;
    // OFFICIAL MINECRAFT:
    //  *0.8 on the Z axis faces, by *0.6 on the X axis faces, and by *0.5 on the bottom face
    outColor = mix(
        vec4(color.rgb *
            max(0.5, dot(vec3(abs(vNormal.x), vNormal.y, abs(vNormal.z)), vec3(0.6,1.0,0.8)))
            , color.a),
        vec4(.722, .855, 1.0, 1.0),
        min(1.0, (1.0 / gl_FragCoord.w) / 3000.0) * uFogScale);
}
