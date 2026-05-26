#version 300 es
precision highp float;
precision highp int;

in vec3 vLocalPos;
in vec3 vLocalCam;

uniform sampler2D texTop;
uniform sampler2D texNorth;
uniform sampler2D texSouth;
uniform sampler2D texEast;
uniform sampler2D texWest;

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

// One 8-bit step: bridges the quantization gap at region boundaries where the
// encoded depth (capped at 254/255) just barely fails the boundary comparison.
const float DEPTH_BIAS = 1.0 / 255.0;

bool isSolid(vec3 p) {
    if (p.x < 0.0 || p.x > 1.0 || p.y < 0.0 || p.y > 1.0 || p.z < 0.0 || p.z > 1.0)
        return false;

    int quadIndex = (p.x >= 0.5 ? 1 : 0) + (p.z >= 0.5 ? 2 : 0);
    if (p.y * 320.0 <= uChunkMaxY[quadIndex]) {
        return false;
    }

    ivec3 ip = clamp(ivec3(floor(p * vec3(256.0, 160.0, 256.0))), ivec3(0), ivec3(255, 159, 255));

    float top_depth   = texelFetch(texTop,   ivec2(ip.x, ip.z), 0).r;
    float north_depth = texelFetch(texNorth, ivec2(ip.x, 159 - ip.y), 0).r;
    float south_depth = texelFetch(texSouth, ivec2(ip.x, 159 - ip.y), 0).r;
    float east_depth  = texelFetch(texEast,  ivec2(ip.z, 159 - ip.y), 0).r;
    float west_depth  = texelFetch(texWest,  ivec2(ip.z, 159 - ip.y), 0).r;

    bool solid_top   = (top_depth   < 0.999) && (p.y <= top_depth   + DEPTH_BIAS);
    bool solid_north = (north_depth < 0.999) && (p.z <= north_depth + DEPTH_BIAS);
    bool solid_south = (south_depth < 0.999) && (p.z >= south_depth - DEPTH_BIAS);
    bool solid_east  = (east_depth  < 0.999) && (p.x <= east_depth  + DEPTH_BIAS);
    bool solid_west  = (west_depth  < 0.999) && (p.x >= west_depth  - DEPTH_BIAS);

    return solid_top && solid_north && solid_south && solid_east && solid_west;
}

bool isSolidCoarse(vec3 p, int level) {
    if (p.x < 0.0 || p.x > 1.0 || p.y < 0.0 || p.y > 1.0 || p.z < 0.0 || p.z > 1.0)
        return false;

    int quadIndex = (p.x >= 0.5 ? 1 : 0) + (p.z >= 0.5 ? 2 : 0);
    if (p.y * 320.0 <= uChunkMaxY[quadIndex]) {
        return false;
    }

    ivec3 dims = ivec3(256 >> level, 160 >> level, 256 >> level);
    ivec3 ip = clamp(ivec3(floor(p * vec3(dims))), ivec3(0), dims - ivec3(1));

    float top_depth   = texelFetch(texTop,   ivec2(ip.x, ip.z), level).r;
    float north_depth = texelFetch(texNorth, ivec2(ip.x, dims.y - 1 - ip.y), level).r;
    float south_depth = texelFetch(texSouth, ivec2(ip.x, dims.y - 1 - ip.y), level).r;
    float east_depth  = texelFetch(texEast,  ivec2(ip.z, dims.y - 1 - ip.y), level).r;
    float west_depth  = texelFetch(texWest,  ivec2(ip.z, dims.y - 1 - ip.y), level).r;

    float x_min = float(ip.x) / float(dims.x);
    float x_max = float(ip.x + 1) / float(dims.x);
    float y_min = float(ip.y) / float(dims.y);
    float y_max = float(ip.y + 1) / float(dims.y);
    float z_min = float(ip.z) / float(dims.z);
    float z_max = float(ip.z + 1) / float(dims.z);

    bool solid_top   = (top_depth   < 0.999) && (y_min <= top_depth   + DEPTH_BIAS);
    bool solid_north = (north_depth < 0.999) && (z_min <= north_depth + DEPTH_BIAS);
    bool solid_south = (south_depth < 0.999) && (z_max >= south_depth - DEPTH_BIAS);
    bool solid_east  = (east_depth  < 0.999) && (x_min <= east_depth  + DEPTH_BIAS);
    bool solid_west  = (west_depth  < 0.999) && (x_max >= west_depth  - DEPTH_BIAS);

    return solid_top && solid_north && solid_south && solid_east && solid_west;
}

void main() {
    vec3 rayOrigin = vLocalCam;
    rayOrigin.y -= 1.0 / 320.0;
    vec3 rayDir = normalize(vLocalPos - vLocalCam);

    // IEEE 754 division by zero produces ±infinity, which is correct for slab intersection.
    vec3 t0 = (vec3(0.0) - rayOrigin) / rayDir;
    vec3 t1 = (vec3(1.0) - rayOrigin) / rayDir;
    vec3 tMin = min(t0, t1);
    vec3 tMax = max(t0, t1);

    float t_enter = max(max(tMin.x, tMin.y), tMin.z);
    float t_exit  = min(min(tMax.x, tMax.y), tMax.z);
    t_enter = max(t_enter, 0.0);

    if (t_enter > t_exit) discard;
    if (isSolid(rayOrigin)) discard;

    int hitFace = 0;
    if (tMin.x >= tMin.y && tMin.x >= tMin.z) {
        hitFace = 2;
    } else if (tMin.y >= tMin.x && tMin.y >= tMin.z) {
        hitFace = 0;
    } else {
        hitFace = 1;
    }

    const vec3 invGridDim = vec3(1.0 / 512.0, 1.0 / 320.0, 1.0 / 512.0);
    vec3 rayStart = rayOrigin + t_enter * rayDir;
    ivec3 ip = clamp(ivec3(floor(rayStart * vec3(512.0, 320.0, 512.0))), ivec3(0), ivec3(511, 319, 511));

    ivec3 stepVec = ivec3(
        rayDir.x >= 0.0 ? 1 : -1,
        rayDir.y >= 0.0 ? 1 : -1,
        rayDir.z >= 0.0 ? 1 : -1
    );

    vec3 safeRayDir = vec3(
        abs(rayDir.x) < 1e-9 ? 1e-9 : rayDir.x,
        abs(rayDir.y) < 1e-9 ? 1e-9 : rayDir.y,
        abs(rayDir.z) < 1e-9 ? 1e-9 : rayDir.z
    );

    vec3 deltaT = invGridDim / abs(safeRayDir);

    vec3 nextVoxelBoundary = vec3(
        rayDir.x >= 0.0 ? float(ip.x + 1) : float(ip.x),
        rayDir.y >= 0.0 ? float(ip.y + 1) : float(ip.y),
        rayDir.z >= 0.0 ? float(ip.z + 1) : float(ip.z)
    );

    vec3 nextT = t_enter + (nextVoxelBoundary * invGridDim - rayStart) / safeRayDir;

    float t_current = t_enter;
    bool hit = false;
    vec3 hitPos = vec3(0.0);

    // Precise DDA traversal loop
    for (int stepIdx = 0; stepIdx < 1344; stepIdx++) {
        if (t_current >= t_exit) {
            break;
        }

        vec3 p = (vec3(ip) + 0.5) * invGridDim;

        // Hierarchical DDA space skipping
        int skipLevel = -1;
        for (int lvl = 3; lvl >= 1; lvl--) {
            if (!isSolidCoarse(p, lvl)) {
                skipLevel = lvl;
                break;
            }
        }

        if (skipLevel >= 0) {
            int S = 1 << (skipLevel + 1);
            ivec3 ip_coarse = ip / S;

            vec3 coarseBoundary = vec3(
                stepVec.x >= 0 ? float((ip_coarse.x + 1) * S) : float(ip_coarse.x * S),
                stepVec.y >= 0 ? float((ip_coarse.y + 1) * S) : float(ip_coarse.y * S),
                stepVec.z >= 0 ? float((ip_coarse.z + 1) * S) : float(ip_coarse.z * S)
            );

            vec3 t_boundary = t_enter + (coarseBoundary * invGridDim - rayStart) / safeRayDir;
            float t_exit_block = min(t_boundary.x, min(t_boundary.y, t_boundary.z));

            if (t_exit_block > t_current) {
                t_current = t_exit_block;
                vec3 p_exit = rayStart + (t_current - t_enter) * rayDir;

                ivec3 ip_exit = clamp(ivec3(
                    int(floor(p_exit.x * 512.0 + float(stepVec.x) * 1e-5)),
                    int(floor(p_exit.y * 320.0 + float(stepVec.y) * 1e-5)),
                    int(floor(p_exit.z * 512.0 + float(stepVec.z) * 1e-5))
                ), ivec3(0), ivec3(511, 319, 511));

                if (t_boundary.x <= t_boundary.y && t_boundary.x <= t_boundary.z) {
                    hitFace = 2;
                    ip = ivec3(
                        stepVec.x >= 0 ? (ip_coarse.x + 1) * S : ip_coarse.x * S - 1,
                        ip_exit.y,
                        ip_exit.z
                    );
                } else if (t_boundary.y <= t_boundary.x && t_boundary.y <= t_boundary.z) {
                    hitFace = 0;
                    ip = ivec3(
                        ip_exit.x,
                        stepVec.y >= 0 ? (ip_coarse.y + 1) * S : ip_coarse.y * S - 1,
                        ip_exit.z
                    );
                } else {
                    hitFace = 1;
                    ip = ivec3(
                        ip_exit.x,
                        ip_exit.y,
                        stepVec.z >= 0 ? (ip_coarse.z + 1) * S : ip_coarse.z * S - 1
                    );
                }
                ip = clamp(ip, ivec3(0), ivec3(511, 319, 511));

                vec3 nextVoxelBoundary = vec3(
                    rayDir.x >= 0.0 ? float(ip.x + 1) : float(ip.x),
                    rayDir.y >= 0.0 ? float(ip.y + 1) : float(ip.y),
                    rayDir.z >= 0.0 ? float(ip.z + 1) : float(ip.z)
                );
                nextT = t_enter + (nextVoxelBoundary * invGridDim - rayStart) / safeRayDir;
                continue;
            }
        }

        if (isSolid(p)) {
            hit = true;
            hitPos = rayOrigin + t_current * rayDir;
            break;
        }

        if (nextT.x < nextT.y) {
            if (nextT.x < nextT.z) {
                t_current = nextT.x;
                nextT.x += deltaT.x;
                ip.x += stepVec.x;
                hitFace = 2;
                if (ip.x < 0 || ip.x >= 512) break;
            } else {
                t_current = nextT.z;
                nextT.z += deltaT.z;
                ip.z += stepVec.z;
                hitFace = 1;
                if (ip.z < 0 || ip.z >= 512) break;
            }
        } else {
            if (nextT.y < nextT.z) {
                t_current = nextT.y;
                nextT.y += deltaT.y;
                ip.y += stepVec.y;
                hitFace = 0;
                if (ip.y < 0 || ip.y >= 320) break;
            } else {
                t_current = nextT.z;
                nextT.z += deltaT.z;
                ip.z += stepVec.z;
                hitFace = 1;
                if (ip.z < 0 || ip.z >= 512) break;
            }
        }
    }

    if (!hit) discard;

    vec3 voxelColor = vec3(0.0);
    vec3 normal = vec3(0.0, 1.0, 0.0);

    if (hitFace == 0) {
        normal = vec3(0.0, rayDir.y < 0.0 ? 1.0 : -1.0, 0.0);
        voxelColor = texelFetch(texTopColor, ivec2(ip.x / 2, ip.z / 2), 0).rgb;
    } else if (hitFace == 1) {
        normal = vec3(0.0, 0.0, rayDir.z < 0.0 ? 1.0 : -1.0);
        ivec2 coord = ivec2(ip.x / 2, (319 - ip.y) / 2);
        if (normal.z > 0.0) {
            voxelColor = texelFetch(texNorthColor, coord, 0).rgb;
        } else {
            voxelColor = texelFetch(texSouthColor, coord, 0).rgb;
        }
    } else { // hitFace == 2
        normal = vec3(rayDir.x < 0.0 ? 1.0 : -1.0, 0.0, 0.0);
        ivec2 coord = ivec2(ip.z / 2, (319 - ip.y) / 2);
        if (normal.x > 0.0) {
            voxelColor = texelFetch(texEastColor, coord, 0).rgb;
        } else {
            voxelColor = texelFetch(texWestColor, coord, 0).rgb;
        }
    }

    float diff = max(0.5, dot(vec3(abs(normal.x), normal.y, abs(normal.z)), vec3(0.6, 1.0, 0.8)));

    // Write depth at the raymarched hit point so adjacent impostors occlude correctly.
    vec4 clipHit = projectionMatrix * modelViewMatrix * vec4(hitPos * vec3(512.0, 320.0, 512.0), 1.0);
    gl_FragDepth = clipHit.z / clipHit.w * 0.5 + 0.5;

    outColor = mix(
        vec4(voxelColor * diff, 1.0),
        vec4(0.722, 0.855, 1.0, 1.0),
        min(1.0, clipHit.w / 3000.0) * uFogScale
    );
}
