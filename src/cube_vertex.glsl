# version 300 es
precision mediump float;
precision highp int;

// #define CUBE_SCALE 16

uniform mat4 modelViewMatrix; // optional
uniform mat4 projectionMatrix; // optional
uniform vec3 cameraPosition;
uniform vec3 offset;

in vec3 position;
in vec4 color;
in vec3 normal;
in vec2 uv;
in uvec3 attr;
in uint normb;

out vec3 vPosition;
out vec4 vColor;
out vec3 vNormal;
out vec2 vTexCoord;

vec3 unpackPos(uint p) { // 26b pos (9b,8b,9b each) => vec3
    return vec3(float((p >> 18) & 1023u) , float(p & 255u), float((p >> 8) & 1023u));
}

vec3 unpackColor(int p) { //12b => 24b color
    return vec3((p >> 20) & 0xf, (p >> 16) & 0xf, (p >> 12) & 0xf) / 15.0;
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
    bool sideSpecial = face < 4 && (attr.y & 64u) == 64u;
    float sideLight = float( (attr.z>>uint(face*4))&0xFu)/15.0 * 0.7 + 0.3;
    vColor = sideSpecial ? vec4(sideLight, sideLight, sideLight, 1.0) : vec4(unpackColor(int(attr.y)) * vec3(sideLight), 1.0);
    // if (face >= 2)vColor = vec4(1,shouldDiscard(face, attr.y),0,1);
#ifdef CUBE_SCALE
    gl_Position = projectionMatrix * modelViewMatrix *
        vec4((shouldFlip ? vec3(1) - position : position) * vec3(CUBE_SCALE) + unpackedPos, 1.0 );
#else
    gl_Position = projectionMatrix * modelViewMatrix *
        vec4((shouldFlip ? vec3(1) - position : position) + unpackedPos, 1.0 );
#endif
    vNormal = normal * vec3(shouldFlip ? -1.0 : 1.0);
    int block = 1023 - (int(attr.y >> 24u) + (sideSpecial ? 256 : 0));
    // TODO: fix alpha bleeding with mipmaps??
    vec2 primCoord = vec2(float(uv.x), float(uv.y)) * (1.0-2./16.) + vec2(1./16.,1./16.);
    vTexCoord = ((shouldFlip ?  primCoord : vec2(1) - primCoord) + vec2(31 - block % 32, block / 32)) / 32.0;
}
