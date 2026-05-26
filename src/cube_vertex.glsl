# version 300 es
precision highp float;
precision highp int;

// PACKING FORMATS:
// 0: basic cube, with one or two face textures, in a 256x256x256 regionlet
// 0: 64 bits: 8b blockid, 24b position (8b x/y/z) // 2b flags 24b lighting (4b * 6 faces) 6b facevis

// #define CUBE_SCALE 16

//DEFINESBLOCK

#ifdef CUBOID
uniform highp usampler2D cuboidDataTex;

uvec4 fetchCuboidPixel(int blockId, int pixelOffset) {
    int pixelIndex = (blockId << 2) + pixelOffset;
    ivec2 texCoord = ivec2(pixelIndex & 511, pixelIndex >> 9);
    return texelFetch(cuboidDataTex, texCoord, 0);
}

vec2 unpackUV(uint p) {
    return vec2(float(p & 0xFFFFu) / 256.0, float(p >> 16u) / 256.0);
}
#endif

uniform mat4 modelViewMatrix; // optional
uniform mat4 projectionMatrix; // optional
uniform vec3 cameraPosition;
uniform vec3 offset;

in vec3 position;
in vec3 normal;
in vec2 uv;
in uvec2 attr;

out vec4 vColor;
out vec3 vNormal;
out vec2 vTexCoord;
flat out int vTexLayer;

vec3 unpackPos(uint p) {
    uvec3 raw = uvec3(p >> 16u, p, p >> 8u);
    return vec3(raw & 255u);
}

vec3 unpackColor(int blockId, bool useColor) {
    // TODO: read biome color from texture?
    if (!useColor) return vec3(1.0);
#ifdef WATER_ID
    if (blockId == WATER_ID)
        return vec3(0.2, 0.4, 0.93);
#endif
    return vec3(0.4,0.73,0.27);
}

vec3 getLocalPos(int face, vec3 unitPos) {
    const vec3 bases[6] = vec3[6](
        vec3(0.0, 0.0, 1.0),
        vec3(1.0, 0.0, 0.0),
        vec3(1.0, 0.0, 1.0),
        vec3(0.0, 0.0, 0.0),
        vec3(1.0, 1.0, 1.0),
        vec3(1.0, 0.0, 0.0)
    );
    const vec3 dx[6] = vec3[6](
        vec3(0.0, 0.0, -1.0),
        vec3(0.0, 0.0, 1.0),
        vec3(-1.0, 0.0, 0.0),
        vec3(1.0, 0.0, 0.0),
        vec3(-1.0, 0.0, 0.0),
        vec3(-1.0, 0.0, 0.0)
    );
    const vec3 dy[6] = vec3[6](
        vec3(0.0, 1.0, 0.0),
        vec3(0.0, 1.0, 0.0),
        vec3(0.0, 1.0, 0.0),
        vec3(0.0, 1.0, 0.0),
        vec3(0.0, 0.0, -1.0),
        vec3(0.0, 0.0, 1.0)
    );
    return bases[face] + dx[face] * unitPos.x + dy[face] * unitPos.y;
}

const vec3 NORMALS[6] = vec3[6](
    vec3(-1.0, 0.0, 0.0),
    vec3(1.0, 0.0, 0.0),
    vec3(0.0, 0.0, 1.0),
    vec3(0.0, 0.0, -1.0),
    vec3(0.0, 1.0, 0.0),
    vec3(0.0, -1.0, 0.0)
);

vec3 getNormal(int face) {
    return NORMALS[face];
}

void main()	{
    vec3 unpackedPos = unpackPos(attr.x);
#ifdef CROSS
    int blockId = int(attr.x >> 24u);
    float light = float(attr.y&0xFu)/15.0 * 0.7 + 0.3;
    bool useColor = (attr.y & (1u << 31u)) != 0u;
    vColor = vec4(unpackColor(blockId, useColor) * vec3(light), 1.0);
    vNormal = normal;
    gl_Position = projectionMatrix * modelViewMatrix * vec4(position + unpackedPos, 1.0 );
    vTexCoord = vec2(uv.x, 1.0 - uv.y);
    vTexLayer = blockId;
#else
#ifdef CUBOID
    int blockId = int((attr.x >> 24u) | (((attr.y >> 24u) & 255u) << 8u));
#else
    int blockId = int(attr.x >> 24u);
#endif

    int face = int(attr.y & 7u);
    float sideLight = float((attr.y >> 3u) & 15u) / 15.0 * 0.7 + 0.3;
#ifdef FALLBACK
    bool sideSpecial = false;
    blockId |= int(attr.y >> 22u) & 256;
#else
    bool sideSpecial = (attr.y & (1u << 7u)) != 0u;
#endif
    bool useColor = (attr.y & (1u << 8u)) != 0u;

#ifdef CUBOID
    sideSpecial = false;
    uvec4 p0 = fetchCuboidPixel(blockId, 0);
    if ((p0.a & 1u) == 0u) useColor = false;
    vec2 fxy = unpackHalf2x16(p0.r);
    vec2 fz_tx = unpackHalf2x16(p0.g);
    vec2 tytz = unpackHalf2x16(p0.b);
    vec3 from = vec3(fxy.x, fxy.y, fz_tx.x);
    vec3 to = vec3(fz_tx.y, tytz.x, tytz.y);
    vec3 scale = (to - from) / 16.0;
    vec3 localOffset = from / 16.0;
    vec3 localPos = getLocalPos(face, position) * scale + localOffset;
#else
    vec3 localPos = getLocalPos(face, position);
#endif
    gl_Position = projectionMatrix * modelViewMatrix * vec4(localPos + unpackedPos, 1.0 );
    vColor = vec4(unpackColor(blockId, useColor) * vec3(sideLight), 1.0);
    vNormal = getNormal(face);

    vec2 mappedLocalUV = vec2(uv.x, 1.0 - uv.y);
    if (face >= 2) {
        mappedLocalUV.x = 1.0 - mappedLocalUV.x;
    }

#ifdef CUBOID
    int pixelOffset = 1 + (face >> 1);
    uvec4 pixelData = fetchCuboidPixel(blockId, pixelOffset);
    uint packedUVStart = ((face & 1) == 0) ? pixelData.r : pixelData.b;
    uint packedUVEnd = ((face & 1) == 0) ? pixelData.g : pixelData.a;
    vec4 faceUv = vec4(unpackUV(packedUVStart), unpackUV(packedUVEnd));

    // Extract block atlas offset using floor on minimum bounds
    float tx = floor(min(faceUv.x, faceUv.z));
    float ty = floor(min(faceUv.y, faceUv.w));
    vTexLayer = int(tx + ty * 64.0);

    // Map custom UV dimensions locally relative to the layer bounds
    vTexCoord = mix(faceUv.xy, faceUv.zw, mappedLocalUV) - vec2(tx, ty);
#else
    int block = (blockId + (sideSpecial ? 256 : 0));
    vTexLayer = block;
    vTexCoord = mappedLocalUV;
#endif
#endif // CROSS
}
