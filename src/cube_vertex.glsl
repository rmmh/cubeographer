# version 300 es
precision mediump float;
precision highp int;

// PACKING FORMATS:
// 0: basic cube, with one or two face textures, in a 256x256x256 regionlet
// 0: 64 bits: 8b blockid, 24b position (8b x/y/z) // 2b flags 24b lighting (4b * 6 faces) 6b facevis

// #define CUBE_SCALE 16

//DEFINESBLOCK

uniform mat4 modelViewMatrix; // optional
uniform mat4 projectionMatrix; // optional
uniform vec3 cameraPosition;
uniform vec3 offset;

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
    return vec3(float((p >> 16) & 255u) , float(p & 255u), float((p >> 8) & 255u));
}

vec3 unpackColor(int blockId, uint color) {
    // TODO: read biome color from texture?
    if ((color & (1u << 31)) == 0u)
        return vec3(1.0);
#ifdef WATER_ID
    if (blockId == WATER_ID)
        return vec3(0.2, 0.4, 0.93);
#endif
    return vec3(0.4,0.73,0.27);
}

bool shouldDiscard(int face, uint s) {
    return (s & uint(1 << face)) == 0u;
}

void main()	{
    vec3 unpackedPos = unpackPos(attr.x);
    int blockId = int(attr.x >> 24u);
#ifdef CROSS
    float light = float(attr.y&0xFu)/15.0 * 0.7 + 0.3;
    vColor = vec4(unpackColor(blockId, attr.y) * vec3(light), 1.0);
    // if (face >= 2)vColor = vec4(1,shouldDiscard(face, attr.y),0,1);
    bool sideSpecial = false;
    vNormal = normal;
    gl_Position = projectionMatrix * modelViewMatrix * vec4(position + unpackedPos, 1.0 );
#else
    bool shouldFlip = dot(normal, cameraPosition - (unpackedPos + offset)) < 0.0;
    int face = int(gl_VertexID / 6) * 2 + (shouldFlip ? 1 : 0);
    if (shouldDiscard(face, attr.y)) {
        gl_Position = vec4(1e20);
        return;
    }
    bool sideSpecial = face >= 4 && (attr.y & (1u<<30)) != 0u;
    float sideLight = float( (attr.y>>uint(6+face*4))&0xFu)/15.0 * 0.7 + 0.3;
    vColor = sideSpecial ? vec4(sideLight, sideLight, sideLight, 1.0) :
             vec4(unpackColor(blockId, attr.y) * vec3(sideLight), 1.0);
    gl_Position = projectionMatrix * modelViewMatrix *
        vec4((shouldFlip ? vec3(1) - position : position) + unpackedPos, 1.0 );
    vNormal = normal * vec3(shouldFlip ? -1.0 : 1.0);
#endif // CROSS
    int block = (blockId + (sideSpecial ? 256 : 0));
    vec2 primCoord;
    primCoord = vec2(float(uv.x), float(uv.y)) * (1.0-1./128.) + vec2(1./256.);
#ifdef CROSS
    vTexCoord = (vec2(1) - primCoord + vec2(block % 32, block / 32)) / 32.0;
#else
    vTexCoord = ((shouldFlip ?  primCoord : vec2(1) - primCoord) + vec2(block % 32, block / 32)) / 32.0;
#endif
}
