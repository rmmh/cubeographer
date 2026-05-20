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
in vec4 color;
in vec3 normal;
in vec2 uv;
in uvec2 attr;
in uint normb;

out vec3 vPosition;
out vec4 vColor;
out vec3 vNormal;
out vec2 vTexCoord;
flat out int vTexLayer;

vec3 unpackPos(uint p) { // 24b pos (8b,8b,8b each) => vec3
    return vec3(float((p >> 16) & 255u) , float(p & 255u), float((p >> 8) & 255u));
}

vec3 unpackColor(int blockId, uint color) {

    // TODO: read biome color from texture?
#ifdef CUBOID
    uvec4 p0 = fetchCuboidPixel(blockId, 0);
    if ((p0.a & 1u) == 0u) return vec3(1.0);
#else
    if ((color & (1u << 31)) == 0u)
        return vec3(1.0);
#endif
#ifdef WATER_ID
    if (blockId == WATER_ID)
        return vec3(0.2, 0.4, 0.93);
#endif
    return vec3(0.4,0.73,0.27);
}

bool shouldDiscard(int face, uint s, int blockId) {
#ifdef CUBOID
    if (face == 4) {
        uvec4 p0 = fetchCuboidPixel(blockId, 0);
        return (p0.a & 2u) != 0u;
    }
#endif
    return (s & uint(1 << face)) == 0u;
}

void main()	{
    vec3 unpackedPos = unpackPos(attr.x);
#ifdef CUBOID
    int blockId = int((attr.x >> 24u) | ((attr.y >> 30u) << 8u) | (((attr.y >> 4u) & 1u) << 10u));
#else
    int blockId = int(attr.x >> 24u);
#endif
#ifdef CROSS
    float light = float(attr.y&0xFu)/15.0 * 0.7 + 0.3;
    vColor = vec4(unpackColor(blockId, attr.y) * vec3(light), 1.0);
    // if (face >= 2)vColor = vec4(1,shouldDiscard(face, attr.y, blockId),0,1);
    bool sideSpecial = false;
    vNormal = normal;
    gl_Position = projectionMatrix * modelViewMatrix * vec4(position + unpackedPos, 1.0 );
#else

#ifdef CUBOID
    uvec4 p0 = fetchCuboidPixel(blockId, 0);
    vec2 fxy = unpackHalf2x16(p0.r);
    vec2 fz_tx = unpackHalf2x16(p0.g);
    vec2 tytz = unpackHalf2x16(p0.b);
    vec3 from = vec3(fxy.x, fxy.y, fz_tx.x);
    vec3 to = vec3(fz_tx.y, tytz.x, tytz.y);
    vec3 worldMidpoint = unpackedPos + offset + (from + to) / 32.0;
    bool shouldFlip = dot(normal, cameraPosition - worldMidpoint) < 0.0;
#else
    bool shouldFlip = dot(normal, cameraPosition - (unpackedPos + offset)) < 0.0;
#endif

    int face = int(gl_VertexID / 6) * 2 + (shouldFlip ? 1 : 0);
    if (shouldDiscard(face, attr.y, blockId)) {
        gl_Position = vec4(1e20);
        return;
    }
    float sideLight = float( (attr.y>>uint(6+face*4))&0xFu)/15.0 * 0.7 + 0.3;
#ifdef FALLBACK
    bool sideSpecial = false;
    blockId |= int(attr.y>>22) & 256;
#else
    bool sideSpecial = face >= 4 && (attr.y & (1u<<30)) != 0u;
#endif

#ifdef CUBOID
    sideSpecial = false;
#endif

    vColor = vec4(unpackColor(blockId, attr.y) * vec3(sideLight), 1.0);

#ifdef CUBOID
    vec3 scale = (to - from) / 16.0;
    vec3 localOffset = from / 16.0;
    vec3 localPos = (shouldFlip ? vec3(1.0) - position : position) * scale + localOffset;
#else
    vec3 localPos = shouldFlip ? vec3(1.0) - position : position;
#endif

    gl_Position = projectionMatrix * modelViewMatrix * vec4(localPos + unpackedPos, 1.0 );
    vNormal = normal * vec3(shouldFlip ? -1.0 : 1.0);
#endif // CROSS
    vec2 localUV = vec2(uv.x, uv.y);

#ifdef CROSS
    vec2 mappedLocalUV = vec2(1.0) - localUV;
#else
    vec2 mappedLocalUV = shouldFlip ? localUV : vec2(1.0) - localUV;
#endif

#ifdef CUBOID
    int pixelOffset = 1 + (face >> 1);
    uvec4 pixelData = fetchCuboidPixel(blockId, pixelOffset);
    uint packedUVStart = ((face & 1) == 0) ? pixelData.r : pixelData.b;
    uint packedUVEnd = ((face & 1) == 0) ? pixelData.g : pixelData.a;
    vec4 faceUv = vec4(unpackUV(packedUVStart), unpackUV(packedUVEnd));

    // Extract block atlas offset using floor on minimum bounds
    float tx = floor(min(faceUv.x, faceUv.z));
    float ty = floor(min(faceUv.y, faceUv.w));
    vTexLayer = int(tx + ty * 32.0);

    // Map custom UV dimensions locally relative to the layer bounds
    vec2 uvStart = faceUv.xy - vec2(tx, ty);
    vec2 uvEnd = faceUv.zw - vec2(tx, ty);
    vTexCoord = mix(uvStart, uvEnd, mappedLocalUV);
#else
    int block = (blockId + (sideSpecial ? 256 : 0));
    vTexLayer = block;
    vTexCoord = mappedLocalUV;
#endif
}
