# version 300 es
precision mediump float;
precision highp int;

uniform mat4 modelViewMatrix; // optional
uniform mat4 projectionMatrix; // optional
uniform vec3 cameraPosition;
uniform vec3 offset;
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

vec3 unpackPos(uint p) { // 26b pos (9b,8b,9b each) => vec3
    return vec3(float(p >> 20), float((p >> 10) & 1023u), float(p & 1023u)) - vec3(space/2);
}

vec3 unpackColor(int p) { //12b => 24b color
    return vec3((p >> 14) & 0xf, (p >> 10) & 0xf, (p >> 6) & 0xf) / 15.0;
}

bool shouldDiscard(int face, uint s) {
    return (s & uint(1 << face)) == 0u;
}

void main()	{
    vec3 unpackedPos = unpackPos(attr.x);
    bool shouldFlip = dot(normal, cameraPosition - (unpackedPos + offset)) < 0.0;
    int face = int(gl_VertexID / 6) * 2 + (shouldFlip ? 1 : 0);
    if (shouldDiscard(face, attr.y)) {
        gl_Position = vec4(1e20);
        return;
    }
    vColor = vec4(unpackColor(int(attr.y)), 1.0);
    // if (face >= 2)vColor = vec4(1,shouldDiscard(face, attr.y),0,1);
    gl_Position = projectionMatrix * modelViewMatrix * vec4((shouldFlip ? vec3(1) - position : position) + unpackedPos, 1.0 );
    vNormal = normal * vec3(shouldFlip ? -1.0 : 1.0);
    int block = 1023 - int(attr.y >> 24u);
    // TODO: fix alpha bleeding with mipmaps??
    vTexCoord = (vec2(float(uv.x), float(uv.y)) * (1.0-2./32.) + vec2(1./32.,1./32.) + vec2(31 - block % 32, block / 32)) / 32.0;
}
