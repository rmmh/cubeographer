#version 300 es
precision highp float;
precision highp int;

in vec3 vLocalPos;
in vec3 vLocalCam;
flat in uint vNormalIndex;

uniform sampler2D texTopColor;
uniform sampler2D texNorthColor;
uniform sampler2D texSouthColor;
uniform sampler2D texEastColor;
uniform sampler2D texWestColor;

uniform mat4 projectionMatrix;
uniform mat4 modelViewMatrix;
uniform float uFogScale;
uniform float uChunkMaxY[4];

out vec4 outColor;

void main() {
    // Dynamic quadrant masking based on LOD0 loaded heights
    int quadIndex = (vLocalPos.x >= 0.5 ? 1 : 0) + (vLocalPos.z >= 0.5 ? 2 : 0);
    if (vLocalPos.y * 320.0 <= uChunkMaxY[quadIndex]) {
        discard;
    }

    vec3 voxelColor = vec3(0.0);
    vec3 normal = vec3(0.0, 1.0, 0.0);

    // Coordinate scaling to texture space (same as original raymarching)
    ivec2 ipTop = ivec2(clamp(int(vLocalPos.x * 256.0), 0, 255), clamp(int(vLocalPos.z * 256.0), 0, 255));
    ivec2 ipSide = ivec2(
        clamp(int(vLocalPos.x * 256.0), 0, 255),
        clamp(int((1.0 - vLocalPos.y) * 160.0), 0, 159)
    );
    ivec2 ipSideZ = ivec2(
        clamp(int(vLocalPos.z * 256.0), 0, 255),
        clamp(int((1.0 - vLocalPos.y) * 160.0), 0, 159)
    );

    if (vNormalIndex == 0u) { // Top (+Y)
        normal = vec3(0.0, 1.0, 0.0);
        voxelColor = texelFetch(texTopColor, ipTop, 0).rgb;
    } else if (vNormalIndex == 1u) { // Bottom (-Y)
        normal = vec3(0.0, -1.0, 0.0);
        voxelColor = texelFetch(texTopColor, ipTop, 0).rgb;
    } else if (vNormalIndex == 2u) { // South (+Z)
        normal = vec3(0.0, 0.0, 1.0);
        voxelColor = texelFetch(texNorthColor, ipSide, 0).rgb; // Matches the original mapping
    } else if (vNormalIndex == 3u) { // North (-Z)
        normal = vec3(0.0, 0.0, -1.0);
        voxelColor = texelFetch(texSouthColor, ipSide, 0).rgb; // Matches the original mapping
    } else if (vNormalIndex == 4u) { // East (+X)
        normal = vec3(1.0, 0.0, 0.0);
        voxelColor = texelFetch(texEastColor, ipSideZ, 0).rgb; // Matches the original mapping
    } else { // West (-X) (normalIndex == 5)
        normal = vec3(-1.0, 0.0, 0.0);
        voxelColor = texelFetch(texWestColor, ipSideZ, 0).rgb; // Matches the original mapping
    }

    float diff = max(0.5, dot(vec3(abs(normal.x), normal.y, abs(normal.z)), vec3(0.6, 1.0, 0.8)));

    // Calculate clip distance for fog
    vec4 clipPos = projectionMatrix * modelViewMatrix * vec4(vLocalPos * vec3(512.0, 320.0, 512.0), 1.0);

    outColor = mix(
        vec4(voxelColor * diff, 1.0),
        vec4(0.722, 0.855, 1.0, 1.0),
        min(1.0, clipPos.w / 3000.0) * uFogScale
    );
}
