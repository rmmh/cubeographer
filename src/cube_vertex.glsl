# version 300 es
precision mediump float;
precision highp int;

uniform mat4 modelViewMatrix; // optional
uniform mat4 projectionMatrix; // optional
uniform int space;

in vec3 position;
in vec4 color;
in vec3 normal;
in vec2 uv;
in uvec2 attr;
in uint normb;

out vec3 vPosition;
out vec4 vColor;
out vec3 vNormal;
out vec2 vTexCoord;

vec3 unpackPos(uint p) {
    return vec3(float(p >> 20), float((p >> 10) & 1023u), float(p & 1023u)) - vec3(space/2);
}
vec3 unpackColor(int p) {
    return vec3((p >> 16) & 0xff, (p >> 8) & 0xff, p & 0xff) / 255.0;
}

void main()	{
    vColor = vec4(unpackColor(int(attr.y)), 1.0);
    vNormal = normal.xyz;
    int block = 1023 - int(attr.y >> 24u);
    vTexCoord = (vec2(float(uv.x), float(uv.y)) + vec2(block % 32, block / 32)) / 32.0;
    gl_Position = projectionMatrix * modelViewMatrix * vec4( position + unpackPos(attr.x), 1.0 );
}
