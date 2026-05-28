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
    int pixelIndex = blockId * 5 + pixelOffset;
    ivec2 texCoord = ivec2(pixelIndex & 511, pixelIndex >> 9);
    return texelFetch(cuboidDataTex, texCoord, 0);
}

vec2 unpackUV(uint p) {
    return vec2(float(p & 0x3FFFu) / 256.0, float((p >> 16u) & 0x3FFFu) / 256.0);
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
out vec2 vTexCoord;
flat out int vTexLayer;

vec3 unpackPos(uint p) {
    uvec3 raw = uvec3(p >> 16u, p, p >> 8u);
    return vec3(raw & 255u);
}

vec3 unpackColor(int blockId, bool useColor, uint packedColor) {
    if (!useColor) return vec3(1.0);
    if (packedColor != 0u) {
        float r = float((packedColor >> 16u) & 255u) / 255.0;
        float g = float((packedColor >> 8u) & 255u) / 255.0;
        float b = float(packedColor & 255u) / 255.0;
        return vec3(r, g, b);
    }
#ifdef WATER_ID
    if (blockId == WATER_ID)
        return vec3(0.2, 0.4, 0.93);
#endif
    // TODO: read biome color from texture?
    return vec3(0.4,0.73,0.27);
}

vec3 unpackColor(int blockId, bool useColor) {
    return unpackColor(blockId, useColor, 0u);
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
    gl_Position = projectionMatrix * modelViewMatrix * vec4(position + unpackedPos, 1.0 );
    vTexCoord = vec2(uv.x, 1.0 - uv.y);
    vTexLayer = blockId;
#else
    uint packedColor = 0u;
    bool useColor = false;
#ifdef CUBOID
    int blockId = int((attr.x >> 24u) | (((attr.y >> 24u) & 255u) << 8u));
#else
    int blockId = int(attr.x >> 24u);
    useColor = (attr.y & (1u << 8u)) != 0u;
#endif

    int face = int(attr.y & 7u);
    float sideLight = float((attr.y >> 3u) & 15u) / 15.0 * 0.7 + 0.3;
#ifdef FALLBACK
    bool sideSpecial = false;
    blockId |= int(attr.y >> 22u) & 256;
#else
    bool sideSpecial = (attr.y & (1u << 7u)) != 0u;
#endif

#ifdef CUBOID
    sideSpecial = false;
    uvec4 p0 = fetchCuboidPixel(blockId, 0);
    uint packedRot = p0.a;
    packedColor = fetchCuboidPixel(blockId, 4).r;
    useColor = (packedRot & 1u) != 0u;
    vec2 fxy = unpackHalf2x16(p0.r);
    vec2 fz_tx = unpackHalf2x16(p0.g);
    vec2 tytz = unpackHalf2x16(p0.b);
    vec3 from = vec3(fxy.x, fxy.y, fz_tx.x);
    vec3 to = vec3(fz_tx.y, tytz.x, tytz.y);
    vec3 scale = (to - from) / 16.0;
    vec3 localOffset = from / 16.0;
    vec3 localPos = getLocalPos(face, position) * scale + localOffset;

    vec3 n = getNormal(face);
    uint rotAxis = (packedRot >> 1u) & 3u;
    if (rotAxis != 0u) {
        bool rotRescale = (packedRot & (1u << 3u)) != 0u;
        uint rotAngleIdx = (packedRot >> 4u) & 7u;
        float rotAngle = 0.0;
        if (rotAngleIdx == 1u) rotAngle = -22.5;
        else if (rotAngleIdx == 2u) rotAngle = 22.5;
        else if (rotAngleIdx == 3u) rotAngle = -45.0;
        else if (rotAngleIdx == 4u) rotAngle = 45.0;

        float rad = rotAngle * 3.14159265359 / 180.0;
        vec3 originLocal = vec3(
            float((packedRot >> 7u) & 255u) / 8.0 - 8.0,
            float((packedRot >> 15u) & 255u) / 8.0 - 8.0,
            float((packedRot >> 23u) & 255u) / 8.0 - 8.0
        ) / 16.0;

        vec3 p = localPos - originLocal;
        if (rotAxis == 1u) {
            float c = cos(rad);
            float s = sin(rad);
            float yNew = p.y * c - p.z * s;
            float zNew = p.y * s + p.z * c;
            p.y = yNew;
            p.z = zNew;
            if (rotRescale) {
                float factor = 1.0 / abs(c);
                p.y *= factor;
                p.z *= factor;
            }

            float nyNew = n.y * c - n.z * s;
            float nzNew = n.y * s + n.z * c;
            n.y = nyNew;
            n.z = nzNew;
        } else if (rotAxis == 2u) {
            float c = cos(rad);
            float s = sin(rad);
            float xNew = p.x * c + p.z * s;
            float zNew = -p.x * s + p.z * c;
            p.x = xNew;
            p.z = zNew;
            if (rotRescale) {
                float factor = 1.0 / abs(c);
                p.x *= factor;
                p.z *= factor;
            }

            float nxNew = n.x * c + n.z * s;
            float nzNew = -n.x * s + n.z * c;
            n.x = nxNew;
            n.z = nzNew;
        } else if (rotAxis == 3u) {
            float c = cos(rad);
            float s = sin(rad);
            float xNew = p.x * c - p.y * s;
            float yNew = p.x * s + p.y * c;
            p.x = xNew;
            p.y = yNew;
            if (rotRescale) {
                float factor = 1.0 / abs(c);
                p.x *= factor;
                p.y *= factor;
            }

            float nxNew = n.x * c - n.y * s;
            float nyNew = n.x * s + n.y * c;
            n.x = nxNew;
            n.y = nyNew;
        }
        localPos = p + originLocal;
    }
#else
    vec3 localPos = getLocalPos(face, position);
    vec3 n = getNormal(face);
#endif
    gl_Position = projectionMatrix * modelViewMatrix * vec4(localPos + unpackedPos, 1.0 );
    vec3 nn = normalize(n);
    // OFFICIAL MINECRAFT: *0.8 on the Z axis faces, *0.6 on the X axis faces, *0.5 on the bottom face
#ifdef CUBOID
    bool noShade = (packedRot & (1u << 31u)) != 0u;
    float normalDarkening = noShade ? 1.0 : max(0.5, dot(vec3(abs(nn.x), nn.y, abs(nn.z)), vec3(0.6, 1.0, 0.8)));
#else
    float normalDarkening = max(0.5, dot(vec3(abs(nn.x), nn.y, abs(nn.z)), vec3(0.6, 1.0, 0.8)));
#endif
    vColor = vec4(unpackColor(blockId, useColor, packedColor) * vec3(sideLight * normalDarkening), 1.0);

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

    // Rotate standard UV first, then apply face reflection
    uint rotIdx = (packedUVStart >> 14u) & 3u;
    if (face >= 2) {
        if (rotIdx == 1u) {
            rotIdx = 3u;
        } else if (rotIdx == 3u) {
            rotIdx = 1u;
        }
    }
    vec2 standardUV = vec2(uv.x, 1.0 - uv.y);
    vec2 rotatedUV = standardUV;
    if (rotIdx == 1u) {
        rotatedUV = vec2(1.0 - standardUV.y, standardUV.x);
    } else if (rotIdx == 2u) {
        rotatedUV = vec2(1.0 - standardUV.x, 1.0 - standardUV.y);
    } else if (rotIdx == 3u) {
        rotatedUV = vec2(standardUV.y, 1.0 - standardUV.x);
    }

    if (face >= 2) {
        rotatedUV.x = 1.0 - rotatedUV.x;
    }

    // Map custom UV dimensions locally relative to the layer bounds
    vTexCoord = mix(faceUv.xy, faceUv.zw, rotatedUV) - vec2(tx, ty);
#else
    int block = (blockId + (sideSpecial ? 256 : 0));
    vTexLayer = block;
    vTexCoord = mappedLocalUV;
#endif
#endif // CROSS
}
