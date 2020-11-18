# version 300 es
precision mediump float;
precision highp int;

uniform sampler2D atlas;

in vec3 vPosition;
in vec4 vColor;
in vec3 vNormal;
in vec2 vTexCoord;

out vec4 outColor;

void main()	{
    vec4 color = vec4( vColor ) * texture(atlas, vTexCoord);
    if (color.a == 0.0) discard;
    outColor = vec4(color.rgb * (0.85+.2*dot(vNormal, normalize(vec3(10,5,2)))), color.a);
}
